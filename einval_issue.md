# The -EINVAL Issue: Complete Investigation Summary

## Executive Summary
After days of investigation, `UBLK_CMD_ADD_DEV` via `IORING_OP_URING_CMD` consistently returns `-EINVAL` on our Linux 6.11 VM, despite apparently correct SQE structure and parameters. Zero kernel debug output appears, suggesting rejection happens before the ublk driver even sees our request.

## Current Status (2025-09-14)
- **Kernel**: Linux 6.11 on VM (192.168.4.79)
- **Module**: ublk_drv loaded successfully
- **Result**: All ADD_DEV attempts return -EINVAL (-22)
- **Debug visibility**: ZERO - no ublk driver prints appear in dmesg/trace

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
- ✓ Opcode = 51 (0x33) = IORING_OP_URING_CMD from kernel headers
- ✓ cmd_op = both ioctl (0xc0207504) and raw (0x04) tested
- ✓ __pad1 after cmd_op is zero (critical!)
- ✓ SQE128 flag enabled on ring creation (0xc00)
- ✓ Ring creation succeeds, mappings work
- ✓ io_uring_enter submits and returns (no ENOSYS)
- ✓ Buffer lifetime maintained during syscall
- ✓ Both opcode 50 and 51 attempted (kernel version variations)

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
**Result**: io_uring syscalls visible, but ZERO ublk driver activity

## The Core Problem

The -EINVAL is returned BEFORE the ublk driver sees our request. This means either:

1. **io_uring core rejects the SQE** - But our structure appears correct
2. **URING_CMD dispatch fails** - cmd_op not recognized or routed
3. **Permission/capability check fails** - But we're running as root
4. **Memory accessibility** - Kernel can't read our Go heap addresses
5. **Subtle SQE field validation** - Some union field has unexpected value

## Why We Can't Debug Further

1. **No kernel source access** for our exact 6.11 build
2. **No ublk driver output** - suggests rejection before driver entry
3. **Generic -EINVAL** - could mean dozens of different validation failures
4. **Go memory model** - Possible incompatibility with kernel expectations

## THE SOLUTION: Get ANYTHING Working First

### Strategy: Bypass Discovery, Focus on Success

Since we can't debug the kernel directly, we need to:

1. **Get ANY implementation working** against our kernel
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

- Days 1-3: Tried various Go implementations, all -EINVAL
- Days 4-5: Deep SQE structure analysis, still -EINVAL
- Day 6 (today): Recognition that we need working reference first
- **Next**: Get libublk-rs or ublksrv working, capture exact bytes

## Key Insights

1. **The kernel is opaque** - We can't debug what we can't see
2. **-EINVAL is too generic** - Could be any of 20+ validation checks
3. **Go memory model** might be incompatible with kernel expectations
4. **We need ground truth** - A working implementation to compare against

## The Path Forward

**STOP** trying to fix our Go code blind.
**START** by getting any implementation working.
**THEN** make our Go code match exactly.

This is now an empirical problem, not a theoretical one.