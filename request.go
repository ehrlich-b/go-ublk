package ublk

import (
	"syscall"

	"github.com/ehrlich-b/go-ublk/internal/queue"
)

// Request is one block request from the kernel, handed to a Handler. Its
// fields, and its Data buffer, are valid only until one of its Complete
// methods is called:
//
//   - Complete(err): success reports the full Length for a data operation and
//     zero for anything else; a non-nil err fails the request with Errno(err).
//   - CompleteN(n, err): a read that transferred only n bytes (the kernel
//     resubmits the rest). Any other short transfer fails with EIO, because
//     the kernel would otherwise treat it as complete success.
//   - CompleteZoneAppend(sector, err): a zone append that wrote at sector.
//
// Exactly one Complete call must be made, exactly once, from any goroutine,
// before or after HandleRequest returns. Completing twice panics.
//
// Deprecated: use RequestHandle through RequestHandlerFunc, or capture Handle
// before completion. Recycled pointers and exported slices cannot be checked
// after tag reuse without changing the API or allocating per delivery.
type Request = queue.Request

// RequestHandle identifies one delivery by generation. Completion returns an
// error on duplicate/stale calls; WithBuffers scopes checked buffer access.
type RequestHandle = queue.RequestHandle

// RequestHandlerFunc serves generation-bound values through DeviceParams.Handler.
// Example: Handler: RequestHandlerFunc(func(h RequestHandle) { ... }).
type RequestHandlerFunc = queue.RequestHandlerFunc

var (
	ErrStaleRequest     = queue.ErrStaleRequest
	ErrRequestCompleted = queue.ErrRequestCompleted
	ErrRequestBusy      = queue.ErrRequestBusy
)

// Handler serves raw block requests: see Request and DeviceParams.Handler.
// Unless DeviceParams.Inline is set, every request runs on its own goroutine,
// so a handler is called concurrently, up to QueueDepth times per queue.
type Handler = queue.Handler

// HandlerFunc adapts a function to Handler.
//
// Deprecated: use RequestHandlerFunc for checked generation-bound values.
type HandlerFunc = queue.HandlerFunc

// Op is a block operation (Request.Op).
type Op = queue.Op

// The block operations a Handler can receive.
const (
	OpRead         = queue.OpRead
	OpWrite        = queue.OpWrite
	OpFlush        = queue.OpFlush
	OpDiscard      = queue.OpDiscard
	OpWriteSame    = queue.OpWriteSame
	OpWriteZeroes  = queue.OpWriteZeroes
	OpZoneOpen     = queue.OpZoneOpen
	OpZoneClose    = queue.OpZoneClose
	OpZoneFinish   = queue.OpZoneFinish
	OpZoneAppend   = queue.OpZoneAppend
	OpZoneResetAll = queue.OpZoneResetAll
	OpZoneReset    = queue.OpZoneReset
	OpReportZones  = queue.OpReportZones
)

// RequestFlags are the UBLK_IO_F_* flags of a request (Request.Flags).
type RequestFlags = queue.RequestFlags

// Request flags.
const (
	FlagFailFastDev       = queue.FlagFailFastDev
	FlagFailFastTransport = queue.FlagFailFastTransport
	FlagFailFastDriver    = queue.FlagFailFastDriver
	FlagMeta              = queue.FlagMeta
	FlagFUA               = queue.FlagFUA     // the write must be durable when completed
	FlagNoUnmap           = queue.FlagNoUnmap // write-zeroes must not deallocate
	FlagSwap              = queue.FlagSwap
	FlagNeedRegBuf        = queue.FlagNeedRegBuf
	FlagIntegrity         = queue.FlagIntegrity
	FlagSharedMemory      = queue.FlagSharedMemory
)

// Errno is the errno a handler error is reported to the kernel as: a
// syscall.Errno (also when wrapped) is passed through, deadline errors become
// ETIMEDOUT, errors.ErrUnsupported becomes EOPNOTSUPP, and anything else EIO.
// Kernels translate it to a block status (ENOSPC, ETIMEDOUT, EOPNOTSUPP, ...
// reach the application); older kernels report every failure as EIO.
func Errno(err error) syscall.Errno { return syscall.Errno(queue.Errno(err)) }

// FUABackend is an optional Backend interface: WriteAtFUA must make the write
// durable before returning, without flushing anything else. A backend that
// implements it can set DeviceParams.EnableFUA; writes the application issues
// with FUA (O_DSYNC, REQ_FUA from a filesystem journal) then arrive here
// instead of as a write followed by a whole-device flush.
type FUABackend interface {
	Backend
	WriteAtFUA(p []byte, off int64) (n int, err error)
}

// ZeroCopyBackend is an optional Backend interface for backends that store
// the device linearly in a file or block device. With DeviceParams.EnableZeroCopy
// (kernel 6.15+) go-ublk then serves every request in the kernel: data moves
// between the request's pages and the file with io_uring fixed-buffer reads
// and writes, flush is fdatasync, discard punches holes and write-zeroes
// zeroes the range. ReadAt, WriteAt and Flush are never called; the file is
// used directly. Device offset 0 is file offset base, and the file must be at
// least base + Size() bytes long.
type ZeroCopyBackend interface {
	Backend
	ZeroCopyFile() (fd int, base int64)
}

// BlkZone is one zone in a zone report; see Request.ReportZones.
type BlkZone = queue.BlkZone

// Zone types and conditions for BlkZone (include/uapi/linux/blkzoned.h).
const (
	ZoneTypeConventional = queue.ZoneTypeConventional
	ZoneTypeSeqWriteReq  = queue.ZoneTypeSeqWriteReq
	ZoneTypeSeqWritePref = queue.ZoneTypeSeqWritePref
	ZoneCondNotWP        = queue.ZoneCondNotWP
	ZoneCondEmpty        = queue.ZoneCondEmpty
	ZoneCondImpOpen      = queue.ZoneCondImpOpen
	ZoneCondExpOpen      = queue.ZoneCondExpOpen
	ZoneCondClosed       = queue.ZoneCondClosed
	ZoneCondReadOnly     = queue.ZoneCondReadOnly
	ZoneCondFull         = queue.ZoneCondFull
	ZoneCondOffline      = queue.ZoneCondOffline
)

// IntegrityBackend is the Backend interface for devices with
// DeviceParams.Integrity: alongside ReadAt and WriteAt for the data, it
// stores and returns each request's integrity metadata (MetadataSize bytes
// per IntervalSize bytes of data, for the same offset). With a checksum type
// the kernel generates the protection information and verifies it on read, so
// metadata returned wrong fails the read. For blocks never written, return
// all 0xff bytes: the T10 escape value (application tag 0xffff), which tells
// the kernel not to check them — otherwise even the partition scan at start
// fails verification.
type IntegrityBackend interface {
	Backend
	ReadIntegrity(meta []byte, off int64) error
	WriteIntegrity(meta []byte, off int64) error
}
