package queue

import (
	"context"
	"fmt"
	"syscall"
	"time"
	"unsafe"

	"github.com/ehrlich-b/go-ublk/internal/interfaces"
	"github.com/ehrlich-b/go-ublk/internal/logging"
	"github.com/ehrlich-b/go-ublk/internal/uapi"
	"github.com/ehrlich-b/go-ublk/internal/uring"
)

// Runner handles I/O for a single ublk queue
type Runner struct {
	devID   uint32
	queueID uint16
	depth   int
	backend interfaces.Backend
	charFd  int
	ring    uring.Ring
	descPtr uintptr // mmap'd descriptor array
	bufPtr  uintptr // I/O buffer base
	ctx     context.Context
	cancel  context.CancelFunc
	logger  Logger
}

type Logger interface {
	Printf(format string, args ...interface{})
	Debugf(format string, args ...interface{})
}

type Config struct {
	DevID   uint32
	QueueID uint16
	Depth   int
	Backend interfaces.Backend
	Logger  Logger
}

// NewRunner creates a new queue runner
func NewRunner(ctx context.Context, config Config) (*Runner, error) {
	fmt.Printf("*** DEBUG: NewRunner called for dev %d queue %d\n", config.DevID, config.QueueID)

	// The character device (/dev/ublkcN) should exist after ADD_DEV.
	// We may need to retry briefly until udev creates the node.
	charPath := uapi.UblkDevicePath(config.DevID)
	fmt.Printf("*** DEBUG: Opening character device %s\n", charPath)

	var fd int
	var err error
	const maxRetries = 50 // up to ~5s with 100ms sleep
	for i := 0; i < maxRetries; i++ {
		fd, err = syscall.Open(charPath, syscall.O_RDWR, 0)
		if err == nil {
			fmt.Printf("*** DEBUG: Opened %s successfully, fd=%d\n", charPath, fd)
			break
		}
		if err != syscall.ENOENT {
			return nil, fmt.Errorf("failed to open %s: %v", charPath, err)
		}
		// Sleep 100ms and retry
		ts := syscall.Timespec{Sec: 0, Nsec: 100 * 1_000_000}
		syscall.Nanosleep(&ts, nil)
	}
	if err != nil {
		return nil, fmt.Errorf("character device did not appear: %s", charPath)
	}

	// Create io_uring for this queue
	ringConfig := uring.Config{
		Entries: uint32(config.Depth),
		FD:      int32(fd),
		Flags:   0,
	}

	fmt.Printf("*** DEBUG: Creating io_uring for queue with fd=%d\n", fd)
	ring, err := uring.NewRing(ringConfig)
	if err != nil {
		syscall.Close(fd)
		return nil, fmt.Errorf("failed to create io_uring: %v", err)
	}
	fmt.Printf("*** DEBUG: io_uring created successfully for queue\n")

	// Memory map the descriptor array and I/O buffers
	fmt.Printf("*** DEBUG: About to call mmapQueues for fd=%d\n", fd)
	descPtr, bufPtr, err := mmapQueues(fd, config.QueueID, config.Depth)
	if err != nil {
		fmt.Printf("*** DEBUG: mmapQueues failed: %v\n", err)
		ring.Close()
		syscall.Close(fd)
		return nil, fmt.Errorf("failed to mmap queues: %v", err)
	}
	fmt.Printf("*** DEBUG: mmapQueues succeeded\n")

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

// Prime submits initial FETCH_REQ commands to fill the queue.
// Can now handle START_DEV in progress by checking for EOPNOTSUPP.
func (r *Runner) Prime() error {
	if r.charFd < 0 || r.ring == nil {
		return fmt.Errorf("runner not initialized")
	}

	// Submit FETCH_REQ for each tag
	for tag := 0; tag < r.depth; tag++ {
		if err := r.submitFetchReq(uint16(tag)); err != nil {
			// If we get EOPNOTSUPP, START_DEV might not be ready yet
			if errno, ok := err.(syscall.Errno); ok && errno == syscall.EOPNOTSUPP {
				// This is expected if START_DEV hasn't been processed yet
				// The queue runner loop will retry
				return fmt.Errorf("device not ready (START_DEV pending): %w", err)
			}
			return fmt.Errorf("submit initial FETCH_REQ[%d]: %w", tag, err)
		}
	}
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

	// CRITICAL: Queue is ready - the io_uring exists and is associated with the char device
	// The kernel can now see this queue exists
	if r.logger != nil {
		r.logger.Printf("Queue %d: Queue io_uring ready", r.queueID)
	}

	// Wait for START_DEV to be submitted
	time.Sleep(300 * time.Millisecond) // Give time for START_DEV to be submitted

	// Add retry logic for initial priming
	primed := false
	retryCount := 0

	for !primed && retryCount < 200 { // Try for up to 20 seconds
		if err := r.Prime(); err != nil {
			// Keep retrying on any error - START_DEV might still be processing
			time.Sleep(100 * time.Millisecond)
			retryCount++
			// Log progress every 10 retries (1 second)
			if retryCount%10 == 0 {
				if r.logger != nil {
					r.logger.Printf("Queue %d: Still waiting for START_DEV (retry %d)", r.queueID, retryCount)
				}
			}
			continue
		}
		primed = true
	}

	if !primed {
		if r.logger != nil {
			r.logger.Printf("Queue %d: Failed to prime queue after %d retries", r.queueID, retryCount)
		}
		return
	}

	if r.logger != nil {
		r.logger.Printf("Queue %d: Successfully primed after %d retries", r.queueID, retryCount)
	}

	// Continue with normal I/O processing loop
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
		// Provide userspace buffer address for this tag
		Addr: uint64(r.bufPtr + uintptr(int(tag)*64*1024)),
	}

	userData := uint64(r.queueID)<<16 | uint64(tag)
	// Use IOCTL-encoded command to avoid -EOPNOTSUPP
	cmd := uapi.UblkIOCmd(uapi.UBLK_IO_FETCH_REQ)
	_, err := r.ring.SubmitIOCmd(cmd, ioCmd, userData)
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

			// Main processing loop
			go func() {
				for {
					select {
					case <-r.ctx.Done():
						logger.Info("data plane loop stopped via context")
						return
					default:
						if err := r.processRequests(); err != nil {
							logger.Error("data plane processing failed", "error", err)
							return
						}
					}
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
		userData := completion.UserData()
		tag := uint16(userData & 0xFFFF)
		result := completion.Value()

		switch result {
		case uapi.UBLK_IO_RES_NEED_GET_DATA:
			if err := r.handleNeedGetData(tag); err != nil {
				if r.logger != nil {
					r.logger.Printf("Queue %d: NEED_GET_DATA failed for tag %d: %v", r.queueID, tag, err)
				}
			}
			continue
		case 0:
			// fall through to descriptor processing below
		default:
			if r.logger != nil {
				r.logger.Printf("Queue %d: Completion for tag %d: result=%d", r.queueID, tag, result)
			}
			continue
		}

		// Get descriptor for this tag
		if tag >= uint16(r.depth) {
			if r.logger != nil {
				r.logger.Printf("Queue %d: Invalid tag %d (depth=%d)", r.queueID, tag, r.depth)
			}
			continue
		}

		descSize := int(unsafe.Sizeof(uapi.UblksrvIODesc{}))
		descPtr := unsafe.Pointer(r.descPtr + uintptr(int(tag)*descSize))
		desc := *(*uapi.UblksrvIODesc)(descPtr)

		// Check if descriptor is valid
		// Note: When we get a completion with result=0, it might be:
		// 1. Initial FETCH_REQ ACK with empty descriptor (all zeros)
		// 2. Real I/O with populated descriptor
		// 3. Corrupted/uninitialized memory (partial data)

		if desc.OpFlags == 0 && desc.NrSectors == 0 && desc.StartSector == 0 && desc.Addr == 0 {
			// All zeros - this is an initial FETCH_REQ ACK with no I/O yet
			if r.logger != nil {
				r.logger.Debugf("Queue %d: Tag %d initial ACK (no I/O yet)", r.queueID, tag)
			}
			// The FETCH_REQ is still active, kernel will send another completion when I/O arrives
			continue
		}

		// Debug output for non-zero descriptors
		if desc.OpFlags != 0 || desc.NrSectors != 0 {
			rawBytes := (*[32]byte)(descPtr)
			fmt.Printf("  RAW DESC[%d]: %x\n", tag, rawBytes)
			fmt.Printf("    Interpreted: OpFlags=0x%08x NrSectors=%d (0x%08x) StartSector=%d Addr=0x%x\n",
				desc.OpFlags, desc.NrSectors, desc.NrSectors, desc.StartSector, desc.Addr)

			// Sanity check - if NrSectors is way too large, it might be corrupted
			if desc.NrSectors > 256 { // More than 128KB (256 * 512)
				fmt.Printf("    WARNING: Suspiciously large request (%d sectors = %d bytes)\n",
					desc.NrSectors, desc.NrSectors*512)
				// Check if it might be uninitialized memory or corruption
				if desc.NrSectors > 1000000 {
					fmt.Printf("    CRITICAL: Likely corrupted descriptor, skipping\n")
					// Re-submit FETCH_REQ for this tag
					if err := r.submitFetchReq(tag); err != nil {
						if r.logger != nil {
							r.logger.Printf("Queue %d: Failed to re-submit FETCH_REQ for corrupted tag %d: %v",
								r.queueID, tag, err)
						}
					}
					continue
				}
			}
		}

		if err := r.handleIORequest(tag, desc); err != nil {
			if r.logger != nil {
				r.logger.Printf("Queue %d: Failed to handle I/O for tag %d: %v", r.queueID, tag, err)
			}
		}
	}

	// If no completions, yield briefly to avoid busy looping
	if len(completions) == 0 {
		syscall.Syscall(syscall.SYS_SCHED_YIELD, 0, 0, 0)
	}

	return nil
}

// handleNeedGetData requests the kernel to copy write data into our buffer
func (r *Runner) handleNeedGetData(tag uint16) error {
	if r.logger != nil {
		r.logger.Debugf("Queue %d: NEED_GET_DATA for tag %d", r.queueID, tag)
	}

	bufAddr := r.bufPtr + uintptr(int(tag)*64*1024)
	ioCmd := &uapi.UblksrvIOCmd{
		QID:  r.queueID,
		Tag:  tag,
		Addr: uint64(bufAddr),
	}

	userData := uint64(r.queueID)<<16 | uint64(tag)
	cmd := uapi.UblkIOCmd(uapi.UBLK_IO_NEED_GET_DATA)
	if _, err := r.ring.SubmitIOCmd(cmd, ioCmd, userData); err != nil {
		return fmt.Errorf("submit NEED_GET_DATA: %w", err)
	}

	return nil
}

// handleIORequest processes a single I/O request
func (r *Runner) handleIORequest(tag uint16, desc uapi.UblksrvIODesc) error {
	// Some completions are just keep-alive acknowledgements with an empty descriptor.
	if desc.OpFlags == 0 && desc.NrSectors == 0 {
		if r.logger != nil {
			r.logger.Debugf("Queue %d: Tag %d noop completion (descriptor empty)", r.queueID, tag)
		}
		return r.commitAndFetch(tag, nil)
	}

	// Extract I/O parameters from descriptor
	op := desc.GetOp()               // Use the provided method to get operation
	offset := desc.StartSector * 512 // Convert sectors to bytes (assuming 512-byte sectors)
	length := desc.NrSectors * 512   // Convert sectors to bytes

	// Progress reporting - show I/O operations as they happen
	opName := ""
	switch op {
	case uapi.UBLK_IO_OP_READ:
		opName = "READ"
	case uapi.UBLK_IO_OP_WRITE:
		opName = "WRITE"
	case uapi.UBLK_IO_OP_FLUSH:
		opName = "FLUSH"
	case uapi.UBLK_IO_OP_DISCARD:
		opName = "DISCARD"
	default:
		opName = fmt.Sprintf("OP_%d", op)
	}

	// Show bytes for small ops, KB for larger ones
	if length < 1024 {
		fmt.Printf("[Q%d:T%02d] %s %dB @ sector %d (offset %dB)\n", r.queueID, tag, opName, length, desc.StartSector, offset)
	} else {
		fmt.Printf("[Q%d:T%02d] %s %dKB @ sector %d (offset %dKB)\n", r.queueID, tag, opName, length/1024, desc.StartSector, offset/1024)
	}

	if r.logger != nil {
		r.logger.Debugf("Queue %d: Handling I/O op=%d offset=%d len=%d tag=%d",
			r.queueID, op, offset, length, tag)
	}

	// Calculate buffer pointer for this tag
	bufOffset := int(tag) * 64 * 1024 // 64KB per buffer
	bufPtr := unsafe.Pointer(r.bufPtr + uintptr(bufOffset))

	// CRITICAL: Check if length exceeds buffer size (64KB)
	const maxBufferSize = 64 * 1024

	var buffer []byte
	var dynamicBuffer []byte

	if length > maxBufferSize {
		// TEMPORARY: Dynamic allocation for large I/O to test if rest works
		fmt.Printf("    DYNAMIC ALLOC: Requested length %d exceeds buffer size %d\n", length, maxBufferSize)
		dynamicBuffer = make([]byte, length)
		buffer = dynamicBuffer
	} else {
		buffer = (*[64 * 1024]byte)(bufPtr)[:length:length]
	}

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
		// Provide buffer for next request
		Addr: uint64(r.bufPtr + uintptr(int(tag)*64*1024)),
	}

	userData := uint64(r.queueID)<<16 | uint64(tag)
	// Use IOCTL-encoded command to avoid -EOPNOTSUPP
	cmd := uapi.UblkIOCmd(uapi.UBLK_IO_COMMIT_AND_FETCH_REQ)
	_, err := r.ring.SubmitIOCmd(cmd, ioCmd, userData)
	return err
}

// mmapQueues maps the descriptor array and allocates I/O buffers
func mmapQueues(fd int, queueID uint16, depth int) (uintptr, uintptr, error) {
	// Calculate sizes
	descSize := depth * int(unsafe.Sizeof(uapi.UblksrvIODesc{}))
	bufSize := depth * 64 * 1024 // 64KB per request buffer

	// Map descriptor array. Kernel exposes descriptors read-only; matching
	// ublksrv, request PROT_READ to avoid EPERM when the VM hardens writes.
	descPtr, _, errno := syscall.Syscall6(
		syscall.SYS_MMAP,
		0,                  // addr
		uintptr(descSize),  // length
		syscall.PROT_READ,  // prot
		syscall.MAP_SHARED, // flags
		uintptr(fd),        // fd
		0,                  // offset
	)
	if errno != 0 {
		return 0, 0, fmt.Errorf("failed to mmap descriptor array: %v", errno)
	}

	// Allocate I/O buffers in userspace memory (NOT mapped from device)
	// The kernel doesn't expose I/O buffers via mmap; we manage them ourselves
	bufPtr, _, errno := syscall.Syscall6(
		syscall.SYS_MMAP,
		0,                                    // addr
		uintptr(bufSize),                     // length
		syscall.PROT_READ|syscall.PROT_WRITE, // prot
		syscall.MAP_PRIVATE|syscall.MAP_ANONYMOUS, // flags - anonymous memory
		^uintptr(0), // fd = -1 for anonymous
		0,           // offset
	)
	if errno != 0 {
		syscall.Syscall(syscall.SYS_MUNMAP, descPtr, uintptr(descSize), 0)
		return 0, 0, fmt.Errorf("failed to allocate I/O buffers: %v", errno)
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
		charFd:  -1,  // No device yet
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
		charFd:  -1,  // No real device
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
