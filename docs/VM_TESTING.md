# VM Testing

VM-based integration tests for go-ublk. Required because ublk needs root + kernel 6.8+.

## VM Setup

**Requirements:**
- Ubuntu 24.04+ (kernel 6.8+)
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
