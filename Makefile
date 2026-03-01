# Copyright The Linux Foundation and each contributor to LFX.
# SPDX-License-Identifier: MIT

.PHONY: all build clean check fmt vet lint test run help docker-build

# Build variables
BINARY_NAME=lfx-mcp-server
CMD_DIR=./cmd/lfx-mcp-server
BUILD_DIR=./bin
GO_FILES=$(shell find . -name "*.go" -type f)

# Build flags
LDFLAGS=-ldflags="-s -w"

# Docker variables
DOCKER_IMAGE=linuxfoundation/lfx-mcp
DOCKER_TAG=latest

# Default target
all: clean check build

# Build the binary
build: $(BUILD_DIR)/$(BINARY_NAME)

$(BUILD_DIR)/$(BINARY_NAME): $(GO_FILES)
	@echo "Building $(BINARY_NAME)..."
	@mkdir -p $(BUILD_DIR)
	go build $(LDFLAGS) -o $(BUILD_DIR)/$(BINARY_NAME) $(CMD_DIR)

# Clean build artifacts
clean:
	@echo "Cleaning build artifacts..."
	@rm -rf $(BUILD_DIR)

# Run all checks
check: fmt vet lint

# Format Go code
fmt:
	@echo "Formatting Go code..."
	go fmt ./...

# Run go vet
vet:
	@echo "Running go vet..."
	go vet ./...

# Run golangci-lint (if available)
lint:
	@echo "Running linters..."
	@if command -v golangci-lint >/dev/null 2>&1; then \
		golangci-lint run; \
	else \
		echo "golangci-lint not installed, skipping..."; \
	fi

# Run tests
test:
	@echo "Running tests..."
	go test -v ./...

# Run tests with coverage
test-coverage:
	@echo "Running tests with coverage..."
	go test -v -coverprofile=coverage.out ./...
	go tool cover -html=coverage.out -o coverage.html

# Run the server in stdio mode
run: build
	@echo "Starting LFX MCP Server..."
	$(BUILD_DIR)/$(BINARY_NAME) stdio

# Download dependencies
deps:
	@echo "Downloading dependencies..."
	go mod download
	go mod tidy

# Install development tools
install-tools:
	@echo "Installing development tools..."
	go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest

# Build Docker image
docker-build:
	@echo "Building Docker image..."
	docker build -t $(DOCKER_IMAGE):$(DOCKER_TAG) -f Dockerfile .
	@echo "Docker image built: $(DOCKER_IMAGE):$(DOCKER_TAG)"

# Show help
help:
	@echo "Available targets:"
	@echo "  all            - Clean, check, and build (default)"
	@echo "  build          - Build the binary"
	@echo "  clean          - Clean build artifacts"
	@echo "  check          - Run all code quality checks"
	@echo "  fmt            - Format Go code"
	@echo "  vet            - Run go vet"
	@echo "  lint           - Run golangci-lint"
	@echo "  test           - Run tests"
	@echo "  test-coverage  - Run tests with coverage report"
	@echo "  run            - Build and run the server"
	@echo "  deps           - Download and tidy dependencies"
	@echo "  install-tools  - Install development tools"
	@echo "  docker-build   - Build Docker image"
	@echo "  help           - Show this help message"
