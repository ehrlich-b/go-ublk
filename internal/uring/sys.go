package uring

import "unsafe"

// io_uring kernel UAPI, from include/uapi/linux/io_uring.h (checked against
// v6.6 and v7.3-rc5). The kernel version that introduced each item is noted
// where it postdates 5.1; go-ublk's documented minimum is 6.8.

// io_uring_setup(2) flags.
const (
	IORING_SETUP_IOPOLL             = 1 << 0
	IORING_SETUP_SQPOLL             = 1 << 1
	IORING_SETUP_SQ_AFF             = 1 << 2
	IORING_SETUP_CQSIZE             = 1 << 3  // 5.5
	IORING_SETUP_CLAMP              = 1 << 4  // 5.6
	IORING_SETUP_ATTACH_WQ          = 1 << 5  // 5.6
	IORING_SETUP_R_DISABLED         = 1 << 6  // 5.10
	IORING_SETUP_SUBMIT_ALL         = 1 << 7  // 5.18
	IORING_SETUP_COOP_TASKRUN       = 1 << 8  // 5.19
	IORING_SETUP_TASKRUN_FLAG       = 1 << 9  // 5.19
	IORING_SETUP_SQE128             = 1 << 10 // 5.19
	IORING_SETUP_CQE32              = 1 << 11 // 5.19
	IORING_SETUP_SINGLE_ISSUER      = 1 << 12 // 6.0
	IORING_SETUP_DEFER_TASKRUN      = 1 << 13 // 6.1
	IORING_SETUP_NO_MMAP            = 1 << 14 // 6.5
	IORING_SETUP_REGISTERED_FD_ONLY = 1 << 15 // 6.5
	IORING_SETUP_NO_SQARRAY         = 1 << 16 // 6.6
)

// io_uring_params.features bits.
const (
	IORING_FEAT_SINGLE_MMAP     = 1 << 0  // 5.4
	IORING_FEAT_NODROP          = 1 << 1  // 5.5
	IORING_FEAT_SUBMIT_STABLE   = 1 << 2  // 5.5
	IORING_FEAT_RW_CUR_POS      = 1 << 3  // 5.6
	IORING_FEAT_CUR_PERSONALITY = 1 << 4  // 5.6
	IORING_FEAT_FAST_POLL       = 1 << 5  // 5.7
	IORING_FEAT_POLL_32BITS     = 1 << 6  // 5.9
	IORING_FEAT_SQPOLL_NONFIXED = 1 << 7  // 5.11
	IORING_FEAT_EXT_ARG         = 1 << 8  // 5.11
	IORING_FEAT_NATIVE_WORKERS  = 1 << 9  // 5.12
	IORING_FEAT_RSRC_TAGS       = 1 << 10 // 5.13
	IORING_FEAT_CQE_SKIP        = 1 << 11 // 5.17
	IORING_FEAT_LINKED_FILE     = 1 << 12 // 5.17
	IORING_FEAT_REG_REG_RING    = 1 << 13 // 6.3
)

// mmap(2) offsets of the rings on the io_uring fd.
const (
	IORING_OFF_SQ_RING = 0
	IORING_OFF_CQ_RING = 0x8000000
	IORING_OFF_SQES    = 0x10000000
)

// io_uring_enter(2) flags.
const (
	IORING_ENTER_GETEVENTS = 1 << 0
	IORING_ENTER_EXT_ARG   = 1 << 3 // 5.11
)

// SQ ring flags (*sq_flags), set by the kernel.
const (
	IORING_SQ_NEED_WAKEUP = 1 << 0
	IORING_SQ_CQ_OVERFLOW = 1 << 1 // completions are waiting in the kernel's overflow list
	IORING_SQ_TASKRUN     = 1 << 2 // 5.19: task work is pending; enter with GETEVENTS to run it
)

// SQE flags (SQE.Flags).
const (
	IOSQE_FIXED_FILE       = 1 << 0 // SQE.Fd is an index into the registered file table
	IOSQE_IO_DRAIN         = 1 << 1
	IOSQE_IO_LINK          = 1 << 2 // the next SQE starts only after this one succeeds
	IOSQE_IO_HARDLINK      = 1 << 3 // like IO_LINK, but the chain survives this SQE failing
	IOSQE_ASYNC            = 1 << 4
	IOSQE_BUFFER_SELECT    = 1 << 5 // pick a buffer from provided-buffer group SQE.BufIndex
	IOSQE_CQE_SKIP_SUCCESS = 1 << 6 // 5.17: post no CQE if the request succeeds
)

// Opcodes (SQE.Opcode).
const (
	IORING_OP_NOP            = 0
	IORING_OP_READV          = 1
	IORING_OP_WRITEV         = 2
	IORING_OP_FSYNC          = 3
	IORING_OP_READ_FIXED     = 4
	IORING_OP_WRITE_FIXED    = 5
	IORING_OP_POLL_ADD       = 6
	IORING_OP_POLL_REMOVE    = 7
	IORING_OP_TIMEOUT        = 11 // 5.4
	IORING_OP_TIMEOUT_REMOVE = 12 // 5.5
	IORING_OP_ASYNC_CANCEL   = 14 // 5.5
	IORING_OP_LINK_TIMEOUT   = 15 // 5.5
	IORING_OP_FALLOCATE      = 17 // 5.6
	IORING_OP_READ           = 22 // 5.6
	IORING_OP_WRITE          = 23 // 5.6
	IORING_OP_MSG_RING       = 40 // 5.18
	IORING_OP_URING_CMD      = 46 // 5.19
	IORING_OP_READ_MULTISHOT = 49 // 6.7
)

// SQE.OpFlags values, per opcode.
const (
	IORING_URING_CMD_FIXED     = 1 << 0 // 6.0: URING_CMD uses registered buffer SQE.BufIndex
	IORING_URING_CMD_MULTISHOT = 1 << 1 // 6.18: multishot URING_CMD; requires IOSQE_BUFFER_SELECT

	IORING_FSYNC_DATASYNC = 1 << 0

	IORING_TIMEOUT_ABS           = 1 << 0
	IORING_TIMEOUT_BOOTTIME      = 1 << 2 // 5.15
	IORING_TIMEOUT_REALTIME      = 1 << 3 // 5.15
	IORING_TIMEOUT_ETIME_SUCCESS = 1 << 5 // 5.16
	IORING_TIMEOUT_MULTISHOT     = 1 << 6 // 6.4

	IORING_ASYNC_CANCEL_ALL = 1 << 0 // 5.19
	IORING_ASYNC_CANCEL_FD  = 1 << 1 // 5.19
	IORING_ASYNC_CANCEL_ANY = 1 << 2 // 5.19

	IORING_MSG_RING_CQE_SKIP = 1 << 0 // 6.3
)

// IORING_POLL_ADD_MULTI is stored in SQE.Len of a POLL_ADD to make it multishot (5.13).
const IORING_POLL_ADD_MULTI = 1 << 0

// IORING_MSG_DATA is the MSG_RING command (in SQE.Addr) that posts a CQE to another ring.
const IORING_MSG_DATA = 0

// CQE flags (CQE.Flags).
const (
	IORING_CQE_F_BUFFER     = 1 << 0 // the upper 16 bits hold the provided buffer ID
	IORING_CQE_F_MORE       = 1 << 1 // the multishot request will post more CQEs
	IORING_CQE_BUFFER_SHIFT = 16
)

// io_uring_register(2) opcodes.
const (
	IORING_REGISTER_BUFFERS        = 0
	IORING_UNREGISTER_BUFFERS      = 1
	IORING_REGISTER_FILES          = 2
	IORING_UNREGISTER_FILES        = 3
	IORING_REGISTER_FILES_UPDATE   = 6  // 5.5
	IORING_REGISTER_PROBE          = 8  // 5.6
	IORING_REGISTER_ENABLE_RINGS   = 12 // 5.10
	IORING_REGISTER_FILES2         = 13 // 5.13
	IORING_REGISTER_BUFFERS2       = 15 // 5.13
	IORING_REGISTER_BUFFERS_UPDATE = 16 // 5.13
	IORING_REGISTER_PBUF_RING      = 22 // 5.19
	IORING_UNREGISTER_PBUF_RING    = 23 // 5.19
)

// IORING_RSRC_REGISTER_SPARSE registers an all-empty file or buffer table (5.19).
const IORING_RSRC_REGISTER_SPARSE = 1 << 0

// IO_URING_OP_SUPPORTED marks a supported opcode in an IORING_REGISTER_PROBE reply.
const IO_URING_OP_SUPPORTED = 1 << 0

// SQE is a 64-byte submission queue entry (struct io_uring_sqe). Field names
// follow the first member of each kernel union; the comments list the others.
type SQE struct {
	Opcode      uint8
	Flags       uint8 // IOSQE_*
	Ioprio      uint16
	Fd          int32  // file descriptor, or registered-file index with IOSQE_FIXED_FILE
	Off         uint64 // off / addr2; URING_CMD keeps cmd_op + __pad1 here (see SetCmdOp)
	Addr        uint64 // addr / splice_off_in
	Len         uint32
	OpFlags     uint32 // rw/fsync/poll32/timeout/cancel/msg_ring/uring_cmd flags
	UserData    uint64
	BufIndex    uint16 // buf_index (registered buffer) or buf_group (IOSQE_BUFFER_SELECT)
	Personality uint16
	SpliceFdIn  int32  // splice_fd_in / file_index
	Addr3       uint64 // addr3; URING_CMD's command area starts here
	Pad2        uint64
}

// SQE128 is a 128-byte submission queue entry (IORING_SETUP_SQE128). Its
// URING_CMD command area is 80 bytes, starting at byte 48.
type SQE128 struct {
	SQE
	Ext [64]byte
}

// CQE is a 16-byte completion queue entry (struct io_uring_cqe).
type CQE struct {
	UserData uint64
	Res      int32
	Flags    uint32
}

// CQE32 is a 32-byte completion queue entry (IORING_SETUP_CQE32).
type CQE32 struct {
	CQE
	BigCQE [2]uint64
}

// Timespec is struct __kernel_timespec (64-bit fields on every architecture).
type Timespec struct {
	Sec  int64
	Nsec int64
}

type sqringOffsets struct {
	head        uint32
	tail        uint32
	ringMask    uint32
	ringEntries uint32
	flags       uint32
	dropped     uint32
	array       uint32
	resv1       uint32
	userAddr    uint64
}

type cqringOffsets struct {
	head        uint32
	tail        uint32
	ringMask    uint32
	ringEntries uint32
	overflow    uint32
	cqes        uint32
	flags       uint32
	resv1       uint32
	userAddr    uint64
}

// ioUringParams is struct io_uring_params.
type ioUringParams struct {
	sqEntries    uint32
	cqEntries    uint32
	flags        uint32
	sqThreadCPU  uint32
	sqThreadIdle uint32
	features     uint32
	wqFd         uint32
	resv         [3]uint32
	sqOff        sqringOffsets
	cqOff        cqringOffsets
}

// getEventsArg is struct io_uring_getevents_arg (IORING_ENTER_EXT_ARG).
type getEventsArg struct {
	sigmask     uint64
	sigmaskSz   uint32
	minWaitUsec uint32 // pad before 6.12
	ts          uint64
}

// rsrcRegister is struct io_uring_rsrc_register (FILES2/BUFFERS2).
type rsrcRegister struct {
	nr    uint32
	flags uint32
	resv2 uint64
	data  uint64
	tags  uint64
}

// rsrcUpdate is struct io_uring_rsrc_update (FILES_UPDATE).
type rsrcUpdate struct {
	offset uint32
	resv   uint32
	data   uint64
}

// rsrcUpdate2 is struct io_uring_rsrc_update2 (BUFFERS_UPDATE).
type rsrcUpdate2 struct {
	offset uint32
	resv   uint32
	data   uint64
	tags   uint64
	nr     uint32
	resv2  uint32
}

// bufReg is struct io_uring_buf_reg (PBUF_RING). minLeft is resv on kernels
// before 7.0; it must be zero there.
type bufReg struct {
	ringAddr    uint64
	ringEntries uint32
	bgid        uint16
	flags       uint16
	minLeft     uint32
	resv        [5]uint32
}

// bufRingEntry is struct io_uring_buf, one slot of a provided buffer ring.
// The ring's tail overlays slot 0's resv field.
type bufRingEntry struct {
	addr uint64
	len  uint32
	bid  uint16
	resv uint16
}

// probeHeader and probeOp are struct io_uring_probe and io_uring_probe_op.
type probeHeader struct {
	lastOp uint8
	opsLen uint8
	resv   uint16
	resv2  [3]uint32
}

type probeOp struct {
	op    uint8
	resv  uint8
	flags uint16
	resv2 uint32
}

// Compile-time size checks against the kernel structures.
var (
	_ [64]byte  = [unsafe.Sizeof(SQE{})]byte{}
	_ [128]byte = [unsafe.Sizeof(SQE128{})]byte{}
	_ [16]byte  = [unsafe.Sizeof(CQE{})]byte{}
	_ [32]byte  = [unsafe.Sizeof(CQE32{})]byte{}
	_ [16]byte  = [unsafe.Sizeof(Timespec{})]byte{}
	_ [120]byte = [unsafe.Sizeof(ioUringParams{})]byte{}
	_ [24]byte  = [unsafe.Sizeof(getEventsArg{})]byte{}
	_ [32]byte  = [unsafe.Sizeof(rsrcRegister{})]byte{}
	_ [16]byte  = [unsafe.Sizeof(rsrcUpdate{})]byte{}
	_ [32]byte  = [unsafe.Sizeof(rsrcUpdate2{})]byte{}
	_ [40]byte  = [unsafe.Sizeof(bufReg{})]byte{}
	_ [16]byte  = [unsafe.Sizeof(bufRingEntry{})]byte{}
	_ [16]byte  = [unsafe.Sizeof(probeHeader{})]byte{}
	_ [8]byte   = [unsafe.Sizeof(probeOp{})]byte{}
)

// SetCmdOp stores a URING_CMD cmd_op in bytes 8..11 and zeroes __pad1 (bytes
// 12..15), which the kernel rejects with EINVAL if nonzero.
func (s *SQE) SetCmdOp(op uint32) {
	p := unsafe.Pointer(&s.Off)
	*(*uint32)(p) = op
	*(*uint32)(unsafe.Add(p, 4)) = 0
}

// CmdOp returns the URING_CMD cmd_op stored by SetCmdOp.
func (s *SQE) CmdOp() uint32 {
	return *(*uint32)(unsafe.Pointer(&s.Off))
}

// Cmd returns the 16-byte URING_CMD command area of a 64-byte SQE (bytes 48..63).
func (s *SQE) Cmd() *[16]byte {
	return (*[16]byte)(unsafe.Pointer(&s.Addr3))
}

// Cmd returns the 80-byte URING_CMD command area of a 128-byte SQE (bytes 48..127).
func (s *SQE128) Cmd() *[80]byte {
	return (*[80]byte)(unsafe.Pointer(&s.SQE.Addr3))
}

// SetBufferSelect makes the request take its buffer from provided-buffer group
// group (IOSQE_BUFFER_SELECT); the CQE reports which one (CQE.BufferID).
func (s *SQE) SetBufferSelect(group uint16) {
	s.Flags |= IOSQE_BUFFER_SELECT
	s.BufIndex = group
}

// BufferID returns the provided buffer the kernel consumed for this
// completion, and whether there is one (IORING_CQE_F_BUFFER).
func (c *CQE) BufferID() (uint16, bool) {
	return uint16(c.Flags >> IORING_CQE_BUFFER_SHIFT), c.Flags&IORING_CQE_F_BUFFER != 0
}

// More reports whether a multishot request will post further CQEs (IORING_CQE_F_MORE).
func (c *CQE) More() bool {
	return c.Flags&IORING_CQE_F_MORE != 0
}
