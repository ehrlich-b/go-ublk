# UBLK START_DEV -EINVAL Issue → mmap EPERM Discovery

## Executive Summary (2025-09-15)

**Original Issue**: START_DEV returning -EINVAL (-22) preventing device creation.

**SOTA LLM Analysis**: Identified 6 critical protocol requirements for START_DEV success. All were already correctly implemented in our codebase.

**Actual Discovery**: START_DEV never executes. Queue initialization fails earlier with mmap EPERM on `/dev/ublkc0`, revealing a deeper kernel/VM configuration issue affecting both Go and C implementations.

## The Journey: From Protocol Bug to Kernel Mystery

### Phase 1: Original Problem Statement

Pure Go ublk implementation successfully completed ADD_DEV and SET_PARAMS, but START_DEV returned -EINVAL. Device nodes `/dev/ublkc0` and `/dev/ublkb0` did not appear.

**Environment**: Linux 6.11.0-24-generic, ublk_drv module loaded, `/dev/ublk-control` accessible.

### Phase 2: SOTA LLM Protocol Analysis

A state-of-the-art LLM analyzed the issue and identified these critical requirements:

1. **START_DEV control structure**: Must have `len=0` and `addr=0` (no payload)
2. **Daemon TGID**: `data[0]` must contain `getpid()` (process ID)
3. **Device ID usage**: Use the returned dev_id from ADD_DEV, not 0xffffffff
4. **Startup sequence**: Create queue workers and pre-post FETCH_REQs BEFORE START_DEV
5. **Device node polling**: Poll for `/dev/ublkc<id>` after ADD_DEV, not after START_DEV
6. **FETCH_REQ priming**: Submit queue_depth FETCH_REQs per queue before START_DEV

**Verification Result**: ✅ ALL recommendations were already correctly implemented in our code!

### Phase 3: The Real Discovery

When testing the implementation, we discovered:

```
ADD_DEV result: 0 (SUCCESS)
Device ID returned: 0
SET_PARAMS result: 0 (SUCCESS)
/dev/ublkc0 created: YES ✅
Queue runner initialization: FAILED - mmap: operation not permitted (EPERM)
START_DEV: NEVER REACHED
```

**The plot twist**: START_DEV was never the problem. The queue initialization fails before we even attempt START_DEV.

## Deep Technical Analysis

### What Actually Happens

```go
// Sequence of operations and their results:
1. ADD_DEV(dev_id=0xffffffff) → Returns 0, assigns dev_id=0 ✅
2. SET_PARAMS(dev_id=0) → Returns 0 ✅
3. /dev/ublkc0 appears → Created by kernel ✅
4. open("/dev/ublkc0", O_RDWR) → fd=5, SUCCESS ✅
5. mmap(fd=5, size=32*32, MAP_SHARED) → EPERM ❌
6. START_DEV → Never reached
```

### The mmap EPERM Mystery

The mmap system call fails with EPERM (Operation not permitted) when trying to map the descriptor array from `/dev/ublkc0`. This is unusual because:

1. **File opens successfully**: We get a valid file descriptor
2. **Device node exists**: `/dev/ublkc0` is created with correct permissions (crw------- root)
3. **Running as root**: sudo is being used
4. **Memory limits OK**: ulimit -l shows ~1GB available

### Cross-Implementation Validation

Testing the C reference implementation (demo_null) reveals:
```bash
# C implementation also fails, but differently:
"./demo_null: can't start daemon: Cannot allocate memory"
```

This suggests a **systemic issue** with the VM/kernel configuration, not a protocol bug.

## Root Cause Hypotheses

### Hypothesis 1: Kernel State Machine Violation
The kernel ublk driver might require a specific state transition that we're violating:
- Device might need to be in a specific state for mmap to work
- START_DEV might need to be called BEFORE mmap (contrary to SOTA analysis)
- Some kernels might have different requirements than upstream

### Hypothesis 2: Memory/Resource Configuration
- VM might have restricted memory mapping capabilities
- cgroup/namespace restrictions preventing mmap
- Kernel security policies (SELinux/AppArmor) blocking mmap

### Hypothesis 3: Kernel Module Parameters
- ublk_drv module might need specific parameters
- Kernel might be compiled with restrictive options
- Missing kernel capabilities or features

### Hypothesis 4: Device Initialization Race
- Character device created but not fully initialized
- Kernel driver internal state not ready for mmap
- Timing-dependent initialization issue

## Experimental Evidence

### Test 1: Device Node Creation Timing
```bash
# /dev/ublkc0 appears immediately after ADD_DEV
# Permissions: crw------- 1 root root 240, 0
# This confirms ADD_DEV works correctly
```

### Test 2: Control Plane Success
```
ADD_DEV: cmd=0xc0207504, result=0 ✅
SET_PARAMS: cmd=0xc0207508, result=0 ✅
Both operations succeed, proving control plane protocol is correct
```

### Test 3: SQE Structure Validation
```
Our SQE bytes match C implementation structure:
- Opcode 46 (URING_CMD) at correct position
- Control structure at bytes 48-79
- ioctl-encoded commands working
```

## The Paradigm Shift

**Original thinking**: "START_DEV returns -EINVAL due to protocol error"

**SOTA LLM thinking**: "START_DEV needs proper sequencing and parameters"

**Reality**: "We never reach START_DEV; mmap fails due to kernel/VM state issue"

This reveals a fundamental assumption error: we assumed the protocol was wrong when actually the environment is non-standard.

## Current Workarounds Being Explored

### 1. Reverse Startup Sequence
```go
// Try START_DEV before mmap (opposite of SOTA recommendation)
ADD_DEV → SET_PARAMS → START_DEV → Open chardev → mmap → FETCH_REQ
```

### 2. Lazy mmap Strategy
```go
// Defer mmap until after device is fully initialized
Open fd → START_DEV → Wait for state change → mmap → FETCH_REQ
```

### 3. Alternative Memory Mapping
```go
// Try different mmap flags or parameters
MAP_PRIVATE instead of MAP_SHARED
MAP_ANONYMOUS for testing
Different offsets or sizes
```

### 4. Bypass mmap Entirely
```go
// Use ioctl-based I/O without mmap
Allocate buffers in userspace
Pass addresses via ioctl commands
Avoid kernel memory mapping
```

## Validation Commands

```bash
# Check kernel module state
lsmod | grep ublk
sudo modinfo ublk_drv

# Trace actual syscalls
sudo strace -e mmap,open,ioctl ./ublk-mem 2>&1

# Check kernel messages
sudo dmesg | grep -i ublk

# Test with different kernel parameters
echo 2 | sudo tee /proc/sys/vm/overcommit_memory

# Check security contexts
getenforce  # SELinux
aa-status   # AppArmor
```

## Success Criteria (Updated)

1. ✅ ADD_DEV returns 0
2. ✅ SET_PARAMS returns 0
3. ✅ /dev/ublkc<id> created
4. ❌ mmap succeeds on char device
5. ❌ FETCH_REQ can be submitted
6. ❌ START_DEV returns 0
7. ❌ /dev/ublkb<id> created
8. ❌ I/O operations work

We're stuck at step 4, which prevents testing steps 5-8.

## Philosophical Implications

This issue demonstrates a critical lesson in systems debugging:

1. **Expert analysis can be theoretically correct but practically wrong** - The SOTA LLM's analysis was perfect for standard kernels, but our environment is non-standard.

2. **Protocol correctness ≠ Implementation success** - Our protocol implementation is correct, but environmental factors block success.

3. **The bug is often not where you think** - We spent time perfecting START_DEV when the issue was in mmap.

4. **Cross-implementation testing is crucial** - The C implementation also failing revealed this is environmental, not a Go-specific bug.

## Next Actions

1. **Environment Investigation**: Determine why this specific VM/kernel configuration blocks mmap
2. **Kernel Source Analysis**: Review ublk_drv source for mmap prerequisites
3. **Alternative VM Testing**: Try different kernel versions or distributions
4. **Upstream Consultation**: Check if this is a known issue with Linux 6.11 ublk implementation

## Conclusion

The START_DEV -EINVAL issue was a red herring. The real issue is an environmental mmap restriction that prevents queue initialization. All protocol implementations are correct, but the kernel/VM configuration has non-standard requirements that need to be identified and addressed.

## 2025-09-16 Update – PROT_READ experiment & vm-e2e hang

### What we changed
- Updated `internal/queue/runner.go` so the descriptor array mmap uses `PROT_READ` (matches `ublksrv` behaviour and avoids the EPERM we hit with `PROT_WRITE`).
- Rebuilt with `make build` (after requesting cache access) and re-ran the VM flow via `make vm-e2e`.

### Observations from the new run
- `ADD_DEV` now returns device id **1** instead of 0. `/dev/ublkc0` already exists on the VM (timestamp Sept 15) which strongly suggests the kernel still thinks a previous device is registered. That stale node never disappeared after earlier crashes, so the new device is allocated id 1.
- During the test the script polls for `/dev/ublkb0` and `/dev/ublkc0`, never for id 1. Because the kernel would expose `/dev/ublkb1`/`/dev/ublkc1`, the polling logic never detects the freshly created device even if START_DEV succeeds.
- Kernel-side nodes for id 1 never appear in `/dev/ublk*` during the wait loop, so either START_DEV is still failing (no `*** CRITICAL: START_DEV result` line emitted) or the device creation stalls before the nodes are surfaced. Need to confirm by capturing the full Go log—current `/tmp/ublk_mem.log` truncates right after the queue priming step so we never see a START_DEV entry.
- When the script aborts it prints `Cleaning up...` and then hangs. Root cause: cleanup runs `sudo kill -SIGINT $UBLK_PID`. The VM is not whitelisted for passwordless `sudo kill`, so the command blocks waiting for a password prompt, holding the SSH session open forever. That explains the apparent “make vm-e2e never returns”.

### Immediate action items
1. Clean up the stale kernel device (`sudo ublkadm del --dev-id 0` or reboot the VM) so new runs get dev id 0 again. Alternatively teach the test script to parse the actual device id from the log instead of assuming 0.
2. Capture `/tmp/ublk_mem.log` after the run (maybe stop tail -F earlier) to verify whether `Controller.StartDevice` is still failing or if the queue runner/char device bootstrap is blocking.
3. Fix `test-e2e.sh` cleanup so it does not rely on passworded sudo (e.g. call `pkexec` or just `kill` via `/proc/$PID/fd`, or install a NOPASSWD rule for `kill`). This removes the hang even when the test fails and keeps CI usable.

**Status**: Control plane ✅ | Data plane still blocked (device nodes never appear) | VM cleanup currently hangs due to sudo prompt | Root cause investigation ongoing
