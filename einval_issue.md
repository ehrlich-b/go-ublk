# UBLK START_DEV -EINVAL Issue

## Current Problem

Pure Go ublk implementation successfully completes ADD_DEV and SET_PARAMS operations, but START_DEV returns -EINVAL (-22). Device nodes `/dev/ublkc0` and `/dev/ublkb0` do not appear.

**Environment**: Linux 6.11.0-24-generic, ublk_drv module loaded, `/dev/ublk-control` accessible.

## What Currently Works ✅

- **ADD_DEV**: Returns 0 (success) with ioctl-encoded cmd_op (0xc0207504)
- **SET_PARAMS**: Returns 0 with 128-byte BASIC parameter buffer
- **SQE Structure**: 32-byte control header correctly placed at SQE bytes 48-79
- **Feature Flags**: Using 0x42 (CMD_IOCTL_ENCODE | URING_CMD_COMP_IN_TASK)
- **io_uring Integration**: IORING_OP_URING_CMD (opcode 46) accepted by kernel
- **Driver Reach**: Kernel traces show ublk_ctrl_uring_cmd() being called

## Critical Technical Findings

### SQE Structure Layout (FIXED)
```
Bytes 0-31:   Base SQE with opcode=46, fd=/dev/ublk-control, cmd_op=0xc0207504
Bytes 32-47:  Standard fields (addr=0, len=32)
Bytes 48-79:  32-byte ublksrv_ctrl_cmd structure (NOT in SQE128 extension)
Bytes 80-127: Zeros
```

### Control Command Structure
```c
struct ublksrv_ctrl_cmd {  // 32 bytes at SQE offset 48
    __u32 dev_id;         // 0xffffffff for new device
    __u16 queue_id;       // 0xffff for control ops
    __u16 len;            // sizeof(target_buffer)
    __u64 addr;           // pointer to dev_info/params buffer
    __u64 data[1];        // 0 for ADD_DEV/SET_PARAMS; daemon_pid for START_DEV
    __u16 dev_path_len;   // 0 (privileged mode)
    __u16 pad;            // 0
    __u32 reserved;       // 0
};
```

## Known Issue: Startup Sequence

**Problem**: Current implementation calls START_DEV before queue initialization. Working C reference (demo_null) shows different order:

1. **C Reference (WORKS)**:
   - ADD_DEV → SET_PARAMS → **Create queue threads** → **Submit initial FETCH_REQ per tag** → START_DEV

2. **Current Go (FAILS)**:
   - ADD_DEV → SET_PARAMS → START_DEV → Create runners → Submit FETCH_REQ

**Evidence**: demo_null successfully creates `/dev/ublkb0` and `/dev/ublkc0` with identical SQE encoding but different startup sequence.

## What We Don't Know / In Progress

1. **Queue Initialization Timing**: Whether `/dev/ublkc<ID>` appears after ADD_DEV or only after START_DEV
2. **FETCH_REQ Requirements**: Exact timing and number of initial FETCH_REQ submissions needed
3. **Device Node Creation**: Why START_DEV succeeds in C but fails in Go despite identical encoding
4. **Adaptive Startup**: Need robust sequence that works regardless of when char device appears

## Next Steps Required

### Immediate Fix (Startup Reordering)
```
1. ADD_DEV + SET_PARAMS (working)
2. Attempt to open /dev/ublkc<ID> with timeout/retry
3. If available: Initialize runners + submit FETCH_REQ per tag
4. START_DEV with daemon PID in data[0]
5. If char device wasn't available: adaptive fallback sequence
```

### Validation Commands (VM)
```bash
# Test current implementation
make vm-e2e

# Check device creation
./vm-ssh.sh "ls -l /dev/ublk* || true; ls -d /sys/class/ublk* || true"

# Compare with working C reference
./vm-ssh.sh "cd ~/go-ublk/.gitignored-repos/ublksrv-c && sudo timeout 8 ./demo_null"
```

### Debugging Infrastructure Available
- **Full kernel tracing**: Dynamic debug for io_uring and ublk_drv modules
- **SQE byte dumps**: Complete 128-byte SQE logging in Go implementation
- **Working reference**: demo_null provides byte-perfect comparison baseline
- **VM testing**: `make vm-debug` for comprehensive trace capture

## Success Criteria

- START_DEV returns 0 (not -22)
- `/dev/ublkb<ID>` block device appears
- `/dev/ublkc<ID>` char device accessible to queue runners
- End-to-end I/O test passes (write + read + compare)

The core control plane encoding is now correct. The remaining issue is startup sequence and timing coordination between control operations and queue initialization.
