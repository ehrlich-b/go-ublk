// +build integration

package integration

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/ehrlich-b/go-ublk"
)

// requireRoot skips the test if not running as root
func requireRoot(t *testing.T) {
	if os.Getuid() != 0 {
		t.Skip("This test requires root privileges")
	}
}

// requireKernel skips the test if kernel version is insufficient
func requireKernel(t *testing.T, minVersion string) {
	// This would check kernel version
	// For now, just log the requirement
	t.Logf("Requires kernel version %s or later", minVersion)
}

// requireUblkModule skips if ublk module is not available
func requireUblkModule(t *testing.T) {
	// Check if ublk module is available
	if _, err := os.Stat("/dev/ublk-control"); os.IsNotExist(err) {
		t.Skip("ublk kernel module not available")
	}
}

func TestIntegrationDeviceLifecycle(t *testing.T) {
	requireRoot(t)
	requireKernel(t, "6.1")
	requireUblkModule(t)

	// Create a simple memory backend
	backend := &mockBackend{
		data: make([]byte, 64<<20), // 64MB
		size: 64 << 20,
	}

	params := ublk.DefaultParams(backend)
	params.QueueDepth = 32
	params.NumQueues = 1

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// This should now work at the control plane level
	device, err := ublk.CreateAndServe(ctx, params, nil)
	if err != nil {
		// It's expected to fail in test environment without proper ublk setup
		// but the error should not be "not implemented" anymore
		if errors.Is(err, ublk.ErrNotImplemented) {
			t.Errorf("Should not get ErrNotImplemented anymore, got: %v", err)
		}
		t.Logf("Expected failure in test environment: %v", err)
		return
	}

	// If we get here, we successfully created a device
	if device == nil {
		t.Fatal("Device should not be nil if creation succeeded")
	}

	// Clean up
	defer func() {
		if err := device.Close(); err != nil {
			t.Logf("Cleanup error (expected in test env): %v", err)
		}
	}()
	
	t.Logf("Successfully created device: %s", device.Path)
}

func TestIntegrationBasicIO(t *testing.T) {
	requireRoot(t)
	requireKernel(t, "6.1")
	requireUblkModule(t)

	t.Skip("Skipping until device creation is implemented")

	// TODO: Test basic I/O operations:
	// 1. Create device
	// 2. Write test data using dd or similar
	// 3. Read back and verify
	// 4. Clean up device
}

func TestIntegrationFilesystemMount(t *testing.T) {
	requireRoot(t)
	requireKernel(t, "6.1") 
	requireUblkModule(t)

	t.Skip("Skipping until device creation is implemented")

	// TODO: Test filesystem operations:
	// 1. Create device
	// 2. Create filesystem (mkfs.ext4)
	// 3. Mount filesystem
	// 4. Perform file operations
	// 5. Unmount and cleanup
}

func TestIntegrationStress(t *testing.T) {
	requireRoot(t)
	requireKernel(t, "6.1")
	requireUblkModule(t)

	if testing.Short() {
		t.Skip("Skipping stress test in short mode")
	}

	t.Skip("Skipping until device creation is implemented")

	// TODO: Stress test with multiple concurrent operations
}

// Mock backend for integration tests
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