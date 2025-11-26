package logging

import (
	"bytes"
	"errors"
	"strings"
	"testing"
)

func TestNewLogger(t *testing.T) {
	tests := []struct {
		name   string
		config *Config
		want   string
	}{
		{
			name:   "default config",
			config: nil,
			want:   "text",
		},
		{
			name: "json format",
			config: &Config{
				Level:  LevelInfo,
				Format: "json",
				Output: &bytes.Buffer{},
			},
			want: "json",
		},
		{
			name: "text format",
			config: &Config{
				Level:  LevelDebug,
				Format: "text",
				Output: &bytes.Buffer{},
			},
			want: "text",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			logger := NewLogger(tt.config)
			if logger == nil {
				t.Error("NewLogger() returned nil")
			}
		})
	}
}

func TestLoggerWithContext(t *testing.T) {
	var buf bytes.Buffer
	config := &Config{
		Level:   LevelDebug,
		Format:  "text",
		Output:  &buf,
		Sync:    true,
		NoColor: true,
	}

	logger := NewLogger(config)

	// Test device context
	deviceLogger := logger.WithDevice(42)
	deviceLogger.Info("test message")

	output := buf.String()
	if !strings.Contains(output, "device_id=42") {
		t.Errorf("Expected device_id=42 in output, got: %s", output)
	}

	// Test queue context
	buf.Reset()
	queueLogger := deviceLogger.WithQueue(1)
	queueLogger.Info("queue message")

	output = buf.String()
	if !strings.Contains(output, "device_id=42") {
		t.Errorf("Expected device_id=42 in queue logger output, got: %s", output)
	}
	if !strings.Contains(output, "queue_id=1") {
		t.Errorf("Expected queue_id=1 in output, got: %s", output)
	}
}

func TestLoggerWithRequest(t *testing.T) {
	var buf bytes.Buffer
	config := &Config{
		Level:   LevelDebug,
		Format:  "text",
		Output:  &buf,
		Sync:    true,
		NoColor: true,
	}

	logger := NewLogger(config)
	requestLogger := logger.WithRequest(123, "READ")
	requestLogger.Debug("processing request")

	output := buf.String()
	if !strings.Contains(output, "tag=123") {
		t.Errorf("Expected tag=123 in output, got: %s", output)
	}
	if !strings.Contains(output, "op=READ") {
		t.Errorf("Expected op=READ in output, got: %s", output)
	}
}

func TestLoggerWithError(t *testing.T) {
	var buf bytes.Buffer
	config := &Config{
		Level:   LevelDebug,
		Format:  "text",
		Output:  &buf,
		Sync:    true,
		NoColor: true,
	}

	logger := NewLogger(config)
	testErr := errors.New("test error")
	errorLogger := logger.WithError(testErr)
	errorLogger.Error("operation failed")

	output := buf.String()
	if !strings.Contains(output, "test error") {
		t.Errorf("Expected 'test error' in output, got: %s", output)
	}
}

func TestGlobalLoggerFunctions(t *testing.T) {
	var buf bytes.Buffer
	config := &Config{
		Level:   LevelDebug,
		Format:  "text",
		Output:  &buf,
		Sync:    true,
		NoColor: true,
	}

	SetDefault(NewLogger(config))

	// Test debug message (should appear since we set LevelDebug)
	Debug("debug message", "key", "value")
	output := buf.String()
	if !strings.Contains(output, "debug message") {
		t.Errorf("Expected debug message, got: %s", output)
	}
	if !strings.Contains(output, "key=value") {
		t.Errorf("Expected key=value, got: %s", output)
	}

	// Test info message
	buf.Reset()
	Info("info message")
	output = buf.String()
	if !strings.Contains(output, "info message") {
		t.Errorf("Expected info message, got: %s", output)
	}

	// Test warn message
	buf.Reset()
	Warn("warning message")
	output = buf.String()
	if !strings.Contains(output, "warning message") {
		t.Errorf("Expected warning message, got: %s", output)
	}

	// Test error message
	buf.Reset()
	Error("error message")
	output = buf.String()
	if !strings.Contains(output, "error message") {
		t.Errorf("Expected error message, got: %s", output)
	}
}
