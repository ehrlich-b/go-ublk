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
	accessingInline
	accessingAsync
)

const stateBits = 4
const MaxGeneration = uint64(1<<(64-stateBits)) - 1

var (
	ErrAlreadyCompleted    = errors.New("request already claimed for completion")
	ErrStaleRequest        = errors.New("stale request generation")
	ErrRequestBusy         = errors.New("request buffer access in progress")
	ErrGenerationExhausted = errors.New("request generation exhausted")
)

// Ownership packs the delivery generation and phase in one atomic word. A
// separate generation check followed by a phase CAS would allow tag-reuse ABA.
// The engine alone calls Begin/Store; handlers use FinishGeneration/Access.
type Ownership struct{ word atomic.Uint64 }

func (o *Ownership) Generation() uint64 { return o.word.Load() >> stateBits }
func (o *Ownership) Load() uint32       { return uint32(o.word.Load() & (1<<stateBits - 1)) }
func (o *Ownership) Store(phase uint32) {
	o.word.Store(o.word.Load() & ^uint64(1<<stateBits-1) | uint64(phase))
}

// Begin starts a delivery before any recycled fields or buffers are exposed.
// Never wrap: a retired generation must not authenticate a retained handle.
func (o *Ownership) Begin() error {
	word := o.word.Load()
	if word>>stateBits == MaxGeneration {
		return ErrGenerationExhausted
	}
	o.word.Store((word>>stateBits + 1) << stateBits)
	return nil
}

// Finish is the legacy pointer-only entrypoint. It cannot identify a retained
// caller after reuse; new callers must snapshot Generation at delivery.
func Finish(state *Ownership, stage, enqueue func()) error {
	return FinishGeneration(state, state.Generation(), stage, enqueue)
}

func claim(state *Ownership, generation uint64, inline, async uint32) (uint64, error) {
	for {
		word := state.word.Load()
		if generation == 0 || word>>stateBits != generation {
			return 0, ErrStaleRequest
		}
		base := generation << stateBits
		var claimed uint64
		switch uint32(word & (1<<stateBits - 1)) {
		case Dispatching:
			claimed = base | uint64(inline)
		case Async:
			claimed = base | uint64(async)
		case accessingInline, accessingAsync:
			return 0, ErrRequestBusy
		default:
			return 0, ErrAlreadyCompleted
		}
		if state.word.CompareAndSwap(word, claimed) {
			return base, nil
		}
	}
}

// FinishGeneration claims the exact generation before staging results or
// touching borrowed data. Publication follows staging; rejection has no effect.
func FinishGeneration(state *Ownership, generation uint64, stage, enqueue func()) error {
	base, err := claim(state, generation, completingInline, completingAsync)
	if err != nil {
		return err
	}
	stage()
	if state.word.CompareAndSwap(base|uint64(completingInline), base|uint64(Done)) {
		return nil
	}
	state.word.Store(base | uint64(Queued))
	enqueue()
	return nil
}

// Access holds a generation's buffers for the callback. Completion cannot
// claim them until it returns. Slices must not escape the callback. A panic
// releases ownership before propagating to the handler's recovery path.
func Access(state *Ownership, generation uint64, use func() error) error {
	base, err := claim(state, generation, accessingInline, accessingAsync)
	if err != nil {
		return err
	}
	defer func() {
		if !state.word.CompareAndSwap(base|uint64(accessingInline), base|uint64(Dispatching)) {
			state.word.Store(base | uint64(Async))
		}
	}()
	return use()
}

// ReturnInline transfers a still-staging completion or active buffer callback
// to async ownership when the inline handler returns. True means ready to commit.
func ReturnInline(state *Ownership) bool {
	for {
		word := state.word.Load()
		base := word & ^uint64(1<<stateBits-1)
		var next uint32
		switch uint32(word & (1<<stateBits - 1)) {
		case Dispatching:
			next = Async
		case completingInline:
			next = completingAsync
		case accessingInline:
			next = accessingAsync
		case Done:
			return true
		default:
			return false
		}
		if state.word.CompareAndSwap(word, base|uint64(next)) {
			return false
		}
	}
}
