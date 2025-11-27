package logging

import (
	"bytes"
	"strings"
	"testing"
)

func TestNewLogger(t *testing.T) {
	tests := []struct {
		name   string
		config *Config
	}{
		{
			name:   "default config",
			config: nil,
		},
		{
			name: "custom level",
			config: &Config{
				Level:  LevelDebug,
				Output: &bytes.Buffer{},
			},
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

func TestLogLevels(t *testing.T) {
	var buf bytes.Buffer
	config := &Config{
		Level:  LevelDebug,
		Output: &buf,
	}

	logger := NewLogger(config)

	// Test debug message
	logger.Debug("debug message", "key", "value")
	output := buf.String()
	if !strings.Contains(output, "debug message") {
		t.Errorf("Expected debug message, got: %s", output)
	}
	if !strings.Contains(output, "key=value") {
		t.Errorf("Expected key=value, got: %s", output)
	}

	// Test info message
	buf.Reset()
	logger.Info("info message")
	output = buf.String()
	if !strings.Contains(output, "info message") {
		t.Errorf("Expected info message, got: %s", output)
	}

	// Test warn message
	buf.Reset()
	logger.Warn("warning message")
	output = buf.String()
	if !strings.Contains(output, "warning message") {
		t.Errorf("Expected warning message, got: %s", output)
	}

	// Test error message
	buf.Reset()
	logger.Error("error message")
	output = buf.String()
	if !strings.Contains(output, "error message") {
		t.Errorf("Expected error message, got: %s", output)
	}
}

func TestLogLevelFiltering(t *testing.T) {
	var buf bytes.Buffer
	config := &Config{
		Level:  LevelWarn,
		Output: &buf,
	}

	logger := NewLogger(config)

	// Debug and Info should be filtered out
	logger.Debug("debug message")
	logger.Info("info message")
	if buf.Len() > 0 {
		t.Errorf("Expected no output for debug/info at warn level, got: %s", buf.String())
	}

	// Warn and Error should appear
	logger.Warn("warning message")
	if !strings.Contains(buf.String(), "warning message") {
		t.Errorf("Expected warning message, got: %s", buf.String())
	}

	buf.Reset()
	logger.Error("error message")
	if !strings.Contains(buf.String(), "error message") {
		t.Errorf("Expected error message, got: %s", buf.String())
	}
}

func TestGlobalLoggerFunctions(t *testing.T) {
	var buf bytes.Buffer
	config := &Config{
		Level:  LevelDebug,
		Output: &buf,
	}

	SetDefault(NewLogger(config))

	Debug("debug message", "key", "value")
	output := buf.String()
	if !strings.Contains(output, "debug message") {
		t.Errorf("Expected debug message, got: %s", output)
	}

	buf.Reset()
	Info("info message")
	output = buf.String()
	if !strings.Contains(output, "info message") {
		t.Errorf("Expected info message, got: %s", output)
	}

	buf.Reset()
	Warn("warning message")
	output = buf.String()
	if !strings.Contains(output, "warning message") {
		t.Errorf("Expected warning message, got: %s", output)
	}

	buf.Reset()
	Error("error message")
	output = buf.String()
	if !strings.Contains(output, "error message") {
		t.Errorf("Expected error message, got: %s", output)
	}
}
