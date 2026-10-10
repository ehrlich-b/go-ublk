// Package validation checks kernel-derived geometry without mapping memory or
// forming pointers. It builds on both Linux and Darwin for native fuzzing.
package validation

import (
	"fmt"
	"math"
)

// Region describes count objects with a fixed stride in one mapping.
type Region struct {
	Name                         string
	Offset, Count, Stride, Align uint64
}

// Mapping describes the actual length of a mapping, not its page-rounded
// virtual address reservation. Regions may share memory (e.g. a buffer-ring
// tail overlays the first entry).
type Mapping struct {
	Size    uint64
	Regions []Region
}

// RingGeometry includes the setup count and the count/mask read from the
// mapping. Before mapping, callers supply the expected count and mask; after
// mapping they read those words through checked byte slices and validate again.
type RingGeometry struct {
	Entries, MappedEntries, Mask uint64
}

// Layout contains lengths safe to narrow to int and the checked regions.
type Layout struct {
	Sizes   []int
	Regions [][]Region
}

// ValidateLayout is the shared geometry gate for SQ/CQ/SQE, provided-buffer
// rings, descriptors and the queue's owned buffers. No arithmetic is narrowed
// or used to form a pointer until it succeeds.
func ValidateLayout(mappings []Mapping, rings ...RingGeometry) (Layout, error) {
	for _, r := range rings {
		if r.Entries == 0 || r.Entries&(r.Entries-1) != 0 || r.Entries > math.MaxUint32 {
			return Layout{}, fmt.Errorf("ring entries %d must be a nonzero uint32 power of two", r.Entries)
		}
		if r.MappedEntries != r.Entries || r.Mask != r.Entries-1 {
			return Layout{}, fmt.Errorf("ring count/mask %d/%#x do not match %d entries", r.MappedEntries, r.Mask, r.Entries)
		}
	}
	l := Layout{Sizes: make([]int, len(mappings)), Regions: make([][]Region, len(mappings))}
	for i, m := range mappings {
		if m.Size == 0 || m.Size > uint64(^uint(0)>>1) {
			return Layout{}, fmt.Errorf("mapping size %d is outside positive int range", m.Size)
		}
		for _, r := range m.Regions {
			if r.Count == 0 || r.Stride == 0 || r.Align == 0 || r.Align&(r.Align-1) != 0 ||
				r.Offset%r.Align != 0 || r.Stride%r.Align != 0 {
				return Layout{}, fmt.Errorf("%s: invalid count, stride or alignment", r.Name)
			}
			if r.Count > math.MaxUint64/r.Stride {
				return Layout{}, fmt.Errorf("%s: count times stride overflows", r.Name)
			}
			n := r.Count * r.Stride
			if r.Offset > m.Size || n > m.Size-r.Offset {
				return Layout{}, fmt.Errorf("%s: offset %d plus size %d exceeds mapping %d", r.Name, r.Offset, n, m.Size)
			}
		}
		l.Sizes[i] = int(m.Size)
		l.Regions[i] = append([]Region(nil), m.Regions...)
	}
	return l, nil
}

// BufferSize checks owned allocations before mmap and before narrowing to int.
func BufferSize(count, stride, align uint64) (int, error) {
	if stride == 0 || count > math.MaxUint64/stride {
		return 0, fmt.Errorf("buffer size overflows")
	}
	l, err := ValidateLayout([]Mapping{{count * stride, []Region{{"buffer", 0, count, stride, align}}}})
	if err != nil {
		return 0, err
	}
	return l.Sizes[0], nil
}

// RingOffsets are all the words and entry array returned by io_uring_setup.
// Aux is SQ dropped or CQ overflow; Flags is also checked on old kernels where
// CQ flags is zero (an unused offset aliasing the head).
type RingOffsets struct {
	Head, Tail, Mask, Entries, Flags, Aux, Array uint64
}

type RingParams struct {
	SQ, CQ                  RingOffsets
	SQGeometry, CQGeometry  RingGeometry
	SQSize, CQSize, SQESize uint64
	SQEStride, CQEStride    uint64
	SingleMmap              bool
}

// RingSizes computes mmap lengths with checked arithmetic before mmap itself.
func RingSizes(p RingParams) (sq, cq, sqe uint64, err error) {
	end := func(off, count, stride uint64) (uint64, error) {
		if stride == 0 || count > (math.MaxUint64-off)/stride {
			return 0, fmt.Errorf("ring offset plus entry span overflows")
		}
		return off + count*stride, nil
	}
	if sq, err = end(p.SQ.Array, p.SQGeometry.Entries, 4); err != nil {
		return
	}
	if cq, err = end(p.CQ.Array, p.CQGeometry.Entries, p.CQEStride); err != nil {
		return
	}
	if sqe, err = end(0, p.SQGeometry.Entries, p.SQEStride); err != nil {
		return
	}
	if p.SingleMmap {
		sq = max(sq, cq)
		cq = sq
	}
	return
}

func ringRegions(o RingOffsets, count, stride, align uint64) []Region {
	return []Region{
		{"head", o.Head, 1, 4, 4}, {"tail", o.Tail, 1, 4, 4},
		{"mask", o.Mask, 1, 4, 4}, {"entries", o.Entries, 1, 4, 4},
		{"flags", o.Flags, 1, 4, 4}, {"aux", o.Aux, 1, 4, 4},
		{"array", o.Array, count, stride, align},
	}
}

// ValidateRingLayout checks both ordinary and SQE128/CQE32 layouts. The strides
// come from accepted setup flags, never from mutable mapping words.
func ValidateRingLayout(p RingParams) (Layout, error) {
	if (p.SQEStride != 64 && p.SQEStride != 128) || (p.CQEStride != 16 && p.CQEStride != 32) {
		return Layout{}, fmt.Errorf("unsupported SQE/CQE stride %d/%d", p.SQEStride, p.CQEStride)
	}
	if p.SingleMmap && p.SQSize != p.CQSize {
		return Layout{}, fmt.Errorf("single mmap ring sizes differ")
	}
	return ValidateLayout([]Mapping{
		{p.SQSize, ringRegions(p.SQ, p.SQGeometry.Entries, 4, 4)},
		{p.CQSize, ringRegions(p.CQ, p.CQGeometry.Entries, p.CQEStride, 8)},
		{p.SQESize, []Region{{"SQEs", 0, p.SQGeometry.Entries, p.SQEStride, 8}}},
	}, p.SQGeometry, p.CQGeometry)
}

type DescriptorParams struct {
	QueueID, Queues, Depth, DescSize, PageSize uint64
}

type DescriptorLayout struct {
	Offset int64
	Size   int
	Stride uint64
}

// ValidateDescriptorLayout uses the fixed 4096-tag queue stride, independently
// of the accepted depth, and validates the atomic 64-bit descriptor fields.
func ValidateDescriptorLayout(p DescriptorParams) (DescriptorLayout, error) {
	if p.Queues == 0 || p.Queues > 65535 || p.QueueID >= p.Queues || p.Depth == 0 || p.Depth > 4096 {
		return DescriptorLayout{}, fmt.Errorf("invalid descriptor queue/count/depth")
	}
	if p.DescSize < 24 || p.DescSize > 256 || p.DescSize%8 != 0 {
		return DescriptorLayout{}, fmt.Errorf("descriptor stride %d must be an 8-byte multiple in 24..256", p.DescSize)
	}
	if p.PageSize == 0 || p.PageSize&(p.PageSize-1) != 0 || p.PageSize > uint64(^uint(0)>>1) {
		return DescriptorLayout{}, fmt.Errorf("invalid page size %d", p.PageSize)
	}
	round := func(n uint64) (uint64, error) {
		if n > math.MaxUint64-(p.PageSize-1) {
			return 0, fmt.Errorf("page rounding overflows")
		}
		return (n + p.PageSize - 1) &^ (p.PageSize - 1), nil
	}
	stride, err := round(4096 * p.DescSize)
	if err != nil {
		return DescriptorLayout{}, err
	}
	size, err := round(p.Depth * p.DescSize)
	if err != nil {
		return DescriptorLayout{}, err
	}
	if p.QueueID > uint64(math.MaxInt64)/stride || stride > uint64(math.MaxInt64) {
		return DescriptorLayout{}, fmt.Errorf("descriptor mmap offset overflows int64")
	}
	offset := p.QueueID * stride
	if size > uint64(math.MaxInt64)-offset {
		return DescriptorLayout{}, fmt.Errorf("descriptor mapping end overflows int64")
	}
	l, err := ValidateLayout([]Mapping{{size, []Region{{"descriptors", 0, p.Depth, p.DescSize, 8}}}})
	if err != nil {
		return DescriptorLayout{}, err
	}
	return DescriptorLayout{int64(offset), l.Sizes[0], p.DescSize}, nil
}
