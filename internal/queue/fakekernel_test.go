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

	// zero copy
	zcAuto      bool         // the device has UBLK_F_AUTO_BUF_REG
	zcFallback  map[int]bool // tags whose auto registration "fails" (NEED_REG_BUF)
	bufTable    int          // sparse buffer table size, 0 = none registered
	registered  map[int]bool // buffer-table slots holding a request's pages
	fileOps     []fkFileOp
	fileShortBy int // shorten READ_FIXED results by this many bytes

	// batch I/O
	batch       bool
	batchFlags  uint16
	fetchUD     uint64
	fetchArmed  bool
	batchQ      []uint16 // delivered tags not yet posted to the fetch
	tagRing     *fakeTagRing
	commitLimit int // consume at most this many elements per COMMIT_IO_CMDS (0: all)
}

// fakeTagRing is the provided-buffer ring the engine registers for batch I/O.
type fakeTagRing struct {
	k      *fakeKernel
	staged []fkTagBuf
	avail  []fkTagBuf
}

type fkTagBuf struct {
	buf []byte
	bid uint16
}

func (r *fakeTagRing) Add(buf []byte, bid uint16, offset int) {
	r.staged = append(r.staged, fkTagBuf{buf, bid})
}

// Advance is called by the engine on its own thread; it may hold no fake lock.
func (r *fakeTagRing) Advance(count int) {
	r.k.mu.Lock()
	r.avail = append(r.avail, r.staged...)
	r.staged = r.staged[:0]
	r.k.flushBatch()
	r.k.mu.Unlock()
}

// NewTagRing is the tagRingProvider side of the production ring.
func (k *fakeKernel) NewTagRing(bgid uint16, entries uint32) (tagRing, error) {
	k.tagRing = &fakeTagRing{k: k}
	return k.tagRing, nil
}

// flushBatch posts delivered tags to the armed multishot fetch, at most 128
// per buffer, while buffers last. Caller holds k.mu.
func (k *fakeKernel) flushBatch() {
	for k.fetchArmed && len(k.batchQ) > 0 && k.tagRing != nil && len(k.tagRing.avail) > 0 {
		b := k.tagRing.avail[0]
		k.tagRing.avail = k.tagRing.avail[1:]
		n := min(len(k.batchQ), len(b.buf)/2, 128)
		for i, tag := range k.batchQ[:n] {
			b.buf[2*i], b.buf[2*i+1] = byte(tag), byte(tag>>8)
		}
		k.batchQ = k.batchQ[n:]
		k.cq = append(k.cq, uring.CQE{UserData: k.fetchUD, Res: int32(2 * n),
			Flags: uring.IORING_CQE_F_BUFFER | uint32(b.bid)<<uring.IORING_CQE_BUFFER_SHIFT | uring.IORING_CQE_F_MORE})
	}
	k.maybeEndFetch()
}

// maybeEndFetch ends the multishot fetch with ABORT once a stopping device has
// no request left with the server, as STOP_DEV does after draining.
func (k *fakeKernel) maybeEndFetch() {
	if !k.batch || !k.stopping || !k.fetchArmed {
		return
	}
	for _, ts := range k.tags {
		if ts.state == fkOwned {
			return
		}
	}
	k.fetchArmed = false
	k.cq = append(k.cq, uring.CQE{UserData: k.fetchUD, Res: uapi.UBLK_IO_RES_ABORT})
}

// batchCmd handles PREP_IO_CMDS, FETCH_IO_CMDS and COMMIT_IO_CMDS.
func (k *fakeKernel) batchCmd(sqe *uring.SQE) {
	h := *(*uapi.UblkBatchIO)(unsafe.Pointer(sqe.Cmd()))
	if sqe.CmdOp() == uapi.UBLK_U_IO_FETCH_IO_CMDS {
		if sqe.OpFlags&uring.IORING_URING_CMD_MULTISHOT == 0 || sqe.Flags&uring.IOSQE_BUFFER_SELECT == 0 || h.ElemBytes != 2 {
			k.violate("FETCH_IO_CMDS without multishot/buffer select/elem_bytes 2: %+v flags %#x", h, sqe.OpFlags)
		}
		k.fetchUD, k.fetchArmed = sqe.UserData, true
		k.flushBatch()
		return
	}
	eb := int(h.ElemBytes)
	if eb != int(uapi.BatchElemBytes(h.Flags)) {
		k.violate("batch elem_bytes %d for flags %#x", eb, h.Flags)
	}
	addr := uintptr(sqe.Addr) // the engine's off-heap element buffer
	base := *(*unsafe.Pointer)(unsafe.Pointer(&addr))
	elem := func(i int) (tag uint16, result int32, addr uint64) {
		p := unsafe.Add(base, i*eb)
		e := (*uapi.UblkElemHeader)(p)
		if h.Flags&uapi.UBLK_BATCH_F_HAS_BUF_ADDR != 0 {
			addr = *(*uint64)(unsafe.Add(p, 8))
		}
		return e.Tag, e.Result, addr
	}
	switch sqe.CmdOp() {
	case uapi.UBLK_U_IO_PREP_IO_CMDS:
		k.batch, k.batchFlags = true, h.Flags
		for i := 0; i < int(h.NrElem); i++ {
			tag, _, addr := elem(i)
			if int(tag) >= k.depth || k.tags[tag].state != fkIdle {
				k.violate("PREP_IO_CMDS: bad tag %d", tag)
				continue
			}
			k.tags[tag].state, k.tags[tag].addr = fkWaiting, addr
			k.deliver(int(tag))
		}
		k.post(sqe.UserData, 0)
	case uapi.UBLK_U_IO_COMMIT_IO_CMDS:
		consumed := 0
		for i := 0; i < int(h.NrElem); i++ {
			if k.commitLimit > 0 && i == k.commitLimit {
				break
			}
			tag, result, addr := elem(i)
			ts := &k.tags[tag]
			if ts.state != fkOwned {
				k.violate("COMMIT_IO_CMDS: tag %d not owned (state %d)", tag, ts.state)
				break
			}
			c := fkCommit{tag: tag, id: ts.cur.id, op: ts.cur.op, result: result, addr: addr}
			if ts.cur.op == uapi.UBLK_IO_OP_READ && result > 0 {
				c.data = make([]byte, result)
				if k.ufile >= 0 {
					_, _ = unix.Pread(k.ufile, c.data, userCopyPos(0, tag, 0))
				} else {
					copy(c.data, k.bytesAt(addr, int(result)))
				}
			}
			k.commits = append(k.commits, c)
			ts.state, ts.addr = fkWaiting, addr
			consumed++
			k.deliver(int(tag))
		}
		res := int32(consumed * eb)
		if consumed == 0 {
			res = -int32(syscall.EBUSY)
		}
		k.post(sqe.UserData, res)
		k.maybeEndFetch()
	}
}

type fkFileOp struct {
	opcode   uint8
	bufIndex uint16
	off      uint64
	length   uint64
	mode     uint32 // fallocate mode
	rwFlags  uint32
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
	shm    uint64 // shared-memory zero copy: descriptor address (index<<32|off); no copy
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
		t:          t,
		depth:      depth,
		desc:       make([]byte, depth*24),
		bufs:       make([]byte, depth*bufSize),
		ufile:      -1,
		tags:       make([]fkTag, depth),
		wakeFd:     -1,
		kick:       make(chan struct{}, 1),
		zcFallback: map[int]bool{},
		registered: map[int]bool{},
	}
}

// RegisterBuffersSparse is the bufRegistrar side of uring.IoUring.
func (k *fakeKernel) RegisterBuffersSparse(n uint32) error {
	k.mu.Lock()
	defer k.mu.Unlock()
	if k.bufTable != 0 {
		k.violate("buffer table registered twice")
	}
	k.bufTable = int(n)
	return nil
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
		case uring.IORING_OP_READ_FIXED, uring.IORING_OP_WRITE_FIXED:
			idx := int(sqe.BufIndex)
			if !k.registered[idx] {
				k.violate("fixed-buffer op on unregistered slot %d", idx)
			}
			k.fileOps = append(k.fileOps, fkFileOp{opcode: sqe.Opcode, bufIndex: sqe.BufIndex, off: sqe.Off,
				length: uint64(sqe.Len), rwFlags: sqe.OpFlags})
			res := int32(sqe.Len)
			if sqe.Opcode == uring.IORING_OP_READ_FIXED {
				res -= int32(k.fileShortBy)
			}
			k.post(sqe.UserData, res)
		case uring.IORING_OP_FALLOCATE:
			k.fileOps = append(k.fileOps, fkFileOp{opcode: sqe.Opcode, off: sqe.Off, length: sqe.Addr, mode: sqe.Len})
			k.post(sqe.UserData, 0)
		case uring.IORING_OP_FSYNC, uring.IORING_OP_NOP:
			k.fileOps = append(k.fileOps, fkFileOp{opcode: sqe.Opcode, rwFlags: sqe.OpFlags})
			k.post(sqe.UserData, 0)
		default:
			k.violate("unexpected opcode %d", sqe.Opcode)
		}
	}
	k.sqLen = 0
	return n
}

func (k *fakeKernel) uringCmd(sqe *uring.SQE) {
	switch sqe.CmdOp() {
	case uapi.UBLK_U_IO_PREP_IO_CMDS, uapi.UBLK_U_IO_COMMIT_IO_CMDS, uapi.UBLK_U_IO_FETCH_IO_CMDS:
		k.batchCmd(sqe)
		return
	}
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
	if k.zcAuto && (nr == uapi.UBLK_IO_FETCH_REQ || nr == uapi.UBLK_IO_COMMIT_AND_FETCH_REQ) {
		want := uapi.UblkAutoBufReg{Index: uint16(tag), Flags: uapi.UBLK_AUTO_BUF_REG_FALLBACK}.SQEAddr()
		if sqe.Addr != want {
			k.violate("tag %d: sqe->addr %#x, want auto-buf-reg %#x", tag, sqe.Addr, want)
		}
	}
	switch nr {
	case uapi.UBLK_IO_REGISTER_IO_BUF:
		if ts.state != fkOwned || k.registered[int(cmd.Addr)] || int(cmd.Addr) >= k.bufTable {
			k.violate("tag %d: REGISTER_IO_BUF slot %d (state %d, registered %v, table %d)",
				tag, cmd.Addr, ts.state, k.registered[int(cmd.Addr)], k.bufTable)
		}
		k.registered[int(cmd.Addr)] = true
		k.post(sqe.UserData, 0)
		return
	case uapi.UBLK_IO_UNREGISTER_IO_BUF:
		if !k.registered[int(cmd.Addr)] {
			k.violate("UNREGISTER_IO_BUF of empty slot %d", cmd.Addr)
		}
		delete(k.registered, int(cmd.Addr))
		k.post(sqe.UserData, 0)
		return
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
		if ts.cur.op == uapi.UBLK_IO_OP_READ && cmd.Result > 0 && k.bufTable == 0 && ts.cur.shm == 0 {
			c.data = make([]byte, cmd.Result)
			if k.ufile >= 0 {
				_, _ = unix.Pread(k.ufile, c.data, userCopyPos(0, uint16(tag), 0))
			} else {
				copy(c.data, k.bytesAt(cmd.Addr, int(cmd.Result)))
			}
		}
		k.commits = append(k.commits, c)
		if k.bufTable > 0 {
			if k.zcAuto && !k.zcFallback[tag] {
				delete(k.registered, tag) // COMMIT unregisters the auto-registered buffer
			} else if k.registered[tag] {
				k.violate("tag %d committed with its buffer still registered", tag)
			}
		}
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
	if k.batch {
		if k.stopping || len(ts.pending) == 0 {
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
		if r.op == uapi.UBLK_IO_OP_WRITE {
			if k.ufile >= 0 {
				_, _ = unix.Pwrite(k.ufile, r.data, userCopyPos(0, uint16(tag), 0))
			} else {
				copy(k.bytesAt(ts.addr, len(r.data)), r.data)
			}
		}
		ts.state = fkOwned
		k.batchQ = append(k.batchQ, uint16(tag))
		k.flushBatch()
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
	if r.shm != 0 {
		atomic.StoreUint32((*uint32)(base), uint32(r.op)|r.flags|uint32(FlagSharedMemory))
		atomic.StoreUint64((*uint64)(unsafe.Add(base, 16)), r.shm)
		ts.state = fkOwned
		k.post(ts.ud, uapi.UBLK_IO_RES_OK)
		return
	}
	if r.op == uapi.UBLK_IO_OP_WRITE && k.needGetData {
		ts.state = fkGetData
		k.post(ts.ud, uapi.UBLK_IO_RES_NEED_GET_DATA)
		return
	}
	if k.bufTable > 0 && (r.op == uapi.UBLK_IO_OP_READ || r.op == uapi.UBLK_IO_OP_WRITE) {
		if k.zcAuto && !k.zcFallback[tag] {
			k.registered[tag] = true // auto registration on delivery
		} else if k.zcAuto {
			atomic.StoreUint32((*uint32)(base), uint32(r.op)|r.flags|uint32(FlagNeedRegBuf))
		}
		ts.state = fkOwned
		k.post(ts.ud, uapi.UBLK_IO_RES_OK)
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
	k.maybeEndFetch()
	select {
	case k.kick <- struct{}{}:
	default:
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
