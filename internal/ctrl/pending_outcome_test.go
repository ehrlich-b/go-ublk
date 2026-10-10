package ctrl

import (
	"context"
	"errors"
	"syscall"
	"testing"
	"time"

	"github.com/ehrlich-b/go-ublk/internal/uapi"
	"github.com/ehrlich-b/go-ublk/internal/uring"
)

func TestPendingControlReportsFinalCQEAndRetainedStorage(t *testing.T) {
	for _, tc := range []struct {
		name      string
		res       uring.Result
		transport error
		reaped    bool
	}{
		{"late success", controlTestResult(0), nil, true},
		{"reaped cancellation", controlTestResult(-int32(syscall.EINTR)), errors.Join(uring.ErrCtrlCanceled, context.DeadlineExceeded), true},
		{"unreaped wait failure", nil, uring.ErrCtrlTimeout, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			release := make(chan struct{})
			ring := &controlTestRing{submit: func(op uint32, _ *uapi.UblksrvCtrlCmd) (uring.Result, error) {
				if op == uapi.UBLK_U_CMD_GET_DEV_INFO {
					return controlTestResult(0), nil
				}
				<-release
				return tc.res, tc.transport
			}}
			c, slots := newTestControllerSlots(t, ring)
			ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
			defer cancel()
			err := c.StopDev(ctx, 7)
			var pending *InFlightError
			if !errors.As(err, &pending) {
				close(release)
				t.Fatalf("STOP timeout did not retain its receipt: %v", err)
			}
			if pending.Reaped() {
				t.Fatal("uncompleted STOP reported a reaped result")
			}
			if err := c.Close(); err != nil {
				t.Fatal(err)
			}
			if created, unmapped := slots.counts(); created != 1 || unmapped != 0 || ring.closeCount() != 0 {
				t.Fatalf("storage freed while STOP still pending: slots=%d unmaps=%d ring closes=%d", created, unmapped, ring.closeCount())
			}
			close(release)
			select {
			case <-pending.Done():
			case <-time.After(time.Second):
				t.Fatal("late outcome was not published")
			}
			if pending.Reaped() != tc.reaped {
				t.Fatalf("Reaped=%v; want %v", pending.Reaped(), tc.reaped)
			}
			wantUnmaps := 0
			if tc.reaped {
				wantUnmaps = 1
			}
			if _, unmapped := slots.counts(); unmapped != wantUnmaps || ring.closeCount() != 1 {
				t.Fatalf("final storage ownership: unmaps=%d ring closes=%d; want unmaps=%d", unmapped, ring.closeCount(), wantUnmaps)
			}
			if tc.name == "reaped cancellation" && !errors.Is(pending.Result(), syscall.EINTR) {
				t.Fatalf("lost final cancellation outcome: %v", pending.Result())
			}
		})
	}
}
