package main

import (
	"fmt"
	"sync"

	"github.com/ehrlich-b/go-ublk"
)

// shardSize is the size of each memory shard (64KB).
// Provides good parallelism for 4K random I/O while keeping lock overhead reasonable.
const shardSize = 64 * 1024

// memoryBackend provides a RAM-based backend for ublk devices.
// Uses sharded locking to allow parallel I/O from multiple queues.
type memoryBackend struct {
	data   []byte
	size   int64
	shards []sync.RWMutex
}

func newMemoryBackend(size int64) *memoryBackend {
	numShards := (size + shardSize - 1) / shardSize
	return &memoryBackend{
		data:   make([]byte, size),
		size:   size,
		shards: make([]sync.RWMutex, numShards),
	}
}

func (m *memoryBackend) shardRange(off, length int64) (start, end int) {
	start = int(off / shardSize)
	end = int((off + length - 1) / shardSize)
	if end >= len(m.shards) {
		end = len(m.shards) - 1
	}
	return start, end
}

func (m *memoryBackend) ReadAt(p []byte, off int64) (int, error) {
	if off >= m.size {
		return 0, nil
	}

	available := m.size - off
	if int64(len(p)) > available {
		p = p[:available]
	}

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

func (m *memoryBackend) WriteAt(p []byte, off int64) (int, error) {
	if off >= m.size {
		return 0, fmt.Errorf("write beyond end of device")
	}

	available := m.size - off
	if int64(len(p)) > available {
		p = p[:available]
	}

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

func (m *memoryBackend) Size() int64 {
	return m.size
}

func (m *memoryBackend) Close() error {
	m.data = nil
	return nil
}

func (m *memoryBackend) Flush() error {
	return nil
}

func (m *memoryBackend) Discard(offset, length int64) error {
	if offset >= m.size {
		return nil
	}

	end := offset + length
	if end > m.size {
		end = m.size
	}
	actualLen := end - offset

	startShard, endShard := m.shardRange(offset, actualLen)
	for i := startShard; i <= endShard; i++ {
		m.shards[i].Lock()
	}

	clear(m.data[offset:end])

	for i := startShard; i <= endShard; i++ {
		m.shards[i].Unlock()
	}

	return nil
}

func (m *memoryBackend) WriteZeroes(offset, length int64) error {
	return m.Discard(offset, length)
}

// Compile-time interface checks
var (
	_ ublk.Backend            = (*memoryBackend)(nil)
	_ ublk.DiscardBackend     = (*memoryBackend)(nil)
	_ ublk.WriteZeroesBackend = (*memoryBackend)(nil)
)
