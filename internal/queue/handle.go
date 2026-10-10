package queue

import (
	"syscall"

	"github.com/ehrlich-b/go-ublk/internal/completion"
)

var (
	// ErrStaleRequest rejects a handle from an earlier tag delivery (or zero handle).
	ErrStaleRequest = completion.ErrStaleRequest
	// ErrRequestCompleted rejects further access to a claimed/completed request.
	ErrRequestCompleted = completion.ErrAlreadyCompleted
	// ErrRequestBusy rejects completion or nested access while WithBuffers runs.
	// The caller may retry after that callback returns.
	ErrRequestBusy = completion.ErrRequestBusy
)

// RequestHandle is a generation-bound value identifying one delivery. Copies
// keep the same identity, even when the underlying tag is reused. Its metadata
// is a snapshot; its methods validate the identity before reading recycled
// fields or touching buffers. The zero value is invalid.
type RequestHandle struct {
	Queue   uint16
	Tag     uint16
	Op      Op
	Flags   RequestFlags
	Offset  int64
	Length  int64
	NrZones uint32

	r          *Request
	generation uint64
}

// Handle captures this delivery's identity. Call it only while the Request
// belongs to this handler, before completion; retain the returned value for
// asynchronous work. Calling Handle on an already stale *Request cannot
// authenticate its original delivery. Prefer RequestHandlerFunc to capture
// the identity automatically at dispatch.
func (r *Request) Handle() RequestHandle {
	return RequestHandle{Queue: r.Queue, Tag: r.Tag, Op: r.Op, Flags: r.Flags,
		Offset: r.Offset, Length: r.Length, NrZones: r.NrZones,
		r: r, generation: r.state.Generation()}
}

// RequestHandlerFunc adapts a function receiving generation-bound values to
// Handler, including DeviceParams.Handler. It allocates no request objects.
type RequestHandlerFunc func(RequestHandle)

func (f RequestHandlerFunc) HandleRequest(r *Request) { f(r.Handle()) }

// WithBuffers gives exclusive access to data, integrity metadata and the
// read-only descriptor extension for this delivery. Slices are borrowed only
// for the callback: do not retain them or use them from another goroutine after
// it returns. Complete methods return ErrRequestBusy during the callback;
// complete after WithBuffers returns. Rejected access never calls use.
func (h RequestHandle) WithBuffers(use func(data, integrity, descriptorExtra []byte) error) error {
	if h.r == nil {
		return ErrStaleRequest
	}
	return completion.Access(&h.r.state, h.generation, func() error {
		return use(h.r.Data, h.r.Integrity, h.r.DescriptorExtra)
	})
}

func (h RequestHandle) finish(stage func(*Request)) error {
	if h.r == nil {
		return ErrStaleRequest
	}
	r := h.r
	return completion.FinishGeneration(&r.state, h.generation, func() {
		stage(r)
		r.e.beforeCommit(r)
	}, func() { r.e.push(r) })
}

// Complete finishes this delivery. Rejected completion has no result, copy,
// buffer or enqueue effect. Duplicate calls return ErrRequestCompleted; calls
// after tag reuse return ErrStaleRequest. See Request.Complete for result rules.
func (h RequestHandle) Complete(err error) error {
	return h.finish(func(r *Request) {
		switch {
		case err != nil:
			r.result = -Errno(err)
		case r.Op.carriesData():
			r.result = int32(r.Length)
		default:
			r.result = 0
		}
	})
}

// CompleteN follows Request.CompleteN's short-transfer rules for this delivery.
func (h RequestHandle) CompleteN(n int, err error) error {
	return h.finish(func(r *Request) {
		switch {
		case err != nil:
			r.result = -Errno(err)
		case n < 0 || int64(n) > r.Length:
			r.result = -int32(syscall.EIO)
		case r.Op == OpRead && n > 0 && r.e.partialReadsOK():
			r.result = int32(n)
		case int64(n) == r.Length && r.Op.carriesData():
			r.result = int32(r.Length)
		case int64(n) == r.Length:
			r.result = 0
		default:
			r.result = -int32(syscall.EIO)
		}
	})
}

// CompleteZoneAppend reports the written sector for this delivery.
func (h RequestHandle) CompleteZoneAppend(sector uint64, err error) error {
	return h.finish(func(r *Request) {
		if err != nil {
			r.result = -Errno(err)
			return
		}
		r.result, r.lba = int32(r.Length), sector
	})
}

// ReportZones fills and completes this delivery after claiming ownership.
func (h RequestHandle) ReportZones(zones []BlkZone) error {
	return h.finish(func(r *Request) {
		if r.Op != OpReportZones {
			r.result = -int32(syscall.EINVAL)
			return
		}
		fillZones(r.Data, zones)
		r.result = int32(r.Length)
	})
}
