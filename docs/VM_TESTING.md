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

## Troubleshooting

| Problem | Solution |
|---------|----------|
| Connection refused | Check VM IP and SSH |
| Module not found | `sudo modprobe ublk_drv` |
| Device creation fails | Check `dmesg \| tail -20` |
| Test hangs | `make vm-reset` |
