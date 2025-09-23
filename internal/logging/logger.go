// Package logging provides structured logging for the go-ublk project
package logging

import (
	"context"
	"io"
	"log/slog"
	"os"
	"sync"
)

// Logger wraps slog.Logger with ublk-specific structured fields
type Logger struct {
	*slog.Logger
	deviceID *int
}

var (
	defaultLogger *Logger
	mu            sync.RWMutex
)

// LogLevel represents the available log levels
type LogLevel int

const (
	LevelDebug LogLevel = LogLevel(slog.LevelDebug)
	LevelInfo  LogLevel = LogLevel(slog.LevelInfo)
	LevelWarn  LogLevel = LogLevel(slog.LevelWarn)
	LevelError LogLevel = LogLevel(slog.LevelError)
)

// Config holds logging configuration
type Config struct {
	Level  LogLevel
	Format string // "json" or "text"
	Output io.Writer
}

// DefaultConfig returns a sensible default configuration
func DefaultConfig() *Config {
	return &Config{
		Level:  LevelInfo,
		Format: "text",
		Output: os.Stderr,
	}
}

// NewLogger creates a new structured logger
func NewLogger(config *Config) *Logger {
	if config == nil {
		config = DefaultConfig()
	}

	var handler slog.Handler
	opts := &slog.HandlerOptions{
		Level:     slog.Level(config.Level),
		AddSource: config.Level <= LevelDebug,
	}

	switch config.Format {
	case "json":
		handler = slog.NewJSONHandler(config.Output, opts)
	default:
		handler = slog.NewTextHandler(config.Output, opts)
	}

	return &Logger{
		Logger: slog.New(handler),
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
		Logger:   l.With("device_id", deviceID),
		deviceID: &deviceID,
	}
}

// WithQueue returns a logger with queue context
func (l *Logger) WithQueue(queueID int) *Logger {
	return &Logger{
		Logger:   l.With("queue_id", queueID),
		deviceID: l.deviceID,
	}
}

// WithRequest returns a logger with request context
func (l *Logger) WithRequest(tag uint16, opType string) *Logger {
	return &Logger{
		Logger:   l.With("tag", tag, "op", opType),
		deviceID: l.deviceID,
	}
}

// WithError returns a logger with error context
func (l *Logger) WithError(err error) *Logger {
	return &Logger{
		Logger:   l.With("error", err),
		deviceID: l.deviceID,
	}
}

// Control plane logging methods
func (l *Logger) ControlStart(operation string) {
	l.Info("control operation starting", "operation", operation)
}

func (l *Logger) ControlSuccess(operation string) {
	l.Info("control operation succeeded", "operation", operation)
}

func (l *Logger) ControlError(operation string, err error) {
	l.Error("control operation failed", "operation", operation, "error", err)
}

// Data plane logging methods
func (l *Logger) IOStart(op string, offset int64, length int) {
	l.Debug("I/O operation starting", "op", op, "offset", offset, "length", length)
}

func (l *Logger) IOComplete(op string, offset int64, length int, latency_us int64) {
	l.Debug("I/O operation completed", "op", op, "offset", offset, "length", length, "latency_us", latency_us)
}

func (l *Logger) IOError(op string, offset int64, length int, err error) {
	l.Error("I/O operation failed", "op", op, "offset", offset, "length", length, "error", err)
}

// Ring operations
func (l *Logger) RingSubmit(entries int) {
	l.Debug("submitting ring entries", "entries", entries)
}

func (l *Logger) RingComplete(completions int) {
	l.Debug("processed ring completions", "completions", completions)
}

// Memory management
func (l *Logger) MemoryMap(size int, offset int64) {
	l.Debug("memory mapped", "size", size, "offset", offset)
}

func (l *Logger) MemoryUnmap(size int) {
	l.Debug("memory unmapped", "size", size)
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
