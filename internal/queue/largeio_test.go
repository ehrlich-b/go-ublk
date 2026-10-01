package queue

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"sync"
	"testing"
	"unsafe"

	"github.com/ehrlich-b/go-ublk/internal/interfaces"
	"github.com/ehrlich-b/go-ublk/internal/uapi"
	"github.com/ehrlich-b/go-ublk/internal/uring"
)

const largeIOTestMaxSize = 1 << 20

type largeIOResult struct {
	userData uint64
}

func (r largeIOResult) UserData() uint64 { return r.userData }
func (largeIOResult) Value() int32       { return 0 }
func (largeIOResult) Error() error       { return nil }

type largeIOCommand struct {
	cmd      uint32
	ioCmd    uapi.UblksrvIOCmd
	userData uint64
}

type largeIOFakeRing struct {
	submitted   []largeIOCommand
	prepared    []largeIOCommand
	flushed     int
	consumeSize int
	consumed    [][]byte
}

func (*largeIOFakeRing) Close() error { return nil }

func (*largeIOFakeRing) SubmitCtrlCmd(
	uint32, *uapi.UblksrvCtrlCmd, uint64,
) (uring.Result, error) {
	return nil, nil
}

func (*largeIOFakeRing) SubmitCtrlCmdAsync(
	uint32, *uapi.UblksrvCtrlCmd, uint64,
) (*uring.AsyncHandle, error) {
	return nil, nil
}

func (f *largeIOFakeRing) SubmitIOCmd(
	cmd uint32, ioCmd *uapi.UblksrvIOCmd, userData uint64,
) (uring.Result, error) {
	f.submitted = append(f.submitted, largeIOCommand{cmd: cmd, ioCmd: *ioCmd, userData: userData})
	return largeIOResult{userData: userData}, nil
}

func (f *largeIOFakeRing) PrepareIOCmd(
	cmd uint32, ioCmd *uapi.UblksrvIOCmd, userData uint64,
) error {
	f.prepared = append(f.prepared, largeIOCommand{cmd: cmd, ioCmd: *ioCmd, userData: userData})
	return nil
}

func (f *largeIOFakeRing) FlushSubmissions() (uint32, error) {
	for _, command := range f.prepared[f.flushed:] {
		if f.consumeSize > 0 {
			data := unsafe.Slice((*byte)(pointerFromMmap(uintptr(command.ioCmd.Addr))), f.consumeSize)
			f.consumed = append(f.consumed, bytes.Clone(data))
		}
	}
	count := len(f.prepared) - f.flushed
	f.flushed = len(f.prepared)
	return uint32(count), nil
}

func (*largeIOFakeRing) WaitForCompletion(int) ([]uring.Result, error) { return nil, nil }
func (*largeIOFakeRing) NewBatch() uring.Batch                         { return nil }

func (f *largeIOFakeRing) writeLast(data []byte) {
	command := f.prepared[len(f.prepared)-1]
	destination := unsafe.Slice((*byte)(pointerFromMmap(uintptr(command.ioCmd.Addr))), len(data))
	copy(destination, data)
}

type largeIOCall struct {
	address uintptr
	offset  int64
	length  int
}

type largeIOBackend struct {
	data        []byte
	reads       []largeIOCall
	writes      []largeIOCall
	readResult  *backendResult
	writeResult *backendResult
	discards    []rangeCall
	zeroes      []rangeCall
	flushCount  int
}

type backendResult struct {
	n   int
	err error
}

type largeIOObserver struct {
	readSuccess  []bool
	writeSuccess []bool
}

func (o *largeIOObserver) ObserveRead(_ uint64, _ uint64, success bool) {
	o.readSuccess = append(o.readSuccess, success)
}

func (o *largeIOObserver) ObserveWrite(_ uint64, _ uint64, success bool) {
	o.writeSuccess = append(o.writeSuccess, success)
}

func (*largeIOObserver) ObserveDiscard(uint64, uint64, bool) {}
func (*largeIOObserver) ObserveFlush(uint64, bool)           {}
func (*largeIOObserver) ObserveQueueDepth(uint32)            {}

type rangeCall struct {
	offset int64
	length int64
}

func newLargeIOBackend(size int) *largeIOBackend {
	return &largeIOBackend{data: make([]byte, size)}
}

func (b *largeIOBackend) ReadAt(p []byte, offset int64) (int, error) {
	b.reads = append(b.reads, largeIOCall{
		address: uintptr(unsafe.Pointer(unsafe.SliceData(p))),
		offset:  offset,
		length:  len(p),
	})
	result := backendResult{n: len(p)}
	if b.readResult != nil {
		result = *b.readResult
	}
	if result.n > 0 && result.n <= len(p) {
		copy(p[:result.n], b.data[offset:int(offset)+result.n])
	}
	return result.n, result.err
}

func (b *largeIOBackend) WriteAt(p []byte, offset int64) (int, error) {
	b.writes = append(b.writes, largeIOCall{
		address: uintptr(unsafe.Pointer(unsafe.SliceData(p))),
		offset:  offset,
		length:  len(p),
	})
	result := backendResult{n: len(p)}
	if b.writeResult != nil {
		result = *b.writeResult
	}
	if result.n > 0 && result.n <= len(p) {
		copy(b.data[offset:int(offset)+result.n], p[:result.n])
	}
	return result.n, result.err
}

func (b *largeIOBackend) Size() int64  { return int64(len(b.data)) }
func (*largeIOBackend) Close() error   { return nil }
func (b *largeIOBackend) Flush() error { b.flushCount++; return nil }

func (b *largeIOBackend) Discard(offset, length int64) error {
	b.discards = append(b.discards, rangeCall{offset: offset, length: length})
	return nil
}

func (b *largeIOBackend) WriteZeroes(offset, length int64) error {
	b.zeroes = append(b.zeroes, rangeCall{offset: offset, length: length})
	return nil
}

func newLargeIOTestRunner(
	depth, maxIOSize int, backend interfaces.Backend,
) (*Runner, *largeIOFakeRing, []byte) {
	storage := make([]byte, depth*maxIOSize)
	ring := &largeIOFakeRing{}
	ctx, cancel := context.WithCancel(context.Background())
	return &Runner{
		depth:        depth,
		maxIOSize:    maxIOSize,
		backend:      backend,
		charDeviceFd: -1,
		ring:         ring,
		bufPtr:       unsafe.Pointer(&storage[0]),
		ctx:          ctx,
		cancel:       cancel,
		tagStates:    make([]TagState, depth),
		tagMutexes:   make([]sync.Mutex, depth),
		ioCmds:       make([]uapi.UblksrvIOCmd, depth),
		done:         make(chan struct{}),
	}, ring, storage
}

func largeIOPattern(seed byte, size int) []byte {
	data := make([]byte, size)
	for i := range data {
		data[i] = byte((int(seed)+i*131)%251 + 1)
	}
	return data
}

func largeIODescriptor(op uint8, offset int, size int) uapi.UblksrvIODesc {
	return uapi.UblksrvIODesc{
		OpFlags:     uint32(op),
		NrSectors:   uint32(size / uapi.SectorSize),
		StartSector: uint64(offset / uapi.SectorSize),
	}
}

func ownLargeIOTag(runner *Runner, tag uint16) {
	runner.tagStates[tag] = TagStateOwned
}

func TestRunnerLargeIOUsesKernelVisibleTagStorage(t *testing.T) {
	sizes := []int{
		60 << 10,
		64<<10 - uapi.SectorSize,
		64 << 10,
		64<<10 + uapi.SectorSize,
		68 << 10,
		128 << 10,
		256 << 10,
		1 << 20,
	}
	operations := []struct {
		name string
		op   uint8
	}{
		{name: "read", op: uapi.UBLK_IO_OP_READ},
		{name: "write", op: uapi.UBLK_IO_OP_WRITE},
	}

	for _, operation := range operations {
		for _, size := range sizes {
			t.Run(fmt.Sprintf("%s/%d", operation.name, size), func(t *testing.T) {
				const depth = 4
				const tag = uint16(depth - 1)
				const offset = 17 * uapi.SectorSize
				backend := newLargeIOBackend(2 * largeIOTestMaxSize)
				runner, ring, storage := newLargeIOTestRunner(depth, largeIOTestMaxSize, backend)
				t.Cleanup(runner.cancel)
				pattern := largeIOPattern(byte(size>>9), size)
				tagStart := int(tag) * largeIOTestMaxSize
				tagStorage := storage[tagStart : tagStart+size]
				expectedAddress := uintptr(unsafe.Pointer(&tagStorage[0]))

				if operation.op == uapi.UBLK_IO_OP_READ {
					copy(backend.data[offset:offset+size], pattern)
				} else {
					copy(tagStorage, pattern)
				}

				if err := runner.submitInitialFetchReq(tag); err != nil {
					t.Fatalf("submit initial fetch: %v", err)
				}
				ownLargeIOTag(runner, tag)
				if err := runner.handleIORequest(tag, largeIODescriptor(operation.op, offset, size)); err != nil {
					t.Fatalf("handle request: %v", err)
				}

				if len(ring.submitted) != 1 {
					t.Fatalf("SubmitIOCmd calls = %d, want 1", len(ring.submitted))
				}
				if ring.submitted[0].ioCmd.Addr != uint64(expectedAddress) {
					t.Errorf("FETCH address = %#x, want tag storage %#x", ring.submitted[0].ioCmd.Addr, expectedAddress)
				}
				if len(ring.prepared) != 1 {
					t.Fatalf("PrepareIOCmd calls = %d, want 1", len(ring.prepared))
				}
				if ring.prepared[0].ioCmd.Addr != uint64(expectedAddress) {
					t.Errorf("COMMIT address = %#x, want tag storage %#x", ring.prepared[0].ioCmd.Addr, expectedAddress)
				}

				var call largeIOCall
				if operation.op == uapi.UBLK_IO_OP_READ {
					if len(backend.reads) != 1 {
						t.Fatalf("backend reads = %d, want 1", len(backend.reads))
					}
					call = backend.reads[0]
					if !bytes.Equal(tagStorage, pattern) {
						t.Error("READ did not fill the kernel-visible tag storage")
					}
				} else {
					if len(backend.writes) != 1 {
						t.Fatalf("backend writes = %d, want 1", len(backend.writes))
					}
					call = backend.writes[0]
					if !bytes.Equal(backend.data[offset:offset+size], pattern) {
						t.Error("WRITE did not use bytes from the kernel-visible tag storage")
					}
				}
				if call.address != expectedAddress || call.offset != offset || call.length != size {
					t.Errorf("backend buffer = {%#x, %d, %d}, want {%#x, %d, %d}",
						call.address, call.offset, call.length, expectedAddress, offset, size)
				}
			})
		}
	}
}

func TestRunnerLargeIOTagIsolation(t *testing.T) {
	const depth = 4
	const size = 128 << 10
	backend := newLargeIOBackend(4 * largeIOTestMaxSize)
	runner, ring, storage := newLargeIOTestRunner(depth, largeIOTestMaxSize, backend)
	t.Cleanup(runner.cancel)
	tags := []uint16{1, depth - 1}
	offsets := []int{19 * uapi.SectorSize, 601 * uapi.SectorSize}
	patterns := [][]byte{largeIOPattern(0x21, size), largeIOPattern(0x91, size)}

	for i, tag := range tags {
		copy(backend.data[offsets[i]:offsets[i]+size], patterns[i])
		ownLargeIOTag(runner, tag)
		if err := runner.handleIORequest(tag, largeIODescriptor(uapi.UBLK_IO_OP_READ, offsets[i], size)); err != nil {
			t.Fatalf("tag %d read: %v", tag, err)
		}
	}

	for i, tag := range tags {
		start := int(tag) * largeIOTestMaxSize
		if !bytes.Equal(storage[start:start+size], patterns[i]) {
			t.Errorf("tag %d storage does not contain its own pattern", tag)
		}
		expectedAddress := uint64(uintptr(unsafe.Pointer(&storage[start])))
		if ring.prepared[i].ioCmd.Addr != expectedAddress {
			t.Errorf("tag %d COMMIT address = %#x, want %#x", tag, ring.prepared[i].ioCmd.Addr, expectedAddress)
		}
	}
	if bytes.Equal(storage[largeIOTestMaxSize:largeIOTestMaxSize+size], patterns[1]) ||
		bytes.Equal(storage[3*largeIOTestMaxSize:3*largeIOTestMaxSize+size], patterns[0]) {
		t.Error("tag storage regions aliased")
	}
}

func TestRunnerLargeIOStorageSurvivesPrepareUntilFlushAndReuse(t *testing.T) {
	const depth = 3
	const tag = uint16(depth - 1)
	const size = 128 << 10
	const readOffset = 37 * uapi.SectorSize
	const writeOffset = 733 * uapi.SectorSize
	const secondReadOffset = 2049 * uapi.SectorSize
	backend := newLargeIOBackend(2 * largeIOTestMaxSize)
	runner, ring, storage := newLargeIOTestRunner(depth, largeIOTestMaxSize, backend)
	t.Cleanup(runner.cancel)
	ring.consumeSize = size
	tagStart := int(tag) * largeIOTestMaxSize
	tagStorage := storage[tagStart : tagStart+size]
	readPattern := largeIOPattern(0x35, size)
	writePattern := largeIOPattern(0xB2, size)
	secondReadPattern := largeIOPattern(0x67, size)
	copy(backend.data[readOffset:readOffset+size], readPattern)

	ownLargeIOTag(runner, tag)
	if err := runner.handleIORequest(tag, largeIODescriptor(uapi.UBLK_IO_OP_READ, readOffset, size)); err != nil {
		t.Fatalf("read: %v", err)
	}
	if len(ring.consumed) != 0 {
		t.Fatal("fake kernel consumed prepared storage before FlushSubmissions")
	}
	if !bytes.Equal(tagStorage, readPattern) {
		t.Fatal("prepared READ storage changed before FlushSubmissions")
	}
	if _, err := ring.FlushSubmissions(); err != nil {
		t.Fatalf("flush read commit: %v", err)
	}
	if !bytes.Equal(ring.consumed[0], readPattern) {
		t.Fatal("kernel consumed bytes from a different address than the backend filled")
	}

	ring.writeLast(writePattern)
	ownLargeIOTag(runner, tag)
	if err := runner.handleIORequest(tag, largeIODescriptor(uapi.UBLK_IO_OP_WRITE, writeOffset, size)); err != nil {
		t.Fatalf("write after reuse: %v", err)
	}
	if !bytes.Equal(backend.data[writeOffset:writeOffset+size], writePattern) {
		t.Fatal("reused tag did not send the kernel-provided bytes to the backend")
	}
	if backend.writes[0].address != uintptr(unsafe.Pointer(&tagStorage[0])) {
		t.Fatal("reused tag backend buffer does not match the committed storage address")
	}
	if _, err := ring.FlushSubmissions(); err != nil {
		t.Fatalf("flush write commit: %v", err)
	}

	copy(backend.data[secondReadOffset:secondReadOffset+size], secondReadPattern)
	ownLargeIOTag(runner, tag)
	if err := runner.handleIORequest(
		tag, largeIODescriptor(uapi.UBLK_IO_OP_READ, secondReadOffset, size),
	); err != nil {
		t.Fatalf("second read after reuse: %v", err)
	}
	if !bytes.Equal(tagStorage, secondReadPattern) {
		t.Fatal("second reuse did not refill the same persistent tag storage")
	}
	if _, err := ring.FlushSubmissions(); err != nil {
		t.Fatalf("flush second read commit: %v", err)
	}
	if !bytes.Equal(ring.consumed[len(ring.consumed)-1], secondReadPattern) {
		t.Fatal("kernel consumed the second reuse from a different address")
	}
}

func TestRunnerBackendTransferResults(t *testing.T) {
	const size = 64 << 10
	backendError := errors.New("backend failed")
	tests := []struct {
		name        string
		op          uint8
		result      backendResult
		wantSuccess bool
	}{
		{name: "full read with EOF", op: uapi.UBLK_IO_OP_READ,
			result: backendResult{n: size, err: io.EOF}, wantSuccess: true},
		{name: "short read without error", op: uapi.UBLK_IO_OP_READ,
			result: backendResult{n: size - uapi.SectorSize}, wantSuccess: false},
		{name: "read error", op: uapi.UBLK_IO_OP_READ,
			result: backendResult{err: backendError}, wantSuccess: false},
		{name: "short write without error", op: uapi.UBLK_IO_OP_WRITE,
			result: backendResult{n: size - uapi.SectorSize}, wantSuccess: false},
		{name: "write error", op: uapi.UBLK_IO_OP_WRITE,
			result: backendResult{err: backendError}, wantSuccess: false},
		{name: "full write with error", op: uapi.UBLK_IO_OP_WRITE,
			result: backendResult{n: size, err: backendError}, wantSuccess: false},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			backend := newLargeIOBackend(largeIOTestMaxSize)
			if test.op == uapi.UBLK_IO_OP_READ {
				backend.readResult = &test.result
			} else {
				backend.writeResult = &test.result
			}
			runner, ring, storage := newLargeIOTestRunner(1, largeIOTestMaxSize, backend)
			t.Cleanup(runner.cancel)
			observer := &largeIOObserver{}
			runner.observer = observer
			copy(storage[:size], largeIOPattern(0x44, size))
			ownLargeIOTag(runner, 0)

			if err := runner.handleIORequest(0, largeIODescriptor(test.op, 0, size)); err != nil {
				t.Fatalf("handle request: %v", err)
			}
			if len(ring.prepared) != 1 {
				t.Fatalf("prepared commands = %d, want 1", len(ring.prepared))
			}
			wantResult := int32(-5)
			if test.wantSuccess {
				wantResult = size
			}
			if got := ring.prepared[0].ioCmd.Result; got != wantResult {
				t.Errorf("completion result = %d, want %d", got, wantResult)
			}
			observations := observer.writeSuccess
			if test.op == uapi.UBLK_IO_OP_READ {
				observations = observer.readSuccess
			}
			if len(observations) != 1 || observations[0] != test.wantSuccess {
				t.Errorf("observer successes = %v, want [%t]", observations, test.wantSuccess)
			}
		})
	}
}

func TestRunnerOversizeDataRequestFailsBeforeBackendAccess(t *testing.T) {
	const maxIOSize = 64 << 10
	const requestSize = maxIOSize + uapi.SectorSize
	for _, operation := range []uint8{uapi.UBLK_IO_OP_READ, uapi.UBLK_IO_OP_WRITE} {
		t.Run(fmt.Sprintf("operation-%d", operation), func(t *testing.T) {
			backend := newLargeIOBackend(2 * requestSize)
			runner, ring, storage := newLargeIOTestRunner(1, maxIOSize, backend)
			t.Cleanup(runner.cancel)
			ownLargeIOTag(runner, 0)

			if err := runner.handleIORequest(0, largeIODescriptor(operation, 0, requestSize)); err != nil {
				t.Fatalf("handle request: %v", err)
			}
			if len(storage) != maxIOSize {
				t.Fatalf("storage length = %d, want %d", len(storage), maxIOSize)
			}
			if len(backend.reads) != 0 || len(backend.writes) != 0 {
				t.Fatal("oversize request reached backend")
			}
			if len(ring.prepared) != 1 || ring.prepared[0].ioCmd.Result != -5 {
				t.Fatalf("oversize completion = %+v, want one EIO", ring.prepared)
			}
		})
	}
}

func TestRunnerRangeOperationsDoNotUseDataBuffer(t *testing.T) {
	const startSector = uint64(23)
	const sectors = uint32(4097)
	want := rangeCall{
		offset: int64(startSector * uapi.SectorSize),
		length: int64(sectors) * uapi.SectorSize,
	}
	tests := []struct {
		name string
		op   uint8
	}{
		{name: "discard", op: uapi.UBLK_IO_OP_DISCARD},
		{name: "write-zeroes", op: uapi.UBLK_IO_OP_WRITE_ZEROES},
		{name: "flush", op: uapi.UBLK_IO_OP_FLUSH},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			backend := newLargeIOBackend(1)
			runner, _, _ := newLargeIOTestRunner(1, largeIOTestMaxSize, backend)
			t.Cleanup(runner.cancel)
			ownLargeIOTag(runner, 0)
			desc := uapi.UblksrvIODesc{OpFlags: uint32(test.op)}
			if test.op != uapi.UBLK_IO_OP_FLUSH {
				desc.StartSector = startSector
				desc.NrSectors = sectors
			}
			if err := runner.handleIORequest(0, desc); err != nil {
				t.Fatalf("handle request: %v", err)
			}
			if len(backend.reads) != 0 || len(backend.writes) != 0 {
				t.Fatal("range operation used a data-transfer buffer")
			}
			switch test.op {
			case uapi.UBLK_IO_OP_DISCARD:
				if len(backend.discards) != 1 || backend.discards[0] != want {
					t.Errorf("Discard call = %+v, want %+v", backend.discards, want)
				}
			case uapi.UBLK_IO_OP_WRITE_ZEROES:
				if len(backend.zeroes) != 1 || backend.zeroes[0] != want {
					t.Errorf("WriteZeroes call = %+v, want %+v", backend.zeroes, want)
				}
			case uapi.UBLK_IO_OP_FLUSH:
				if backend.flushCount != 1 {
					t.Errorf("Flush calls = %d, want 1", backend.flushCount)
				}
			}
		})
	}
}

func TestBufferAllocationSize(t *testing.T) {
	const configuredMaxIOSize = 64 << 10
	const depth = 4
	if got, err := bufferAllocationSize(depth, configuredMaxIOSize); err != nil ||
		got != depth*configuredMaxIOSize {
		t.Fatalf("bufferAllocationSize() = %d, %v", got, err)
	}

	storage := make([]byte, depth*configuredMaxIOSize)
	runner := &Runner{maxIOSize: configuredMaxIOSize, bufPtr: unsafe.Pointer(&storage[0])}
	wantAddress := uintptr(unsafe.Pointer(&storage[(depth-1)*configuredMaxIOSize]))
	if got := runner.bufferAddress(depth - 1); got != wantAddress {
		t.Errorf("last tag address = %#x, want %#x", got, wantAddress)
	}

	maxInt := int(^uint(0) >> 1)
	tests := []struct {
		name      string
		depth     int
		maxIOSize int
	}{
		{name: "zero depth", depth: 0, maxIOSize: configuredMaxIOSize},
		{name: "depth above UAPI limit", depth: uapi.UBLK_MAX_QUEUE_DEPTH + 1, maxIOSize: configuredMaxIOSize},
		{name: "zero max I/O", depth: 1, maxIOSize: 0},
		{name: "overflow", depth: 2, maxIOSize: maxInt},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if _, err := bufferAllocationSize(test.depth, test.maxIOSize); err == nil {
				t.Error("accepted unusable buffer allocation")
			}
		})
	}
}
