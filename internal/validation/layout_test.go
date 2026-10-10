package validation

import (
	"encoding/binary"
	"math"
	"math/big"
	"testing"
)

func fixtureRing() RingParams {
	// The head/tail/flags and CQE offsets are the synthetic ring fixture in
	// internal/uring/ring_index_test.go, with mask/count/array words added.
	return RingParams{
		SQ:         RingOffsets{Head: 0, Tail: 4, Flags: 8, Mask: 12, Entries: 16, Aux: 20, Array: 24},
		CQ:         RingOffsets{Head: 0, Tail: 4, Aux: 8, Mask: 12, Entries: 8, Flags: 0, Array: 16},
		SQGeometry: RingGeometry{4, 4, 3}, CQGeometry: RingGeometry{4, 4, 3},
		SQSize: 40, CQSize: 80, SQESize: 256, SQEStride: 64, CQEStride: 16,
	}
}

type ringCase struct {
	name  string
	p     RingParams
	valid bool
}

func ringCases() []ringCase {
	base := fixtureRing()
	cases := []ringCase{{"fixture", base, true}}
	add := func(name string, valid bool, change func(*RingParams)) {
		p := base
		change(&p)
		cases = append(cases, ringCase{name, p, valid})
	}
	for _, cq := range []bool{false, true} {
		prefix := "SQ/"
		if cq {
			prefix = "CQ/"
		}
		for _, field := range []string{"head", "tail", "mask", "entries", "flags", "aux"} {
			set := func(p *RingParams, off uint64) {
				o := &p.SQ
				if cq {
					o = &p.CQ
				}
				switch field {
				case "head":
					o.Head = off
				case "tail":
					o.Tail = off
				case "mask":
					o.Mask = off
				case "entries":
					o.Entries = off
				case "flags":
					o.Flags = off
				case "aux":
					o.Aux = off
				}
			}
			size := base.SQSize
			if cq {
				size = base.CQSize
			}
			add(prefix+field+"/last-word", true, func(p *RingParams) { set(p, size-4) })
			add(prefix+field+"/past-end", false, func(p *RingParams) { set(p, size) })
			add(prefix+field+"/unaligned", false, func(p *RingParams) { set(p, size-3) })
			add(prefix+field+"/wrapped-offset", false, func(p *RingParams) { set(p, math.MaxUint64-3) })
		}
		add(prefix+"zero-count", false, func(p *RingParams) {
			g := &p.SQGeometry
			if cq {
				g = &p.CQGeometry
			}
			*g = RingGeometry{}
		})
		add(prefix+"non-power-count", false, func(p *RingParams) {
			g := &p.SQGeometry
			if cq {
				g = &p.CQGeometry
			}
			*g = RingGeometry{3, 3, 2}
		})
		add(prefix+"wrong-mask", false, func(p *RingParams) {
			g := &p.SQGeometry
			if cq {
				g = &p.CQGeometry
			}
			g.Mask = 4
		})
		add(prefix+"mapped-count", false, func(p *RingParams) {
			g := &p.SQGeometry
			if cq {
				g = &p.CQGeometry
			}
			g.MappedEntries = 8
		})
		add(prefix+"count-overflow", false, func(p *RingParams) {
			g := &p.SQGeometry
			if cq {
				g = &p.CQGeometry
			}
			*g = RingGeometry{1 << 32, 1 << 32, (1 << 32) - 1}
		})
	}
	add("SQ-array/last-span", true, func(p *RingParams) { p.SQ.Array = 24 })
	add("SQ-array/one-entry-out", false, func(p *RingParams) { p.SQ.Array = 28 })
	add("SQ-array/unaligned", false, func(p *RingParams) { p.SQ.Array = 23 })
	add("CQ-array/last-span", true, func(p *RingParams) { p.CQ.Array = 16 })
	add("CQ-array/one-entry-out", false, func(p *RingParams) { p.CQ.Array = 24 })
	add("CQ-array/unaligned", false, func(p *RingParams) { p.CQ.Array = 15 })
	add("SQEs/one-byte-short", false, func(p *RingParams) { p.SQESize-- })
	add("SQ/one-byte-short", false, func(p *RingParams) { p.SQSize-- })
	add("CQ/one-byte-short", false, func(p *RingParams) { p.CQSize-- })
	add("SQE128", true, func(p *RingParams) { p.SQEStride = 128; p.SQESize = 512 })
	add("SQE128/short", false, func(p *RingParams) { p.SQEStride = 128; p.SQESize = 511 })
	add("CQE32", true, func(p *RingParams) { p.CQEStride = 32; p.CQSize = 144 })
	add("CQE32/short", false, func(p *RingParams) { p.CQEStride = 32; p.CQSize = 143 })
	add("SQE-stride", false, func(p *RingParams) { p.SQEStride = 65 })
	add("CQE-stride", false, func(p *RingParams) { p.CQEStride = 17 })
	add("single-mmap", true, func(p *RingParams) { p.SingleMmap = true; p.SQSize = 80 })
	add("single-mmap/different-lengths", false, func(p *RingParams) { p.SingleMmap = true })
	add("empty-mapping", false, func(p *RingParams) { p.SQSize = 0 })
	add("int-size-overflow", false, func(p *RingParams) { p.SQSize = math.MaxUint64 })
	add("offset-sum-overflow", false, func(p *RingParams) { p.SQ.Array = math.MaxUint64 - 3 })
	add("span-product-overflow", false, func(p *RingParams) { p.SQGeometry = RingGeometry{1 << 63, 1 << 63, (1 << 63) - 1} })
	return cases
}

func TestRingLayoutBounds(t *testing.T) {
	for _, tc := range ringCases() {
		t.Run(tc.name, func(t *testing.T) {
			l, err := ValidateRingLayout(tc.p)
			if (err == nil) != tc.valid {
				t.Fatalf("accepted=%v, want %v: %v", err == nil, tc.valid, err)
			}
			if err == nil {
				assertRingAddresses(t, tc.p, l)
			}
		})
	}
}

// This oracle uses arbitrary precision and enumerates independent source
// offsets, not the validator's arithmetic or its returned region list.
func assertSpan(t *testing.T, size, off, count, stride uint64) {
	t.Helper()
	end := new(big.Int).Mul(new(big.Int).SetUint64(count), new(big.Int).SetUint64(stride))
	end.Add(end, new(big.Int).SetUint64(off))
	if end.Cmp(new(big.Int).SetUint64(size)) > 0 {
		t.Fatalf("accepted address span %d+%d*%d outside %d", off, count, stride, size)
	}
}

func assertRingAddresses(t *testing.T, p RingParams, l Layout) {
	t.Helper()
	for i, o := range []RingOffsets{p.SQ, p.CQ} {
		size := p.SQSize
		if i == 1 {
			size = p.CQSize
		}
		for _, off := range []uint64{o.Head, o.Tail, o.Mask, o.Entries, o.Flags, o.Aux} {
			assertSpan(t, size, off, 1, 4)
			if off%4 != 0 {
				t.Fatal("accepted unaligned word")
			}
		}
	}
	if p.SQGeometry.Entries == 0 || p.SQGeometry.Entries&(p.SQGeometry.Entries-1) != 0 || p.SQGeometry.Mask != p.SQGeometry.Entries-1 ||
		p.CQGeometry.Entries == 0 || p.CQGeometry.Entries&(p.CQGeometry.Entries-1) != 0 || p.CQGeometry.Mask != p.CQGeometry.Entries-1 {
		t.Fatal("accepted invalid count/mask")
	}
	assertSpan(t, p.SQSize, p.SQ.Array, p.SQGeometry.Entries, 4)
	assertSpan(t, p.CQSize, p.CQ.Array, p.CQGeometry.Entries, p.CQEStride)
	assertSpan(t, p.SQESize, 0, p.SQGeometry.Entries, p.SQEStride)
	// Maximum masked index, including the uint32 wrap edge, fits each array.
	for _, index := range []uint64{0, math.MaxUint32 - 1, math.MaxUint32} {
		assertSpan(t, p.SQESize, (index&p.SQGeometry.Mask)*p.SQEStride, 1, p.SQEStride)
		assertSpan(t, p.CQSize, p.CQ.Array+(index&p.CQGeometry.Mask)*p.CQEStride, 1, p.CQEStride)
	}
	for i, size := range []uint64{p.SQSize, p.CQSize, p.SQESize} {
		if l.Sizes[i] <= 0 || uint64(l.Sizes[i]) != size {
			t.Fatal("narrowed size changed")
		}
	}
}

func TestMappingArithmeticBounds(t *testing.T) {
	for _, tc := range []struct {
		name  string
		size  uint64
		r     Region
		valid bool
	}{
		{"exact-end", 64, Region{"data", 32, 4, 8, 8}, true},
		{"past-end", 63, Region{"data", 32, 4, 8, 8}, false},
		{"product-overflow", 64, Region{"data", 0, 1 << 63, 8, 8}, false},
		{"sum-overflow", 64, Region{"data", math.MaxUint64 - 7, 2, 8, 8}, false},
		{"zero-count", 64, Region{"data", 0, 0, 8, 8}, false},
		{"zero-stride", 64, Region{"data", 0, 1, 0, 8}, false},
		{"stride-alignment", 64, Region{"data", 0, 2, 12, 8}, false},
		{"zero-alignment", 64, Region{"data", 0, 1, 8, 0}, false},
		{"odd-alignment", 64, Region{"data", 0, 1, 8, 3}, false},
		{"size-overflow", math.MaxUint64, Region{"data", 0, 1, 8, 8}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := ValidateLayout([]Mapping{{tc.size, []Region{tc.r}}})
			if (err == nil) != tc.valid {
				t.Fatalf("accepted=%v want %v", err == nil, tc.valid)
			}
		})
	}
}

func TestRingSizesBounds(t *testing.T) {
	for _, single := range []bool{false, true} {
		p := fixtureRing()
		p.SingleMmap = single
		sq, cq, sqe, err := RingSizes(p)
		wantSQ := uint64(40)
		if single {
			wantSQ = 80
		}
		if err != nil || sq != wantSQ || cq != 80 || sqe != 256 {
			t.Fatalf("sizes=%d/%d/%d: %v", sq, cq, sqe, err)
		}
	}
	for _, p := range []RingParams{
		{SQ: RingOffsets{Array: math.MaxUint64}, SQGeometry: RingGeometry{Entries: 1}},
		{SQGeometry: RingGeometry{Entries: math.MaxUint64}, SQEStride: 128, CQEStride: 16},
		{SQGeometry: RingGeometry{Entries: 1}, CQ: RingOffsets{Array: math.MaxUint64}, CQGeometry: RingGeometry{Entries: 1}, SQEStride: 64, CQEStride: 32},
	} {
		if _, _, _, err := RingSizes(p); err == nil {
			t.Fatal("accepted size arithmetic overflow")
		}
	}
}

func TestDescriptorLayoutBounds(t *testing.T) {
	for _, depth := range []uint64{1, 2, 3, 31, 32, 127, 128, 4096} {
		for _, desc := range []uint64{24, 32, 256} {
			p := DescriptorParams{1, 2, depth, desc, 4096}
			l, err := ValidateDescriptorLayout(p)
			if err != nil || l.Offset != int64(4096*desc) || uint64(l.Size) < depth*desc || l.Size%4096 != 0 {
				t.Fatalf("%+v -> %+v: %v", p, l, err)
			}
		}
	}
	base := DescriptorParams{1, 2, 128, 24, 4096}
	for _, tc := range []struct {
		name   string
		change func(*DescriptorParams)
	}{
		{"queue-end", func(p *DescriptorParams) { p.QueueID = 2 }},
		{"no-queues", func(p *DescriptorParams) { p.Queues = 0 }},
		{"queue-count-overflow", func(p *DescriptorParams) { p.Queues = 65536 }},
		{"zero-depth", func(p *DescriptorParams) { p.Depth = 0 }},
		{"depth-end", func(p *DescriptorParams) { p.Depth = 4097 }},
		{"short-desc", func(p *DescriptorParams) { p.DescSize = 16 }},
		{"unaligned-desc", func(p *DescriptorParams) { p.DescSize = 25 }},
		{"large-desc", func(p *DescriptorParams) { p.DescSize = 264 }},
		{"zero-page", func(p *DescriptorParams) { p.PageSize = 0 }},
		{"non-power-page", func(p *DescriptorParams) { p.PageSize = 4095 }},
		{"page-overflow", func(p *DescriptorParams) { p.PageSize = 1 << 63 }},
		{"offset-overflow", func(p *DescriptorParams) { p.PageSize = 1 << 62; p.QueueID = 3; p.Queues = 4 }},
		{"mapping-end-overflow", func(p *DescriptorParams) { p.PageSize = 1 << 62 }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := base
			tc.change(&p)
			if _, err := ValidateDescriptorLayout(p); err == nil {
				t.Fatal("accepted invalid descriptor mapping")
			}
		})
	}
}

// Fixed-length encoding permits arbitrary 64-bit boundary values without any
// allocations whose size comes from the fuzz input.
func encodeRing(p RingParams) []byte {
	words := []uint64{p.SQ.Head, p.SQ.Tail, p.SQ.Mask, p.SQ.Entries, p.SQ.Flags, p.SQ.Aux, p.SQ.Array,
		p.CQ.Head, p.CQ.Tail, p.CQ.Mask, p.CQ.Entries, p.CQ.Flags, p.CQ.Aux, p.CQ.Array,
		p.SQGeometry.Entries, p.SQGeometry.MappedEntries, p.SQGeometry.Mask,
		p.CQGeometry.Entries, p.CQGeometry.MappedEntries, p.CQGeometry.Mask,
		p.SQSize, p.CQSize, p.SQESize, p.SQEStride, p.CQEStride, 0}
	if p.SingleMmap {
		words[25] = 1
	}
	b := make([]byte, len(words)*8)
	for i, v := range words {
		binary.LittleEndian.PutUint64(b[i*8:], v)
	}
	return b
}

func FuzzRingLayout(f *testing.F) {
	for _, tc := range ringCases() {
		f.Add(encodeRing(tc.p))
	}
	for _, d := range []DescriptorParams{
		{1, 2, 128, 24, 4096}, {1, 2, 128, 32, 4096}, {0, 1, 4096, 256, 4096},
		{2, 2, 128, 24, 4096}, {0, 1, 0, 24, 4096}, {0, 1, 4097, 24, 4096},
		{0, 1, 128, 23, 4096}, {0, 1, 128, 25, 4096}, {0, 1, 128, 264, 4096},
		{1, 2, 128, 24, 1 << 62}, {3, 4, 128, 24, 1 << 62},
	} {
		b := encodeRing(fixtureRing())
		for i, v := range []uint64{d.QueueID, d.Queues, d.Depth, d.DescSize, d.PageSize} {
			binary.LittleEndian.PutUint64(b[i*8:], v)
		}
		f.Add(b)
	}
	f.Fuzz(func(t *testing.T, b []byte) {
		if len(b) != 26*8 {
			return
		}
		var w [26]uint64
		for i := range w {
			w[i] = binary.LittleEndian.Uint64(b[i*8:])
		}
		p := RingParams{SQ: RingOffsets{w[0], w[1], w[2], w[3], w[4], w[5], w[6]}, CQ: RingOffsets{w[7], w[8], w[9], w[10], w[11], w[12], w[13]},
			SQGeometry: RingGeometry{w[14], w[15], w[16]}, CQGeometry: RingGeometry{w[17], w[18], w[19]},
			SQSize: w[20], CQSize: w[21], SQESize: w[22], SQEStride: w[23], CQEStride: w[24], SingleMmap: w[25]&1 != 0}
		l, err := ValidateRingLayout(p)
		if err == nil {
			assertRingAddresses(t, p, l)
		}
		// Exercise the same generic gate for descriptor mappings too.
		d := DescriptorParams{w[0], w[1], w[2], w[3], w[4]}
		if l, err := ValidateDescriptorLayout(d); err == nil {
			assertSpan(t, uint64(l.Size), 0, d.Depth, d.DescSize)
			end := new(big.Int).Add(big.NewInt(l.Offset), big.NewInt(int64(l.Size)))
			if l.Offset < 0 || end.Cmp(big.NewInt(math.MaxInt64)) > 0 {
				t.Fatal("accepted descriptor offset/end overflow")
			}
		}
	})
}
