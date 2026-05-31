# Backend Makefile

.PHONY: help test test-v test-cover build run clean lint fmt vet deps

help:
	@echo "Backend Makefile"
	@echo ""
	@echo "  make test       - Run all tests"
	@echo "  make test-v     - Run tests with verbose output"
	@echo "  make test-cover - Run tests with coverage"
	@echo "  make build      - Build the server binary"
	@echo "  make run        - Run the server"
	@echo "  make clean      - Clean build artifacts"
	@echo "  make lint       - Run linter"
	@echo "  make fmt        - Format code"
	@echo "  make vet        - Run go vet"
	@echo "  make deps       - Download dependencies"

test:
	@echo "=== Running tests ==="
	go test -v ./tests/...

test-v:
	@echo "=== Running tests (verbose) ==="
	go test -v ./...

test-cover:
	@echo "=== Running tests with coverage ==="
	go test -coverprofile=coverage.out ./...
	go tool cover -html=coverage.out -o coverage.html
	@echo "Coverage report generated: coverage.html"

build:
	@echo "=== Building backend ==="
	go build -o bin/server ./cmd/server

run:
	@echo "=== Running server ==="
	go run ./cmd/server/main.go

clean:
	@echo "=== Cleaning ==="
	rm -rf bin/
	rm -f coverage.out coverage.html

lint:
	@echo "=== Running linter ==="
	golangci-lint run || go vet ./...

fmt:
	@echo "=== Formatting code ==="
	go fmt ./...

vet:
	@echo "=== Running go vet ==="
	go vet ./...

deps:
	@echo "=== Downloading dependencies ==="
	go mod download
	go mod tidy