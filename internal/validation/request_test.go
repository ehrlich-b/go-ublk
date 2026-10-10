package validation

import (
	"encoding/binary"
	"math"
	"math/big"
	"testing"

	"github.com/ehrlich-b/go-ublk/internal/uapi"
)

type requestCase struct {
	name  string
	p     RequestParams
	valid bool
}

func requestCases() []requestCase {
	base := RequestParams{Op: uapi.UBLK_IO_OP_READ, StartSector: 8, Sectors: 8,
		Limits: RequestLimits{Capacity: 1 << 20, LogicalBlockSize: 4096, MaxPayload: 128 << 10}}
	cases := []requestCase{{"valid-4Kn", base, true}}
	add := func(name string, valid bool, change func(*RequestParams)) {
		p := base
		change(&p)
		cases = append(cases, requestCase{name, p, valid})
	}
	add("first-block", true, func(p *RequestParams) { p.StartSector = 0 })
	add("last-block", true, func(p *RequestParams) { p.StartSector = (1<<20)/512 - 8 })
	add("start-at-end", false, func(p *RequestParams) { p.StartSector = (1 << 20) / 512 })
	add("start-past-end", false, func(p *RequestParams) { p.StartSector = (1<<20)/512 + 8 })
	add("end-past-capacity", false, func(p *RequestParams) { p.StartSector = (1<<20)/512 - 8; p.Sectors = 16 })
	add("start-unaligned", false, func(p *RequestParams) { p.StartSector++ })
	add("length-unaligned", false, func(p *RequestParams) { p.Sectors++ })
	add("maximum-payload", true, func(p *RequestParams) { p.Sectors = 256 })
	add("payload-over-limit", false, func(p *RequestParams) { p.Sectors = 264 })
	add("zero-payload", false, func(p *RequestParams) { p.Sectors = 0 })
	for _, start := range []uint64{uint64(math.MaxInt64)/512 + 1, 1 << 55, 1 << 63, math.MaxUint64} {
		add("start-conversion-overflow", false, func(p *RequestParams) { p.StartSector = start })
	}
	for _, n := range []uint64{uint64(math.MaxInt64)/512 + 1, 1 << 55, math.MaxUint64} {
		add("length-conversion-overflow", false, func(p *RequestParams) { p.Sectors = n; p.Op = uapi.UBLK_IO_OP_DISCARD })
	}
	add("uint32-sector-count", false, func(p *RequestParams) { p.Sectors = math.MaxUint32 })
	add("signed-last-sector", true, func(p *RequestParams) {
		p.Limits = RequestLimits{Capacity: math.MaxInt64 &^ 511, LogicalBlockSize: 512, MaxPayload: 512}
		p.StartSector = uint64(p.Limits.Capacity)/512 - 1
		p.Sectors = 1
	})
	add("signed-end-overflow", false, func(p *RequestParams) {
		p.Limits = RequestLimits{Capacity: math.MaxInt64 &^ 511, LogicalBlockSize: 512, MaxPayload: 1024}
		p.StartSector = uint64(p.Limits.Capacity)/512 - 1
		p.Sectors = 2
	})
	add("512-byte-block", true, func(p *RequestParams) { p.Limits.LogicalBlockSize = 512; p.StartSector = 1; p.Sectors = 1 })
	for _, op := range []uint8{uapi.UBLK_IO_OP_WRITE, uapi.UBLK_IO_OP_ZONE_APPEND} {
		add("data-op-valid", true, func(p *RequestParams) { p.Op = op })
		add("data-op-over-limit", false, func(p *RequestParams) { p.Op = op; p.Sectors = 264 })
	}
	for _, op := range []uint8{uapi.UBLK_IO_OP_DISCARD, uapi.UBLK_IO_OP_WRITE_ZEROES} {
		add("range-exceeds-payload", true, func(p *RequestParams) { p.Op = op; p.StartSector = 0; p.Sectors = (1 << 20) / 512 })
		add("range-end-outside", false, func(p *RequestParams) { p.Op = op; p.StartSector = 8; p.Sectors = (1 << 20) / 512 })
	}
	add("4GiB-discard", true, func(p *RequestParams) {
		p.Op = uapi.UBLK_IO_OP_DISCARD
		p.Limits.Capacity = 8 << 30
		p.Sectors = 1 << 23
	})
	add("flush", true, func(p *RequestParams) { p.Op = uapi.UBLK_IO_OP_FLUSH; p.Sectors = 0 })
	add("flush-range", false, func(p *RequestParams) { p.Op = uapi.UBLK_IO_OP_FLUSH })
	add("zero-capacity", false, func(p *RequestParams) { p.Limits.Capacity = 0 })
	add("negative-capacity", false, func(p *RequestParams) { p.Limits.Capacity = -1 })
	add("unaligned-capacity", false, func(p *RequestParams) { p.Limits.Capacity++ })
	add("zero-block", false, func(p *RequestParams) { p.Limits.LogicalBlockSize = 0 })
	add("non-power-block", false, func(p *RequestParams) { p.Limits.LogicalBlockSize = 1536 })
	add("subsector-block", false, func(p *RequestParams) { p.Limits.LogicalBlockSize = 256 })
	add("block-signed-overflow", false, func(p *RequestParams) { p.Limits.LogicalBlockSize = 1 << 63 })
	add("zero-max-payload", false, func(p *RequestParams) { p.Limits.MaxPayload = 0 })
	add("payload-signed-completion-overflow", false, func(p *RequestParams) { p.Limits.MaxPayload = 1 << 31 })
	add("unaligned-max-payload", false, func(p *RequestParams) { p.Limits.MaxPayload++ })
	add("negative-file-base", false, func(p *RequestParams) { p.Limits.FileBase = -1 })
	add("file-base-end-overflow", false, func(p *RequestParams) { p.Limits.FileBase = math.MaxInt64 - p.Limits.Capacity + 1 })
	add("last-file-base", true, func(p *RequestParams) { p.Limits.FileBase = math.MaxInt64 - p.Limits.Capacity })
	add("zone-report", true, func(p *RequestParams) { p.Op = uapi.UBLK_IO_OP_REPORT_ZONES; p.Sectors = 2 })
	add("zone-report-capped", true, func(p *RequestParams) { p.Op = uapi.UBLK_IO_OP_REPORT_ZONES; p.Sectors = math.MaxUint32 })
	add("zone-report-zero-count", false, func(p *RequestParams) { p.Op = uapi.UBLK_IO_OP_REPORT_ZONES; p.Sectors = 0 })
	add("zone-report-end", false, func(p *RequestParams) {
		p.Op = uapi.UBLK_IO_OP_REPORT_ZONES
		p.StartSector = uint64(p.Limits.Capacity) / 512
	})
	return cases
}

// rejectingBackend fails immediately if an invalid descriptor crosses the
// production dispatch seam; a successful result alone is not the oracle.
type rejectingBackend struct {
	t      *testing.T
	calls  int
	reject bool
}

func (b *rejectingBackend) dispatch(r RequestRange) {
	b.calls++
	if b.reject {
		b.t.Fatalf("invalid range reached backend: %+v", r)
	}
}

func TestRequestRangeBoundsBeforeBackend(t *testing.T) {
	for _, tc := range requestCases() {
		t.Run(tc.name, func(t *testing.T) {
			b := rejectingBackend{t: t, reject: !tc.valid}
			err := DispatchRequest(tc.p, b.dispatch)
			if (err == nil) != tc.valid {
				t.Fatalf("accepted=%v want %v: %v", err == nil, tc.valid, err)
			}
			if tc.valid && b.calls != 1 {
				t.Fatalf("backend calls=%d want 1", b.calls)
			}
			if tc.valid {
				r, _ := ValidateRequestRange(tc.p)
				assertRequestRange(t, tc.p, r)
			}
		})
	}
}

func assertRequestRange(t *testing.T, p RequestParams, r RequestRange) {
	t.Helper()
	off := new(big.Int).Mul(new(big.Int).SetUint64(p.StartSector), big.NewInt(512))
	if !off.IsInt64() || off.Int64() != r.Offset || r.Offset < 0 || r.Offset > p.Limits.Capacity {
		t.Fatal("accepted invalid signed offset")
	}
	if r.Offset%int64(p.Limits.LogicalBlockSize) != 0 {
		t.Fatal("accepted unaligned offset")
	}
	if p.Op == uapi.UBLK_IO_OP_REPORT_ZONES {
		want := min(p.Sectors*64, p.Limits.MaxPayload)
		if uint64(r.Length) != want {
			t.Fatal("incorrect report buffer length")
		}
		return
	}
	n := new(big.Int).Mul(new(big.Int).SetUint64(p.Sectors), big.NewInt(512))
	end := new(big.Int).Add(off, n)
	if !n.IsInt64() || n.Int64() != r.Length || r.Length < 0 || end.Cmp(big.NewInt(p.Limits.Capacity)) > 0 {
		t.Fatal("accepted invalid range end/length")
	}
	if r.Length%int64(p.Limits.LogicalBlockSize) != 0 {
		t.Fatal("accepted unaligned length")
	}
	if p.Op == uapi.UBLK_IO_OP_READ || p.Op == uapi.UBLK_IO_OP_WRITE || p.Op == uapi.UBLK_IO_OP_ZONE_APPEND {
		if r.Length == 0 || uint64(r.Length) > p.Limits.MaxPayload {
			t.Fatal("accepted invalid payload length")
		}
	}
	end.Add(end, big.NewInt(p.Limits.FileBase))
	if p.Limits.FileBase < 0 || !end.IsInt64() {
		t.Fatal("accepted overflowing file address")
	}
}

func encodeRequest(p RequestParams) []byte {
	words := []uint64{uint64(p.Op), p.StartSector, p.Sectors, uint64(p.Limits.Capacity), p.Limits.LogicalBlockSize, p.Limits.MaxPayload, uint64(p.Limits.FileBase)}
	b := make([]byte, len(words)*8)
	for i, v := range words {
		binary.LittleEndian.PutUint64(b[i*8:], v)
	}
	return b
}

func FuzzRequestRange(f *testing.F) {
	for _, tc := range requestCases() {
		f.Add(encodeRequest(tc.p))
	}
	f.Fuzz(func(t *testing.T, b []byte) {
		if len(b) != 7*8 {
			return
		}
		var w [7]uint64
		for i := range w {
			w[i] = binary.LittleEndian.Uint64(b[i*8:])
		}
		p := RequestParams{uint8(w[0]), w[1], w[2], RequestLimits{Capacity: int64(w[3]), FileBase: int64(w[6]), LogicalBlockSize: w[4], MaxPayload: w[5]}}
		calls := 0
		err := DispatchRequest(p, func(r RequestRange) { calls++; assertRequestRange(t, p, r) })
		if err != nil && calls != 0 {
			t.Fatal("rejected descriptor reached backend")
		}
		if err == nil && calls != 1 {
			t.Fatal("accepted descriptor did not reach backend exactly once")
		}
		if n, err := MetadataLength(w[2], w[4], w[5], 128<<10); err == nil {
			want := new(big.Int).Mul(new(big.Int).SetUint64(w[2]/w[4]), new(big.Int).SetUint64(w[5]))
			if !want.IsInt64() || want.Int64() != int64(n) || n > 128<<10 || n < 0 {
				t.Fatal("accepted invalid integrity span")
			}
		}
	})
}

func TestMetadataLengthBounds(t *testing.T) {
	for _, tc := range []struct {
		name                             string
		length, interval, metadata, size uint64
		want                             int
		valid                            bool
	}{
		{"exact-end", 4096, 512, 8, 64, 64, true},
		{"one-tuple-out", 4608, 512, 8, 64, 0, false},
		{"product-overflow", math.MaxInt64, 1, 8, 64, 0, false},
		{"unsigned-product-overflow", math.MaxUint64, 1, math.MaxUint64, 64, 0, false},
		{"zero-interval", 4096, 0, 8, 64, 0, false},
		{"zero-metadata", 4096, 512, 0, 64, 0, false},
		{"int-size-overflow", 4096, 512, 8, math.MaxUint64, 0, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			n, err := MetadataLength(tc.length, tc.interval, tc.metadata, tc.size)
			if (err == nil) != tc.valid || n != tc.want {
				t.Fatalf("length=%d error=%v, want %d valid=%v", n, err, tc.want, tc.valid)
			}
		})
	}
}
