# Contributing to go-ublk

## Development Setup

### Prerequisites

1. Linux system with kernel ≥ 6.1
2. Go 1.22 or later
3. Root access or sudo for testing
4. Development tools:
   ```bash
   # Ubuntu/Debian
   sudo apt-get install build-essential linux-headers-$(uname -r)
   
   # Fedora/RHEL
   sudo dnf install kernel-devel kernel-headers
   ```

### Initial Setup

```bash
# Clone the repository
git clone https://github.com/[user]/go-ublk.git
cd go-ublk

# Install dependencies
go mod download

# Install development tools
make dev-tools

# Verify kernel support
make check-kernel

# Run tests (requires root)
sudo make test
```

## Development Workflow

### Code Organization

- `/ublk` - Public API (keep minimal and stable)
- `/internal` - Implementation details (can change freely)
- `/cmd` - CLI tools
- `/test` - Integration tests
- `/bench` - Performance benchmarks

### Making Changes

1. Create a feature branch
2. Make changes following Go conventions
3. Add/update tests
4. Run checks: `make check`
5. Test manually with demo programs
6. Update documentation if needed

### Testing

```bash
# Unit tests
go test ./...

# Integration tests (requires ublk support)
sudo go test -tags=ublk ./test/...

# Specific backend test
sudo go test -v ./ublk/backend/mem

# Benchmarks
sudo go test -bench=. ./bench/...
```

### Code Style

- Follow standard Go conventions
- Use `gofmt` and `goimports`
- Run `golangci-lint` before committing
- Keep lines under 100 characters
- Comment exported functions
- Use meaningful variable names

### Commit Messages

Format:
```
component: brief description

Longer explanation if needed.

Fixes #123
```

Examples:
- `ublk/queue: fix race in descriptor allocation`
- `backend/file: add O_DIRECT support`
- `cmd: add --version flag to all tools`

## Testing Guidelines

### Manual Testing Checklist

Before submitting PRs, test:

1. **Basic functionality**
   ```bash
   # Create device
   sudo ./ublk-mem --size=100M
   
   # Create filesystem
   sudo mkfs.ext4 /dev/ublkb0
   
   # Mount and test
   sudo mount /dev/ublkb0 /mnt
   echo "test" | sudo tee /mnt/test.txt
   sudo umount /mnt
   ```

2. **Signal handling**
   - Start device, Ctrl+C, verify cleanup
   - Start device, `kill -TERM`, verify cleanup

3. **Error cases**
   - Invalid parameters
   - Missing kernel support
   - Resource exhaustion

4. **Performance**
   ```bash
   # Basic I/O test
   sudo fio --name=test --filename=/dev/ublkb0 \
            --size=1G --bs=4k --rw=randrw
   ```

### Debugging

Enable debug logging:
```bash
UBLK_DEBUG=1 sudo ./ublk-mem --size=1G
```

Monitor kernel messages:
```bash
sudo dmesg -w | grep ublk
```

Trace block I/O:
```bash
sudo blktrace -d /dev/ublkb0
```

## Architecture Decisions

### Why Pure Go?

- Easier deployment (single binary)
- Better integration with Go ecosystem
- Simpler build process
- Good enough performance for most use cases

### Why io_uring?

- Zero-copy I/O operations
- High performance async I/O
- Native kernel support for ublk
- Better than epoll/select for this use case

### Design Principles

1. **Correctness over performance** - Get it working right first
2. **Clean abstractions** - Hide complexity from users
3. **Fail fast** - Detect problems early
4. **Resource cleanup** - Always clean up, even on panic

## Submitting Pull Requests

1. Fork the repository
2. Create a feature branch
3. Make your changes
4. Add tests for new functionality
5. Ensure all tests pass
6. Update documentation
7. Submit PR with clear description

### PR Checklist

- [ ] Tests pass (`make test`)
- [ ] Code formatted (`make fmt`)
- [ ] Linter passes (`make lint`)
- [ ] Documentation updated
- [ ] Commits are logical units
- [ ] PR description explains changes

## Reporting Issues

Include:
- Kernel version (`uname -r`)
- Go version (`go version`)
- Steps to reproduce
- Expected vs actual behavior
- Any error messages
- dmesg output if relevant

## Performance Contributions

When submitting performance improvements:
- Include benchmark results (before/after)
- Test on multiple kernel versions if possible
- Document any trade-offs
- Consider impact on different backends

## Security

- Never log sensitive data
- Validate all user inputs
- Handle privilege transitions carefully
- Report security issues privately first

## Questions?

- Open a discussion on GitHub
- Check existing issues first
- Provide context and examples
- Be patient and respectful