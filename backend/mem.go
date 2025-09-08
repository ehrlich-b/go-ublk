// Package backend provides standard ublk backend implementations
package backend

import (
	"fmt"
	"sync"

	"github.com/ehrlich-b/go-ublk/internal/interfaces"
)

// Memory provides a RAM-based backend for ublk devices
type Memory struct {
	data []byte
	size int64
	mu   sync.RWMutex
}

// NewMemory creates a new memory backend of the specified size
func NewMemory(size int64) *Memory {
	return &Memory{
		data: make([]byte, size),
		size: size,
	}
}

// ReadAt implements the Backend interface
func (m *Memory) ReadAt(p []byte, off int64) (int, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if off >= m.size {
		return 0, nil
	}

	// Calculate how much we can actually read
	available := m.size - off
	if int64(len(p)) > available {
		p = p[:available]
	}

	n := copy(p, m.data[off:off+int64(len(p))])
	return n, nil
}

// WriteAt implements the Backend interface
func (m *Memory) WriteAt(p []byte, off int64) (int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if off >= m.size {
		return 0, fmt.Errorf("write beyond end of device")
	}

	// Calculate how much we can actually write
	available := m.size - off
	if int64(len(p)) > available {
		p = p[:available]
	}

	n := copy(m.data[off:off+int64(len(p))], p)
	return n, nil
}

// Size implements the Backend interface
func (m *Memory) Size() int64 {
	return m.size
}

// Close implements the Backend interface
func (m *Memory) Close() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	
	// Clear the data to help with GC
	m.data = nil
	return nil
}

// Flush implements the Backend interface
func (m *Memory) Flush() error {
	// Memory backend doesn't need flushing
	return nil
}

// Discard implements the DiscardBackend interface
func (m *Memory) Discard(offset, length int64) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if offset >= m.size {
		return nil
	}

	end := offset + length
	if end > m.size {
		end = m.size
	}

	// Zero out the discarded region
	for i := offset; i < end; i++ {
		m.data[i] = 0
	}

	return nil
}

// WriteZeroes implements the WriteZeroesBackend interface
func (m *Memory) WriteZeroes(offset, length int64) error {
	return m.Discard(offset, length)
}

// Sync implements the SyncBackend interface
func (m *Memory) Sync() error {
	// Memory backend doesn't need syncing
	return nil
}

// SyncRange implements the SyncBackend interface  
func (m *Memory) SyncRange(offset, length int64) error {
	// Memory backend doesn't need syncing
	return nil
}

// Stats implements the StatBackend interface
func (m *Memory) Stats() map[string]interface{} {
	m.mu.RLock()
	defer m.mu.RUnlock()

	return map[string]interface{}{
		"type":   "memory",
		"size":   m.size,
		"allocated": len(m.data),
	}
}

// Compile-time interface checks
var (
	_ interfaces.Backend           = (*Memory)(nil)
	_ interfaces.DiscardBackend    = (*Memory)(nil)
	_ interfaces.WriteZeroesBackend = (*Memory)(nil)
	_ interfaces.SyncBackend       = (*Memory)(nil)
	_ interfaces.StatBackend       = (*Memory)(nil)
)