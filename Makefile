.PHONY: all build build-cli build-agent test test-unit test-integration test-e2e clean sign lint fmt deps

# Build variables
BINARY_NAME := fc-macos
AGENT_NAME := fc-agent
VERSION := $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
BUILD_TIME := $(shell date -u '+%Y-%m-%d_%H:%M:%S')
LDFLAGS := -ldflags "-X main.version=$(VERSION) -X main.buildTime=$(BUILD_TIME)"

# Go settings
GOOS := darwin
GOARCH := arm64
CGO_ENABLED := 1

# Directories
BUILD_DIR := build
ASSETS_DIR := assets

all: build

# Dependencies
deps:
	go mod download
	go mod tidy

# Build the CLI for macOS (without VM support - for testing/development)
build-cli-lite:
	@mkdir -p $(BUILD_DIR)
	GOWORK=off CGO_ENABLED=0 GOOS=$(GOOS) GOARCH=$(GOARCH) \
		go build $(LDFLAGS) -o $(BUILD_DIR)/$(BINARY_NAME)-lite ./cmd/fc-macos

# Build the CLI for macOS (with Tart-based VM support - recommended)
build-cli:
	@mkdir -p $(BUILD_DIR)
	GOWORK=off CGO_ENABLED=0 GOOS=$(GOOS) GOARCH=$(GOARCH) \
		go build -tags tart $(LDFLAGS) -o $(BUILD_DIR)/$(BINARY_NAME) ./cmd/fc-macos

# Build the CLI for macOS (with Code-Hex/vz VM support - requires provisioning profile)
build-cli-vz:
	@mkdir -p $(BUILD_DIR)
	GOWORK=off CGO_ENABLED=$(CGO_ENABLED) GOOS=$(GOOS) GOARCH=$(GOARCH) \
		go build -tags vm $(LDFLAGS) -o $(BUILD_DIR)/$(BINARY_NAME)-vz ./cmd/fc-macos

# Build the agent for Linux ARM64 (cross-compile)
build-agent:
	@mkdir -p $(BUILD_DIR)
	GOWORK=off CGO_ENABLED=0 GOOS=linux GOARCH=arm64 \
		go build $(LDFLAGS) -o $(BUILD_DIR)/$(AGENT_NAME)-linux-arm64 ./cmd/fc-agent

# Build everything
build: build-cli build-agent

# Code signing configuration
# Set SIGNING_IDENTITY to your Apple Developer identity (e.g., "Apple Development: Name (ID)")
# Ad-hoc signing (-) works only on macOS versions before Tahoe (26.x)
SIGNING_IDENTITY ?= -

# Code signing with entitlements (required for Virtualization.framework)
sign: build-cli
	codesign --entitlements entitlements.plist --force -s "$(SIGNING_IDENTITY)" $(BUILD_DIR)/$(BINARY_NAME)

# Sign with ad-hoc identity (may not work on macOS Tahoe+)
sign-adhoc: build-cli
	codesign --entitlements entitlements.plist --force -s - $(BUILD_DIR)/$(BINARY_NAME)

# Build and sign in one step
release: build sign

# Tests
test-unit:
	go test -v -short -race ./internal/... ./pkg/...

test-integration:
	go test -v -tags=integration -race ./test/integration/...

test-e2e: release
	go test -v -tags=e2e ./test/e2e/...

test: test-unit

# Test coverage
coverage:
	go test -coverprofile=coverage.out ./internal/... ./pkg/...
	go tool cover -html=coverage.out -o coverage.html
	@echo "Coverage report: coverage.html"

# Linting
lint:
	@which golangci-lint > /dev/null || (echo "Installing golangci-lint..." && brew install golangci-lint)
	golangci-lint run ./...

# Formatting
fmt:
	go fmt ./...
	goimports -w .

# Build kernel (requires Docker and cross-compilation tools)
build-kernel:
	./scripts/build-kernel.sh

# Build rootfs (requires Docker)
build-rootfs: build-agent
	./scripts/build-rootfs.sh

# Build all artifacts including kernel and rootfs
build-all: build build-kernel build-rootfs sign

# Clean build artifacts
clean:
	rm -rf $(BUILD_DIR)
	rm -f coverage.out coverage.html
	rm -rf $(ASSETS_DIR)/kernel/*.img $(ASSETS_DIR)/kernel/vmlinux-*
	rm -rf $(ASSETS_DIR)/rootfs/*.ext4

# Install to /usr/local/bin
install: release
	sudo cp $(BUILD_DIR)/$(BINARY_NAME) /usr/local/bin/
	@echo "Installed $(BINARY_NAME) to /usr/local/bin/"

# Uninstall
uninstall:
	sudo rm -f /usr/local/bin/$(BINARY_NAME)

# Development: build and run version command
dev: release
	./$(BUILD_DIR)/$(BINARY_NAME) version

# Show help
help:
	@echo "fc-macos Makefile targets:"
	@echo ""
	@echo "  build         - Build CLI and agent binaries"
	@echo "  build-cli     - Build only the macOS CLI"
	@echo "  build-agent   - Build only the Linux agent"
	@echo "  sign          - Sign the CLI binary with entitlements"
	@echo "  release       - Build and sign (production build)"
	@echo ""
	@echo "  test          - Run unit tests"
	@echo "  test-unit     - Run unit tests with verbose output"
	@echo "  test-integration - Run integration tests"
	@echo "  test-e2e      - Run end-to-end tests"
	@echo "  coverage      - Generate test coverage report"
	@echo ""
	@echo "  build-kernel  - Build Linux kernel with KVM support"
	@echo "  build-rootfs  - Build Alpine rootfs with agent"
	@echo "  build-all     - Build everything (CLI, agent, kernel, rootfs)"
	@echo ""
	@echo "  lint          - Run linters"
	@echo "  fmt           - Format code"
	@echo "  deps          - Download and tidy dependencies"
	@echo ""
	@echo "  install       - Install to /usr/local/bin"
	@echo "  uninstall     - Remove from /usr/local/bin"
	@echo "  clean         - Remove build artifacts"
	@echo ""
	@echo "  dev           - Build, sign, and run version command"
	@echo "  help          - Show this help"
