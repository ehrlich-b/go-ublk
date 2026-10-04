package uring

import (
	"bytes"
	"errors"
	"fmt"
	"testing"
	"time"
	"unsafe"

	"github.com/ehrlich-b/go-ublk/internal/uapi"
	"golang.org/x/sys/unix"
)

func iovecOf(b []byte) unix.Iovec {
	iov := unix.Iovec{Base: &b[0]}
	iov.SetLen(len(b))
	return iov
}

func addr(b []byte) uint64 { return uint64(uintptr(unsafe.Pointer(&b[0]))) }

// Classic registered buffers: addr is a user virtual address inside the
// registered buffer, anywhere in it, and must match the buffer index.
func TestReadWriteFixedClassicBuffers(t *testing.T) {
	r := newTestIoUring(t, SetupOptions{Entries: 8})
	_, fd := tempFileFd(t)
	mem := offHeap(t, 2*4096)
	buf0, buf1 := mem[:4096], mem[4096:]
	if err := r.RegisterBuffers([]unix.Iovec{iovecOf(buf0), iovecOf(buf1)}); err != nil {
		t.Fatalf("RegisterBuffers: %v", err)
	}
	for i := range buf0 {
		buf0[i] = byte(i ^ 0x5A)
	}
	sqe := mustGetSQE(t, r)
	PrepWriteFixed(sqe, fd, addr(buf0), 4096, 0, 0)
	sqe.Flags |= IOSQE_IO_LINK // the read starts after the write
	sqe.UserData = 1
	sqe = mustGetSQE(t, r)
	PrepReadFixed(sqe, fd, addr(buf1), 4096, 0, 1)
	sqe.UserData = 2
	got := reapAfterSubmit(t, r, 2)
	if got[1].Res != 4096 || got[2].Res != 4096 || !bytes.Equal(buf0, buf1) {
		t.Fatalf("fixed write/read = %d/%d, equal %v", got[1].Res, got[2].Res, bytes.Equal(buf0, buf1))
	}
	// Mid-buffer: 512 bytes from file offset 1024 into buf1 at +256.
	clear(buf1)
	sqe = mustGetSQE(t, r)
	PrepReadFixed(sqe, fd, addr(buf1[256:]), 512, 1024, 1)
	sqe.UserData = 3
	if got = reapAfterSubmit(t, r, 1); got[3].Res != 512 || !bytes.Equal(buf1[256:768], buf0[1024:1536]) {
		t.Fatalf("mid-buffer READ_FIXED res %d", got[3].Res)
	}
	// An address outside the indexed buffer is rejected.
	sqe = mustGetSQE(t, r)
	PrepReadFixed(sqe, fd, addr(buf0), 512, 0, 1)
	sqe.UserData = 4
	if got = reapAfterSubmit(t, r, 1); got[4].Res != -int32(unix.EFAULT) {
		t.Fatalf("READ_FIXED with buf0's address on index 1: res %d, want -EFAULT", got[4].Res)
	}
	if err := r.UnregisterBuffers(); err != nil {
		t.Fatalf("UnregisterBuffers: %v", err)
	}
}

// A sparse table starts empty; slots are filled and emptied by update. ublk
// zero copy installs its request buffers into exactly such slots.
func TestSparseBufferTable(t *testing.T) {
	r := newTestIoUring(t, SetupOptions{Entries: 8})
	_, fd := tempFileFd(t)
	mem := offHeap(t, 4096)
	copy(mem, "sparse slot three")
	writeFixed := func(userData uint64, index uint16) int32 {
		sqe := mustGetSQE(t, r)
		PrepWriteFixed(sqe, fd, addr(mem), 4096, 0, index)
		sqe.UserData = userData
		return reapAfterSubmit(t, r, 1)[userData].Res
	}
	if err := r.RegisterBuffersSparse(16); err != nil {
		t.Fatalf("RegisterBuffersSparse: %v", err)
	}
	if res := writeFixed(1, 3); res != -int32(unix.EFAULT) {
		t.Fatalf("empty slot: res %d, want -EFAULT", res)
	}
	if n, err := r.UpdateBuffers(3, []unix.Iovec{iovecOf(mem)}); err != nil || n != 1 {
		t.Fatalf("UpdateBuffers = %d, %v", n, err)
	}
	if res := writeFixed(2, 3); res != 4096 {
		t.Fatalf("filled slot: res %d, want 4096", res)
	}
	if _, err := r.UpdateBuffers(3, []unix.Iovec{{}}); err != nil {
		t.Fatalf("UpdateBuffers(empty): %v", err)
	}
	if res := writeFixed(3, 3); res != -int32(unix.EFAULT) {
		t.Fatalf("emptied slot: res %d, want -EFAULT", res)
	}
	if res := writeFixed(4, 16); res != -int32(unix.EFAULT) {
		t.Fatalf("index past the table: res %d, want -EFAULT", res)
	}
}

func TestFixedFiles(t *testing.T) {
	r := newTestIoUring(t, SetupOptions{Entries: 8})
	f, fd := tempFileFd(t)
	if _, err := f.WriteString("fixed file contents"); err != nil {
		t.Fatalf("write temp file: %v", err)
	}
	buf := offHeap(t, 64)
	readIndex := func(userData uint64, index int32) int32 {
		sqe := mustGetSQE(t, r)
		PrepRead(sqe, index, buf, 0)
		sqe.Flags |= IOSQE_FIXED_FILE
		sqe.UserData = userData
		return reapAfterSubmit(t, r, 1)[userData].Res
	}
	if err := r.RegisterFilesSparse(4); err != nil {
		t.Fatalf("RegisterFilesSparse: %v", err)
	}
	if n, err := r.UpdateFiles(2, []int32{fd}); err != nil || n != 1 {
		t.Fatalf("UpdateFiles = %d, %v", n, err)
	}
	if res := readIndex(1, 2); res != 19 || string(buf[:19]) != "fixed file contents" {
		t.Fatalf("read via fixed slot 2: res %d %q", res, buf[:max(res, 0)])
	}
	if res := readIndex(2, 1); res != -int32(unix.EBADF) {
		t.Fatalf("read via empty slot 1: res %d, want -EBADF", res)
	}
	if err := r.UnregisterFiles(); err != nil {
		t.Fatalf("UnregisterFiles: %v", err)
	}
	if err := r.RegisterFiles([]int32{-1, fd}); err != nil {
		t.Fatalf("RegisterFiles: %v", err)
	}
	if res := readIndex(3, 1); res != 19 {
		t.Fatalf("read via dense slot 1: res %d", res)
	}
}

// Provided buffer ring: each read takes a buffer and reports its ID; an empty
// ring fails reads with -ENOBUFS until a buffer is returned.
func TestProvidedBufferRing(t *testing.T) {
	requireBufRings(t)
	r := newTestIoUring(t, SetupOptions{Entries: 8})
	const entries, size, group = 8, 64, 7
	br, err := r.RegisterBufRing(group, entries)
	if err != nil {
		t.Fatalf("RegisterBufRing: %v", err)
	}
	if br.BGID() != group || br.Entries() != entries {
		t.Fatalf("BufRing group %d entries %d", br.BGID(), br.Entries())
	}
	mem := offHeap(t, entries*size)
	bufAt := func(bid uint16) []byte { return mem[int(bid)*size : int(bid+1)*size] }
	for i := 0; i < entries; i++ {
		br.Add(bufAt(uint16(i)), uint16(i), i)
	}
	br.Advance(entries)
	rfd, wfd := pipeFds(t)
	read := func(userData uint64, msg string) (CQE, uint16) {
		if msg != "" {
			if _, err := unix.Write(int(wfd), []byte(msg)); err != nil {
				t.Fatalf("write pipe: %v", err)
			}
		}
		sqe := mustGetSQE(t, r)
		PrepReadSelect(sqe, rfd, size, 0, group)
		sqe.UserData = userData
		cqe := reapAfterSubmit(t, r, 1)[userData]
		bid, _ := cqe.BufferID()
		return cqe, bid
	}
	used := make(map[uint16]bool)
	for i := 0; i < entries; i++ {
		msg := fmt.Sprintf("message %d", i)
		cqe, bid := read(uint64(i), msg)
		if _, ok := cqe.BufferID(); !ok || cqe.Res != int32(len(msg)) {
			t.Fatalf("read %d: res %d flags %#x", i, cqe.Res, cqe.Flags)
		}
		if used[bid] || string(bufAt(bid)[:cqe.Res]) != msg {
			t.Fatalf("read %d: buffer %d reused or holds %q", i, bid, bufAt(bid)[:cqe.Res])
		}
		used[bid] = true
	}
	if _, err := unix.Write(int(wfd), []byte("no buffer")); err != nil {
		t.Fatalf("write pipe: %v", err)
	}
	if cqe, _ := read(100, ""); cqe.Res != -int32(unix.ENOBUFS) {
		t.Fatalf("read with the ring exhausted: res %d, want -ENOBUFS", cqe.Res)
	}
	br.Add(bufAt(5), 5, 0)
	br.Advance(1)
	if cqe, bid := read(101, ""); bid != 5 || string(bufAt(5)[:cqe.Res]) != "no buffer" {
		t.Fatalf("read after returning buffer 5: res %d bid %d", cqe.Res, bid)
	}
	if err := r.UnregisterBufRing(br); err != nil {
		t.Fatalf("UnregisterBufRing: %v", err)
	}
	if _, err := r.RegisterBufRing(group, 3); !errors.Is(err, unix.EINVAL) {
		t.Errorf("RegisterBufRing(3 entries): %v, want EINVAL", err)
	}
}

// Close must release everything an IoUring registered or mapped, including
// buffer-ring memory, and a Ring its control staging buffer.
func TestIoUringCloseReleasesRegistrations(t *testing.T) {
	requireBufRings(t)
	quietLogs(t)
	_, fd := tempFileFd(t)
	assertNoLeak(t, func() error {
		r, err := NewIoUring(SetupOptions{Entries: 8, Flags: IORING_SETUP_SQE128 | IORING_SETUP_CQE32})
		if err != nil {
			return err
		}
		if err := r.RegisterFilesSparse(4); err != nil {
			return err
		}
		if _, err := r.UpdateFiles(0, []int32{fd}); err != nil {
			return err
		}
		if err := r.RegisterBuffersSparse(8); err != nil {
			return err
		}
		if _, err := r.RegisterBufRing(1, 8); err != nil {
			return err
		}
		PrepNop(r.GetSQE())
		if _, err := r.SubmitAndWait(1, time.Second); err != nil {
			return err
		}
		return r.Close()
	})
	rfd, _ := pipeFds(t)
	assertNoLeak(t, func() error {
		ring, err := newMinimalRing(Config{Entries: 4, FD: rfd})
		if err != nil {
			return err
		}
		buf := make([]byte, 64)
		if _, err := ring.SubmitCtrlCmd(0, &uapi.UblksrvCtrlCmd{Len: 64, Addr: AddrOf(buf)}, 1); err != nil {
			return err
		}
		if ring.ctrlScratch == nil {
			return errors.New("control command with a buffer was not staged")
		}
		return ring.Close()
	})
}
