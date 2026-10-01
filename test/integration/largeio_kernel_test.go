//go:build integration
// +build integration

package integration

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"sync"
	"testing"
	"time"
	"unsafe"

	"golang.org/x/sys/unix"

	"github.com/ehrlich-b/go-ublk"
)

const disposableTestEnv = "GO_UBLK_DISPOSABLE_TEST"

type recordingMemoryBackend struct {
	mu           sync.Mutex
	data         []byte
	readLengths  []int
	writeLengths []int
}

func newRecordingMemoryBackend(size int) *recordingMemoryBackend {
	return &recordingMemoryBackend{data: make([]byte, size)}
}

func (b *recordingMemoryBackend) ReadAt(p []byte, offset int64) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.readLengths = append(b.readLengths, len(p))
	if offset < 0 || offset >= int64(len(b.data)) {
		return 0, io.EOF
	}
	n := copy(p, b.data[offset:])
	if n != len(p) {
		return n, io.EOF
	}
	return n, nil
}

func (b *recordingMemoryBackend) WriteAt(p []byte, offset int64) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.writeLengths = append(b.writeLengths, len(p))
	if offset < 0 || offset >= int64(len(b.data)) {
		return 0, io.ErrShortWrite
	}
	n := copy(b.data[offset:], p)
	if n != len(p) {
		return n, io.ErrShortWrite
	}
	return n, nil
}

func (b *recordingMemoryBackend) Size() int64 { return int64(len(b.data)) }
func (*recordingMemoryBackend) Close() error  { return nil }
func (*recordingMemoryBackend) Flush() error  { return nil }

func (b *recordingMemoryBackend) lengths() ([]int, []int) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return append([]int(nil), b.readLengths...), append([]int(nil), b.writeLengths...)
}

func TestDisposableLargeIOPublicRunnerPaths(t *testing.T) {
	requireDisposableKernelTest(t)
	tests := []struct {
		name  string
		start func(*testing.T, context.Context, ublk.DeviceParams) *ublk.Device
	}{
		{name: "CreateAndServe", start: startWithCreateAndServe},
		{name: "CreateThenStart", start: startWithCreateThenStart},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			runDisposableLargeIO(t, test.start)
		})
	}
}

func requireDisposableKernelTest(t *testing.T) {
	t.Helper()
	if os.Getenv(disposableTestEnv) != "1" {
		t.Skipf("set %s=1 only in a disposable ublk test guest", disposableTestEnv)
	}
	control, err := os.OpenFile("/dev/ublk-control", os.O_RDWR, 0)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			t.Fatalf("%s=1 but /dev/ublk-control is missing", disposableTestEnv)
		}
		t.Fatalf("%s=1 but existing privilege cannot open /dev/ublk-control read-write: %v",
			disposableTestEnv, err)
	}
	if err := control.Close(); err != nil {
		t.Fatalf("close /dev/ublk-control capability check: %v", err)
	}
}

func startWithCreateAndServe(
	t *testing.T, ctx context.Context, params ublk.DeviceParams,
) *ublk.Device {
	t.Helper()
	device, err := ublk.CreateAndServe(ctx, params, nil)
	if err != nil {
		t.Fatalf("CreateAndServe with existing ublk privilege: %v", err)
	}
	return device
}

func startWithCreateThenStart(
	t *testing.T, ctx context.Context, params ublk.DeviceParams,
) *ublk.Device {
	t.Helper()
	device, err := ublk.Create(params, nil)
	if err != nil {
		t.Fatalf("Create with existing ublk privilege: %v", err)
	}
	needsCleanup := true
	t.Cleanup(func() {
		if needsCleanup {
			if err := device.Close(); err != nil {
				t.Errorf("cleanup device %d after Start failure: %v", device.DeviceID(), err)
			}
		}
	})
	if err := device.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	needsCleanup = false
	return device
}

func runDisposableLargeIO(
	t *testing.T,
	start func(*testing.T, context.Context, ublk.DeviceParams) *ublk.Device,
) {
	t.Helper()
	backend := newRecordingMemoryBackend(32 << 20)
	params := ublk.DefaultParams(backend)
	params.DeviceID = ublk.AutoAssignDeviceID
	params.NumQueues = 1
	params.QueueDepth = 8
	params.EnableIoctlEncode = true
	device := start(t, context.Background(), params)
	closed := false
	t.Cleanup(func() {
		if !closed {
			if err := device.Close(); err != nil {
				t.Errorf("cleanup device %d: %v", device.DeviceID(), err)
			}
		}
	})

	fd, err := openOwnedBlockDevice(device.Path, 5*time.Second)
	if err != nil {
		t.Fatalf("open own block device %s: %v", device.Path, err)
	}
	defer func() {
		if fd >= 0 {
			_ = unix.Close(fd)
		}
	}()
	exerciseDirectLargeIO(t, fd)

	readLengths, writeLengths := backend.lengths()
	if maxRequestLength(readLengths) <= 64<<10 {
		t.Fatalf("largest backend READ = %d; kernel did not deliver a request above 64KiB",
			maxRequestLength(readLengths))
	}
	if maxRequestLength(writeLengths) <= 64<<10 {
		t.Fatalf("largest backend WRITE = %d; kernel did not deliver a request above 64KiB",
			maxRequestLength(writeLengths))
	}
	t.Logf("backend request lengths: reads=%v writes=%v", readLengths, writeLengths)

	if err := unix.Close(fd); err != nil {
		t.Fatalf("close own block device %s: %v", device.Path, err)
	}
	fd = -1
	if err := device.Close(); err != nil {
		t.Fatalf("close device %d: %v", device.DeviceID(), err)
	}
	closed = true
}

func openOwnedBlockDevice(path string, timeout time.Duration) (int, error) {
	deadline := time.Now().Add(timeout)
	var lastErr error
	for time.Now().Before(deadline) {
		fd, err := unix.Open(path, unix.O_RDWR|unix.O_DIRECT|unix.O_CLOEXEC, 0)
		if err == nil {
			return fd, nil
		}
		lastErr = err
		time.Sleep(20 * time.Millisecond)
	}
	return -1, lastErr
}

func exerciseDirectLargeIO(t *testing.T, fd int) {
	t.Helper()
	sizes := []int{64<<10 - 512, 64 << 10, 64<<10 + 512, 128 << 10, 256 << 10, 1 << 20}
	const spacing = 2 << 20
	for repeat := 0; repeat < 2; repeat++ {
		for i, size := range sizes {
			offset := int64(1<<20 + (repeat*len(sizes)+i)*spacing)
			written := alignedKernelBuffer(size)
			for j := range written {
				written[j] = byte((repeat*47+i*29+j*131)%251 + 1)
			}
			n, err := unix.Pwrite(fd, written, offset)
			if err != nil || n != size {
				t.Fatalf("direct write size=%d offset=%d: n=%d err=%v", size, offset, n, err)
			}

			read := alignedKernelBuffer(size)
			for j := range read {
				read[j] = 0xA5
			}
			n, err = unix.Pread(fd, read, offset)
			if err != nil || n != size {
				t.Fatalf("direct read size=%d offset=%d: n=%d err=%v", size, offset, n, err)
			}
			if !bytes.Equal(read, written) {
				t.Fatalf("direct I/O mismatch for size=%d offset=%d repeat=%d", size, offset, repeat)
			}
		}
	}
}

func alignedKernelBuffer(size int) []byte {
	alignment := uintptr(os.Getpagesize())
	storage := make([]byte, size+int(alignment))
	address := uintptr(unsafe.Pointer(&storage[0]))
	start := int((alignment - address%alignment) % alignment)
	return storage[start : start+size]
}

func maxRequestLength(lengths []int) int {
	maximum := 0
	for _, length := range lengths {
		if length > maximum {
			maximum = length
		}
	}
	return maximum
}
