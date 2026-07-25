package ctrl

import (
	"encoding/binary"
	"testing"

	"github.com/ehrlich-b/go-ublk/internal/uapi"
)

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
