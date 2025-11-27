package queue

import (
	"context"
	"fmt"
	"os"
	"runtime"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
	"unsafe"

	"golang.org/x/sys/unix"

	"github.com/ehrlich-b/go-ublk/internal/constants"
	"github.com/ehrlich-b/go-ublk/internal/interfaces"
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

// pointerFromMmap converts a uintptr from mmap syscall to unsafe.Pointer.
// Uses pointer indirection to satisfy go vet's unsafeptr checker.
// This is safe for mmap'd memory which has a fixed address.
//
//go:noinline
func pointerFromMmap(addr uintptr) unsafe.Pointer {
	return *(*unsafe.Pointer)(unsafe.Pointer(&addr))
}

// Runner handles I/O for a single ublk queue
type Runner struct {
	deviceID     uint32
	queueID      uint16
	depth        int
	blockSize    int // Logical block size in bytes
	backend      interfaces.Backend
	charDeviceFd int
	ring         uring.Ring
	descPtr      unsafe.Pointer // mmap'd descriptor array
	bufPtr       unsafe.Pointer // I/O buffer base
	ctx          context.Context
	cancel       context.CancelFunc
	logger       interfaces.Logger
	observer     interfaces.Observer // Metrics observer (may be nil)
	cpuAffinity  []int               // CPU affinity mask (nil = no affinity)
	// Per-tag state tracking for proper serialization
	tagStates  []TagState
	tagMutexes []sync.Mutex // Per-tag mutexes to prevent double submission
	// Pre-allocated per-tag command structs to avoid hot path allocations
	ioCmds []uapi.UblksrvIOCmd
}

const (
	descOpFlagsOffset     = uintptr(0)
	descNrSectorsOffset   = uintptr(4)
	descStartSectorOffset = uintptr(8)
	descAddrOffset        = uintptr(16)
)

type Config struct {
	DevID       uint32
	QueueID     uint16
	Depth       int
	BlockSize   int // Logical block size in bytes (default: 512)
	Backend     interfaces.Backend
	Logger      interfaces.Logger
	Observer    interfaces.Observer // Metrics observer (may be nil)
	CPUAffinity []int               // Optional CPU affinity (nil = no affinity)
	CharFd      int                 // Character device fd (if 0, will open device)
}

// NewRunner creates a new queue runner
func NewRunner(ctx context.Context, config Config) (*Runner, error) {
	if config.Logger != nil {
		config.Logger.Debugf("creating queue runner for device %d queue %d", config.DevID, config.QueueID)
	}

	var fd int
	var err error

	// Use provided fd or open the character device
	if config.CharFd > 0 {
		// Use the provided fd (duplicate it so each queue has its own)
		fd, err = syscall.Dup(config.CharFd)
		if err != nil {
			return nil, fmt.Errorf("failed to dup char fd: %v", err)
		}
	} else {
		// The character device (/dev/ublkcN) should exist after ADD_DEV.
		// We may need to retry briefly until udev creates the node.
		charPath := uapi.UblkDevicePath(config.DevID)
		if config.Logger != nil {
			config.Logger.Debugf("opening character device %s", charPath)
		}

		// Wait up to ~5s for udev to create the character device after ADD_DEV.
		// udev typically creates the node in <100ms, but slow systems or high
		// udev queue depth can cause delays. 50 * 100ms = 5s is generous.
		const maxRetries = 50
		const retryDelayNs = 100 * 1_000_000 // 100ms in nanoseconds
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
			ts := syscall.Timespec{Sec: 0, Nsec: retryDelayNs}
			_ = syscall.Nanosleep(&ts, nil) // Best effort sleep
		}
		if err != nil {
			return nil, fmt.Errorf("character device did not appear: %s", charPath)
		}
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

	// Default block size to 512 if not specified
	blockSize := config.BlockSize
	if blockSize <= 0 {
		blockSize = 512
	}

	runner := &Runner{
		deviceID:     config.DevID,
		queueID:      config.QueueID,
		depth:        config.Depth,
		blockSize:    blockSize,
		backend:      config.Backend,
		charDeviceFd: fd,
		ring:         ring,
		descPtr:      descPtr,
		bufPtr:       bufPtr,
		ctx:          ctx,
		cancel:       cancel,
		logger:       config.Logger,
		observer:     config.Observer,
		cpuAffinity:  config.CPUAffinity,
		tagStates:    make([]TagState, config.Depth),
		tagMutexes:   make([]sync.Mutex, config.Depth),
		ioCmds:       make([]uapi.UblksrvIOCmd, config.Depth),
	}

	return runner, nil
}

// Start begins processing I/O requests
func (r *Runner) Start() error {
	if r.logger != nil {
		r.logger.Printf("Starting queue %d for device %d", r.queueID, r.deviceID)
	}

	startErr := make(chan error, 1)
	go r.ioLoop(startErr)

	err := <-startErr
	if err != nil {
		return fmt.Errorf("failed to prime queue %d: %w", r.queueID, err)
	}
	return nil
}

// Prime submits initial FETCH_REQ commands to fill the queue.
// Can now handle START_DEV in progress by checking for EOPNOTSUPP.
func (r *Runner) Prime() error {
	if r.charDeviceFd < 0 || r.ring == nil {
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
		// Set initial state: FETCH_REQ is now in flight (moved to submitInitialFetchReq)
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
	_ = r.Stop() // Cleanup, ignore error

	if r.ring != nil {
		r.ring.Close()
	}

	// Unmap memory-mapped regions
	if r.descPtr != nil {
		descSize := r.depth * int(unsafe.Sizeof(uapi.UblksrvIODesc{}))
		_, _, _ = syscall.Syscall(syscall.SYS_MUNMAP, uintptr(r.descPtr), uintptr(descSize), 0)
		r.descPtr = nil
	}

	if r.bufPtr != nil {
		bufSize := r.depth * constants.IOBufferSizePerTag // 64KB per request buffer
		_, _, _ = syscall.Syscall(syscall.SYS_MUNMAP, uintptr(r.bufPtr), uintptr(bufSize), 0)
		r.bufPtr = nil
	}

	if r.charDeviceFd >= 0 {
		syscall.Close(r.charDeviceFd)
		r.charDeviceFd = -1
	}

	return nil
}

// ioLoop is the main I/O processing loop
func (r *Runner) ioLoop(started chan<- error) {
	// Pin to OS thread for ublk thread affinity requirement
	// ublk_drv records one thread per queue and rejects commands from different threads
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	// Set CPU affinity if configured
	// Uses round-robin assignment: queue N -> CPU (CPUAffinity[N % len(CPUAffinity)])
	if len(r.cpuAffinity) > 0 {
		cpuIdx := r.cpuAffinity[int(r.queueID)%len(r.cpuAffinity)]
		var mask unix.CPUSet
		mask.Set(cpuIdx)
		if err := unix.SchedSetaffinity(0, &mask); err != nil {
			if r.logger != nil {
				r.logger.Printf("Queue %d: Failed to set CPU affinity to CPU %d: %v", r.queueID, cpuIdx, err)
			}
			// Continue without affinity - not fatal
		} else if r.logger != nil {
			r.logger.Debugf("Queue %d: Set CPU affinity to CPU %d", r.queueID, cpuIdx)
		}
	}

	if r.logger != nil {
		r.logger.Debugf("Queue %d: Starting I/O loop (pinned to OS thread)", r.queueID)
	}

	// Check if we're in stub mode
	if r.charDeviceFd == -1 || r.ring == nil {
		if started != nil {
			started <- nil
		}
		r.stubLoop()
		return
	}

	// Submit initial FETCH_REQs from the pinned thread to honor kernel expectations
	primeErr := r.Prime()
	if started != nil {
		started <- primeErr
	}
	if primeErr != nil {
		if r.logger != nil {
			r.logger.Printf("Queue %d: Failed to prime queue: %v", r.queueID, primeErr)
		}
		return
	}

	// Queue is ready - the io_uring exists and is associated with the char device
	if r.logger != nil {
		r.logger.Printf("Queue %d: Queue io_uring ready", r.queueID)
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
	bufferAddr := uintptr(r.bufPtr) + uintptr(int(tag)*constants.IOBufferSizePerTag)

	// Use pre-allocated ioCmd to avoid heap allocation
	ioCmd := &r.ioCmds[tag]
	ioCmd.QID = r.queueID
	ioCmd.Tag = tag
	ioCmd.Result = 0
	ioCmd.Addr = uint64(bufferAddr)

	// Encode FETCH operation in userData
	userData := udOpFetch | (uint64(r.queueID) << 16) | uint64(tag)
	// Use the IOCTL-encoded command
	cmd := uapi.UblkIOCmd(uapi.UBLK_IO_FETCH_REQ) // This creates UBLK_U_IO_FETCH_REQ
	_, err := r.ring.SubmitIOCmd(cmd, ioCmd, userData)
	if err != nil {
		// Don't update state on submission failure
		return err
	}

	// ONLY set state to InFlightFetch after successful submission
	r.tagStates[tag] = TagStateInFlightFetch

	// Log initial FETCH_REQ submission
	if r.logger != nil {
		r.logger.Debugf("Queue %d: Initial FETCH_REQ submitted for tag %d", r.queueID, tag)
	}
	return nil
}

// processRequests processes completed I/O requests using proper per-tag state machine.
// Uses batched io_uring submissions: all completion handlers prepare SQEs, then
// one FlushSubmissions() call submits them all with a single syscall.
func (r *Runner) processRequests() error {
	// Wait for completion events from io_uring - this blocks until events arrive
	completions, err := r.ring.WaitForCompletion(0) // 0 = block until at least 1 completion
	if err != nil {
		return fmt.Errorf("failed to wait for completions: %w", err)
	}

	// Handle empty completions as no-work, not an error
	if len(completions) == 0 {
		return nil // No work to do - continue loop
	}

	// Process each completion event using per-tag state machine.
	// Each handler prepares an SQE but doesn't submit - we batch them.
	for _, completion := range completions {
		// Guard against nil completions (should never happen)
		if completion == nil {
			continue
		}

		userData := completion.UserData()
		tag := uint16(userData & 0xFFFF)
		isCommit := (userData & udOpCommit) != 0
		result := completion.Value()

		// Validate tag range (should never fail)
		if tag >= uint16(r.depth) {
			continue
		}

		// Process completion based on per-tag state machine
		if err := r.handleCompletion(tag, isCommit, result); err != nil {
			return err
		}
	}

	// Submit all prepared SQEs with ONE syscall.
	// Before: N completions → N syscalls (50%+ CPU in syscall overhead)
	// After:  N completions → 1 syscall
	if _, err := r.ring.FlushSubmissions(); err != nil {
		return fmt.Errorf("failed to flush submissions: %w", err)
	}

	return nil
}

// handleCompletion processes a single CQE using the per-tag state machine
func (r *Runner) handleCompletion(tag uint16, isCommit bool, result int32) error {
	// Guard this tag to prevent concurrent state changes
	r.tagMutexes[tag].Lock()
	defer r.tagMutexes[tag].Unlock()

	currentState := r.tagStates[tag]

	// State machine transitions
	switch currentState {
	case TagStateInFlightFetch:
		// CQE from FETCH_REQ - this means I/O is ready
		if result == 0 {
			// UBLK_IO_RES_OK: I/O request available - transition to Owned and process
			r.tagStates[tag] = TagStateOwned
			return r.processIOAndCommit(tag)
		} else if result == 1 {
			// UBLK_IO_RES_NEED_GET_DATA: Two-step write path (not implemented yet)
			r.tagStates[tag] = TagStateOwned
			return fmt.Errorf("NEED_GET_DATA not implemented")
		} else {
			// Unexpected result code
			return fmt.Errorf("unexpected FETCH result: %d", result)
		}

	case TagStateInFlightCommit:
		// CQE from COMMIT_AND_FETCH_REQ - ALWAYS means next I/O is ready
		// There is NO "commit done but no next I/O" state - the CQE only arrives
		// when the next request is ready (or on abort/error)
		if result == 0 {
			// UBLK_IO_RES_OK: Next I/O request available - transition to Owned and process immediately
			r.tagStates[tag] = TagStateOwned
			return r.processIOAndCommit(tag)
		} else if result == 1 {
			// UBLK_IO_RES_NEED_GET_DATA: Two-step write path
			r.tagStates[tag] = TagStateOwned
			return fmt.Errorf("NEED_GET_DATA not implemented")
		} else if result < 0 {
			// Error/abort path
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

// loadDescriptor reads a descriptor with acquire semantics to avoid stale data.
func (r *Runner) loadDescriptor(tag uint16) uapi.UblksrvIODesc {
	descSize := unsafe.Sizeof(uapi.UblksrvIODesc{})
	base := unsafe.Add(r.descPtr, uintptr(tag)*descSize)

	return uapi.UblksrvIODesc{
		OpFlags:     atomic.LoadUint32((*uint32)(base)),
		NrSectors:   atomic.LoadUint32((*uint32)(unsafe.Add(base, descNrSectorsOffset))),
		StartSector: atomic.LoadUint64((*uint64)(unsafe.Add(base, descStartSectorOffset))),
		Addr:        atomic.LoadUint64((*uint64)(unsafe.Add(base, descAddrOffset))),
	}
}

// processIOAndCommit reads descriptor, processes I/O, and submits COMMIT_AND_FETCH_REQ
func (r *Runner) processIOAndCommit(tag uint16) error {
	// Read descriptor for this tag using atomic loads to avoid stale cache lines
	desc := r.loadDescriptor(tag)

	// Handle empty descriptor - submit COMMIT_AND_FETCH with result=0
	// to acknowledge and wait for next I/O
	if desc.OpFlags == 0 && desc.NrSectors == 0 {
		return r.submitCommitAndFetch(tag, nil, desc)
	}

	// Process real I/O request
	if err := r.handleIORequest(tag, desc); err != nil {
		return err
	}

	return nil
}

// handleIORequest processes a single I/O request
func (r *Runner) handleIORequest(tag uint16, desc uapi.UblksrvIODesc) error {
	// Some completions are just keep-alive acknowledgements with an empty descriptor.
	if desc.OpFlags == 0 && desc.NrSectors == 0 {
		return r.submitCommitAndFetch(tag, nil, desc)
	}

	// Extract I/O parameters from descriptor
	op := desc.GetOp()                                     // Use the provided method to get operation
	offset := desc.StartSector * uint64(r.blockSize)       // Convert sectors to bytes
	length := uint32(desc.NrSectors) * uint32(r.blockSize) // Convert sectors to bytes

	// Calculate buffer pointer for this tag
	bufOffset := int(tag) * constants.IOBufferSizePerTag // 64KB per buffer
	bufPtr := unsafe.Add(r.bufPtr, bufOffset)

	// Check if length exceeds buffer size (64KB)
	const maxBufferSize = constants.IOBufferSizePerTag

	var buffer []byte

	if length > maxBufferSize {
		// Use buffer pool for large I/Os to avoid hot-path allocations
		buffer = GetBuffer(length)
		defer PutBuffer(buffer)
	} else {
		buffer = (*[constants.IOBufferSizePerTag]byte)(bufPtr)[:length:length]
	}

	var err error

	// Only measure time if observer is set (avoid syscall overhead)
	var startTime time.Time
	if r.observer != nil {
		startTime = time.Now()
	}

	switch op {
	case uapi.UBLK_IO_OP_READ:
		_, err = r.backend.ReadAt(buffer, int64(offset))
		if r.observer != nil {
			r.observer.ObserveRead(uint64(length), uint64(time.Since(startTime).Nanoseconds()), err == nil)
		}
	case uapi.UBLK_IO_OP_WRITE:
		_, err = r.backend.WriteAt(buffer, int64(offset))
		if r.observer != nil {
			r.observer.ObserveWrite(uint64(length), uint64(time.Since(startTime).Nanoseconds()), err == nil)
		}
	case uapi.UBLK_IO_OP_FLUSH:
		err = r.backend.Flush()
		if r.observer != nil {
			r.observer.ObserveFlush(uint64(time.Since(startTime).Nanoseconds()), err == nil)
		}
	case uapi.UBLK_IO_OP_DISCARD:
		// Handle discard if backend supports it
		if discardBackend, ok := r.backend.(interfaces.DiscardBackend); ok {
			err = discardBackend.Discard(int64(offset), int64(length))
		}
		if r.observer != nil {
			r.observer.ObserveDiscard(uint64(length), uint64(time.Since(startTime).Nanoseconds()), err == nil)
		}
	default:
		err = fmt.Errorf("unsupported operation: %d", op)
	}

	// Submit COMMIT_AND_FETCH_REQ with result
	return r.submitCommitAndFetch(tag, err, desc)
}

// submitCommitAndFetch prepares COMMIT_AND_FETCH_REQ with proper state tracking.
// Note: This only prepares the SQE - caller must call FlushSubmissions() to submit.
func (r *Runner) submitCommitAndFetch(tag uint16, ioErr error, desc uapi.UblksrvIODesc) error {
	// Calculate result: bytes processed for success, negative errno for error
	// Always set result = nr_sectors << 9 (nr_sectors * 512) as per expert guidance
	result := int32(desc.NrSectors) << 9 // Success: return bytes processed
	if ioErr != nil {
		result = -5 // -EIO
	}

	// Only submit if we're in Owned state
	if r.tagStates[tag] != TagStateOwned {
		return fmt.Errorf("cannot submit COMMIT for tag %d in state %d (not Owned)", tag, r.tagStates[tag])
	}

	// Addr must point to the data buffer for next I/O
	bufferAddr := uintptr(r.bufPtr) + uintptr(int(tag)*constants.IOBufferSizePerTag)

	// Use pre-allocated ioCmd to avoid heap allocation
	ioCmd := &r.ioCmds[tag]
	ioCmd.QID = r.queueID
	ioCmd.Tag = tag
	ioCmd.Result = result
	ioCmd.Addr = uint64(bufferAddr)

	// Encode COMMIT operation in userData
	userData := udOpCommit | (uint64(r.queueID) << 16) | uint64(tag)
	// Use the IOCTL-encoded command
	cmd := uapi.UblkIOCmd(uapi.UBLK_IO_COMMIT_AND_FETCH_REQ) // This creates UBLK_U_IO_COMMIT_AND_FETCH_REQ

	// Prepare SQE without submitting - enables batching multiple completions
	// into a single io_uring_enter syscall
	err := r.ring.PrepareIOCmd(cmd, ioCmd, userData)
	if err != nil {
		return fmt.Errorf("COMMIT_AND_FETCH_REQ prepare failed: %w", err)
	}

	// Update state: COMMIT_AND_FETCH_REQ is now prepared (will be in flight after flush)
	r.tagStates[tag] = TagStateInFlightCommit
	return nil
}

// mmapQueues maps the descriptor array and allocates I/O buffers
func mmapQueues(fd int, queueID uint16, depth int) (unsafe.Pointer, unsafe.Pointer, error) {
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
		return nil, nil, fmt.Errorf("failed to mmap descriptor array: %v", errno)
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
		_, _, _ = syscall.Syscall(syscall.SYS_MUNMAP, descPtr, uintptr(descSize), 0)
		return nil, nil, fmt.Errorf("failed to allocate I/O buffers: %v", errno)
	}

	// Convert uintptr to unsafe.Pointer using helper to avoid go vet false positive
	return pointerFromMmap(descPtr), pointerFromMmap(bufPtr), nil
}

// NewStubRunner creates a stub runner for simulation/testing
func NewStubRunner(ctx context.Context, config Config) *Runner {
	ctx, cancel := context.WithCancel(ctx)

	// Default block size to 512 if not specified
	blockSize := config.BlockSize
	if blockSize <= 0 {
		blockSize = 512
	}

	return &Runner{
		deviceID:     config.DevID,
		queueID:      config.QueueID,
		depth:        config.Depth,
		blockSize:    blockSize,
		backend:      config.Backend,
		charDeviceFd: -1,  // No real device
		ring:         nil, // No real ring
		descPtr:      nil,
		bufPtr:       nil,
		ctx:          ctx,
		cancel:       cancel,
		logger:       config.Logger,
		tagStates:    make([]TagState, config.Depth),
		tagMutexes:   make([]sync.Mutex, config.Depth),
		ioCmds:       make([]uapi.UblksrvIOCmd, config.Depth),
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
