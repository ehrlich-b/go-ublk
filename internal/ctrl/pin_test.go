package ctrl

import (
	"encoding/binary"
	"testing"
	"unsafe"

	"github.com/ehrlich-b/go-ublk/internal/uapi"
	"github.com/ehrlich-b/go-ublk/internal/uring"
)

// growStack forces the calling goroutine's stack to be copied to a bigger one,
// moving every stack-allocated variable of its callers.
//
//go:noinline
func growStack(depth int) byte {
	var pad [4096]byte
	pad[depth%len(pad)] = byte(depth)
	if depth == 0 {
		return pad[0]
	}
	return growStack(depth-1) + pad[depth%len(pad)]
}

// lateKernelWrite stands in for the driver's copy_to_user from an io-wq
// worker: it writes through the address carried in the command, after the
// submitting goroutine's stack has had a chance to move.
//
//go:nocheckptr
func lateKernelWrite(cmd *uapi.UblksrvCtrlCmd, payload []byte) {
	addr := *(*unsafe.Pointer)(unsafe.Pointer(&cmd.Addr))
	copy(unsafe.Slice((*byte)(addr), int(cmd.Len)), payload)
}

// TestControlBufferSurvivesStackMove is the regression test for Critical Bug
// #21's control side. GET_DEV_INFO's reply buffer used to be a non-escaping
// make([]byte, 80): stack-allocated, with its address passed to the kernel as
// an integer. When the stack grows between building the command and the
// kernel's write, the runtime copies the buffer elsewhere and the kernel's
// write lands in the freed old stack, so the reply reads back as zeros (or
// corrupts another goroutine). Control buffers now live in a Controller-owned
// mmap'd page that never moves.
func TestControlBufferSurvivesStackMove(t *testing.T) {
	reply := uapi.Marshal(&uapi.UblksrvCtrlDevInfo{NrHwQueues: 3, QueueDepth: 77, DevID: 9, Flags: 0x1234, UblksrvFlags: 0xfeed})
	ring := &controlTestRing{submit: func(_ uint32, cmd *uapi.UblksrvCtrlCmd) (uring.Result, error) {
		growStack(64) // ~256 KiB of frames: the stack is copied at least once
		lateKernelWrite(cmd, reply)
		return controlTestResult(0), nil
	}}
	c := newTestController(ring)
	for i := 0; i < 8; i++ {
		info, err := getDevInfoForPinTest(c, 9)
		if err != nil {
			t.Fatal(err)
		}
		if info.QueueDepth != 77 || info.UblksrvFlags != 0xfeed || binary.LittleEndian.Uint16(reply[2:4]) != 77 {
			t.Fatalf("iteration %d: reply lost after a stack move: %+v", i, info)
		}
	}
}
