# STYLE.md - Go Code Style Guide

This document defines the coding standards for go-ublk. Follow these conventions for all new code and when refactoring existing code.

## Package Structure

```
go-ublk/
├── *.go              # Public API (ublk package)
├── backend/          # Backend implementations
├── cmd/              # CLI applications
│   └── ublk-mem/     # Memory-backed ublk device
└── internal/         # Private implementation
    ├── constants/    # Shared constants
    ├── ctrl/         # Control plane (device lifecycle)
    ├── interfaces/   # Internal interface definitions
    ├── logging/      # Async zerolog wrapper
    ├── queue/        # Data plane (I/O processing)
    ├── uapi/         # Kernel UAPI structs
    └── uring/        # io_uring implementation
```

## Naming Conventions

### Packages
- All lowercase, single word preferred: `ctrl`, `queue`, `uring`, `uapi`
- No underscores, no mixed case

### Types
- **Exported**: `PascalCase` - `DeviceParams`, `Backend`, `Runner`
- **Unexported**: `camelCase` - `tagState`, `sqe128`, `cqe32`
- **Interfaces**: Name for behavior, not "I" prefix: `Backend`, `Ring`, `Result`

### Functions
- **Exported**: `PascalCase` - `CreateAndServe`, `NewRunner`
- **Unexported**: `camelCase` - `submitAndWait`, `processRequests`
- **Constructors**: `New` prefix - `NewRunner`, `NewRing`

### Constants
- **Exported**: `PascalCase` - `IOBufferSizePerTag`
- **Unexported**: `camelCase` or `SCREAMING_SNAKE` for kernel constants
- **Kernel UAPI constants**: Match kernel naming: `UBLK_CMD_ADD_DEV`, `IORING_SETUP_SQE128`

### Variables
- **Package-level**: `camelCase` - avoid when possible
- **Local**: Short, contextual - `r` for ring, `err` for error, `ctx` for context
- **Loop indices**: `i`, `j`, `k` or descriptive: `tag`, `index`

## Error Handling

### Return errors, don't panic
```go
// Good
func NewRunner(ctx context.Context, config Config) (*Runner, error) {
    if config.Depth <= 0 {
        return nil, fmt.Errorf("invalid depth: %d", config.Depth)
    }
    // ...
}

// Bad
func NewRunner(ctx context.Context, config Config) *Runner {
    if config.Depth <= 0 {
        panic("invalid depth")
    }
    // ...
}
```

### Wrap errors with context
```go
// Good
if err := r.submitInitialFetchReq(tag); err != nil {
    return fmt.Errorf("submit initial FETCH_REQ[%d]: %w", tag, err)
}

// Bad
if err := r.submitInitialFetchReq(tag); err != nil {
    return err
}
```

### Use sentinel errors for expected conditions
```go
var ErrDeviceNotReady = errors.New("device not ready")

// Caller can check: errors.Is(err, ErrDeviceNotReady)
```

## Logging

### Use the internal/logging package
```go
import "github.com/ehrlich-b/go-ublk/internal/logging"

logger := logging.Default()
logger.Debug("operation details", "key", value)
logger.Info("significant event", "device_id", devID)
logger.Error("failure", "error", err)
```

### Log levels
- **Debug**: Internal state, useful for development
- **Info**: Significant lifecycle events (device created, started, stopped)
- **Warn**: Recoverable issues, degraded operation
- **Error**: Failures that affect operation

### Never log in hot paths without guards
```go
// Good - logger check prevents allocation
if r.logger != nil {
    r.logger.Debugf("processing tag %d", tag)
}

// Bad - always allocates even when logging disabled
r.logger.Debugf("processing tag %d", tag)
```

## Comments

### Package comments
Every package needs a doc comment in one file (usually the primary file):
```go
// Package uring provides io_uring operations for ublk control and data planes.
package uring
```

### Type comments
Public types need doc comments:
```go
// Runner handles I/O for a single ublk queue.
// It manages the io_uring instance and per-tag state machine.
type Runner struct {
    // ...
}
```

### Function comments
Public functions need doc comments:
```go
// CreateAndServe creates a ublk device and begins serving I/O requests.
// It blocks until the context is cancelled or an error occurs.
func CreateAndServe(ctx context.Context, params DeviceParams, opts ...Option) (*Device, error)
```

### Implementation comments
Explain "why", not "what":
```go
// CRITICAL: Store fence before tail update ensures SQE is visible to kernel.
// Without this, the kernel may see the updated tail before the SQE data.
Sfence()
atomic.StoreUint32(sqTail, newTail)
```

### No AI-generated filler comments
```go
// Bad - obvious
// increment the counter
counter++

// Good - explains non-obvious behavior
// Counter must be incremented before submission to reserve the slot
counter++
```

## Formatting

### Use gofmt/goimports
All code must pass `gofmt`. Use `goimports` for import organization.

### Import grouping
```go
import (
    // Standard library
    "context"
    "fmt"
    "syscall"

    // External dependencies
    "golang.org/x/sys/unix"

    // Internal packages
    "github.com/ehrlich-b/go-ublk/internal/logging"
    "github.com/ehrlich-b/go-ublk/internal/uapi"
)
```

### Line length
Prefer lines under 100 characters. Break long lines at logical points.

## Unsafe Code

### Document all unsafe usage
```go
// Safety: bufPtr is a valid pointer obtained from mmap, and offset
// is within bounds (checked by tag validation above).
buffer := (*[IOBufferSizePerTag]byte)(unsafe.Pointer(bufPtr + uintptr(offset)))[:length:length]
```

### Minimize scope
Keep unsafe operations as small and isolated as possible:
```go
// Good - unsafe is isolated
func loadDescriptor(ptr uintptr, tag uint16) Descriptor {
    base := unsafe.Add(unsafe.Pointer(ptr), uintptr(tag)*descSize)
    return Descriptor{
        OpFlags: atomic.LoadUint32((*uint32)(base)),
        // ...
    }
}

// Bad - unsafe spreads through function
func processTag(tag uint16) {
    base := unsafe.Add(unsafe.Pointer(r.descPtr), uintptr(tag)*descSize)
    // ... many lines of code using unsafe.Pointer ...
}
```

## Testing

### Test files
- Unit tests: `*_test.go` alongside source
- Integration tests: `test/integration/`
- Benchmarks: Include in `*_test.go` files

### Test naming
```go
func TestRunner_processRequests(t *testing.T) { }
func TestRunner_processRequests_emptyCompletion(t *testing.T) { }
func BenchmarkRunner_processRequests(b *testing.B) { }
```

### Table-driven tests preferred
```go
func TestTagState_transitions(t *testing.T) {
    tests := []struct {
        name     string
        initial  TagState
        event    string
        expected TagState
    }{
        {"fetch_completes", TagStateInFlightFetch, "completion", TagStateOwned},
        // ...
    }
    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            // ...
        })
    }
}
```

## Memory & Performance

### Avoid allocations in hot paths
```go
// Good - reuse buffer
buffer := r.buffers[tag][:length]

// Bad - allocate per request
buffer := make([]byte, length)
```

### Use sync.Pool for temporary allocations
```go
var resultPool = sync.Pool{
    New: func() interface{} {
        return &minimalResult{}
    },
}
```

### Atomic operations for shared state
```go
// Good
currentTail := atomic.LoadUint32(cqTail)
atomic.StoreUint32(cqHead, newHead)

// Bad
currentTail := *cqTail
*cqHead = newHead
```

## Concurrency

### Document goroutine ownership
```go
// ioLoop runs in a dedicated goroutine, pinned to an OS thread.
// It owns the io_uring instance and all tag state for this queue.
func (r *Runner) ioLoop(started chan<- error) {
    runtime.LockOSThread()
    defer runtime.UnlockOSThread()
    // ...
}
```

### Use context for cancellation
```go
for {
    select {
    case <-ctx.Done():
        return ctx.Err()
    default:
        if err := r.processRequests(); err != nil {
            return err
        }
    }
}
```

### Prefer channels over shared memory
```go
// Good
started := make(chan error, 1)
go r.ioLoop(started)
if err := <-started; err != nil {
    return err
}
```

## Build Tags

### Platform-specific code
```go
//go:build linux && cgo

package uring
```

### Stub implementations for non-Linux
```go
//go:build !linux || !cgo

package uring

func Sfence() {} // no-op on non-Linux
```
