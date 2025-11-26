# Multi-Queue Performance Debugging Prompt

## Status: RESOLVED (2025-11-26)

**Root Cause Identified:** Memory backend mutex contention, NOT kernel or io_uring issues.

See TODO.md Phase 3.1 for full analysis. The kernel ublk driver and our io_uring implementation
are correct. Multi-queue performance scaling requires backends that support concurrent access.

---

## Project Context (Historical)

`go-ublk` is a pure Go implementation of Linux ublk (userspace block device). It creates virtual block devices (/dev/ublkb*) backed by user-defined storage (memory, files, network, etc.) using the Linux ublk kernel module and io_uring for high-performance I/O. The project currently achieves 80-110k IOPS on a single queue with 4K random workloads, comparable to kernel loop devices when accounting for userspace overhead. The multi-queue feature was recently implemented to allow scaling across multiple CPU cores, but it unexpectedly shows performance degradation instead of linear scaling.

## Problem Statement

Multi-queue I/O is functionally working (reads/writes succeed, data integrity verified), but shows **severe performance degradation** compared to single-queue:

- **Single queue**: 80-110k IOPS (4K random)
- **4 queues**: 47-59k IOPS (27-57% slower)

All queues appear to serialize through a single bottleneck instead of scaling linearly.

## Current Multi-Queue Architecture

### Character Device Sharing (Potential Issue)

In `backend.go:243-261`, we open the character device ONCE and share it across all queues:

```go
// Open character device once (kernel only allows single open)
charPath := fmt.Sprintf("/dev/ublkc%d", devID)
var charFd int
for i := 0; i < 50; i++ {
    var err error
    charFd, err = syscall.Open(charPath, syscall.O_RDWR, 0)
    if err == nil {
        logger.Info("opened char device for multi-queue", "fd", charFd, "path", charPath)
        break
    }
    if err != syscall.ENOENT {
        return nil, fmt.Errorf("failed to open %s: %v", charPath, err)
    }
    time.Sleep(100 * time.Millisecond)
}

// Pass charFd to each queue runner
for i := 0; i < numQueues; i++ {
    runnerConfig := queue.Config{
        DevID:       devID,
        QueueID:     uint16(i),
        Depth:       params.QueueDepth,
        Backend:     params.Backend,
        Logger:      options.Logger,
        Observer:    observer,
        CPUAffinity: params.CPUAffinity,
        CharFd:      charFd,  // Share the fd (runner will dup it)
    }
    // ...
}
```

### Queue Runner FD Duplication

In `internal/queue/runner.go:86-91`, each queue dups the shared fd:

```go
if config.CharFd > 0 {
    // Use the provided fd (duplicate it so each queue has its own)
    fd, err = syscall.Dup(config.CharFd)
    if err != nil {
        return nil, fmt.Errorf("failed to dup char fd: %v", err)
    }
}
```

Each queue then creates its own io_uring ring with this duped fd.

### Queue ID Encoding

In `internal/queue/runner.go:355` (initial FETCH_REQ):

```go
userData := udOpFetch | (uint64(r.queueID) << 16) | uint64(tag)
```

And in `internal/queue/runner.go:719` (COMMIT_AND_FETCH):

```go
userData := udOpCommit | (uint64(r.queueID) << 16) | uint64(tag)
```

The queue ID is encoded in userdata (bits 16-31), not passed to the kernel in the actual URING_CMD.

## Theories on What's Wrong

### Theory 1: Single-Open Kernel Enforcement
The kernel ublk driver may be enforcing single-open semantics on the character device and serializing all URING_CMD submissions through a single lock, regardless of queue ID. Evidence:
- Performance degrades with more queues instead of scaling
- No parallelism benefit despite having separate io_uring rings and threads

### Theory 2: Queue ID Not Properly Communicated
The queue ID is only in userdata (completion context), not in the URING_CMD itself. The kernel may not know which queue a command belongs to, treating all commands as the same queue. Check:
- Is the queue ID supposed to be in the URING_CMD structure somewhere?
- Does the kernel expect specific queue handling in multi-queue mode?

### Theory 3: Shared FD Serialization at VFS Layer
Even with duped fds, the kernel may route all operations from the same underlying file back to a single handler, causing serialization. Evidence:
- Loop device (kernel-based) shows 195-198k IOPS with full parallelism
- Our userspace ublk with duped fds shows 47-59k IOPS (serialization)

### Theory 4: Missing Per-Queue Initialization
Multi-queue devices may require per-queue initialization or setup commands we're not issuing. The kernel might be:
- Only accepting commands from queue 0
- Requiring explicit queue registration/activation
- Expecting different URING_CMD encoding for multi-queue mode

## Key Files and Context

### Control Plane (Device Setup)
- `internal/ctrl/control.go`: ADD_DEV, SET_PARAMS, START_DEV, STOP_DEV, DEL_DEV commands
- Sets `NumQueues` in device params

### Data Plane (I/O Processing)
- `internal/queue/runner.go`: Per-queue I/O loop, state machine (FETCH_REQ → process → COMMIT_AND_FETCH)
- `internal/uring/minimal.go`: io_uring wrapper, URING_CMD submission

### UAPI Structures
- `internal/uapi/structs.go`: UblksrvIOCmd (command to/from kernel)
- Check: Is there a queue-specific command structure?

## Test Results

### Single Queue (QD=32)
```
4K Random Read:  80k IOPS, 314 MiB/s
4K Random Write: 110k IOPS, 430 MiB/s
Loop Device:     202-210k IOPS (baseline)
go-ublk overhead: 1.8-2.6x
```

### 4 Queues (QD=32)
```
4K Random Read:  58.9k IOPS (27% slower than single queue)
4K Random Write: 47.4k IOPS (57% slower than single queue)
Loop Device:     195-198k IOPS (similar to single queue)
```

## Debugging Approach

1. **Check Kernel Trace**: Are all URING_CMD submissions going through the same kernel code path?
   ```bash
   make vm-reset && timeout 30 make vm-simple-benchmark 2>&1 | grep -E "(IOPS|queue)" &&
   scripts/vm-ssh.sh 'sudo cat /sys/kernel/tracing/trace | grep ublk_ctrl_uring_cmd' | tail -20
   ```

2. **Verify Queue IDs**: Add logging to show which queue is handling which commands
   - Check if all 4 queues are actually receiving I/O or if it's serialized to one queue

3. **Check URING_CMD Structure**:
   - Does UblksrvIOCmd encode queue ID anywhere besides userdata?
   - Should queue ID be in the `QID` field during I/O commands? (Currently only in control commands)

4. **Test with Kernel Source**:
   - Review `drivers/block/ublk_drv.c` in Linux kernel source
   - Check `ublk_ch_uring_cmd()` - does it distinguish between queues when multiple threads submit from same fd?

5. **Single FD Per Queue Experiment**:
   - Modify to NOT share the fd: Each queue opens `/dev/ublkc*` separately
   - Kernel will reject the second open (single-open enforced)
   - But if we're hitting that, we should see an error, not silent serialization

## Questions for Investigation

1. When 4 queues share a duped fd, does the kernel route all commands back to queue 0?
2. Is there a queue context or queue-specific registration required in multi-queue mode?
3. Should we be using different control paths or SET_PARAMS commands per queue?
4. Does the Linux ublk driver support true parallel multi-queue, or is it inherently single-queue with a queue ID API?

## Reference: ublksrv (C Reference Implementation)

The `ublksrv` project (https://github.com/ublk-org/ublksrv) is the reference implementation in C. It may show the correct multi-queue pattern:
- How does it open the character device?
- How does it handle multiple queue threads?
- Does it use a different pattern than our fd duplication approach?

## Next Steps

1. Add detailed logging per queue to see actual throughput distribution
2. Check kernel trace to confirm URING_CMD routing
3. Review ublksrv source for correct multi-queue pattern
4. If kernel is single-open by design, document and optimize single-queue instead
