package queue

import (
	"context"
	"fmt"
	"syscall"
	"unsafe"

	"github.com/ehrlich-b/go-ublk/internal/interfaces"
	"github.com/ehrlich-b/go-ublk/internal/logging"
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
	// The character device (/dev/ublkcN) gets created by the kernel AFTER
	// we start submitting FETCH_REQ commands. So we need to retry opening it.
	charPath := uapi.UblkDevicePath(config.DevID)
	
	// Try to open the character device with retries
	var fd int
	var err error
	maxRetries := 10
	for i := 0; i < maxRetries; i++ {
		fd, err = syscall.Open(charPath, syscall.O_RDWR, 0)
		if err == nil {
			break
		}
		
		if err == syscall.ENOENT {
			// Device doesn't exist yet - this is expected initially
			// Return a special "waiting" runner that will start the data plane
			return NewWaitingRunner(ctx, config), nil
		} else {
			return nil, fmt.Errorf("failed to open %s: %v", charPath, err)
		}
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
	
	// Unmap memory-mapped regions
	if r.descPtr != 0 {
		descSize := r.depth * int(unsafe.Sizeof(uapi.UblksrvIODesc{}))
		syscall.Syscall(syscall.SYS_MUNMAP, r.descPtr, uintptr(descSize), 0)
		r.descPtr = 0
	}
	
	if r.bufPtr != 0 {
		bufSize := r.depth * 64 * 1024 // 64KB per request buffer
		syscall.Syscall(syscall.SYS_MUNMAP, r.bufPtr, uintptr(bufSize), 0)
		r.bufPtr = 0
	}
	
	if r.charFd >= 0 {
		syscall.Close(r.charFd)
		r.charFd = -1
	}
	
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

// waitAndStartDataPlane waits for the character device to appear and starts the data plane
func (r *Runner) waitAndStartDataPlane() {
	logger := logging.Default().WithDevice(int(r.devID)).WithQueue(int(r.queueID))
	logger.Info("waiting for character device to appear")
	
	charPath := uapi.UblkDevicePath(r.devID)
	
	// Wait for the character device to appear with longer timeout
	maxWait := 30 // 30 seconds
	for i := 0; i < maxWait; i++ {
		select {
		case <-r.ctx.Done():
			logger.Info("context cancelled while waiting for device")
			return
		default:
		}
		
		// Try to open the character device
		fd, err := syscall.Open(charPath, syscall.O_RDWR, 0)
		if err == nil {
			logger.Info("character device appeared, starting data plane", "char_path", charPath)
			
			// Initialize the real data plane
			err = r.initializeDataPlane(fd)
			if err != nil {
				logger.Error("failed to initialize data plane", "error", err)
				syscall.Close(fd)
				return
			}
			
			// Start processing requests
			go func() {
				err := r.processRequests()
				if err != nil {
					logger.Error("data plane processing failed", "error", err)
				}
			}()
			return
		}
		
		if err != syscall.ENOENT {
			logger.Error("failed to open character device", "error", err, "char_path", charPath)
			return
		}
		
		// Wait 1 second before retrying
		syscall.Syscall(syscall.SYS_NANOSLEEP, uintptr(unsafe.Pointer(&syscall.Timespec{Sec: 1})), 0, 0)
	}
	
	logger.Error("character device never appeared", "char_path", charPath)
}

// initializeDataPlane sets up the io_uring and memory mapping for the data plane
func (r *Runner) initializeDataPlane(fd int) error {
	logger := logging.Default().WithDevice(int(r.devID)).WithQueue(int(r.queueID))
	logger.Debug("initializing data plane", "fd", fd)
	
	// Create io_uring for this queue
	ringConfig := uring.Config{
		Entries: uint32(r.depth),
		FD:      int32(fd),
		Flags:   0,
	}

	ring, err := uring.NewRing(ringConfig)
	if err != nil {
		return fmt.Errorf("failed to create io_uring: %v", err)
	}

	// Memory map the descriptor array and I/O buffers
	descPtr, bufPtr, err := mmapQueues(fd, r.queueID, r.depth)
	if err != nil {
		ring.Close()
		return fmt.Errorf("failed to mmap queues: %v", err)
	}
	
	// Update runner with initialized resources
	r.charFd = fd
	r.ring = ring
	r.descPtr = descPtr
	r.bufPtr = bufPtr
	
	logger.Info("data plane initialized successfully")
	return nil
}

// processRequests processes completed I/O requests
func (r *Runner) processRequests() error {
	// Wait for completion events from io_uring
	// Use a short timeout to avoid blocking forever
	completions, err := r.ring.WaitForCompletion(100) // 100ms timeout
	if err != nil {
		return fmt.Errorf("failed to wait for completions: %w", err)
	}
	
	// Process each completion event
	for _, completion := range completions {
		// Extract tag from user data
		userData := completion.UserData()
		tag := uint16(userData & 0xFFFF)
		
		if r.logger != nil && completion.Value() != 0 {
			r.logger.Printf("Queue %d: Completion for tag %d: result=%d", 
				r.queueID, tag, completion.Value())
		}
		
		// For FETCH_REQ completions, we get new I/O requests to process
		if completion.Value() == 0 { // Success
			// Get descriptor for this tag
			if tag >= uint16(r.depth) {
				if r.logger != nil {
					r.logger.Printf("Queue %d: Invalid tag %d (depth=%d)", r.queueID, tag, r.depth)
				}
				continue
			}
			
			// Access memory-mapped descriptor array
			descSize := int(unsafe.Sizeof(uapi.UblksrvIODesc{}))
			descPtr := unsafe.Pointer(r.descPtr + uintptr(int(tag)*descSize))
			desc := (*uapi.UblksrvIODesc)(descPtr)
			
			// Process the I/O request
			if err := r.handleIORequest(tag, desc); err != nil {
				if r.logger != nil {
					r.logger.Printf("Queue %d: Failed to handle I/O for tag %d: %v", r.queueID, tag, err)
				}
				// Continue processing other requests even if one fails
			}
		}
	}
	
	// If no completions, yield briefly to avoid busy looping
	if len(completions) == 0 {
		syscall.Syscall(syscall.SYS_SCHED_YIELD, 0, 0, 0)
	}
	
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
// NewWaitingRunner creates a runner that waits for device creation and starts data plane
func NewWaitingRunner(ctx context.Context, config Config) *Runner {
	ctx, cancel := context.WithCancel(ctx)
	
	runner := &Runner{
		devID:   config.DevID,
		queueID: config.QueueID,  
		depth:   config.Depth,
		backend: config.Backend,
		charFd:  -1, // No device yet
		ring:    nil, // No ring yet
		descPtr: 0,   // No mmap yet
		bufPtr:  0,   // No buffers yet
		ctx:     ctx,
		cancel:  cancel,
		logger:  config.Logger,
	}
	
	// Start a goroutine that will initialize the real data plane once the device appears
	go runner.waitAndStartDataPlane()
	
	return runner
}

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