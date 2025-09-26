package queue

import (
	"context"
	"fmt"
	"os"
	"runtime"
	"sync"
	"syscall"
	"unsafe"

	"github.com/ehrlich-b/go-ublk/internal/constants"
	"github.com/ehrlich-b/go-ublk/internal/interfaces"
	"github.com/ehrlich-b/go-ublk/internal/logging"
	"github.com/ehrlich-b/go-ublk/internal/uapi"
	"github.com/ehrlich-b/go-ublk/internal/uring"
)

// TagState represents the state of a tag in the ublk state machine
type TagState int

const (
	TagStateInFlightFetch  TagState = iota // Kernel owns; FETCH_REQ in flight
	TagStateOwned                          // User owns; descriptor is readable
	TagStateInFlightCommit                 // Kernel owns; COMMIT_AND_FETCH_REQ in flight
)

// User data encoding: high bit indicates operation type
const (
	udOpFetch  uint64 = 0 << 63 // FETCH_REQ completion
	udOpCommit uint64 = 1 << 63 // COMMIT_AND_FETCH_REQ completion
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
	// Per-tag state tracking for proper serialization
	tagStates  []TagState
	tagMutexes []sync.Mutex // Per-tag mutexes to prevent double submission
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
	if config.Logger != nil {
		config.Logger.Debugf("creating queue runner for device %d queue %d", config.DevID, config.QueueID)
	}

	// The character device (/dev/ublkcN) should exist after ADD_DEV.
	// We may need to retry briefly until udev creates the node.
	charPath := uapi.UblkDevicePath(config.DevID)
	if config.Logger != nil {
		config.Logger.Debugf("opening character device %s", charPath)
	}

	var fd int
	var err error
	const maxRetries = 50 // up to ~5s with 100ms sleep
	for i := 0; i < maxRetries; i++ {
		fd, err = syscall.Open(charPath, syscall.O_RDWR, 0)
		if err == nil {
			if config.Logger != nil {
				config.Logger.Debugf("opened %s successfully, fd=%d", charPath, fd)
			}
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

	if config.Logger != nil {
		config.Logger.Debugf("creating io_uring for queue with fd=%d", fd)
	}
	ring, err := uring.NewRing(ringConfig)
	if err != nil {
		syscall.Close(fd)
		return nil, fmt.Errorf("failed to create io_uring: %v", err)
	}
	if config.Logger != nil {
		config.Logger.Debugf("io_uring created successfully for queue")
	}

	// Memory map the descriptor array and I/O buffers
	if config.Logger != nil {
		config.Logger.Debugf("mmapping queues for fd=%d", fd)
	}
	descPtr, bufPtr, err := mmapQueues(fd, config.QueueID, config.Depth)
	if err != nil {
		if config.Logger != nil {
			config.Logger.Debugf("mmapQueues failed: %v", err)
		}
		ring.Close()
		syscall.Close(fd)
		return nil, fmt.Errorf("failed to mmap queues: %v", err)
	}
	if config.Logger != nil {
		config.Logger.Debugf("mmapQueues succeeded")
	}

	ctx, cancel := context.WithCancel(ctx)

	runner := &Runner{
		devID:      config.DevID,
		queueID:    config.QueueID,
		depth:      config.Depth,
		backend:    config.Backend,
		charFd:     fd,
		ring:       ring,
		descPtr:    descPtr,
		bufPtr:     bufPtr,
		ctx:        ctx,
		cancel:     cancel,
		logger:     config.Logger,
		tagStates:  make([]TagState, config.Depth),
		tagMutexes: make([]sync.Mutex, config.Depth),
	}

	return runner, nil
}

// Start begins processing I/O requests
func (r *Runner) Start() error {
	if r.logger != nil {
		r.logger.Printf("Starting queue %d for device %d", r.queueID, r.devID)
	}

	// Submit initial FETCH_REQs before starting the I/O loop
	if err := r.Prime(); err != nil {
		return fmt.Errorf("failed to prime queue %d: %w", r.queueID, err)
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

	// Submit initial FETCH_REQ for each tag (ONLY ONCE at startup)
	for tag := 0; tag < r.depth; tag++ {
		if err := r.submitInitialFetchReq(uint16(tag)); err != nil {
			// If we get EOPNOTSUPP, START_DEV might not be ready yet
			if errno, ok := err.(syscall.Errno); ok && errno == syscall.EOPNOTSUPP {
				// This is expected if START_DEV hasn't been processed yet
				// The queue runner loop will retry
				return fmt.Errorf("device not ready (START_DEV pending): %w", err)
			}
			return fmt.Errorf("submit initial FETCH_REQ[%d]: %w", tag, err)
		}
		// Set initial state: FETCH_REQ is now in flight
		r.tagStates[tag] = TagStateInFlightFetch
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
		bufSize := r.depth * constants.IOBufferSizePerTag // 64KB per request buffer
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
	// Pin to OS thread for ublk thread affinity requirement
	// ublk_drv records one thread per queue and rejects commands from different threads
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	if r.logger != nil {
		r.logger.Debugf("Queue %d: Starting I/O loop (pinned to OS thread)", r.queueID)
	}

	// Check if we're in stub mode
	if r.charFd == -1 || r.ring == nil {
		r.stubLoop()
		return
	}

	// Queue is ready - the io_uring exists and is associated with the char device
	if r.logger != nil {
		r.logger.Printf("Queue %d: Queue io_uring ready", r.queueID)
	}

	// FETCH_REQs were already submitted in Start(), just log that we're ready
	if r.logger != nil {
		r.logger.Printf("Queue %d: I/O loop ready for processing", r.queueID)
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

// submitInitialFetchReq submits the initial FETCH_REQ command (ONLY at startup)
func (r *Runner) submitInitialFetchReq(tag uint16) error {
	// Guard against double submission
	r.tagMutexes[tag].Lock()
	defer r.tagMutexes[tag].Unlock()

	if r.tagStates[tag] != TagState(0) { // Should be uninitialized
		return fmt.Errorf("tag %d already initialized (state=%d)", tag, r.tagStates[tag])
	}

	// Addr must point to the data buffer for this tag
	bufferAddr := r.bufPtr + uintptr(int(tag)*64*1024) // 64KB per tag

	ioCmd := &uapi.UblksrvIOCmd{
		QID:    r.queueID,
		Tag:    tag,
		Result: 0,
		// Must point to the I/O data buffer
		Addr: uint64(bufferAddr),
	}

	// Encode FETCH operation in userData
	userData := udOpFetch | (uint64(r.queueID) << 16) | uint64(tag)
	// Use the IOCTL-encoded command
	cmd := uapi.UblkIOCmd(uapi.UBLK_IO_FETCH_REQ) // This creates UBLK_U_IO_FETCH_REQ
	_, err := r.ring.SubmitIOCmd(cmd, ioCmd, userData)
	if err != nil {
		return err
	}

	// Log initial FETCH_REQ submission
	if r.logger != nil {
		r.logger.Debugf("Queue %d: Initial FETCH_REQ submitted for tag %d", r.queueID, tag)
	}
	return nil
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
	// Initialize per-tag state tracking
	r.tagStates = make([]TagState, r.depth)
	r.tagMutexes = make([]sync.Mutex, r.depth)

	logger.Info("data plane initialized successfully")
	return nil
}

// processRequests processes completed I/O requests using proper per-tag state machine
func (r *Runner) processRequests() error {
	// Wait for completion events from io_uring - this blocks until events arrive
	completions, err := r.ring.WaitForCompletion(0) // 0 = block until at least 1 completion
	if err != nil {
		return fmt.Errorf("failed to wait for completions: %w", err)
	}

	// Process each completion event using per-tag state machine
	for _, completion := range completions {
		userData := completion.UserData()
		tag := uint16(userData & 0xFFFF)
		isCommit := (userData & udOpCommit) != 0
		result := completion.Value()

		// Validate tag range
		if tag >= uint16(r.depth) {
			if r.logger != nil {
				r.logger.Printf("Queue %d: Invalid tag %d (depth=%d)", r.queueID, tag, r.depth)
			}
			continue
		}

		// Process completion based on per-tag state machine
		if err := r.handleCompletion(tag, isCommit, result); err != nil {
			if r.logger != nil {
				r.logger.Printf("Queue %d: Failed to handle completion for tag %d: %v", r.queueID, tag, err)
			}
			return err
		}
	}

	return nil
}

// handleCompletion processes a single CQE using the per-tag state machine
func (r *Runner) handleCompletion(tag uint16, isCommit bool, result int32) error {
	// Guard this tag to prevent concurrent state changes
	r.tagMutexes[tag].Lock()
	defer r.tagMutexes[tag].Unlock()

	currentState := r.tagStates[tag]
	opType := "FETCH"
	if isCommit {
		opType = "COMMIT"
	}

	if r.logger != nil {
		r.logger.Debugf("queue %d tag %d %s completion, result=%d, state=%d", r.queueID, tag, opType, result, currentState)
	}

	// State machine transitions
	switch currentState {
	case TagStateInFlightFetch:
		// CQE from FETCH_REQ - this means I/O is ready
		if result == 0 {
			// UBLK_IO_RES_OK: I/O request available - transition to Owned and process
			r.tagStates[tag] = TagStateOwned
			if r.logger != nil {
				r.logger.Debugf("queue %d tag %d I/O arrived, processing", r.queueID, tag)
			}
			return r.processIOAndCommit(tag)
		} else if result == 1 {
			// UBLK_IO_RES_NEED_GET_DATA: Two-step write path (not implemented yet)
			r.tagStates[tag] = TagStateOwned
			if r.logger != nil {
				r.logger.Printf("queue %d tag %d NEED_GET_DATA not implemented", r.queueID, tag)
			}
			return fmt.Errorf("NEED_GET_DATA not implemented")
		} else {
			// Unexpected result code
			if r.logger != nil {
				r.logger.Printf("queue %d tag %d unexpected FETCH result=%d", r.queueID, tag, result)
			}
			return fmt.Errorf("unexpected FETCH result: %d", result)
		}

	case TagStateInFlightCommit:
		// CQE from COMMIT_AND_FETCH_REQ - ALWAYS means next I/O is ready
		// There is NO "commit done but no next I/O" state - the CQE only arrives
		// when the next request is ready (or on abort/error)
		if result == 0 {
			// UBLK_IO_RES_OK: Next I/O request available - transition to Owned and process immediately
			r.tagStates[tag] = TagStateOwned
			if r.logger != nil {
				r.logger.Debugf("queue %d tag %d next I/O arrived, processing", r.queueID, tag)
			}
			return r.processIOAndCommit(tag)
		} else if result == 1 {
			// UBLK_IO_RES_NEED_GET_DATA: Two-step write path
			r.tagStates[tag] = TagStateOwned
			if r.logger != nil {
				r.logger.Printf("queue %d tag %d next NEED_GET_DATA not implemented", r.queueID, tag)
			}
			return fmt.Errorf("NEED_GET_DATA not implemented")
		} else if result < 0 {
			// Error/abort path
			if r.logger != nil {
				r.logger.Printf("queue %d tag %d COMMIT error/abort: result=%d", r.queueID, tag, result)
			}
			r.tagStates[tag] = TagStateOwned // Tag can be reused after error
			return fmt.Errorf("COMMIT_AND_FETCH error: %d", result)
		} else {
			// Should never happen
			return fmt.Errorf("unexpected COMMIT result: %d", result)
		}

	case TagStateOwned:
		// This shouldn't happen - we only submit when transitioning from Owned
		return fmt.Errorf("unexpected completion for tag %d in Owned state", tag)

	default:
		return fmt.Errorf("invalid state %d for tag %d", currentState, tag)
	}
}

// processIOAndCommit reads descriptor, processes I/O, and submits COMMIT_AND_FETCH_REQ
func (r *Runner) processIOAndCommit(tag uint16) error {
	// Read descriptor for this tag
	descSize := int(unsafe.Sizeof(uapi.UblksrvIODesc{}))
	descPtr := unsafe.Add(unsafe.Pointer(r.descPtr), uintptr(tag)*uintptr(descSize))
	desc := *(*uapi.UblksrvIODesc)(descPtr)

	if r.logger != nil {
		r.logger.Debugf("processIOAndCommit: tag=%d, OpFlags=0x%x, NrSectors=%d, StartSector=%d, Addr=0x%x",
			tag, desc.OpFlags, desc.NrSectors, desc.StartSector, desc.Addr)
	}

	// Handle empty descriptor - submit COMMIT_AND_FETCH with result=0
	// to acknowledge and wait for next I/O
	if desc.OpFlags == 0 && desc.NrSectors == 0 {
		if r.logger != nil {
			r.logger.Debugf("Queue %d: Tag %d empty descriptor, submitting noop COMMIT_AND_FETCH", r.queueID, tag)
		}
		// Submit COMMIT_AND_FETCH with result=0 (no-op)
		return r.submitCommitAndFetch(tag, nil, desc)
	}

	// Process real I/O request
	if err := r.handleIORequest(tag, desc); err != nil {
		return err
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
		return r.submitCommitAndFetch(tag, nil, desc)
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

	// Log I/O operations for progress visibility
	if r.logger != nil {
		if length < 1024 {
			r.logger.Printf("[Q%d:T%02d] %s %dB @ sector %d", r.queueID, tag, opName, length, desc.StartSector)
		} else {
			r.logger.Printf("[Q%d:T%02d] %s %dKB @ sector %d", r.queueID, tag, opName, length/1024, desc.StartSector)
		}
	}

	if r.logger != nil {
		r.logger.Debugf("Queue %d: Handling I/O op=%d offset=%d len=%d tag=%d",
			r.queueID, op, offset, length, tag)
	}

	// Calculate buffer pointer for this tag
	bufOffset := int(tag) * constants.IOBufferSizePerTag // 64KB per buffer
	bufPtr := unsafe.Pointer(r.bufPtr + uintptr(bufOffset))

	// Check if length exceeds buffer size (64KB)
	const maxBufferSize = constants.IOBufferSizePerTag

	var buffer []byte
	var dynamicBuffer []byte

	if length > maxBufferSize {
		if r.logger != nil {
			r.logger.Printf("dynamic allocation: length %d exceeds buffer size %d", length, maxBufferSize)
		}
		dynamicBuffer = make([]byte, length)
		buffer = dynamicBuffer
	} else {
		buffer = (*[constants.IOBufferSizePerTag]byte)(bufPtr)[:length:length]
	}

	var err error
	switch op {
	case uapi.UBLK_IO_OP_READ:
		if r.logger != nil {
			r.logger.Debugf("calling ReadAt: offset=%d, buflen=%d", offset, len(buffer))
		}
		_, err = r.backend.ReadAt(buffer, int64(offset))
		if r.logger != nil {
			r.logger.Debugf("ReadAt completed: err=%v", err)
		}
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
	if r.logger != nil {
		r.logger.Debugf("calling submitCommitAndFetch: tag=%d, err=%v", tag, err)
	}
	submitErr := r.submitCommitAndFetch(tag, err, desc)
	if r.logger != nil {
		r.logger.Debugf("submitCommitAndFetch completed: err=%v", submitErr)
	}
	return submitErr
}

// submitCommitAndFetch submits COMMIT_AND_FETCH_REQ with proper state tracking
func (r *Runner) submitCommitAndFetch(tag uint16, ioErr error, desc uapi.UblksrvIODesc) error {
	// Tag mutex is already held by caller to prevent deadlock
	if r.logger != nil {
		r.logger.Debugf("submitCommitAndFetch: starting for tag=%d", tag)
	}

	// Calculate result: bytes processed for success, negative errno for error
	// Always set result = nr_sectors << 9 (nr_sectors * 512) as per expert guidance
	result := int32(desc.NrSectors) << 9 // Success: return bytes processed
	if ioErr != nil {
		result = -5 // -EIO
		if r.logger != nil {
			r.logger.Printf("Queue %d: I/O error for tag %d: %v", r.queueID, tag, ioErr)
		}
	}
	if r.logger != nil {
		r.logger.Debugf("submitCommitAndFetch: calculated result=%d", result)
	}

	// Only submit if we're in Owned state
	if r.tagStates[tag] != TagStateOwned {
		return fmt.Errorf("cannot submit COMMIT for tag %d in state %d (not Owned)", tag, r.tagStates[tag])
	}

	// Addr must point to the data buffer for next I/O
	bufferAddr := r.bufPtr + uintptr(int(tag)*64*1024) // 64KB per tag

	ioCmd := &uapi.UblksrvIOCmd{
		QID:    r.queueID,
		Tag:    tag,
		Result: result,
		// Must point to the I/O data buffer for next operation
		Addr: uint64(bufferAddr),
	}

	// Encode COMMIT operation in userData
	userData := udOpCommit | (uint64(r.queueID) << 16) | uint64(tag)
	// Use the IOCTL-encoded command
	cmd := uapi.UblkIOCmd(uapi.UBLK_IO_COMMIT_AND_FETCH_REQ) // This creates UBLK_U_IO_COMMIT_AND_FETCH_REQ
	_, err := r.ring.SubmitIOCmd(cmd, ioCmd, userData)
	if err != nil {
		return fmt.Errorf("COMMIT_AND_FETCH_REQ failed: %w", err)
	}

	// Update state: COMMIT_AND_FETCH_REQ is now in flight
	r.tagStates[tag] = TagStateInFlightCommit

	if r.logger != nil {
		r.logger.Debugf("queue %d: COMMIT_AND_FETCH_REQ submitted for tag %d with result=%d bytes",
			r.queueID, tag, result)
	}

	return nil
}

// mmapQueues maps the descriptor array and allocates I/O buffers
func mmapQueues(fd int, queueID uint16, depth int) (uintptr, uintptr, error) {
	// Calculate sizes
	descSize := depth * int(unsafe.Sizeof(uapi.UblksrvIODesc{}))
	bufSize := depth * constants.IOBufferSizePerTag // 64KB per request buffer

	// Page-round the mmap size
	pageSize := os.Getpagesize()
	if rem := descSize % pageSize; rem != 0 {
		descSize += pageSize - rem
	}

	// Calculate per-queue offset for mmap
	// Formula: offset = queueID * round_up(queue_depth * sizeof(desc), PAGE_SIZE)
	mmapOffset := uintptr(queueID) * uintptr(descSize)

	// Map descriptor array as READ-ONLY from userspace perspective
	// The kernel writes to descriptors internally, userspace only reads
	descPtr, _, errno := syscall.Syscall6(
		syscall.SYS_MMAP,
		0,                 // addr
		uintptr(descSize), // length (page-rounded)
		syscall.PROT_READ, // prot - READ ONLY from userspace!
		syscall.MAP_SHARED|syscall.MAP_POPULATE, // flags - populate to avoid page faults
		uintptr(fd), // fd
		mmapOffset,  // per-queue offset
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
		devID:      config.DevID,
		queueID:    config.QueueID,
		depth:      config.Depth,
		backend:    config.Backend,
		charFd:     -1,  // No device yet
		ring:       nil, // No ring yet
		descPtr:    0,   // No mmap yet
		bufPtr:     0,   // No buffers yet
		ctx:        ctx,
		cancel:     cancel,
		logger:     config.Logger,
		tagStates:  make([]TagState, config.Depth),
		tagMutexes: make([]sync.Mutex, config.Depth),
	}

	// Start a goroutine that will initialize the real data plane once the device appears
	go runner.waitAndStartDataPlane()

	return runner
}

func NewStubRunner(ctx context.Context, config Config) *Runner {
	ctx, cancel := context.WithCancel(ctx)

	return &Runner{
		devID:      config.DevID,
		queueID:    config.QueueID,
		depth:      config.Depth,
		backend:    config.Backend,
		charFd:     -1,  // No real device
		ring:       nil, // No real ring
		descPtr:    0,
		bufPtr:     0,
		ctx:        ctx,
		cancel:     cancel,
		logger:     config.Logger,
		tagStates:  make([]TagState, config.Depth),
		tagMutexes: make([]sync.Mutex, config.Depth),
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
