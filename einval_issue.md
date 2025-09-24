# UBLK Userspace Block Device - I/O Routing Issue

## Problem Summary

**Go-based ublk implementation successfully creates devices but I/O operations hang indefinitely.**

- Device creation works: ADD_DEV, SET_PARAMS, START_DEV all succeed
- Block device appears: `/dev/ublkbN` exists with correct major number (259)
- But any I/O operation (like `dd`) hangs in D state forever
- No kernel trace events generated for I/O operations

## What Actually Works ✅

1. **Device Creation**: Perfect
   - ADD_DEV → Device ID assigned (returns 0)
   - SET_PARAMS → Parameters set (returns 0)
   - START_DEV → Block device `/dev/ublkbN` appears
   - Character device `/dev/ublkcN` accessible

2. **Queue Infrastructure**: Working
   - All 32 FETCH_REQ commands submitted successfully
   - io_uring operations complete successfully
   - Queue runners active and processing completions

3. **Control Communication**: Working
   - io_uring_enter syscalls return success
   - Control commands reach completion
   - Application logs show all operations succeeding

## What's Broken ❌

**Block I/O routing completely fails:**

- `dd if=/dev/zero of=/dev/ublkbN` hangs immediately in D state
- No kernel trace events generated despite comprehensive tracing setup
- Process enters uninterruptible sleep and cannot be killed
- Zero block layer activity recorded

## Core Issue: Kernel Tracing Shows Nothing

**We cannot debug the I/O routing failure because our kernel tracing filters are wrong.**

Current tracing setup shows `0/0 entries` even for operations that work (like device creation). This means we're filtering out all kernel activity by using incorrect function names.

**Wrong function names we're currently trying to trace:**
- `ublk_ctrl_ioctl` ❌ (doesn't exist)
- `ublk_ctrl_add_dev` ❌ (doesn't exist)
- `ublk_ctrl_set_params` ❌ (doesn't exist)
- `ublk_queue_rq` ❌ (might not exist)

## What We Need Help With

### Critical Missing Information

**We need the actual function names from the Linux ublk driver (`drivers/block/ublk_drv.c`) to fix our kernel tracing:**

1. **Control path functions** (handle ioctls from `/dev/ublk-control`):
   ```c
   ???_ioctl()       // Handles ADD_DEV, SET_PARAMS, START_DEV commands
   ???_add_dev()     // ADD_DEV implementation
   ???_set_params()  // SET_PARAMS implementation
   ???_start_dev()   // START_DEV implementation
   ```

2. **Data path functions** (handle io_uring URING_CMD from `/dev/ublkcN`):
   ```c
   ???_uring_cmd()   // Main URING_CMD handler for FETCH_REQ operations
   ???_queue_rq()    // Block layer request queue handler
   ```

3. **Block device functions** (handle I/O to `/dev/ublkbN`):
   ```c
   ???_submit_bio()  // Block layer entry point
   ???_make_request() // Request processing
   ```

### How to Find These Function Names

**Option 1: Kernel source examination**
- Browse: https://github.com/torvalds/linux/blob/master/drivers/block/ublk_drv.c
- Search for ioctl handlers and function definitions

**Option 2: Runtime discovery on our VM**
```bash
# List all ublk kernel symbols:
grep ublk /proc/kallsyms

# Function names from kernel module:
nm /lib/modules/$(uname -r)/kernel/drivers/block/ublk_drv.ko
```

**Option 3: Function name patterns to search for**
- Functions calling `misc_register()` (for `/dev/ublk-control`)
- Functions calling `add_disk()` (for block device registration)
- Functions with `uring_cmd` in name (io_uring passthrough)
- Functions processing `UBLK_CMD_*` constants

### Specific Question for Kernel Experts

**"What are the actual function names in the Linux ublk driver for:**

1. **ioctl handler** that processes ADD_DEV, SET_PARAMS, START_DEV from `/dev/ublk-control`?
2. **URING_CMD handler** that processes FETCH_REQ operations from `/dev/ublkcN`?
3. **Block request handler** that processes I/O requests to `/dev/ublkbN`?

**Context:** Our Go ublk implementation succeeds at syscall level (io_uring_enter returns success) but we can't trace kernel execution because we're using wrong function names in ftrace filters. We need correct names to debug why block I/O hangs."

## Technical Environment

- **Kernel**: 6.11.0-24-generic (Ubuntu VM)
- **ublk Module**: Loaded and functional for device creation
- **Go Implementation**: Pure Go using io_uring URING_CMD
- **Test VM**: Accessible for kernel debugging

## Expected Outcome

Once we have correct function names:
1. **Trace control operations** to verify they reach ublk driver
2. **Trace FETCH_REQ operations** to see if they reach ublk driver
3. **Trace block I/O operations** to find where routing fails
4. **Identify exact failure point** in I/O processing pipeline

## Critical Logic Error: I Am Clearly Doing Something Wrong With Tracing

**Wait, this makes no sense at all:**

If our URING_CMD submissions aren't reaching the ublk driver (0/0 trace entries), then **how are block devices appearing?**

- `/dev/ublkb0` gets created successfully ✅
- ADD_DEV returns success and assigns device ID 0 ✅
- SET_PARAMS returns success ✅
- START_DEV succeeds and block device appears ✅
- **But kernel traces show 0/0 entries for ublk functions** ❌

**This is logically impossible.** Block devices can't appear without the ublk driver being involved.

### Deep Questions About My Tracing Setup Failures

**I am clearly doing something wrong with tracing. Here are the fundamental questions I need to answer:**

1. **Am I tracing the wrong phase?**
   - Are control commands (ADD_DEV, SET_PARAMS, START_DEV) handled by different functions than what I'm tracing?
   - Maybe `ublk_ctrl_uring_cmd` is the dispatcher, but actual work happens in untraced subfunctions?

2. **Am I missing trace activation?**
   - Does ftrace require additional activation steps I'm missing?
   - Are the function filters applied but tracing not actually enabled for those functions?
   - Is there a difference between "function in filter" vs "function being traced"?

3. **Are the function names still wrong?**
   - The `.isra.0` suffixes suggest inlined functions - are these the actual callable functions?
   - Should I be tracing the base functions without `.isra.0`?
   - Are there wrapper functions that call these internal functions?

4. **Is ftrace the wrong tool?**
   - Should I be using `perf trace` instead of ftrace?
   - Are URING_CMD operations handled by a different tracing subsystem?
   - Do I need to trace at the io_uring level rather than ublk driver level?

5. **Am I tracing at the wrong time?**
   - Do I need to enable tracing BEFORE starting ublk-mem?
   - Are the operations completing so fast they're not captured?
   - Is there a race condition in trace buffer updates?

6. **What am I fundamentally misunderstanding about ublk architecture?**
   - Are control commands handled by a different code path than what I think?
   - Is there a userspace ublk daemon that handles some operations?
   - Are some operations handled entirely in userspace without kernel involvement?

### Specific Debugging Questions

**To fix my obviously broken tracing setup:**

1. **How do I verify ftrace is actually working?**
   ```bash
   # Does this generate ANY trace entries for basic operations?
   echo '*sys_write*' > /sys/kernel/debug/tracing/set_ftrace_filter
   echo 'test' > /tmp/test
   cat /sys/kernel/debug/tracing/trace
   ```

2. **How do I trace io_uring operations specifically?**
   - What functions handle URING_CMD operations in the kernel?
   - Should I be tracing `io_uring_enter` and related functions instead?

3. **How do I verify the ublk driver is actually loaded and functioning?**
   ```bash
   # Are these operations actually reaching the driver?
   lsmod | grep ublk
   cat /sys/module/ublk_drv/sections/.text  # Driver loaded?
   cat /proc/modules | grep ublk            # Module state?
   ```

4. **What's the actual call chain I should be tracing?**
   - User calls io_uring_enter() → ??? → ublk driver functions
   - What are the intermediate steps I'm missing?

### The Core Question

**If control commands succeed but generate zero kernel traces, either:**

1. **I'm tracing wrong** - The operations ARE reaching the kernel but I'm not capturing them
2. **Something is very broken** - Operations appear to succeed but don't actually reach the kernel
3. **I misunderstand the architecture** - Control operations work differently than I think

**The fact that block devices actually appear proves the kernel is involved somehow. My tracing setup is fundamentally flawed.**

### What I Need to Figure Out

Before I can debug the I/O hanging issue, I need to fix my obviously broken tracing setup by understanding:

- **How to actually capture ublk driver function calls in ftrace**
- **What the real call chain is from io_uring_enter to ublk driver**
- **Why successful kernel operations generate zero trace entries**
- **Whether I'm using the right tracing approach at all**

**I clearly don't understand something basic about Linux kernel tracing or ublk architecture.**

## Code Architecture (Working Parts)

**Pure Go implementation using:**
- `/dev/ublk-control` for device management (ADD_DEV/SET_PARAMS/START_DEV) ✅
- `/dev/ublkcN` character devices with io_uring URING_CMD ✅
- Custom io_uring wrapper at `internal/uring/minimal.go` ✅
- Queue runners at `internal/queue/runner.go` ✅

**All userspace code works correctly - the issue is in kernel I/O routing that we cannot trace due to wrong function names.**