# CLAUDE.md - Project-Specific Guidance for go-ublk

## Project Overview
Pure Go implementation of Linux ublk (userspace block driver) framework. No cgo dependencies.

## 🚀 CURRENT STATUS: MAJOR MILESTONE ACHIEVED!

**Phase 1-3 COMPLETE**: Core ublk implementation working on real kernels!

### Recent Achievements ✅
- **Complete Control Plane**: Device lifecycle management (ADD_DEV → SET_PARAMS → START_DEV → serve → STOP_DEV → DEL_DEV)
- **Full Data Plane**: Real I/O processing with io_uring URING_CMD operations
- **Kernel Integration**: Successfully creates `/dev/ublkb0` devices on Linux 6.11
- **Production Architecture**: Clean separation of control/data planes, proper error handling
- **Automated Testing**: VM testing infrastructure with real kernel validation
- **Memory Backend**: Complete implementation with CLI tool (`ublk-mem`)

### What Works Now ✅
- Device creation and deletion
- I/O request processing (READ/WRITE/FLUSH/DISCARD)  
- Queue runner management with goroutines
- Memory-mapped descriptor arrays
- Complete device lifecycle
- Graceful shutdown and cleanup
- Automated VM testing with real kernels

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

### Security Rules
**NEVER EVER HARDCODE PASSWORDS OR CREDENTIALS IN ANY FILES**
- Never put passwords in source code, scripts, or documentation
- Never commit credentials to version control
- Always use environment variables, config files, or prompt for credentials
- If you catch yourself about to hardcode a password, STOP IMMEDIATELY
- This is a firing offense in real development - treat it as such