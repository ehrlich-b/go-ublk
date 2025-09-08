package interfaces

// Backend defines the interface that all ublk backends must implement.
// This interface is intentionally similar to standard Go interfaces like
// io.ReaderAt and io.WriterAt for familiarity and composability.
type Backend interface {
	// ReadAt reads len(p) bytes into p starting at offset off.
	// It returns the number of bytes read (0 <= n <= len(p)) and any error encountered.
	// When ReadAt returns n < len(p), it returns a non-nil error explaining
	// why more bytes were not returned.
	//
	// If the n = len(p) bytes returned by ReadAt are at the end of the input source,
	// ReadAt may return either err == nil or err == io.EOF.
	//
	// Implementations must not retain p.
	ReadAt(p []byte, off int64) (n int, err error)

	// WriteAt writes len(p) bytes from p to the underlying data stream at offset off.
	// It returns the number of bytes written from p (0 <= n <= len(p)) and
	// any error encountered that caused the write to stop early.
	// WriteAt must return a non-nil error if it returns n < len(p).
	//
	// If WriteAt is writing to a destination with a seek offset,
	// WriteAt should not affect nor be affected by the underlying seek offset.
	//
	// Implementations must not retain p.
	WriteAt(p []byte, off int64) (n int, err error)

	// Size returns the size of the backend in bytes.
	// This determines the size of the block device as seen by the kernel.
	Size() int64

	// Close closes the backend and releases any resources.
	// After Close is called, no other methods should be called.
	Close() error

	// Flush flushes any cached writes to stable storage.
	// This is called when the block layer issues a flush/fsync request.
	Flush() error
}

// DiscardBackend is an optional interface that backends can implement
// to support TRIM/DISCARD operations efficiently.
type DiscardBackend interface {
	Backend

	// Discard discards the data in the given range, making it available for reuse.
	// The backend may choose to actually deallocate the space or simply mark it as unused.
	// offset and length are in bytes.
	Discard(offset, length int64) error
}

// WriteZeroesBackend is an optional interface for efficient zero-writing.
type WriteZeroesBackend interface {
	Backend

	// WriteZeroes efficiently writes zeros to the given range.
	// This is more efficient than WriteAt with a zero-filled buffer.
	// offset and length are in bytes.
	WriteZeroes(offset, length int64) error
}

// SyncBackend is an optional interface for fine-grained sync control.
type SyncBackend interface {
	Backend

	// Sync synchronizes the backend state to stable storage.
	// This is different from Flush in that it may also sync metadata.
	Sync() error

	// SyncRange synchronizes only the specified range to stable storage.
	// This can be more efficient than syncing the entire backend.
	SyncRange(offset, length int64) error
}

// StatBackend is an optional interface that provides device statistics.
type StatBackend interface {
	Backend

	// Stats returns backend-specific statistics.
	// The returned map contains string keys with numeric values.
	Stats() map[string]interface{}
}

// ResizeBackend is an optional interface for backends that support resizing.
type ResizeBackend interface {
	Backend

	// Resize changes the size of the backend.
	// The new size must be greater than or equal to 0.
	// If the new size is smaller, data may be truncated.
	// If the new size is larger, the new space should read as zeros.
	Resize(newSize int64) error
}