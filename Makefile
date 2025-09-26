# Makefile for go-ublk

# Go parameters
GOCMD=go
GOBUILD=$(GOCMD) build
TAGS?=
GOCLEAN=$(GOCMD) clean
GOTEST=$(GOCMD) test
GOGET=$(GOCMD) get
GOMOD=$(GOCMD) mod

# Project info
BINARY_NAME=ublk
BINARY_DIR=./cmd

# Build targets
BINARIES=ublk-mem ublk-file ublk-null ublk-zip

# Test parameters
TEST_FLAGS=-v
INTEGRATION_FLAGS=-tags=integration
UNIT_FLAGS=-tags=!integration

.PHONY: all build clean test test-unit test-integration setup-vm test-vm benchmark deps tidy lint check-kernel help vm-reset kernel-trace vm-simple-e2e

# Default target
all: deps build test

# Build all binaries (FORCE ensures always rebuild)
build: FORCE $(BINARIES)

# Individual binary targets (FORCE ensures always rebuild)
ublk-mem: FORCE
	@echo "Building ublk-mem..."
	@CGO_ENABLED=0 $(GOBUILD) -o ublk-mem ./cmd/ublk-mem

ublk-file: FORCE
	@echo "Building ublk-file (Phase 4)"

ublk-null: FORCE
	@echo "Building ublk-null (Phase 4)"

ublk-zip: FORCE
	@echo "Building ublk-zip (Phase 4)"

# Clean build artifacts
clean:
	$(GOCLEAN)
	rm -f $(BINARIES)

# Run all tests
test: test-unit
	@echo "All tests completed"

# Run unit tests only (no kernel dependencies)
test-unit:
	@echo "Running unit tests..."
	$(GOTEST) $(TEST_FLAGS) ./...
	$(GOTEST) $(TEST_FLAGS) $(UNIT_FLAGS) ./test/unit/...

# Run integration tests (requires ublk kernel support and root)
test-integration:
	@echo "Running integration tests (requires root and ublk kernel support)..."
	@if [ "$$(id -u)" != "0" ]; then \
		echo "Integration tests require root privileges"; \
		echo "Run: sudo make test-integration"; \
		exit 1; \
	fi
	$(GOTEST) $(TEST_FLAGS) $(INTEGRATION_FLAGS) ./test/integration/...

# Set up VM for passwordless sudo (run once)
setup-vm:
	@echo "🔧 Setting up VM for passwordless sudo..."
	@PASSWORD=$$(cat /tmp/devvm_pwd.txt) && \
		sshpass -p "$$PASSWORD" ssh -o StrictHostKeyChecking=no behrlich@192.168.4.79 \
		"echo \"$$PASSWORD\" | sudo -S bash -c 'echo \"# ublk dev rules\" > /etc/sudoers.d/ublk-dev'"
	@PASSWORD=$$(cat /tmp/devvm_pwd.txt) && \
		sshpass -p "$$PASSWORD" ssh -o StrictHostKeyChecking=no behrlich@192.168.4.79 \
		"echo \"$$PASSWORD\" | sudo -S bash -c 'echo \"behrlich ALL=(ALL) NOPASSWD: /usr/sbin/modprobe ublk_drv\" >> /etc/sudoers.d/ublk-dev'"
	@PASSWORD=$$(cat /tmp/devvm_pwd.txt) && \
		sshpass -p "$$PASSWORD" ssh -o StrictHostKeyChecking=no behrlich@192.168.4.79 \
		"echo \"$$PASSWORD\" | sudo -S bash -c 'echo \"behrlich ALL=(ALL) NOPASSWD: /usr/sbin/modprobe -r ublk_drv\" >> /etc/sudoers.d/ublk-dev'"
	@PASSWORD=$$(cat /tmp/devvm_pwd.txt) && \
		sshpass -p "$$PASSWORD" ssh -o StrictHostKeyChecking=no behrlich@192.168.4.79 \
		"echo \"$$PASSWORD\" | sudo -S bash -c 'echo \"behrlich ALL=(ALL) NOPASSWD: /home/behrlich/ublk-test/ublk-mem\" >> /etc/sudoers.d/ublk-dev'"
	@echo "✓ Passwordless sudo configured for ublk operations"

# Test on VM (requires SSH access to 192.168.4.79)
test-vm: 
	@echo "🚀 Testing go-ublk on VM..."
	@echo "Building ublk-mem binary..."
	@$(GOBUILD) -o ublk-mem ./cmd/ublk-mem
	@echo "Copying files to VM..."
	@mkdir -p build
	@cp ublk-mem test-vm.sh build/
	@echo "Creating remote directory and copying files..."
	@sshpass -p "$$(cat /tmp/devvm_pwd.txt)" ssh -o StrictHostKeyChecking=no behrlich@192.168.4.79 "mkdir -p ~/ublk-test"
	@sshpass -p "$$(cat /tmp/devvm_pwd.txt)" scp -o StrictHostKeyChecking=no build/ublk-mem build/test-vm.sh behrlich@192.168.4.79:~/ublk-test/
	@echo "✓ Files copied successfully"
	@echo ""
	@echo "🧪 Running tests on VM..."
	@echo "=============================================="
	@sshpass -p "$$(cat /tmp/devvm_pwd.txt)" ssh -o StrictHostKeyChecking=no behrlich@192.168.4.79 'cd ~/ublk-test && chmod +x test-vm.sh && ./test-vm.sh' || \
		(echo ""; echo "❌ VM test failed"; echo "Try running: make setup-vm"; exit 1)
	@echo "=============================================="
	@echo "✅ VM test completed!"
	@echo ""
	@echo "🎉 If you saw 'Device created: /dev/ublkb0' above,"
	@echo "   then the go-ublk control plane is working!"

# --- VM Convenience Targets ---
.PHONY: vm-copy vm-run vm-stop vm-e2e

VM_HOST=192.168.4.79
VM_USER=behrlich
VM_DIR=~/ublk-test
VM_PASS=$(shell cat /tmp/devvm_pwd.txt)

vm-copy: ublk-mem
	@echo "📦 Copying ublk-mem and tests to VM..."
	@sshpass -p "$(VM_PASS)" ssh -o StrictHostKeyChecking=no $(VM_USER)@$(VM_HOST) "mkdir -p $(VM_DIR); sudo killall ublk-mem 2>/dev/null || true; rm -f $(VM_DIR)/ublk-mem"
	@sshpass -p "$(VM_PASS)" scp -o StrictHostKeyChecking=no ublk-mem test-e2e.sh $(VM_USER)@$(VM_HOST):$(VM_DIR)/
	@echo "✓ Copied."

vm-run: ublk-mem vm-copy
	@echo "🚀 Running ublk-mem on VM (10s)..."
	@sshpass -p "$(VM_PASS)" ssh -o StrictHostKeyChecking=no $(VM_USER)@$(VM_HOST) \
		"cd $(VM_DIR) && sudo timeout 10 ./ublk-mem --size=16M -v || true; ls -la /dev/ublk* || true"

vm-stop:
	@echo "🛑 Stopping ublk-mem on VM (best-effort)..."
	@sshpass -p "$(VM_PASS)" ssh -o StrictHostKeyChecking=no $(VM_USER)@$(VM_HOST) \
		"sudo killall ublk-mem 2>/dev/null || true"

vm-e2e: ublk-mem vm-copy
	@echo "🧪 Running e2e I/O test on VM..."
	@sshpass -p "$(VM_PASS)" ssh -o StrictHostKeyChecking=no $(VM_USER)@$(VM_HOST) \
		"set -e; cd $(VM_DIR) && chmod +x ./test-e2e.sh && ./test-e2e.sh"
	@echo "✅ VM e2e test completed"

vm-benchmark: ublk-mem vm-copy
	@echo "📊 Running baseline performance benchmark on VM..."
	@sshpass -p "$(VM_PASS)" scp -o StrictHostKeyChecking=no test-benchmark.sh $(VM_USER)@$(VM_HOST):$(VM_DIR)/
	@sshpass -p "$(VM_PASS)" ssh -o StrictHostKeyChecking=no $(VM_USER)@$(VM_HOST) \
		"set -e; cd $(VM_DIR) && chmod +x ./test-benchmark.sh && ./test-benchmark.sh"
	@echo "✅ VM benchmark completed"

.PHONY: vm-e2e-80 vm-e2e-64 vm-e2e-80-raw vm-e2e-64-raw vm-run-env
vm-e2e-80: ublk-mem vm-copy
	@echo "🧪 Running e2e with UBLK_DEVINFO_LEN=80 ..."
	@sshpass -p "$(VM_PASS)" ssh -o StrictHostKeyChecking=no $(VM_USER)@$(VM_HOST) \
		"set -e; cd $(VM_DIR) && chmod +x ./test-e2e.sh && UBLK_DEVINFO_LEN=80 ./test-e2e.sh"

vm-e2e-64: ublk-mem vm-copy
	@echo "🧪 Running e2e with UBLK_DEVINFO_LEN=64 ..."
	@sshpass -p "$(VM_PASS)" ssh -o StrictHostKeyChecking=no $(VM_USER)@$(VM_HOST) \
		"set -e; cd $(VM_DIR) && chmod +x ./test-e2e.sh && UBLK_DEVINFO_LEN=64 ./test-e2e.sh"

vm-e2e-80-raw: ublk-mem vm-copy
	@echo "🧪 Running e2e with UBLK_DEVINFO_LEN=80 + raw ctrl encoding..."
	@sshpass -p "$(VM_PASS)" ssh -o StrictHostKeyChecking=no $(VM_USER)@$(VM_HOST) \
		"set -e; cd $(VM_DIR) && chmod +x ./test-e2e.sh && UBLK_CTRL_ENC=raw UBLK_DEVINFO_LEN=80 ./test-e2e.sh"

vm-e2e-64-raw: ublk-mem vm-copy
	@echo "🧪 Running e2e with UBLK_DEVINFO_LEN=64 + raw ctrl encoding..."
	@sshpass -p "$(VM_PASS)" ssh -o StrictHostKeyChecking=no $(VM_USER)@$(VM_HOST) \
		"set -e; cd $(VM_DIR) && chmod +x ./test-e2e.sh && UBLK_CTRL_ENC=raw UBLK_DEVINFO_LEN=64 ./test-e2e.sh"

# Run ublk-mem on VM with custom environment variables
# Usage: make vm-run-env ENV="UBLK_CTRL_ENC=raw UBLK_DEVINFO_LEN=64"
vm-run-env: ublk-mem vm-copy
	@echo "🚀 Running ublk-mem on VM with custom env: $(ENV)"
	@sshpass -p "$(VM_PASS)" ssh -o StrictHostKeyChecking=no $(VM_USER)@$(VM_HOST) \
		"cd $(VM_DIR) && sudo env $(ENV) timeout 15 ./ublk-mem --size=16M -v || true; ls -la /dev/ublk* || true"

.PHONY: vm-enable-logs vm-dump-logs
vm-enable-logs:
	@echo "🪵 Enabling maximal kernel logging on VM (io_uring + ublk + tracing)..."
	@./vm-ssh.sh 'bash -s' < scripts/vm-enable-logs.sh

vm-dump-logs:
	@echo "📤 Dumping kernel logs and tracing buffers (last ~2k lines)..."
	@./vm-ssh.sh 'bash -s' < scripts/vm-dump-logs.sh

.PHONY: vm-debug vm-fetch-latest-logs
vm-debug: ublk-mem vm-copy
	@echo "🧪 Running deep debug on VM (strace + kernel tracing) ..."
	@sshpass -p "$(VM_PASS)" scp -o StrictHostKeyChecking=no scripts/vm-debug-run.sh $(VM_USER)@$(VM_HOST):~/ublk-test/
	@./vm-ssh.sh 'bash -lc "cd ~/ublk-test && chmod +x ./vm-debug-run.sh && ./vm-debug-run.sh"'

vm-fetch-latest-logs:
	@echo "📥 Fetching latest ublk-debug archive from VM home..."
	@./vm-ssh.sh 'bash -lc "ls -1t ~ | grep ^ublk-debug- | head -1"' > build/.last_log 2>/dev/null || true
	@[ -s build/.last_log ] && echo "Latest: $$(cat build/.last_log)" || (echo "No log archives found" && exit 1)
	@sshpass -p "$(VM_PASS)" scp -o StrictHostKeyChecking=no $(VM_USER)@$(VM_HOST):~/$$(cat build/.last_log) build/
	@echo "✓ Logs downloaded to build/$$(cat build/.last_log)"

.PHONY: vm-install-go
vm-install-go:
	@echo "🛠️  Installing Go toolchain and build deps on VM..."
	@./vm-ssh.sh 'bash -lc "sudo DEBIAN_FRONTEND=noninteractive apt-get -o Acquire::Check-Valid-Until=false -o Acquire::AllowInsecureRepositories=true update -y && sudo DEBIAN_FRONTEND=noninteractive apt-get install -y golang-go build-essential linux-libc-dev"'
	@./vm-ssh.sh 'bash -lc "go version || echo go not found"'
	@echo "✓ VM Go installation step attempted"

.PHONY: vm-fix-time
vm-fix-time:
	@echo "🕒 Attempting to sync VM time for apt validity..."
	@./vm-ssh.sh 'bash -lc "sudo timedatectl set-ntp true || true; sudo systemctl restart systemd-timesyncd || true; sleep 3; timedatectl || true"'
	@echo "✓ VM time sync attempted"

.PHONY: vm-src-copy vm-build-e2e
vm-src-copy:
	@echo "📦 Archiving and copying source to VM..."
	@mkdir -p build
	@tar --exclude='./build' -czf build/ublk-src.tgz .
	@sshpass -p "$(VM_PASS)" scp -o StrictHostKeyChecking=no build/ublk-src.tgz $(VM_USER)@$(VM_HOST):~/
	@./vm-ssh.sh 'bash -lc "rm -rf ~/ublk-src && mkdir -p ~/ublk-src && tar -xzf ~/ublk-src.tgz -C ~/ublk-src"'
	@echo "✓ Source copied to VM"

vm-build-e2e: vm-src-copy
	@echo "🧰 Building on VM with cgo to use VM kernel headers..."
	@./vm-ssh.sh 'bash -lc "cd ~/ublk-src && CGO_ENABLED=1 go build -o ublk-mem ./cmd/ublk-mem"'
	@./vm-ssh.sh 'bash -lc "cd ~/ublk-src && chmod +x ./test-e2e.sh && sudo ./test-e2e.sh"'

# Run benchmarks
benchmark:
	@echo "Running benchmarks..."
	$(GOTEST) -bench=. -benchmem ./...

# Install/update dependencies
deps:
	$(GOGET) -v ./...
	$(GOMOD) download

# Tidy dependencies
tidy:
	$(GOMOD) tidy

# Lint code (requires golangci-lint)
lint:
	@which golangci-lint > /dev/null || (echo "golangci-lint not found, install with: go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest"; exit 1)
	golangci-lint run

# Format code
fmt:
	$(GOCMD) fmt ./...

# Vet code
vet:
	$(GOCMD) vet ./...

# Check if ublk kernel support is available
check-kernel:
	@echo "Checking ublk kernel support..."
	@if [ -e /dev/ublk-control ]; then \
		echo "✓ /dev/ublk-control exists"; \
	else \
		echo "✗ /dev/ublk-control not found"; \
		echo "  Make sure ublk_drv module is loaded: sudo modprobe ublk_drv"; \
	fi
	@if lsmod | grep -q ublk_drv; then \
		echo "✓ ublk_drv module is loaded"; \
	else \
		echo "✗ ublk_drv module not loaded"; \
		echo "  Load with: sudo modprobe ublk_drv"; \
	fi

# Check module info
check-module:
	@echo "Checking ublk module information..."
	@if modinfo ublk_drv >/dev/null 2>&1; then \
		echo "Module information:"; \
		modinfo ublk_drv | head -10; \
	else \
		echo "ublk_drv module not available"; \
	fi

# Development helpers
dev-setup: deps
	@echo "Setting up development environment..."
	@if ! which golangci-lint >/dev/null 2>&1; then \
		echo "Installing golangci-lint..."; \
		go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest; \
	fi

# Full check (formatting, vetting, linting, testing)
check: fmt vet lint test

# Coverage report
coverage:
	@echo "Generating coverage report..."
	$(GOTEST) -coverprofile=coverage.out ./...
	$(GOCMD) tool cover -html=coverage.out -o coverage.html
	@echo "Coverage report generated: coverage.html"

# Race condition testing
test-race:
	@echo "Running tests with race detector..."
	$(GOTEST) $(TEST_FLAGS) -race ./...

# Help target
help:
	@echo "Available targets:"
	@echo "  all             - Build and test everything"
	@echo "  build           - Build all binaries"
	@echo "  clean           - Clean build artifacts"
	@echo "  test            - Run all tests"
	@echo "  test-unit       - Run unit tests only"
	@echo "  test-integration- Run integration tests (requires root)"
	@echo "  setup-vm        - Configure VM for passwordless sudo (run once)"
	@echo "  test-vm         - Test on VM with real ublk kernel support"
	@echo "  benchmark       - Run benchmarks"
	@echo "  deps            - Install/update dependencies"
	@echo "  tidy            - Tidy dependencies"
	@echo "  lint            - Run linter"
	@echo "  fmt             - Format code"
	@echo "  vet             - Vet code"
	@echo "  check-kernel    - Check ublk kernel support"
	@echo "  check-module    - Show ublk module info"
	@echo "  dev-setup       - Setup development environment"
	@echo "  check           - Full check (fmt, vet, lint, test)"
	@echo "  coverage        - Generate test coverage report"
	@echo "  test-race       - Run tests with race detector"
	@echo ""
	@echo "Enhanced Debug Workflow:"
	@echo "  vm-reset        - Hard reset VM and setup clean environment"
	@echo "  kernel-trace    - Read kernel trace buffer (last 50 lines)"
	@echo "  vm-simple-e2e   - Simple single I/O test with max verbosity"
	@echo ""
	@echo "  help            - Show this help"
# Advanced I/O test
test-vm-io:
	@echo "🧪 Advanced I/O Testing on VM..."
	@echo "Building ublk-mem binary..."
	@go build -o ublk-mem ./cmd/ublk-mem
	@echo "Deploying to VM..."
	@sshpass -f /tmp/devvm_pwd.txt ssh -o StrictHostKeyChecking=no behrlich@192.168.4.79 "mkdir -p ~/ublk-test"
	@sshpass -f /tmp/devvm_pwd.txt scp -o StrictHostKeyChecking=no ublk-mem test-vm.sh behrlich@192.168.4.79:~/ublk-test/
	@echo "Running advanced I/O test..."
	@sshpass -f /tmp/devvm_pwd.txt ssh -o StrictHostKeyChecking=no behrlich@192.168.4.79 << 'REMOTE_EOF' || echo "Test completed (may have failed)"
		cd ~/ublk-test
		echo "=== Advanced I/O Test ==="
		echo "Starting ublk-mem in background..."
		sudo timeout 20s ./ublk-mem --size=64M -v &
		UBLK_PID=$$!
		sleep 3
		echo "Checking if block device exists..."
		if [ -b /dev/ublkb0 ]; then
		    echo "✅ /dev/ublkb0 exists and is a block device"
		    ls -la /dev/ublkb0
		    sudo blockdev --getsize64 /dev/ublkb0
		    echo "Testing basic I/O operations..."
		    echo "Hello, ublk world!" | sudo dd of=/dev/ublkb0 bs=17 count=1 2>/dev/null
		    echo "Reading back data..."
		    sudo dd if=/dev/ublkb0 bs=17 count=1 2>/dev/null
		    echo "✅ Basic I/O operations work!"
		else
		    echo "❌ /dev/ublkb0 does not exist or is not a block device"
		fi
		sudo kill $$UBLK_PID 2>/dev/null || true
		wait $$UBLK_PID 2>/dev/null || true
		echo "=== Advanced test completed ==="
	REMOTE_EOF
minimal_test: minimal_test.c
	gcc -o minimal_test minimal_test.c

vm-minimal: minimal_test
	sshpass -p "$$(cat /tmp/devvm_pwd.txt)" scp minimal_test behrlich@192.168.4.79:~/
	./vm-ssh.sh "sudo ./minimal_test"

# Copy entire repo to VM (excluding .git, build artifacts, etc.)
vm-scp-all:
	@echo "📦 Copying entire repo to VM..."
	@echo "Excluding: .git, build artifacts, IDE files, etc."
	@sshpass -p "$(VM_PASS)" ssh -o StrictHostKeyChecking=no $(VM_USER)@$(VM_HOST) "mkdir -p ~/go-ublk"
	@sshpass -p "$(VM_PASS)" rsync -avz \
		--exclude='.git' \
		--exclude='*.o' \
		--exclude='*.so' \
		--exclude='*.exe' \
		--exclude='ublk-mem' \
		--exclude='ublk-file' \
		--exclude='ublk-null' \
		--exclude='ublk-zip' \
		--exclude='build/' \
		--exclude='.vscode/' \
		--exclude='.idea/' \
		--exclude='*.log' \
		--exclude='*.tmp' \
		-e "sshpass -p '$(VM_PASS)' ssh -o StrictHostKeyChecking=no" \
		./ $(VM_USER)@$(VM_HOST):~/go-ublk/
	@echo "✓ Repo copied to ~/go-ublk on VM"
	@echo "💡 Run: ./vm-ssh.sh \"cd go-ublk && make build\" to build on VM"

# --- Enhanced Debug Workflow ---
.PHONY: vm-reset kernel-trace vm-simple-e2e

# Hard reset VM and prepare clean environment
vm-reset:
	@echo "🔄 Performing hard VM reset and clean environment setup..."
	@echo "Step 1: Hard reset via sysrq..."
	@timeout 3 ./vm-ssh.sh 'echo "Triggering hard reset..." && sudo bash -c "echo 1 > /proc/sys/kernel/sysrq && echo b > /proc/sysrq-trigger"' || echo "Reset triggered (expected timeout)"
	@echo "Step 2: Waiting for VM to restart..."
	@for i in $$(seq 1 30); do \
		sleep 2; \
		if ./vm-ssh.sh 'echo "VM is up"' >/dev/null 2>&1; then \
			echo "✓ VM responsive after $$((i*2)) seconds"; \
			break; \
		fi; \
		echo "  ($$i/30) polling..."; \
	done
	@echo "Step 3: Waiting for system to fully initialize..."
	@sleep 5
	@echo "Step 4: Cleaning up any existing ublk devices..."
	@./vm-ssh.sh 'sudo pkill -9 ublk-mem 2>/dev/null || true; sudo rm -f /dev/ublkb* /dev/ublkc* 2>/dev/null || true'
	@echo "Step 5: Reloading ublk module..."
	@./vm-ssh.sh 'sudo modprobe -r ublk_drv 2>/dev/null || true; sudo modprobe ublk_drv'
	@echo "Step 6: Setting up enhanced kernel tracing..."
	@./vm-ssh.sh 'bash -s' < scripts/vm-enable-logs.sh
	@echo "Step 7: Verifying trace setup..."
	@./vm-ssh.sh 'echo "Active kprobes:"; sudo cat /sys/kernel/tracing/kprobe_events | head -10 || echo "No kprobes set"'
	@echo "✅ VM reset and tracing setup complete"

# Read kernel trace buffer
kernel-trace:
	@echo "📋 Reading kernel trace buffer..."
	@./vm-ssh.sh 'sudo cat /sys/kernel/tracing/trace' | tail -n 50

# Simple single read/write test with maximum verbosity
vm-simple-e2e: ublk-mem vm-copy
	@echo "🧪 Running simple single I/O test with maximum verbosity..."
	@sshpass -p "$(VM_PASS)" scp -o StrictHostKeyChecking=no scripts/vm-simple-e2e.sh $(VM_USER)@$(VM_HOST):~/ublk-test/
	@echo "Test will timeout after 60 seconds if hanging..."
	@timeout 60 ./vm-ssh.sh 'cd ~/ublk-test && chmod +x ./vm-simple-e2e.sh && ./vm-simple-e2e.sh' || \
	 (echo "❌ Overall test timed out - checking VM state..." && \
	  echo "=== FINAL KERNEL TRACE ===" && \
	  make kernel-trace && \
	  echo "=== FINAL DMESG ===" && \
	  ./vm-ssh.sh 'sudo dmesg | tail -n 20' || true)

# FIO test to debug Direct I/O vs Buffered I/O issue
vm-fio-simple-e2e: ublk-mem vm-copy
	@echo "🧪 Running FIO debug test (buffered vs direct I/O)..."
	@sshpass -p "$(VM_PASS)" scp -o StrictHostKeyChecking=no scripts/vm-fio-simple-e2e.sh $(VM_USER)@$(VM_HOST):~/ublk-test/
	@echo "Test will timeout after 60 seconds if hanging..."
	@timeout 60 ./vm-ssh.sh 'cd ~/ublk-test && chmod +x ./vm-fio-simple-e2e.sh && ./vm-fio-simple-e2e.sh' || \
	 (echo "❌ Overall test timed out - checking VM state..." && \
	  echo "=== FINAL KERNEL TRACE ===" && \
	  make kernel-trace && \
	  echo "=== FINAL DMESG ===" && \
	  ./vm-ssh.sh 'sudo dmesg | tail -n 20' || true)

# FORCE target to ensure rebuilds
FORCE:
