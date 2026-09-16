# Stable targets so `bin/gate api` in the product_management repo has something
# unchanging to call, whatever the toolchain does underneath.

VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
COMMIT  ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo none)
PKG     := github.com/pierrehrt/aoc-api/internal/version
LDFLAGS := -X '$(PKG).Version=$(VERSION)' -X '$(PKG).Commit=$(COMMIT)'

.PHONY: run build compile test lint fmt tidy check db-up db-down db-reset db-restore db-psql

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

# ---- local database ---------------------------------------------------------
# There is ONE hosted environment (DECISIONS.md, 2026-09-16). A migration's first execution is
# against the LOCAL database below, restored from a real production dump, so it meets the actual
# data instead of an empty schema. See docs/architecture.md § Deploy.

COMPOSE ?= docker compose

db-up:
	$(COMPOSE) up -d db
	@echo "waiting for postgres to accept connections..."
	@until $(COMPOSE) exec -T db pg_isready -U aoc -d aoc_dev >/dev/null 2>&1; do sleep 1; done
	@echo "ready on localhost:5433 (db aoc_dev, user aoc)"

db-down:
	$(COMPOSE) down

# -v drops the volume: the next db-up is an empty database, not yesterday's half-migrated one.
db-reset:
	$(COMPOSE) down -v
	$(MAKE) db-up

db-psql:
	$(COMPOSE) exec db psql -U aoc -d aoc_dev

# Restore the NEWEST dump in tmp/dumps into the local database.
#
# ⚠️ Deliberately refuses rather than guesses. An empty tmp/dumps/ used to mean "restore nothing,
# report success", and a rehearsal against an empty database is not a rehearsal — it is the exact
# "I could not look reads as an answer" shape bin/gate spent three tickets removing.
db-restore: db-up
	@dump="$$(ls -t tmp/dumps/*.dump tmp/dumps/*.sql 2>/dev/null | head -1)"; 	if [ -z "$$dump" ]; then 	  echo "no dump in tmp/dumps/ — nothing was restored."; 	  echo "Pull one from production first; rehearsing against an empty database proves nothing."; 	  exit 1; 	fi; 	echo "restoring $$dump"; 	base="$$(basename "$$dump")"; 	case "$$base" in 	  *.sql) $(COMPOSE) exec -T db psql -U aoc -d aoc_dev -v ON_ERROR_STOP=1 -f "/dumps/$$base" ;; 	  *)     $(COMPOSE) exec -T db pg_restore -U aoc -d aoc_dev --clean --if-exists --no-owner "/dumps/$$base" ;; 	esac; 	echo "restored $$base"
