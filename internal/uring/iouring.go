//go:build giouring
// +build giouring

// Package uring implements real io_uring operations using iceber/iouring-go
package uring

import (
	"fmt"

	"github.com/ehrlich-b/go-ublk/internal/uapi"
	"github.com/iceber/iouring-go"
	iouring_syscall "github.com/iceber/iouring-go/syscall"
)

// iouRing implements the Ring interface using iceber/iouring-go
type iouRing struct {
	ring   *iouring.IOURing
	config Config
}

// iouResult wraps iouring results
type iouResult struct {
	userData uint64
	value    int32
	err      error
}

func (r *iouResult) UserData() uint64 { return r.userData }
func (r *iouResult) Value() int32     { return r.value }
func (r *iouResult) Error() error     { return r.err }

// NewRealRing creates a real io_uring implementation with SQE128/CQE32 support
func NewRealRing(config Config) (Ring, error) {
	// Create io_uring with SQE128/CQE32 support for ublk URING_CMD operations
	ring, err := iouring.New(uint(config.Entries), iouring.WithSQE128(), iouring.WithCQE32())
	if err != nil {
		return nil, fmt.Errorf("failed to create io_uring: %v", err)
	}

	return &iouRing{
		ring:   ring,
		config: config,
	}, nil
}

func (r *iouRing) Close() error {
	if r.ring != nil {
		r.ring.Close()
	}
	return nil
}

// prepUblkCtrlCmd creates a PrepRequest for ublk control operations
func (r *iouRing) prepUblkCtrlCmd(cmd uint32, ctrlCmd *uapi.UblksrvCtrlCmd, userData uint64) iouring.PrepRequest {
	return func(sqe iouring_syscall.SubmissionQueueEntry, udata *iouring.UserData) {
		// Set up URING_CMD operation for ublk control device
		sqe.PrepOperation(
			iouring_syscall.IORING_OP_URING_CMD,
			int32(r.config.FD), // ublk control device fd
			0,                  // offset (unused for URING_CMD)
			0,                  // len (unused for URING_CMD)
			uint64(cmd),        // cmd in off field
		)

		// Set user data
		sqe.SetUserData(userData)

		// Use the command area in SQE128 for the ublk command
		// Copy the command structure to the SQE command area
		cmdPtr := sqe.CMD(*ctrlCmd)
		*cmdPtr.(*uapi.UblksrvCtrlCmd) = *ctrlCmd
	}
}

// prepUblkIOCmd creates a PrepRequest for ublk I/O operations
func (r *iouRing) prepUblkIOCmd(cmd uint32, ioCmd *uapi.UblksrvIOCmd, userData uint64) iouring.PrepRequest {
	return func(sqe iouring_syscall.SubmissionQueueEntry, udata *iouring.UserData) {
		// Set up URING_CMD operation for ublk queue device
		sqe.PrepOperation(
			iouring_syscall.IORING_OP_URING_CMD,
			int32(r.config.FD), // ublk queue device fd (/dev/ublkcN)
			0,                  // offset (unused for URING_CMD)
			0,                  // len (unused for URING_CMD)
			uint64(cmd),        // cmd in off field
		)

		// Set user data
		sqe.SetUserData(userData)

		// Copy the I/O command structure to the SQE command area
		cmdPtr := sqe.CMD(*ioCmd)
		*cmdPtr.(*uapi.UblksrvIOCmd) = *ioCmd
	}
}

func (r *iouRing) SubmitCtrlCmd(cmd uint32, ctrlCmd *uapi.UblksrvCtrlCmd, userData uint64) (Result, error) {
	ch := make(chan iouring.Result)

	// Create and submit the request
	prepReq := r.prepUblkCtrlCmd(cmd, ctrlCmd, userData)
	_, err := r.ring.SubmitRequest(prepReq, ch)
	if err != nil {
		return nil, fmt.Errorf("submit control command failed: %v", err)
	}

	// Wait for completion
	result := <-ch

	// Extract return value as int32
	retVal, retErr := result.ReturnInt()
	if retErr != nil {
		return nil, fmt.Errorf("failed to get return value: %v", retErr)
	}

	return &iouResult{
		userData: userData, // We know the userData we sent
		value:    int32(retVal),
		err:      result.Err(),
	}, nil
}

func (r *iouRing) SubmitIOCmd(cmd uint32, ioCmd *uapi.UblksrvIOCmd, userData uint64) (Result, error) {
	ch := make(chan iouring.Result)

	// Create and submit the request
	prepReq := r.prepUblkIOCmd(cmd, ioCmd, userData)
	_, err := r.ring.SubmitRequest(prepReq, ch)
	if err != nil {
		return nil, fmt.Errorf("submit I/O command failed: %v", err)
	}

	// Wait for completion
	result := <-ch

	// Extract return value as int32
	retVal, retErr := result.ReturnInt()
	if retErr != nil {
		return nil, fmt.Errorf("failed to get return value: %v", retErr)
	}

	return &iouResult{
		userData: userData, // We know the userData we sent
		value:    int32(retVal),
		err:      result.Err(),
	}, nil
}

func (r *iouRing) WaitForCompletion(timeout int) ([]Result, error) {
	// This is for data plane operations - typically used for I/O completion polling
	// For control operations, we use synchronous SubmitCtrlCmd
	// For now, implement a basic polling mechanism

	// TODO: Implement proper asynchronous completion polling for data plane
	// This would involve submitting multiple requests and waiting for their completions

	return []Result{}, nil
}

func (r *iouRing) NewBatch() Batch {
	return &iouBatch{
		ring:   r.ring,
		config: r.config,
	}
}

// iouBatch implements batched operations
type iouBatch struct {
	ring     *iouring.IOURing
	config   Config
	requests []iouring.PrepRequest
}

func (b *iouBatch) AddCtrlCmd(cmd uint32, ctrlCmd *uapi.UblksrvCtrlCmd, userData uint64) error {
	prepReq := func(sqe iouring_syscall.SubmissionQueueEntry, udata *iouring.UserData) {
		sqe.PrepOperation(
			iouring_syscall.IORING_OP_URING_CMD,
			int32(b.config.FD), // ublk control device fd
			0, 0, uint64(cmd),
		)
		sqe.SetUserData(userData)

		cmdPtr := sqe.CMD(*ctrlCmd)
		*cmdPtr.(*uapi.UblksrvCtrlCmd) = *ctrlCmd
	}

	b.requests = append(b.requests, prepReq)
	return nil
}

func (b *iouBatch) AddIOCmd(cmd uint32, ioCmd *uapi.UblksrvIOCmd, userData uint64) error {
	prepReq := func(sqe iouring_syscall.SubmissionQueueEntry, udata *iouring.UserData) {
		sqe.PrepOperation(
			iouring_syscall.IORING_OP_URING_CMD,
			int32(b.config.FD), // ublk queue device fd
			0, 0, uint64(cmd),
		)
		sqe.SetUserData(userData)

		cmdPtr := sqe.CMD(*ioCmd)
		*cmdPtr.(*uapi.UblksrvIOCmd) = *ioCmd
	}

	b.requests = append(b.requests, prepReq)
	return nil
}

func (b *iouBatch) Submit() ([]Result, error) {
	if len(b.requests) == 0 {
		return nil, nil
	}

	ch := make(chan iouring.Result)

	// Submit all requests
	_, err := b.ring.SubmitRequests(b.requests, ch)
	if err != nil {
		return nil, fmt.Errorf("batch submit failed: %v", err)
	}

	// Collect results
	results := make([]Result, len(b.requests))
	for i := 0; i < len(b.requests); i++ {
		result := <-ch

		// Extract return value as int32
		retVal, retErr := result.ReturnInt()
		if retErr != nil {
			return nil, fmt.Errorf("failed to get return value for batch item %d: %v", i, retErr)
		}

		results[i] = &iouResult{
			userData: uint64(i), // Use index as userData for batches
			value:    int32(retVal),
			err:      result.Err(),
		}
	}

	// Clear batch
	b.requests = b.requests[:0]

	return results, nil
}

func (b *iouBatch) Len() int {
	return len(b.requests)
}
