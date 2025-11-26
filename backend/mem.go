// Package backend provides standard ublk backend implementations
package backend

import (
	"fmt"
	"sync"

	"github.com/ehrlich-b/go-ublk"
)

// ShardSize is the size of each memory shard (64KB)
// This provides good parallelism for 4K random I/O while keeping lock overhead reasonable.
// With 64KB shards, a 256MB device has 4096 shards.
const ShardSize = 64 * 1024

// Memory provides a RAM-based backend for ublk devices.
// It uses sharded locking to allow parallel I/O from multiple queues.
type Memory struct {
	data   []byte
	size   int64
	shards []sync.RWMutex
}

// NewMemory creates a new memory backend of the specified size
func NewMemory(size int64) *Memory {
	numShards := (size + ShardSize - 1) / ShardSize
	return &Memory{
		data:   make([]byte, size),
		size:   size,
		shards: make([]sync.RWMutex, numShards),
	}
}

// shardRange returns the range of shards that cover [off, off+len)
func (m *Memory) shardRange(off, length int64) (start, end int) {
	start = int(off / ShardSize)
	end = int((off + length - 1) / ShardSize)
	if end >= len(m.shards) {
		end = len(m.shards) - 1
	}
	return start, end
}

// ReadAt implements the Backend interface
func (m *Memory) ReadAt(p []byte, off int64) (int, error) {
	if off >= m.size {
		return 0, nil
	}

	// Calculate how much we can actually read
	available := m.size - off
	if int64(len(p)) > available {
		p = p[:available]
	}

	// Lock only the shards we need (for reads, use RLock)
	startShard, endShard := m.shardRange(off, int64(len(p)))
	for i := startShard; i <= endShard; i++ {
		m.shards[i].RLock()
	}

	n := copy(p, m.data[off:off+int64(len(p))])

	for i := startShard; i <= endShard; i++ {
		m.shards[i].RUnlock()
	}

	return n, nil
}

// WriteAt implements the Backend interface
func (m *Memory) WriteAt(p []byte, off int64) (int, error) {
	if off >= m.size {
		return 0, fmt.Errorf("write beyond end of device")
	}

	// Calculate how much we can actually write
	available := m.size - off
	if int64(len(p)) > available {
		p = p[:available]
	}

	// Lock only the shards we need
	startShard, endShard := m.shardRange(off, int64(len(p)))
	for i := startShard; i <= endShard; i++ {
		m.shards[i].Lock()
	}

	n := copy(m.data[off:off+int64(len(p))], p)

	for i := startShard; i <= endShard; i++ {
		m.shards[i].Unlock()
	}

	return n, nil
}

// Size implements the Backend interface
func (m *Memory) Size() int64 {
	return m.size
}

// Close implements the Backend interface
func (m *Memory) Close() error {
	// No need to lock all shards - just clear the data
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
	if offset >= m.size {
		return nil
	}

	end := offset + length
	if end > m.size {
		end = m.size
	}
	actualLen := end - offset

	// Lock only the shards we need
	startShard, endShard := m.shardRange(offset, actualLen)
	for i := startShard; i <= endShard; i++ {
		m.shards[i].Lock()
	}

	// Zero out the discarded region
	for i := offset; i < end; i++ {
		m.data[i] = 0
	}

	for i := startShard; i <= endShard; i++ {
		m.shards[i].Unlock()
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
	return map[string]interface{}{
		"type":       "memory",
		"size":       m.size,
		"allocated":  len(m.data),
		"num_shards": len(m.shards),
		"shard_size": ShardSize,
	}
}

// Compile-time interface checks
var (
	_ ublk.Backend            = (*Memory)(nil)
	_ ublk.DiscardBackend     = (*Memory)(nil)
	_ ublk.WriteZeroesBackend = (*Memory)(nil)
	_ ublk.SyncBackend        = (*Memory)(nil)
	_ ublk.StatBackend        = (*Memory)(nil)
)
