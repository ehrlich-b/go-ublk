package queue

// DispatchMode selects how a queue invokes handlers when Inline is false.
type DispatchMode uint8

const (
	// DispatchGoroutine launches a goroutine for each request (the default).
	DispatchGoroutine DispatchMode = iota
	// DispatchPool reuses one handler goroutine per tag, with a queue-depth
	// buffered handoff. Each engine owns the workers for its tag range, so a
	// multi-engine queue still has exactly QueueDepth workers in total.
	DispatchPool
)

func (e *engine) startDispatch() {
	if e.cfg.inline || e.cfg.zeroCopy != nil || e.cfg.dispatch != DispatchPool {
		return
	}
	e.work = make(chan *Request, len(e.reqs))
	for range e.reqs {
		go e.handleRequests()
	}
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
