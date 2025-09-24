# CLAUDE.md - Project-Specific Guidance for go-ublk

## ⚠️ PROJECT STATUS: BROKEN

**START_DEV hangs forever. Nothing works until this is fixed.**

### What Actually Works:
- ADD_DEV command (creates /dev/ublkc0)
- SET_PARAMS command

### What's Broken:
- START_DEV hangs forever
- No block device created
- Data plane never tested

### The One Bug:
START_DEV command submitted but kernel never completes it. See TODO.md.

## Core Design Principles

### Architecture Philosophy
- **Pure Go**: Absolutely no cgo. Use syscalls and unsafe where needed for kernel interfaces
- **Modular Design**: Clean separation between control plane, data plane, and backends
- **Production Ready**: Not a toy - this should be usable in real systems
- **Idiomatic Go**: Follow Go conventions, not C patterns translated to Go

### Technical Constraints
- Linux kernel ≥ 6.1 (ublk introduced)
- io_uring support required (for IORING_OP_URING_CMD)
- Must handle both privileged and unprivileged device modes
- Feature negotiation with kernel is critical - degrade gracefully

## Package Structure

### Public API Surface (`/ublk`)
- Minimal, stable API - think standard library quality
- Backend interface should be simple (like io.ReaderAt/WriterAt)
- Hide all complexity behind clean abstractions

### Internal Packages
- `/internal/uapi` - Kernel UAPI definitions (constants, structs)
- `/internal/uring` - io_uring wrapper (hide library choice)
- `/internal/ctrl` - Control plane operations
- `/internal/queue` - Data plane per-queue runners

### Demo Applications (`/cmd`)
- Each demo should be production-usable, not just examples
- Include proper signal handling, cleanup, logging

## Critical Implementation Details

### Control Plane
- Use `/dev/ublk-control` for device management
- Command sequence: ADD_DEV → SET_PARAMS → START_DEV → (serve) → STOP_DEV → DEL_DEV
- Feature negotiation happens at ADD_DEV - respect kernel's decisions

### Data Plane
- Per-queue io_uring with dedicated goroutine
- 2-state loop: FETCH_REQ → handle I/O → COMMIT_AND_FETCH_REQ
- mmapped descriptor array indexed by (queue_id, tag)
- Consider CPU affinity for queue threads

### io_uring Usage
- Use giouring or similar pure-Go library (behind abstraction)
- Enable SQE128/CQE32 when supported (probe first)
- IORING_OP_URING_CMD for passthrough commands

## Development Workflow

### Testing Strategy
- Unit tests for all packages
- Integration tests with `-tags=ublk` (require kernel support)
- Manual testing checklist for each backend
- Performance benchmarks against kernel loop device

### Debugging Approach
- Extensive logging behind debug flag
- Trace control/data plane operations
- Monitor with `blktrace` and `iostat`
- Check `dmesg` for kernel-side issues

## References to Consult
- Linux kernel docs: docs.kernel.org/block/ublk.html
- Kernel UAPI: include/uapi/linux/ublk_cmd.h
- io_uring man pages for URING_CMD
- ublksrv C implementation for behavior reference

## Known Challenges
- RLIMIT_MEMLOCK for buffer registration
- Write path variants (direct vs NEED_GET_DATA)
- Queue affinity and NUMA awareness
- Graceful degradation on older kernels

## Future Considerations (Not v1)
- User recovery modes
- Zoned block device support
- Advanced features like caching/writeback
- Integration with container runtimes

## CRITICAL DEVELOPMENT RULES

### Build and CLI Operations
**ALWAYS USE MAKE FOR CLI OPERATIONS**
- Never use `go build`, `go test`, `go run` directly
- Always use make targets: `make build`, `make test`, `make lint`, etc.
- This ensures consistent build flags, environment, and processes
- Check Makefile for available targets before running commands
- Example: Use `make test-unit` not `go test ./...`

**CRITICAL: Testing Commands**
- **NEVER run tests on local machine** - use VM only
- Use `make vm-e2e` for end-to-end VM testing (NOT `make test-vm`)
- Use `make test-unit` for unit tests (local machine OK)
- Other VM test variants: `make vm-e2e-64`, `make vm-e2e-80`, etc.
- Always build first: `make build` before VM testing

**Enhanced Debug Workflow**
- `make vm-reset` - Hard reset VM, remodprobe ublk, setup kernel tracing
- `make kernel-trace` - Read last 50 lines of kernel trace buffer
- `make vm-simple-e2e` - Simple single read/write test with max verbosity
- Always use vm-reset between test runs to ensure clean state

### Security Rules
**NEVER EVER HARDCODE PASSWORDS OR CREDENTIALS IN ANY FILES**
- Never put passwords in source code, scripts, or documentation
- Never commit credentials to version control
- Always use environment variables, config files, or prompt for credentials
- If you catch yourself about to hardcode a password, STOP IMMEDIATELY
- This is a firing offense in real development - treat it as such

### Development Helpers
**VM SSH Helper Script**
- Use `./vm-ssh.sh "command"` instead of typing the full sshpass command
- The script is gitignored and contains VM IP/password access
- Example: `./vm-ssh.sh "ls -la ublk-test/"` or `./vm-ssh.sh` for interactive shell
- Recreate if missing: 
  ```bash
  echo '#!/bin/bash' > vm-ssh.sh
  echo 'sshpass -p "$(cat /tmp/devvm_pwd.txt | tr -d '"'"'\n'"'"')" ssh -o StrictHostKeyChecking=no behrlich@192.168.4.79 "$@"' >> vm-ssh.sh
  chmod +x vm-ssh.sh
  ```