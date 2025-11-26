package constants

import "time"

// Default configuration constants
const (
	// DefaultQueueDepth is the default I/O queue depth per queue.
	// 128 provides good throughput for most workloads while keeping memory
	// overhead reasonable (128 tags * 64KB buffers = 8MB per queue).
	DefaultQueueDepth = 128

	// DefaultLogicalBlockSize is the default logical block size in bytes.
	// 512 bytes matches standard disk sector size and is universally supported
	// by Linux block layer and all filesystems.
	DefaultLogicalBlockSize = 512

	// DefaultMaxIOSize is the default maximum I/O size in bytes (1MB).
	// 1MB is the Linux kernel's default max_sectors_kb for most devices,
	// providing good throughput without excessive memory copy overhead.
	DefaultMaxIOSize = 1 << 20

	// DefaultDiscardAlignment is the default discard alignment in bytes.
	// 4096 bytes (4KB) aligns with modern disk physical sector size and
	// filesystem block size, ensuring efficient discard operations.
	DefaultDiscardAlignment = 4096

	// DefaultDiscardGranularity is the default discard granularity in bytes.
	// 4096 bytes (4KB) matches filesystem block size, ensuring discards
	// don't split filesystem blocks and cause fragmentation.
	DefaultDiscardGranularity = 4096

	// DefaultMaxDiscardSectors is the default maximum sectors per discard.
	// 0xffffffff (max uint32) indicates unlimited discard size, allowing
	// the kernel to send large discard requests for better efficiency.
	DefaultMaxDiscardSectors = 0xffffffff

	// DefaultMaxDiscardSegments is the default maximum segments per discard.
	// 256 segments balances supporting scattered discards with kernel
	// memory overhead for tracking discard bio segments.
	DefaultMaxDiscardSegments = 256

	// AutoAssignDeviceID is passed to ADD_DEV to let the kernel auto-assign
	// a device ID. This is the kernel's API contract (-1 means auto-assign).
	AutoAssignDeviceID = -1
)

// Timing constants for device lifecycle
//
// These delays account for kernel and udev processing latency during device setup.
// The ublk protocol requires strict ordering:
//  1. ADD_DEV creates device in kernel (udev creates /dev/ublkc*)
//  2. Queue threads open char device and submit FETCH_REQs
//  3. START_DEV transitions to LIVE state (kernel waits for FETCH_REQs)
//  4. Block device /dev/ublkb* becomes available
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

	// CharDeviceOpenRetries is the number of times to retry opening the
	// character device before giving up. With a 100ms sleep between retries,
	// 50 retries = 5 seconds total timeout, which accounts for slow udev
	// processing on heavily loaded systems.
	CharDeviceOpenRetries = 50
)

// Memory allocation constants
const (
	// IOBufferSizePerTag is the I/O buffer size allocated per queue tag (64KB)
	IOBufferSizePerTag = 64 * 1024
)
