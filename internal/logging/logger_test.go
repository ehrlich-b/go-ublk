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
		Level:  LevelDebug,
		Format: "text",
		Output: &buf,
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
		Level:  LevelDebug,
		Format: "text",
		Output: &buf,
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
		Level:  LevelDebug,
		Format: "text",
		Output: &buf,
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

func TestControlPlaneLogging(t *testing.T) {
	var buf bytes.Buffer
	config := &Config{
		Level:  LevelInfo,
		Format: "text",
		Output: &buf,
	}
	
	logger := NewLogger(config)
	
	// Test control start
	logger.ControlStart("ADD_DEV")
	output := buf.String()
	if !strings.Contains(output, "control operation starting") {
		t.Errorf("Expected control start message, got: %s", output)
	}
	if !strings.Contains(output, "operation=ADD_DEV") {
		t.Errorf("Expected operation=ADD_DEV, got: %s", output)
	}
	
	// Test control success
	buf.Reset()
	logger.ControlSuccess("ADD_DEV")
	output = buf.String()
	if !strings.Contains(output, "control operation succeeded") {
		t.Errorf("Expected control success message, got: %s", output)
	}
	
	// Test control error
	buf.Reset()
	testErr := errors.New("device exists")
	logger.ControlError("ADD_DEV", testErr)
	output = buf.String()
	if !strings.Contains(output, "control operation failed") {
		t.Errorf("Expected control error message, got: %s", output)
	}
	if !strings.Contains(output, "device exists") {
		t.Errorf("Expected error text, got: %s", output)
	}
}

func TestIOLogging(t *testing.T) {
	var buf bytes.Buffer
	config := &Config{
		Level:  LevelDebug,
		Format: "text",
		Output: &buf,
	}
	
	logger := NewLogger(config)
	
	// Test I/O start
	logger.IOStart("READ", 4096, 512)
	output := buf.String()
	if !strings.Contains(output, "I/O operation starting") {
		t.Errorf("Expected I/O start message, got: %s", output)
	}
	if !strings.Contains(output, "op=READ") {
		t.Errorf("Expected op=READ, got: %s", output)
	}
	if !strings.Contains(output, "offset=4096") {
		t.Errorf("Expected offset=4096, got: %s", output)
	}
	if !strings.Contains(output, "length=512") {
		t.Errorf("Expected length=512, got: %s", output)
	}
	
	// Test I/O complete
	buf.Reset()
	logger.IOComplete("READ", 4096, 512, 150)
	output = buf.String()
	if !strings.Contains(output, "I/O operation completed") {
		t.Errorf("Expected I/O complete message, got: %s", output)
	}
	if !strings.Contains(output, "latency_us=150") {
		t.Errorf("Expected latency_us=150, got: %s", output)
	}
	
	// Test I/O error
	buf.Reset()
	testErr := errors.New("read failed")
	logger.IOError("READ", 4096, 512, testErr)
	output = buf.String()
	if !strings.Contains(output, "I/O operation failed") {
		t.Errorf("Expected I/O error message, got: %s", output)
	}
	if !strings.Contains(output, "read failed") {
		t.Errorf("Expected error text, got: %s", output)
	}
}

func TestGlobalLoggerFunctions(t *testing.T) {
	var buf bytes.Buffer
	config := &Config{
		Level:  LevelDebug,
		Format: "text",
		Output: &buf,
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