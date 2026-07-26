package ctrl

import (
	"encoding/binary"
	"testing"
	"unsafe"

	"github.com/ehrlich-b/go-ublk/internal/uapi"
)

// plainBackend implements interfaces.Backend but NOT DiscardBackend.
type plainBackend struct{}

func (plainBackend) ReadAt(p []byte, off int64) (int, error)  { return len(p), nil }
func (plainBackend) WriteAt(p []byte, off int64) (int, error) { return len(p), nil }
func (plainBackend) Size() int64                              { return 1 << 20 }
func (plainBackend) Close() error                             { return nil }
func (plainBackend) Flush() error                             { return nil }

// discardingBackend additionally implements interfaces.DiscardBackend.
type discardingBackend struct{ plainBackend }

func (discardingBackend) Discard(offset, length int64) error { return nil }

// The device attributes are plumbed all the way from the public DeviceParams
// into ctrl.DeviceParams, so the only thing that can silently drop them is the
// SET_PARAMS payload. These tests pin both the flag mapping and the byte the
// kernel actually reads.
func TestBasicAttrs(t *testing.T) {
	tests := []struct {
		name   string
		params DeviceParams
		want   uint32
	}{
		{"none", DeviceParams{}, 0},
		{"read-only", DeviceParams{ReadOnly: true}, uapi.UBLK_ATTR_READ_ONLY},
		{"rotational", DeviceParams{Rotational: true}, uapi.UBLK_ATTR_ROTATIONAL},
		{"volatile cache", DeviceParams{VolatileCache: true}, uapi.UBLK_ATTR_VOLATILE_CACHE},
		{
			"read-only rotational with cache",
			DeviceParams{ReadOnly: true, Rotational: true, VolatileCache: true},
			uapi.UBLK_ATTR_READ_ONLY | uapi.UBLK_ATTR_ROTATIONAL | uapi.UBLK_ATTR_VOLATILE_CACHE,
		},
		// FUA is accepted by the API but must never be advertised until the
		// per-IO UBLK_IO_F_FUA flag is honored: claiming it without honoring it
		// is a silent durability lie on power loss.
		{"fua alone is not advertised", DeviceParams{EnableFUA: true}, 0},
		{
			"fua does not leak in alongside cache",
			DeviceParams{VolatileCache: true, EnableFUA: true},
			uapi.UBLK_ATTR_VOLATILE_CACHE,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := basicAttrs(&tt.params); got != tt.want {
				t.Errorf("basicAttrs() = %#x, want %#x", got, tt.want)
			}
		})
	}
}

// TestSetParamsPayloadCarriesAttrs marshals the same structure SetParams sends
// and checks the attrs land where the kernel reads them: struct ublk_params is
// {len, types} followed by ublk_param_basic, whose first field is attrs.
func TestSetParamsPayloadCarriesAttrs(t *testing.T) {
	params := &DeviceParams{ReadOnly: true, VolatileCache: true}
	want := uapi.UBLK_ATTR_READ_ONLY | uapi.UBLK_ATTR_VOLATILE_CACHE

	buf := uapi.Marshal(&uapi.UblkParams{
		Types: uapi.UBLK_PARAM_TYPE_BASIC,
		Basic: uapi.UblkParamBasic{Attrs: basicAttrs(params)},
	})

	if len(buf) < 12 {
		t.Fatalf("marshaled params too short: %d bytes", len(buf))
	}
	if got := binary.LittleEndian.Uint32(buf[4:8]); got != uapi.UBLK_PARAM_TYPE_BASIC {
		t.Errorf("types = %#x, want %#x", got, uapi.UBLK_PARAM_TYPE_BASIC)
	}
	if got := binary.LittleEndian.Uint32(buf[8:12]); got != uint32(want) {
		t.Errorf("attrs in payload = %#x, want %#x", got, want)
	}
}

func TestDiscardParams(t *testing.T) {
	limits := DeviceParams{
		DiscardAlignment:   4096,
		DiscardGranularity: 4096,
		MaxDiscardSectors:  2048,
		MaxDiscardSegments: 1,
	}

	t.Run("backend without Discard is not advertised", func(t *testing.T) {
		p := limits
		p.Backend = plainBackend{}
		if _, ok := discardParams(&p); ok {
			t.Error("advertised discard limits for a backend that cannot discard")
		}
	})

	t.Run("zero max sectors opts out", func(t *testing.T) {
		p := limits
		p.Backend = discardingBackend{}
		p.MaxDiscardSectors = 0
		if _, ok := discardParams(&p); ok {
			t.Error("advertised discard with MaxDiscardSectors == 0")
		}
	})

	t.Run("discarding backend advertises its limits", func(t *testing.T) {
		p := limits
		p.Backend = discardingBackend{}
		got, ok := discardParams(&p)
		if !ok {
			t.Fatal("discard limits not advertised for a DiscardBackend")
		}
		if got.MaxDiscardSectors != 2048 || got.DiscardGranularity != 4096 ||
			got.DiscardAlignment != 4096 || got.MaxDiscardSegments != 1 {
			t.Errorf("limits = %+v, want the caller's values", got)
		}
		// Never advertise write-zeroes: the runner has no case for it.
		if got.MaxWriteZeroesSectors != 0 {
			t.Errorf("MaxWriteZeroesSectors = %d, want 0 (op not implemented)", got.MaxWriteZeroesSectors)
		}
	})

	// Both of these make the kernel reject the entire SET_PARAMS with -EINVAL,
	// which fails device creation, so they must be normalized here.
	t.Run("segments are clamped to the only value ublk accepts", func(t *testing.T) {
		p := limits
		p.Backend = discardingBackend{}
		p.MaxDiscardSegments = 256
		got, ok := discardParams(&p)
		if !ok {
			t.Fatal("discard limits not advertised")
		}
		if got.MaxDiscardSegments != 1 {
			t.Errorf("MaxDiscardSegments = %d, want 1", got.MaxDiscardSegments)
		}
	})

	t.Run("zero granularity falls back to the logical block size", func(t *testing.T) {
		p := limits
		p.Backend = discardingBackend{}
		p.DiscardGranularity = 0
		p.LogicalBlockSize = 512
		got, ok := discardParams(&p)
		if !ok {
			t.Fatal("discard limits not advertised")
		}
		if got.DiscardGranularity != 512 {
			t.Errorf("DiscardGranularity = %d, want 512", got.DiscardGranularity)
		}
	})
}

// The kernel reads struct ublk_params at fixed offsets, so the Go structs must
// match the UAPI sizes exactly or the discard block lands in the wrong place.
func TestParamStructSizesMatchUAPI(t *testing.T) {
	if got := unsafe.Sizeof(uapi.UblkParamBasic{}); got != 32 {
		t.Errorf("sizeof(ublk_param_basic) = %d, want 32", got)
	}
	if got := unsafe.Sizeof(uapi.UblkParamDiscard{}); got != 20 {
		t.Errorf("sizeof(ublk_param_discard) = %d, want 20", got)
	}
}
