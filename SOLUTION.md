# SOLUTION: Root Causes Identified

## CRITICAL BUG #1: sqe.len MUST be 16
**Location**: `internal/uring/minimal.go:613`
**Current (WRONG)**: `sqe.len = 0`
**Fix**: `sqe.len = 16`

The SQE must specify the length of the ublksrv_io_cmd struct (16 bytes). Without this, the kernel doesn't know there's payload in the cmd area!

## CRITICAL BUG #2: mmap offset calculation
**Location**: `internal/queue/runner.go` mmapQueues function
**Issue**: Must calculate per-queue offset correctly
**Formula**: `offset = qid * round_up(queue_depth * sizeof(ublksrv_io_desc), PAGE_SIZE)`

## Why Phantom Completions Happen
The kernel never reaches `ublk_ch_uring_cmd()` because:
1. With `sqe.len = 0`, the kernel thinks there's no payload
2. The generic io_uring path completes it immediately with result=0
3. No actual FETCH_REQ is registered with ublk
4. Descriptors stay empty because ublk never writes them

## Correct FETCH_REQ Semantics
- FETCH_REQ should **block** until I/O arrives
- Posted once per (qid,tag) initially
- Completes only when kernel has I/O to deliver
- Descriptor is populated before completion
- Then use COMMIT_AND_FETCH_REQ for subsequent I/Os

## Verified Correct Values
- **Command**: 0xC0107520 (IOCTL-encoded FETCH_REQ) ✓
- **FD**: /dev/ublkcN (char device) ✓
- **SQE opcode**: 46 (IORING_OP_URING_CMD) ✓
- **SQE cmd area**: bytes 48-63 ✓
- **SQE len**: MUST BE 16 ❌ (we have 0)
- **mmap flags**: MAP_SHARED ✓

## Fixes Applied (Based on Expert Analysis)
1. ✅ **BUG #1 FIXED**: Set `sqe.len = 16` in SubmitIOCmd (was 0)
   - Without this, kernel doesn't know there's a 16-byte payload
2. ✅ **BUG #2 FIXED**: Fixed mmap offset to `queueID * descSize` (was always 0)
   - All queues were looking at queue 0's descriptors
3. ✅ **BUG #3 FIXED**: Submit FETCH_REQs BEFORE START_DEV (was after)
   - Correct sequence: Create runners → Submit FETCHes → START_DEV

## Expert's Key Insights
- FETCH_REQ should block until I/O arrives (not return immediately)
- Posted once per (qid,tag) initially
- Completes only when kernel has I/O to deliver
- Must use IOCTL-encoded commands (0xC0107520 for FETCH)
- SQE must have len=16 to indicate payload size

## Test Status
- Build successful
- Device created (/dev/ublkb0)
- But I/O still hangs (dd in D state)
- Need to check kernel traces to see if ublk_ch_uring_cmd is now being called