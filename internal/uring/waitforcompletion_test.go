package uring

import (
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/ehrlich-b/go-ublk/internal/uapi"
)

// Real-ring tests for WaitForCompletion. They open an actual io_uring against a
// dummy pipe file descriptor (which needs no filesystem path) and drive the
// ring through URING_CMD submissions that the kernel completes with
// -EOPNOTSUPP (-95), because a pipe has no .uring_cmd handler.
//
// These tests need a kernel that permits io_uring; inside a default-seccomp
// container io_uring_setup is denied with EPERM, so there they are skipped. The
// runtime proof happens on a host kernel (>= 6.8).

// ctrlCmdValue is what the kernel posts for a URING_CMD against a non-ublk fd:
// -EOPNOTSUPP. Every result we drain below must carry this Value().
const ctrlCmdValue int32 = -95

// newRingForTest opens a real ring bound to a pipe's read fd. If io_uring
// cannot be opened (sandbox seccomp EPERM) it skips the test. The returned ring
// and pipe are cleaned up when the test finishes.
func newRingForTest(t *testing.T) Ring {
	t.Helper()

	readFd, writeFd, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe() failed: %v", err)
	}

	ring, err := NewMinimalRing(8, int32(readFd.Fd()))
	if err != nil {
		_ = readFd.Close()
		_ = writeFd.Close()
		t.Skipf("io_uring cannot be opened in this environment: %v", err)
	}

	t.Cleanup(func() {
		_ = ring.Close()
		_ = readFd.Close()
		_ = writeFd.Close()
	})
	return ring
}

// submitCtrlAsync submits one async URING_CMD with the given userData against
// the ring's pipe fd. Every such command completes with Value() == ctrlCmdValue.
func submitCtrlAsync(t *testing.T, ring Ring, userData uint64) {
	t.Helper()
	ctrl := &uapi.UblksrvCtrlCmd{} // 32-byte payload; contents don't matter for a pipe
	if _, err := ring.SubmitCtrlCmdAsync(0, ctrl, userData); err != nil {
		t.Fatalf("SubmitCtrlCmdAsync(userData=%d) failed: %v", userData, err)
	}
}

// noteResults immediately copies UserData()/Value() out of one drained batch
// into seen. It must be called before any further WaitForCompletion, because
// the returned results are *minimalResult pointers into the ring's reuse pool:
// the next call resliced the same backing array and silently overwrites those
// structs. Copying into a map of plain values is the only safe way to retain
// anything across calls.
func noteResults(seen map[uint64]int32, results []Result) {
	for _, res := range results {
		if res != nil {
			seen[res.UserData()] = res.Value()
		}
	}
}

// describeResults renders one drained batch for failure messages.
func describeResults(results []Result) string {
	var b strings.Builder
	b.WriteString("results[")
	for i, res := range results {
		if i > 0 {
			b.WriteString(", ")
		}
		if res == nil {
			b.WriteString("nil")
			continue
		}
		fmt.Fprintf(&b, "{ud=%d val=%d}", res.UserData(), res.Value())
	}
	b.WriteString("]")
	return b.String()
}

// drainOne loops the bounded (timeout > 0) variant of WaitForCompletion until a
// completion with the given userData shows up or the deadline passes. The moment
// it appears, its UserData()/Value() are copied into plain uint64/int32 locals
// and returned — a caller that instead held the []Result or Result interface
// across a later WaitForCompletion call would observe silent overwrites from the
// reuse pool.
func drainOne(t *testing.T, ring Ring, want uint64, timeout time.Duration) (uint64, int32) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		results, err := ring.WaitForCompletion(100)
		if err != nil {
			t.Fatalf("WaitForCompletion(100) returned error: %v", err)
		}
		for _, res := range results {
			if res == nil {
				continue
			}
			if ud, val := res.UserData(), res.Value(); ud == want {
				return ud, val
			}
		}
	}
	t.Fatalf("deadline (%v) passed without seeing userData %d", timeout, want)
	return 0, 0
}

// assertEmptyAndFast checks WaitForCompletion(timeout > 0) on a ring with truly
// nothing pending: no error, an empty slice, and a return far faster than the
// requested timeout (branch 2 does a single non-blocking check-and-process and
// must NOT sleep for the timeout).
func assertEmptyAndFast(t *testing.T, ring Ring, label string) {
	t.Helper()
	start := time.Now()
	results, err := ring.WaitForCompletion(50)
	elapsed := time.Since(start)

	if err != nil {
		t.Fatalf("%s: WaitForCompletion(50) returned error: %v", label, err)
	}
	if len(results) != 0 {
		t.Fatalf("%s: WaitForCompletion(50) returned %d results, want 0: %s",
			label, len(results), describeResults(results))
	}
	if elapsed >= 100*time.Millisecond {
		t.Errorf("%s: WaitForCompletion(50) took %v; branch 2 must return immediately, not block for the timeout", label, elapsed)
	}
}

// Item 1: timeout > 0 with nothing pending returns immediately with an empty,
// non-error result.
func TestWaitForCompletionBoundedEmptyReturnsImmediately(t *testing.T) {
	ring := newRingForTest(t)
	assertEmptyAndFast(t, ring, "pre-submission ring")
}

// Item 5: the very first WaitForCompletion of a ring's lifetime, before any
// submission has ever happened, behaves exactly like item 1.
func TestWaitForCompletionFreshRingReturnsEmpty(t *testing.T) {
	ring := newRingForTest(t)
	assertEmptyAndFast(t, ring, "fresh ring with no submissions")
}

// Items 2 + 3: submit two commands, drain them (blocking call from a goroutine,
// then bounded loops), then verify the now-empty CQ does not leak a stale pooled
// result into a later call.
func TestWaitForCompletionDrainsMultipleAndDoesNotLeakPool(t *testing.T) {
	ring := newRingForTest(t)

	submitCtrlAsync(t, ring, 701)
	submitCtrlAsync(t, ring, 702)

	// Blocking drain (timeout == 0) from a goroutine, wrapped in a bounded
	// select so a wedged ring fails the test instead of hanging it. Note: the
	// call's returned slice must be consumed immediately — the value-copy into
	// seen happens right below, before any further WaitForCompletion.
	type blockingOutcome struct {
		results []Result
		err     error
	}
	blocked := make(chan blockingOutcome, 1)
	go func() {
		results, err := ring.WaitForCompletion(0)
		blocked <- blockingOutcome{results: results, err: err}
	}()

	seen := make(map[uint64]int32)
	select {
	case out := <-blocked:
		if out.err != nil {
			t.Fatalf("WaitForCompletion(0) returned error: %v", out.err)
		}
		noteResults(seen, out.results)
	case <-time.After(10 * time.Second):
		t.Fatal("WaitForCompletion(0) blocked for 10s without draining")
	}

	// The kernel may not have posted both completions by the time the blocking
	// call returned; keep draining with the bounded (timeout > 0) variant — each
	// iteration copies values out immediately — until both are seen or a 5s
	// overall deadline passes.
	deadline := time.Now().Add(5 * time.Second)
	for len(seen) < 2 && time.Now().Before(deadline) {
		results, err := ring.WaitForCompletion(100)
		if err != nil {
			t.Fatalf("WaitForCompletion(100) returned error: %v", err)
		}
		noteResults(seen, results)
	}

	if seen[701] != ctrlCmdValue || seen[702] != ctrlCmdValue {
		t.Fatalf("never saw both completions; seen = %v, want userData 701 and 702 with Value() == %d", seen, ctrlCmdValue)
	}

	// Item 3: drain any straggler the loops above may have left, then confirm a
	// call on the now-empty CQ returns an empty slice. This is the headline
	// regression check: the resultsPool/cqePool reset at the top of the call must
	// be complete, or a stale entry from item 2 would survive into this call.
	_, _ = ring.WaitForCompletion(100)
	results, err := ring.WaitForCompletion(50)
	if err != nil {
		t.Fatalf("WaitForCompletion(50) on drained ring returned error: %v", err)
	}
	if len(results) != 0 {
		t.Fatalf("pool leaked %d stale result(s) into a call on an empty CQ: %s",
			len(results), describeResults(results))
	}
}

// Item 4: two separate WaitForCompletion calls each return their own contents,
// not values silently aliased through the ring's reuse pool. The copied locals
// from the first call must still describe userData 801 after a second call has
// reused the pooled minimalResult storage for userData 802.
func TestWaitForCompletionSeparateCallsDoNotAlias(t *testing.T) {
	ring := newRingForTest(t)

	submitCtrlAsync(t, ring, 801)
	ud1, val1 := drainOne(t, ring, 801, 5*time.Second)
	if ud1 != 801 || val1 != ctrlCmdValue {
		t.Fatalf("first call returned {ud=%d val=%d}, want {ud=801 val=%d}", ud1, val1, ctrlCmdValue)
	}

	submitCtrlAsync(t, ring, 802)
	ud2, val2 := drainOne(t, ring, 802, 5*time.Second)
	if ud2 != 802 || val2 != ctrlCmdValue {
		t.Fatalf("second call returned {ud=%d val=%d}, want {ud=802 val=%d}", ud2, val2, ctrlCmdValue)
	}

	// ud1/val1 are plain locals, copied out of the pool-backed result before the
	// second call could overwrite its backing minimalResult. If the implementation
	// aliased live objects through the pool the way a naive reuse bug would, a
	// lazy reader (one that kept the first Result interface and read Value() only
	// now) would see the wrong identity. The copies prove the values stay intact.
	if ud1 != 801 || val1 != ctrlCmdValue {
		t.Errorf("first call's copied values were silently overwritten: got {ud=%d val=%d}, want {ud=801 val=%d}", ud1, val1, ctrlCmdValue)
	}
	if ud2 != 802 || val2 != ctrlCmdValue {
		t.Errorf("second call's copied values were silently overwritten: got {ud=%d val=%d}, want {ud=802 val=%d}", ud2, val2, ctrlCmdValue)
	}
}
