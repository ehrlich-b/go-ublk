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
//
// These delays account for kernel and udev processing latency during device setup.
// The ublk protocol requires strict ordering:
//   1. ADD_DEV creates device in kernel (udev creates /dev/ublkc*)
//   2. Queue threads open char device and submit FETCH_REQs
//   3. START_DEV transitions to LIVE state (kernel waits for FETCH_REQs)
//   4. Block device /dev/ublkb* becomes available
//
// Without proper delays, START_DEV may hang waiting for FETCH_REQs that haven't
// propagated through io_uring, or the block device may not be visible yet.
const (
	// DeviceStartupDelay is the initial wait after START_DEV before polling.
	// The kernel needs time to transition device state from INIT to LIVE,
	// and udev needs time to create the block device node (/dev/ublkb*).
	// 500ms provides margin for slow systems; most complete in <100ms.
	DeviceStartupDelay = 500 * time.Millisecond

	// DevicePollingInterval is how often to check if /dev/ublkb* exists.
	// 10ms balances responsiveness with CPU overhead during the ~100ms
	// window between START_DEV and device visibility.
	DevicePollingInterval = 10 * time.Millisecond

	// QueueInitDelay is the wait after submitting initial FETCH_REQs.
	// START_DEV blocks until the kernel sees FETCH_REQs in each queue's
	// completion queue. This delay ensures io_uring submissions are visible
	// to the kernel before we call START_DEV. Empirically, 100ms is
	// sufficient; shorter delays risk START_DEV timeout on loaded systems.
	QueueInitDelay = 100 * time.Millisecond
)

// Memory allocation constants
const (
	// IOBufferSizePerTag is the I/O buffer size allocated per queue tag (64KB)
	IOBufferSizePerTag = 64 * 1024
)