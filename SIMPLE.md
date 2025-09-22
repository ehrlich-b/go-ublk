# SIMPLE.md - What Actually Works

## Current State
- **ADD_DEV**: ✅ Works - creates /dev/ublkc0
- **SET_PARAMS**: ✅ Works - returns 0
- **START_DEV**: ❌ HANGS FOREVER - blocks in io_uring_enter
- **Data plane**: Never tested (blocked by START_DEV)
- **Block device**: Never created (requires START_DEV to complete)

## The Bug
START_DEV command is submitted to kernel but never gets a completion. The process hangs forever waiting.

## Code Structure
```
/internal/uring/minimal.go - io_uring wrapper (has the bug)
/internal/ctrl/control.go  - device control operations
/internal/uapi/structs.go  - kernel structures (32-byte UblksrvCtrlCmd)
/cmd/ublk-mem/main.go      - test program
```

## How to Test
```bash
make build && make vm-copy
./vm-ssh.sh "cd ~/ublk-test && sudo timeout 5 ./ublk-mem --size=16M"
```
If it times out after 5 seconds, START_DEV is still hanging.

## What Needs Fixing
Find why START_DEV hangs. Compare with working C code in `.gitignored-repos/ublksrv-c/`

## Ignore Everything Else
Don't trust the docs. Don't add features. Just fix START_DEV.