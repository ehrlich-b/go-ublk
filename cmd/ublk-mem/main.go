package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/ehrlich-b/go-ublk"
	"github.com/ehrlich-b/go-ublk/backend"
	"github.com/ehrlich-b/go-ublk/internal/logging"
)

func main() {
	var (
		sizeStr = flag.String("size", "64M", "Size of the memory disk (e.g., 64M, 1G)")
		verbose = flag.Bool("v", false, "Verbose output")
		minimal = flag.Bool("minimal", false, "Use minimal resource parameters for debugging")
	)
	flag.Parse()

	// Parse size
	size, err := parseSize(*sizeStr)
	if err != nil {
		log.Fatalf("Invalid size '%s': %v", *sizeStr, err)
	}

	// Create memory backend
	memBackend := backend.NewMemory(size)
	defer memBackend.Close()

	// Create device parameters
	params := ublk.DefaultParams(memBackend)
	if *minimal {
		// Use minimal parameters for testing
		params.QueueDepth = 1                     // Absolute minimum
		params.NumQueues = 1                      // Single queue
		params.MaxIOSize = ublk.IOBufferSizePerTag // Match buffer size
	} else {
		params.QueueDepth = 32
		params.NumQueues = 1
		params.MaxIOSize = ublk.IOBufferSizePerTag // Match buffer size
	}

	// Critical for kernel 6.11+: use ioctl-encoded control commands
	// This sets UBLK_F_CMD_IOCTL_ENCODE in the feature flags sent at ADD_DEV.
	params.EnableIoctlEncode = true

	// Set up logging
	logConfig := logging.DefaultConfig()
	if *verbose {
		logConfig.Level = logging.LevelDebug
	}
	logger := logging.NewLogger(logConfig)
	logging.SetDefault(logger)

	// Create options
	options := &ublk.Options{}

	if *minimal {
		logger.Info("using minimal queue depth for faster initialization", "depth", params.QueueDepth)
	}
	logger.Info("creating memory disk", "size", formatSize(size), "size_bytes", size)

	// Create and serve the device
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	device, err := ublk.CreateAndServe(ctx, params, options)
	if err != nil {
		logger.Error("failed to create device", "error", err)
		os.Exit(1)
	}
	defer func() {
		logger.Info("stopping device")
		if err := ublk.StopAndDelete(ctx, device); err != nil {
			logger.Error("error stopping device", "error", err)
		} else {
			logger.Info("device stopped successfully")
		}
	}()

	logger.Info("device created successfully",
		"block_device", device.Path,
		"char_device", device.CharPath,
		"size", formatSize(size),
		"size_bytes", size)

	fmt.Printf("Device created: %s\n", device.Path)
	fmt.Printf("Character device: %s\n", device.CharPath)
	fmt.Printf("Size: %s (%d bytes)\n", formatSize(size), size)
	fmt.Printf("\nYou can now use the device:\n")
	fmt.Printf("  sudo mkfs.ext4 %s\n", device.Path)
	fmt.Printf("  sudo mkdir -p /mnt/ublk\n")
	fmt.Printf("  sudo mount %s /mnt/ublk\n", device.Path)
	fmt.Printf("\nPress Ctrl+C to stop...\n")

	// Wait for signal
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	<-sigCh

	logger.Info("received shutdown signal")

	// Cancel the context to signal all goroutines to stop
	cancel()

	// Try cleanup with a timeout
	cleanupDone := make(chan bool)
	go func() {
		if err := ublk.StopAndDelete(context.Background(), device); err != nil {
			logger.Error("error stopping device", "error", err)
		} else {
			logger.Info("device stopped successfully")
		}
		cleanupDone <- true
	}()

	select {
	case <-cleanupDone:
		// Cleanup completed
	case <-time.After(1 * time.Second):
		// Cleanup taking too long, exit anyway
		logger.Info("cleanup timeout, forcing exit")
	}

	os.Exit(0)
}

// parseSize parses a size string like "64M", "1G", "512K"
func parseSize(s string) (int64, error) {
	s = strings.ToUpper(s)

	var multiplier int64 = 1
	var numStr string

	if strings.HasSuffix(s, "K") {
		multiplier = 1024
		numStr = strings.TrimSuffix(s, "K")
	} else if strings.HasSuffix(s, "M") {
		multiplier = 1024 * 1024
		numStr = strings.TrimSuffix(s, "M")
	} else if strings.HasSuffix(s, "G") {
		multiplier = 1024 * 1024 * 1024
		numStr = strings.TrimSuffix(s, "G")
	} else {
		numStr = s
	}

	num, err := strconv.ParseInt(numStr, 10, 64)
	if err != nil {
		return 0, err
	}

	return num * multiplier, nil
}

// formatSize formats a byte count as a human-readable string
func formatSize(bytes int64) string {
	const unit = 1024
	if bytes < unit {
		return fmt.Sprintf("%d B", bytes)
	}

	div, exp := int64(unit), 0
	for n := bytes / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}

	units := []string{"K", "M", "G", "T"}
	return fmt.Sprintf("%.1f %sB", float64(bytes)/float64(div), units[exp])
}
