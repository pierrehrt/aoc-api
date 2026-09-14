# Stable targets so `bin/gate api` in the product_management repo has something
# unchanging to call, whatever the toolchain does underneath.

VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
COMMIT  ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo none)
PKG     := github.com/pierrehrt/aoc-api/internal/version
LDFLAGS := -X '$(PKG).Version=$(VERSION)' -X '$(PKG).Commit=$(COMMIT)'

.PHONY: run build test lint fmt tidy check

run:
	go run -ldflags "$(LDFLAGS)" ./cmd/api

build:
	go build -ldflags "$(LDFLAGS)" -o bin/aoc-api ./cmd/api

test:
	go test ./...

lint:
	go vet ./...

fmt:
	gofmt -w .

tidy:
	go mod tidy

# What CI and bin/gate run. Deliberately the same command a person runs locally.
check: fmt-check lint test

.PHONY: fmt-check
fmt-check:
	@out="$$(gofmt -l . | grep -v '^vendor/' || true)"; \
	if [ -n "$$out" ]; then echo "not gofmt'd:"; echo "$$out"; exit 1; fi
