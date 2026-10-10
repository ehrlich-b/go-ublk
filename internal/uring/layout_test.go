package uring

import (
	"strings"
	"testing"
)

// Invalid setup geometry must fail at the pure gate, before mmap can even
// consult this invalid fd, and before any ring pointer is installed.
func TestRingLayoutRejectedBeforeMapping(t *testing.T) {
	for _, tc := range []struct {
		name string
		p    ioUringParams
	}{
		{"non-power-count", ioUringParams{sqEntries: 3, cqEntries: 4}},
		{"head-outside", ioUringParams{sqEntries: 4, cqEntries: 4,
			sqOff: sqringOffsets{head: 1000, array: 24}, cqOff: cqringOffsets{cqes: 16}}},
		{"tail-unaligned", ioUringParams{sqEntries: 4, cqEntries: 4,
			sqOff: sqringOffsets{tail: 3, array: 24}, cqOff: cqringOffsets{cqes: 16}}},
		{"CQEs-unaligned", ioUringParams{sqEntries: 4, cqEntries: 4,
			sqOff: sqringOffsets{array: 24}, cqOff: cqringOffsets{cqes: 17}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := &IoUring{fd: -1}
			err := r.mapRings(&tc.p)
			if err == nil || !strings.HasPrefix(err.Error(), "ring layout:") {
				t.Fatalf("expected geometry rejection before mmap, got %v", err)
			}
			if r.sqRing != nil || r.cqRing != nil || r.sqeRing != nil || r.sqHead != nil || r.cqHead != nil || r.sqes != nil {
				t.Fatal("invalid geometry installed mappings or pointers")
			}
		})
	}
}
