// +build !integration

package unit

import (
	"testing"

	"github.com/ehrlich-b/go-ublk"
	"github.com/ehrlich-b/go-ublk/internal/uapi"
	"github.com/ehrlich-b/go-ublk/internal/uring"
)

// These tests run without requiring ublk kernel support

func TestUAPIConstants(t *testing.T) {
	// Test that constants are properly defined
	if uapi.UBLK_CMD_ADD_DEV != 0x04 {
		t.Errorf("UBLK_CMD_ADD_DEV = %x, want 0x04", uapi.UBLK_CMD_ADD_DEV)
	}

	if uapi.UBLK_IO_FETCH_REQ != 0x20 {
		t.Errorf("UBLK_IO_FETCH_REQ = %x, want 0x20", uapi.UBLK_IO_FETCH_REQ)
	}

	if uapi.UBLKSRV_IO_BUF_OFFSET != 0x80000000 {
		t.Errorf("UBLKSRV_IO_BUF_OFFSET = %x, want 0x80000000", uapi.UBLKSRV_IO_BUF_OFFSET)
	}
}

func TestBackendInterface(t *testing.T) {
	backend := &mockBackend{
		data: make([]byte, 1024),
		size: 1024,
	}

	// Test basic backend interface compliance
	var _ ublk.Backend = backend
	
	// Test optional interfaces
	var _ ublk.DiscardBackend = backend
	var _ ublk.WriteZeroesBackend = backend
	var _ ublk.SyncBackend = backend
	var _ ublk.StatBackend = backend
	var _ ublk.ResizeBackend = backend

	// Test operations
	testData := []byte("test data")
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
	if string(readBuf) != string(testData) {
		t.Errorf("ReadAt got %q, want %q", readBuf, testData)
	}
}

func TestURingInterface(t *testing.T) {
	config := uring.Config{
		Entries: 32,
		FD:      -1,
		Flags:   0,
	}

	ring, err := uring.NewRing(config)
	if err != nil {
		t.Fatalf("NewRing failed: %v", err)
	}
	defer ring.Close()

	// Test interface compliance
	var _ uring.Ring = ring

	// Test basic operations with stub implementation
	ctrlCmd := &uapi.UblksrvCtrlCmd{
		DevID:   1,
		QueueID: 0xFFFF,
		Len:     0,
		Addr:    0,
	}

	result, err := ring.SubmitCtrlCmd(uapi.UBLK_CMD_GET_DEV_INFO, ctrlCmd, 123)
	if err != nil {
		t.Errorf("SubmitCtrlCmd failed: %v", err)
	}

	if result.UserData() != 123 {
		t.Errorf("UserData = %d, want 123", result.UserData())
	}

	if result.Value() != -38 { // ENOSYS in stub
		t.Errorf("Value = %d, want -38", result.Value())
	}
}

func TestDefaultParams(t *testing.T) {
	backend := &mockBackend{data: make([]byte, 1024), size: 1024}
	params := ublk.DefaultParams(backend)

	// Validate sensible defaults
	if params.QueueDepth <= 0 {
		t.Error("QueueDepth should be positive")
	}
	if params.LogicalBlockSize <= 0 {
		t.Error("LogicalBlockSize should be positive")
	}
	if params.MaxIOSize <= 0 {
		t.Error("MaxIOSize should be positive")
	}

	// Test that backend is set
	if params.Backend != backend {
		t.Error("Backend not set correctly")
	}

	// Test reasonable defaults
	if params.LogicalBlockSize != 512 {
		t.Errorf("LogicalBlockSize = %d, want 512", params.LogicalBlockSize)
	}
}

func TestFeatureFlags(t *testing.T) {
	// Test feature flag values match expectations
	if uapi.UBLK_F_SUPPORT_ZERO_COPY != (1 << 0) {
		t.Error("UBLK_F_SUPPORT_ZERO_COPY has wrong value")
	}

	if uapi.UBLK_F_NEED_GET_DATA != (1 << 2) {
		t.Error("UBLK_F_NEED_GET_DATA has wrong value") 
	}

	if uapi.UBLK_F_UNPRIVILEGED_DEV != (1 << 5) {
		t.Error("UBLK_F_UNPRIVILEGED_DEV has wrong value")
	}
}

func TestErrorTypes(t *testing.T) {
	// Test that error types implement error interface
	var _ error = ublk.ErrNotImplemented
	var _ error = ublk.ErrDeviceNotFound
	var _ error = ublk.ErrInvalidParameters

	// Test error messages
	if ublk.ErrNotImplemented.Error() != "not implemented" {
		t.Errorf("ErrNotImplemented message = %q, want 'not implemented'", ublk.ErrNotImplemented.Error())
	}
}

func TestDevicePathGeneration(t *testing.T) {
	// Test device path generation
	charPath := uapi.UblkDevicePath(0)
	if charPath != "/dev/ublkc0" {
		t.Errorf("UblkDevicePath(0) = %s, want /dev/ublkc0", charPath)
	}

	blockPath := uapi.UblkBlockDevicePath(42)
	if blockPath != "/dev/ublkb42" {
		t.Errorf("UblkBlockDevicePath(42) = %s, want /dev/ublkb42", blockPath)
	}
}

// Mock backend for unit tests
type mockBackend struct {
	data []byte
	size int64
}

func (m *mockBackend) ReadAt(p []byte, off int64) (int, error) {
	if off >= m.size {
		return 0, nil
	}
	n := copy(p, m.data[off:])
	return n, nil
}

func (m *mockBackend) WriteAt(p []byte, off int64) (int, error) {
	if off >= m.size {
		return 0, ublk.ErrInvalidParameters
	}
	n := copy(m.data[off:], p)
	return n, nil
}

func (m *mockBackend) Size() int64 {
	return m.size
}

func (m *mockBackend) Close() error {
	return nil
}

func (m *mockBackend) Flush() error {
	return nil
}

// Optional interface implementations
func (m *mockBackend) Discard(offset, length int64) error {
	end := offset + length
	if end > m.size {
		end = m.size
	}
	for i := offset; i < end; i++ {
		m.data[i] = 0
	}
	return nil
}

func (m *mockBackend) WriteZeroes(offset, length int64) error {
	return m.Discard(offset, length)
}

func (m *mockBackend) Sync() error {
	return nil
}

func (m *mockBackend) SyncRange(offset, length int64) error {
	return nil
}

func (m *mockBackend) Stats() map[string]interface{} {
	return map[string]interface{}{
		"size": m.size,
		"data_len": len(m.data),
	}
}

func (m *mockBackend) Resize(newSize int64) error {
	if newSize < 0 {
		return ublk.ErrInvalidParameters
	}
	if newSize != m.size {
		newData := make([]byte, newSize)
		copy(newData, m.data)
		m.data = newData
		m.size = newSize
	}
	return nil
}