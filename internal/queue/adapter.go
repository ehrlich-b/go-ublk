package queue

import (
	"fmt"
	"io"
	"syscall"
	"time"

	"github.com/ehrlich-b/go-ublk/internal/interfaces"
)

// backendHandler serves requests with a synchronous Backend.
type backendHandler struct {
	b   interfaces.Backend
	obs interfaces.Observer
}

// BackendHandler adapts a Backend (plus whichever optional interfaces it
// implements) to a Handler. The observer, if non-nil, sees every operation.
func BackendHandler(b interfaces.Backend, obs interfaces.Observer) Handler {
	return &backendHandler{b: b, obs: obs}
}

func (h *backendHandler) HandleRequest(r *Request) {
	// A synchronous Backend never receives a handle. Claim this delivery once
	// around its buffer use, result staging and copy-out, rather than borrowing
	// and releasing buffers only to claim them again for completion.
	handle := RequestHandle{r: r, generation: r.state.Generation()}
	var backendPanic any
	if err := handle.finish(func(r *Request) {
		// Release and publish EIO even when the backend or observer panics.
		// Re-panic after publication so the engine's usual recovery logs it.
		defer func() {
			if p := recover(); p != nil {
				backendPanic = p
				r.result = -int32(syscall.EIO)
			}
		}()
		err := h.serve(r)
		switch {
		case err != nil:
			r.result = -Errno(err)
		case r.Op.carriesData():
			r.result = int32(r.Length)
		default:
			r.result = 0
		}
	}); err != nil {
		panic(err)
	}
	if backendPanic != nil {
		panic(backendPanic)
	}
}

func (h *backendHandler) serve(r *Request) error {
	var start time.Time
	if h.obs != nil {
		start = time.Now()
	}
	var err error
	switch r.Op {
	case OpRead:
		n, rerr := h.b.ReadAt(r.Data, r.Offset)
		err = readResultError(n, len(r.Data), rerr)
		if ib, ok := h.b.(interfaces.IntegrityBackend); ok && err == nil && r.Integrity != nil {
			err = ib.ReadIntegrity(r.Integrity, r.Offset)
		}
		if h.obs != nil {
			h.obs.ObserveRead(uint64(r.Length), uint64(time.Since(start)), err == nil)
		}
	case OpWrite:
		var n int
		var werr error
		if fua, ok := h.b.(interfaces.FUABackend); ok && r.Flags&FlagFUA != 0 {
			n, werr = fua.WriteAtFUA(r.Data, r.Offset)
		} else {
			n, werr = h.b.WriteAt(r.Data, r.Offset)
		}
		err = writeResultError(n, len(r.Data), werr)
		if ib, ok := h.b.(interfaces.IntegrityBackend); ok && err == nil && r.Integrity != nil {
			err = ib.WriteIntegrity(r.Integrity, r.Offset)
		}
		if h.obs != nil {
			h.obs.ObserveWrite(uint64(r.Length), uint64(time.Since(start)), err == nil)
		}
	case OpFlush:
		err = h.b.Flush()
		if h.obs != nil {
			h.obs.ObserveFlush(uint64(time.Since(start)), err == nil)
		}
	case OpDiscard:
		if d, ok := h.b.(interfaces.DiscardBackend); ok {
			err = d.Discard(r.Offset, r.Length)
		} else {
			err = syscall.EOPNOTSUPP
		}
		if h.obs != nil {
			h.obs.ObserveDiscard(uint64(r.Length), uint64(time.Since(start)), err == nil)
		}
	case OpWriteZeroes:
		if z, ok := h.b.(interfaces.WriteZeroesBackend); ok {
			err = z.WriteZeroes(r.Offset, r.Length)
		} else {
			err = syscall.EOPNOTSUPP
		}
		if h.obs != nil {
			h.obs.ObserveWrite(uint64(r.Length), uint64(time.Since(start)), err == nil)
		}
	default:
		err = fmt.Errorf("%s: %w", r.Op, syscall.EOPNOTSUPP)
	}
	return err
}

func readResultError(n, length int, err error) error {
	if n < 0 || n > length {
		return fmt.Errorf("backend ReadAt returned invalid count %d for %d-byte buffer", n, length)
	}
	if n == length && err == io.EOF {
		return nil
	}
	if err != nil {
		return err
	}
	if n != length {
		return io.ErrUnexpectedEOF
	}
	return nil
}

func writeResultError(n, length int, err error) error {
	if n < 0 || n > length {
		return fmt.Errorf("backend WriteAt returned invalid count %d for %d-byte buffer", n, length)
	}
	if err != nil {
		return err
	}
	if n != length {
		return io.ErrShortWrite
	}
	return nil
}
