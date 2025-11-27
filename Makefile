# Makefile for go-ublk

# Include local overrides if present (gitignored)
-include Makefile.local

#==============================================================================
# Go Build Configuration
#==============================================================================

GOCMD    = go
GOBUILD  = $(GOCMD) build
GOCLEAN  = $(GOCMD) clean
GOTEST   = $(GOCMD) test
GOGET    = $(GOCMD) get
GOMOD    = $(GOCMD) mod

# Build flags
TAGS ?=

# Race detector: make RACE=1 <target>
RACE ?=
ifeq ($(RACE),1)
  BUILD_FLAGS = -race
  CGO_SETTING = CGO_ENABLED=1
else
  BUILD_FLAGS =
  CGO_SETTING = CGO_ENABLED=0
endif

# Binary targets
BINARIES = ublk-mem ublk-file ublk-null ublk-zip

#==============================================================================
# VM Configuration (override in Makefile.local or environment)
#==============================================================================

VM_HOST ?= $(UBLK_VM_HOST)
VM_USER ?= $(UBLK_VM_USER)
VM_DIR  ?= ~/ublk-test
VM_PASS ?= $(UBLK_VM_PASS)

# SSH command construction
ifdef VM_PASS
  VM_SSH = sshpass -p "$(VM_PASS)" ssh -o StrictHostKeyChecking=no $(VM_USER)@$(VM_HOST)
  VM_SCP = sshpass -p "$(VM_PASS)" scp -o StrictHostKeyChecking=no
else
  VM_SSH = ssh $(VM_USER)@$(VM_HOST)
  VM_SCP = scp
endif

#==============================================================================
# Core Targets
#==============================================================================

.PHONY: all build clean test test-unit test-integration deps tidy fmt lint vet help

all: deps build test

# Build all binaries
build: FORCE $(BINARIES)

ublk-mem: FORCE
	@mkdir -p bin
	@echo "Building ublk-mem$(if $(BUILD_FLAGS), (with race detector),)..."
	@$(CGO_SETTING) $(GOBUILD) $(BUILD_FLAGS) -o bin/ublk-mem ./examples/ublk-mem

ublk-file: FORCE
	@echo "Building ublk-file (Phase 4)"

ublk-null: FORCE
	@echo "Building ublk-null (Phase 4)"

ublk-zip: FORCE
	@echo "Building ublk-zip (Phase 4)"

clean:
	$(GOCLEAN)
	rm -rf bin/ build/

#==============================================================================
# Testing
#==============================================================================

test: test-unit

test-unit:
	@echo "Running unit tests..."
	$(GOTEST) -v ./...
	$(GOTEST) -v -tags=!integration ./test/unit/...

test-integration:
	@echo "Running integration tests (requires root and ublk kernel support)..."
	@if [ "$$(id -u)" != "0" ]; then \
		echo "Integration tests require root privileges"; \
		echo "Run: sudo make test-integration"; \
		exit 1; \
	fi
	$(GOTEST) -v -tags=integration ./test/integration/...

test-race:
	@echo "Running tests with race detector..."
	$(GOTEST) -v -race ./...

benchmark:
	@echo "Running benchmarks..."
	$(GOTEST) -bench=. -benchmem ./...

coverage:
	@echo "Generating coverage report..."
	$(GOTEST) -coverprofile=coverage.out ./...
	$(GOCMD) tool cover -html=coverage.out -o coverage.html
	@echo "Coverage report: coverage.html"

#==============================================================================
# Code Quality
#==============================================================================

deps:
	$(GOGET) -v ./...
	$(GOMOD) download

tidy:
	$(GOMOD) tidy

fmt:
	@echo "Running gofmt -s -w ..."
	@gofmt -s -w .
	@echo "Running goimports -w ..."
	@which goimports > /dev/null || go install golang.org/x/tools/cmd/goimports@latest
	@goimports -w .
	@echo "Code formatted"

lint:
	@echo "Checking formatting..."
	@UNFORMATTED=$$(gofmt -l . | grep -v '^vendor/' || true); \
	if [ -n "$$UNFORMATTED" ]; then \
		echo "Unformatted files:"; echo "$$UNFORMATTED"; \
		echo "Run 'make fmt' to fix"; exit 1; \
	fi
	@echo "Checking imports..."
	@which goimports > /dev/null || (echo "goimports not found"; exit 1)
	@BADIMPORTS=$$(goimports -l . | grep -v '^vendor/' || true); \
	if [ -n "$$BADIMPORTS" ]; then \
		echo "Bad imports:"; echo "$$BADIMPORTS"; \
		echo "Run 'make fmt' to fix"; exit 1; \
	fi
	@echo "Running golangci-lint..."
	@which golangci-lint > /dev/null || (echo "golangci-lint not found, skipping..."; exit 0)
	@golangci-lint run || exit 1
	@echo "Lint passed"

vet:
	$(GOCMD) vet ./...

check: fmt vet lint test

dev-setup: deps
	@echo "Setting up development environment..."
	@which golangci-lint >/dev/null 2>&1 || go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest

#==============================================================================
# Kernel Support Checks
#==============================================================================

check-kernel:
	@echo "Checking ublk kernel support..."
	@if [ -e /dev/ublk-control ]; then echo "✓ /dev/ublk-control exists"; \
	else echo "✗ /dev/ublk-control not found - run: sudo modprobe ublk_drv"; fi
	@if lsmod | grep -q ublk_drv; then echo "✓ ublk_drv module loaded"; \
	else echo "✗ ublk_drv not loaded - run: sudo modprobe ublk_drv"; fi

check-module:
	@if modinfo ublk_drv >/dev/null 2>&1; then modinfo ublk_drv | head -10; \
	else echo "ublk_drv module not available"; fi

#==============================================================================
# VM Testing (requires VM_HOST, VM_USER configured)
#==============================================================================

.PHONY: vm-check vm-copy vm-e2e vm-simple-e2e vm-benchmark vm-reset vm-stress vm-fuzz

# Check VM configuration before running VM targets
vm-check:
	@if [ -z "$(VM_HOST)" ] || [ -z "$(VM_USER)" ]; then \
		echo "Error: VM not configured"; \
		echo "Set VM_HOST and VM_USER in Makefile.local or environment:"; \
		echo "  export UBLK_VM_HOST=192.168.1.100"; \
		echo "  export UBLK_VM_USER=myuser"; \
		echo "  export UBLK_VM_PASS=mypassword  # or use SSH keys"; \
		echo ""; \
		echo "Or copy Makefile.local.example to Makefile.local and edit it."; \
		exit 1; \
	fi

vm-copy: vm-check ublk-mem
	@echo "Copying ublk-mem to VM..."
	@$(VM_SSH) "mkdir -p $(VM_DIR); sudo killall ublk-mem 2>/dev/null || true"
	@$(VM_SCP) bin/ublk-mem $(VM_USER)@$(VM_HOST):$(VM_DIR)/
	@echo "Copied."

vm-e2e: vm-copy
	@echo "Running e2e I/O test on VM..."
	@$(VM_SCP) scripts/vm-simple-e2e.sh $(VM_USER)@$(VM_HOST):$(VM_DIR)/
	@$(VM_SSH) "cd $(VM_DIR) && chmod +x ./vm-simple-e2e.sh && ./vm-simple-e2e.sh"
	@echo "VM e2e test completed"

vm-simple-e2e: vm-copy
	@echo "Running simple I/O test..."
	@$(VM_SCP) scripts/vm-simple-e2e.sh $(VM_USER)@$(VM_HOST):$(VM_DIR)/
	@timeout 60 $(VM_SSH) "cd $(VM_DIR) && chmod +x ./vm-simple-e2e.sh && ./vm-simple-e2e.sh" || \
		(echo "Test timed out" && $(MAKE) vm-trace && exit 1)

vm-benchmark: vm-copy
	@echo "Running benchmark on VM..."
	@$(VM_SCP) scripts/vm-quick-bench.sh $(VM_USER)@$(VM_HOST):$(VM_DIR)/
	@$(VM_SSH) "cd $(VM_DIR) && chmod +x ./vm-quick-bench.sh && ./vm-quick-bench.sh"
	@echo "VM benchmark completed"

# Fetch a file from the VM: make vm-fetch SRC=/tmp/cpu.prof DST=./cpu.prof
vm-fetch: vm-check
	@if [ -z "$(SRC)" ] || [ -z "$(DST)" ]; then \
		echo "Usage: make vm-fetch SRC=/path/on/vm DST=/path/local"; \
		exit 1; \
	fi
	$(VM_SCP) $(VM_USER)@$(VM_HOST):$(SRC) $(DST)

vm-reset: vm-check
	@echo "Hard reset VM..."
	@timeout 3 $(VM_SSH) 'sudo sh -c "echo 1 > /proc/sys/kernel/sysrq; echo b > /proc/sysrq-trigger"' || true
	@echo "Waiting for VM..."
	@for i in $$(seq 1 30); do \
		sleep 2; \
		if $(VM_SSH) 'echo ok' >/dev/null 2>&1; then echo "VM up"; break; fi; \
		echo "  ($$i/30)..."; \
	done
	@sleep 5
	@$(VM_SSH) 'sudo pkill -9 ublk-mem 2>/dev/null || true; sudo modprobe -r ublk_drv 2>/dev/null || true; sudo modprobe ublk_drv'
	@echo "VM reset complete"

vm-trace: vm-check
	@$(VM_SSH) 'sudo cat /sys/kernel/tracing/trace 2>/dev/null | tail -50 || echo "No trace available"'

vm-stress: ublk-mem
	@echo "Running 10x stress test..."
	@$(MAKE) vm-reset
	@for i in 1 2 3 4 5 6 7 8 9 10; do \
		echo "=== Iteration $$i/10 ==="; \
		timeout 60 $(MAKE) vm-e2e || exit 1; \
		timeout 180 $(MAKE) vm-benchmark || exit 1; \
	done
	@echo "All 10 iterations passed"

vm-fuzz: vm-copy
	@echo "Running comprehensive fuzz test on VM..."
	@$(VM_SCP) scripts/vm-fuzz.sh $(VM_USER)@$(VM_HOST):$(VM_DIR)/
	@$(VM_SSH) "cd $(VM_DIR) && chmod +x vm-fuzz.sh && sudo ./vm-fuzz.sh"
	@echo "VM fuzz test completed"

# Alias for backwards compatibility
test-vm: vm-simple-e2e

#==============================================================================
# Race Detector Variants
#==============================================================================

vm-e2e-racedetect:
	@$(MAKE) RACE=1 vm-e2e

vm-simple-e2e-racedetect:
	@$(MAKE) RACE=1 vm-simple-e2e

#==============================================================================
# VM Kernel Management
#==============================================================================

.PHONY: vm-kernel vm-kernel-list vm-kernel-install vm-kernel-switch

# Show current kernel
vm-kernel: vm-check
	@$(VM_SSH) "uname -r"

# List available and installed kernels
vm-kernel-list: vm-check
	@echo "=== Installed kernels ==="
	@$(VM_SSH) "dpkg -l | grep linux-image | grep -E '^ii' | awk '{print \$$2, \$$3}'"
	@echo ""
	@echo "=== Available 6.8 kernels ==="
	@$(VM_SSH) "apt-cache search linux-image-6.8 | grep generic | grep -v unsigned | sort -V | tail -5"

# Install a specific kernel: make vm-kernel-install VER=6.8.0-31
vm-kernel-install: vm-check
	@if [ -z "$(VER)" ]; then \
		echo "Usage: make vm-kernel-install VER=6.8.0-31"; \
		exit 1; \
	fi
	@echo "Installing kernel $(VER)..."
	@$(VM_SSH) "sudo apt-get update && sudo apt-get install -y linux-image-$(VER)-generic linux-modules-$(VER)-generic"

# Switch to a kernel and reboot: make vm-kernel-switch VER=6.8.0-31
vm-kernel-switch: vm-check
	@if [ -z "$(VER)" ]; then \
		echo "Usage: make vm-kernel-switch VER=6.8.0-31"; \
		exit 1; \
	fi
	@echo "Setting default kernel to $(VER) and rebooting..."
	@$(VM_SSH) "sudo grub-set-default 'Advanced options for Ubuntu>Ubuntu, with Linux $(VER)-generic' && sudo update-grub"
	@$(VM_SSH) "sudo reboot" || true
	@echo "Waiting for VM to reboot..."
	@sleep 10
	@for i in $$(seq 1 30); do \
		if $(VM_SSH) 'uname -r' 2>/dev/null; then break; fi; \
		sleep 2; \
		echo "  ($$i/30)..."; \
	done

# Quick downgrade to 6.8 (oldest available on 24.04)
vm-kernel-6.8: vm-check
	@echo "Installing and switching to kernel 6.8.0-31..."
	@$(VM_SSH) "sudo apt-get update && sudo DEBIAN_FRONTEND=noninteractive apt-get install -y linux-image-6.8.0-31-generic linux-modules-6.8.0-31-generic linux-modules-extra-6.8.0-31-generic" || true
	@$(VM_SSH) "sudo grub-set-default 'Advanced options for Ubuntu>Ubuntu, with Linux 6.8.0-31-generic' && sudo update-grub"
	@$(VM_SSH) "sudo reboot" || true
	@echo "Waiting for VM to reboot..."
	@sleep 10
	@for i in $$(seq 1 30); do \
		if $(VM_SSH) 'uname -r' 2>/dev/null; then break; fi; \
		sleep 2; \
		echo "  ($$i/30)..."; \
	done
	@$(VM_SSH) "sudo modprobe ublk_drv"
	@echo "Done. Kernel: $$($(VM_SSH) 'uname -r')"

# Switch back to latest kernel (6.11)
vm-kernel-latest: vm-check
	@echo "Switching to latest kernel..."
	@$(VM_SSH) "sudo grub-set-default 0 && sudo update-grub"
	@$(VM_SSH) "sudo reboot" || true
	@echo "Waiting for VM to reboot..."
	@sleep 10
	@for i in $$(seq 1 30); do \
		if $(VM_SSH) 'uname -r' 2>/dev/null; then break; fi; \
		sleep 2; \
		echo "  ($$i/30)..."; \
	done
	@$(VM_SSH) "sudo modprobe ublk_drv"
	@echo "Done. Kernel: $$($(VM_SSH) 'uname -r')"

#==============================================================================
# Help
#==============================================================================

help:
	@echo "go-ublk Makefile"
	@echo ""
	@echo "Build:"
	@echo "  make build          Build all binaries"
	@echo "  make clean          Clean build artifacts"
	@echo "  make RACE=1 build   Build with race detector"
	@echo ""
	@echo "Test:"
	@echo "  make test           Run unit tests"
	@echo "  make test-race      Run tests with race detector"
	@echo "  make benchmark      Run benchmarks"
	@echo "  make coverage       Generate coverage report"
	@echo ""
	@echo "Code Quality:"
	@echo "  make fmt            Format code"
	@echo "  make lint           Run linter"
	@echo "  make vet            Run go vet"
	@echo "  make check          Run all checks"
	@echo ""
	@echo "VM Testing (requires configuration, see Makefile.local.example):"
	@echo "  make vm-simple-e2e  Simple I/O test"
	@echo "  make vm-e2e         Full e2e test"
	@echo "  make vm-benchmark   Performance benchmark"
	@echo "  make vm-fuzz        Comprehensive fuzz test (30s/test)"
	@echo "  make vm-stress      10x stress test"
	@echo "  make vm-reset       Hard reset VM"
	@echo ""
	@echo "Kernel:"
	@echo "  make check-kernel   Check ublk kernel support"
	@echo ""
	@echo "VM Kernel Management:"
	@echo "  make vm-kernel      Show current VM kernel"
	@echo "  make vm-kernel-list List installed/available kernels"
	@echo "  make vm-kernel-6.8  Downgrade to kernel 6.8 (minimum supported)"
	@echo "  make vm-kernel-switch VER=6.8.0-31  Switch to specific kernel"

FORCE:
