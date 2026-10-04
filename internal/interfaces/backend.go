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

// WriteZeroesBackend is an optional interface for zeroing a range without
// transferring a buffer of zeros.
type WriteZeroesBackend interface {
	Backend
	WriteZeroes(offset, length int64) error
}

// FUABackend is an optional interface for backends that can make a single
// write durable before returning (Force Unit Access) without flushing
// everything else. Its presence lets a device advertise FUA.
type FUABackend interface {
	Backend
	WriteAtFUA(p []byte, off int64) (n int, err error)
}

// IntegrityBackend stores per-block integrity metadata alongside the data.
type IntegrityBackend interface {
	Backend
	ReadIntegrity(meta []byte, off int64) error
	WriteIntegrity(meta []byte, off int64) error
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
