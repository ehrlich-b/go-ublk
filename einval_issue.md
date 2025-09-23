# UBLK Userspace Block Device - I/O Request Routing Issue

## Current Status: Block Requests Issued But Never Reach Userspace

**Device creation works perfectly. Block layer issues I/O requests to ublk driver successfully. But requests never reach our userspace process.**

## The Problem (After Proper Kernel Tracing)

1. **Device Creation**: ✅ Working perfectly
   - ADD_DEV, SET_PARAMS, START_DEV all work
   - `/dev/ublkbN` appears correctly
   - Block device has correct major/minor numbers (259,N)

2. **Block Layer**: ✅ Working correctly
   - dd command successfully submits I/O to block layer
   - Kernel trace shows: `block_rq_issue: 259,12 R 4096 () 0 + 8`
   - READ request for 4096 bytes at sector 0 is issued

3. **ublk Driver**: ❌ **REQUEST ROUTING BROKEN**
   - Block requests reach ublk driver but **never get routed to userspace**
   - **No corresponding `block_rq_complete` in trace** - requests hang forever
   - dd process enters D state waiting for I/O completion
   - Our userspace process never receives any CQE completions

4. **Userspace Process**: ❌ Never sees the I/O
   - WaitForCompletion() blocks forever
   - No calls to processIOAndCommit for real I/O
   - Only sees spurious empty descriptors during startup

## Kernel Trace Evidence

**Block request issued successfully:**
```
iou-wrk-4887-4895    [000] .....  1494.679415: block_rq_issue: 259,12 R 4096 () 0 + 8 none,0,0 [iou-wrk-4887]
```

**No completion (this should appear but doesn't):**
```
# Should see: block_rq_complete: 259,12 R () 0 + 8 [0]
# But never appears - request hangs in ublk driver
```

## Process State Evidence

```bash
# dd process stuck in uninterruptible sleep:
root    1234    D    dd if=/dev/zero of=/dev/ublkb12 bs=512 count=1

# Our ublk-mem process running but idle:
root    1235    S    ./ublk-mem --size=16M
# WaitForCompletion() never returns - no CQEs arrive
```

## Code Flow Analysis

### What Works ✅
```go
// Device creation sequence:
controller.AddDevice()    // ✅ Device ID assigned
controller.SetParams()   // ✅ Queue config set
controller.StartDevice() // ✅ Block device appears

// Queue initialization:
runner.Prime()           // ✅ FETCH_REQ submitted for all tags
// All 32 tags submit FETCH_REQ via io_uring
```

### What's Broken ❌
```go
// I/O flow should be:
// 1. dd writes to /dev/ublkbN                    ✅ Works
// 2. Block layer issues request to ublk driver   ✅ Works (trace shows block_rq_issue)
// 3. ublk driver routes to userspace io_uring    ❌ BROKEN - never happens
// 4. Our WaitForCompletion() gets CQE            ❌ Never happens
// 5. processIOAndCommit() processes I/O          ❌ Never called
// 6. submitCommitAndFetch() completes I/O        ❌ Never happens
// 7. Block layer gets completion                 ❌ No block_rq_complete in trace
```

## The Core Question

**Why doesn't the ublk driver route block requests to our userspace io_uring?**

The block layer successfully issues requests to the ublk driver (we see `block_rq_issue` traces), but these requests never generate CQE completions for our userspace process. The ublk driver has the I/O but doesn't deliver it to us.

## Areas to Investigate

1. **FETCH_REQ State** - Are our FETCH_REQ submissions correct? Does ublk driver see them?
2. **io_uring Association** - Is character device properly linked to our io_uring instance?
3. **Queue State** - Is the ublk driver queue in the correct state to route I/O?
4. **Thread Binding** - Are we missing required thread/CPU affinity?
5. **Command Format** - Are our FETCH_REQ commands properly formatted?
6. **Timing Issues** - Do we need to wait longer after START_DEV before submitting FETCH_REQ?

## Debug Evidence

### Successful Startup Log
```
*** SUCCESS: Device /dev/ublkb12 created!
Device created: /dev/ublkb12
Character device: /dev/ublkc12
Size: 16.0 MB (16777216 bytes)
```

### When dd Runs (Kernel Trace)
```
# Block request issued:
block_rq_issue: 259,12 R 4096 () 0 + 8 none,0,0

# Our process logs: NO ACTIVITY
# WaitForCompletion() never returns
# No CQE completions arrive
# processIOAndCommit() never called
```

### Key Files for Debugging

**I/O Command Submission** (`internal/uring/minimal.go:595-610`):
```go
// FETCH_REQ submission at startup
func (r *minimalRing) SubmitIOCmd(cmd uint32, ioCmd *uapi.UblksrvIOCmd, userData uint64) {
    payload := uapi.Marshal(ioCmd)
    cmdArea := (*[16]byte)(unsafe.Add(unsafe.Pointer(sqe), 48))
    copy(cmdArea[:], payload)
    // Are these FETCH_REQ commands reaching ublk driver correctly?
}
```

**Queue Initialization** (`internal/queue/runner.go:290-322`):
```go
// Initial FETCH_REQ for each tag
func (r *Runner) submitInitialFetchReq(tag uint16) error {
    ioCmd := &uapi.UblksrvIOCmd{
        QID:  r.queueID,
        Tag:  tag,
        Addr: uint64(r.bufPtr + uintptr(int(tag)*64*1024)),
    }
    cmd := uapi.UblkIOCmd(uapi.UBLK_IO_FETCH_REQ)
    _, err := r.ring.SubmitIOCmd(cmd, ioCmd, userData)
    // Do these reach the ublk driver? Are they properly formed?
}
```

## Environment
- **Kernel**: 6.11.0-24-generic (Ubuntu VM)
- **Go**: 1.23.1
- **Architecture**: x86_64
- **ublk module**: Loaded successfully

## Request for Help

**What could cause the ublk driver to receive block requests but not route them to userspace io_uring?**

The gap is specifically between the ublk driver receiving block I/O (confirmed by kernel traces) and generating CQE completions for our userspace process (which never happens).

## Additional Debug Steps Needed

**What else can be done to debug this routing issue?**

Current tracing is probably still too basic. Need better visibility into:

1. **ublk Driver Internal State**
   - Are FETCH_REQ commands actually received by ublk driver?
   - What's the state of the ublk queue when block I/O arrives?
   - Is the driver trying to route I/O but failing?

2. **io_uring Command Flow**
   - Add ublk-specific tracepoints if available
   - Trace io_uring command submissions and completions
   - Verify FETCH_REQ vs actual I/O command routing

3. **Missing Kernel Events**
   - Are there ublk-specific trace events we should enable?
   - Function-level tracing of ublk driver functions during I/O
   - io_uring event tracing to see if commands are reaching the ring

4. **Process/Thread State**
   - Verify our io_uring thread is actually blocked in the right syscall
   - Check if there are pending CQEs that we're not processing
   - Validate thread affinity requirements

5. **Command Validation**
   - Dump the exact bytes of FETCH_REQ commands we're sending
   - Compare with working reference implementation
   - Verify SQE structure and command encoding

**Suggested kernel tracing improvements:**
- Enable specific ublk driver function tracing
- Add io_uring submit/complete event tracing
- Monitor ublk queue state changes
- Trace the path from block_rq_issue to attempted userspace delivery

The current block-layer tracing shows I/O arrives but we need deeper visibility into the ublk driver's attempt to route it to userspace.