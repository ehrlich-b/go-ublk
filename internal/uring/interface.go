// Package uring provides interfaces for io_uring operations
package uring

import (
	"errors"

	"github.com/ehrlich-b/go-ublk/internal/logging"
	"github.com/ehrlich-b/go-ublk/internal/uapi"
)

// ErrRingFull is returned when the submission queue is full.
// In normal ublk operation this should never happen - the state machine
// guarantees at most depth in-flight operations.
var ErrRingFull = errors.New("submission queue full")

// Ring provides the interface for io_uring operations needed by ublk
type Ring interface {
	// Close closes the ring and releases resources
	Close() error

	// SubmitCtrlCmd submits a control command and returns the result
	SubmitCtrlCmd(cmd uint32, ctrlCmd *uapi.UblksrvCtrlCmd, userData uint64) (Result, error)

	// SubmitCtrlCmdAsync submits a control command without waiting for completion
	SubmitCtrlCmdAsync(cmd uint32, ctrlCmd *uapi.UblksrvCtrlCmd, userData uint64) (*AsyncHandle, error)

	// SubmitIOCmd submits an I/O command and returns the result.
	// This is a convenience method that calls PrepareIOCmd + FlushSubmissions.
	SubmitIOCmd(cmd uint32, ioCmd *uapi.UblksrvIOCmd, userData uint64) (Result, error)

	// PrepareIOCmd prepares an I/O command SQE without submitting to the kernel.
	// The SQE is written to ring memory but not visible to the kernel until
	// FlushSubmissions is called. This enables batching multiple I/O commands
	// into a single io_uring_enter syscall.
	// Returns ErrRingFull if the submission queue is full.
	PrepareIOCmd(cmd uint32, ioCmd *uapi.UblksrvIOCmd, userData uint64) error

	// FlushSubmissions submits all prepared SQEs with a single io_uring_enter syscall.
	// Returns the number of SQEs submitted.
	FlushSubmissions() (uint32, error)

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

// NewRing creates a new Ring implementation using pure Go io_uring
func NewRing(config Config) (Ring, error) {
	logger := logging.Default()
	logger.Debug("creating io_uring", "entries", config.Entries, "fd", config.FD)

	ring, err := NewMinimalRing(config.Entries, config.FD)
	if err != nil {
		logger.Error("failed to create io_uring", "error", err)
		return nil, err
	}

	logger.Info("created io_uring", "entries", config.Entries)
	return ring, nil
}
