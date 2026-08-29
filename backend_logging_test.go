package ublk

import (
	"fmt"
	"regexp"
	"strings"
	"sync"
	"testing"

	"github.com/ehrlich-b/go-ublk/internal/logging"
)

// fakeLogger is an in-memory Logger that records every formatted line. It is
// used exclusively to observe what loggerWriter and configureLogging actually
// forward, without touching any real output.
type fakeLogger struct {
	mu    sync.Mutex
	lines []string
}

func (f *fakeLogger) Printf(format string, args ...interface{}) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.lines = append(f.lines, fmt.Sprintf(format, args...))
}

func (f *fakeLogger) Debugf(format string, args ...interface{}) {
	f.Printf(format, args...)
}

// captured returns a snapshot of all recorded lines.
func (f *fakeLogger) captured() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.lines...)
}

// count returns how many lines were recorded.
func (f *fakeLogger) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.lines)
}

// hasLine reports whether any recorded line contains substr.
func (f *fakeLogger) hasLine(substr string) bool {
	for _, l := range f.captured() {
		if strings.Contains(l, substr) {
			return true
		}
	}
	return false
}

// restoreDefaultLogger pins the logging footgun in the task: configureLogging
// replaces the process-wide default logger via logging.SetDefault. Every
// subtest here captures the current default BEFORE any configureLogging call
// and restores it via t.Cleanup, so no other test in this package (or, if run
// together, in internal/logging) ever observes a changed global after our file
// is done. Tests in this file deliberately do NOT use t.Parallel because they
// all share that one mutable global.
func restoreDefaultLogger(t *testing.T) {
	t.Helper()
	prev := logging.Default()
	t.Cleanup(func() { logging.SetDefault(prev) })
}

// TestLoggingLoggerWriterTrim pins what loggerWriter.Write forwards: it trims
// trailing newlines with strings.TrimRight(s, "\n") — which removes ALL
// trailing newlines, not just one — forwards the rest to the caller's Logger,
// and still calls Printf even for empty input rather than short-circuiting.
func TestLoggingLoggerWriterTrim(t *testing.T) {
	cases := []struct {
		name  string
		input string
		want  string
	}{
		{"single trailing newline", "hello\n", "hello"},
		// strings.TrimRight(s, "\n") removes the ENTIRE trailing run of
		// newlines, not a single one: "hello\n\n" -> "hello". If someone later
		// "fixes" this to trim only one newline, the forwarded line would be
		// "hello\n" and this assertion fails.
		{"multiple trailing newlines are all trimmed", "hello\n\n", "hello"},
		{"no trailing newline", "no newline", "no newline"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			fake := &fakeLogger{}
			w := loggerWriter{l: fake}

			n, err := w.Write([]byte(tc.input))
			if err != nil {
				t.Fatalf("Write(%q) error = %v", tc.input, err)
			}

			lines := fake.captured()
			if len(lines) != 1 {
				t.Fatalf("Write(%q) produced %d lines, want 1: %q", tc.input, len(lines), lines)
			}
			if lines[0] != tc.want {
				t.Errorf("forwarded line = %q, want %q", lines[0], tc.want)
			}
			if n != len(tc.input) {
				t.Errorf("returned n = %d, want len(input) = %d", n, len(tc.input))
			}
		})
	}

	t.Run("empty input still calls Printf", func(t *testing.T) {
		fake := &fakeLogger{}
		w := loggerWriter{l: fake}

		n, err := w.Write([]byte(""))
		if err != nil {
			t.Fatalf("Write([]byte(\"\")) error = %v", err)
		}
		if got := fake.count(); got != 1 {
			t.Errorf("Write([]byte(\"\")) called Printf %d time(s), want 1 (it must not short-circuit)", got)
		}
		lines := fake.captured()
		if len(lines) != 1 || lines[0] != "" {
			t.Errorf("captured lines = %q, want exactly [\"\"]", lines)
		}
		if n != 0 {
			t.Errorf("returned n = %d, want 0 for empty input", n)
		}
	})
}

// TestLoggingLoggerWriterReturnsOriginalLength pins the io.Writer contract
// described for loggerWriter: Write reports n = len(p) of the ORIGINAL input,
// even though the bytes actually forwarded to Printf (after trimming) are
// shorter. Both numbers are reported on purpose so nobody "fixes" the mismatch
// under the wrong assumption that n should equal the length of the line that
// reached the logger.
func TestLoggingLoggerWriterReturnsOriginalLength(t *testing.T) {
	fake := &fakeLogger{}
	w := loggerWriter{l: fake}

	input := "hello\n\n" // 8 bytes input, 5 bytes forwarded
	n, err := w.Write([]byte(input))
	if err != nil {
		t.Fatalf("Write(%q) error = %v", input, err)
	}

	if n != len(input) {
		t.Errorf("Write returned n = %d, want len(original input) = %d", n, len(input))
	}

	lines := fake.captured()
	if len(lines) != 1 {
		t.Fatalf("captured %d lines, want 1: %q", len(lines), lines)
	}
	if lines[0] != "hello" {
		t.Errorf("forwarded line = %q, want %q", lines[0], "hello")
	}
	if n == len(lines[0]) {
		t.Errorf("n (%d) equals the trimmed line length (%d) — the io.Writer distinction is not being exercised; input must end in newlines so the lengths differ", n, len(lines[0]))
	}
}

// TestLoggingRoutesToCallerLogger confirms configureLogging routes the
// library's internal logger output to a caller-supplied Options.Logger, which
// is the seam TODO Critical Bug #12 covers: without the fix the caller's logger
// never saw the internal diagnostics at all.
func TestLoggingRoutesToCallerLogger(t *testing.T) {
	restoreDefaultLogger(t)

	fake := &fakeLogger{}
	configureLogging(&Options{Logger: fake})

	logging.Info("test message", "k", "v")

	if !fake.hasLine("test message") {
		t.Errorf("caller logger did not receive the internal INFO line; captured: %q", fake.captured())
	}
}

// TestLoggingNilLoggerLeavesStderr asserts the observable half of
// "a nil Options.Logger points the internal logger back at os.Stderr": after a
// fresh configureLogging(&Options{}) runs, the caller's logger from an earlier
// configuration no longer receives any lines. We cannot capture os.Stderr
// itself here (that would require redirecting the process's stderr fd), so we
// pin the negative instead.
func TestLoggingNilLoggerLeavesStderr(t *testing.T) {
	restoreDefaultLogger(t)

	fake := &fakeLogger{}
	configureLogging(&Options{Logger: fake})
	logging.Info("routed to fake")
	if fake.count() == 0 {
		t.Fatal("fake logger received no line while it was installed; cannot test the reroute")
	}
	before := fake.count()

	configureLogging(&Options{}) // Logger == nil: output reverts to stderr

	logging.Info("should go to stderr, not the fake")

	if after := fake.count(); after != before {
		t.Errorf("fake logger received %d new line(s) after configureLogging(&Options{}); routing did not leave it (captured: %q)",
			after-before, fake.captured())
	}
}

// TestLoggingDebugLevel exercises the level filter in internal/logging's log
// method (level < l.level returns early). Keep in mind LevelDebug == 0 and
// LevelInfo == 1, so DEBUG lines are dropped unless Options.Debug sets the
// level to LevelDebug.
func TestLoggingDebugLevel(t *testing.T) {
	restoreDefaultLogger(t)

	fake := &fakeLogger{}
	configureLogging(&Options{Logger: fake, Debug: true})
	logging.Debug("debug message")
	if !fake.hasLine("debug message") {
		t.Errorf("DEBUG line not captured with Debug: true; captured: %q", fake.captured())
	}

	filtered := &fakeLogger{}
	configureLogging(&Options{Logger: filtered, Debug: false})
	logging.Debug("should not appear")
	if filtered.hasLine("should not appear") {
		t.Errorf("DEBUG line captured with Debug: false; captured: %q", filtered.captured())
	}
	logging.Info("still info")
	if !filtered.hasLine("still info") {
		t.Errorf("INFO line not captured with Debug: false (only DEBUG should be filtered); captured: %q", filtered.captured())
	}
}

// TestLoggingNoTimestamp infers NoTimestamp observably: with a caller's Logger
// installed, a line must carry only the level prefix plus the message — never
// the stdlib log.LstdFlags date/time prefix (e.g. "2024/01/02 15:04:05").
func TestLoggingNoTimestamp(t *testing.T) {
	restoreDefaultLogger(t)

	fake := &fakeLogger{}
	configureLogging(&Options{Logger: fake})
	logging.Info("timestamp check")

	lines := fake.captured()
	if len(lines) != 1 {
		t.Fatalf("captured %d lines, want 1: %q", len(lines), lines)
	}

	timestampRe := regexp.MustCompile(`^\d{4}/\d{2}/\d{2} \d{2}:\d{2}:\d{2}`)
	if timestampRe.MatchString(lines[0]) {
		t.Errorf("captured line %q carries a stdlib timestamp prefix; NoTimestamp was not honored", lines[0])
	}

	want := "[INFO] timestamp check"
	if lines[0] != want {
		t.Errorf("captured line = %q, want %q (only the level prefix plus the message)", lines[0], want)
	}
}
