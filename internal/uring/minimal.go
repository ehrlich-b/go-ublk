// Package uring provides minimal URING_CMD implementation for ublk control operations
package uring

import (
	"fmt"
	"runtime"
	"sync/atomic"
	"syscall"
	"time"
	"unsafe"

	"github.com/ehrlich-b/go-ublk/internal/logging"
	"github.com/ehrlich-b/go-ublk/internal/uapi"
	"golang.org/x/sys/unix"
)

// System call numbers for io_uring
const (
	__NR_io_uring_setup = 425
	__NR_io_uring_enter = 426
)

// Minimal io_uring structures for URING_CMD operations only
// Based on kernel include/uapi/linux/io_uring.h

const (
	IORING_SETUP_SQE128 = 1 << 10
	IORING_SETUP_CQE32  = 1 << 11

	// io_uring mmap offsets
	IORING_OFF_SQ_RING = 0
	IORING_OFF_CQ_RING = 0x8000000
	IORING_OFF_SQES    = 0x10000000
)

// SQE128 structure for URING_CMD
// With SQE128 enabled, the cmd area for URING_CMD is 80 bytes starting at byte 48
// The kernel UAPI says: "If IORING_SETUP_SQE128, this field is 80 bytes" for io_uring_sqe.cmd
// Layout:
//   - Bytes 0-47: Standard SQE fields
//   - Bytes 48-127: cmd area (80 bytes) for URING_CMD operations
type sqe128 struct {
	// 0..31: common header
	opcode      uint8   // 0
	flags       uint8   // 1
	ioprio      uint16  // 2
	fd          int32   // 4
	union0      [8]byte // 8  (overlays: off/addr2/cmd_op+__pad1)
	addr        uint64  // 16
	len         uint32  // 24
	opcodeFlags uint32  // 28 (aka rw_flags/uring_cmd_flags)

	// 32..47: rest of base SQE fields
	userData    uint64 // 32..39
	bufIndex    uint16 // 40..41
	personality uint16 // 42..43
	spliceFdIn  int32  // 44..47

	// 48..127: cmd area for URING_CMD (80 bytes with SQE128)
	// This is where ublksrv_io_cmd (16 bytes) goes for FETCH_REQ/COMMIT_AND_FETCH_REQ
	cmd [80]byte // 48..127
}

// setCmdOp sets the cmd_op field in the union AND ensures the adjacent pad is zero
func (sqe *sqe128) setCmdOp(cmdOp uint32) {
	// cmd_op is at bytes 8-11
	*(*uint32)(unsafe.Pointer(&sqe.union0[0])) = cmdOp
	// __pad1 is at bytes 12-15 and MUST be zero
	*(*uint32)(unsafe.Pointer(&sqe.union0[4])) = 0
}

// getCmdOp gets the cmd_op field from the union
func (sqe *sqe128) getCmdOp() uint32 {
	return *(*uint32)(unsafe.Pointer(&sqe.union0[0]))
}

// Minimal CQE (32-byte version)
type cqe32 struct {
	userData uint64
	res      int32
	flags    uint32
	bigCQE   [16]uint8 // Extra data for CQE32
}

// AsyncHandle represents a pending io_uring operation
type AsyncHandle struct {
	userData uint64
	ring     *minimalRing
}

// Wait polls for completion of async operation
func (h *AsyncHandle) Wait(timeout time.Duration) (Result, error) {
	logger := logging.Default()
	logger.Debug("waiting for completion", "userData", h.userData, "timeout", timeout)
	deadline := time.Now().Add(timeout)

	attempts := 0
	for time.Now().Before(deadline) {
		attempts++
		// Try to get completion without blocking
		result, err := h.ring.tryGetCompletion(h.userData)
		if err == nil {
			logger.Debug("found completion", "attempts", attempts, "result", result.Value())
			return result, nil
		}

		// Log every 100 attempts (1 second)
		if attempts%100 == 0 {
			logger.Debug("still waiting for completion", "attempts", attempts, "error", err.Error())
		}

		// Not ready yet, sleep briefly.
		// 10ms balances responsiveness with CPU overhead for async polling.
		time.Sleep(10 * time.Millisecond)
	}

	logger.Debug("timeout waiting for completion", "attempts", attempts)
	return nil, fmt.Errorf("timeout waiting for completion after %d attempts", attempts)
}

// Minimal ring structures
type io_uring_params struct {
	sqEntries    uint32
	cqEntries    uint32
	flags        uint32
	sqThreadCpu  uint32
	sqThreadIdle uint32
	features     uint32
	wqFd         uint32
	resv         [3]uint32
	sqOff        struct {
		head        uint32
		tail        uint32
		ringMask    uint32
		ringEntries uint32
		flags       uint32
		dropped     uint32
		array       uint32
		resv1       uint32
		userAddr    uint64
	}
	cqOff struct {
		head        uint32
		tail        uint32
		ringMask    uint32
		ringEntries uint32
		overflow    uint32
		cqes        uint32
		flags       uint32
		resv1       uint32
		userAddr    uint64
	}
}

// minimalRing implements just URING_CMD for ublk operations
type minimalRing struct {
	ringFd   int // io_uring file descriptor
	targetFd int // target fd (/dev/ublk-control for control ops, /dev/ublkcN for I/O ops)
	params   io_uring_params
	sqAddr   unsafe.Pointer // SQ ring mapping base
	cqAddr   unsafe.Pointer // CQ ring mapping base
	sqesAddr unsafe.Pointer // SQEs mapping base

	// Pre-allocated fields to avoid hot path allocations
	sqePool      sqe128          // Reusable SQE (submissions are sequential per ring)
	resultsPool  []Result        // Reusable results slice
	cqePoolSize  int             // Size of CQE result pool
	cqePool      []minimalResult // Pool of result structs to avoid allocation
	cqePoolIndex int             // Next available result in pool
}

// kernelUringCmdOpcode returns the runtime kernel's IORING_OP_URING_CMD
// value when built with cgo on Linux. On non-cgo builds, a reasonable
// fallback is used. See kernelopcode_linux.go and kernelopcode_stub.go.
// kernelUringCmdOpcode provided by platform-specific files

// NewMinimalRing creates a minimal io_uring for ublk control operations
func NewMinimalRing(entries uint32, ctrlFd int32) (Ring, error) {
	logger := logging.Default()
	logger.Debug("creating minimal io_uring", "entries", entries, "ctrl_fd", ctrlFd)

	// Verify SQE structure size is exactly 128 bytes
	sqeSize := unsafe.Sizeof(sqe128{})
	if sqeSize != 128 {
		return nil, fmt.Errorf("sqe128 size is %d bytes, expected 128", sqeSize)
	}
	logger.Debug("SQE128 size verified", "size", sqeSize)

	// Set up ring parameters with SQE128/CQE32 for URING_CMD
	// Note: Some kernels may require both flags for URING_CMD operations
	params := io_uring_params{
		sqEntries: entries,
		cqEntries: entries * 2, // Usually CQ is 2x SQ size
		flags:     IORING_SETUP_SQE128 | IORING_SETUP_CQE32,
	}

	logger.Debug("calling io_uring_setup", "flags", fmt.Sprintf("0x%x", params.flags))

	// Create io_uring
	ringFd, _, errno := syscall.Syscall(unix.SYS_IO_URING_SETUP,
		uintptr(entries),
		uintptr(unsafe.Pointer(&params)),
		0)
	if errno != 0 {
		logger.Error("io_uring_setup failed", "errno", errno)
		return nil, fmt.Errorf("io_uring_setup failed: %v", errno)
	}

	logger.Debug("io_uring_setup succeeded", "ring_fd", ringFd)

	// Verify the kernel accepted our flags
	if (params.flags & IORING_SETUP_SQE128) == 0 {
		logger.Error("kernel did not accept IORING_SETUP_SQE128 flag")
		syscall.Close(int(ringFd))
		return nil, fmt.Errorf("kernel rejected IORING_SETUP_SQE128 flag")
	}
	logger.Debug("Kernel accepted SQE128 flag", "params.flags", fmt.Sprintf("0x%x", params.flags))

	// Map SQ ring
	sqSize := params.sqOff.array + params.sqEntries*4
	sqAddr, err := unix.Mmap(int(ringFd), IORING_OFF_SQ_RING, int(sqSize), unix.PROT_READ|unix.PROT_WRITE, unix.MAP_SHARED)
	if err != nil {
		syscall.Close(int(ringFd))
		return nil, fmt.Errorf("failed to mmap SQ: %v", err)
	}
	// Map CQ ring
	cqSize := params.cqOff.cqes + params.cqEntries*uint32(unsafe.Sizeof(cqe32{}))
	cqAddr, err := unix.Mmap(int(ringFd), IORING_OFF_CQ_RING, int(cqSize), unix.PROT_READ|unix.PROT_WRITE, unix.MAP_SHARED)
	if err != nil {
		unix.Munmap(sqAddr)
		syscall.Close(int(ringFd))
		return nil, fmt.Errorf("failed to mmap CQ: %v", err)
	}
	// Map SQEs array
	sqesSize := int(params.sqEntries) * int(unsafe.Sizeof(sqe128{}))
	sqesAddr, err := unix.Mmap(int(ringFd), IORING_OFF_SQES, sqesSize, unix.PROT_READ|unix.PROT_WRITE, unix.MAP_SHARED)
	if err != nil {
		unix.Munmap(cqAddr)
		unix.Munmap(sqAddr)
		syscall.Close(int(ringFd))
		return nil, fmt.Errorf("failed to mmap SQEs: %v", err)
	}

	// Pre-allocate pool sizes based on queue depth
	// CQE pool needs to be larger since multiple completions can arrive at once
	cqePoolSize := int(params.cqEntries)
	if cqePoolSize < 64 {
		cqePoolSize = 64 // Minimum pool size
	}

	r := &minimalRing{
		ringFd:      int(ringFd),
		targetFd:    int(ctrlFd),
		params:      params,
		sqAddr:      unsafe.Pointer(&sqAddr[0]),
		cqAddr:      unsafe.Pointer(&cqAddr[0]),
		sqesAddr:    unsafe.Pointer(&sqesAddr[0]),
		resultsPool: make([]Result, 0, cqePoolSize),
		cqePoolSize: cqePoolSize,
		cqePool:     make([]minimalResult, cqePoolSize),
	}

	// Register the char device FD with io_uring (like C code does)
	// Required for queue operations
	if ctrlFd >= 0 {
		fds := []int32{ctrlFd}
		if err := r.RegisterFiles(fds); err != nil {
			logger.Warn("failed to register files with io_uring", "error", err)
			// Continue anyway - might not be required on all kernels
		} else {
			logger.Info("registered char device with io_uring", "fd", ctrlFd)
		}
	}

	return r, nil
}

// SubmitCtrlCmdAsync submits command without waiting
func (r *minimalRing) SubmitCtrlCmdAsync(cmd uint32, ctrlCmd *uapi.UblksrvCtrlCmd, userData uint64) (*AsyncHandle, error) {
	logger := logging.Default()
	logger.Debug("submitting async ctrl command", "cmd_hex", fmt.Sprintf("0x%08x", cmd), "dev_id", ctrlCmd.DevID)

	// Keep the buffer alive until kernel copies it
	var bufferPtr unsafe.Pointer
	if ctrlCmd.Addr != 0 {
		bufferPtr = unsafe.Pointer(uintptr(ctrlCmd.Addr))
		defer runtime.KeepAlive(bufferPtr)
	}

	// Create URING_CMD SQE for control operations (same as synchronous version)
	sqe := &sqe128{}

	// Zero all fields first
	for i := range sqe.union0 {
		sqe.union0[i] = 0
	}
	// No _pad64 field anymore - cmd area starts at byte 48
	for i := range sqe.cmd {
		sqe.cmd[i] = 0
	}

	// Set the base SQE fields
	sqe.opcode = kernelUringCmdOpcode()
	sqe.flags = 0
	sqe.ioprio = 0
	sqe.fd = int32(r.targetFd)
	sqe.addr = 0
	sqe.len = uint32(ctrlCmd.Len)
	sqe.opcodeFlags = 0
	sqe.bufIndex = 0
	sqe.personality = 0
	sqe.spliceFdIn = 0
	// fileIndex removed - part of cmd area now
	sqe.userData = userData

	// Marshal and place control command
	ctrlCmdBytes := uapi.Marshal(ctrlCmd)
	if len(ctrlCmdBytes) != 32 {
		return nil, fmt.Errorf("control command marshal returned %d bytes, expected 32", len(ctrlCmdBytes))
	}

	sqe.setCmdOp(cmd)

	// Place control command at byte 48
	controlCmdArea := (*[32]byte)(unsafe.Pointer(uintptr(unsafe.Pointer(sqe)) + 48))
	copy(controlCmdArea[:], ctrlCmdBytes)

	// Submit without waiting
	if err := r.submitToRing(sqe); err != nil {
		return nil, err
	}

	// Call io_uring_enter to submit but don't wait
	submitted, errno := r.submitOnly(1)
	if errno != 0 || submitted != 1 {
		return nil, fmt.Errorf("failed to submit: %v", errno)
	}

	logger.Debug("command submitted without waiting", "userData", userData)

	// Return handle for later polling
	return &AsyncHandle{
		userData: userData,
		ring:     r,
	}, nil
}

// submitToRing prepares and submits an SQE to the ring without calling io_uring_enter
func (r *minimalRing) submitToRing(sqe *sqe128) error {
	logger := logging.Default()

	// Get SQ head and tail
	sqHead := (*uint32)(unsafe.Add(r.sqAddr, r.params.sqOff.head))
	sqTail := (*uint32)(unsafe.Add(r.sqAddr, r.params.sqOff.tail))
	sqMask := r.params.sqEntries - 1

	// Check if queue is full
	if (*sqTail - *sqHead) >= r.params.sqEntries {
		return fmt.Errorf("submission queue full")
	}

	// Get SQE slot and copy our prepared SQE
	sqArray := (*uint32)(unsafe.Add(r.sqAddr, r.params.sqOff.array))
	sqIndex := *sqTail & sqMask
	sqeSlot := unsafe.Add(r.sqesAddr, 128*uintptr(sqIndex))
	*(*sqe128)(sqeSlot) = *sqe

	// Copy control command to correct offset if URING_CMD
	if sqe.opcode == kernelUringCmdOpcode() {
		srcCmd := (*[32]byte)(unsafe.Pointer(uintptr(unsafe.Pointer(sqe)) + 48))
		dstCmd := (*[32]byte)(unsafe.Pointer(uintptr(sqeSlot) + 48))
		copy(dstCmd[:], srcCmd[:])
	}

	// Update array entry
	*(*uint32)(unsafe.Add(unsafe.Pointer(sqArray), unsafe.Sizeof(uint32(0))*uintptr(sqIndex))) = sqIndex

	// CRITICAL: Store fence before tail update to ensure SQE is visible to kernel
	Sfence()

	// Update tail atomically
	atomic.StoreUint32(sqTail, *sqTail+1)

	logger.Debug("SQE prepared in ring", "index", sqIndex, "tail", *sqTail)
	return nil
}

// tryGetCompletion checks CQ for a specific completion
func (r *minimalRing) tryGetCompletion(userData uint64) (Result, error) {
	logger := logging.Default()

	// First, call io_uring_enter to force kernel to process any pending completions
	// This is critical for async operations as the kernel might not have pushed completions yet
	_, _, errno := r.submitAndWaitRing(0, 0) // submit=0, wait=0 but with GETEVENTS
	if errno != 0 {
		logger.Debug("io_uring_enter for completion processing failed", "errno", errno)
	}

	// Check CQ head/tail with proper atomic acquire semantics
	cqHead := (*uint32)(unsafe.Add(r.cqAddr, r.params.cqOff.head))
	cqTail := (*uint32)(unsafe.Add(r.cqAddr, r.params.cqOff.tail))

	// Load tail with acquire semantics (kernel publishes with release)
	currentTail := atomic.LoadUint32(cqTail)
	currentHead := atomic.LoadUint32(cqHead)

	logger.Debug("checking completions", "cqHead", currentHead, "cqTail", currentTail, "looking_for", userData)

	if currentHead == currentTail {
		return nil, fmt.Errorf("no completions available")
	}

	// Process completions looking for our userData
	cqMask := r.params.cqEntries - 1

	for currentHead != currentTail {
		index := currentHead & cqMask
		cqeSlot := unsafe.Add(r.cqAddr, uintptr(r.params.cqOff.cqes)+uintptr(unsafe.Sizeof(cqe32{})*uintptr(index)))
		cqe := (*cqe32)(cqeSlot)

		logger.Debug("found completion", "index", index, "userData", cqe.userData, "res", cqe.res)

		if cqe.userData == userData {
			// Found our completion - advance head with release semantics
			atomic.StoreUint32(cqHead, currentHead+1)

			result := &minimalResult{
				userData: cqe.userData,
				value:    cqe.res,
				err:      nil,
			}

			if cqe.res < 0 {
				result.err = fmt.Errorf("operation failed with result: %d", cqe.res)
			}

			logger.Debug("found matching completion", "userData", userData, "result", cqe.res)
			return result, nil
		}

		currentHead++
	}

	// Didn't find our completion - don't modify head
	return nil, fmt.Errorf("completion not found")
}

func (r *minimalRing) Close() error {
	// This is a minimal implementation - full cleanup would unmap regions
	return syscall.Close(r.ringFd)
}

// RegisterFiles registers file descriptors with io_uring for IOSQE_FIXED_FILE operations
func (r *minimalRing) RegisterFiles(fds []int32) error {
	const IORING_REGISTER_FILES = 2

	// Convert []int32 to unsafe pointer
	var ptr unsafe.Pointer
	if len(fds) > 0 {
		ptr = unsafe.Pointer(&fds[0])
	}

	_, _, errno := syscall.Syscall6(
		unix.SYS_IO_URING_REGISTER,
		uintptr(r.ringFd),
		IORING_REGISTER_FILES,
		uintptr(ptr),
		uintptr(len(fds)),
		0, 0)

	if errno != 0 {
		return fmt.Errorf("io_uring_register files failed: %v", errno)
	}
	return nil
}

func (r *minimalRing) SubmitCtrlCmd(cmd uint32, ctrlCmd *uapi.UblksrvCtrlCmd, userData uint64) (Result, error) {
	logger := logging.Default()

	logger.Debug("submitting ctrl command", "cmd_hex", fmt.Sprintf("0x%08x", cmd), "dev_id", ctrlCmd.DevID)
	logger.Debug("preparing URING_CMD", "cmd", cmd, "dev_id", ctrlCmd.DevID)

	// Log the actual command being used
	logger.Debug("using command", "cmd", cmd)

	// Keep the buffer alive until kernel copies it
	// The kernel needs to access this memory during the syscall
	var bufferPtr unsafe.Pointer
	if ctrlCmd.Addr != 0 {
		bufferPtr = unsafe.Pointer(uintptr(ctrlCmd.Addr))
		defer runtime.KeepAlive(bufferPtr)
	}

	// Create URING_CMD SQE for control operations
	// The 32-byte ublksrv_ctrl_cmd is placed in the SQE cmd area
	sqe := &sqe128{}

	// Zero all fields first to ensure clean state
	for i := range sqe.union0 {
		sqe.union0[i] = 0
	}
	// No _pad64 field anymore - cmd area starts at byte 48
	for i := range sqe.cmd {
		sqe.cmd[i] = 0
	}

	// Set the base SQE fields
	sqe.opcode = kernelUringCmdOpcode()
	sqe.flags = 0
	sqe.ioprio = 0
	sqe.fd = int32(r.targetFd)

	// addr field is 0 for URING_CMD operations
	sqe.addr = 0
	sqe.len = uint32(ctrlCmd.Len)
	sqe.opcodeFlags = 0
	sqe.bufIndex = 0
	sqe.personality = 0
	sqe.spliceFdIn = 0
	// fileIndex removed - part of cmd area now

	// Set userData from caller
	sqe.userData = userData

	// Marshal the 32-byte control command
	ctrlCmdBytes := uapi.Marshal(ctrlCmd)
	if len(ctrlCmdBytes) != 32 {
		return nil, fmt.Errorf("control command marshal returned %d bytes, expected 32", len(ctrlCmdBytes))
	}

	// Set cmd_op field to ioctl-encoded value (like working C implementation)
	sqe.setCmdOp(cmd)

	// With sqe128 layout, sqe.cmd starts at byte 48
	// Copy the 32-byte control command to the cmd area
	copy(sqe.cmd[:32], ctrlCmdBytes)

	logger.Debug("SQE prepared", "fd", sqe.fd, "cmd", cmd, "addr", sqe.addr)

	// START_DEV must wait for completion

	// Submit the command and wait for completion using real io_uring
	result, err := r.submitAndWait(sqe)
	if err != nil {
		logger.Error("submitAndWait failed", "error", err)
		return nil, fmt.Errorf("failed to submit control command: %v", err)
	}

	logger.Debug("URING_CMD completed", "result", result.Value(), "error", result.Error())
	return result, nil
}

// minimalResult implements the Result interface
type minimalResult struct {
	userData uint64
	value    int32
	err      error
}

func (r *minimalResult) UserData() uint64 { return r.userData }
func (r *minimalResult) Value() int32     { return r.value }
func (r *minimalResult) Error() error     { return r.err }

func (r *minimalRing) SubmitIOCmd(cmd uint32, ioCmd *uapi.UblksrvIOCmd, userData uint64) (Result, error) {
	// Hot path optimization: Use pre-allocated sqePool instead of heap allocation
	// Reuse the same sqe128 struct for all submissions (sequential per ring)
	sqe := &r.sqePool

	// Set minimal SQE fields (kernel expects these)
	sqe.opcode = kernelUringCmdOpcode()
	sqe.flags = 0
	sqe.ioprio = 0
	sqe.fd = int32(r.targetFd)
	sqe.setCmdOp(cmd)
	sqe.userData = userData
	sqe.len = 16 // 16-byte ublksrv_io_cmd payload
	sqe.opcodeFlags = 0
	sqe.bufIndex = 0
	sqe.personality = 0
	sqe.spliceFdIn = 0
	sqe.addr = 0

	// Copy the ublksrv_io_cmd (16 bytes) to cmd area
	// Using direct assignment is faster than copy() for small fixed sizes
	*(*[16]byte)(unsafe.Pointer(&sqe.cmd[0])) = *(*[16]byte)(unsafe.Pointer(ioCmd))

	// Zero remaining cmd area (bytes 16-79) - required for kernel
	// Use 64-bit writes for efficiency
	*(*uint64)(unsafe.Pointer(&sqe.cmd[16])) = 0
	*(*uint64)(unsafe.Pointer(&sqe.cmd[24])) = 0
	*(*uint64)(unsafe.Pointer(&sqe.cmd[32])) = 0
	*(*uint64)(unsafe.Pointer(&sqe.cmd[40])) = 0
	*(*uint64)(unsafe.Pointer(&sqe.cmd[48])) = 0
	*(*uint64)(unsafe.Pointer(&sqe.cmd[56])) = 0
	*(*uint64)(unsafe.Pointer(&sqe.cmd[64])) = 0
	*(*uint64)(unsafe.Pointer(&sqe.cmd[72])) = 0

	// Submit the command and flush to kernel
	if _, err := r.submitOnlyCmd(sqe); err != nil {
		return nil, fmt.Errorf("failed to submit I/O command: %w", err)
	}

	// Make sure the payload stays alive until after submission.
	runtime.KeepAlive(ioCmd)

	return &minimalResult{userData: userData, value: 0, err: nil}, nil
}

func (r *minimalRing) WaitForCompletion(timeout int) ([]Result, error) {
	// Hot path optimization: Reuse pre-allocated results slice
	// Reset length to 0 but keep capacity
	r.resultsPool = r.resultsPool[:0]
	r.cqePoolIndex = 0 // Reset pool index for this batch

	drain := func() {
		cqHead := (*uint32)(unsafe.Add(r.cqAddr, r.params.cqOff.head))
		cqTail := (*uint32)(unsafe.Add(r.cqAddr, r.params.cqOff.tail))

		// Load tail with acquire semantics (kernel publishes with release)
		currentTail := atomic.LoadUint32(cqTail)

		// CRITICAL: Full memory barrier to ensure CQE data is visible
		// after we see the updated tail from the kernel. The kernel does
		// a release store to tail after writing CQE data, so we need an
		// acquire barrier here to ensure we see that data.
		Mfence()

		currentHead := atomic.LoadUint32(cqHead)

		// Pre-calculate constant offset for cqe slot computation
		cqMask := r.params.cqEntries - 1
		cqeBase := uintptr(r.params.cqOff.cqes)
		cqeSize := uintptr(unsafe.Sizeof(cqe32{}))

		for currentHead != currentTail {
			cqIndex := currentHead & cqMask
			cqeSlot := unsafe.Add(r.cqAddr, cqeBase+cqeSize*uintptr(cqIndex))
			cqe := (*cqe32)(cqeSlot)

			// Use pre-allocated result struct from pool
			var res *minimalResult
			if r.cqePoolIndex < r.cqePoolSize {
				res = &r.cqePool[r.cqePoolIndex]
				r.cqePoolIndex++
			} else {
				// Pool exhausted - fall back to allocation (rare)
				res = &minimalResult{}
			}

			res.userData = cqe.userData
			res.value = cqe.res
			res.err = nil // Don't allocate error string - caller checks Value()

			r.resultsPool = append(r.resultsPool, res)
			currentHead++
		}

		// Update head with release semantics only if we consumed completions
		if currentHead != atomic.LoadUint32(cqHead) {
			atomic.StoreUint32(cqHead, currentHead)
		}
	}

	// First, non-blocking drain
	drain()
	if len(r.resultsPool) > 0 {
		return r.resultsPool, nil
	}

	// If timeout is specified, don't block forever
	if timeout > 0 {
		// Don't wait for any completions, just check if there are any
		_, _, _ = r.submitAndWaitRing(0, 0)
		drain()
		return r.resultsPool, nil // Return empty slice if no work - NOT an error
	}

	// Block for at least one completion (only if no timeout)
	// Retry on EINTR - signals can interrupt the syscall
	for {
		_, _, errno := r.submitAndWaitRing(0, 1)
		if errno == 0 {
			break
		}
		if errno == syscall.EINTR {
			// Signal interrupted us, retry
			continue
		}
		return nil, fmt.Errorf("io_uring_enter wait failed: %v", errno)
	}

	// Drain whatever arrived
	drain()
	return r.resultsPool, nil // Always return slice, even if empty
}

func (r *minimalRing) NewBatch() Batch {
	return &minimalBatch{}
}

// Minimal batch implementation
type minimalBatch struct{}

func (b *minimalBatch) AddCtrlCmd(cmd uint32, ctrlCmd *uapi.UblksrvCtrlCmd, userData uint64) error {
	return fmt.Errorf("batch not implemented in minimal ring")
}

func (b *minimalBatch) AddIOCmd(cmd uint32, ioCmd *uapi.UblksrvIOCmd, userData uint64) error {
	return fmt.Errorf("batch not implemented in minimal ring")
}

func (b *minimalBatch) Submit() ([]Result, error) {
	return nil, fmt.Errorf("batch not implemented in minimal ring")
}

func (b *minimalBatch) Len() int {
	return 0
}

// submitAndWait submits an SQE and waits for completion using real io_uring
func (r *minimalRing) submitAndWait(sqe *sqe128) (Result, error) {
	logger := logging.Default()
	logger.Debug("submitAndWait called", "fd", sqe.fd, "opcode", sqe.opcode)
	logger.Debug("submitting URING_CMD via io_uring", "fd", sqe.fd, "opcode", sqe.opcode)

	// This is the real io_uring submission implementation
	// Step 1: Get next available SQ entry
	sqHead := (*uint32)(unsafe.Add(r.sqAddr, r.params.sqOff.head))
	sqTail := (*uint32)(unsafe.Add(r.sqAddr, r.params.sqOff.tail))
	sqMask := r.params.sqEntries - 1

	// Check if queue is full
	if (*sqTail - *sqHead) >= r.params.sqEntries {
		return nil, fmt.Errorf("submission queue full")
	}

	// Step 2: Get SQE slot and copy our prepared SQE into SQEs mapping
	sqArray := (*uint32)(unsafe.Add(r.sqAddr, r.params.sqOff.array))
	sqIndex := *sqTail & sqMask

	// Calculate SQE slot offset
	sqeSize := unsafe.Sizeof(*sqe)
	if sqeSize != 128 {
		logger.Error("SQE size mismatch during submission", "size", sqeSize)
	}
	sqeSlot := unsafe.Add(r.sqesAddr, sqeSize*uintptr(sqIndex))

	// Copy our SQE to the SQEs array
	*(*sqe128)(sqeSlot) = *sqe

	// For URING_CMD, write control command directly to sqeSlot at byte 48
	if sqe.opcode == kernelUringCmdOpcode() {
		// Extract and copy the control command
		srcCmd := (*[32]byte)(unsafe.Pointer(uintptr(unsafe.Pointer(sqe)) + 48))
		dstCmd := (*[32]byte)(unsafe.Pointer(uintptr(sqeSlot) + 48))
		copy(dstCmd[:], srcCmd[:])
	}

	// Update array entry
	*(*uint32)(unsafe.Add(unsafe.Pointer(sqArray), uintptr(4*sqIndex))) = sqIndex

	// Step 3: Update tail to submit the entry
	oldTail := *sqTail
	newTail := oldTail + 1

	// CRITICAL: Full store fence to ensure SQE writes are visible to kernel
	// before we update the tail. runtime.KeepAlive and atomic operations
	// do NOT provide this guarantee for non-atomic stores.
	Sfence()

	// Use atomic store to ensure the tail update is visible to the kernel
	atomic.StoreUint32(sqTail, newTail)

	logger.Debug("updated SQ tail", "old", oldTail, "new", newTail)

	// Submit and wait for completion
	submitted, completed, errno := r.submitAndWaitRing(1, 1)
	if errno != 0 {
		logger.Error("io_uring_enter failed", "errno", errno, "submitted", submitted, "completed", completed)
		return nil, fmt.Errorf("io_uring_enter failed: %v", errno)
	}

	logger.Debug("io_uring_enter succeeded", "submitted", submitted, "completed", completed)

	// Step 5: Process completion
	return r.processCompletion()
}

// submitAndWaitRing calls io_uring_enter to submit and wait for completions
func (r *minimalRing) submitAndWaitRing(toSubmit, minComplete uint32) (submitted, completed uint32, errno syscall.Errno) {
	logger := logging.Default()
	const (
		IORING_ENTER_GETEVENTS = 1 << 0
	)

	// Only use GETEVENTS flag if we're actually waiting for completions
	var flags uint32
	if minComplete > 0 {
		flags = IORING_ENTER_GETEVENTS
	}

	logger.Debug("calling io_uring_enter", "toSubmit", toSubmit, "minComplete", minComplete, "flags", flags)

	r1, r2, err := syscall.Syscall6(
		unix.SYS_IO_URING_ENTER,
		uintptr(r.ringFd),
		uintptr(toSubmit),
		uintptr(minComplete),
		uintptr(flags),
		0, 0)

	logger.Debug("io_uring_enter returned", "r1", r1, "r2", r2, "err", err)

	return uint32(r1), uint32(r2), err
}

// submitOnly calls io_uring_enter to submit without waiting
func (r *minimalRing) submitOnly(toSubmit uint32) (submitted uint32, errno syscall.Errno) {
	r1, _, err := syscall.Syscall6(
		unix.SYS_IO_URING_ENTER,
		uintptr(r.ringFd),
		uintptr(toSubmit),
		0, // don't wait for completions
		0, // no flags
		0, 0)

	return uint32(r1), err
}

// submitOnlyCmd submits a command SQE without waiting for completion
func (r *minimalRing) submitOnlyCmd(sqe *sqe128) (uint32, error) {
	// Get SQ head and tail
	sqHead := (*uint32)(unsafe.Add(r.sqAddr, r.params.sqOff.head))
	sqTail := (*uint32)(unsafe.Add(r.sqAddr, r.params.sqOff.tail))
	sqMask := r.params.sqEntries - 1

	// Check if queue is full
	if (*sqTail - *sqHead) >= r.params.sqEntries {
		return 0, fmt.Errorf("submission queue full")
	}

	// Get SQE slot and copy our prepared SQE
	sqArray := (*uint32)(unsafe.Add(r.sqAddr, r.params.sqOff.array))
	sqIndex := *sqTail & sqMask
	sqeSlot := unsafe.Add(r.sqesAddr, 128*uintptr(sqIndex))
	*(*sqe128)(sqeSlot) = *sqe
	// NOTE: No extra copy needed - the struct copy includes the cmd area at bytes 48-127

	// Update array entry
	*(*uint32)(unsafe.Add(unsafe.Pointer(sqArray), unsafe.Sizeof(uint32(0))*uintptr(sqIndex))) = sqIndex

	// Update tail with proper memory ordering
	oldTail := *sqTail
	newTail := oldTail + 1

	// CRITICAL: Full store fence to ensure SQE writes are visible to kernel
	// before we update the tail. This is the key fix for the race condition.
	Sfence()

	atomic.StoreUint32(sqTail, newTail)

	// Submit without waiting
	submitted, errno := r.submitOnly(1)
	if errno != 0 {
		return 0, fmt.Errorf("io_uring_enter failed: %v", errno)
	}

	return submitted, nil
}

// processCompletion processes a completion from the CQ ring
func (r *minimalRing) processCompletion() (Result, error) {
	logger := logging.Default()

	// Get CQ head and tail
	cqHead := (*uint32)(unsafe.Add(r.cqAddr, r.params.cqOff.head))
	cqTail := (*uint32)(unsafe.Add(r.cqAddr, r.params.cqOff.tail))

	// Read tail with acquire semantics (kernel publishes with release)
	currentTail := atomic.LoadUint32(cqTail)
	currentHead := *cqHead

	// Check if we have completions, with a retry loop for memory visibility.
	// After io_uring_enter returns, the kernel has updated CQ tail, but the
	// store may not be visible to this CPU yet due to cache coherence latency.
	// 5 retries * 10µs = 50µs max wait, which is sufficient for cross-CPU
	// visibility on modern x86-64 systems (typically <1µs).
	const maxRetries = 5
	const retryDelay = 10 * time.Microsecond
	for i := 0; i < maxRetries; i++ {
		currentTail = atomic.LoadUint32(cqTail)
		if currentHead != currentTail {
			break
		}
		time.Sleep(retryDelay)
	}

	if currentHead == currentTail {
		logger.Warn("no completions available after retries")
		return nil, fmt.Errorf("no completions available after retries")
	}

	// Get CQE
	cqMask := r.params.cqEntries - 1
	cqIndex := currentHead & cqMask
	cqeSlot := unsafe.Add(r.cqAddr, uintptr(r.params.cqOff.cqes)+uintptr(unsafe.Sizeof(cqe32{})*uintptr(cqIndex)))
	cqe := (*cqe32)(cqeSlot)

	logger.Debug("processing completion", "user_data", cqe.userData, "res", cqe.res, "flags", cqe.flags)

	// Extract result
	result := &minimalResult{
		userData: cqe.userData,
		value:    cqe.res,
		err:      nil,
	}

	if cqe.res < 0 {
		result.err = fmt.Errorf("operation failed with result: %d", cqe.res)
	}

	// Update head with release semantics to consume the completion
	atomic.StoreUint32(cqHead, currentHead+1)

	return result, nil
}

// performControlOperation performs the actual kernel communication for control operations
func (r *minimalRing) performControlOperation(cmd uint32, ctrlCmd *uapi.UblksrvCtrlCmd) (int32, syscall.Errno) {
	// Not used; URING_CMD is implemented via submitAndWait
	return 0, syscall.ENOSYS
}
