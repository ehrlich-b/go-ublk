# Examples

## Implementing a Backend

A backend is anything that can store and retrieve blocks of data. The simplest possible backend is a `/dev/null` equivalent - it accepts writes and returns zeros on read:

```go
package main

import (
    "context"
    "log"

    "github.com/ehrlich-b/go-ublk"
)

type NullBackend struct {
    size int64
}

func (n *NullBackend) ReadAt(p []byte, off int64) (int, error) {
    // Return zeros (Go slices are zero-initialized)
    clear(p)
    return len(p), nil
}

func (n *NullBackend) WriteAt(p []byte, off int64) (int, error) {
    // Discard all writes
    return len(p), nil
}

func (n *NullBackend) Size() int64     { return n.size }
func (n *NullBackend) Close() error    { return nil }
func (n *NullBackend) Flush() error    { return nil }

func main() {
    backend := &NullBackend{size: 1 << 30} // 1GB

    params := ublk.DefaultParams(backend)
    device, err := ublk.CreateAndServe(context.Background(), params, &ublk.Options{})
    if err != nil {
        log.Fatal(err)
    }

    log.Printf("Device: %s", device.Info().BlockPath)
    select {} // Block forever
}
```

That's it. Five methods, and you have a block device.

## Optional Interfaces

For better performance or additional features, implement these optional interfaces:

### DiscardBackend

Handle TRIM/discard operations (useful for SSDs and sparse files):

```go
func (b *MyBackend) Discard(offset, length int64) error {
    // Mark region as unused, potentially freeing space
    return nil
}
```

### WriteZeroesBackend

Efficiently zero a region without allocating a buffer:

```go
func (b *MyBackend) WriteZeroes(offset, length int64) error {
    // Zero the region efficiently
    return nil
}
```

### SyncBackend

Fine-grained sync control:

```go
func (b *MyBackend) Sync() error {
    // Sync all data to stable storage
    return nil
}

func (b *MyBackend) SyncRange(offset, length int64) error {
    // Sync only the specified range
    return nil
}
```

## Included Examples

### ublk-mem

A memory-backed block device. Useful for testing and as a RAM disk.

```bash
make build
sudo ./bin/ublk-mem --size=512M
```

See [ublk-mem/main.go](ublk-mem/main.go) for the full implementation.
