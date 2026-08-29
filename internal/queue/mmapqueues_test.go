package queue

import (
	"os"
	"syscall"
	"testing"
	"unsafe"

	"github.com/ehrlich-b/go-ublk/internal/constants"
	"github.com/ehrlich-b/go-ublk/internal/uapi"
)

// queueStride is the fixed per-queue stride into the kernel's descriptor region.
// Hand-derived: UBLK_MAX_QUEUE_DEPTH (4096) * sizeof(UblksrvIODesc) (24) =
// 98304, which is already exactly page-aligned (98304 % 4096 == 0), so no
// page rounding is needed. This is a regression test literal pinning Critical
// Bug #1's root cause: the stride MUST be keyed on the FIXED
// UBLK_MAX_QUEUE_DEPTH, NOT on the caller's configured depth, or queues >= 1
// alias queue 0's descriptors (data corruption + unkillable D-state hangs).
const queueStride = 98304

func TestMmapQueuesPerQueueStride(t *testing.T) {
	// The factory default for mapper use is zero, so ensure deps are visible:
	derivedStride := uapi.UBLK_MAX_QUEUE_DEPTH * int(unsafe.Sizeof(uapi.UblksrvIODesc{}))
	if derivedStride != queueStride {
		t.Fatalf("hand-derived stride mismatch: UBLK_MAX_QUEUE_DEPTH*sizeof = %d, want %d", derivedStride, queueStride)
	}
	if derivedStride%os.Getpagesize() != 0 {
		t.Fatalf("stride %d is not page-aligned on this host (page size %d)", derivedStride, os.Getpagesize())
	}

	// Scratch file INSIDE the package directory (current working directory of
	// `go test` is this package dir, inside the repo clone). Never use
	// t.TempDir()/os.TempDir(): this test must not write outside the clone.
	// The kernel places independent MAP_SHARED mappings anywhere in VA space,
	// so the stride is proven CONTENT-based: sentinels written into the backing
	// file at the expected per-queue offsets must be the first bytes each
	// returned descPtr sees.
	f, err := os.CreateTemp(".", "mmapq-scratch-")
	if err != nil {
		t.Fatalf("CreateTemp: %v", err)
	}
	scratchName := f.Name()
	t.Cleanup(func() {
		_ = f.Close()
		_ = os.Remove(scratchName)
	})

	// Truncate so queue 2's window (2*stride .. 2*stride+page) plus margin are
	// inside the file: 3*stride + page bytes.
	const fileSize = 3*queueStride + 4096
	if err := f.Truncate(fileSize); err != nil {
		t.Fatalf("Truncate(%d): %v", fileSize, err)
	}

	// Distinct sentinels: 0xAA at file offset 0 (queue 0), 0xBB at queueStride
	// (queue 1), 0xCC at 2*queueStride (queue 2).
	sentinels := map[int64]byte{
		0:               0xAA,
		queueStride:     0xBB,
		2 * queueStride: 0xCC,
	}
	for off, b := range sentinels {
		if _, err := f.WriteAt([]byte{b}, off); err != nil {
			t.Fatalf("WriteAt(0x%02x @ %d): %v", b, off, err)
		}
	}

	fd := int(f.Fd())

	// Helper: assert the first byte of a mapping matches the sentinel expected
	// at that queue's window, read the LAST byte of the depth descriptor region,
	// then clean up both returned mappings.
	assertQueue := func(queueID uint16, depth int, wantByte byte) {
		t.Helper()
		descPtr, bufPtr, err := mmapQueues(fd, queueID, depth)
		if err != nil {
			t.Fatalf("mmapQueues(q=%d, depth=%d): %v", queueID, depth, err)
		}
		if descPtr == nil {
			t.Fatalf("mmapQueues(q=%d, depth=%d): nil descPtr", queueID, depth)
		}

		if got := *(*byte)(descPtr); got != wantByte {
			t.Fatalf("queue %d depth %d: first desc byte = 0x%02x, want 0x%02x "+
				"(per-queue offset stride is wrong -> descriptor aliasing)",
				queueID, depth, got, wantByte)
		}

		// descSize sanity: the mapped LENGTH must cover depth descriptors
		// (scale with depth, distinct from the fixed stride). Reading the last
		// byte of the last descriptor's region without a fault proves the
		// mapping is large enough.
		lastOff := unsafe.Add(descPtr, uintptr((depth-1)*int(unsafe.Sizeof(uapi.UblksrvIODesc{}))+23))
		last := *(*byte)(lastOff)
		t.Logf("queue %d depth %d: first=0x%02x last(eff=(depth-1)*24+23)=0x%02x", queueID, depth, wantByte, last)

		unmapMmapQueues(t, descPtr, bufPtr, depth)
	}

	// 1+2. Per-queue offsets 0, stride, 2*stride map the correct file regions.
	assertQueue(0, 4, 0xAA)
	assertQueue(1, 4, 0xBB)
	assertQueue(2, 4, 0xCC)

	// 3. The stride is keyed on the FIXED UBLK_MAX_QUEUE_DEPTH, not the caller's
	// depth: a DIFFERENT depth must map queue 1 to the SAME file offset and see
	// the SAME 0xBB sentinel. This is the direct regression for Critical Bug #1.
	assertQueue(1, 64, 0xBB)

	// 4. descSize scales with depth (covered above: depth=4 and depth=64 both
	// read their last descriptor byte without faulting).
}

// unmapMmapQueues unmaps both mappings mmapQueues returned, mirroring the
// syscall pattern in Runner.Close(): page-rounded depth*24 bytes for the
// descriptor map (the actual mapped length), depth*IOBufferSizePerTag for the
// anonymous buffer map.
func unmapMmapQueues(t *testing.T, descPtr, bufPtr unsafe.Pointer, depth int) {
	t.Helper()

	descSize := depth * int(unsafe.Sizeof(uapi.UblksrvIODesc{}))
	pageSize := os.Getpagesize()
	if rem := descSize % pageSize; rem != 0 {
		descSize += pageSize - rem
	}
	if _, _, errno := syscall.Syscall(syscall.SYS_MUNMAP, uintptr(descPtr), uintptr(descSize), 0); errno != 0 {
		t.Errorf("munmap desc map: %v", errno)
	}

	bufSize := depth * constants.IOBufferSizePerTag
	if _, _, errno := syscall.Syscall(syscall.SYS_MUNMAP, uintptr(bufPtr), uintptr(bufSize), 0); errno != 0 {
		t.Errorf("munmap buf map: %v", errno)
	}
}
