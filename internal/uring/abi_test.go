package uring

import (
	"errors"
	"fmt"
	"testing"
	"unsafe"
)

// sqe128 and cqe32 are laid over memory the KERNEL reads and writes. A wrong
// size or field offset does not fail loudly -- it silently corrupts a
// submission/completion queue entry and the kernel acts on garbage. Every
// offset below was derived by hand from the kernel UAPI
// (include/uapi/linux/io_uring.h) and from the sizes of the preceding fields.
// Nothing in this file records "what the code currently does"; it records what
// the kernel ABI requires.

func TestSqe128SizeIs128(t *testing.T) {
	// The kernel requires exactly 128-byte SQEs when IORING_SETUP_SQE128 is
	// used (64-byte standard SQE plus the 64-byte big-SQE extension).
	if got := unsafe.Sizeof(sqe128{}); got != 128 {
		t.Errorf("sizeof(sqe128) = %d, want 128", got)
	}
}

func TestSqe128OffsetsMatchKernelABI(t *testing.T) {
	var s sqe128
	tests := []struct {
		name string
		got  uintptr
		want uintptr
	}{
		{"opcode", unsafe.Offsetof(s.opcode), 0},            // kernel: __u8 opcode
		{"flags", unsafe.Offsetof(s.flags), 1},              // kernel: __u8 flags
		{"ioprio", unsafe.Offsetof(s.ioprio), 2},            // kernel: __u16 ioprio
		{"fd", unsafe.Offsetof(s.fd), 4},                    // kernel: __s32 fd
		{"union0", unsafe.Offsetof(s.union0), 8},            // kernel: struct {__u32 cmd_op; __u32 __pad1;}
		{"addr", unsafe.Offsetof(s.addr), 16},               // kernel: __u64 addr
		{"len", unsafe.Offsetof(s.len), 24},                 // kernel: __u32 len
		{"opcodeFlags", unsafe.Offsetof(s.opcodeFlags), 28}, // kernel: uring_cmd_flags (__u32)
		{"userData", unsafe.Offsetof(s.userData), 32},       // kernel: __u64 user_data
		{"bufIndex", unsafe.Offsetof(s.bufIndex), 40},       // kernel: __u16 buf_index
		{"personality", unsafe.Offsetof(s.personality), 42}, // kernel: __u16 personality
		{"spliceFdIn", unsafe.Offsetof(s.spliceFdIn), 44},   // kernel: __s32 splice_fd_in
		{"cmd", unsafe.Offsetof(s.cmd), 48},                 // kernel: __u8 cmd[] (80 bytes with SQE128)
	}
	for _, tt := range tests {
		if tt.got != tt.want {
			t.Errorf("offsetof(sqe128.%s) = %d, want %d", tt.name, tt.got, tt.want)
		}
	}
}

// Every sqe128 offset above is naturally aligned (u8/u16/u32/u64 fields land on
// their own alignment after the previous field, no implicit padding), so the
// struct is 13 contiguous fields with no interior hole. Verifying sum-of-sizes
// == sizeof catches a widened field or a field that would otherwise be added
// with insertion padding.
func TestSqe128NoUnexpectedPadding(t *testing.T) {
	var s sqe128
	sum := uintptr(unsafe.Sizeof(s.opcode)) +
		unsafe.Sizeof(s.flags) +
		unsafe.Sizeof(s.ioprio) +
		unsafe.Sizeof(s.fd) +
		unsafe.Sizeof(s.union0) +
		unsafe.Sizeof(s.addr) +
		unsafe.Sizeof(s.len) +
		unsafe.Sizeof(s.opcodeFlags) +
		unsafe.Sizeof(s.userData) +
		unsafe.Sizeof(s.bufIndex) +
		unsafe.Sizeof(s.personality) +
		unsafe.Sizeof(s.spliceFdIn) +
		unsafe.Sizeof(s.cmd)
	if sum != unsafe.Sizeof(s) {
		t.Errorf("sum of sqe128 field sizes = %d, sizeof = %d (unexpected padding)", sum, unsafe.Sizeof(s))
	}
}

// The struct's natural alignment is 8 (the uint64 fields), and 128 is a
// multiple of 8, so there is no silent tail padding either.
func TestSqe128NoTailPadding(t *testing.T) {
	if unsafe.Sizeof(sqe128{})%unsafe.Alignof(sqe128{}) != 0 {
		t.Errorf("sqe128 size %d not a multiple of its alignment %d (tail padding)",
			unsafe.Sizeof(sqe128{}), unsafe.Alignof(sqe128{}))
	}
}

func TestCqe32SizeIs32(t *testing.T) {
	// The kernel requires exactly 32-byte CQEs when IORING_SETUP_CQE32 is
	// used (16-byte standard CQE plus the 16-byte big_cqe[] extension).
	if got := unsafe.Sizeof(cqe32{}); got != 32 {
		t.Errorf("sizeof(cqe32) = %d, want 32", got)
	}
}

func TestCqe32OffsetsMatchKernelABI(t *testing.T) {
	var c cqe32
	tests := []struct {
		name string
		got  uintptr
		want uintptr
	}{
		{"userData", unsafe.Offsetof(c.userData), 0}, // kernel: __u64 user_data
		{"res", unsafe.Offsetof(c.res), 8},           // kernel: __s32 res
		{"flags", unsafe.Offsetof(c.flags), 12},      // kernel: __u32 flags
		{"bigCQE", unsafe.Offsetof(c.bigCQE), 16},    // kernel: __u64 big_cqe[] (16 bytes for CQE32)
	}
	for _, tt := range tests {
		if tt.got != tt.want {
			t.Errorf("offsetof(cqe32.%s) = %d, want %d", tt.name, tt.got, tt.want)
		}
	}
}

func TestCqe32NoUnexpectedPadding(t *testing.T) {
	var c cqe32
	sum := uintptr(unsafe.Sizeof(c.userData)) +
		unsafe.Sizeof(c.res) +
		unsafe.Sizeof(c.flags) +
		unsafe.Sizeof(c.bigCQE)
	if sum != unsafe.Sizeof(c) {
		t.Errorf("sum of cqe32 field sizes = %d, sizeof = %d (unexpected padding)", sum, unsafe.Sizeof(c))
	}
	// Alignment is 8 and the size 32 is a multiple of 8: no tail padding.
	if unsafe.Sizeof(cqe32{})%unsafe.Alignof(cqe32{}) != 0 {
		t.Errorf("cqe32 size %d not a multiple of its alignment %d (tail padding)",
			unsafe.Sizeof(cqe32{}), unsafe.Alignof(cqe32{}))
	}
}

// setCmdOp must write exactly the cmd_op cells (bytes 8..11) and leave every
// other byte of the SQE untouched -- in particular __pad1 (bytes 12..15) and
// the whole cmd area. A mis-sized write that spills into a neighbouring field
// is the exact bug a naive round-trip test would miss, so we check raw bytes.
func TestSetCmdOpWritesOnlyCmdOp(t *testing.T) {
	values := []uint32{0, 1, 0xFFFFFFFF, 0xA5A5A5A5}
	for _, v := range values {
		var sqe sqe128
		sqe.setCmdOp(v)

		// The target field must hold exactly v.
		if got := *(*uint32)(unsafe.Pointer(&sqe.union0[0])); got != v {
			t.Errorf("setCmdOp(%#x): cmd field = %#x, want %#x", v, got, v)
		}

		b := unsafe.Slice((*byte)(unsafe.Pointer(&sqe)), unsafe.Sizeof(sqe))

		// Bytes 8..11 hold v little-endian.
		for i := 0; i < 4; i++ {
			want := byte(v >> (8 * i))
			if b[8+i] != want {
				t.Errorf("setCmdOp(%#x): byte %d = %#x, want %#x", v, 8+i, b[8+i], want)
			}
		}

		// Every other byte (0..7 and 12..127) must still be zero.
		for i := range b {
			if i >= 8 && i < 12 {
				continue
			}
			if b[i] != 0 {
				t.Errorf("setCmdOp(%#x): byte %d = %#x, want 0", v, i, b[i])
			}
		}
	}
}

// kernelUringCmdOpcode feeds the kernel's opcode field. It must be
// IORING_OP_URING_CMD = 46 on Linux (io_uring.h); a change is an ABI break.
func TestKernelUringCmdOpcode(t *testing.T) {
	if got := kernelUringCmdOpcode(); got != 46 {
		t.Errorf("kernelUringCmdOpcode() = %d, want 46 (IORING_OP_URING_CMD)", got)
	}
}

// Callers match ErrRingFull through errors.Is, including across a %w wrap.
func TestErrRingFullIdentity(t *testing.T) {
	if !errors.Is(ErrRingFull, ErrRingFull) {
		t.Error("errors.Is(ErrRingFull, ErrRingFull) = false")
	}
	wrapped := fmt.Errorf("prepare io cmd: %w", ErrRingFull)
	if !errors.Is(wrapped, ErrRingFull) {
		t.Errorf("errors.Is(%q, ErrRingFull) = false", wrapped)
	}
}

// The const block in minimal.go mirrors io_uring.h. Values are compared at
// runtime (assigned to a variable first) so a drift is caught as a test
// failure rather than constant-folded away.
func TestSetupAndMmapConstantsMatchKernel(t *testing.T) {
	// kernel: IORING_SETUP_SQE128 (1U << 10)
	got := IORING_SETUP_SQE128
	if got != 1024 {
		t.Errorf("IORING_SETUP_SQE128 = %d, want 1024", got)
	}
	// kernel: IORING_SETUP_CQE32 (1U << 11)
	got = IORING_SETUP_CQE32
	if got != 2048 {
		t.Errorf("IORING_SETUP_CQE32 = %d, want 2048", got)
	}
	// kernel: IORING_OFF_SQ_RING 0ULL
	got = IORING_OFF_SQ_RING
	if got != 0 {
		t.Errorf("IORING_OFF_SQ_RING = %#x, want 0x0", got)
	}
	// kernel: IORING_OFF_CQ_RING 0x8000000ULL
	got = IORING_OFF_CQ_RING
	if got != 0x08000000 {
		t.Errorf("IORING_OFF_CQ_RING = %#x, want 0x8000000", got)
	}
	// kernel: IORING_OFF_SQES 0x10000000ULL
	got = IORING_OFF_SQES
	if got != 0x10000000 {
		t.Errorf("IORING_OFF_SQES = %#x, want 0x10000000", got)
	}
}

// Zero Config and Features values must be usable struct values. This cannot
// call NewRing (needs a live io_uring + ublk control device, absent here).
func TestZeroValuesAreSafe(t *testing.T) {
	var c Config
	if c.Entries != 0 || c.FD != 0 || c.Flags != 0 {
		t.Errorf("zero Config = %+v, want all zero fields", c)
	}
	var f Features
	if f.SQE128 || f.CQE32 || f.UringCmd || f.SQPOLL {
		t.Errorf("zero Features = %+v, want all false fields", f)
	}
}
