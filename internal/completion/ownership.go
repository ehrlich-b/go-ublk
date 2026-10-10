package completion

import (
	"errors"
	"sync/atomic"
)

const (
	Idle uint32 = iota
	Dispatching
	Async
	Done
	Queued
	completingInline
	completingAsync
)

var ErrAlreadyCompleted = errors.New("request already claimed for completion")

// Finish claims completion before staging a result or touching borrowed data.
// Publication follows staging, so the engine can never commit a half-written
// result. A losing completer has no callback or queue side effect.
func Finish(state *atomic.Uint32, stage, enqueue func()) error {
	for {
		s := state.Load()
		var claimed uint32
		switch s {
		case Dispatching:
			claimed = completingInline
		case Async:
			claimed = completingAsync
		default:
			return ErrAlreadyCompleted
		}
		if state.CompareAndSwap(s, claimed) {
			break
		}
	}
	stage()
	// The inline handler may return while staging runs in another goroutine.
	// ReturnInline transfers that completion to the async publication path.
	if state.CompareAndSwap(completingInline, Done) {
		return nil
	}
	state.Store(Queued)
	enqueue()
	return nil
}

// ReturnInline is called once by the engine after the handler returns. True
// means a fully staged inline result is ready to commit. A completion still
// staging must enqueue itself instead; its buffers remain owned meanwhile.
func ReturnInline(state *atomic.Uint32) bool {
	for {
		switch s := state.Load(); s {
		case Dispatching:
			if state.CompareAndSwap(s, Async) {
				return false
			}
		case completingInline:
			if state.CompareAndSwap(s, completingAsync) {
				return false
			}
		case Done:
			return true
		default:
			return false
		}
	}
}
