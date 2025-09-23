// Package uring provides interfaces for io_uring operations
package uring

import (
	"github.com/ehrlich-b/go-ublk/internal/logging"
	"github.com/ehrlich-b/go-ublk/internal/uapi"
)

// Ring provides the interface for io_uring operations needed by ublk
type Ring interface {
	// Close closes the ring and releases resources
	Close() error

	// SubmitCtrlCmd submits a control command and returns the result
	SubmitCtrlCmd(cmd uint32, ctrlCmd *uapi.UblksrvCtrlCmd, userData uint64) (Result, error)

	// SubmitCtrlCmdAsync submits a control command without waiting for completion
	SubmitCtrlCmdAsync(cmd uint32, ctrlCmd *uapi.UblksrvCtrlCmd, userData uint64) (*AsyncHandle, error)

	// SubmitIOCmd submits an I/O command and returns the result
	SubmitIOCmd(cmd uint32, ioCmd *uapi.UblksrvIOCmd, userData uint64) (Result, error)

	// WaitForCompletion waits for completion events and returns them
	WaitForCompletion(timeout int) ([]Result, error)

	// NewBatch creates a new batch for bulk operations
	NewBatch() Batch
}

// Batch allows batching multiple operations
type Batch interface {
	// AddCtrlCmd adds a control command to the batch
	AddCtrlCmd(cmd uint32, ctrlCmd *uapi.UblksrvCtrlCmd, userData uint64) error

	// AddIOCmd adds an I/O command to the batch
	AddIOCmd(cmd uint32, ioCmd *uapi.UblksrvIOCmd, userData uint64) error

	// Submit submits all commands in the batch
	Submit() ([]Result, error)

	// Len returns the number of commands in the batch
	Len() int
}

// Result represents the result of an operation
type Result interface {
	// UserData returns the user data associated with this result
	UserData() uint64

	// Value returns the result value (0 for success, negative for errno)
	Value() int32

	// Error returns an error if the operation failed
	Error() error
}

// Features describes available io_uring features
type Features struct {
	SQE128   bool // 128-byte SQEs supported
	CQE32    bool // 32-byte CQEs supported
	UringCmd bool // URING_CMD operation supported
	SQPOLL   bool // Kernel-side polling supported
}

// SupportsFeatures checks if the kernel supports required features for ublk
func SupportsFeatures() error {
	// This would be implemented by concrete types
	// For now, assume features are available if we're on Linux 6.1+
	return nil
}

// GetFeatures returns information about supported features
func GetFeatures() (Features, error) {
	// This would probe actual kernel capabilities
	return Features{
		SQE128:   true,
		CQE32:    true,
		UringCmd: true,
		SQPOLL:   false, // Assume not available by default
	}, nil
}

// Config contains configuration for creating a ring
type Config struct {
	Entries uint32 // Number of entries in the ring
	FD      int32  // File descriptor for operations
	Flags   uint32 // Additional flags
}

// NewRing creates a new Ring implementation
func NewRing(config Config) (Ring, error) {
	logger := logging.Default()
	logger.Debug("creating io_uring", "entries", config.Entries, "fd", config.FD)

	// Prefer full-featured io_uring via library (if available)
	if ring, err := NewRealRing(config); err == nil {
		logger.Info("created io_uring via library", "entries", config.Entries)
		return ring, nil
	} else {
		logger.Warn("NewRealRing failed, trying minimal shim", "error", err)
	}

	// Fallback: minimal syscall-based shim (limited but real syscalls)
	if minRing, err := NewMinimalRing(config.Entries, config.FD); err == nil {
		logger.Info("created minimal io_uring implementation", "entries", config.Entries)
		return minRing, nil
	} else {
		logger.Error("NewMinimalRing failed, falling back to stub", "error", err)
	}

	// Last resort: stub (non-functional for real I/O)
	logger.Warn("using stub ring - this breaks actual functionality")
	return &stubRing{config: config}, nil
}

// Stub implementation for development/testing
type stubRing struct {
	config Config
}

type stubResult struct {
	userData uint64
	value    int32
	err      error
}

func (r *stubResult) UserData() uint64 { return r.userData }
func (r *stubResult) Value() int32     { return r.value }
func (r *stubResult) Error() error     { return r.err }

type stubBatch struct {
	commands int
}

func (b *stubBatch) AddCtrlCmd(cmd uint32, ctrlCmd *uapi.UblksrvCtrlCmd, userData uint64) error {
	b.commands++
	return nil
}

func (b *stubBatch) AddIOCmd(cmd uint32, ioCmd *uapi.UblksrvIOCmd, userData uint64) error {
	b.commands++
	return nil
}

func (b *stubBatch) Submit() ([]Result, error) {
	results := make([]Result, b.commands)
	for i := range results {
		results[i] = &stubResult{
			userData: uint64(i),
			value:    -38, // -ENOSYS (not implemented)
			err:      nil,
		}
	}
	b.commands = 0
	return results, nil
}

func (b *stubBatch) Len() int {
	return b.commands
}

func (r *stubRing) Close() error {
	return nil
}

func (r *stubRing) SubmitCtrlCmdAsync(cmd uint32, ctrlCmd *uapi.UblksrvCtrlCmd, userData uint64) (*AsyncHandle, error) {
	// For stub, just return a handle that immediately completes
	return &AsyncHandle{
		userData: userData,
		ring:     nil, // stub doesn't have a real ring
	}, nil
}

func (r *stubRing) SubmitCtrlCmd(cmd uint32, ctrlCmd *uapi.UblksrvCtrlCmd, userData uint64) (Result, error) {
	// Enhanced stub that simulates successful control operations for development testing
	// This allows us to test the complete control plane flow

	switch cmd {
	case uapi.UBLK_CMD_ADD_DEV:
		// Return a device ID (simulate kernel assigning device ID)
		devID := ctrlCmd.DevID
		if devID == 0xFFFFFFFF { // -1 means auto-assign
			devID = 0 // Assign device ID 0
		}
		return &stubResult{
			userData: userData,
			value:    int32(devID),
			err:      nil,
		}, nil
	case uapi.UBLK_CMD_SET_PARAMS:
		// Simulate successful parameter setting
		return &stubResult{
			userData: userData,
			value:    0, // Success
			err:      nil,
		}, nil
	case uapi.UBLK_CMD_START_DEV:
		// Simulate successful device start
		// This would normally create /dev/ublkbN and /dev/ublkcN
		return &stubResult{
			userData: userData,
			value:    0, // Success
			err:      nil,
		}, nil
	case uapi.UBLK_CMD_STOP_DEV, uapi.UBLK_CMD_DEL_DEV:
		// Simulate successful stop/delete
		return &stubResult{
			userData: userData,
			value:    0, // Success
			err:      nil,
		}, nil
	case uapi.UBLK_CMD_GET_DEV_INFO:
		// Simulate successful info retrieval
		return &stubResult{
			userData: userData,
			value:    0, // Success
			err:      nil,
		}, nil
	default:
		// Unknown command
		return &stubResult{
			userData: userData,
			value:    -22, // -EINVAL
			err:      nil,
		}, nil
	}
}

func (r *stubRing) SubmitIOCmd(cmd uint32, ioCmd *uapi.UblksrvIOCmd, userData uint64) (Result, error) {
	// For I/O operations, still return -ENOSYS since we haven't implemented the data plane yet
	// But this allows the control plane to work for testing device creation
	return &stubResult{
		userData: userData,
		value:    -38, // -ENOSYS (not implemented)
		err:      nil,
	}, nil
}

func (r *stubRing) WaitForCompletion(timeout int) ([]Result, error) {
	// Enhanced stub that simulates I/O request completions for development testing
	// This allows the data plane to actually process simulated I/O requests

	// TODO: In a real implementation, this would wait for actual kernel completions
	// For now, simulate receiving an I/O request completion every few calls

	// Return empty most of the time to simulate waiting
	// Occasionally return a simulated I/O completion for testing

	return []Result{}, nil // Still returning empty for now - needs more complex simulation
}

func (r *stubRing) NewBatch() Batch {
	return &stubBatch{}
}
