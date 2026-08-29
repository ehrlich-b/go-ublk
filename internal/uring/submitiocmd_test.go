package uring

import (
	"os"
	"testing"
	"time"

	"github.com/ehrlich-b/go-ublk/internal/uapi"
)

// This file pins, precisely, the fact that minimalRing.SubmitIOCmd fabricates
// its Result. Its documented contract (Ring.SubmitIOCmd, internal/uring/
// interface.go:27-29) says it "submits an I/O command and returns the result",
// but its body (internal/uring/minimal.go:614-624) calls PrepareIOCmd +
// FlushSubmissions and then UNCONDITIONALLY returns
//
//	&minimalResult{userData: userData, value: 0, err: nil}
//
// without ever reading a single completion-queue entry. The Result is
// fabricated success regardless of what the kernel actually does with the
// command. That gap is the thing under test here; fixing SubmitIOCmd (making
// it read the real completion) is deliberately out of scope for this test.
//
// The real kernel completion for the very same submission is drained here
// separately via WaitForCompletion. On a pipe fd - which does not implement
// .uring_cmd - the kernel completes the submission with value -95
// (-EOPNOTSUPP). Both values cannot be correct for one submission; that is the
// contradiction this test makes visible by name.

// waitForRealCompletion polls ring.WaitForCompletion until the kernel-published
// completion carrying wantUserData is observed, bounded to 10s total so the
// loop can never run forever.
func waitForRealCompletion(t *testing.T, ring Ring, wantUserData uint64) (uint64, int32) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		results, err := ring.WaitForCompletion(100)
		if err != nil {
			t.Fatalf("WaitForCompletion: %v", err)
		}
		for _, r := range results {
			if r.UserData() == wantUserData {
				// Copy out now: the ring reuses its pooled result structs on
				// the next WaitForCompletion call.
				return r.UserData(), r.Value()
			}
		}
		// Not posted yet - poll again.
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("timed out after 10s waiting for a real kernel completion with userData %d", wantUserData)
	return 0, 0
}

// submitOneRound submits one I/O command against a pipe fd via SubmitIOCmd and
// returns BOTH the fabricated Result.Value() and the genuinely kernel-published
// completion Value() for that same submission.
func submitOneRound(t *testing.T, ring Ring, userData uint64) (int32, int32) {
	t.Helper()
	ioCmd := &uapi.UblksrvIOCmd{QID: 0, Tag: 1, Result: 0, Addr: uint64(userData)}
	cmd := uapi.UblkIOCmd(uapi.UBLK_IO_COMMIT_AND_FETCH_REQ)

	res, err := ring.SubmitIOCmd(cmd, ioCmd, userData)
	if err != nil {
		t.Fatalf("SubmitIOCmd returned a submission error: %v", err)
	}

	realUserData, realValue := waitForRealCompletion(t, ring, userData)
	if realUserData != userData {
		t.Fatalf("real completion userData = %d, want %d", realUserData, userData)
	}

	return res.Value(), realValue
}

// TestSubmitIOCmdReturnsFabricatedSuccess opens a real ring against a plain
// pipe (a file descriptor that cannot service URING_CMD) and shows that
// SubmitIOCmd reports Value()==0 while the REAL completion the kernel posts for
// that exact submission is Value()==-95.
func TestSubmitIOCmdReturnsFabricatedSuccess(t *testing.T) {
	pr, pw, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe: %v", err)
	}
	defer pr.Close()
	defer pw.Close()

	ring, err := NewMinimalRing(4, int32(pr.Fd()))
	if err != nil {
		t.Fatalf("NewMinimalRing: %v", err)
	}
	defer ring.Close()

	// Round 1: SubmitIOCmd against a pipe (which cannot service URING_CMD).
	fakeValue1, realValue1 := submitOneRound(t, ring, 4242)
	// SubmitIOCmd's contract promises "returns the result" of the submitted
	// command. It reports fabricated success...
	if fakeValue1 != 0 {
		t.Fatalf("SubmitIOCmd reported Value()=%d, want 0 (fabricated success)", fakeValue1)
	}
	// ... while the REAL completion the kernel posts for that same submission
	// is -95 (-EOPNOTSUPP: pipes do not implement .uring_cmd). SubmitIOCmd's
	// contract is violated: it never reads a completion at all, so its Result
	// cannot reflect what the kernel actually did.
	if realValue1 != -95 {
		t.Fatalf("real kernel completion Value()=%d, want -95 (-EOPNOTSUPP)", realValue1)
	}

	// Round 2: repeat on the SAME ring with a different userData. This rules
	// out "maybe the first completion just hadn't arrived yet" as an
	// alternative explanation for the divergence.
	fakeValue2, realValue2 := submitOneRound(t, ring, 4243)
	if fakeValue2 != 0 {
		t.Fatalf("SubmitIOCmd reported Value()=%d, want 0 (fabricated success)", fakeValue2)
	}
	if realValue2 != -95 {
		t.Fatalf("real kernel completion Value()=%d, want -95 (-EOPNOTSUPP)", realValue2)
	}
}

// TestSubmitIOCmdProductionCallSiteDiscardsResult documents the blast radius of
// the one production call site, precisely. submitInitialFetchReq
// (internal/queue/runner.go:402) calls:
//
//	_, err := r.ring.SubmitIOCmd(cmd, ioCmd, userData)
//
// The fabricated Result is discarded and only the (meaningful)
// submission-mechanics error is checked. So today this SPECIFIC call site is
// NOT corrupted by the fabricated value - nothing reads it. That is incidental
// to how the call happens to be written today, not a property of SubmitIOCmd's
// contract: a future caller that DOES read the Result (or a refactor of
// submitInitialFetchReq that starts checking it) would silently believe every
// I/O command succeeded regardless of what the kernel actually did.
//
// This test reproduces the call site's exact handling - keep the error, throw
// the Result away - and then drains the real completion to show the submission
// genuinely failed at the kernel (-95) while the call site saw no problem.
func TestSubmitIOCmdProductionCallSiteDiscardsResult(t *testing.T) {
	pr, pw, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe: %v", err)
	}
	defer pr.Close()
	defer pw.Close()

	ring, err := NewMinimalRing(4, int32(pr.Fd()))
	if err != nil {
		t.Fatalf("NewMinimalRing: %v", err)
	}
	defer ring.Close()

	const userData = 4244
	ioCmd := &uapi.UblksrvIOCmd{QID: 0, Tag: 1, Result: 0, Addr: uint64(userData)}
	cmd := uapi.UblkIOCmd(uapi.UBLK_IO_COMMIT_AND_FETCH_REQ)

	// Exactly what runner.go:402 does: discard the Result, keep only the error.
	_, err = ring.SubmitIOCmd(cmd, ioCmd, userData)
	if err != nil {
		t.Fatalf("call site sees a submission error: %v", err)
	}

	// The kernel still genuinely failed this submission (-95/-EOPNOTSUPP); the
	// call site is simply blind to it because it threw the Result away. A call
	// site that instead checked the Result would silently believe this command
	// succeeded.
	realUserData, realValue := waitForRealCompletion(t, ring, userData)
	if realUserData != userData {
		t.Fatalf("real completion userData = %d, want %d", realUserData, userData)
	}
	if realValue != -95 {
		t.Fatalf("real kernel completion Value()=%d, want -95 (-EOPNOTSUPP)", realValue)
	}
}
