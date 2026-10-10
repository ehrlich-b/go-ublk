package queue

import (
	"os"
	"syscall"
	"testing"
)

func TestErrnoDirectValuesDoNotAllocate(t *testing.T) {
	for _, tc := range []struct {
		name string
		err  error
		want int32
	}{
		{"success", nil, 0}, {"errno", syscall.ENOSPC, int32(syscall.ENOSPC)},
		{"zero", syscall.Errno(0), int32(syscall.EIO)},
		{"largest", syscall.Errno(4095), 4095},
		{"out-of-range", syscall.Errno(4096), int32(syscall.EIO)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			allocs := testing.AllocsPerRun(1000, func() {
				if got := Errno(tc.err); got != tc.want {
					panic("incorrect errno conversion")
				}
			})
			if allocs != 0 {
				t.Fatalf("errno conversion allocates %.2f objects, want zero", allocs)
			}
		})
	}
	wrapped := &os.PathError{Op: "write", Err: syscall.ENOSPC}
	if got := Errno(wrapped); got != int32(syscall.ENOSPC) {
		t.Fatalf("wrapped errno=%d, want %d", got, syscall.ENOSPC)
	}
}
