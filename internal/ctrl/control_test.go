package ctrl

import (
	"fmt"
	"testing"

	"github.com/ehrlich-b/go-ublk/internal/uapi"
)

// Mock backend for testing
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
		return 0, fmt.Errorf("invalid parameters")
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

func TestDefaultDeviceParams(t *testing.T) {
	backend := &mockBackend{
		data: make([]byte, 1024),
		size: 1024,
	}

	params := DefaultDeviceParams(backend)

	if params.Backend != backend {
		t.Error("Backend not set correctly")
	}

	if params.QueueDepth != 128 {
		t.Errorf("QueueDepth = %d, want 128", params.QueueDepth)
	}

	if params.LogicalBlockSize != 512 {
		t.Errorf("LogicalBlockSize = %d, want 512", params.LogicalBlockSize)
	}

	if params.MaxIOSize != 1<<20 {
		t.Errorf("MaxIOSize = %d, want %d", params.MaxIOSize, 1<<20)
	}

	if params.DeviceID != -1 {
		t.Errorf("DeviceID = %d, want -1", params.DeviceID)
	}
}

func TestSizeToShift(t *testing.T) {
	tests := []struct {
		size     int
		expected int
	}{
		{512, 9},   // 2^9 = 512
		{1024, 10}, // 2^10 = 1024
		{4096, 12}, // 2^12 = 4096
		{1, 0},     // 2^0 = 1
		{2, 1},     // 2^1 = 2
	}

	for _, test := range tests {
		result := sizeToShift(test.size)
		if result != test.expected {
			t.Errorf("sizeToShift(%d) = %d, want %d", test.size, result, test.expected)
		}
	}
}

func TestBuildFeatureFlags(t *testing.T) {
	c := &Controller{}
	backend := &mockBackend{data: make([]byte, 1024), size: 1024}

	params := DefaultDeviceParams(backend)
	flags := c.buildFeatureFlags(&params)

	// Default params should have no flags set
	if flags != 0 {
		t.Errorf("Default flags = %d, want 0", flags)
	}

	// Test zero-copy flag
	params.EnableZeroCopy = true
	flags = c.buildFeatureFlags(&params)
	if (flags & uapi.UBLK_F_SUPPORT_ZERO_COPY) == 0 {
		t.Error("Zero-copy flag not set")
	}

	// Test unprivileged flag
	params.EnableZeroCopy = false
	params.EnableUnprivileged = true
	flags = c.buildFeatureFlags(&params)
	if (flags & uapi.UBLK_F_UNPRIVILEGED_DEV) == 0 {
		t.Error("Unprivileged flag not set")
	}

	// Test user-copy flag
	params.EnableUnprivileged = false
	params.EnableUserCopy = true
	flags = c.buildFeatureFlags(&params)
	if (flags & uapi.UBLK_F_USER_COPY) == 0 {
		t.Error("User-copy flag not set")
	}
}

func TestDeviceInfo(t *testing.T) {
	info := &DeviceInfo{
		ID:         1,
		BlockSize:  512,
		DevSectors: 2048,
	}

	expectedSize := int64(2048 * 512)
	if info.Size() != expectedSize {
		t.Errorf("Size() = %d, want %d", info.Size(), expectedSize)
	}
}

// Note: We can't test the actual Controller methods without root privileges
// and ublk kernel support. These would go in integration tests.
func TestControllerInterface(t *testing.T) {
	// Test that Controller satisfies expected interface behavior
	// This is mainly a compile-time check

	var c *Controller

	// Verify methods exist and have correct signatures
	if c != nil {
		_ = c.Close()
	}

	// Test that we can create device params
	backend := &mockBackend{data: make([]byte, 1024), size: 1024}
	params := DefaultDeviceParams(backend)

	if params.Backend == nil {
		t.Error("Backend should not be nil")
	}
}

func BenchmarkSizeToShift(b *testing.B) {
	for i := 0; i < b.N; i++ {
		sizeToShift(4096)
	}
}

func BenchmarkBuildFeatureFlags(b *testing.B) {
	c := &Controller{}
	backend := &mockBackend{data: make([]byte, 1024), size: 1024}
	params := DefaultDeviceParams(backend)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		c.buildFeatureFlags(&params)
	}
}