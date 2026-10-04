# CLAUDE.md - Project Guidance for go-ublk

## Anchor Documents

- `README.md` - Project overview and usage
- `TODO.md` - Production roadmap
- `STYLE.md` - Code style and visual consistency rules
- `CLAUDE.md` - This file
- `docs/INTERNALS.md` - io_uring and ublk struct reference
- `docs/VM_TESTING.md` - VM test setup and troubleshooting

## Project Status: v0.2.0 overhaul (2026-10-04) — production-oriented, verified across a kernel matrix

go-ublk implements Linux ublk in Go without cgo or liburing. It uses `golang.org/x/sys` for Linux syscalls.
See **`TODO.md` → Critical Bugs** for every defect found and its status, and `docs/INTERNALS.md` for the
architecture.

- **Engine:** one locked OS thread + io_uring per queue (or tag range); requests run on a goroutine each
  (or inline); completions return through an eventfd armed in the ring. Tested against a fake-kernel model
  of ublk_drv (`internal/queue/fakekernel_test.go`) and fuzzed (`FuzzEngine`).
- **Kernel surface:** every command, feature and parameter block of the 7.3-rc5 UAPI is implemented:
  recovery (Detach/Recover), zero copy, shared-memory zero copy, batch I/O, zoned, integrity, user copy,
  NEED_GET_DATA, unprivileged, per-I/O daemons, resize, safe stop, IO_DESC_SIZE.
- **Testing:** `make suite` builds the real-kernel conformance suite; `test/matrix` boots it under many
  kernels/distros (QEMU, TCG on the WSL rig, KVM in CI). Results: `site/data/matrix.json`.
- **Docs site:** `site/` (Hugo), published at ublk.ehrlich.dev via `site/deploy.sh`.
- **Deployment:** run the daemon as a systemd unit with the mount ordered after it (`examples/systemd/`);
  with `-recovery`, crashes and upgrades keep the device and its mount (TODO Critical Bug #15).
- **Minimum kernel:** 6.4 (ioctl-encoded commands); features need newer kernels and are negotiated.

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
- **Pure Go** - no cgo or liburing, uses `golang.org/x/sys`, builds with `CGO_ENABLED=0`
- io_uring stays internal (tightly coupled to ublk's URING_CMD requirements)
- Multi-queue with sharded memory backend for parallelism

## Critical Files

| File | Purpose |
|------|---------|
| `internal/queue/engine.go` | Per-tag state machine, dispatch, data modes (copy, user copy, zero copy, batch) |
| `internal/queue/queue.go` | Per-queue mappings and engine lifecycle |
| `internal/uring/ring.go` | io_uring core (memory ordering, submit/wait) |
| `internal/ctrl/commands.go` | One method per control command |
| `internal/uapi/structs.go` | Kernel UAPI structures |
| `backend.go`, `recover.go` | Public lifecycle, recovery |

## Development Workflow

1. Check `TODO.md` for current roadmap and priorities
2. Run `make test-unit` before committing (`make vm-test-unit` from a macOS box)
3. Use `make vm-e2e` to verify I/O functionality
4. Use `make vm-stress` to verify stability after significant changes

## Technical Constraints

- Linux kernel >= 6.4 (ioctl-encoded commands); newer features are negotiated per device
- io_uring with URING_CMD support required
- Device creation requires root or CAP_SYS_ADMIN

## Security Rules

- **NEVER hardcode passwords or credentials**
- Use environment variables or prompt for sensitive data
- Never commit secrets to version control

## VM Helper

There is no `scripts/vm-ssh.sh`. The VM transport lives in `Makefile.local` (gitignored) as
`VM_SSH` / `VM_SCP`; every `vm-*` target goes through it. To reach a VM by hand:

```bash
# The arm64 Lima VM used by the vm-* targets
ssh -F ~/.lima/ublk/ssh.config lima-ublk "command"
limactl shell ublk                      # interactive

# A second VM on a different kernel: override the transport on the make line.
# This is how the linux-hwe-7.0 (noble) box is driven.
make vm-verify \
  VM_HOST=lima-ublk-noble VM_USER=ehrlich \
  VM_SSH='ssh -F $HOME/.lima/ublk-noble/ssh.config lima-ublk-noble' \
  VM_SCP='scp -F $HOME/.lima/ublk-noble/ssh.config'
```

## References

- Linux kernel docs: docs.kernel.org/block/ublk.html
- Kernel UAPI: include/uapi/linux/ublk_cmd.h
- Project internals: docs/INTERNALS.md
