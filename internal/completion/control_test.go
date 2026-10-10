package completion

import (
	"context"
	"errors"
	"syscall"
	"testing"
)

type controlReply struct {
	done   chan struct{}
	err    error
	reaped bool
}

func (p *controlReply) Error() string         { return "pending STOP" }
func (p *controlReply) Done() <-chan struct{} { return p.done }
func (p *controlReply) Result() error         { return p.err }
func (p *controlReply) Reaped() bool          { return p.reaped }

func TestControlCompletionRequiresFinalKernelResult(t *testing.T) {
	for _, tc := range []struct {
		name   string
		reaped bool
		result error
	}{
		{"late success", true, nil},
		{"reaped cancellation", true, errors.Join(syscall.EINTR, context.DeadlineExceeded)},
		{"unreaped transport failure", false, errors.New("wait failed with kernel command still pending")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			// A premature reaped flag cannot substitute for publication of Done.
			p := &controlReply{done: make(chan struct{}), reaped: true}
			if resolved, err := ReconcileControl(p); resolved || err != p {
				t.Fatal("timeout was treated as cancellation")
			}
			p.reaped, p.err = tc.reaped, tc.result
			close(p.done)
			resolved, err := ReconcileControl(p)
			if resolved != tc.reaped {
				t.Fatalf("resolved=%v for reaped=%v", resolved, tc.reaped)
			}
			if tc.reaped && err != tc.result || !tc.reaped && err != p {
				t.Fatalf("wrong final outcome: %v", err)
			}
		})
	}
}
