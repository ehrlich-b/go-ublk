# CLAUDE.md - Project Guidance for go-ublk

## Anchor Documents

- `README.md` - Project overview and usage
- `TODO.md` - Production roadmap
- `STYLE.md` - Code style and visual consistency rules
- `CLAUDE.md` - This file
- `docs/INTERNALS.md` - io_uring and ublk struct reference
- `docs/VM_TESTING.md` - VM test setup and troubleshooting

## Project Status: Stable Working Prototype

go-ublk is a pure Go, dependency-free implementation of Linux ublk (userspace block device).

**Verified working:**
- Device lifecycle: ADD_DEV, SET_PARAMS, START_DEV, STOP_DEV, DEL_DEV
- Block I/O: Read, Write, Flush, Discard
- Multi-queue: 4 queues with batched io_uring submissions
- Performance: ~100k IOPS (85-91% of kernel loop device)
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
├── *.go               # Public API (ublk package)
├── examples/ublk-mem/ # Memory-backed device example
├── docs/              # Documentation
├── scripts/           # VM test scripts
├── test/              # Unit and integration tests
└── internal/
    ├── ctrl/          # Control plane (device lifecycle)
    ├── queue/         # Data plane (I/O processing)
    ├── uring/         # io_uring implementation
    ├── uapi/          # Kernel UAPI structs
    ├── interfaces/    # Internal interfaces (Backend)
    ├── logging/       # Structured logger
    └── constants/     # Shared constants
```

**Key design decisions:**
- **Pure Go** - no cgo, no external dependencies, builds with `CGO_ENABLED=0`
- io_uring stays internal (tightly coupled to ublk's URING_CMD requirements)
- Multi-queue with sharded memory backend for parallelism

## Critical Files

| File | Purpose |
|------|---------|
| `internal/uring/minimal.go` | io_uring implementation (EINTR handling, memory barriers) |
| `internal/queue/runner.go` | Queue state machine (FETCH_REQ / COMMIT_AND_FETCH) |
| `internal/uapi/structs.go` | Kernel UAPI structures |
| `internal/ctrl/control.go` | Device lifecycle management |

## Development Workflow

1. Check `TODO.md` for current roadmap and priorities
2. Run `make test-unit` before committing
3. Use `make vm-e2e` to verify I/O functionality
4. Use `make vm-stress` to verify stability after significant changes

## Technical Constraints

- Linux kernel >= 6.8 (IOCTL encoding required)
- io_uring with URING_CMD support required
- Device creation requires root or CAP_SYS_ADMIN

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
- Project internals: docs/INTERNALS.md
