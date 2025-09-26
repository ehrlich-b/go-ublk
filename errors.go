package ublk

import (
	"errors"
	"fmt"
	"syscall"
)

// Error represents a structured ublk error with context and errno mapping
type Error struct {
	Op     string        // Operation that failed (e.g., "CREATE_DEV", "START_DEV")
	DevID  uint32        // Device ID (0 if not applicable)
	Queue  int           // Queue number (-1 if not applicable)
	Code   UblkErrorCode // High-level error category
	Errno  syscall.Errno // Kernel errno (0 if not applicable)
	Msg    string        // Human-readable message
	Inner  error         // Wrapped error
}

// Error implements the error interface
func (e *Error) Error() string {
	var parts []string

	if e.Op != "" {
		parts = append(parts, fmt.Sprintf("op=%s", e.Op))
	}

	if e.DevID != 0 {
		parts = append(parts, fmt.Sprintf("dev=%d", e.DevID))
	}

	if e.Queue >= 0 {
		parts = append(parts, fmt.Sprintf("queue=%d", e.Queue))
	}

	if e.Errno != 0 {
		parts = append(parts, fmt.Sprintf("errno=%d", e.Errno))
	}

	msg := e.Msg
	if msg == "" {
		msg = string(e.Code)
	}

	if len(parts) > 0 {
		return fmt.Sprintf("ublk: %s (%s)", msg, fmt.Sprintf("%s", parts[0]))
	}

	return fmt.Sprintf("ublk: %s", msg)
}

// Unwrap returns the wrapped error for errors.Is/As support
func (e *Error) Unwrap() error {
	return e.Inner
}

// Is provides errors.Is support for UblkError compatibility
func (e *Error) Is(target error) bool {
	if target == nil {
		return false
	}

	// Support legacy UblkError comparison
	if ue, ok := target.(UblkError); ok {
		return e.Code == UblkErrorCode(ue)
	}

	// Support structured Error comparison
	if te, ok := target.(*Error); ok {
		return e.Code == te.Code
	}

	return false
}

// UblkErrorCode represents high-level error categories
type UblkErrorCode string

const (
	ErrCodeNotImplemented     UblkErrorCode = "not implemented"
	ErrCodeDeviceNotFound     UblkErrorCode = "device not found"
	ErrCodeDeviceBusy         UblkErrorCode = "device busy"
	ErrCodeInvalidParameters  UblkErrorCode = "invalid parameters"
	ErrCodeKernelNotSupported UblkErrorCode = "kernel does not support ublk"
	ErrCodePermissionDenied   UblkErrorCode = "permission denied"
	ErrCodeInsufficientMemory UblkErrorCode = "insufficient memory"
	ErrCodeIOError            UblkErrorCode = "I/O error"
	ErrCodeTimeout            UblkErrorCode = "timeout"
	ErrCodeDeviceOffline      UblkErrorCode = "device offline"
)

// Legacy UblkError type for backward compatibility
type UblkError string

func (e UblkError) Error() string {
	return string(e)
}

// Legacy error constants (maintain backward compatibility)
const (
	ErrNotImplemented     UblkError = "not implemented"
	ErrDeviceNotFound     UblkError = "device not found"
	ErrDeviceBusy         UblkError = "device busy"
	ErrInvalidParameters  UblkError = "invalid parameters"
	ErrKernelNotSupported UblkError = "kernel does not support ublk"
	ErrPermissionDenied   UblkError = "permission denied"
	ErrInsufficientMemory UblkError = "insufficient memory"
)

// Error constructors

// NewError creates a new structured error
func NewError(op string, code UblkErrorCode, msg string) *Error {
	return &Error{
		Op:   op,
		Code: code,
		Msg:  msg,
	}
}

// NewErrorWithErrno creates a new structured error with errno
func NewErrorWithErrno(op string, code UblkErrorCode, errno syscall.Errno) *Error {
	return &Error{
		Op:    op,
		Code:  code,
		Errno: errno,
		Msg:   errno.Error(),
	}
}

// NewDeviceError creates a new device-specific error
func NewDeviceError(op string, devID uint32, code UblkErrorCode, msg string) *Error {
	return &Error{
		Op:    op,
		DevID: devID,
		Code:  code,
		Msg:   msg,
	}
}

// NewQueueError creates a new queue-specific error
func NewQueueError(op string, devID uint32, queue int, code UblkErrorCode, msg string) *Error {
	return &Error{
		Op:    op,
		DevID: devID,
		Queue: queue,
		Code:  code,
		Msg:   msg,
	}
}

// WrapError wraps an existing error with ublk context
func WrapError(op string, inner error) *Error {
	if inner == nil {
		return nil
	}

	// If it's already a structured error, just update the operation
	if ue, ok := inner.(*Error); ok {
		return &Error{
			Op:    op,
			DevID: ue.DevID,
			Queue: ue.Queue,
			Code:  ue.Code,
			Errno: ue.Errno,
			Msg:   ue.Msg,
			Inner: ue.Inner,
		}
	}

	// Map common syscall errors to ublk error codes
	code := ErrCodeIOError
	if errno, ok := inner.(syscall.Errno); ok {
		code = mapErrnoToCode(errno)
		return &Error{
			Op:    op,
			Code:  code,
			Errno: errno,
			Msg:   errno.Error(),
			Inner: inner,
		}
	}

	return &Error{
		Op:    op,
		Code:  code,
		Msg:   inner.Error(),
		Inner: inner,
	}
}

// mapErrnoToCode maps syscall errno to ublk error codes
func mapErrnoToCode(errno syscall.Errno) UblkErrorCode {
	switch errno {
	case syscall.ENOENT:
		return ErrCodeDeviceNotFound
	case syscall.EBUSY:
		return ErrCodeDeviceBusy
	case syscall.EINVAL, syscall.E2BIG:
		return ErrCodeInvalidParameters
	case syscall.ENOSYS, syscall.EOPNOTSUPP:
		return ErrCodeKernelNotSupported
	case syscall.EPERM, syscall.EACCES:
		return ErrCodePermissionDenied
	case syscall.ENOMEM, syscall.ENOSPC:
		return ErrCodeInsufficientMemory
	case syscall.ETIMEDOUT:
		return ErrCodeTimeout
	default:
		return ErrCodeIOError
	}
}

// IsCode checks if an error matches a specific error code
func IsCode(err error, code UblkErrorCode) bool {
	var ublkErr *Error
	if errors.As(err, &ublkErr) {
		return ublkErr.Code == code
	}
	return false
}

// IsErrno checks if an error matches a specific errno
func IsErrno(err error, errno syscall.Errno) bool {
	var ublkErr *Error
	if errors.As(err, &ublkErr) {
		return ublkErr.Errno == errno
	}
	return false
}