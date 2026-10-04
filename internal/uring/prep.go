package uring

import (
	"fmt"
	"unsafe"

	"golang.org/x/sys/unix"
)

// Prep helpers fill an SQE fresh from GetSQE (already zeroed) and set only
// the fields their opcode uses. Set UserData, and OR in Flags such as
// IOSQE_IO_LINK or IOSQE_FIXED_FILE, afterwards. With IOSQE_FIXED_FILE the fd
// argument is an index into the registered file table. Buffers must follow
// the pinning rules in ring.go. They allocate nothing.

func prepRW(sqe *SQE, op uint8, fd int32, addr uint64, length uint32, offset uint64) {
	sqe.Opcode = op
	sqe.Fd = fd
	sqe.Addr = addr
	sqe.Len = length
	sqe.Off = offset
}

// PrepNop prepares an IORING_OP_NOP, which completes with res 0.
func PrepNop(sqe *SQE) {
	sqe.Opcode = IORING_OP_NOP
}

// PrepRead prepares an IORING_OP_READ of len(buf) bytes at offset (5.6).
// offset ^uint64(0) reads at the file position.
func PrepRead(sqe *SQE, fd int32, buf []byte, offset uint64) {
	prepRW(sqe, IORING_OP_READ, fd, AddrOf(buf), uint32(len(buf)), offset)
}

// PrepReadSelect prepares an IORING_OP_READ of up to length bytes into a
// buffer the kernel takes from provided-buffer group group; CQE.BufferID
// reports which one.
func PrepReadSelect(sqe *SQE, fd int32, length uint32, offset uint64, group uint16) {
	prepRW(sqe, IORING_OP_READ, fd, 0, length, offset)
	sqe.SetBufferSelect(group)
}

// PrepWrite prepares an IORING_OP_WRITE of buf at offset (5.6).
func PrepWrite(sqe *SQE, fd int32, buf []byte, offset uint64) {
	prepRW(sqe, IORING_OP_WRITE, fd, AddrOf(buf), uint32(len(buf)), offset)
}

// PrepReadFixed prepares an IORING_OP_READ_FIXED of length bytes at offset
// into registered buffer bufIndex, starting at addr.
//
// What addr means depends on how the buffer got into the table (kernel
// io_import_fixed, io_uring/rsrc.c): validate_fixed_range requires
// [addr, addr+length) to lie inside [imu->ubuf, imu->ubuf+imu->len). For a
// buffer registered from userspace (RegisterBuffers, UpdateBuffers) ubuf is
// the buffer's virtual address, so addr is a pointer into it. For a kernel
// buffer installed into a sparse slot by a driver (ublk UBLK_U_IO_REGISTER_IO_BUF
// or UBLK_F_AUTO_BUF_REG, 6.15+), io_kernel_buffer_init sets ubuf = 0, so
// addr is the byte offset into the request's data. Kernel buffers are also
// direction-checked (imu->dir): a ublk READ request's buffer can only be the
// destination of READ_FIXED, a WRITE request's only the source of
// WRITE_FIXED; the wrong direction fails with -EFAULT.
func PrepReadFixed(sqe *SQE, fd int32, addr uint64, length uint32, offset uint64, bufIndex uint16) {
	prepRW(sqe, IORING_OP_READ_FIXED, fd, addr, length, offset)
	sqe.BufIndex = bufIndex
}

// PrepWriteFixed prepares an IORING_OP_WRITE_FIXED of length bytes at offset
// from registered buffer bufIndex; see PrepReadFixed for what addr means.
func PrepWriteFixed(sqe *SQE, fd int32, addr uint64, length uint32, offset uint64, bufIndex uint16) {
	prepRW(sqe, IORING_OP_WRITE_FIXED, fd, addr, length, offset)
	sqe.BufIndex = bufIndex
}

// PrepReadv prepares an IORING_OP_READV into iovecs at offset. The iovec
// array must stay valid until submission; the buffers until completion.
func PrepReadv(sqe *SQE, fd int32, iovecs []unix.Iovec, offset uint64) {
	prepRW(sqe, IORING_OP_READV, fd, iovecsAddr(iovecs), uint32(len(iovecs)), offset)
}

// PrepWritev prepares an IORING_OP_WRITEV from iovecs at offset; lifetimes
// as for PrepReadv.
func PrepWritev(sqe *SQE, fd int32, iovecs []unix.Iovec, offset uint64) {
	prepRW(sqe, IORING_OP_WRITEV, fd, iovecsAddr(iovecs), uint32(len(iovecs)), offset)
}

func iovecsAddr(iovecs []unix.Iovec) uint64 {
	if len(iovecs) == 0 {
		return 0
	}
	p := unsafe.Pointer(unsafe.SliceData(iovecs))
	escape(p)
	return uint64(uintptr(p))
}

// PrepFsync prepares an IORING_OP_FSYNC of the whole file; flags 0 is
// fsync, IORING_FSYNC_DATASYNC is fdatasync.
func PrepFsync(sqe *SQE, fd int32, flags uint32) {
	sqe.Opcode = IORING_OP_FSYNC
	sqe.Fd = fd
	sqe.OpFlags = flags
}

// PrepFallocate prepares an IORING_OP_FALLOCATE (5.6) of length bytes at
// offset with fallocate(2) mode, e.g. FALLOC_FL_PUNCH_HOLE|FALLOC_FL_KEEP_SIZE
// for discard or FALLOC_FL_ZERO_RANGE for write-zeroes. The kernel takes the
// length from addr and the mode from len.
func PrepFallocate(sqe *SQE, fd int32, mode uint32, offset, length uint64) {
	prepRW(sqe, IORING_OP_FALLOCATE, fd, length, mode, offset)
}

// PrepPollAdd prepares a one-shot IORING_OP_POLL_ADD for poll(2) events;
// set sqe.Len = IORING_POLL_ADD_MULTI afterwards for multishot. The events
// word is stored as on little-endian kernels (amd64, arm64).
func PrepPollAdd(sqe *SQE, fd int32, events uint32) {
	sqe.Opcode = IORING_OP_POLL_ADD
	sqe.Fd = fd
	sqe.OpFlags = events
}

// PrepCancel prepares an IORING_OP_ASYNC_CANCEL (5.5) of the request(s)
// whose user_data is targetUserData. With IORING_ASYNC_CANCEL_FD (5.19) in
// flags, set sqe.Fd to the file to match instead.
func PrepCancel(sqe *SQE, targetUserData uint64, flags uint32) {
	sqe.Opcode = IORING_OP_ASYNC_CANCEL
	sqe.Fd = -1
	sqe.Addr = targetUserData
	sqe.OpFlags = flags
}

// PrepTimeout prepares an IORING_OP_TIMEOUT (5.4). It completes with -ETIME
// when ts elapses, or with 0 once count other completions have posted
// (count 0: only the timer). ts must stay valid until submission.
func PrepTimeout(sqe *SQE, ts *Timespec, count uint32, flags uint32) {
	p := unsafe.Pointer(ts)
	escape(p)
	sqe.Opcode = IORING_OP_TIMEOUT
	sqe.Fd = -1
	sqe.Addr = uint64(uintptr(p))
	sqe.Len = 1
	sqe.Off = uint64(count)
	sqe.OpFlags = flags
}

// PrepLinkTimeout prepares an IORING_OP_LINK_TIMEOUT (5.5) bounding the
// preceding SQE, which must carry IOSQE_IO_LINK. If ts elapses first the
// linked request is cancelled (-ECANCELED) and this one completes -ETIME.
// ts must stay valid until submission.
func PrepLinkTimeout(sqe *SQE, ts *Timespec, flags uint32) {
	p := unsafe.Pointer(ts)
	escape(p)
	sqe.Opcode = IORING_OP_LINK_TIMEOUT
	sqe.Fd = -1
	sqe.Addr = uint64(uintptr(p))
	sqe.Len = 1
	sqe.OpFlags = flags
}

// PrepMsgRing prepares an IORING_OP_MSG_RING (5.18) that posts a CQE with
// user_data userData and res res to the ring whose fd is ringFd, waking a
// thread waiting on it.
func PrepMsgRing(sqe *SQE, ringFd int32, res uint32, userData uint64, flags uint32) {
	prepRW(sqe, IORING_OP_MSG_RING, ringFd, IORING_MSG_DATA, res, userData)
	sqe.OpFlags = flags
}

// PrepUringCmd prepares an IORING_OP_URING_CMD (5.19) with a payload of up
// to 16 bytes, which fits a 64-byte SQE (a ublk I/O command). For a
// registered buffer set sqe.OpFlags |= IORING_URING_CMD_FIXED and
// sqe.BufIndex; for multishot (6.18) set IORING_URING_CMD_MULTISHOT and call
// sqe.SetBufferSelect. It panics if the payload does not fit.
func PrepUringCmd(sqe *SQE, fd int32, cmdOp uint32, payload []byte) {
	if len(payload) > 16 {
		panic(fmt.Sprintf("uring: %d-byte URING_CMD payload does not fit a 64-byte SQE", len(payload)))
	}
	sqe.Opcode = IORING_OP_URING_CMD
	sqe.Fd = fd
	sqe.SetCmdOp(cmdOp)
	copy(sqe.Cmd()[:], payload)
}

// PrepUringCmd128 is PrepUringCmd for an SQE128 ring, with a payload of up
// to 80 bytes (a 32-byte ublk control command). It panics if the payload
// does not fit.
func PrepUringCmd128(sqe *SQE128, fd int32, cmdOp uint32, payload []byte) {
	if len(payload) > 80 {
		panic(fmt.Sprintf("uring: %d-byte URING_CMD payload does not fit a 128-byte SQE", len(payload)))
	}
	sqe.Opcode = IORING_OP_URING_CMD
	sqe.Fd = fd
	sqe.SetCmdOp(cmdOp)
	copy(sqe.Cmd()[:], payload)
}
