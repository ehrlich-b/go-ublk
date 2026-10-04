package queue

import (
	"fmt"
	"sync"
	"sync/atomic"
	"syscall"
	"testing"
	"time"
	"unsafe"

	"golang.org/x/sys/unix"

	"github.com/ehrlich-b/go-ublk/internal/uapi"
	"github.com/ehrlich-b/go-ublk/internal/uring"
)

// fakeKernel stands in for ublk_drv behind the engine's ring interface. It
// enforces the driver's per-tag protocol (FETCH once, COMMIT_AND_FETCH only
// while the server owns the tag, NEED_GET_DATA only when asked), copies data
// the way copy mode does, and records every commit, so tests can drive the
// real engine through any interleaving and check what the kernel would see.
type fakeKernel struct {
	t     testing.TB
	depth int
	desc  []byte // descriptor array the engine reads
	bufs  []byte // the engine's data buffers (copy mode: the fake copies through them)
	ufile int    // user-copy mode: regular file standing in for /dev/ublkcN, or -1

	mu          sync.Mutex
	sq          []uring.SQE
	sqLen       int
	cq          []uring.CQE
	tags        []fkTag
	needGetData bool
	stopping    bool
	wakeFd      int
	wakeArmed   bool
	wakeUD      uint64
	closed      bool
	commits     []fkCommit
	violations  []string
	kick        chan struct{}
}

type fkTagState int

const (
	fkIdle    fkTagState = iota // never fetched
	fkWaiting                   // FETCH/COMMIT_AND_FETCH outstanding, no request yet
	fkOwned                     // request delivered to the server
	fkGetData                   // RES_NEED_GET_DATA delivered, waiting for NEED_GET_DATA
	fkAborted
)

type fkTag struct {
	state   fkTagState
	pending []fkReq // requests queued for this tag
	cur     fkReq
	addr    uint64 // buffer address the server last gave
	ud      uint64 // user_data of the outstanding fetch
}

type fkReq struct {
	op     uint8
	flags  uint32
	sector uint64
	nr     uint32
	data   []byte // payload for writes
	id     int
}

type fkCommit struct {
	tag    uint16
	id     int
	op     uint8
	result int32
	data   []byte // read payload as copied back
	addr   uint64
}

func newFakeKernel(t testing.TB, depth, bufSize int) *fakeKernel {
	return &fakeKernel{
		t:      t,
		depth:  depth,
		desc:   make([]byte, depth*24),
		bufs:   make([]byte, depth*bufSize),
		ufile:  -1,
		tags:   make([]fkTag, depth),
		wakeFd: -1,
		kick:   make(chan struct{}, 1),
	}
}

func (k *fakeKernel) violate(format string, args ...any) {
	k.violations = append(k.violations, fmt.Sprintf(format, args...))
}

// ring implementation

func (k *fakeKernel) GetSQE() *uring.SQE {
	k.mu.Lock()
	defer k.mu.Unlock()
	if k.sq == nil {
		k.sq = make([]uring.SQE, 4*k.depth+8)
	}
	if k.sqLen == len(k.sq) {
		return nil
	}
	k.sq[k.sqLen] = uring.SQE{}
	k.sqLen++
	return &k.sq[k.sqLen-1]
}

func (k *fakeKernel) Submit() (int, error) {
	k.mu.Lock()
	defer k.mu.Unlock()
	return k.consume(), nil
}

func (k *fakeKernel) SubmitAndWait(minComplete uint32, timeout time.Duration) (int, error) {
	k.mu.Lock()
	n := k.consume()
	k.mu.Unlock()
	deadline := time.Now().Add(timeout)
	for {
		k.mu.Lock()
		k.pollWake()
		ready := len(k.cq)
		k.mu.Unlock()
		if uint32(ready) >= minComplete {
			return n, nil
		}
		if time.Now().After(deadline) {
			return n, unix.ETIME
		}
		select {
		case <-k.kick:
		case <-time.After(200 * time.Microsecond):
		}
	}
}

func (k *fakeKernel) PeekCQE() *uring.CQE {
	k.mu.Lock()
	defer k.mu.Unlock()
	if len(k.cq) == 0 {
		return nil
	}
	c := k.cq[0]
	return &c
}

func (k *fakeKernel) CQAdvance(n uint32) {
	k.mu.Lock()
	defer k.mu.Unlock()
	k.cq = k.cq[n:]
}

func (k *fakeKernel) Close() error {
	k.mu.Lock()
	defer k.mu.Unlock()
	k.closed = true
	return nil
}

func (k *fakeKernel) post(ud uint64, res int32) {
	k.cq = append(k.cq, uring.CQE{UserData: ud, Res: res})
	select {
	case k.kick <- struct{}{}:
	default:
	}
}

// pollWake completes the armed eventfd read once the eventfd is readable.
func (k *fakeKernel) pollWake() {
	if !k.wakeArmed {
		return
	}
	fds := []unix.PollFd{{Fd: int32(k.wakeFd), Events: unix.POLLIN}}
	if n, _ := unix.Poll(fds, 0); n == 1 {
		var b [8]byte
		_, _ = unix.Read(k.wakeFd, b[:])
		k.wakeArmed = false
		k.post(k.wakeUD, 8)
	}
}

// consume processes queued SQEs as the driver's issue path would.
func (k *fakeKernel) consume() int {
	n := k.sqLen
	for i := 0; i < n; i++ {
		sqe := k.sq[i]
		switch sqe.Opcode {
		case uring.IORING_OP_READ:
			k.wakeFd, k.wakeArmed, k.wakeUD = int(sqe.Fd), true, sqe.UserData
		case uring.IORING_OP_URING_CMD:
			k.uringCmd(&sqe)
		default:
			k.violate("unexpected opcode %d", sqe.Opcode)
		}
	}
	k.sqLen = 0
	return n
}

func (k *fakeKernel) uringCmd(sqe *uring.SQE) {
	cmd := *(*uapi.UblksrvIOCmd)(unsafe.Pointer(sqe.Cmd()))
	nr := sqe.CmdOp() & 0xff
	if want := uapi.UblkIOCmd(nr); sqe.CmdOp() != want {
		k.violate("cmd op %#x is not the ioctl encoding %#x", sqe.CmdOp(), want)
	}
	tag := int(cmd.Tag)
	if tag >= k.depth {
		k.post(sqe.UserData, -int32(syscall.EINVAL))
		return
	}
	ts := &k.tags[tag]
	switch nr {
	case uapi.UBLK_IO_FETCH_REQ:
		if ts.state != fkIdle {
			k.violate("tag %d fetched twice", tag)
			k.post(sqe.UserData, -int32(syscall.EINVAL))
			return
		}
		ts.state, ts.addr = fkWaiting, cmd.Addr
		ts.ud = sqe.UserData
		k.deliver(tag)
	case uapi.UBLK_IO_COMMIT_AND_FETCH_REQ:
		if ts.state != fkOwned {
			k.violate("tag %d committed in state %d", tag, ts.state)
			k.post(sqe.UserData, -int32(syscall.EBUSY))
			return
		}
		c := fkCommit{tag: uint16(tag), id: ts.cur.id, op: ts.cur.op, result: cmd.Result, addr: cmd.Addr}
		if ts.cur.op == uapi.UBLK_IO_OP_READ && cmd.Result > 0 {
			c.data = make([]byte, cmd.Result)
			if k.ufile >= 0 {
				_, _ = unix.Pread(k.ufile, c.data, userCopyPos(0, uint16(tag), 0))
			} else {
				copy(c.data, k.bytesAt(cmd.Addr, int(cmd.Result)))
			}
		}
		k.commits = append(k.commits, c)
		ts.state, ts.addr, ts.ud = fkWaiting, cmd.Addr, sqe.UserData
		k.deliver(tag)
	case uapi.UBLK_IO_NEED_GET_DATA:
		if ts.state != fkGetData {
			k.violate("tag %d NEED_GET_DATA in state %d", tag, ts.state)
			k.post(sqe.UserData, -int32(syscall.EINVAL))
			return
		}
		copy(k.bytesAt(cmd.Addr, len(ts.cur.data)), ts.cur.data)
		ts.state = fkOwned
		k.post(sqe.UserData, 0)
	default:
		k.violate("unexpected io command %#x", nr)
	}
}

// bytesAt resolves a buffer address the engine handed over into k.bufs.
func (k *fakeKernel) bytesAt(addr uint64, n int) []byte {
	base := uint64(uintptr(unsafe.Pointer(&k.bufs[0])))
	if addr < base || addr+uint64(n) > base+uint64(len(k.bufs)) {
		k.violate("buffer address %#x+%d outside the engine's buffers", addr, n)
		return make([]byte, n)
	}
	return k.bufs[addr-base : addr-base+uint64(n)]
}

// deliver hands the tag its next request if it is waiting and one is queued,
// or aborts it if the device is stopping.
func (k *fakeKernel) deliver(tag int) {
	ts := &k.tags[tag]
	if ts.state != fkWaiting {
		return
	}
	if k.stopping {
		ts.state = fkAborted
		k.post(ts.ud, uapi.UBLK_IO_RES_ABORT)
		return
	}
	if len(ts.pending) == 0 {
		return
	}
	r := ts.pending[0]
	ts.pending = ts.pending[1:]
	ts.cur = r
	base := unsafe.Pointer(&k.desc[tag*24])
	atomic.StoreUint32((*uint32)(base), uint32(r.op)|r.flags)
	atomic.StoreUint32((*uint32)(unsafe.Add(base, 4)), r.nr)
	atomic.StoreUint64((*uint64)(unsafe.Add(base, 8)), r.sector)
	atomic.StoreUint64((*uint64)(unsafe.Add(base, 16)), ts.addr)
	if r.op == uapi.UBLK_IO_OP_WRITE && k.needGetData {
		ts.state = fkGetData
		k.post(ts.ud, uapi.UBLK_IO_RES_NEED_GET_DATA)
		return
	}
	if r.op == uapi.UBLK_IO_OP_WRITE {
		if k.ufile >= 0 {
			_, _ = unix.Pwrite(k.ufile, r.data, userCopyPos(0, uint16(tag), 0))
		} else {
			copy(k.bytesAt(ts.addr, len(r.data)), r.data)
		}
	}
	ts.state = fkOwned
	k.post(ts.ud, uapi.UBLK_IO_RES_OK)
}

// inject queues a request for a tag (from any goroutine).
func (k *fakeKernel) inject(tag int, r fkReq) {
	k.mu.Lock()
	defer k.mu.Unlock()
	k.tags[tag].pending = append(k.tags[tag].pending, r)
	k.deliver(tag)
}

// stop aborts every waiting tag and every tag as soon as it commits, like
// STOP_DEV after del_gendisk has drained the queue.
func (k *fakeKernel) stop() {
	k.mu.Lock()
	defer k.mu.Unlock()
	k.stopping = true
	for tag := range k.tags {
		k.deliver(tag)
	}
}

func (k *fakeKernel) snapshot() ([]fkCommit, []string) {
	k.mu.Lock()
	defer k.mu.Unlock()
	return append([]fkCommit(nil), k.commits...), append([]string(nil), k.violations...)
}

// waitCommits polls until n commits have been recorded.
func (k *fakeKernel) waitCommits(n int, timeout time.Duration) []fkCommit {
	k.t.Helper()
	deadline := time.Now().Add(timeout)
	for {
		c, v := k.snapshot()
		if len(v) > 0 {
			k.t.Fatalf("protocol violations: %v", v)
		}
		if len(c) >= n {
			return c
		}
		if time.Now().After(deadline) {
			k.t.Fatalf("got %d commits, want %d", len(c), n)
		}
		time.Sleep(time.Millisecond)
	}
}
