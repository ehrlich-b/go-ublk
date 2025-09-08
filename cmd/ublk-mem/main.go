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

	"github.com/ehrlich-b/go-ublk"
	"github.com/ehrlich-b/go-ublk/backend"
)

func main() {
	var (
		sizeStr = flag.String("size", "64M", "Size of the memory disk (e.g., 64M, 1G)")
		verbose = flag.Bool("v", false, "Verbose output")
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
	params.QueueDepth = 32
	params.NumQueues = 1

	// Create logger if verbose
	var logger ublk.Logger
	if *verbose {
		logger = &simpleLogger{}
	}

	// Create options
	options := &ublk.Options{
		Logger: logger,
	}

	fmt.Printf("Creating %s memory disk...\n", formatSize(size))
	
	// Create and serve the device
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	device, err := ublk.CreateAndServe(ctx, params, options)
	if err != nil {
		log.Fatalf("Failed to create device: %v", err)
	}
	defer func() {
		fmt.Printf("Stopping device...\n")
		if err := ublk.StopAndDelete(ctx, device); err != nil {
			log.Printf("Error stopping device: %v", err)
		} else {
			fmt.Printf("Device stopped successfully\n")
		}
	}()

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

	fmt.Printf("\nReceived signal, shutting down...\n")
}

// simpleLogger implements the Logger interface
type simpleLogger struct{}

func (l *simpleLogger) Printf(format string, args ...interface{}) {
	log.Printf(format, args...)
}

func (l *simpleLogger) Debugf(format string, args ...interface{}) {
	log.Printf("[DEBUG] "+format, args...)
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