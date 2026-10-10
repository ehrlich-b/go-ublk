package uring

import (
	"fmt"
	"runtime"
	"sync/atomic"
	"unsafe"

	"golang.org/x/sys/unix"

	"github.com/ehrlich-b/go-ublk/internal/validation"
)

// register calls io_uring_register(2), retrying EINTR (a failed registration
// has no effect). Arguments are read synchronously, so they may live on the
// stack; nested user pointers must be kept alive across the call.
func (r *IoUring) register(op uint32, arg unsafe.Pointer, nrArgs uint32) (int, error) {
	for {
		n, _, errno := unix.Syscall6(unix.SYS_IO_URING_REGISTER, uintptr(r.fd), uintptr(op),
			uintptr(arg), uintptr(nrArgs), 0, 0)
		if errno == 0 {
			return int(n), nil
		}
		if errno != unix.EINTR {
			return 0, errno
		}
	}
}

// EnableRings starts an IORING_SETUP_R_DISABLED ring (5.10). On a
// SINGLE_ISSUER ring the calling thread becomes the submitter, which lets a
// ring be created on one thread and owned by another.
func (r *IoUring) EnableRings() error {
	if _, err := r.register(IORING_REGISTER_ENABLE_RINGS, nil, 0); err != nil {
		return fmt.Errorf("enable io_uring rings: %w", err)
	}
	return nil
}

// RegisterFiles registers a dense fixed-file table: fds[i] becomes index i
// for IOSQE_FIXED_FILE. Close unregisters it.
func (r *IoUring) RegisterFiles(fds []int32) error {
	if len(fds) == 0 {
		return fmt.Errorf("register files: empty table: %w", unix.EINVAL)
	}
	if _, err := r.register(IORING_REGISTER_FILES, unsafe.Pointer(&fds[0]), uint32(len(fds))); err != nil {
		return fmt.Errorf("register %d files: %w", len(fds), err)
	}
	r.filesRegistered = true
	return nil
}

// RegisterFilesSparse registers an empty fixed-file table of n slots (5.19),
// filled later with UpdateFiles. Close unregisters it.
func (r *IoUring) RegisterFilesSparse(n uint32) error {
	reg := rsrcRegister{nr: n, flags: IORING_RSRC_REGISTER_SPARSE}
	if _, err := r.register(IORING_REGISTER_FILES2, unsafe.Pointer(&reg), uint32(unsafe.Sizeof(reg))); err != nil {
		return fmt.Errorf("register sparse table of %d files: %w", n, err)
	}
	r.filesRegistered = true
	return nil
}

// UpdateFiles replaces fixed-file slots offset..offset+len(fds)-1 (5.5). An
// fd of -1 empties a slot. It returns how many slots were updated.
func (r *IoUring) UpdateFiles(offset uint32, fds []int32) (int, error) {
	if len(fds) == 0 {
		return 0, nil
	}
	up := rsrcUpdate{offset: offset, data: uint64(uintptr(unsafe.Pointer(&fds[0])))}
	n, err := r.register(IORING_REGISTER_FILES_UPDATE, unsafe.Pointer(&up), uint32(len(fds)))
	runtime.KeepAlive(fds)
	if err != nil {
		return n, fmt.Errorf("update %d files at slot %d: %w", len(fds), offset, err)
	}
	return n, nil
}

// UnregisterFiles drops the fixed-file table and the file references it
// holds, synchronously.
func (r *IoUring) UnregisterFiles() error {
	if _, err := r.register(IORING_UNREGISTER_FILES, nil, 0); err != nil {
		return fmt.Errorf("unregister files: %w", err)
	}
	r.filesRegistered = false
	return nil
}

// RegisterBuffers registers a dense table of buffers for READ_FIXED and
// WRITE_FIXED (IORING_REGISTER_BUFFERS); iovecs[i] becomes index i. The
// kernel pins the pages, so the memory must stay allocated (AllocOffHeap,
// or Go memory kept reachable) until UnregisterBuffers or Close. Close
// unregisters the table.
func (r *IoUring) RegisterBuffers(iovecs []unix.Iovec) error {
	if len(iovecs) == 0 {
		return fmt.Errorf("register buffers: empty table: %w", unix.EINVAL)
	}
	_, err := r.register(IORING_REGISTER_BUFFERS, unsafe.Pointer(&iovecs[0]), uint32(len(iovecs)))
	runtime.KeepAlive(iovecs)
	if err != nil {
		return fmt.Errorf("register %d buffers: %w", len(iovecs), err)
	}
	r.buffersRegistered = true
	return nil
}

// RegisterBuffersSparse registers an empty buffer table of n slots
// (IORING_REGISTER_BUFFERS2 with IORING_RSRC_REGISTER_SPARSE, 5.19). ublk
// zero copy needs one: UBLK_U_IO_REGISTER_IO_BUF and UBLK_F_AUTO_BUF_REG
// install request buffers into empty slots (-EBUSY if occupied, -EINVAL past
// the end), so it must have at least one slot per tag in use. Close
// unregisters it.
func (r *IoUring) RegisterBuffersSparse(n uint32) error {
	reg := rsrcRegister{nr: n, flags: IORING_RSRC_REGISTER_SPARSE}
	if _, err := r.register(IORING_REGISTER_BUFFERS2, unsafe.Pointer(&reg), uint32(unsafe.Sizeof(reg))); err != nil {
		return fmt.Errorf("register sparse table of %d buffers: %w", n, err)
	}
	r.buffersRegistered = true
	return nil
}

// UpdateBuffers replaces buffer slots offset..offset+len(iovecs)-1
// (IORING_REGISTER_BUFFERS_UPDATE, 5.13); an iovec with a nil Base and zero
// Len empties a slot. It returns how many slots were updated.
func (r *IoUring) UpdateBuffers(offset uint32, iovecs []unix.Iovec) (int, error) {
	if len(iovecs) == 0 {
		return 0, nil
	}
	up := rsrcUpdate2{
		offset: offset,
		data:   uint64(uintptr(unsafe.Pointer(&iovecs[0]))),
		nr:     uint32(len(iovecs)),
	}
	n, err := r.register(IORING_REGISTER_BUFFERS_UPDATE, unsafe.Pointer(&up), uint32(unsafe.Sizeof(up)))
	runtime.KeepAlive(iovecs)
	if err != nil {
		return n, fmt.Errorf("update %d buffers at slot %d: %w", len(iovecs), offset, err)
	}
	return n, nil
}

// UnregisterBuffers drops the buffer table.
func (r *IoUring) UnregisterBuffers() error {
	if _, err := r.register(IORING_UNREGISTER_BUFFERS, nil, 0); err != nil {
		return fmt.Errorf("unregister buffers: %w", err)
	}
	r.buffersRegistered = false
	return nil
}

// BufRing is a provided buffer ring (IORING_REGISTER_PBUF_RING, 5.19): a
// ring of buffers the kernel picks from for requests with
// IOSQE_BUFFER_SELECT on its group. Its memory is mapped by us and pinned by
// the kernel. Hand buffers to the kernel with Add then Advance; each CQE
// that consumed one reports it (CQE.BufferID), after which it is the
// application's again until re-added. Like IoUring, not safe for concurrent
// use.
type BufRing struct {
	ring      *IoUring
	mem       []byte
	entries   []bufRingEntry
	tailWord  *uint32 // the aligned word holding slot 0's bid and the tail
	tailShift uint32
	tail      uint16
	mask      uint16
	bgid      uint16
}

// RegisterBufRing maps and registers a provided buffer ring of entries slots
// (a power of two up to 32768) for buffer group bgid. Close unregisters it.
func (r *IoUring) RegisterBufRing(bgid uint16, entries uint32) (*BufRing, error) {
	if entries == 0 || entries&(entries-1) != 0 || entries > 32768 {
		return nil, fmt.Errorf("buffer ring of %d entries: need a power of two <= 32768: %w", entries, unix.EINVAL)
	}
	layout, err := validation.ValidateLayout([]validation.Mapping{
		{Size: uint64(entries) * 16, Regions: []validation.Region{
			{Name: "buffer entries", Count: uint64(entries), Stride: 16, Align: 8},
			{Name: "buffer tail word", Offset: 12, Count: 1, Stride: 4, Align: 4},
		}},
	}, validation.RingGeometry{Entries: uint64(entries), MappedEntries: uint64(entries), Mask: uint64(entries - 1)})
	if err != nil {
		return nil, fmt.Errorf("buffer ring layout: %w", err)
	}
	mem, err := AllocOffHeap(layout.Sizes[0])
	if err != nil {
		return nil, err
	}
	reg := bufReg{ringAddr: uint64(uintptr(unsafe.Pointer(&mem[0]))), ringEntries: entries, bgid: bgid}
	if _, err := r.register(IORING_REGISTER_PBUF_RING, unsafe.Pointer(&reg), 1); err != nil {
		_ = FreeOffHeap(mem)
		return nil, fmt.Errorf("register buffer ring for group %d: %w", bgid, err)
	}
	// The u16 tail overlays slot 0's resv (bytes 14..15). Go has no 16-bit
	// atomics, so Advance stores the aligned word at bytes 12..15, which it
	// can rewrite whole because only we write slot 0's bid (bytes 12..13).
	one := uint32(1)
	shift := uint32(0)
	if *(*byte)(unsafe.Pointer(&one)) == 1 {
		shift = 16 // little-endian: bytes 14..15 are the high half
	}
	br := &BufRing{
		ring:      r,
		mem:       mem,
		entries:   unsafe.Slice((*bufRingEntry)(unsafe.Pointer(&mem[0])), entries),
		tailWord:  (*uint32)(unsafe.Pointer(&mem[12])),
		tailShift: shift,
		mask:      uint16(entries - 1),
		bgid:      bgid,
	}
	r.bufRings = append(r.bufRings, br)
	return br, nil
}

// UnregisterBufRing unregisters a buffer ring and unmaps it.
func (r *IoUring) UnregisterBufRing(br *BufRing) error {
	if err := br.unregister(); err != nil {
		return err
	}
	for i, b := range r.bufRings {
		if b == br {
			r.bufRings = append(r.bufRings[:i], r.bufRings[i+1:]...)
			break
		}
	}
	return br.free()
}

func (b *BufRing) unregister() error {
	reg := bufReg{bgid: b.bgid}
	if _, err := b.ring.register(IORING_UNREGISTER_PBUF_RING, unsafe.Pointer(&reg), 1); err != nil {
		return fmt.Errorf("unregister buffer ring for group %d: %w", b.bgid, err)
	}
	return nil
}

// free unmaps the ring memory. The kernel keeps its own pin on the pages
// while still registered, so this is safe even if unregister failed.
func (b *BufRing) free() error {
	if b.mem == nil {
		return nil
	}
	err := FreeOffHeap(b.mem)
	b.mem, b.entries, b.tailWord = nil, nil, nil
	return err
}

// BGID returns the ring's buffer group ID.
func (b *BufRing) BGID() uint16 { return b.bgid }

// Entries returns the number of slots.
func (b *BufRing) Entries() int { return len(b.entries) }

// Add writes buf as buffer bid into the slot offset places past the tail;
// nothing is visible to the kernel until Advance. The buffer must follow the
// pinning rules in ring.go until a CQE returns it.
func (b *BufRing) Add(buf []byte, bid uint16, offset int) {
	e := &b.entries[(b.tail+uint16(offset))&b.mask]
	// Field by field: slot 0's resv is the tail.
	e.addr = AddrOf(buf)
	e.len = uint32(len(buf))
	e.bid = bid
}

// Advance publishes count added buffers to the kernel. The atomic store is a
// release, ordering the slot writes before the kernel's acquire of the tail.
func (b *BufRing) Advance(count int) {
	b.tail += uint16(count)
	w := atomic.LoadUint32(b.tailWord)
	w = w&^(0xFFFF<<b.tailShift) | uint32(b.tail)<<b.tailShift
	atomic.StoreUint32(b.tailWord, w)
}

// Probe reports which opcodes the running kernel supports.
type Probe struct {
	lastOp    uint8
	supported [256]bool
}

// Probe queries the kernel's opcode support (IORING_REGISTER_PROBE, 5.6).
func (r *IoUring) Probe() (*Probe, error) {
	var buf struct {
		hdr probeHeader
		ops [256]probeOp
	}
	if _, err := r.register(IORING_REGISTER_PROBE, unsafe.Pointer(&buf), uint32(len(buf.ops))); err != nil {
		return nil, fmt.Errorf("probe io_uring opcodes: %w", err)
	}
	p := &Probe{lastOp: buf.hdr.lastOp}
	for _, op := range buf.ops[:min(int(buf.hdr.opsLen), len(buf.ops))] {
		if op.flags&IO_URING_OP_SUPPORTED != 0 {
			p.supported[op.op] = true
		}
	}
	return p, nil
}

// Supported reports whether the kernel supports opcode op.
func (p *Probe) Supported(op uint8) bool { return p.supported[op] }

// LastOp returns the highest opcode the kernel knows.
func (p *Probe) LastOp() uint8 { return p.lastOp }
