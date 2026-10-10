package queue

import (
	"github.com/ehrlich-b/go-ublk/internal/interfaces"
	"github.com/ehrlich-b/go-ublk/internal/uapi"
	"github.com/ehrlich-b/go-ublk/internal/uring"
)

// DispatchMode selects how a queue invokes handlers when Inline is false.
type DispatchMode uint8

const (
	// DispatchGoroutine launches a goroutine for each request (the default).
	DispatchGoroutine DispatchMode = iota
	// DispatchPool reuses one handler goroutine per tag, with a queue-depth
	// buffered handoff. Each engine owns the workers for its tag range, so a
	// multi-engine queue still has exactly QueueDepth workers in total.
	DispatchPool
	// DispatchAuto runs declared nonblocking handlers inline and all others
	// on a goroutine per request. User-copy engines remain asynchronous.
	DispatchAuto
	// DispatchAdaptive additionally requires a singleton ready batch and no
	// outstanding handler before inlining a declared nonblocking handler.
	DispatchAdaptive
)

func (e *engine) startDispatch() {
	if !e.cfg.inline && !e.cfg.userCopy && e.cfg.zeroCopy == nil &&
		(e.cfg.dispatch == DispatchAuto || e.cfg.dispatch == DispatchAdaptive) {
		declaration, ok := e.cfg.handler.(interfaces.NonBlockingDeclarer)
		e.autoInline = ok && declaration.NonBlocking()
	}
	if e.autoInline && e.cfg.dispatch == DispatchAdaptive {
		// Match setup's ring capacity and bound a snapshot even if the kernel
		// keeps posting CQEs while it is collected. Reuse storage every round.
		e.cqeBatch = make([]uring.CQE, 0, 2*len(e.reqs)+4)
	}
	if e.cfg.inline || e.cfg.zeroCopy != nil || e.cfg.dispatch != DispatchPool {
		return
	}
	e.work = make(chan *Request, len(e.reqs))
	for range e.reqs {
		go e.handleRequests()
	}
}

func (e *engine) dispatchInline() bool {
	if e.cfg.inline {
		return true
	}
	if !e.autoInline {
		return false
	}
	return e.cfg.dispatch == DispatchAuto || (!e.crowded && e.handlers.Load() == 0)
}

func (e *engine) drainCQEs() {
	if !e.cfg.inline && e.autoInline && e.cfg.dispatch == DispatchAdaptive {
		e.drainAdaptiveCQEs()
		return
	}
	for cqe := e.ring.PeekCQE(); cqe != nil; cqe = e.ring.PeekCQE() {
		ud, result, flags := cqe.UserData, cqe.Res, cqe.Flags
		e.ring.CQAdvance(1)
		e.handleCQE(ud, result, flags)
	}
}

func (e *engine) drainAdaptiveCQEs() {
	e.cqeBatch = e.cqeBatch[:0]
	// Peek first so io_uring can pull pending task work or overflow CQEs into
	// the ring. Then freeze the count: later arrivals belong to the next turn.
	if e.ring.PeekCQE() == nil {
		return
	}
	count := e.ring.CQReady()
	limit := min(int(count), cap(e.cqeBatch))
	ready := 0
	for len(e.cqeBatch) < limit {
		cqe := e.ring.PeekCQE()
		if cqe == nil {
			break
		}
		entry := *cqe
		e.cqeBatch = append(e.cqeBatch, entry)
		e.ring.CQAdvance(1)
		switch entry.UserData & kindMask {
		case kindIO:
			if entry.Res == uapi.UBLK_IO_RES_OK {
				ready++
			}
		case kindFetch:
			if entry.Res > 0 && entry.Flags&uring.IORING_CQE_F_BUFFER != 0 {
				ready += int(entry.Res) / 2 // FETCH tag lists contain u16 tags.
			}
		}
	}
	// A backlog larger than the snapshot may hide another request. Be
	// conservative even if the captured prefix contains only control CQEs.
	e.crowded = ready > 1 || int(count) > cap(e.cqeBatch)
	for _, cqe := range e.cqeBatch {
		e.handleCQE(cqe.UserData, cqe.Res, cqe.Flags)
	}
	e.crowded = false
}

func (e *engine) handleRequests() {
	for r := range e.work {
		e.call(r)
	}
}

func (e *engine) stopDispatch() {
	if e.work != nil && !e.workClosed {
		close(e.work)
		e.workClosed = true
		// Completion releases the request, even if the callback is still
		// unwinding. As with goroutine dispatch, teardown does not wait for
		// callback code after Complete; workers exit when that code returns.
	}
}
