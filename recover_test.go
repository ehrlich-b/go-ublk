package ublk

import (
	"fmt"
	"strings"
	"syscall"
	"testing"
)

// TestExplainControlError: the io_uring-disabled hint must fire for EPERM from
// io_uring_setup even though syscall.EPERM also matches os.ErrPermission.
func TestExplainControlError(t *testing.T) {
	for _, c := range []struct {
		err  error
		want string
	}{
		{fmt.Errorf("failed to create io_uring: io_uring_setup entries=4 flags=0xc00: %w", syscall.EPERM), "io_uring_disabled"},
		{fmt.Errorf("failed to open /dev/ublk-control: %w", syscall.ENOENT), "modprobe ublk_drv"},
		{fmt.Errorf("failed to open /dev/ublk-control: %w", syscall.EACCES), "CAP_SYS_ADMIN"},
	} {
		got := explainControlError(c.err)
		if !strings.Contains(got.Error(), c.want) {
			t.Errorf("explain(%v) = %v, want a hint containing %q", c.err, got, c.want)
		}
		if !strings.Contains(got.Error(), c.err.Error()) {
			t.Errorf("explain dropped the original error: %v", got)
		}
	}
}
