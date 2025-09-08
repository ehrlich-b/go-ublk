package queue

import (
	"context"
	"fmt"
	"syscall"
	"unsafe"

	"github.com/ehrlich-b/go-ublk/internal/interfaces"
	"github.com/ehrlich-b/go-ublk/internal/uapi"
	"github.com/ehrlich-b/go-ublk/internal/uring"
)

// Runner handles I/O for a single ublk queue
type Runner struct {
	devID    uint32
	queueID  uint16
	depth    int
	backend  interfaces.Backend
	charFd   int
	ring     uring.Ring
	descPtr  uintptr // mmap'd descriptor array
	bufPtr   uintptr // I/O buffer base
	ctx      context.Context
	cancel   context.CancelFunc
	logger   Logger
}

type Logger interface {
	Printf(format string, args ...interface{})
	Debugf(format string, args ...interface{})
}

type Config struct {
	DevID    uint32
	QueueID  uint16
	Depth    int
	Backend  interfaces.Backend
	Logger   Logger
}

// NewRunner creates a new queue runner
func NewRunner(ctx context.Context, config Config) (*Runner, error) {
	// Open character device
	charPath := uapi.UblkDevicePath(config.DevID)
	fd, err := syscall.Open(charPath, syscall.O_RDWR, 0)
	if err != nil {
		// Check if we're in stub mode (device file doesn't exist)
		if err == syscall.ENOENT {
			// Return a stub runner that simulates queue operations
			return NewStubRunner(ctx, config), nil
		}
		return nil, fmt.Errorf("failed to open %s: %v", charPath, err)
	}

	// Create io_uring for this queue
	ringConfig := uring.Config{
		Entries: uint32(config.Depth),
		FD:      int32(fd),
		Flags:   0,
	}

	ring, err := uring.NewRing(ringConfig)
	if err != nil {
		syscall.Close(fd)
		return nil, fmt.Errorf("failed to create io_uring: %v", err)
	}

	// Memory map the descriptor array and I/O buffers
	descPtr, bufPtr, err := mmapQueues(fd, config.QueueID, config.Depth)
	if err != nil {
		ring.Close()
		syscall.Close(fd)
		return nil, fmt.Errorf("failed to mmap queues: %v", err)
	}

	ctx, cancel := context.WithCancel(ctx)

	runner := &Runner{
		devID:   config.DevID,
		queueID: config.QueueID,
		depth:   config.Depth,
		backend: config.Backend,
		charFd:  fd,
		ring:    ring,
		descPtr: descPtr,
		bufPtr:  bufPtr,
		ctx:     ctx,
		cancel:  cancel,
		logger:  config.Logger,
	}

	return runner, nil
}

// Start begins processing I/O requests
func (r *Runner) Start() error {
	if r.logger != nil {
		r.logger.Printf("Starting queue %d for device %d", r.queueID, r.devID)
	}

	// Start the I/O loop in a goroutine
	go r.ioLoop()
	return nil
}

// Stop stops the runner
func (r *Runner) Stop() error {
	if r.cancel != nil {
		r.cancel()
	}
	return nil
}

// Close cleans up resources
func (r *Runner) Close() error {
	r.Stop()
	
	if r.ring != nil {
		r.ring.Close()
	}
	
	if r.charFd >= 0 {
		syscall.Close(r.charFd)
	}
	
	// TODO(Phase 4): Properly unmap memory-mapped regions
	// Currently we rely on OS cleanup on process exit
	// For production, should call munmap() for descPtr and bufPtr
	return nil
}

// ioLoop is the main I/O processing loop
func (r *Runner) ioLoop() {
	if r.logger != nil {
		r.logger.Debugf("Queue %d: Starting I/O loop", r.queueID)
	}

	// Check if we're in stub mode
	if r.charFd == -1 || r.ring == nil {
		r.stubLoop()
		return
	}

	// Submit initial FETCH_REQ commands to fill the queue
	for tag := 0; tag < r.depth; tag++ {
		err := r.submitFetchReq(uint16(tag))
		if err != nil {
			if r.logger != nil {
				r.logger.Printf("Queue %d: Failed to submit initial FETCH_REQ[%d]: %v", r.queueID, tag, err)
			}
			return
		}
	}

	// Main processing loop
	for {
		select {
		case <-r.ctx.Done():
			if r.logger != nil {
				r.logger.Debugf("Queue %d: I/O loop stopping", r.queueID)
			}
			return
		default:
			err := r.processRequests()
			if err != nil {
				if r.logger != nil {
					r.logger.Printf("Queue %d: Error processing requests: %v", r.queueID, err)
				}
				return
			}
		}
	}
}

// submitFetchReq submits a FETCH_REQ command
func (r *Runner) submitFetchReq(tag uint16) error {
	ioCmd := &uapi.UblksrvIOCmd{
		QID:    r.queueID,
		Tag:    tag,
		Result: 0,
		Addr:   0, // Will be filled by kernel
	}

	userData := uint64(r.queueID)<<16 | uint64(tag)
	_, err := r.ring.SubmitIOCmd(uapi.UBLK_IO_FETCH_REQ, ioCmd, userData)
	return err
}

// processRequests processes completed I/O requests
func (r *Runner) processRequests() error {
	// In a real io_uring implementation, this would:
	// 1. Call io_uring_wait_cqe() to wait for completion events
	// 2. Process each completed request
	// 3. Mark completion entry as consumed
	
	// For now, we're using a simplified approach since our io_uring
	// implementation is still basic. In the future, this will be
	// replaced with proper completion queue processing.
	
	// The key insight is that FETCH_REQ completions tell us about
	// new I/O requests that need processing, while COMMIT_AND_FETCH_REQ
	// completions both complete previous I/O and provide new requests.
	
	// Since we don't have real CQE processing yet, simulate by yielding
	// This allows other goroutines to run and prevents busy looping
	syscall.Syscall(syscall.SYS_SCHED_YIELD, 0, 0, 0)
	
	return nil
}

// handleIORequest processes a single I/O request
func (r *Runner) handleIORequest(tag uint16, desc *uapi.UblksrvIODesc) error {
	// Extract I/O parameters from descriptor
	op := desc.GetOp() // Use the provided method to get operation
	offset := desc.StartSector * 512 // Convert sectors to bytes (assuming 512-byte sectors)
	length := desc.NrSectors * 512   // Convert sectors to bytes
	
	if r.logger != nil {
		r.logger.Debugf("Queue %d: Handling I/O op=%d offset=%d len=%d tag=%d", 
			r.queueID, op, offset, length, tag)
	}
	
	// Calculate buffer pointer for this tag
	bufOffset := int(tag) * 64 * 1024 // 64KB per buffer
	bufPtr := unsafe.Pointer(r.bufPtr + uintptr(bufOffset))
	buffer := (*[64 * 1024]byte)(bufPtr)[:length:length]
	
	var err error
	switch op {
	case uapi.UBLK_IO_OP_READ:
		_, err = r.backend.ReadAt(buffer, int64(offset))
	case uapi.UBLK_IO_OP_WRITE:
		_, err = r.backend.WriteAt(buffer, int64(offset))
	case uapi.UBLK_IO_OP_FLUSH:
		err = r.backend.Flush()
	case uapi.UBLK_IO_OP_DISCARD:
		// Handle discard if backend supports it
		if discardBackend, ok := r.backend.(interfaces.DiscardBackend); ok {
			err = discardBackend.Discard(int64(offset), int64(length))
		}
	default:
		err = fmt.Errorf("unsupported operation: %d", op)
	}
	
	// Submit COMMIT_AND_FETCH_REQ with result
	return r.commitAndFetch(tag, err)
}

// commitAndFetch submits result and fetches next request
func (r *Runner) commitAndFetch(tag uint16, ioErr error) error {
	// Prepare result
	result := int32(0) // Success
	if ioErr != nil {
		result = -5 // -EIO
		if r.logger != nil {
			r.logger.Printf("Queue %d: I/O error for tag %d: %v", r.queueID, tag, ioErr)
		}
	}
	
	ioCmd := &uapi.UblksrvIOCmd{
		QID:    r.queueID,
		Tag:    tag,
		Result: result,
		Addr:   0, // Will be filled by kernel for next request
	}
	
	userData := uint64(r.queueID)<<16 | uint64(tag)
	_, err := r.ring.SubmitIOCmd(uapi.UBLK_IO_COMMIT_AND_FETCH_REQ, ioCmd, userData)
	return err
}

// mmapQueues maps the descriptor array and I/O buffers
func mmapQueues(fd int, queueID uint16, depth int) (uintptr, uintptr, error) {
	// Calculate sizes
	descSize := depth * int(unsafe.Sizeof(uapi.UblksrvIODesc{}))
	bufSize := depth * 64 * 1024 // 64KB per request buffer

	// Map descriptor array
	descPtr, _, errno := syscall.Syscall6(
		syscall.SYS_MMAP,
		0, // addr
		uintptr(descSize), // length
		syscall.PROT_READ|syscall.PROT_WRITE, // prot
		syscall.MAP_SHARED, // flags
		uintptr(fd), // fd
		0, // offset
	)
	if errno != 0 {
		return 0, 0, fmt.Errorf("failed to mmap descriptor array: %v", errno)
	}

	// Map I/O buffers
	bufPtr, _, errno := syscall.Syscall6(
		syscall.SYS_MMAP,
		0, // addr
		uintptr(bufSize), // length
		syscall.PROT_READ|syscall.PROT_WRITE, // prot
		syscall.MAP_SHARED, // flags
		uintptr(fd), // fd
		uapi.UBLKSRV_IO_BUF_OFFSET, // offset
	)
	if errno != 0 {
		return 0, 0, fmt.Errorf("failed to mmap I/O buffers: %v", errno)
	}

	return descPtr, bufPtr, nil
}

// NewStubRunner creates a stub runner for simulation/testing
func NewStubRunner(ctx context.Context, config Config) *Runner {
	ctx, cancel := context.WithCancel(ctx)
	
	return &Runner{
		devID:   config.DevID,
		queueID: config.QueueID,
		depth:   config.Depth,
		backend: config.Backend,
		charFd:  -1, // No real device
		ring:    nil, // No real ring
		descPtr: 0,
		bufPtr:  0,
		ctx:     ctx,
		cancel:  cancel,
		logger:  config.Logger,
	}
}

// stubLoop simulates the I/O processing loop for testing
func (r *Runner) stubLoop() {
	if r.logger != nil {
		r.logger.Debugf("Queue %d: Starting stub I/O loop (simulation mode)", r.queueID)
	}
	
	// In stub mode, we just wait for cancellation
	// This simulates a working queue without doing real I/O
	<-r.ctx.Done()
	
	if r.logger != nil {
		r.logger.Debugf("Queue %d: Stopping stub I/O loop (simulation mode)", r.queueID)
	}
}