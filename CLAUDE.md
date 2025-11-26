# CLAUDE.md - Project Guidance for go-ublk

## Anchor Documents

- `README.md` - Project overview and usage
- `TODO.md` - Production roadmap and cleanup tasks
- `STYLE.md` - Code style and visual consistency rules
- `CLAUDE.md` - This file
- `docs/REVIEW.md` - Detailed code review with cleanup recommendations

## Project Status: Stable Working Prototype

go-ublk is a pure Go implementation of Linux ublk (userspace block device).

**Verified working:**
- Device lifecycle: ADD_DEV, SET_PARAMS, START_DEV, STOP_DEV, DEL_DEV
- Block I/O: Read, Write, Flush, Discard
- Performance: ~500k IOPS, 1.6-2.0 GB/s throughput
- Stability: Passes 10x stress test cycles

## Build and Test Commands

**ALWAYS USE MAKE FOR CLI OPERATIONS**

```bash
# Build
make build              # Build all binaries

# Unit tests (local machine)
make test-unit          # Run unit tests

# VM tests (requires VM setup)
make vm-reset           # Hard reset VM state
make vm-simple-e2e      # Basic I/O test
make vm-e2e             # Full test suite
make vm-benchmark       # Performance benchmark
make vm-stress          # 10x alternating e2e + benchmark
```

**Never use go build/test/run directly** - use make targets.

## Architecture Overview

```
go-ublk/
├── *.go              # Public API (ublk package)
├── backend/          # Backend implementations (mem.go)
├── cmd/ublk-mem/     # Memory-backed device CLI
├── docs/             # Documentation (REVIEW.md, etc.)
├── scripts/          # Shell scripts (vm-ssh.sh, etc.)
└── internal/
    ├── ctrl/         # Control plane (device lifecycle)
    ├── queue/        # Data plane (I/O processing, runner.go)
    ├── uring/        # io_uring implementation
    └── uapi/         # Kernel UAPI structs
```

**Key design decisions:**
- **Pure Go** - no cgo, builds with `CGO_ENABLED=0`
- io_uring stays internal (see TODO.md section 1.1 for rationale)
- Single queue currently, multi-queue planned

## Critical Files

| File | Purpose |
|------|---------|
| `internal/uring/minimal.go` | io_uring implementation (EINTR handling, memory barriers) |
| `internal/queue/runner.go` | Queue state machine (FETCH_REQ / COMMIT_AND_FETCH) |
| `internal/uapi/structs.go` | Kernel UAPI structures |
| `internal/ctrl/control.go` | Device lifecycle management |

## Development Workflow

1. Read `docs/REVIEW.md` for known issues and cleanup tasks
2. Run `make test-unit` before committing
3. Use `make vm-e2e` to verify I/O functionality
4. Use `make vm-stress` to verify stability after significant changes

## Technical Constraints

- Linux kernel >= 6.1 (ublk was introduced)
- io_uring with URING_CMD support required
- Device creation requires root or CAP_SYS_ADMIN

## Known Issues

**Slow initialization:** Device takes ~9 seconds to initialize (queue_depth * 250ms). This is a setup-time issue only, not a runtime issue. Low priority to fix.

## Security Rules

- **NEVER hardcode passwords or credentials**
- Use environment variables or prompt for sensitive data
- Never commit secrets to version control

## VM Helper

```bash
# SSH to test VM
scripts/vm-ssh.sh "command"     # Run command
scripts/vm-ssh.sh               # Interactive shell
```

## References

- Linux kernel docs: docs.kernel.org/block/ublk.html
- Kernel UAPI: include/uapi/linux/ublk_cmd.h
- io_uring man pages for URING_CMD
