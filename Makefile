# Stable targets so `bin/gate api` in the product_management repo has something
# unchanging to call, whatever the toolchain does underneath.

VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
COMMIT  ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo none)
PKG     := github.com/pierrehrt/aoc-api/internal/version
LDFLAGS := -X '$(PKG).Version=$(VERSION)' -X '$(PKG).Commit=$(COMMIT)'

.PHONY: run build compile test lint fmt tidy check

run:
	go run -ldflags "$(LDFLAGS)" ./cmd/api

build:
	go build -ldflags "$(LDFLAGS)" -o bin/aoc-api ./cmd/api

# Compiles every package, including ones with no test file. `go test` compiles what it lists,
# but a package with no tests still deserves to be known to build.
compile:
	go build ./...

test:
	go test ./...

lint:
	go vet ./...

fmt:
	gofmt -w .

tidy:
	go mod tidy

# THE CANONICAL LIST. CI calls these targets one by one so each is its own step; a person runs
# `make check`. Adding a check means adding it here, once.
check: fmt-check lint compile test

.PHONY: fmt-check
fmt-check:
	@out="$$(gofmt -l . | grep -v '^vendor/' || true)"; \
	if [ -n "$$out" ]; then echo "not gofmt'd:"; echo "$$out"; exit 1; fi
