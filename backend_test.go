package ublk

import (
	"context"
	"errors"
	"math"
	"runtime"
	"testing"

	"github.com/ehrlich-b/go-ublk/internal/uapi"
)

// Tests now use the public MockBackend from testing.go

func TestMockBackend(t *testing.T) {
	backend := NewMockBackend(1024)

	// Test size
	if backend.Size() != 1024 {
		t.Errorf("Size() = %d, want 1024", backend.Size())
	}

	// Test write/read
	testData := []byte("hello world")
	n, err := backend.WriteAt(testData, 0)
	if err != nil {
		t.Errorf("WriteAt failed: %v", err)
	}
	if n != len(testData) {
		t.Errorf("WriteAt wrote %d bytes, want %d", n, len(testData))
	}

	readBuf := make([]byte, len(testData))
	n, err = backend.ReadAt(readBuf, 0)
	if err != nil {
		t.Errorf("ReadAt failed: %v", err)
	}
	if n != len(testData) {
		t.Errorf("ReadAt read %d bytes, want %d", n, len(testData))
	}
	if string(readBuf) != string(testData) {
		t.Errorf("ReadAt got %q, want %q", readBuf, testData)
	}

	// Test flush
	err = backend.Flush()
	if err != nil {
		t.Errorf("Flush failed: %v", err)
	}
	if !backend.IsFlushed() {
		t.Error("backend not marked as flushed")
	}

	// Test close
	err = backend.Close()
	if err != nil {
		t.Errorf("Close failed: %v", err)
	}
	if !backend.IsClosed() {
		t.Error("backend not marked as closed")
	}

	// Test operations after close
	_, err = backend.ReadAt(readBuf, 0)
	if err == nil {
		t.Error("ReadAt should fail after close")
	}
}

func TestDiscardBackend(t *testing.T) {
	backend := NewMockBackend(1024)

	// Write some data
	testData := []byte("hello world")
	_, _ = backend.WriteAt(testData, 0)

	// Verify data is there
	readBuf := make([]byte, len(testData))
	_, _ = backend.ReadAt(readBuf, 0)
	if string(readBuf) != string(testData) {
		t.Errorf("Data not written correctly")
	}

	// Check if backend supports discard
	discardBackend, ok := Backend(backend).(DiscardBackend)
	if !ok {
		t.Fatal("Backend should implement DiscardBackend")
	}

	// Discard the data
	err := discardBackend.Discard(0, int64(len(testData)))
	if err != nil {
		t.Errorf("Discard failed: %v", err)
	}

	// Verify data is zeroed
	_, _ = backend.ReadAt(readBuf, 0)
	for i, b := range readBuf {
		if b != 0 {
			t.Errorf("Byte %d not zeroed after discard: %d", i, b)
		}
	}
}

func TestWriteZeroesBackend(t *testing.T) {
	backend := NewMockBackend(1024)

	// Write some data first
	testData := []byte("hello world")
	_, _ = backend.WriteAt(testData, 0)

	// Check if backend supports WriteZeroes
	writeZeroesBackend, ok := Backend(backend).(WriteZeroesBackend)
	if !ok {
		t.Fatal("Backend should implement WriteZeroesBackend")
	}

	// Write zeros
	err := writeZeroesBackend.WriteZeroes(0, int64(len(testData)))
	if err != nil {
		t.Errorf("WriteZeroes failed: %v", err)
	}

	// Verify data is zeroed
	readBuf := make([]byte, len(testData))
	_, _ = backend.ReadAt(readBuf, 0)
	for i, b := range readBuf {
		if b != 0 {
			t.Errorf("Byte %d not zeroed: %d", i, b)
		}
	}
}

// The kernel requires 9 <= logical_bs_shift <= PAGE_SHIFT, and a bad block size
// used to be accepted and then corrupt data rather than fail here.
func TestValidateParamsBlockSize(t *testing.T) {
	tests := []struct {
		blockSize int
		wantErr   bool
	}{
		{512, false},
		{4096, false}, // 4Kn: valid now that sectors are counted in 512 bytes
		{0, true},     // would divide by zero in the control plane
		{511, true},   // below one sector
		{1536, true},  // not a power of two
		{8192, true},  // above the page size
	}

	for _, tt := range tests {
		params := DefaultParams(NewMockBackend(1 << 20))
		params.LogicalBlockSize = tt.blockSize
		err := validateParams(&params)
		if (err != nil) != tt.wantErr {
			t.Errorf("validateParams(LogicalBlockSize=%d) error = %v, wantErr %v",
				tt.blockSize, err, tt.wantErr)
		}
	}
}

func TestValidateParamsRejectsUnusableSizes(t *testing.T) {
	t.Run("nil backend", func(t *testing.T) {
		params := DefaultParams(nil)
		if err := validateParams(&params); err == nil {
			t.Error("accepted a nil Backend")
		}
	})

	t.Run("MaxIOSize below a page", func(t *testing.T) {
		params := DefaultParams(NewMockBackend(1 << 20))
		params.MaxIOSize = 2048
		if err := validateParams(&params); err == nil {
			t.Error("accepted MaxIOSize below the page size, which the kernel rejects")
		}
	})

	t.Run("MaxIOSize not page aligned", func(t *testing.T) {
		params := DefaultParams(NewMockBackend(1 << 20))
		params.MaxIOSize += uapi.SectorSize
		if err := validateParams(&params); err == nil {
			t.Error("accepted MaxIOSize that ADD_DEV would round down")
		}
	})

	if uint64(^uint(0)>>1) > math.MaxInt32 {
		t.Run("MaxIOSize over signed result limit", func(t *testing.T) {
			params := DefaultParams(NewMockBackend(1 << 20))
			overLimit := int64(math.MaxInt32) + 1
			params.MaxIOSize = int(overLimit)
			if err := validateParams(&params); err == nil {
				t.Error("accepted MaxIOSize that cannot fit a completion result")
			}
		})
	}

	t.Run("invalid queue depth", func(t *testing.T) {
		params := DefaultParams(NewMockBackend(1 << 20))
		params.QueueDepth = uapi.UBLK_MAX_QUEUE_DEPTH + 1
		if err := validateParams(&params); err == nil {
			t.Error("accepted queue depth above the UAPI limit")
		}
	})

	t.Run("backend size not a whole number of blocks", func(t *testing.T) {
		params := DefaultParams(NewMockBackend(1<<20 + 100))
		if err := validateParams(&params); err == nil {
			t.Error("accepted a backend size that is not a multiple of the block size")
		}
	})
}

func TestApplyNegotiatedDeviceInfo(t *testing.T) {
	params := DefaultParams(NewMockBackend(1 << 20))
	ctrlParams := convertToCtrlParams(params)
	info := &uapi.UblksrvCtrlDevInfo{
		NrHwQueues:    2,
		QueueDepth:    64,
		MaxIOBufBytes: 128 << 10,
	}

	if err := applyNegotiatedDeviceInfo(&params, &ctrlParams, info); err != nil {
		t.Fatalf("apply negotiated info: %v", err)
	}
	if params.NumQueues != 2 || params.QueueDepth != 64 || params.MaxIOSize != 128<<10 {
		t.Errorf("public params = queues %d, depth %d, max I/O %d",
			params.NumQueues, params.QueueDepth, params.MaxIOSize)
	}
	if ctrlParams.NumQueues != 2 || ctrlParams.QueueDepth != 64 || ctrlParams.MaxIOSize != 128<<10 {
		t.Errorf("control params = queues %d, depth %d, max I/O %d",
			ctrlParams.NumQueues, ctrlParams.QueueDepth, ctrlParams.MaxIOSize)
	}

	info.MaxIOBufBytes = uint32(128<<10 - uapi.SectorSize)
	if err := applyNegotiatedDeviceInfo(&params, &ctrlParams, info); err == nil {
		t.Error("accepted a negotiated capacity that is not page aligned")
	}
}

func TestDefaultParams(t *testing.T) {
	backend := NewMockBackend(1024)
	params := DefaultParams(backend)

	if params.Backend != backend {
		t.Error("Backend not set correctly")
	}

	if params.QueueDepth != DefaultQueueDepth {
		t.Errorf("QueueDepth = %d, want %d", params.QueueDepth, DefaultQueueDepth)
	}

	if params.LogicalBlockSize != DefaultLogicalBlockSize {
		t.Errorf("LogicalBlockSize = %d, want %d", params.LogicalBlockSize, DefaultLogicalBlockSize)
	}

	if params.MaxIOSize != DefaultMaxIOSize {
		t.Errorf("MaxIOSize = %d, want %d", params.MaxIOSize, DefaultMaxIOSize)
	}

	if params.DeviceID != AutoAssignDeviceID {
		t.Errorf("DeviceID = %d, want %d", params.DeviceID, AutoAssignDeviceID)
	}

	// Test boolean defaults
	if params.ReadOnly {
		t.Error("ReadOnly should default to false")
	}
	if params.Rotational {
		t.Error("Rotational should default to false")
	}
	if params.EnableZeroCopy {
		t.Error("EnableZeroCopy should default to false")
	}
}

func TestConvertToCtrlParamsQueues(t *testing.T) {
	params := DefaultParams(NewMockBackend(1024))
	if got := convertToCtrlParams(params).NumQueues; got != runtime.NumCPU() {
		t.Errorf("auto queues sent to ADD_DEV = %d, want %d", got, runtime.NumCPU())
	}

	params.NumQueues = 2
	if got := convertToCtrlParams(params).NumQueues; got != 2 {
		t.Errorf("explicit queues sent to ADD_DEV = %d, want 2", got)
	}
}

func BenchmarkMockBackendRead(b *testing.B) {
	backend := NewMockBackend(1024 * 1024) // 1MB
	buf := make([]byte, 4096)              // 4KB reads

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		offset := int64(i*4096) % (1024*1024 - 4096)
		_, err := backend.ReadAt(buf, offset)
		if err != nil {
			b.Fatalf("ReadAt failed: %v", err)
		}
	}
}

func BenchmarkMockBackendWrite(b *testing.B) {
	backend := NewMockBackend(1024 * 1024) // 1MB
	buf := make([]byte, 4096)              // 4KB writes
	for i := range buf {
		buf[i] = byte(i)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		offset := int64(i*4096) % (1024*1024 - 4096)
		_, err := backend.WriteAt(buf, offset)
		if err != nil {
			b.Fatalf("WriteAt failed: %v", err)
		}
	}
}

func TestDeviceStateInspection(t *testing.T) {
	// Test nil device - nil is equivalent to closed (doesn't exist)
	var device *Device
	if device.State() != DeviceStateClosed {
		t.Error("Nil device should be in closed state")
	}
	if device.IsRunning() {
		t.Error("Nil device should not be running")
	}

	// Test device info for nil device
	info := device.Info()
	if info.State != "" {
		t.Errorf("Nil device info should show empty state, got %s", info.State)
	}
}

func TestDeviceInfo(t *testing.T) {
	backend := NewMockBackend(1024 * 1024)
	params := DefaultParams(backend)
	params.QueueDepth = 64
	params.NumQueues = 2

	// Create a device struct manually for testing (since we can't actually create devices in unit tests)
	device := &Device{
		ID:        5,
		Path:      "/dev/ublkb5",
		CharPath:  "/dev/ublkc5",
		Backend:   backend,
		queues:    params.NumQueues,
		depth:     params.QueueDepth,
		blockSize: params.LogicalBlockSize,
		state:     DeviceStateRunning,
		params:    params,
		done:      make(chan struct{}),
	}

	// Test inspection methods
	if device.DeviceID() != 5 {
		t.Errorf("DeviceID() = %d, want 5", device.DeviceID())
	}
	if device.BlockPath() != "/dev/ublkb5" {
		t.Errorf("BlockPath() = %s, want /dev/ublkb5", device.BlockPath())
	}
	if device.CharDevicePath() != "/dev/ublkc5" {
		t.Errorf("CharDevicePath() = %s, want /dev/ublkc5", device.CharDevicePath())
	}
	if device.NumQueues() != 2 {
		t.Errorf("NumQueues() = %d, want 2", device.NumQueues())
	}
	if device.QueueDepth() != 64 {
		t.Errorf("QueueDepth() = %d, want 64", device.QueueDepth())
	}
	if device.BlockSize() != 512 {
		t.Errorf("BlockSize() = %d, want 512", device.BlockSize())
	}
	if device.Size() != 1024*1024 {
		t.Errorf("Size() = %d, want %d", device.Size(), 1024*1024)
	}

	// Test comprehensive info
	info := device.Info()
	if info.ID != 5 {
		t.Errorf("Info.ID = %d, want 5", info.ID)
	}
	if info.BlockPath != "/dev/ublkb5" {
		t.Errorf("Info.BlockPath = %s, want /dev/ublkb5", info.BlockPath)
	}
	if info.NumQueues != 2 {
		t.Errorf("Info.NumQueues = %d, want 2", info.NumQueues)
	}
	if info.QueueDepth != 64 {
		t.Errorf("Info.QueueDepth = %d, want 64", info.QueueDepth)
	}
	if info.Size != 1024*1024 {
		t.Errorf("Info.Size = %d, want %d", info.Size, 1024*1024)
	}
}

// TestDeviceLifecycleStates tests how State reports each lifecycle state.
// Real transitions need root and a kernel; these check the state machine's
// reporting, including a running device whose queue failed.
func TestDeviceLifecycleStates(t *testing.T) {
	backend := NewMockBackend(1024 * 1024)
	mk := func(state DeviceState) *Device {
		return &Device{ID: 1, Backend: backend, state: state, done: make(chan struct{}), options: &Options{}}
	}
	for _, st := range []DeviceState{DeviceStateCreated, DeviceStateRunning, DeviceStateStopped,
		DeviceStateClosed, DeviceStateDetached} {
		d := mk(st)
		if got := d.State(); got != st {
			t.Errorf("State() = %s, want %s", got, st)
		}
		if d.IsRunning() != (st == DeviceStateRunning) {
			t.Errorf("IsRunning() for %s = %v", st, d.IsRunning())
		}
	}

	failed := mk(DeviceStateRunning)
	failed.finish(errors.New("queue 0 died"))
	if failed.State() != DeviceStateFailed || failed.IsRunning() {
		t.Errorf("a running device whose queue failed reports %s", failed.State())
	}
	if failed.Err() == nil {
		t.Error("Err() is nil after a failure")
	}
	select {
	case <-failed.Done():
	default:
		t.Error("Done() not closed after a failure")
	}

	stopped := mk(DeviceStateRunning)
	if stopped.Err() != nil {
		t.Error("Err() non-nil while running")
	}
	stopped.finish(nil)
	if stopped.Err() != nil {
		t.Error("Err() non-nil after an orderly stop")
	}
}

// TestDeviceLifecycleAPIPreconditions tests that lifecycle methods enforce preconditions
func TestDeviceLifecycleAPIPreconditions(t *testing.T) {
	backend := NewMockBackend(1024 * 1024)
	options := &Options{}

	// Test Start on nil device
	var nilDevice *Device
	if err := nilDevice.Start(context.Background()); err == nil {
		t.Error("Start on nil device should return error")
	}

	// Test Start on already started device
	startedDevice := &Device{ID: 1, Backend: backend, state: DeviceStateRunning, done: make(chan struct{}), options: options}
	if err := startedDevice.Start(context.Background()); err == nil {
		t.Error("Start on already started device should return error")
	}

	// Test Start on a stopped device: restart is not supported
	stoppedDevice := &Device{ID: 4, Backend: backend, state: DeviceStateStopped, done: make(chan struct{}), options: options}
	if err := stoppedDevice.Start(context.Background()); !errors.Is(err, ErrStopped) {
		t.Errorf("Start on a stopped device = %v, want ErrStopped", err)
	}

	// Test Start on closed device
	closedDevice := &Device{ID: 2, Backend: backend, state: DeviceStateClosed, done: make(chan struct{}), options: options}
	if err := closedDevice.Start(context.Background()); err == nil {
		t.Error("Start on closed device should return error")
	}

	// Test Stop on nil device
	if err := nilDevice.Stop(); err == nil {
		t.Error("Stop on nil device should return error")
	}

	// Test Stop on not started device
	notStartedDevice := &Device{ID: 3, Backend: backend, state: DeviceStateCreated, done: make(chan struct{}), options: options}
	if err := notStartedDevice.Stop(); err == nil {
		t.Error("Stop on not started device should return error")
	}

	// Test Stop on closed device
	if err := closedDevice.Stop(); err == nil {
		t.Error("Stop on closed device should return error")
	}

	// Test Close on nil device
	if err := nilDevice.Close(); err == nil {
		t.Error("Close on nil device should return error")
	}

	// Test Close is idempotent (calling on already closed device returns nil)
	if err := closedDevice.Close(); err != nil {
		t.Errorf("Close on already closed device should return nil, got %v", err)
	}
}

// TestDeviceInfoWithStates tests that DeviceInfo correctly reflects all states
func TestDeviceInfoWithStates(t *testing.T) {
	backend := NewMockBackend(1024 * 1024)

	tests := []struct {
		name          string
		device        *Device
		expectedState DeviceState
	}{
		{
			name:          "nil device",
			device:        nil,
			expectedState: "", // Info() on nil returns empty struct
		},
		{
			name:          "created device",
			device:        &Device{ID: 1, Backend: backend, state: DeviceStateCreated, done: make(chan struct{})},
			expectedState: DeviceStateCreated,
		},
		{
			name:          "closed device",
			device:        &Device{ID: 2, Backend: backend, state: DeviceStateClosed, done: make(chan struct{})},
			expectedState: DeviceStateClosed,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			info := tt.device.Info()
			if info.State != tt.expectedState {
				t.Errorf("Info.State = %s, want %s", info.State, tt.expectedState)
			}
		})
	}
}
