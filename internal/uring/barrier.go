package uring

import "sync/atomic"

// barrierDummy is used for atomic operations that provide memory barrier semantics.
// On x86-64, atomic.AddInt64 compiles to LOCK XADD which has full fence semantics.
var barrierDummy int64

// Sfence issues a store fence equivalent.
// atomic.AddInt64 with 0 compiles to LOCK XADD on x86-64, which provides
// full memory fence semantics with no contention and minimal overhead (~20 cycles).
func Sfence() {
	atomic.AddInt64(&barrierDummy, 0)
}

// Mfence issues a full memory fence equivalent.
// Same implementation as Sfence - LOCK XADD provides full fence on x86-64.
func Mfence() {
	atomic.AddInt64(&barrierDummy, 0)
}
