package queue

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"sync/atomic"
	"syscall"

	"github.com/ehrlich-b/go-ublk/internal/completion"
	"github.com/ehrlich-b/go-ublk/internal/uapi"
)

// Op is a block operation, the low byte of ublksrv_io_desc.op_flags.
type Op uint8

const (
	OpRead         Op = uapi.UBLK_IO_OP_READ
	OpWrite        Op = uapi.UBLK_IO_OP_WRITE
	OpFlush        Op = uapi.UBLK_IO_OP_FLUSH
	OpDiscard      Op = uapi.UBLK_IO_OP_DISCARD
	OpWriteSame    Op = uapi.UBLK_IO_OP_WRITE_SAME
	OpWriteZeroes  Op = uapi.UBLK_IO_OP_WRITE_ZEROES
	OpZoneOpen     Op = uapi.UBLK_IO_OP_ZONE_OPEN
	OpZoneClose    Op = uapi.UBLK_IO_OP_ZONE_CLOSE
	OpZoneFinish   Op = uapi.UBLK_IO_OP_ZONE_FINISH
	OpZoneAppend   Op = uapi.UBLK_IO_OP_ZONE_APPEND
	OpZoneResetAll Op = uapi.UBLK_IO_OP_ZONE_RESET_ALL
	OpZoneReset    Op = uapi.UBLK_IO_OP_ZONE_RESET
	OpReportZones  Op = uapi.UBLK_IO_OP_REPORT_ZONES
)

var opNames = map[Op]string{
	OpRead: "READ", OpWrite: "WRITE", OpFlush: "FLUSH", OpDiscard: "DISCARD",
	OpWriteSame: "WRITE_SAME", OpWriteZeroes: "WRITE_ZEROES", OpZoneOpen: "ZONE_OPEN",
	OpZoneClose: "ZONE_CLOSE", OpZoneFinish: "ZONE_FINISH", OpZoneAppend: "ZONE_APPEND",
	OpZoneResetAll: "ZONE_RESET_ALL", OpZoneReset: "ZONE_RESET", OpReportZones: "REPORT_ZONES",
}

func (o Op) String() string {
	if s, ok := opNames[o]; ok {
		return s
	}
	return fmt.Sprintf("OP(%d)", uint8(o))
}

// carriesData reports whether the op moves a data buffer between the kernel
// and the server (and so has a byte-count result).
func (o Op) carriesData() bool {
	return o == OpRead || o == OpWrite || o == OpZoneAppend || o == OpReportZones
}

// RequestFlags are the UBLK_IO_F_* bits of ublksrv_io_desc.op_flags, in place.
type RequestFlags uint32

const (
	FlagFailFastDev       RequestFlags = uapi.UBLK_IO_F_FAILFAST_DEV
	FlagFailFastTransport RequestFlags = uapi.UBLK_IO_F_FAILFAST_TRANSPORT
	FlagFailFastDriver    RequestFlags = uapi.UBLK_IO_F_FAILFAST_DRIVER
	FlagMeta              RequestFlags = uapi.UBLK_IO_F_META
	FlagFUA               RequestFlags = uapi.UBLK_IO_F_FUA
	FlagNoUnmap           RequestFlags = uapi.UBLK_IO_F_NOUNMAP
	FlagSwap              RequestFlags = uapi.UBLK_IO_F_SWAP
	FlagNeedRegBuf        RequestFlags = 1 << 17 // UBLK_IO_F_NEED_REG_BUF
	FlagIntegrity         RequestFlags = 1 << 18 // UBLK_IO_F_INTEGRITY
	FlagSharedMemory      RequestFlags = 1 << 19 // UBLK_IO_F_SHMEM_ZC
)

// Handler serves block requests. HandleRequest must arrange for exactly one
// of the Request's Complete methods to be called exactly once — before it
// returns or later, from any goroutine.
type Handler interface {
	HandleRequest(*Request)
}

// HandlerFunc adapts a function to Handler.
type HandlerFunc func(*Request)

func (f HandlerFunc) HandleRequest(r *Request) { f(r) }

// Request is one block request the kernel handed to the server. Its fields are
// valid, and Data may be used, only until a Complete method is called; the
// engine reuses the Request and its buffer for the tag's next request.
type Request struct {
	Queue uint16
	Tag   uint16
	Op    Op
	Flags RequestFlags
	// Offset and Length are in bytes. For OpReportZones, Length is the size of
	// the report buffer and NrZones the number of zones asked for.
	Offset  int64
	Length  int64
	NrZones uint32
	// Data is the request's payload buffer: the bytes to write for OpWrite,
	// where to put the bytes read for OpRead. Nil for operations without data
	// and in zero-copy mode, where the bytes never pass through the server.
	Data []byte
	// Integrity is the request's integrity metadata (FlagIntegrity, on a
	// device with integrity parameters): what to store for a write, where to
	// put the stored metadata for a read. MetadataSize bytes per interval.
	Integrity []byte
	// DescriptorExtra is the part of the kernel's I/O descriptor beyond the
	// standard 24 bytes, on a device created with a larger IODescSize
	// (UBLK_F_IO_DESC_SIZE, kernel 7.3+); nil otherwise. Read-only.
	DescriptorExtra []byte

	e          *engine
	state      atomic.Uint32
	result     int32
	lba        uint64 // zone append result, in sectors
	next       *Request
	generation uint64 // batch ledger generation; never inferred from a reused pointer

	zcManual      bool // zero copy: we registered the request's buffer and must unregister it
	zcUnsupported bool // zero copy: the op has no file equivalent
	zcPunched     bool // zero copy: write-zeroes retried as a hole punch
}

// Request completion states. A request is handed to the handler in
// reqDispatching (inline mode) or reqAsync (goroutine mode); Complete moves it
// to reqDone (inline: the engine commits it when the handler returns) or
// reqQueued (pushed to the engine's completion list).
const (
	reqIdle        = completion.Idle
	reqDispatching = completion.Dispatching
	reqAsync       = completion.Async
	reqDone        = completion.Done
	reqQueued      = completion.Queued
)

// Complete finishes the request. On success a data operation reports its full
// Length; anything else reports zero. A non-nil err fails it with the errno
// from Errno(err).
func (r *Request) Complete(err error) {
	if err != nil {
		r.finish(-Errno(err))
		return
	}
	if r.Op.carriesData() {
		r.finish(int32(r.Length))
		return
	}
	r.finish(0)
}

// CompleteN finishes a request that transferred only n bytes. Only a read in
// the default copy mode can complete partially: the kernel ends those n bytes
// and resubmits the rest as a new request. Anything else that did not
// transfer its full Length fails with EIO, because the kernel treats every
// non-negative result for it as complete success (__ublk_complete_rq) — a
// short write, or a short read in user-copy or zero-copy mode, reported as
// success would silently lose or invent data.
func (r *Request) CompleteN(n int, err error) {
	switch {
	case err != nil:
		r.finish(-Errno(err))
	case n < 0 || int64(n) > r.Length:
		r.finish(-int32(syscall.EIO))
	case r.Op == OpRead && n > 0 && r.e.partialReadsOK():
		r.finish(int32(n))
	case int64(n) == r.Length:
		r.Complete(nil)
	default:
		r.finish(-int32(syscall.EIO)) // short non-read, or a zero-byte read
	}
}

// CompleteZoneAppend finishes an OpZoneAppend that wrote its data at sector.
func (r *Request) CompleteZoneAppend(sector uint64, err error) {
	if err != nil {
		r.finish(-Errno(err))
		return
	}
	r.finishResult(int32(r.Length), sector, true)
}

func (r *Request) finish(res int32) {
	r.finishResult(res, 0, false)
}

func (r *Request) finishResult(res int32, lba uint64, appendResult bool) {
	err := completion.Finish(&r.state, func() {
		r.result = res
		if appendResult {
			r.lba = lba
		}
		r.e.beforeCommit(r)
	}, func() { r.e.push(r) })
	if err != nil {
		panic(fmt.Sprintf("ublk: request queue %d tag %d completed twice", r.Queue, r.Tag))
	}
}

// Errno maps a handler's error to the errno reported to the kernel. A
// syscall.Errno (also when wrapped) is passed through; deadline errors become
// ETIMEDOUT and unsupported operations EOPNOTSUPP; everything else is EIO.
// Kernels before errno_to_blk_status support in ublk report every failure to
// the block layer as EIO regardless.
func Errno(err error) int32 {
	var errno syscall.Errno
	switch {
	case err == nil:
		return 0
	case errors.As(err, &errno) && errno > 0 && errno < 4096:
		return int32(errno)
	case errors.Is(err, context.DeadlineExceeded), errors.Is(err, os.ErrDeadlineExceeded):
		return int32(syscall.ETIMEDOUT)
	case errors.Is(err, errors.ErrUnsupported):
		return int32(syscall.EOPNOTSUPP)
	case errors.Is(err, io.EOF), errors.Is(err, io.ErrUnexpectedEOF), errors.Is(err, io.ErrShortWrite):
		return int32(syscall.EIO)
	default:
		return int32(syscall.EIO)
	}
}
