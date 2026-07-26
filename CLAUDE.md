# CLAUDE.md - Project Guidance for go-ublk

## Anchor Documents

- `README.md` - Project overview and usage
- `TODO.md` - Production roadmap
- `STYLE.md` - Code style and visual consistency rules
- `CLAUDE.md` - This file
- `docs/INTERNALS.md` - io_uring and ublk struct reference
- `docs/VM_TESTING.md` - VM test setup and troubleshooting

## Project Status: Prototype — approaching usable, not yet production-hardened

go-ublk is a pure Go, dependency-free implementation of Linux ublk (userspace block device).
See **`TODO.md` → Critical Bugs** for the full state before relying on this.

**Works (single- AND multi-queue):**
- Device lifecycle: ADD_DEV, SET_PARAMS, START_DEV, STOP_DEV, DEL_DEV
- Block I/O: Read, Write, Flush, Discard, Write-Zeroes. Flush reaches the backend only if the
  device advertises a volatile write cache (`VolatileCache`, now the fail-safe default), Discard
  only if the backend implements `DiscardBackend`, Write-Zeroes only `WriteZeroesBackend`.
- **Logical block sizes 512 through PAGE_SIZE**, including 4Kn; out-of-range params are
  rejected at Create rather than producing a device with the wrong capacity.
- **Multi-queue is now correct** on both arm64 and x86_64: the descriptor-mmap-offset bug that
  caused data corruption + unkillable D-state hangs is fixed (TODO Critical Bugs #1/#2).
  Verified Q=1/2/4/8, O_DIRECT, concurrent — 0 hangs / 0 mismatches. Honest O_DIRECT perf is
  ~1.37M IOPS 4K randread / 816k randwrite (RAM backend, ublk-path ceiling).
- `ublk-mem --del=all` reaps stuck/zombie devices; failed startup tears down cleanly.
- Crash / power-fail consistency is tested (`make vm-crash`, `make vm-powerfail`): 8
  SIGKILL-mid-write cycles and 4 hard resets mid-write, 0 lost / 0 torn / 0 aliased, clean
  host recovery each time. See TODO.md → Phase 2.

**Still open before prod:**
- Host power cut (not just a guest `sysrq-b`) — the host's cache of the VM disk is untested.
- Host-reboot-under-load (systemd shutdown storm while a device serves I/O).
- Per-IO FUA; `UBLK_F_USER_RECOVERY`; no CI.

## Build and Test Commands

**ALWAYS USE MAKE FOR CLI OPERATIONS**

```bash
# Build
make build              # Build all binaries

# Unit tests. The code is Linux-only (io_uring), so test-unit does not even
# compile on macOS — cross-compile and run them on the VM instead.
make test-unit          # Run unit tests (on Linux)
make vm-test-unit       # Cross-compile the unit tests and run them on the VM

# VM tests (requires VM setup)
make vm-reset           # Hard reset VM state
make vm-simple-e2e      # Basic I/O test
make vm-e2e             # Full test suite
make vm-crash           # Crash consistency: SIGKILL daemon mid-write, recover, verify
make vm-powerfail       # Power-fail consistency: sysrq hard reset mid-write, then verify
make vm-benchmark       # Performance benchmark
make vm-stress          # 10x alternating e2e + benchmark
```

**Never use go build/test/run directly** - use make targets.

## Architecture Overview

```
go-ublk/
├── *.go               # Public API (ublk package)
├── examples/ublk-mem/  # RAM-backed device example (--zip = compressed)
├── examples/ublk-loop/ # File-backed device example (losetup-style)
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
2. Run `make test-unit` before committing (`make vm-test-unit` from a macOS box)
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
