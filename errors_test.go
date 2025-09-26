package ublk

import (
	"errors"
	"syscall"
	"testing"
)

func TestStructuredError(t *testing.T) {
	// Test basic error creation
	err := NewError("CREATE_DEV", ErrCodeInvalidParameters, "invalid queue depth")

	if err.Op != "CREATE_DEV" {
		t.Errorf("Expected Op=CREATE_DEV, got %s", err.Op)
	}

	if err.Code != ErrCodeInvalidParameters {
		t.Errorf("Expected Code=ErrCodeInvalidParameters, got %s", err.Code)
	}

	expected := "ublk: invalid queue depth (op=CREATE_DEV)"
	if err.Error() != expected {
		t.Errorf("Expected error message %q, got %q", expected, err.Error())
	}
}

func TestErrorWithErrno(t *testing.T) {
	err := NewErrorWithErrno("START_DEV", ErrCodePermissionDenied, syscall.EPERM)

	if err.Errno != syscall.EPERM {
		t.Errorf("Expected Errno=EPERM, got %v", err.Errno)
	}

	if err.Code != ErrCodePermissionDenied {
		t.Errorf("Expected Code=ErrCodePermissionDenied, got %s", err.Code)
	}
}

func TestDeviceError(t *testing.T) {
	err := NewDeviceError("SET_PARAMS", 123, ErrCodeDeviceBusy, "device in use")

	if err.DevID != 123 {
		t.Errorf("Expected DevID=123, got %d", err.DevID)
	}

	expected := "ublk: device in use (op=SET_PARAMS)"
	if err.Error() != expected {
		t.Errorf("Expected error message %q, got %q", expected, err.Error())
	}
}

func TestQueueError(t *testing.T) {
	err := NewQueueError("FETCH_REQ", 42, 1, ErrCodeIOError, "queue stalled")

	if err.DevID != 42 {
		t.Errorf("Expected DevID=42, got %d", err.DevID)
	}

	if err.Queue != 1 {
		t.Errorf("Expected Queue=1, got %d", err.Queue)
	}
}

func TestWrapError(t *testing.T) {
	inner := syscall.ENOENT
	err := WrapError("DELETE_DEV", inner)

	if err.Code != ErrCodeDeviceNotFound {
		t.Errorf("Expected Code=ErrCodeDeviceNotFound, got %s", err.Code)
	}

	if err.Errno != syscall.ENOENT {
		t.Errorf("Expected Errno=ENOENT, got %v", err.Errno)
	}

	if !errors.Is(err, syscall.ENOENT) {
		t.Error("Expected wrapped error to satisfy errors.Is for ENOENT")
	}
}

func TestBackwardCompatibility(t *testing.T) {
	// Legacy UblkError should still work
	var legacyErr error = ErrDeviceNotFound

	// New structured error should be comparable with legacy error
	structuredErr := &Error{Code: ErrCodeDeviceNotFound}

	if !errors.Is(structuredErr, ErrDeviceNotFound) {
		t.Error("Structured error should be compatible with legacy UblkError")
	}

	// Test that legacy error still implements error interface
	if legacyErr.Error() != "device not found" {
		t.Errorf("Expected legacy error message, got %q", legacyErr.Error())
	}
}

func TestIsCode(t *testing.T) {
	err := NewError("TEST", ErrCodeTimeout, "operation timed out")

	if !IsCode(err, ErrCodeTimeout) {
		t.Error("IsCode should return true for matching code")
	}

	if IsCode(err, ErrCodeIOError) {
		t.Error("IsCode should return false for non-matching code")
	}

	// Test with nil error
	if IsCode(nil, ErrCodeTimeout) {
		t.Error("IsCode should return false for nil error")
	}
}

func TestIsErrno(t *testing.T) {
	err := NewErrorWithErrno("TEST", ErrCodeIOError, syscall.EIO)

	if !IsErrno(err, syscall.EIO) {
		t.Error("IsErrno should return true for matching errno")
	}

	if IsErrno(err, syscall.EPERM) {
		t.Error("IsErrno should return false for non-matching errno")
	}

	// Test with nil error
	if IsErrno(nil, syscall.EIO) {
		t.Error("IsErrno should return false for nil error")
	}
}

func TestErrnoMapping(t *testing.T) {
	testCases := []struct {
		errno    syscall.Errno
		expected UblkErrorCode
	}{
		{syscall.ENOENT, ErrCodeDeviceNotFound},
		{syscall.EBUSY, ErrCodeDeviceBusy},
		{syscall.EINVAL, ErrCodeInvalidParameters},
		{syscall.EPERM, ErrCodePermissionDenied},
		{syscall.ENOMEM, ErrCodeInsufficientMemory},
		{syscall.ETIMEDOUT, ErrCodeTimeout},
		{syscall.ENOSYS, ErrCodeKernelNotSupported},
	}

	for _, tc := range testCases {
		code := mapErrnoToCode(tc.errno)
		if code != tc.expected {
			t.Errorf("mapErrnoToCode(%v) = %s, want %s", tc.errno, code, tc.expected)
		}
	}
}