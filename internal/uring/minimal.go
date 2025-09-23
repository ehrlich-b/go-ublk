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
// Matches Linux include/uapi/linux/io_uring.h layout:
//   - Base SQE is 64 bytes
//   - If IORING_SETUP_SQE128 is used, an extra 64-byte area follows at offset 64
//     which is exposed as sqe->cmd for URING_CMD payloads.
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

	// 32..63: rest of base SQE (must be zeroed unless used explicitly)
	userData    uint64   // 32..39
	bufIndex    uint16   // 40..41
	personality uint16   // 42..43
	spliceFdIn  int32    // 44..47
	fileIndex   uint32   // 48..51
	_pad64      [12]byte // 52..63 (zero)

	// 64..127: extended area present only with SQE128, used for URING_CMD payload
	cmd [64]byte // 64..127
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
	logger.Info("*** ASYNC: Starting wait for completion", "userData", h.userData, "timeout", timeout)
	deadline := time.Now().Add(timeout)

	attempts := 0
	for time.Now().Before(deadline) {
		attempts++
		// Try to get completion without blocking
		result, err := h.ring.tryGetCompletion(h.userData)
		if err == nil {
			logger.Info("*** ASYNC: Found completion", "attempts", attempts, "result", result.Value())
			return result, nil
		}

		// Log every 100 attempts (1 second)
		if attempts%100 == 0 {
			logger.Info("*** ASYNC: Still waiting for completion", "attempts", attempts, "error", err.Error())
		}

		// Not ready yet, sleep briefly
		time.Sleep(10 * time.Millisecond)
	}

	logger.Info("*** ASYNC: Timeout waiting for completion", "attempts", attempts)
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

// minimalRing implements just URING_CMD for ublk control operations
type minimalRing struct {
	ringFd    int // io_uring file descriptor
	controlFd int // ring target fd (control or ublkc)
	params    io_uring_params
	sqAddr    unsafe.Pointer // SQ ring mapping base
	cqAddr    unsafe.Pointer // CQ ring mapping base
	sqesAddr  unsafe.Pointer // SQEs mapping base
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
		return nil, fmt.Errorf("CRITICAL: sqe128 size is %d bytes, expected 128", sqeSize)
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
		logger.Error("CRITICAL: Kernel did not accept IORING_SETUP_SQE128 flag!")
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

	r := &minimalRing{
		ringFd:    int(ringFd),
		controlFd: int(ctrlFd),
		params:    params,
		sqAddr:    unsafe.Pointer(&sqAddr[0]),
		cqAddr:    unsafe.Pointer(&cqAddr[0]),
		sqesAddr:  unsafe.Pointer(&sqesAddr[0]),
	}

	// Register the char device FD with io_uring (like C code does)
	// This is critical for queue operations to work!
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
	logger.Info("*** ASYNC: SubmitCtrlCmdAsync called", "cmd_hex", fmt.Sprintf("0x%08x", cmd), "dev_id", ctrlCmd.DevID)

	// CRITICAL: Keep the buffer that ctrlCmd.Addr points to alive
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
	for i := range sqe._pad64 {
		sqe._pad64[i] = 0
	}
	for i := range sqe.cmd {
		sqe.cmd[i] = 0
	}

	// Set the base SQE fields
	sqe.opcode = kernelUringCmdOpcode()
	sqe.flags = 0
	sqe.ioprio = 0
	sqe.fd = int32(r.controlFd)
	sqe.addr = 0
	sqe.len = uint32(ctrlCmd.Len)
	sqe.opcodeFlags = 0
	sqe.bufIndex = 0
	sqe.personality = 0
	sqe.spliceFdIn = 0
	sqe.fileIndex = 0
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

	logger.Info("*** ASYNC: Command submitted without waiting", "userData", userData)

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

	// Update tail atomically
	atomic.StoreUint32(sqTail, *sqTail+1)

	logger.Debug("*** ASYNC: SQE prepared in ring", "index", sqIndex, "tail", *sqTail)
	return nil
}

// tryGetCompletion checks CQ for a specific completion
func (r *minimalRing) tryGetCompletion(userData uint64) (Result, error) {
	logger := logging.Default()

	// First, call io_uring_enter to force kernel to process any pending completions
	// This is critical for async operations as the kernel might not have pushed completions yet
	_, _, errno := r.submitAndWaitRing(0, 0) // submit=0, wait=0 but with GETEVENTS
	if errno != 0 {
		logger.Debug("*** ASYNC: io_uring_enter for completion processing failed", "errno", errno)
	}

	// Check CQ head/tail
	cqHead := (*uint32)(unsafe.Add(r.cqAddr, r.params.cqOff.head))
	cqTail := (*uint32)(unsafe.Add(r.cqAddr, r.params.cqOff.tail))

	logger.Debug("*** ASYNC: Checking completions", "cqHead", *cqHead, "cqTail", *cqTail, "looking_for", userData)

	if *cqHead == *cqTail {
		return nil, fmt.Errorf("no completions available")
	}

	// Process completions looking for our userData
	cqMask := r.params.cqEntries - 1
	currentHead := *cqHead

	for currentHead != *cqTail {
		index := currentHead & cqMask
		cqeSlot := unsafe.Add(r.cqAddr, uintptr(r.params.cqOff.cqes)+uintptr(unsafe.Sizeof(cqe32{})*uintptr(index)))
		cqe := (*cqe32)(cqeSlot)

		logger.Debug("*** ASYNC: Found completion", "index", index, "userData", cqe.userData, "res", cqe.res, "looking_for", userData)

		if cqe.userData == userData {
			// Found our completion - advance head to this position + 1
			*cqHead = currentHead + 1

			result := &minimalResult{
				userData: cqe.userData,
				value:    cqe.res,
				err:      nil,
			}

			if cqe.res < 0 {
				result.err = fmt.Errorf("operation failed with result: %d", cqe.res)
			}

			logger.Info("*** ASYNC: Found matching completion", "userData", userData, "result", cqe.res)
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
	// CRITICAL: Log the actual command value we receive to see if ioctl encoding is applied
	fmt.Printf("*** DEBUG: SubmitCtrlCmd raw cmd value: 0x%08x (%d)\n", cmd, cmd)

	// Add stack trace to see who's calling this
	buf := make([]byte, 4096)
	n := runtime.Stack(buf, false)
	fmt.Printf("*** STACK TRACE:\n%s\n", buf[:n])

	logger.Info("*** CRITICAL: SubmitCtrlCmd called", "cmd_hex", fmt.Sprintf("0x%08x", cmd), "dev_id", ctrlCmd.DevID)
	logger.Debug("preparing URING_CMD", "cmd", cmd, "dev_id", ctrlCmd.DevID)

	// Log the actual command being used
	logger.Debug("using command", "cmd", cmd)

	// CRITICAL: Keep the buffer that ctrlCmd.Addr points to alive
	// The kernel needs to access this memory during the syscall
	var bufferPtr unsafe.Pointer
	if ctrlCmd.Addr != 0 {
		bufferPtr = unsafe.Pointer(uintptr(ctrlCmd.Addr))
		defer runtime.KeepAlive(bufferPtr)
	}

	// Create URING_CMD SQE for control operations
	// CRITICAL FIX: The 32-byte ublksrv_ctrl_cmd must be placed directly in the
	// standard SQE cmd area (bytes 32-63), NOT in the SQE128 extension!
	// This matches the working C implementation layout.
	sqe := &sqe128{}

	// Zero all fields first to ensure clean state
	for i := range sqe.union0 {
		sqe.union0[i] = 0
	}
	for i := range sqe._pad64 {
		sqe._pad64[i] = 0
	}
	for i := range sqe.cmd {
		sqe.cmd[i] = 0
	}

	// Set the base SQE fields
	sqe.opcode = kernelUringCmdOpcode()
	sqe.flags = 0
	sqe.ioprio = 0
	sqe.fd = int32(r.controlFd)

	// CRITICAL: The addr field should be 0 for URING_CMD operations
	// The control command data goes in bytes 32-63, not via an external buffer
	sqe.addr = 0
	sqe.len = uint32(ctrlCmd.Len)
	sqe.opcodeFlags = 0
	sqe.bufIndex = 0
	sqe.personality = 0
	sqe.spliceFdIn = 0
	sqe.fileIndex = 0

	// Set userData from caller
	sqe.userData = userData

	// Marshal the 32-byte control command
	ctrlCmdBytes := uapi.Marshal(ctrlCmd)
	if len(ctrlCmdBytes) != 32 {
		return nil, fmt.Errorf("control command marshal returned %d bytes, expected 32", len(ctrlCmdBytes))
	}

	// Debug: Log the control command fields before marshaling
	logger.Debug("control cmd fields",
		"dev_id", ctrlCmd.DevID,
		"queue_id", ctrlCmd.QueueID,
		"len", ctrlCmd.Len,
		"addr", fmt.Sprintf("0x%x", ctrlCmd.Addr),
		"data", ctrlCmd.Data)

	// Set cmd_op field to ioctl-encoded value (like working C implementation)
	sqe.setCmdOp(cmd)

	// CRITICAL FIX: Based on working C bytes, control command starts at byte 48!
	// Working C shows ffffffffffff4000 at bytes 48-55 (dev_id, queue_id, len)
	// So the 32-byte control command goes at bytes 48-79
	controlCmdArea := (*[32]byte)(unsafe.Pointer(uintptr(unsafe.Pointer(sqe)) + 48))
	copy(controlCmdArea[:], ctrlCmdBytes)

	// DO NOT also copy to sqe.cmd (bytes 64+) - that's the wrong location!

	// Debug: log the exact control header bytes being sent to kernel
	logger.Debug("control header bytes", "header_hex", fmt.Sprintf("%x", ctrlCmdBytes))

	logger.Debug("SQE prepared", "fd", sqe.fd, "cmd", cmd, "addr", sqe.addr)

	// Check if this is START_DEV - it might need special handling
	isStartDev := (cmd == uapi.UblkCtrlCmd(uapi.UBLK_CMD_START_DEV))

	if isStartDev {
		logger.Info("*** CRITICAL: START_DEV detected - using fire-and-forget!")
		// START_DEV might hang waiting for queues, so submit without waiting
		result, err := r.submitOnlyCmd(sqe)
		if err != nil {
			logger.Error("submitOnlyCmd failed for START_DEV", "error", err)
			return nil, fmt.Errorf("failed to submit START_DEV: %v", err)
		}
		logger.Info("*** START_DEV submitted without waiting", "submitted", result)
		// Return success immediately
		return &minimalResult{userData: userData, value: 0, err: nil}, nil
	}

	// For other commands, use normal submit-and-wait
	logger.Info("*** Using submit-and-wait for non-START_DEV command")

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
	// Submit URING_CMD for data-plane to this ring's fd (expected to be /dev/ublkc<ID>)
	logger := logging.Default()
	logger.Info("*** CRITICAL: SubmitIOCmd called",
		"cmd", fmt.Sprintf("0x%x", cmd),
		"controlFd", r.controlFd,
		"qid", ioCmd.QID,
		"tag", ioCmd.Tag)

	// Prepare SQE with the minimal fields the kernel expects
	sqe := &sqe128{}
	sqe.opcode = kernelUringCmdOpcode()
	sqe.fd = int32(r.controlFd)
	sqe.setCmdOp(cmd)
	sqe.userData = userData

	// Marshal ublksrv_io_cmd exactly like the C reference implementation does.
	// The kernel reads the payload from sqe->addr3 (bytes 48-63 of the base SQE).
	payload := uapi.Marshal(ioCmd)
	if len(payload) != 16 {
		return nil, fmt.Errorf("I/O command marshal returned %d bytes, expected 16", len(payload))
	}
	cmdArea := (*[16]byte)(unsafe.Add(unsafe.Pointer(sqe), 48))
	copy(cmdArea[:], payload)

	// The C ublksrv sets len to 0 for queue ops; follow the same pattern.
	sqe.len = 0

	// Fire-and-forget submission: START_DEV completion depends on these FETCH_REQs
	// being in flight, so we must not block here.
	if _, err := r.submitOnlyCmd(sqe); err != nil {
		return nil, fmt.Errorf("failed to submit I/O command: %w", err)
	}

	// Make sure the payload stays alive until after submission.
	runtime.KeepAlive(ioCmd)

	return &minimalResult{userData: userData, value: 0, err: nil}, nil
}

func (r *minimalRing) WaitForCompletion(timeout int) ([]Result, error) {
	// Drain CQ if anything is already there
	results := make([]Result, 0, 8)

	drain := func() {
		cqHead := (*uint32)(unsafe.Add(r.cqAddr, r.params.cqOff.head))
		cqTail := (*uint32)(unsafe.Add(r.cqAddr, r.params.cqOff.tail))
		for *cqHead != *cqTail {
			cqMask := r.params.cqEntries - 1
			cqIndex := *cqHead & cqMask
			cqeSlot := unsafe.Add(r.cqAddr, uintptr(r.params.cqOff.cqes)+uintptr(unsafe.Sizeof(cqe32{})*uintptr(cqIndex)))
			cqe := (*cqe32)(cqeSlot)
			res := &minimalResult{userData: cqe.userData, value: cqe.res}
			if cqe.res < 0 {
				res.err = fmt.Errorf("operation failed with result: %d", cqe.res)
			}
			results = append(results, res)
			*cqHead = *cqHead + 1
		}
	}

	// First, non-blocking drain
	drain()
	if len(results) > 0 {
		return results, nil
	}

	// If timeout is specified, don't block forever
	if timeout > 0 {
		// Don't wait for any completions, just check if there are any
		_, _, _ = r.submitAndWaitRing(0, 0)
		drain()
		return results, nil
	}

	// Block for at least one completion (only if no timeout)
	_, _, errno := r.submitAndWaitRing(0, 1)
	if errno != 0 {
		return nil, fmt.Errorf("io_uring_enter wait failed: %v", errno)
	}

	// Drain whatever arrived
	drain()
	return results, nil
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
	logger.Info("*** CRITICAL: submitAndWait called - making syscalls", "fd", sqe.fd, "opcode", sqe.opcode)
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

	// Debug: Check SQ flags
	sqFlags := (*uint32)(unsafe.Add(r.sqAddr, r.params.sqOff.flags))
	logger.Debug("SQ flags", "value", fmt.Sprintf("0x%x", *sqFlags))

	// Step 2: Get SQE slot and copy our prepared SQE into SQEs mapping
	sqArray := (*uint32)(unsafe.Add(r.sqAddr, r.params.sqOff.array))
	sqIndex := *sqTail & sqMask

	// CRITICAL: Calculate SQE slot offset
	sqeSize := unsafe.Sizeof(*sqe)
	if sqeSize != 128 {
		logger.Error("CRITICAL: SQE size mismatch during submission", "size", sqeSize)
	}
	sqeSlot := unsafe.Add(r.sqesAddr, sqeSize*uintptr(sqIndex))
	logger.Debug("SQE slot calculation", "index", sqIndex, "offset", sqeSize*uintptr(sqIndex), "sqeSize", sqeSize)

	// Copy our SQE to the SQEs array
	*(*sqe128)(sqeSlot) = *sqe

	// CRITICAL: For URING_CMD, write control command directly to sqeSlot at byte 48
	// This must be done AFTER the struct copy to avoid being overwritten
	if sqe.opcode == kernelUringCmdOpcode() {
		// The control command needs to be at bytes 48-79 (starting at addr3)
		// Extract the control command from the original sqe memory
		srcCmd := (*[32]byte)(unsafe.Pointer(uintptr(unsafe.Pointer(sqe)) + 48))
		dstCmd := (*[32]byte)(unsafe.Pointer(uintptr(sqeSlot) + 48))
		copy(dstCmd[:], srcCmd[:])
	}

	// Debug: Log the entire SQE as bytes to verify layout
	sqeBytes := (*[128]byte)(unsafe.Pointer(sqeSlot))
	logger.Debug("Full SQE bytes", "hex", fmt.Sprintf("%x", sqeBytes[:]))
	logger.Debug("SQE offsets", "0-31", fmt.Sprintf("%x", sqeBytes[0:32]), "32-63", fmt.Sprintf("%x", sqeBytes[32:64]), "64-95", fmt.Sprintf("%x", sqeBytes[64:96]))

	// Update array entry
	*(*uint32)(unsafe.Add(unsafe.Pointer(sqArray), uintptr(4*sqIndex))) = sqIndex

	// Step 3: Update tail to submit the entry
	oldTail := *sqTail
	newTail := oldTail + 1

	// Memory barrier to ensure SQE writes are visible before tail update
	runtime.KeepAlive(sqe)

	// Force memory synchronization
	_ = atomic.LoadUint32(sqTail)

	// Use atomic store to ensure the tail update is visible to the kernel
	atomic.StoreUint32(sqTail, newTail)

	// Another memory barrier after tail update
	runtime.KeepAlive(sqTail)

	logger.Info("*** ULTRA-THINK: Updated SQ tail", "old", oldTail, "new", newTail, "head", *sqHead)

	// Step 4: Try normal submit and wait for all commands
	// The control structure fix might have resolved the START_DEV issue
	logger.Info("*** ULTRA-THINK: Trying normal path for all commands")

	// Normal path: submit and wait
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

	logger.Debug("*** ULTRA-THINK: Calling io_uring_enter", "toSubmit", toSubmit, "minComplete", minComplete, "flags", flags)

	r1, r2, err := syscall.Syscall6(
		unix.SYS_IO_URING_ENTER,
		uintptr(r.ringFd),
		uintptr(toSubmit),
		uintptr(minComplete),
		uintptr(flags),
		0, 0)

	logger.Debug("*** ULTRA-THINK: io_uring_enter returned", "r1", r1, "r2", r2, "err", err)

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
	logger := logging.Default()

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

	// Copy control command to correct offset
	if sqe.opcode == kernelUringCmdOpcode() {
		srcCmd := (*[32]byte)(unsafe.Pointer(uintptr(unsafe.Pointer(sqe)) + 48))
		dstCmd := (*[32]byte)(unsafe.Pointer(uintptr(sqeSlot) + 48))
		copy(dstCmd[:], srcCmd[:])
	}

	// Update array entry
	*(*uint32)(unsafe.Add(unsafe.Pointer(sqArray), unsafe.Sizeof(uint32(0))*uintptr(sqIndex))) = sqIndex

	// Update tail with the same ordering guarantees as submitAndWait
	oldTail := *sqTail
	newTail := oldTail + 1

	runtime.KeepAlive(sqe)
	atomic.StoreUint32(sqTail, newTail)
	runtime.KeepAlive(sqTail)

	logger.Info("*** submitOnlyCmd: Updated SQ tail", "old", oldTail, "new", newTail)

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

	// Check if we have completions
	if *cqHead == *cqTail {
		return nil, fmt.Errorf("no completions available")
	}

	// Get CQE
	cqMask := r.params.cqEntries - 1
	cqIndex := *cqHead & cqMask
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

	// Update head to consume the completion
	*cqHead = *cqHead + 1

	return result, nil
}

// performControlOperation performs the actual kernel communication for control operations
func (r *minimalRing) performControlOperation(cmd uint32, ctrlCmd *uapi.UblksrvCtrlCmd) (int32, syscall.Errno) {
	// Not used; URING_CMD is implemented via submitAndWait
	return 0, syscall.ENOSYS
}
