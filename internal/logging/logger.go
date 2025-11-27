// Package logging provides simple logging for the go-ublk project
package logging

import (
	"fmt"
	"io"
	"log"
	"os"
	"sync"
)

// Logger wraps stdlib log with level support
type Logger struct {
	logger *log.Logger
	level  LogLevel
	mu     sync.Mutex
}

var (
	defaultLogger *Logger
	mu            sync.RWMutex
)

// LogLevel represents the available log levels
type LogLevel int

const (
	LevelDebug LogLevel = iota
	LevelInfo
	LevelWarn
	LevelError
)

// Config holds logging configuration
type Config struct {
	Level  LogLevel
	Output io.Writer
}

// DefaultConfig returns a sensible default configuration
func DefaultConfig() *Config {
	return &Config{
		Level:  LevelInfo,
		Output: os.Stderr,
	}
}

// NewLogger creates a new logger
func NewLogger(config *Config) *Logger {
	if config == nil {
		config = DefaultConfig()
	}
	output := config.Output
	if output == nil {
		output = os.Stderr
	}
	return &Logger{
		logger: log.New(output, "", log.LstdFlags),
		level:  config.Level,
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

// formatArgs converts key-value pairs to a string
func formatArgs(args []any) string {
	if len(args) == 0 {
		return ""
	}
	var result string
	for i := 0; i < len(args); i += 2 {
		if i+1 < len(args) {
			if result != "" {
				result += " "
			}
			result += fmt.Sprintf("%v=%v", args[i], args[i+1])
		}
	}
	if result != "" {
		return " " + result
	}
	return ""
}

func (l *Logger) log(level LogLevel, prefix, msg string, args ...any) {
	if level < l.level {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	l.logger.Printf("%s %s%s", prefix, msg, formatArgs(args))
}

func (l *Logger) Debug(msg string, args ...any) {
	l.log(LevelDebug, "[DEBUG]", msg, args...)
}

func (l *Logger) Info(msg string, args ...any) {
	l.log(LevelInfo, "[INFO]", msg, args...)
}

func (l *Logger) Warn(msg string, args ...any) {
	l.log(LevelWarn, "[WARN]", msg, args...)
}

func (l *Logger) Error(msg string, args ...any) {
	l.log(LevelError, "[ERROR]", msg, args...)
}

// Printf-style logging
func (l *Logger) Debugf(format string, args ...any) {
	l.log(LevelDebug, "[DEBUG]", fmt.Sprintf(format, args...))
}

func (l *Logger) Infof(format string, args ...any) {
	l.log(LevelInfo, "[INFO]", fmt.Sprintf(format, args...))
}

func (l *Logger) Warnf(format string, args ...any) {
	l.log(LevelWarn, "[WARN]", fmt.Sprintf(format, args...))
}

func (l *Logger) Errorf(format string, args ...any) {
	l.log(LevelError, "[ERROR]", fmt.Sprintf(format, args...))
}

// Printf for compatibility
func (l *Logger) Printf(format string, args ...any) {
	l.Infof(format, args...)
}

// Global convenience functions
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
