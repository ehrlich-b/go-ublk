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

### DiscardBackend — wired

Handle TRIM/discard operations (useful for SSDs and sparse files). Implementing
it is also what makes the device advertise discard support at all: without it,
`discard_max_bytes` is 0 and `blkdiscard` reports "operation not supported".

```go
func (b *MyBackend) Discard(offset, length int64) error {
    // Mark region as unused, potentially freeing space
    return nil
}
```

### WriteZeroesBackend — wired

Zero a range without transferring a buffer of zeros, and likewise what makes the
device advertise `write_zeroes_max_bytes` at all. Unlike discard, this one is not
advisory: the range must actually read back as zeros afterwards.

```go
func (b *MyBackend) WriteZeroes(offset, length int64) error {
    // Punch a hole, or write zeros — but the bytes must read back zeroed
    return nil
}
```

Those two are the complete set. `Read`, `Write` and `Flush` come from `Backend`
itself; the I/O loop dispatches exactly these five operations.

## Durability

A completed write is durable only if your backend made it durable. The device
tells the kernel which of those two worlds it lives in, and that decides whether
you ever receive a `Flush`:

- `params.VolatileCache = true` (the default) — writes may still be in a cache
  when `WriteAt` returns. The kernel sends a FLUSH whenever something above needs
  durability (a journal commit, an `fsync`, a barrier), and your `Flush()` must
  make previous writes durable before it returns.
- `params.VolatileCache = false` — you are promising every completed write is
  *already* durable. The kernel then never sends a flush at all: it completes
  empty flushes itself and drops `REQ_PREFLUSH`. Claiming this when it isn't true
  loses data on power failure, with no error anywhere.

The default is the fail-safe one because the library cannot know which world your
backend lives in, and the mistakes are not symmetric: a cache that doesn't exist
costs one no-op round-trip, while hiding one that does costs data. Set it false
only if a returned write is genuinely durable — `ublk-mem` does (RAM has nothing
underneath it to flush to).

`ublk-loop` shows both, honestly: buffered by default (volatile cache, flush →
`fsync`), and `-sync` for `O_DSYNC` (write-through, no flushes needed).

## Included Examples

### ublk-mem — RAM disk, optionally compressed

The smallest useful backend: a `[]byte` behind sharded locks. With `--zip` it
stores 64KB chunks flate-compressed instead, which shows the block layer cannot
tell what a backend does with the bytes — 128MB of compressible data fits in
about 1MB of RAM, and reads back byte-exact.

```bash
make build
sudo ./bin/ublk-mem --size=512M            # plain RAM disk
sudo ./bin/ublk-mem --size=512M --zip      # compressed in RAM
sudo ./bin/ublk-mem --del=all              # reap devices a killed daemon left behind
```

### ublk-loop — export a file, like losetup

The backend a real user is more likely to write: positional file I/O, sparse
allocation, discard that punches holes and actually returns space to the
filesystem, and an explicit answer to the durability question above.

```bash
make build
sudo ./bin/ublk-loop --file=/var/tmp/disk.img --size=1G   # creates a sparse file
sudo ./bin/ublk-loop --file=/var/tmp/disk.img --sync      # O_DSYNC, write-through
sudo ./bin/ublk-loop --file=/var/tmp/disk.img --read-only
```

Both examples use only the public API, so they compile the same way outside this
repository. `scripts/vm-loop-e2e.sh` is their test.

## Teardown

Both examples share one non-obvious shutdown rule, and copying it matters:

**Do not cancel the context before calling `Close()`.** The kernel drains
in-flight I/O before `STOP_DEV` returns, and only the running I/O goroutines can
complete that drain. `Close()` stops them itself, in the right order. Cancelling
first strands the in-flight requests and hangs teardown on a busy device.
