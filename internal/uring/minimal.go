// Package uring provides minimal URING_CMD implementation for ublk control operations
package uring

import (
    "fmt"
    "syscall"
    "unsafe"

    "golang.org/x/sys/unix"
    "github.com/ehrlich-b/go-ublk/internal/logging"
    "github.com/ehrlich-b/go-ublk/internal/uapi"
)

// System call numbers for io_uring
const (
	__NR_io_uring_setup = 425
	__NR_io_uring_enter = 426
)

// Minimal io_uring structures for URING_CMD operations only
// Based on kernel include/uapi/linux/io_uring.h

const (
    IORING_OP_URING_CMD = 50
    
    IORING_SETUP_SQE128 = 1 << 10
    IORING_SETUP_CQE32  = 1 << 11

    // io_uring mmap offsets
    IORING_OFF_SQ_RING = 0
    IORING_OFF_CQ_RING = 0x8000000
    IORING_OFF_SQES    = 0x10000000
)

// Minimal SQE for URING_CMD (128-byte version)
type sqe128 struct {
	opcode      uint8
	flags       uint8
	ioprio      uint16
	fd          int32
	// Union field: off overlaps with cmd_op for URING_CMD
	union0      [8]byte // Contains cmd_op (uint32) at offset 0 for URING_CMD
	addr        uint64
	len         uint32
	opcodeFlags uint32
	userData    uint64
	bufIndex    uint16
	personality uint16
	spliceOff   int32
	addr3       uint64
	_           uint64
	cmd         [80]byte // Command-specific data for URING_CMD
}

// setCmdOp sets the cmd_op field in the union
func (sqe *sqe128) setCmdOp(cmdOp uint32) {
	*(*uint32)(unsafe.Pointer(&sqe.union0[0])) = cmdOp
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

// NewMinimalRing creates a minimal io_uring for ublk control operations
func NewMinimalRing(entries uint32, ctrlFd int32) (Ring, error) {
	logger := logging.Default()
	logger.Debug("creating minimal io_uring", "entries", entries, "ctrl_fd", ctrlFd)
	
	// Set up ring parameters with SQE128/CQE32 for URING_CMD
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
	
    return &minimalRing{
        ringFd:    int(ringFd),
        controlFd: int(ctrlFd),
        params:    params,
        sqAddr:    unsafe.Pointer(&sqAddr[0]),
        cqAddr:    unsafe.Pointer(&cqAddr[0]),
        sqesAddr:  unsafe.Pointer(&sqesAddr[0]),
    }, nil
}

func (r *minimalRing) Close() error {
	// This is a minimal implementation - full cleanup would unmap regions
	return syscall.Close(r.ringFd)
}

func (r *minimalRing) SubmitCtrlCmd(cmd uint32, ctrlCmd *uapi.UblksrvCtrlCmd, userData uint64) (Result, error) {
	logger := logging.Default()
	logger.Info("*** CRITICAL: SubmitCtrlCmd called", "cmd", cmd, "dev_id", ctrlCmd.DevID)
	logger.Debug("preparing URING_CMD", "cmd", cmd, "dev_id", ctrlCmd.DevID)

	// Log the actual command being used
	logger.Debug("using command", "cmd", cmd)
	
    // Create URING_CMD SQE for control operations
    sqe := &sqe128{
        opcode:      IORING_OP_URING_CMD,
        flags:       0,
        ioprio:      0,
        fd:          int32(r.controlFd), // Control device fd
        addr:        ctrlCmd.Addr,       // buffer address from control command
        len:         uint32(ctrlCmd.Len), // buffer length from control command
        opcodeFlags: 0,
        userData:    userData,
        bufIndex:    0,
		personality: 0,
		spliceOff:   0,
		addr3:       0,
	}

	// Set cmd_op field properly for URING_CMD
	sqe.setCmdOp(cmd)

    // Marshal the control command properly to ensure correct layout
    ctrlCmdBytes := uapi.Marshal(ctrlCmd)
    if len(ctrlCmdBytes) != 32 {
        return nil, fmt.Errorf("control command marshal returned %d bytes, expected 32", len(ctrlCmdBytes))
    }

    // Copy the marshaled control command (32 bytes) into the SQE128 cmd area
    cmdBytes := (*[80]byte)(unsafe.Pointer(&sqe.cmd[0]))
    copy(cmdBytes[:32], ctrlCmdBytes)

    // Debug: log the exact control header bytes being sent to kernel
    logger.Debug("control header bytes", "header_hex", fmt.Sprintf("%x", ctrlCmdBytes))
	
	logger.Debug("SQE prepared", "fd", sqe.fd, "cmd", cmd, "addr", sqe.addr)

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
    sqe := &sqe128{
        opcode:      IORING_OP_URING_CMD,
        flags:       0,
        ioprio:      0,
        fd:          int32(r.controlFd),
        addr:        0,
        len:         0,
        opcodeFlags: 0,
        userData:    userData,
        bufIndex:    0,
        personality: 0,
        spliceOff:   0,
        addr3:       0,
    }

    // Set cmd_op field properly for URING_CMD
    sqe.setCmdOp(cmd)

    // Encode io_cmd in SQE cmd area
    cmdBytes := (*[80]byte)(unsafe.Pointer(&sqe.cmd[0]))
    ioCmdBytes := (*[16]byte)(unsafe.Pointer(ioCmd))
    copy(cmdBytes[:16], ioCmdBytes[:])

    result, err := r.submitAndWait(sqe)
    if err != nil {
        return nil, fmt.Errorf("failed to submit I/O command: %v", err)
    }
    if result.Value() < 0 {
        return result, fmt.Errorf("I/O command failed: %d", result.Value())
    }
    return result, nil
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

    // Block for at least one completion
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
	logger.Info("*** CRITICAL: submitAndWait called - making syscalls", "fd", sqe.fd, "opcode", sqe.opcode, "user_data", sqe.userData)
	logger.Debug("submitting URING_CMD via io_uring", "fd", sqe.fd, "opcode", sqe.opcode, "user_data", sqe.userData)
	
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
    sqeSlot := unsafe.Add(r.sqesAddr, uintptr(unsafe.Sizeof(*sqe))*uintptr(sqIndex))

    // Copy our SQE to the SQEs array
    *(*sqe128)(sqeSlot) = *sqe
	
	// Update array entry
	*(*uint32)(unsafe.Add(unsafe.Pointer(sqArray), uintptr(4*sqIndex))) = sqIndex
	
	// Step 3: Update tail to submit the entry
	*sqTail = *sqTail + 1
	
	// Step 4: Call io_uring_enter to submit and wait for completion
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
	const (
		IORING_ENTER_GETEVENTS = 1 << 0
	)
	
	flags := uint32(IORING_ENTER_GETEVENTS)
	
	r1, r2, err := syscall.Syscall6(
		unix.SYS_IO_URING_ENTER,
		uintptr(r.ringFd),
		uintptr(toSubmit),
		uintptr(minComplete),
		uintptr(flags),
		0, 0)
	
	return uint32(r1), uint32(r2), err
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
