package ublk

import "sync"

// MockBackend provides a mock implementation of Backend for testing.
// It implements all optional interfaces and tracks method calls for verification.
type MockBackend struct {
	data     []byte
	size     int64
	closed   bool
	flushed  bool
	synced   bool
	stats    map[string]interface{}

	// Method call tracking
	mu sync.RWMutex
	readCalls  int
	writeCalls int
	flushCalls int
	syncCalls  int
}

// NewMockBackend creates a new mock backend with the specified size.
// This is useful for unit testing applications that use ublk backends.
func NewMockBackend(size int64) *MockBackend {
	return &MockBackend{
		data:  make([]byte, size),
		size:  size,
		stats: make(map[string]interface{}),
	}
}

// ReadAt implements the Backend interface
func (m *MockBackend) ReadAt(p []byte, off int64) (int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.readCalls++

	if m.closed {
		return 0, ErrDeviceNotFound
	}

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
func (m *MockBackend) WriteAt(p []byte, off int64) (int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.writeCalls++

	if m.closed {
		return 0, ErrDeviceNotFound
	}

	if off >= m.size {
		return 0, ErrInvalidParameters
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
func (m *MockBackend) Size() int64 {
	return m.size
}

// Close implements the Backend interface
func (m *MockBackend) Close() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.closed = true
	// Clear the data to help with GC
	m.data = nil
	return nil
}

// Flush implements the Backend interface
func (m *MockBackend) Flush() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.flushCalls++
	m.flushed = true
	return nil
}

// Discard implements the DiscardBackend interface
func (m *MockBackend) Discard(offset, length int64) error {
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
func (m *MockBackend) WriteZeroes(offset, length int64) error {
	return m.Discard(offset, length)
}

// Sync implements the SyncBackend interface
func (m *MockBackend) Sync() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.syncCalls++
	m.synced = true
	return nil
}

// SyncRange implements the SyncBackend interface
func (m *MockBackend) SyncRange(offset, length int64) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.syncCalls++
	m.synced = true
	return nil
}

// Stats implements the StatBackend interface
func (m *MockBackend) Stats() map[string]interface{} {
	m.mu.RLock()
	defer m.mu.RUnlock()

	stats := make(map[string]interface{})
	for k, v := range m.stats {
		stats[k] = v
	}

	stats["read_calls"] = m.readCalls
	stats["write_calls"] = m.writeCalls
	stats["flush_calls"] = m.flushCalls
	stats["sync_calls"] = m.syncCalls

	return stats
}

// Resize implements the ResizeBackend interface
func (m *MockBackend) Resize(newSize int64) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if newSize < 0 {
		return ErrInvalidParameters
	}

	if newSize > m.size {
		// Expand
		newData := make([]byte, newSize)
		copy(newData, m.data)
		m.data = newData
	} else if newSize < m.size {
		// Truncate
		m.data = m.data[:newSize]
	}

	m.size = newSize
	return nil
}

// Testing utility methods

// IsClosed returns true if the backend has been closed
func (m *MockBackend) IsClosed() bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.closed
}

// IsFlushed returns true if Flush has been called
func (m *MockBackend) IsFlushed() bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.flushed
}

// IsSynced returns true if Sync or SyncRange has been called
func (m *MockBackend) IsSynced() bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.synced
}

// CallCounts returns the number of times each method has been called
func (m *MockBackend) CallCounts() map[string]int {
	m.mu.RLock()
	defer m.mu.RUnlock()

	return map[string]int{
		"read":  m.readCalls,
		"write": m.writeCalls,
		"flush": m.flushCalls,
		"sync":  m.syncCalls,
	}
}

// Reset resets all call counters and state flags
func (m *MockBackend) Reset() {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.readCalls = 0
	m.writeCalls = 0
	m.flushCalls = 0
	m.syncCalls = 0
	m.flushed = false
	m.synced = false
}

// SetCustomStats allows setting custom statistics for testing
func (m *MockBackend) SetCustomStats(stats map[string]interface{}) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.stats = make(map[string]interface{})
	for k, v := range stats {
		m.stats[k] = v
	}
}

// Compile-time interface checks
var (
	_ Backend            = (*MockBackend)(nil)
	_ DiscardBackend     = (*MockBackend)(nil)
	_ WriteZeroesBackend = (*MockBackend)(nil)
	_ SyncBackend        = (*MockBackend)(nil)
	_ StatBackend        = (*MockBackend)(nil)
	_ ResizeBackend      = (*MockBackend)(nil)
)