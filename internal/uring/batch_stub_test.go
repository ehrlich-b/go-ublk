package uring

import (
	"strings"
	"testing"

	"github.com/ehrlich-b/go-ublk/internal/uapi"
)

const batchNotImplementedErr = "batch not implemented in minimal ring"

func assertBatchNotImplemented(t *testing.T, err error) {
	t.Helper()
	if err == nil {
		t.Fatal("expected a non-nil error, got nil")
	}
	if !strings.Contains(err.Error(), batchNotImplementedErr) {
		t.Fatalf("error %q does not name batching as unimplemented (want substring %q)", err.Error(), batchNotImplementedErr)
	}
}

// TestBatchStubNewBatchNonNil checks that (&minimalRing{}).NewBatch() returns a
// non-nil Batch. A nil concrete pointer boxed in a non-nil interface is a
// classic Go footgun, so assert at runtime even though the compile-time return
// type is already Batch.
func TestBatchStubNewBatchNonNil(t *testing.T) {
	b := (&minimalRing{}).NewBatch()
	if b == nil {
		t.Fatal("NewBatch() returned nil")
	}
	var _ Batch = b
}

// TestBatchStubLenAlwaysZero checks that Len() is 0 immediately after
// construction and stays 0 after failed adds. A half-fix that increments the
// count before erroring would be caught here.
func TestBatchStubLenAlwaysZero(t *testing.T) {
	b := &minimalBatch{}
	if got := b.Len(); got != 0 {
		t.Fatalf("Len() immediately after construction = %d, want 0", got)
	}

	b.AddCtrlCmd(0, &uapi.UblksrvCtrlCmd{}, 0)
	b.AddIOCmd(0, &uapi.UblksrvIOCmd{}, 0)

	if got := b.Len(); got != 0 {
		t.Fatalf("Len() after failed adds = %d, want 0 (failed add must not increment)", got)
	}
}

// TestBatchStubAddCtrlCmdAlwaysErrors checks that AddCtrlCmd errors regardless
// of its arguments.
func TestBatchStubAddCtrlCmdAlwaysErrors(t *testing.T) {
	for _, tc := range []struct {
		name     string
		cmd      uint32
		ctrlCmd  *uapi.UblksrvCtrlCmd
		userData uint64
	}{
		{"zero-value cmd and zero-value ctrlCmd", 0, &uapi.UblksrvCtrlCmd{}, 0},
		{"fully-populated ctrlCmd", 0x01020304, &uapi.UblksrvCtrlCmd{}, 0},
		{"cmd 0xFFFFFFFF", 0xFFFFFFFF, &uapi.UblksrvCtrlCmd{}, 0},
		{"large userData", 0, &uapi.UblksrvCtrlCmd{}, 0xFFFFFFFFFFFFFFFF},
	} {
		t.Run(tc.name, func(t *testing.T) {
			b := &minimalBatch{}
			assertBatchNotImplemented(t, b.AddCtrlCmd(tc.cmd, tc.ctrlCmd, tc.userData))
		})
	}
}

// TestBatchStubAddIOCmdAlwaysErrors checks that AddIOCmd errors regardless of
// its arguments.
func TestBatchStubAddIOCmdAlwaysErrors(t *testing.T) {
	for _, tc := range []struct {
		name     string
		cmd      uint32
		ioCmd    *uapi.UblksrvIOCmd
		userData uint64
	}{
		{"zero-value cmd and zero-value ioCmd", 0, &uapi.UblksrvIOCmd{}, 0},
		{"fully-populated ioCmd", 0x01020304, &uapi.UblksrvIOCmd{}, 0},
		{"cmd 0xFFFFFFFF", 0xFFFFFFFF, &uapi.UblksrvIOCmd{}, 0},
		{"large userData", 0, &uapi.UblksrvIOCmd{}, 0xFFFFFFFFFFFFFFFF},
	} {
		t.Run(tc.name, func(t *testing.T) {
			b := &minimalBatch{}
			assertBatchNotImplemented(t, b.AddIOCmd(tc.cmd, tc.ioCmd, tc.userData))
		})
	}
}

// TestBatchStubSubmitAlwaysErrors checks that Submit() errors and returns a nil
// []Result. The source explicitly returns `nil, fmt.Errorf(...)`, so neither
// half of the return may be assumed non-nil just because the other is set.
func TestBatchStubSubmitAlwaysErrors(t *testing.T) {
	b := &minimalBatch{}
	results, err := b.Submit()
	if results != nil {
		t.Fatalf("Submit() returned %#v results, want nil", results)
	}
	assertBatchNotImplemented(t, err)
}

// TestBatchStubNoAccumulationAcrossCalls checks that repeated AddCtrlCmd and
// AddIOCmd calls before Submit() change nothing: Len() stays 0 and Submit()
// still errors identically. This documents that no accumulation happens
// anywhere, not even internally before Submit is called.
func TestBatchStubNoAccumulationAcrossCalls(t *testing.T) {
	b := &minimalBatch{}

	for i := 0; i < 5; i++ {
		if err := b.AddCtrlCmd(uint32(i), &uapi.UblksrvCtrlCmd{}, uint64(i)); err == nil {
			t.Fatalf("AddCtrlCmd #%d returned nil error", i)
		}
	}
	for i := 0; i < 5; i++ {
		if err := b.AddIOCmd(uint32(i), &uapi.UblksrvIOCmd{}, uint64(i)); err == nil {
			t.Fatalf("AddIOCmd #%d returned nil error", i)
		}
	}

	if got := b.Len(); got != 0 {
		t.Fatalf("Len() after 10 adds = %d, want 0", got)
	}

	results, err := b.Submit()
	if results != nil {
		t.Fatalf("Submit() returned %#v results, want nil", results)
	}
	assertBatchNotImplemented(t, err)
}

// TestBatchStubInstancesIdentical checks that every minimalBatch instance
// behaves identically, regardless of which construction path produced it. A
// half-implementation that only wired up one construction path would be caught
// here.
func TestBatchStubInstancesIdentical(t *testing.T) {
	viaRing := (&minimalRing{}).NewBatch()
	direct := &minimalBatch{}

	ctrlCmd := &uapi.UblksrvCtrlCmd{}
	ioCmd := &uapi.UblksrvIOCmd{}

	if viaRing == nil {
		t.Fatal("(&minimalRing{}).NewBatch() returned nil")
	}

	if viaRing.Len() != 0 || direct.Len() != 0 {
		t.Fatalf("initial Len() mismatch: viaRing=%d direct=%d, want both 0", viaRing.Len(), direct.Len())
	}

	assertBatchNotImplemented(t, viaRing.AddCtrlCmd(1, ctrlCmd, 2))
	assertBatchNotImplemented(t, direct.AddCtrlCmd(1, ctrlCmd, 2))
	assertBatchNotImplemented(t, viaRing.AddIOCmd(3, ioCmd, 4))
	assertBatchNotImplemented(t, direct.AddIOCmd(3, ioCmd, 4))

	viaResults, viaErr := viaRing.Submit()
	directResults, directErr := direct.Submit()
	if viaResults != nil || directResults != nil {
		t.Fatalf("Submit() result mismatch: viaRing=%#v direct=%#v, want both nil", viaResults, directResults)
	}
	if viaErr == nil || directErr == nil {
		t.Fatalf("Submit() error mismatch: viaRing=%v direct=%v, want both non-nil", viaErr, directErr)
	}
	if viaErr.Error() != directErr.Error() {
		t.Fatalf("Submit() error text mismatch: viaRing=%q direct=%q", viaErr.Error(), directErr.Error())
	}

	if viaRing.Len() != 0 || direct.Len() != 0 {
		t.Fatalf("final Len() mismatch: viaRing=%d direct=%d, want both 0", viaRing.Len(), direct.Len())
	}
}
