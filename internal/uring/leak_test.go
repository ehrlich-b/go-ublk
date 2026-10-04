package uring

import (
	"errors"
	"io"
	"os"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/ehrlich-b/go-ublk/internal/logging"
	"golang.org/x/sys/unix"
)

// Leak tests for Critical Bug #17: every ring a device lifecycle (or a
// ListDevices call) creates must give back its mappings and fds on Close.
// This file uses only the Ring API that predates the IoUring core, so it can
// be dropped onto the old implementation as the control that proves the test
// fails there (each ring leaked its SQ, CQ and SQE mappings).

// leakCycles is enough to expose a per-ring leak (the old code left three
// mappings per ring) while staying inside small RLIMIT_MEMLOCK budgets: the
// kernel frees a closed ring's memory asynchronously, so a tight loop of
// thousands can transiently exhaust the budget (seen on GitHub's runners).
const leakCycles = 500

func countMapsAndFds(t *testing.T) (int, int) {
	t.Helper()
	maps, err := os.ReadFile("/proc/self/maps")
	if err != nil {
		t.Skipf("no /proc/self/maps: %v", err)
	}
	fds, err := os.ReadDir("/proc/self/fd")
	if err != nil {
		t.Skipf("no /proc/self/fd: %v", err)
	}
	return strings.Count(string(maps), "\n"), len(fds)
}

func quietLogs(t testing.TB) {
	t.Helper()
	prev := logging.Default()
	logging.SetDefault(logging.NewLogger(&logging.Config{Level: logging.LevelError, Output: io.Discard}))
	t.Cleanup(func() { logging.SetDefault(prev) })
}

// assertNoLeak runs cycle leakCycles times and checks that the mapping and
// fd counts return to their baseline.
func assertNoLeak(t *testing.T, cycle func() error) {
	t.Helper()
	if err := cycle(); err != nil { // warm-up: lazily created runtime state
		if errors.Is(err, unix.EPERM) || errors.Is(err, unix.ENOSYS) {
			t.Skipf("io_uring unavailable: %v", err)
		}
		t.Fatalf("warm-up cycle: %v", err)
	}
	runtime.GC()
	baseMaps, baseFds := countMapsAndFds(t)
	for i := 0; i < leakCycles; i++ {
		err := cycle()
		// ENOMEM here is the locked-memory budget of rings closed moments
		// ago that the kernel has not finished freeing; wait it out.
		for tries := 0; errors.Is(err, unix.ENOMEM) && tries < 100; tries++ {
			time.Sleep(20 * time.Millisecond)
			err = cycle()
		}
		if err != nil {
			t.Fatalf("cycle %d: %v", i, err)
		}
	}
	runtime.GC()
	// Leave the locked-memory budget usable for the tests that follow.
	for tries := 0; tries < 250 && errors.Is(cycle(), unix.ENOMEM); tries++ {
		time.Sleep(20 * time.Millisecond)
	}
	maps, fds := countMapsAndFds(t)
	t.Logf("%d cycles: /proc/self/maps %d -> %d lines, /proc/self/fd %d -> %d", leakCycles, baseMaps, maps, baseFds, fds)
	if fds != baseFds {
		t.Errorf("fd count went from %d to %d over %d cycles", baseFds, fds, leakCycles)
	}
	// The Go runtime may map a few heap arenas while the loop runs; a leaking
	// ring costs at least two mappings per cycle, thousands over the loop.
	if maps > baseMaps+64 {
		t.Errorf("/proc/self/maps grew from %d to %d lines over %d cycles", baseMaps, maps, leakCycles)
	}
}

func TestRingCloseReleasesMappingsAndFds(t *testing.T) {
	quietLogs(t)
	pr, pw, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe: %v", err)
	}
	defer pr.Close()
	defer pw.Close()
	assertNoLeak(t, func() error {
		ring, err := NewRing(Config{Entries: 4, FD: int32(pr.Fd())})
		if err != nil {
			return err
		}
		return ring.Close()
	})
}
