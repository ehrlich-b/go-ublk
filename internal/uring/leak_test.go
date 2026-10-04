package uring

import (
	"errors"
	"io"
	"os"
	"runtime"
	"strings"
	"testing"

	"github.com/ehrlich-b/go-ublk/internal/logging"
	"golang.org/x/sys/unix"
)

// Leak tests for Critical Bug #17: every ring a device lifecycle (or a
// ListDevices call) creates must give back its mappings and fds on Close.
// This file uses only the Ring API that predates the IoUring core, so it can
// be dropped onto the old implementation as the control that proves the test
// fails there (each ring leaked its SQ, CQ and SQE mappings).

const leakCycles = 5000

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
		if err := cycle(); err != nil {
			t.Fatalf("cycle %d: %v", i, err)
		}
	}
	runtime.GC()
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
