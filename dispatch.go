package ublk

import "github.com/ehrlich-b/go-ublk/internal/queue"

// DispatchMode selects handler scheduling when DeviceParams.Inline is false.
type DispatchMode = queue.DispatchMode

const (
	// DispatchGoroutine launches a goroutine per request and is the default.
	DispatchGoroutine = queue.DispatchGoroutine
	// DispatchPool reuses QueueDepth handler goroutines per queue. Workers
	// share a buffered handoff without allocating a job for each request.
	DispatchPool = queue.DispatchPool
)
