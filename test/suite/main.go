//go:build linux

// Command ublk-suite is go-ublk's real-kernel conformance suite. It runs as
// root on a machine (or throwaway VM) with ublk_drv available, creates real
// /dev/ublkbN devices through the public API, drives them through the kernel's
// block layer, and checks what the backend saw against what userspace asked
// for. It is the payload the multi-kernel matrix (test/matrix) boots under
// every kernel and distro.
//
// Output contract: one JSON object per test on stdout,
//
//	{"test": "io/discard", "status": "pass|fail|skip|error|timeout",
//	 "duration_s": 0.12, "detail": "..."}
//
// and human-readable progress on stderr. The exit status is 0 when nothing
// failed, 1 when any test failed, errored or timed out.
//
// It is a static, CGO-free binary: build with `make suite` and copy it to the
// target. Destructive by design — only run it where creating, killing and
// deleting ublk devices is acceptable.
package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"regexp"
	"runtime"
	"runtime/debug"
	"strings"
	"time"
)

// errSkip marks a test as not applicable on this machine (missing kernel
// feature, missing mkfs, ...). Wrap it with a reason via skipf.
var errSkip = errors.New("skip")

func skipf(format string, args ...any) error {
	return fmt.Errorf("%w: %s", errSkip, fmt.Sprintf(format, args...))
}

// T is the per-test context. Tests register cleanups that always run, in
// reverse order, even when the test fails.
type T struct {
	name     string
	scale    float64
	cleanups []func()
	logs     []string
}

func (t *T) Cleanup(f func()) { t.cleanups = append(t.cleanups, f) }

func (t *T) Logf(format string, args ...any) {
	msg := fmt.Sprintf(format, args...)
	t.logs = append(t.logs, msg)
	fmt.Fprintf(os.Stderr, "    %s: %s\n", t.name, msg)
}

// Duration scales a nominal run time by -scale, so the same suite is quick
// under TCG emulation and thorough on real hardware.
func (t *T) Duration(d time.Duration) time.Duration {
	return time.Duration(float64(d) * t.scale)
}

// Count scales a nominal iteration count by -scale (at least 1).
func (t *T) Count(n int) int {
	if c := int(float64(n) * t.scale); c > 1 {
		return c
	}
	return 1
}

type test struct {
	name    string
	timeout time.Duration
	fn      func(*T) error
}

var registry []test

func register(name string, timeout time.Duration, fn func(*T) error) {
	registry = append(registry, test{name: name, timeout: timeout, fn: fn})
}

type result struct {
	Test     string  `json:"test"`
	Status   string  `json:"status"`
	Duration float64 `json:"duration_s"`
	Detail   string  `json:"detail,omitempty"`
}

func main() {
	if len(os.Args) > 1 && os.Args[1] == serverSubcommand {
		serverMain(os.Args[2:])
		return
	}

	var (
		list  = flag.Bool("list", false, "list tests and exit")
		run   = flag.String("run", "", "only run tests whose name matches this regexp")
		skip  = flag.String("skip", "", "skip tests whose name matches this regexp")
		scale = flag.Float64("scale", 1, "multiply run times and iteration counts (use <1 under emulation)")
	)
	flag.Parse()

	// Run order is registration order (file order, then order within a file):
	// tests that kill servers or race teardown are registered last.
	if *list {
		for _, tc := range registry {
			fmt.Println(tc.name)
		}
		return
	}
	var runRe, skipRe *regexp.Regexp
	if *run != "" {
		runRe = regexp.MustCompile(*run)
	}
	if *skip != "" {
		skipRe = regexp.MustCompile(*skip)
	}

	fmt.Fprintf(os.Stderr, "ublk-suite: kernel %s, %s/%s, %d CPUs\n",
		kernelRelease(), runtime.GOOS, runtime.GOARCH, runtime.NumCPU())
	if os.Geteuid() != 0 {
		fmt.Fprintln(os.Stderr, "ublk-suite: must run as root")
		os.Exit(2)
	}

	enc := json.NewEncoder(os.Stdout)
	failed := false
	for _, tc := range registry {
		if runRe != nil && !runRe.MatchString(tc.name) {
			continue
		}
		if skipRe != nil && skipRe.MatchString(tc.name) {
			continue
		}
		res := runOne(tc, *scale)
		if res.Status == "fail" || res.Status == "error" || res.Status == "timeout" {
			failed = true
		}
		_ = enc.Encode(res)
		fmt.Fprintf(os.Stderr, "%-7s %s (%.2fs) %s\n", strings.ToUpper(res.Status), res.Test, res.Duration, res.Detail)
	}
	if failed {
		os.Exit(1)
	}
}

// runOne runs a test with a wall-clock timeout. A test that times out is
// abandoned, not killed — its goroutine may be stuck in an uninterruptible
// syscall — and its cleanups are left to run if it ever returns. That is the
// honest outcome: a hung kernel is a result, and the harness's own VM timeout
// is the backstop.
func runOne(tc test, scale float64) result {
	t := &T{name: tc.name, scale: scale}
	start := time.Now()
	done := make(chan error, 1)
	go func() {
		var err error
		defer func() {
			if p := recover(); p != nil {
				err = fmt.Errorf("panic: %v\n%s", p, debug.Stack())
			}
			for i := len(t.cleanups) - 1; i >= 0; i-- {
				func() {
					defer func() { _ = recover() }()
					t.cleanups[i]()
				}()
			}
			done <- err
		}()
		err = tc.fn(t)
	}()

	timeout := time.Duration(float64(tc.timeout) * max(scale, 1))
	var err error
	select {
	case err = <-done:
	case <-time.After(timeout):
		buf := make([]byte, 1<<20)
		n := runtime.Stack(buf, true)
		fmt.Fprintf(os.Stderr, "=== %s timed out after %s; goroutines:\n%s\n", tc.name, timeout, buf[:n])
		return result{Test: tc.name, Status: "timeout", Duration: time.Since(start).Seconds(),
			Detail: fmt.Sprintf("no result after %s", timeout)}
	}

	res := result{Test: tc.name, Duration: time.Since(start).Seconds()}
	switch {
	case err == nil:
		res.Status = "pass"
	case errors.Is(err, errSkip):
		res.Status = "skip"
		res.Detail = strings.TrimPrefix(err.Error(), errSkip.Error()+": ")
	default:
		res.Status = "fail"
		res.Detail = err.Error()
	}
	return res
}

func kernelRelease() string {
	b, err := os.ReadFile("/proc/sys/kernel/osrelease")
	if err != nil {
		return "unknown"
	}
	return strings.TrimSpace(string(b))
}
