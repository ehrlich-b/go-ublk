// Package logging provides structured logging for the go-ublk project
package logging

import (
	"context"
	"io"
	"os"
	"sync"

	"github.com/rs/zerolog"
)

// Logger wraps zerolog.Logger with ublk-specific structured fields
type Logger struct {
	zlog     zerolog.Logger
	deviceID *int
}

var (
	defaultLogger *Logger
	mu            sync.RWMutex
)

// LogLevel represents the available log levels
type LogLevel int

const (
	LevelDebug LogLevel = LogLevel(zerolog.DebugLevel)
	LevelInfo  LogLevel = LogLevel(zerolog.InfoLevel)
	LevelWarn  LogLevel = LogLevel(zerolog.WarnLevel)
	LevelError LogLevel = LogLevel(zerolog.ErrorLevel)
)

// Config holds logging configuration
type Config struct {
	Level   LogLevel
	Format  string // "json" or "text"
	Output  io.Writer
	Sync    bool // If true, writes are synchronous (useful for testing)
	NoColor bool // If true, disables ANSI color codes (useful for testing)
}

// DefaultConfig returns a sensible default configuration
func DefaultConfig() *Config {
	return &Config{
		Level:  LevelInfo,
		Format: "text",
		Output: os.Stderr,
	}
}

// asyncWriter wraps an io.Writer with an async buffered channel
// This prevents blocking in hot paths
type asyncWriter struct {
	out    io.Writer
	ch     chan []byte
	done   chan struct{}
	closed bool
	mu     sync.Mutex
}

func newAsyncWriter(w io.Writer, bufferSize int) *asyncWriter {
	aw := &asyncWriter{
		out:  w,
		ch:   make(chan []byte, bufferSize),
		done: make(chan struct{}),
	}
	go aw.run()
	return aw
}

func (aw *asyncWriter) run() {
	defer close(aw.done)
	for msg := range aw.ch {
		aw.out.Write(msg)
	}
}

func (aw *asyncWriter) Write(p []byte) (n int, err error) {
	aw.mu.Lock()
	if aw.closed {
		aw.mu.Unlock()
		return 0, io.ErrClosedPipe
	}
	aw.mu.Unlock()

	// Make a copy since p might be reused
	msg := make([]byte, len(p))
	copy(msg, p)

	// Non-blocking write - drop if buffer full (better than blocking)
	select {
	case aw.ch <- msg:
		return len(p), nil
	default:
		// Buffer full - drop message to avoid blocking
		return len(p), nil
	}
}

func (aw *asyncWriter) Close() error {
	aw.mu.Lock()
	if !aw.closed {
		aw.closed = true
		close(aw.ch)
	}
	aw.mu.Unlock()
	<-aw.done
	return nil
}

// NewLogger creates a new structured logger
func NewLogger(config *Config) *Logger {
	if config == nil {
		config = DefaultConfig()
	}

	// Use async writer unless Sync mode is enabled
	var output io.Writer = config.Output
	if !config.Sync {
		output = newAsyncWriter(config.Output, 1000)
	}

	var zlog zerolog.Logger
	switch config.Format {
	case "json":
		zlog = zerolog.New(output).With().Timestamp().Logger()
	default:
		// Console format (colors can be disabled via config)
		consoleWriter := zerolog.ConsoleWriter{Out: output, NoColor: config.NoColor}
		zlog = zerolog.New(consoleWriter).With().Timestamp().Logger()
	}

	zlog = zlog.Level(zerolog.Level(config.Level))

	return &Logger{
		zlog: zlog,
	}
}

// Default returns the default logger, creating it if necessary
func Default() *Logger {
	mu.RLock()
	if defaultLogger != nil {
		defer mu.RUnlock()
		return defaultLogger
	}
	mu.RUnlock()

	mu.Lock()
	defer mu.Unlock()
	if defaultLogger == nil {
		defaultLogger = NewLogger(nil)
	}
	return defaultLogger
}

// SetDefault sets the default logger
func SetDefault(logger *Logger) {
	mu.Lock()
	defer mu.Unlock()
	defaultLogger = logger
}

// WithDevice returns a logger with device ID context
func (l *Logger) WithDevice(deviceID int) *Logger {
	return &Logger{
		zlog:     l.zlog.With().Int("device_id", deviceID).Logger(),
		deviceID: &deviceID,
	}
}

// WithQueue returns a logger with queue context
func (l *Logger) WithQueue(queueID int) *Logger {
	return &Logger{
		zlog:     l.zlog.With().Int("queue_id", queueID).Logger(),
		deviceID: l.deviceID,
	}
}

// WithRequest returns a logger with request context
func (l *Logger) WithRequest(tag uint16, opType string) *Logger {
	return &Logger{
		zlog:     l.zlog.With().Uint16("tag", tag).Str("op", opType).Logger(),
		deviceID: l.deviceID,
	}
}

// WithError returns a logger with error context
func (l *Logger) WithError(err error) *Logger {
	return &Logger{
		zlog:     l.zlog.With().Err(err).Logger(),
		deviceID: l.deviceID,
	}
}

// Standard logging methods
func (l *Logger) Debug(msg string, args ...any) {
	event := l.zlog.Debug()
	for i := 0; i < len(args); i += 2 {
		if i+1 < len(args) {
			key := args[i].(string)
			event = event.Interface(key, args[i+1])
		}
	}
	event.Msg(msg)
}

func (l *Logger) Info(msg string, args ...any) {
	event := l.zlog.Info()
	for i := 0; i < len(args); i += 2 {
		if i+1 < len(args) {
			key := args[i].(string)
			event = event.Interface(key, args[i+1])
		}
	}
	event.Msg(msg)
}

func (l *Logger) Warn(msg string, args ...any) {
	event := l.zlog.Warn()
	for i := 0; i < len(args); i += 2 {
		if i+1 < len(args) {
			key := args[i].(string)
			event = event.Interface(key, args[i+1])
		}
	}
	event.Msg(msg)
}

func (l *Logger) Error(msg string, args ...any) {
	event := l.zlog.Error()
	for i := 0; i < len(args); i += 2 {
		if i+1 < len(args) {
			key := args[i].(string)
			event = event.Interface(key, args[i+1])
		}
	}
	event.Msg(msg)
}

// Context-aware logging
func (l *Logger) DebugContext(ctx context.Context, msg string, args ...any) {
	l.Debug(msg, args...)
}

func (l *Logger) InfoContext(ctx context.Context, msg string, args ...any) {
	l.Info(msg, args...)
}

func (l *Logger) WarnContext(ctx context.Context, msg string, args ...any) {
	l.Warn(msg, args...)
}

func (l *Logger) ErrorContext(ctx context.Context, msg string, args ...any) {
	l.Error(msg, args...)
}

// Printf-style logging for compatibility
func (l *Logger) Debugf(format string, args ...any) {
	l.zlog.Debug().Msgf(format, args...)
}

func (l *Logger) Infof(format string, args ...any) {
	l.zlog.Info().Msgf(format, args...)
}

func (l *Logger) Warnf(format string, args ...any) {
	l.zlog.Warn().Msgf(format, args...)
}

func (l *Logger) Errorf(format string, args ...any) {
	l.zlog.Error().Msgf(format, args...)
}

// Convenience functions for global logger
func Debug(msg string, args ...any) {
	Default().Debug(msg, args...)
}

func Info(msg string, args ...any) {
	Default().Info(msg, args...)
}

func Warn(msg string, args ...any) {
	Default().Warn(msg, args...)
}

func Error(msg string, args ...any) {
	Default().Error(msg, args...)
}

func DebugCtx(ctx context.Context, msg string, args ...any) {
	Default().DebugContext(ctx, msg, args...)
}

func InfoCtx(ctx context.Context, msg string, args ...any) {
	Default().InfoContext(ctx, msg, args...)
}

func WarnCtx(ctx context.Context, msg string, args ...any) {
	Default().WarnContext(ctx, msg, args...)
}

func ErrorCtx(ctx context.Context, msg string, args ...any) {
	Default().ErrorContext(ctx, msg, args...)
}