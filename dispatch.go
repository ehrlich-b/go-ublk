package ublk

import (
	"github.com/ehrlich-b/go-ublk/internal/interfaces"
	"github.com/ehrlich-b/go-ublk/internal/queue"
)

// DispatchMode selects handler scheduling when DeviceParams.Inline is false.
type DispatchMode = queue.DispatchMode

// NonBlockingDeclarer is an optional Backend or Handler declaration permitting
// automatic inline dispatch. True promises prompt return for every operation,
// without waiting for external I/O or request completion. Asynchronous completion
// is allowed. The declaration is sampled once per engine; false or absence keeps
// goroutine dispatch. An incorrect true declaration can stall a queue indefinitely.
type NonBlockingDeclarer = interfaces.NonBlockingDeclarer

const (
	// DispatchGoroutine launches a goroutine per request and is the default.
	DispatchGoroutine = queue.DispatchGoroutine
	// DispatchPool reuses QueueDepth handler goroutines per queue. Workers
	// share a buffered handoff without allocating a job for each request.
	DispatchPool = queue.DispatchPool
	// DispatchAuto runs declared nonblocking handlers inline, otherwise on a
	// goroutine per request. User-copy engines retain goroutine dispatch.
	DispatchAuto = queue.DispatchAuto
	// DispatchAdaptive runs declared nonblocking handlers inline only when the
	// engine has one ready request and no other handler in flight. Crowded CQE
	// batches and undeclared handlers use a goroutine per request. It never
	// speculatively calls an arbitrary blocking handler on the queue thread.
	DispatchAdaptive = queue.DispatchAdaptive
)
