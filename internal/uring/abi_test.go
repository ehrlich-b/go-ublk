package uring

import (
	"encoding/binary"
	"errors"
	"fmt"
	"testing"
	"unsafe"

	"golang.org/x/sys/unix"
)

// SQE and CQE are laid over memory the KERNEL reads and writes. A wrong size
// or field offset does not fail loudly -- it silently corrupts a submission or
// completion queue entry and the kernel acts on garbage. Every offset below
// was derived by hand from the kernel UAPI (include/uapi/linux/io_uring.h) and
// from the sizes of the preceding fields. Nothing in this file records "what
// the code currently does"; it records what the kernel ABI requires.

func TestSQEOffsetsMatchKernelABI(t *testing.T) {
	var s SQE
	tests := []struct {
		name string
		got  uintptr
		want uintptr
	}{
		{"Opcode", unsafe.Offsetof(s.Opcode), 0},            // __u8 opcode
		{"Flags", unsafe.Offsetof(s.Flags), 1},              // __u8 flags
		{"Ioprio", unsafe.Offsetof(s.Ioprio), 2},            // __u16 ioprio
		{"Fd", unsafe.Offsetof(s.Fd), 4},                    // __s32 fd
		{"Off", unsafe.Offsetof(s.Off), 8},                  // union { off; addr2; {cmd_op; __pad1} }
		{"Addr", unsafe.Offsetof(s.Addr), 16},               // union { addr; splice_off_in }
		{"Len", unsafe.Offsetof(s.Len), 24},                 // __u32 len
		{"OpFlags", unsafe.Offsetof(s.OpFlags), 28},         // union { rw_flags; ...; uring_cmd_flags }
		{"UserData", unsafe.Offsetof(s.UserData), 32},       // __u64 user_data
		{"BufIndex", unsafe.Offsetof(s.BufIndex), 40},       // packed union { buf_index; buf_group }
		{"Personality", unsafe.Offsetof(s.Personality), 42}, // __u16 personality
		{"SpliceFdIn", unsafe.Offsetof(s.SpliceFdIn), 44},   // union { splice_fd_in; file_index }
		{"Addr3", unsafe.Offsetof(s.Addr3), 48},             // addr3; also cmd[] start
		{"Pad2", unsafe.Offsetof(s.Pad2), 56},               // __pad2[1]
	}
	for _, tt := range tests {
		if tt.got != tt.want {
			t.Errorf("offsetof(SQE.%s) = %d, want %d", tt.name, tt.got, tt.want)
		}
	}
	if got := unsafe.Sizeof(SQE{}); got != 64 {
		t.Errorf("sizeof(SQE) = %d, want 64", got)
	}
	// IORING_SETUP_SQE128 doubles the slot; cmd[] then spans bytes 48..127.
	if got := unsafe.Sizeof(SQE128{}); got != 128 {
		t.Errorf("sizeof(SQE128) = %d, want 128", got)
	}
}

func TestCQEOffsetsMatchKernelABI(t *testing.T) {
	var c CQE32
	tests := []struct {
		name string
		got  uintptr
		want uintptr
	}{
		{"UserData", unsafe.Offsetof(c.UserData), 0}, // __u64 user_data
		{"Res", unsafe.Offsetof(c.Res), 8},           // __s32 res
		{"Flags", unsafe.Offsetof(c.Flags), 12},      // __u32 flags
		{"BigCQE", unsafe.Offsetof(c.BigCQE), 16},    // __u64 big_cqe[] (16 bytes with CQE32)
	}
	for _, tt := range tests {
		if tt.got != tt.want {
			t.Errorf("offsetof(CQE32.%s) = %d, want %d", tt.name, tt.got, tt.want)
		}
	}
	if unsafe.Sizeof(CQE{}) != 16 || unsafe.Sizeof(CQE32{}) != 32 {
		t.Errorf("sizeof(CQE)=%d sizeof(CQE32)=%d, want 16 and 32", unsafe.Sizeof(CQE{}), unsafe.Sizeof(CQE32{}))
	}
}

// The register/enter argument structs, by kernel offset.
func TestArgStructOffsetsMatchKernelABI(t *testing.T) {
	var p ioUringParams
	var g getEventsArg
	var rr rsrcRegister
	var u2 rsrcUpdate2
	var br bufReg
	var e bufRingEntry
	tests := []struct {
		name string
		got  uintptr
		want uintptr
	}{
		{"params.features", unsafe.Offsetof(p.features), 20},
		{"params.sq_off", unsafe.Offsetof(p.sqOff), 40},
		{"params.cq_off", unsafe.Offsetof(p.cqOff), 80},
		{"getevents.ts", unsafe.Offsetof(g.ts), 16},
		{"rsrc_register.data", unsafe.Offsetof(rr.data), 16},
		{"rsrc_register.tags", unsafe.Offsetof(rr.tags), 24},
		{"rsrc_update2.nr", unsafe.Offsetof(u2.nr), 24},
		{"buf_reg.bgid", unsafe.Offsetof(br.bgid), 12},
		{"buf_reg.resv", unsafe.Offsetof(br.resv), 20},
		{"io_uring_buf.bid", unsafe.Offsetof(e.bid), 12},
		{"io_uring_buf.resv (ring tail)", unsafe.Offsetof(e.resv), 14},
	}
	for _, tt := range tests {
		if tt.got != tt.want {
			t.Errorf("offsetof(%s) = %d, want %d", tt.name, tt.got, tt.want)
		}
	}
}

// SetCmdOp must write exactly the cmd_op cells (bytes 8..11), zero __pad1
// (bytes 12..15, which the kernel rejects if nonzero) and leave every other
// byte untouched. A mis-sized write that spills into a neighbouring field is
// the exact bug a naive round-trip test would miss, so we check raw bytes.
func TestSetCmdOpWritesOnlyCmdOp(t *testing.T) {
	for _, v := range []uint32{0, 1, 0xFFFFFFFF, 0xA5A5A5A5} {
		var sqe SQE128
		b := unsafe.Slice((*byte)(unsafe.Pointer(&sqe)), unsafe.Sizeof(sqe))
		b[12], b[13], b[14], b[15] = 0xEE, 0xEE, 0xEE, 0xEE // stale __pad1
		sqe.SetCmdOp(v)
		if got := sqe.CmdOp(); got != v {
			t.Errorf("SetCmdOp(%#x): CmdOp() = %#x", v, got)
		}
		for i := 0; i < 4; i++ {
			if want := byte(v >> (8 * i)); b[8+i] != want {
				t.Errorf("SetCmdOp(%#x): byte %d = %#x, want %#x", v, 8+i, b[8+i], want)
			}
		}
		for i := range b {
			if (i < 8 || i >= 12) && b[i] != 0 {
				t.Errorf("SetCmdOp(%#x): byte %d = %#x, want 0", v, i, b[i])
			}
		}
	}
}

func TestOpcodeAndFlagConstantsMatchKernel(t *testing.T) {
	tests := []struct {
		name string
		got  int
		want int
	}{
		{"IORING_OP_NOP", IORING_OP_NOP, 0},
		{"IORING_OP_READV", IORING_OP_READV, 1},
		{"IORING_OP_WRITEV", IORING_OP_WRITEV, 2},
		{"IORING_OP_FSYNC", IORING_OP_FSYNC, 3},
		{"IORING_OP_READ_FIXED", IORING_OP_READ_FIXED, 4},
		{"IORING_OP_WRITE_FIXED", IORING_OP_WRITE_FIXED, 5},
		{"IORING_OP_POLL_ADD", IORING_OP_POLL_ADD, 6},
		{"IORING_OP_TIMEOUT", IORING_OP_TIMEOUT, 11},
		{"IORING_OP_ASYNC_CANCEL", IORING_OP_ASYNC_CANCEL, 14},
		{"IORING_OP_LINK_TIMEOUT", IORING_OP_LINK_TIMEOUT, 15},
		{"IORING_OP_FALLOCATE", IORING_OP_FALLOCATE, 17},
		{"IORING_OP_READ", IORING_OP_READ, 22},
		{"IORING_OP_WRITE", IORING_OP_WRITE, 23},
		{"IORING_OP_MSG_RING", IORING_OP_MSG_RING, 40},
		{"IORING_OP_URING_CMD", IORING_OP_URING_CMD, 46},
		{"IORING_SETUP_SQE128", IORING_SETUP_SQE128, 1024},
		{"IORING_SETUP_CQE32", IORING_SETUP_CQE32, 2048},
		{"IORING_SETUP_SINGLE_ISSUER", IORING_SETUP_SINGLE_ISSUER, 4096},
		{"IORING_SETUP_DEFER_TASKRUN", IORING_SETUP_DEFER_TASKRUN, 8192},
		{"IORING_OFF_SQ_RING", IORING_OFF_SQ_RING, 0},
		{"IORING_OFF_CQ_RING", IORING_OFF_CQ_RING, 0x8000000},
		{"IORING_OFF_SQES", IORING_OFF_SQES, 0x10000000},
		{"IORING_REGISTER_BUFFERS2", IORING_REGISTER_BUFFERS2, 15},
		{"IORING_REGISTER_PBUF_RING", IORING_REGISTER_PBUF_RING, 22},
		{"IOSQE_CQE_SKIP_SUCCESS", IOSQE_CQE_SKIP_SUCCESS, 64},
		{"IORING_URING_CMD_MULTISHOT", IORING_URING_CMD_MULTISHOT, 2},
	}
	for _, tt := range tests {
		if tt.got != tt.want {
			t.Errorf("%s = %#x, want %#x", tt.name, tt.got, tt.want)
		}
	}
}

// Callers match ErrRingFull through errors.Is, including across a %w wrap.
func TestErrRingFullIdentity(t *testing.T) {
	wrapped := fmt.Errorf("prepare io cmd: %w", ErrRingFull)
	if !errors.Is(wrapped, ErrRingFull) {
		t.Errorf("errors.Is(%q, ErrRingFull) = false", wrapped)
	}
}

// Zero Config and Features values must be usable struct values.
func TestZeroValuesAreSafe(t *testing.T) {
	var c Config
	if c.Entries != 0 || c.FD != 0 || c.Flags != 0 || c.CtrlTimeout != 0 {
		t.Errorf("zero Config = %+v, want all zero fields", c)
	}
	var f Features
	if f.SQE128 || f.CQE32 || f.UringCmd || f.SQPOLL {
		t.Errorf("zero Features = %+v, want all false fields", f)
	}
}

// field is one kernel SQE field: offset, width in bytes, value.
type field struct {
	off, size int
	val       uint64
}

// encodeSQE builds the expected SQE bytes from kernel offsets, independently
// of the SQE struct.
func encodeSQE(size int, fields ...field) []byte {
	b := make([]byte, size)
	for _, f := range fields {
		switch f.size {
		case 1:
			b[f.off] = byte(f.val)
		case 2:
			binary.LittleEndian.PutUint16(b[f.off:], uint16(f.val))
		case 4:
			binary.LittleEndian.PutUint32(b[f.off:], uint32(f.val))
		case 8:
			binary.LittleEndian.PutUint64(b[f.off:], f.val)
		}
	}
	return b
}

func sqeBytes(p unsafe.Pointer, size int) []byte {
	return append([]byte(nil), unsafe.Slice((*byte)(p), size)...)
}

// Kernel offsets used by the prep layout table.
const (
	offOpcode   = 0
	offFlags    = 1
	offFd       = 4
	offOff      = 8
	offAddr     = 16
	offLen      = 24
	offOpFlags  = 28
	offUserData = 32
	offBufIndex = 40
	offCmd      = 48
)

func TestPrepHelperLayouts(t *testing.T) {
	buf := make([]byte, 4096)
	bufAddr := uint64(uintptr(unsafe.Pointer(&buf[0])))
	iovecs := make([]unix.Iovec, 2)
	iovAddr := uint64(uintptr(unsafe.Pointer(&iovecs[0])))
	ts := &Timespec{Nsec: 1000}
	tsAddr := uint64(uintptr(unsafe.Pointer(ts)))
	minus1 := uint64(0xFFFFFFFF) // fd -1 as a __s32
	tests := []struct {
		name string
		prep func(*SQE)
		want []field
	}{
		{"Nop", PrepNop, nil},
		{"Read", func(s *SQE) { PrepRead(s, 7, buf, 4096) }, []field{
			{offOpcode, 1, IORING_OP_READ}, {offFd, 4, 7}, {offOff, 8, 4096},
			{offAddr, 8, bufAddr}, {offLen, 4, 4096}}},
		{"ReadSelect", func(s *SQE) { PrepReadSelect(s, 3, 512, 0, 9) }, []field{
			{offOpcode, 1, IORING_OP_READ}, {offFlags, 1, IOSQE_BUFFER_SELECT}, {offFd, 4, 3},
			{offLen, 4, 512}, {offBufIndex, 2, 9}}},
		{"Write", func(s *SQE) { PrepWrite(s, 7, buf[:100], 1) }, []field{
			{offOpcode, 1, IORING_OP_WRITE}, {offFd, 4, 7}, {offOff, 8, 1},
			{offAddr, 8, bufAddr}, {offLen, 4, 100}}},
		{"ReadFixed", func(s *SQE) { PrepReadFixed(s, 5, 0x1000, 4096, 8192, 3) }, []field{
			{offOpcode, 1, IORING_OP_READ_FIXED}, {offFd, 4, 5}, {offOff, 8, 8192},
			{offAddr, 8, 0x1000}, {offLen, 4, 4096}, {offBufIndex, 2, 3}}},
		{"WriteFixed", func(s *SQE) { PrepWriteFixed(s, 5, 512, 1024, 0, 65535) }, []field{
			{offOpcode, 1, IORING_OP_WRITE_FIXED}, {offFd, 4, 5}, {offAddr, 8, 512},
			{offLen, 4, 1024}, {offBufIndex, 2, 65535}}},
		{"Readv", func(s *SQE) { PrepReadv(s, 8, iovecs, 77) }, []field{
			{offOpcode, 1, IORING_OP_READV}, {offFd, 4, 8}, {offOff, 8, 77},
			{offAddr, 8, iovAddr}, {offLen, 4, 2}}},
		{"Writev", func(s *SQE) { PrepWritev(s, 8, iovecs, 0) }, []field{
			{offOpcode, 1, IORING_OP_WRITEV}, {offFd, 4, 8}, {offAddr, 8, iovAddr}, {offLen, 4, 2}}},
		{"Fsync", func(s *SQE) { PrepFsync(s, 4, IORING_FSYNC_DATASYNC) }, []field{
			{offOpcode, 1, IORING_OP_FSYNC}, {offFd, 4, 4}, {offOpFlags, 4, 1}}},
		// Kernel io_fallocate_prep: off = sqe->off, len = sqe->addr, mode = sqe->len.
		{"Fallocate", func(s *SQE) { PrepFallocate(s, 4, 3, 4096, 8192) }, []field{
			{offOpcode, 1, IORING_OP_FALLOCATE}, {offFd, 4, 4}, {offOff, 8, 4096},
			{offAddr, 8, 8192}, {offLen, 4, 3}}},
		{"PollAdd", func(s *SQE) { PrepPollAdd(s, 6, unix.POLLIN) }, []field{
			{offOpcode, 1, IORING_OP_POLL_ADD}, {offFd, 4, 6}, {offOpFlags, 4, unix.POLLIN}}},
		{"Cancel", func(s *SQE) { PrepCancel(s, 0xABC, IORING_ASYNC_CANCEL_ALL) }, []field{
			{offOpcode, 1, IORING_OP_ASYNC_CANCEL}, {offFd, 4, minus1}, {offAddr, 8, 0xABC},
			{offOpFlags, 4, IORING_ASYNC_CANCEL_ALL}}},
		{"Timeout", func(s *SQE) { PrepTimeout(s, ts, 2, IORING_TIMEOUT_ABS) }, []field{
			{offOpcode, 1, IORING_OP_TIMEOUT}, {offFd, 4, minus1}, {offOff, 8, 2},
			{offAddr, 8, tsAddr}, {offLen, 4, 1}, {offOpFlags, 4, IORING_TIMEOUT_ABS}}},
		{"LinkTimeout", func(s *SQE) { PrepLinkTimeout(s, ts, 0) }, []field{
			{offOpcode, 1, IORING_OP_LINK_TIMEOUT}, {offFd, 4, minus1}, {offAddr, 8, tsAddr}, {offLen, 4, 1}}},
		// Kernel io_msg_ring_prep: user_data = sqe->off, len = sqe->len, cmd = sqe->addr.
		{"MsgRing", func(s *SQE) { PrepMsgRing(s, 9, 77, 0x55, IORING_MSG_RING_CQE_SKIP) }, []field{
			{offOpcode, 1, IORING_OP_MSG_RING}, {offFd, 4, 9}, {offOff, 8, 0x55},
			{offAddr, 8, IORING_MSG_DATA}, {offLen, 4, 77}, {offOpFlags, 4, IORING_MSG_RING_CQE_SKIP}}},
		{"UringCmd", func(s *SQE) {
			PrepUringCmd(s, 2, 0xC0107520, []byte{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16})
		}, []field{
			{offOpcode, 1, IORING_OP_URING_CMD}, {offFd, 4, 2}, {offOff, 4, 0xC0107520},
			{offCmd, 8, 0x0807060504030201}, {offCmd + 8, 8, 0x100F0E0D0C0B0A09}}},
		{"BufferSelect", func(s *SQE) { s.SetBufferSelect(0x1234) }, []field{
			{offFlags, 1, IOSQE_BUFFER_SELECT}, {offBufIndex, 2, 0x1234}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var sqe SQE
			tt.prep(&sqe)
			got := sqeBytes(unsafe.Pointer(&sqe), 64)
			want := encodeSQE(64, tt.want...)
			for i := range want {
				if got[i] != want[i] {
					t.Errorf("byte %d = %#x, want %#x\n got %x\nwant %x", i, got[i], want[i], got, want)
					return
				}
			}
		})
	}
}

// A 32-byte ublk control payload lands at byte 48 of a 128-byte SQE, and
// nothing past it is written.
func TestPrepUringCmd128Layout(t *testing.T) {
	payload := make([]byte, 32)
	for i := range payload {
		payload[i] = byte(0x40 + i)
	}
	var sqe SQE128
	PrepUringCmd128(&sqe, 3, 0xC0207504, payload)
	got := sqeBytes(unsafe.Pointer(&sqe), 128)
	want := encodeSQE(128, field{offOpcode, 1, IORING_OP_URING_CMD}, field{offFd, 4, 3},
		field{offOff, 4, 0xC0207504})
	copy(want[offCmd:], payload)
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("byte %d = %#x, want %#x", i, got[i], want[i])
		}
	}
}

func TestPrepUringCmdRejectsOversizePayload(t *testing.T) {
	expectPanic := func(name string, f func()) {
		t.Helper()
		defer func() {
			if recover() == nil {
				t.Errorf("%s: no panic for oversize payload", name)
			}
		}()
		f()
	}
	expectPanic("PrepUringCmd", func() { PrepUringCmd(&SQE{}, 0, 0, make([]byte, 17)) })
	expectPanic("PrepUringCmd128", func() { PrepUringCmd128(&SQE128{}, 0, 0, make([]byte, 81)) })
}

func TestCQEAccessors(t *testing.T) {
	c := CQE{Flags: IORING_CQE_F_BUFFER | IORING_CQE_F_MORE | 0xBEEF<<IORING_CQE_BUFFER_SHIFT}
	if bid, ok := c.BufferID(); !ok || bid != 0xBEEF {
		t.Errorf("BufferID() = %#x, %v; want 0xbeef, true", bid, ok)
	}
	if !c.More() {
		t.Error("More() = false with IORING_CQE_F_MORE set")
	}
	if _, ok := (&CQE{Flags: 0xBEEF << IORING_CQE_BUFFER_SHIFT}).BufferID(); ok {
		t.Error("BufferID() reported a buffer without IORING_CQE_F_BUFFER")
	}
}
