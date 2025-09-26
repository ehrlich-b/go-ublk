package constants

import "time"

// Default configuration constants
const (
	// DefaultQueueDepth is the default I/O queue depth per queue
	DefaultQueueDepth = 128

	// DefaultLogicalBlockSize is the default logical block size in bytes
	DefaultLogicalBlockSize = 512

	// DefaultMaxIOSize is the default maximum I/O size in bytes (1MB)
	DefaultMaxIOSize = 1 << 20

	// DefaultDiscardAlignment is the default discard alignment in bytes
	DefaultDiscardAlignment = 4096

	// DefaultDiscardGranularity is the default discard granularity in bytes
	DefaultDiscardGranularity = 4096

	// DefaultMaxDiscardSectors is the default maximum sectors per discard
	DefaultMaxDiscardSectors = 0xffffffff

	// DefaultMaxDiscardSegments is the default maximum segments per discard
	DefaultMaxDiscardSegments = 256

	// AutoAssignDeviceID indicates the kernel should auto-assign a device ID
	AutoAssignDeviceID = -1
)

// Timing constants for device lifecycle
const (
	// DeviceStartupDelay is the time to wait for kernel to process START_DEV
	DeviceStartupDelay = 500 * time.Millisecond

	// DevicePollingInterval is the interval to check for device readiness
	DevicePollingInterval = 10 * time.Millisecond

	// QueueInitDelay is the time to wait after submitting FETCH_REQs
	QueueInitDelay = 100 * time.Millisecond
)

// Memory allocation constants
const (
	// IOBufferSizePerTag is the I/O buffer size allocated per queue tag (64KB)
	IOBufferSizePerTag = 64 * 1024
)