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

	expected := "ublk: invalid queue depth (op=CREATE_DEV, queue=0)"
	if err.Error() != expected {
		t.Errorf("Expected error message %q, got %q", expected, err.Error())
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

func TestSentinelErrors(t *testing.T) {
	// Sentinel errors work with errors.Is
	var sentinelErr error = ErrDeviceNotFound

	// Structured error should match sentinel by code
	structuredErr := &Error{Code: ErrCodeDeviceNotFound}

	if !errors.Is(structuredErr, ErrDeviceNotFound) {
		t.Error("Structured error should match sentinel via errors.Is")
	}

	// Sentinel error message
	if sentinelErr.Error() != "ublk: device not found" {
		t.Errorf("Expected sentinel error message, got %q", sentinelErr.Error())
	}

	// Wrapped errors should match sentinel
	wrappedErr := WrapError("TEST_OP", syscall.ENOENT)
	if !errors.Is(wrappedErr, ErrDeviceNotFound) {
		t.Error("Wrapped ENOENT should match ErrDeviceNotFound")
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
	// Create error with errno via WrapError
	err := WrapError("TEST", syscall.EIO)

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
