.PHONY: all build test clean fmt lint check dev-tools check-kernel

# Go parameters
GOCMD=go
GOBUILD=$(GOCMD) build
GOTEST=$(GOCMD) test
GOCLEAN=$(GOCMD) clean
GOGET=$(GOCMD) get
GOMOD=$(GOCMD) mod
GOFMT=gofmt
GOLINT=golangci-lint

# Binary names
BINARIES=ublk-mem ublk-file ublk-zip ublk-null

# Build directories
BUILD_DIR=build
CMD_DIR=cmd

all: build

build: $(BINARIES)

$(BINARIES):
	@mkdir -p $(BUILD_DIR)
	$(GOBUILD) -o $(BUILD_DIR)/$@ ./$(CMD_DIR)/$@

test:
	$(GOTEST) -v -race ./...

test-integration:
	@echo "Running integration tests (requires root and ublk support)..."
	sudo $(GOTEST) -v -tags=ublk ./...

test-coverage:
	$(GOTEST) -v -race -coverprofile=coverage.out ./...
	$(GOCMD) tool cover -html=coverage.out -o coverage.html
	@echo "Coverage report generated: coverage.html"

benchmark:
	$(GOTEST) -bench=. -benchmem ./...

clean:
	$(GOCLEAN)
	rm -rf $(BUILD_DIR)
	rm -f coverage.out coverage.html

fmt:
	$(GOFMT) -s -w .
	$(GOCMD) fmt ./...

lint:
	@if ! which $(GOLINT) > /dev/null; then \
		echo "golangci-lint not found, installing..."; \
		go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest; \
	fi
	$(GOLINT) run

check: fmt lint test

deps:
	$(GOMOD) download
	$(GOMOD) tidy

dev-tools:
	@echo "Installing development tools..."
	go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest
	go install golang.org/x/tools/cmd/goimports@latest
	go install github.com/google/go-licenses@latest
	@echo "Development tools installed"

check-kernel:
	@echo "Checking kernel support for ublk..."
	@if [ -e /dev/ublk-control ]; then \
		echo "✓ /dev/ublk-control exists"; \
	else \
		echo "✗ /dev/ublk-control not found"; \
		echo "  Try: sudo modprobe ublk_drv"; \
	fi
	@echo ""
	@echo "Kernel version: $$(uname -r)"
	@if [ $$(uname -r | cut -d. -f1) -ge 6 ] && [ $$(uname -r | cut -d. -f2) -ge 1 ]; then \
		echo "✓ Kernel version >= 6.1"; \
	else \
		echo "✗ Kernel version < 6.1 (ublk not supported)"; \
	fi
	@echo ""
	@echo "Checking kernel config..."
	@if zgrep -q CONFIG_BLK_DEV_UBLK=y /proc/config.gz 2>/dev/null || \
	    zgrep -q CONFIG_BLK_DEV_UBLK=m /proc/config.gz 2>/dev/null; then \
		echo "✓ CONFIG_BLK_DEV_UBLK enabled"; \
	else \
		echo "? CONFIG_BLK_DEV_UBLK status unknown"; \
	fi
	@if zgrep -q CONFIG_IO_URING=y /proc/config.gz 2>/dev/null; then \
		echo "✓ CONFIG_IO_URING enabled"; \
	else \
		echo "? CONFIG_IO_URING status unknown"; \
	fi

run-mem: build
	@echo "Starting memory-backed ublk device (requires root)..."
	sudo ./$(BUILD_DIR)/ublk-mem --size=100M

run-file: build
	@echo "Starting file-backed ublk device (requires root)..."
	@if [ -z "$(FILE)" ]; then \
		echo "Usage: make run-file FILE=/path/to/disk.img"; \
		exit 1; \
	fi
	sudo ./$(BUILD_DIR)/ublk-file --path=$(FILE)

docker-test:
	@echo "Running tests in Docker container..."
	docker build -t go-ublk-test -f test/Dockerfile .
	docker run --rm --privileged go-ublk-test

# Development helpers
watch:
	@echo "Watching for changes..."
	@while true; do \
		$(MAKE) -q || $(MAKE); \
		sleep 1; \
	done

.PHONY: help
help:
	@echo "go-ublk Makefile"
	@echo ""
	@echo "Usage:"
	@echo "  make              - Build all binaries"
	@echo "  make test         - Run unit tests"
	@echo "  make test-integration - Run integration tests (requires root)"
	@echo "  make benchmark    - Run benchmarks"
	@echo "  make clean        - Clean build artifacts"
	@echo "  make fmt          - Format code"
	@echo "  make lint         - Run linter"
	@echo "  make check        - Run fmt, lint, and tests"
	@echo "  make deps         - Download dependencies"
	@echo "  make dev-tools    - Install development tools"
	@echo "  make check-kernel - Check kernel ublk support"
	@echo "  make run-mem      - Run memory-backed device"
	@echo "  make run-file FILE=path - Run file-backed device"
	@echo "  make help         - Show this help"