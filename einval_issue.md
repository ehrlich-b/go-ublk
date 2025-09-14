# The -EINVAL Issue: Complete Investigation Summary

## MAJOR BREAKTHROUGH (2025-09-14)

After switching to proper Linux 6.11 kernel and enabling debug infrastructure, we discovered:

1. **WRONG OPCODE**: We were using opcode 51 (FUTEX_WAIT) instead of 46 (URING_CMD)
2. **Kernel debug works on 6.11!**: Full trace output now available
3. **WE'RE REACHING UBLK!**: Trace shows `ublk_ctrl_uring_cmd` is being called

The issue has moved from io_uring (now working!) to ublk driver validation.

### The Critical Debug Commands That Revealed Everything

```bash
# Enable all kernel debugging (ONLY WORKS ON REAL KERNELS!)
sudo bash -c '
echo "module ublk_drv +p" > /sys/kernel/debug/dynamic_debug/control
echo "module io_uring +p" > /sys/kernel/debug/dynamic_debug/control
echo 8 > /proc/sys/kernel/printk
echo 1 > /sys/kernel/debug/tracing/events/io_uring/enable
echo 1 > /sys/kernel/debug/tracing/events/block/enable
echo 1 > /sys/kernel/debug/tracing/events/syscalls/sys_enter_io_uring_enter/enable
'

# Enable function tracing
sudo bash -c '
echo "ublk*" >> /sys/kernel/debug/tracing/set_ftrace_filter
echo "io_uring*" >> /sys/kernel/debug/tracing/set_ftrace_filter
echo function_graph > /sys/kernel/debug/tracing/current_tracer
echo 1 > /sys/kernel/debug/tracing/tracing_on
'

# View the trace - THIS SHOWED THE OPCODE PROBLEM!
sudo cat /sys/kernel/debug/tracing/trace | grep io_uring

# Result showed:
# io_uring_req_failed: opcode FUTEX_WAIT (should be URING_CMD!)
# After fix: io_uring_cmd() { ublk_ctrl_uring_cmd() } ✅
```

## Executive Summary
After days of blind debugging with `-EINVAL` on a fake "6.14" kernel, switching to proper 6.11.0-24-generic revealed the core issue: **wrong io_uring opcode**. We were using 51 (FUTEX_WAIT) instead of 46 (URING_CMD). Now io_uring accepts our SQE and passes it to the ublk driver, which returns -EINVAL for the control command itself.

## THIRD BREAKTHROUGH (2025-09-14 - Structure Fix!)

After fixing the control command structure from 32 bytes to **48 bytes**, we're now reaching the actual ADD_DEV handler:

### Kernel Trace Shows We're IN the Driver! ✅
```
ublk_ctrl_uring_cmd() {
  ublk_ctrl_add_dev.isra.0();   # ✅ WE'RE HERE!
+ 42.415 us |   io_uring_cmd_done();
}
result -22  # Still -EINVAL, but from INSIDE ublk_ctrl_add_dev
```

**MASSIVE PROGRESS**:
- ✅ 48-byte structure is accepted by the kernel
- ✅ We reach `ublk_ctrl_uring_cmd()`
- ✅ **NEW**: We reach `ublk_ctrl_add_dev.isra.0()` function!
- ❌ Still returns `-22` (-EINVAL) from within the ADD_DEV handler

This means our structure format is now correct, but there's a validation error inside the actual ADD_DEV processing logic.

### Kernel Trace Shows Full Flow Working ✅
```
io_uring_submit_req: ring 000000008432dbea, req 000000005d3fe04c, user_data 0x0, opcode URING_CMD
io_uring_cmd() {
  ublk_ctrl_uring_cmd [ublk_drv]() {
+ 53.147 us |      io_uring_cmd_done();
}
io_uring_complete: ring 000000008432dbea, req 000000005d3fe04c, user_data 0x0, result -95
```

**Analysis**:
- ✅ io_uring accepts our SQE perfectly
- ✅ Calls `ublk_ctrl_uring_cmd()` in the driver
- ✅ Driver spends **53μs** processing (substantial work!)
- ❌ Then returns `-95` (EOPNOTSUPP) or `-22` (EINVAL)

## Current Status (2025-09-14 - MAJOR PROGRESS!)
- **Kernel**: Linux 6.11.0-24-generic on VM (192.168.4.79)
- **Module**: ublk_drv loaded successfully
- **io_uring**: ✅ WORKING - accepts URING_CMD with opcode 46
- **ublk driver**: ✅ WORKING - reaches ublk_ctrl_add_dev function
- **Structure**: ✅ FIXED - 48-byte control command structure accepted
- **ADD_DEV Handler**: ❌ Returns -EINVAL from within ublk_ctrl_add_dev
- **Debug visibility**: ✅ FULL - can see exact function calls in kernel
- **Progress**: Moved from structure rejection to parameter validation inside ADD_DEV

## What We've Verified ✓

### 1. SQE Structure (128 bytes)
```
Bytes 0-63 (Base SQE):
  0-3:   opcode(u8) flags(u8) ioprio(u16) = 0x33 00 00 00
  4-7:   fd(i32) = 0x03 00 00 00 (fd=3, /dev/ublk-control)
  8-15:  cmd_op(u32) __pad1(u32) = 0x04 75 20 c0 00 00 00 00
         (ioctl-encoded) or 0x04 00 00 00 00 00 00 00 (raw)
  16-23: addr(u64) = pointer to dev_info buffer
  24-31: len(u32) uring_cmd_flags(u32) = 0x40 00 00 00 00 00 00 00
  32-63: userData(u64) + other fields (all zeroed)

Bytes 64-127 (SQE128 extension - cmd area):
  64-95: 32-byte ublksrv_ctrl_cmd inline header
  96-127: zeros
```

### 2. Control Header (bytes 64-95)
```c
struct ublksrv_ctrl_cmd {
  __u32 dev_id;       // 0xFFFFFFFF for new device
  __u16 queue_id;     // 0xFFFF for control commands
  __u16 len;          // 64 (sizeof dev_info)
  __u64 addr;         // pointer to dev_info buffer
  __u64 data[1];      // 0 (no inline data)
  __u16 dev_path_len; // 0 (privileged mode)
  __u16 pad;          // 0
  __u32 reserved;     // 0
};
```

### 3. Device Info Buffer (64 bytes at addr)
```
01 00  // nr_hw_queues = 1
20 00  // queue_depth = 32
00 00 00 00  // state = 0
00 00 10 00  // max_io_buf_bytes = 1MB
ff ff ff ff  // dev_id = -1 (new)
[pid] [flags] [uid] [gid] // process info
[40 bytes of zeros] // padding
```

### 4. Verified Correct
- ✅ **FIXED**: Opcode = 46 (0x2e) = IORING_OP_URING_CMD (was using 51/FUTEX_WAIT!)
- ✓ cmd_op = both ioctl (0xc0207504) and raw (0x04) tested
- ✓ __pad1 after cmd_op is zero (critical!)
- ✓ SQE128 flag enabled on ring creation (0xc00)
- ✓ Ring creation succeeds, mappings work
- ✅ io_uring now accepts SQE and calls ublk driver!
- ✓ Buffer lifetime maintained during syscall
- ✅ Kernel trace shows: `io_uring_cmd() { ublk_ctrl_uring_cmd() }`

## What We've Tried

### Approach 1: giouring Library (iceber/iouring-go)
- **Result**: -EINVAL
- **Issue**: Library abstraction may not properly construct URING_CMD SQEs

### Approach 2: Minimal Pure-Go Shim
- **Result**: -EINVAL
- **Implementation**: Direct syscalls, manual SQE construction
- **Verified**: Byte-perfect SQE according to kernel headers

### Approach 3: Multiple cmd_op Encodings
- **Ioctl-encoded**: 0xc0207504 = _IOWR('u', 4, 32)
- **Raw legacy**: 0x00000004
- **Result**: Both return -EINVAL

### Approach 4: Extensive Kernel Tracing
```bash
# Dynamic debug
echo 'module io_uring +p' > /sys/kernel/debug/dynamic_debug/control
echo 'module ublk_drv +p' > /sys/kernel/debug/dynamic_debug/control

# Tracepoints
echo 1 > /sys/kernel/debug/tracing/events/io_uring/enable
echo 1 > /sys/kernel/debug/tracing/events/syscalls/sys_enter_io_uring_enter/enable

# Function tracing
echo 'io_uring*' > /sys/kernel/debug/tracing/set_ftrace_filter
echo 'ublk*' >> /sys/kernel/debug/tracing/set_ftrace_filter
```
**Result on 6.11**: ✅ FULL VISIBILITY! Shows:
- `io_uring_submit_req: opcode URING_CMD` (not FUTEX_WAIT!)
- `io_uring_cmd() { ublk_ctrl_uring_cmd() }` - we reach the driver!
- Driver still returns -EINVAL but now we know why

## The Core Problem (UPDATED)

~~The -EINVAL is returned BEFORE the ublk driver sees our request.~~ **FIXED!**

Now the -EINVAL comes FROM the ublk driver itself:

1. ✅ **io_uring accepts our SQE** - Opcode 46 works!
2. ✅ **URING_CMD dispatch works** - Calls ublk_ctrl_uring_cmd
3. ❌ **ublk driver validation fails** - Returns -EINVAL for our control command
4. **Possible issues**:
   - Control command structure layout
   - Device info parameters
   - cmd_op encoding (ioctl vs raw)

## Why We Can't Debug Further

1. **No kernel source access** for our exact 6.11 build
2. **No ublk driver output** - suggests rejection before driver entry
3. **Generic -EINVAL** - could mean dozens of different validation failures
4. **Go memory model** - Possible incompatibility with kernel expectations

## THE SOLUTION: Get ANYTHING Working First

### Strategy: Bypass Discovery, Focus on Success

Since we can't debug the kernel directly, we need to:

1. **Get ANY implementation working** against our 6.11 kernel
2. **Capture the EXACT bytes** that succeed
3. **Replicate those bytes exactly** in our Go code

### Option 1: libublk-rs (Rust)
```bash
cd .gitignored-repos/libublk-rs
cargo build --example basic
sudo ./target/debug/examples/basic

# If this works, capture with:
sudo strace -e io_uring_enter -xx ./target/debug/examples/basic
```

### Option 2: ublksrv (C Reference)
```bash
git clone https://github.com/ublk-org/ublksrv
cd ublksrv
./configure && make
sudo ./ublk add -t null

# Capture exact SQE bytes
```

### Option 3: Minimal C with liburing
```c
// test_ublk.c - absolute minimal ADD_DEV
#include <liburing.h>
#include <linux/ublk_cmd.h>
#include <stdio.h>
#include <fcntl.h>
#include <string.h>

int main() {
    struct io_uring ring;
    struct io_uring_sqe *sqe;
    struct io_uring_cqe *cqe;

    // Open control device
    int fd = open("/dev/ublk-control", O_RDWR);
    if (fd < 0) return 1;

    // Setup ring with SQE128
    struct io_uring_params params = {0};
    params.flags = IORING_SETUP_SQE128 | IORING_SETUP_CQE32;
    if (io_uring_queue_init_params(32, &ring, &params) < 0) return 1;

    // Prepare ADD_DEV
    sqe = io_uring_get_sqe(&ring);

    // Build control command
    struct ublksrv_ctrl_cmd cmd = {
        .dev_id = -1,
        .queue_id = -1,
        .len = sizeof(struct ublksrv_ctrl_dev_info),
        .addr = (unsigned long)&dev_info,
    };

    struct ublksrv_ctrl_dev_info dev_info = {
        .nr_hw_queues = 1,
        .queue_depth = 32,
        .max_io_buf_bytes = 1048576,
        .dev_id = -1,
    };

    // Setup URING_CMD
    io_uring_prep_cmd(sqe, fd, UBLK_CMD_ADD_DEV);
    sqe->cmd[0] = cmd;  // Copy control header inline

    // Submit and wait
    io_uring_submit(&ring);
    io_uring_wait_cqe(&ring, &cqe);

    printf("Result: %d\n", cqe->res);

    // CRITICAL: Dump the exact SQE bytes
    unsigned char *sqe_bytes = (unsigned char *)sqe;
    printf("SQE bytes:\n");
    for (int i = 0; i < 128; i++) {
        printf("%02x ", sqe_bytes[i]);
        if ((i + 1) % 16 == 0) printf("\n");
    }

    return 0;
}
```

### Option 4: Python with ctypes
```python
# Might be easier to experiment with exact byte layouts
import ctypes
import os

# Open control device and construct exact bytes...
```

## Capture Plan

Once we get ANY implementation working:

1. **strace** the working binary:
   ```bash
   sudo strace -e io_uring_enter,io_uring_setup -xx working_binary 2>&1 | tee success.log
   ```

2. **gdb** to dump memory:
   ```bash
   gdb working_binary
   break io_uring_enter
   run
   x/128bx $rsi  # Dump SQE bytes
   ```

3. **bpftrace** to capture in-kernel:
   ```bash
   sudo bpftrace -e 'kprobe:io_uring_submit_sqes {
     printf("SQE: %r\n", arg1);
   }'
   ```

4. **Instrument the working code** to printf exact bytes

## Success Criteria

We need to capture:
1. Exact 128-byte SQE that succeeds
2. Exact memory layout of dev_info buffer
3. Any special alignment requirements
4. Exact ioctl values being used
5. Any preparatory ioctl calls we might be missing

## Once We Have Working Bytes

1. **Compare byte-by-byte** with our Go implementation
2. **Identify the exact difference** causing -EINVAL
3. **Replicate the working pattern** exactly in Go
4. **Document the critical requirement** we were missing

## Fallback Options

If we can't get pure Go working:

1. **CGO wrapper** around working C code (minimal, just for control plane)
2. **External helper binary** for control operations
3. **Kernel module** to add debug output
4. **Different kernel version** with better debugging

## Timeline

- Days 1-3: Tried various Go implementations, all -EINVAL (on fake 6.14 kernel)
- Days 4-5: Deep SQE structure analysis, still -EINVAL
- Day 6 (today):
  - Discovered VM was running non-existent "6.14" kernel
  - Switched to stable 6.11 kernel, removed problematic 6.14
  - **BREAKTHROUGH**: Kernel debug works on 6.11!
  - **FOUND THE BUG**: Wrong opcode (51 instead of 46)
  - **PROGRESS**: Now reaching ublk driver!
  - Driver returns -EINVAL, but we have visibility
- **Next**: Get libublk-rs working to compare exact control command bytes

## Key Insights

1. ~~**The kernel is opaque**~~ - **6.11 debug infrastructure works great!**
2. **Wrong kernel version hides bugs** - Fake 6.14 had no debug output
3. **Opcode values vary by kernel** - URING_CMD is 46 on 6.11, not 51
4. **We're SO CLOSE** - io_uring works, just need correct ublk command format
5. **We still need ground truth** - A working implementation to compare against

## The Path Forward

We've made **MASSIVE PROGRESS**:
- ✅ io_uring accepts our SQE
- ✅ ublk driver is being called
- ❌ Just need correct control command format

**Next Steps**:
1. Get libublk-rs or ublksrv working as reference
2. Compare exact control command bytes
3. Fix our command structure to match

We're literally one struct layout away from success!