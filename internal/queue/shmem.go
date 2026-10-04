package queue

import "sync"

// SharedMemory is the set of server memory regions registered with the
// kernel for shared-memory zero copy (UBLK_F_SHMEM_ZC, REG_BUF). A request
// whose pages all lie in one region arrives with FlagSharedMemory and a
// descriptor address of index<<32 | offset; its Data then points straight into
// that region. One table serves all of a device's queues.
type SharedMemory struct {
	mu      sync.RWMutex
	regions map[uint16][]byte
}

// Add records region index once the kernel has registered it.
func (s *SharedMemory) Add(index uint16, mem []byte) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.regions == nil {
		s.regions = make(map[uint16][]byte)
	}
	s.regions[index] = mem
}

// Remove forgets region index, after the kernel has unregistered it.
func (s *SharedMemory) Remove(index uint16) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.regions, index)
}

// slice resolves a descriptor address to the request's bytes in a region.
func (s *SharedMemory) slice(addr uint64, length int64) ([]byte, bool) {
	index, off := uint16(addr>>32), int64(uint32(addr))
	s.mu.RLock()
	mem, ok := s.regions[index]
	s.mu.RUnlock()
	if !ok || off+length > int64(len(mem)) {
		return nil, false
	}
	return mem[off : off+length : off+length], true
}
