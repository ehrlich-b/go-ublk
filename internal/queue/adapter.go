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
	handle := r.Handle()
	var start time.Time
	if h.obs != nil {
		start = time.Now()
	}
	var err error
	err = handle.WithBuffers(func(data, integrity, _ []byte) error {
		switch handle.Op {
		case OpRead:
			n, rerr := h.b.ReadAt(data, handle.Offset)
			err = readResultError(n, len(data), rerr)
			if ib, ok := h.b.(interfaces.IntegrityBackend); ok && err == nil && integrity != nil {
				err = ib.ReadIntegrity(integrity, handle.Offset)
			}
			if h.obs != nil {
				h.obs.ObserveRead(uint64(handle.Length), uint64(time.Since(start)), err == nil)
			}
		case OpWrite:
			var n int
			var werr error
			if fua, ok := h.b.(interfaces.FUABackend); ok && handle.Flags&FlagFUA != 0 {
				n, werr = fua.WriteAtFUA(data, handle.Offset)
			} else {
				n, werr = h.b.WriteAt(data, handle.Offset)
			}
			err = writeResultError(n, len(data), werr)
			if ib, ok := h.b.(interfaces.IntegrityBackend); ok && err == nil && integrity != nil {
				err = ib.WriteIntegrity(integrity, handle.Offset)
			}
			if h.obs != nil {
				h.obs.ObserveWrite(uint64(handle.Length), uint64(time.Since(start)), err == nil)
			}
		case OpFlush:
			err = h.b.Flush()
			if h.obs != nil {
				h.obs.ObserveFlush(uint64(time.Since(start)), err == nil)
			}
		case OpDiscard:
			if d, ok := h.b.(interfaces.DiscardBackend); ok {
				err = d.Discard(handle.Offset, handle.Length)
			} else {
				err = syscall.EOPNOTSUPP
			}
			if h.obs != nil {
				h.obs.ObserveDiscard(uint64(handle.Length), uint64(time.Since(start)), err == nil)
			}
		case OpWriteZeroes:
			if z, ok := h.b.(interfaces.WriteZeroesBackend); ok {
				err = z.WriteZeroes(handle.Offset, handle.Length)
			} else {
				err = syscall.EOPNOTSUPP
			}
			if h.obs != nil {
				h.obs.ObserveWrite(uint64(handle.Length), uint64(time.Since(start)), err == nil)
			}
		default:
			err = fmt.Errorf("%s: %w", handle.Op, syscall.EOPNOTSUPP)
		}
		return err
	})
	if completionErr := handle.Complete(err); completionErr != nil {
		panic(completionErr)
	}
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
