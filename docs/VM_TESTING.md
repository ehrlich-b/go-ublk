# VM Testing

VM-based integration tests for go-ublk. Required because ublk needs root and a kernel with `ublk_drv` (6.4+).

## VM Setup

**Requirements:**
- Ubuntu 24.04+ (any kernel 6.4+ works; 7.0+ exercises every feature but shared memory and IO_DESC_SIZE)
- 2GB+ RAM
- `ublk_drv` module: `sudo modprobe ublk_drv`

**SSH config:** Create `Makefile.local`:
```makefile
VM_HOST = 192.168.x.x
VM_USER = youruser
VM_PASS = yourpass
```

Or use environment variables: `UBLK_VM_HOST`, `UBLK_VM_USER`, `UBLK_VM_PASS`

## Test Commands

```bash
make vm-simple-e2e    # Basic I/O test
make vm-e2e           # Full test suite
make vm-benchmark     # Performance benchmark
make vm-stress        # 10x stress test
make vm-reset         # Hard reset VM
```

Race detector: `RACE=1 make vm-e2e`

Durability and deployment tests (each documents what it can and cannot prove at
the top of its script):

```bash
make vm-crash           # SIGKILL the daemon mid-write, recover, verify
make vm-powerfail       # sysrq hard reset mid-write, then verify after reboot
make vm-shutdown-storm  # reboot under load with a mounted fs (STORM_ARM=arm-unit for the systemd control)
make vm-soak            # hours of verified fs I/O via the shipped systemd units, with crashes, handoffs and a leak check
```

## Conformance Suite

`make suite` builds `bin/ublk-suite`, the real-kernel conformance suite (about
60 tests: data integrity, every feature, recovery, teardown under load, chaos).
Copy it to a disposable guest and run it as root:

```bash
sudo ./ublk-suite                       # everything; one JSON result per line on stdout
sudo ./ublk-suite -run '^recovery/'     # a subset, by regexp
sudo ./ublk-suite -scale 0.3            # shorter runs
```

Tests that need a newer kernel skip with the missing feature named. The kernel
matrix in `test/matrix` boots the same binary under many kernels; see its README.

## Disposable Large-I/O Regression

The focused real-kernel regression is intentionally opt-in and must run only in
a disposable guest that already has `/dev/ublk-control` and sufficient
privilege:

```bash
GO_UBLK_DISPOSABLE_TEST=1 make test-large-io-kernel
```

It does not load modules, change kernel policy, enumerate devices, or delete
unrelated devices. It creates two auto-assigned memory-backed devices in
sequence, tests both public startup paths, and cleans up only those device IDs
through `Device.Close`. Compile it without running it with:

```bash
make test-large-io-kernel-compile
```

## Troubleshooting

| Problem | Solution |
|---------|----------|
| Connection refused | Check VM IP and SSH |
| Module not found | `sudo modprobe ublk_drv` |
| Device creation fails | Check `dmesg \| tail -20` |
| Test hangs | `make vm-reset` |
