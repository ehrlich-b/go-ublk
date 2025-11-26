# go-ublk Code Style Guide

## Base Style Guide

This project follows the [Uber Go Style Guide](https://github.com/uber-go/guide/blob/master/style.md) with the addendums below.

If Uber's guide conflicts with these addendums, **these rules take precedence**.

---

## Visual Consistency Rules

### Whitespace

**No extraneous whitespace anywhere.**

```go
// BAD - random blank lines
func foo() {
    x := 1

    y := 2

    return x + y
}

// BAD - trailing whitespace
func bar() {••
    return 42••
}••

// GOOD - clean and tight
func foo() {
    x := 1
    y := 2
    return x + y
}
```

**Specific rules:**
- No blank lines within a function unless separating logical blocks
- No blank lines between struct fields
- No trailing whitespace on any line
- Exactly one blank line between top-level declarations (functions, types, consts)
- No multiple consecutive blank lines

### Imports

Group imports in exactly 3 groups with one blank line between:

```go
import (
    // stdlib
    "context"
    "fmt"

    // external dependencies
    "github.com/some/package"

    // this project
    "github.com/ehrlich-b/go-ublk/internal/ctrl"
    "github.com/ehrlich-b/go-ublk/internal/uapi"
)
```

### Function Length

- Functions should fit on one screen (≤50 lines ideally)
- If longer, strongly consider extracting helpers
- Exception: generated code, test tables, large switch statements

### Line Length

- Soft limit: 100 characters
- Hard limit: 120 characters
- Exception: URLs in comments

### Comments

**Every exported symbol must have a godoc comment.**

```go
// Device represents a ublk block device.
type Device struct {
    // ...
}

// Create creates a new ublk device without starting I/O.
func Create(params DeviceParams, options *Options) (*Device, error) {
    // ...
}
```

**Internal implementation comments:**
- Use `//` for single-line comments
- Use `/* */` only for disabling code blocks (rare)
- No "decorative" comment boxes

```go
// BAD - decorative
/*********************
 * Some function
 *********************/

// GOOD - simple
// processRequest handles a single I/O request
func processRequest() {
}
```

### Error Messages

- Start with lowercase (no punctuation at end)
- Be specific about what failed
- Include context (device ID, queue number, etc.)

```go
// BAD
return fmt.Errorf("Error!")
return fmt.Errorf("Failed to create device.")

// GOOD
return fmt.Errorf("failed to create device %d: %w", id, err)
return fmt.Errorf("queue %d: invalid buffer size %d", qid, size)
```

### Variable Naming

**No abbreviations except common acronyms.**

```go
// BAD
devID := 5
charFd := open()
infoBuf := marshal()

// GOOD
deviceID := 5
charDeviceFd := open()
deviceInfoBytes := marshal()

// Acceptable acronyms
ID, URL, HTTP, IO, EOF, CPU, API, UAPI
```

**Exceptions:**
- Loop variables can be single letters: `i`, `j`, `k`
- `err` can be shadowed - it's treated as a reserved variable name

```go
// ALLOWED - err shadowing only
if err := foo(); err != nil {
    return err
}
if err := bar(); err != nil {  // OK to reuse 'err'
    return err
}

// FORBIDDEN - other variable shadowing
ctx := context.Background()
if condition {
    ctx := context.WithTimeout(ctx, 5*time.Second)  // NO - shadows ctx
}
```

### Constants

All caps with underscores for UAPI constants (kernel interface):

```go
const (
    UBLK_CMD_START_DEV = 0x01
    UBLK_IO_OP_READ    = 0x02
)
```

CamelCase for application constants:

```go
const (
    DefaultQueueDepth = 64
    MaxDevices        = 256
)
```

### Struct Field Ordering

**Logical grouping over alphabetical.**

Group related fields together, even if it breaks alphabetical order:

```go
type Device struct {
    // Exported
    ID       uint32
    Path     string
    Backend  Backend

    // Unexported - lifecycle grouped together
    ctx      context.Context
    cancel   context.CancelFunc
    started  bool
    closed   bool
}
```

### Error Handling

Never ignore errors. Use explicit `_ =` if intentionally discarding:

```go
// BAD
device.Close()

// GOOD - intentional
_ = device.Close() // Cleanup in defer, error already handled

// BEST - handle it
if err := device.Close(); err != nil {
    logger.Warn("cleanup failed", "error", err)
}
```

**Always wrap errors with `%w`:**

```go
// BAD
return fmt.Errorf("failed to create device: %v", err)

// GOOD
return fmt.Errorf("failed to create device: %w", err)
```

This enables `errors.Is()` and `errors.As()` throughout the call stack.

---

## Project-Specific Rules

### No Backwards Compatibility

**This is a new, unreleased project.**

- No deprecated functions
- No "kept for backwards compatibility" comments
- If something is wrong, fix it - don't work around it
- No version shims or compatibility layers

### Kernel Interface

Code touching the kernel interface (internal/uapi) should:
- Match kernel structure layout exactly
- Use compile-time size assertions
- Document which kernel version introduced each feature

```go
// Compile-time size check
var _ [32]byte = [unsafe.Sizeof(UblksrvCtrlCmd{})]byte{}
```

### Magic Numbers

**Every magic number needs a comment explaining WHY that value.**

```go
// BAD
time.Sleep(100 * time.Millisecond)
const maxRetries = 5

// GOOD
// Wait 100ms for udev to create device nodes
time.Sleep(100 * time.Millisecond)

// Retry up to 5 times to handle transient EINTR from signals
const maxRetries = 5
```

### Testing

- Unit tests in `*_test.go` alongside code
- Integration tests in `test/integration/`
- Test functions named `TestFunctionName_Scenario`
- Table-driven tests for multiple cases

---

## Formatting

Run before every commit:

```bash
gofmt -s -w .
goimports -w .
```

Better: Use an editor that runs these on save.

### Logging

**Use structured logging exclusively.**

```go
// BAD - printf style
logger.Infof("device %d created with %d queues", deviceID, numQueues)

// GOOD - structured
logger.Info("device created", "device_id", deviceID, "queues", numQueues)
```

Keys use `snake_case` for consistency with metrics/tracing systems.

### Interface Naming

**Always use `-er` suffix, even if it sounds awkward.**

```go
// GOOD
type Reader interface { Read() }
type Writer interface { Write() }
type Closer interface { Close() }
type Flusher interface { Flush() }

// Also acceptable (even if not perfect English)
type Discorder interface { Discard() }
type Resizer interface { Resize() }
```

Avoid `-able` suffixes - stick to Go convention.
