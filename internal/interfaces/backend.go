// Package interfaces provides internal interface definitions for go-ublk.
// These are separate from the public interfaces to avoid circular imports
// between the main package and internal packages.
package interfaces

// Backend defines the interface that all ublk backends must implement.
type Backend interface {
	ReadAt(p []byte, off int64) (n int, err error)
	WriteAt(p []byte, off int64) (n int, err error)
	Size() int64
	Close() error
	Flush() error
}

// DiscardBackend is an optional interface for TRIM/DISCARD support.
type DiscardBackend interface {
	Backend
	Discard(offset, length int64) error
}

// Logger interface for optional logging.
type Logger interface {
	Printf(format string, args ...interface{})
	Debugf(format string, args ...interface{})
}

// Observer interface for metrics collection.
// Implementations must be thread-safe as methods are called from the I/O loop.
type Observer interface {
	ObserveRead(bytes uint64, latencyNs uint64, success bool)
	ObserveWrite(bytes uint64, latencyNs uint64, success bool)
	ObserveDiscard(bytes uint64, latencyNs uint64, success bool)
	ObserveFlush(latencyNs uint64, success bool)
	ObserveQueueDepth(depth uint32)
}
