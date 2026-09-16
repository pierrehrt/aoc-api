# Stable targets so `bin/gate api` in the product_management repo has something
# unchanging to call, whatever the toolchain does underneath.

VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
COMMIT  ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo none)
PKG     := github.com/pierrehrt/aoc-api/internal/version
LDFLAGS := -X '$(PKG).Version=$(VERSION)' -X '$(PKG).Commit=$(COMMIT)'

.PHONY: run build compile test lint fmt tidy check db-up db-down db-reset db-restore db-psql assets tools
.PHONY: migrate-up migrate-down migrate-status migrate-redo migrate-create sqlc schema-dump

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
	@dump="$$(ls -t tmp/dumps/*.dump tmp/dumps/*.sql 2>/dev/null | head -1)"; \
	if [ -z "$$dump" ]; then \
	  echo "no dump in tmp/dumps/ — nothing was restored."; \
	  echo "Pull one from production first; rehearsing against an empty database proves nothing."; \
	  exit 1; \
	fi; \
	echo "restoring $$dump"; \
	case "$$dump" in \
	  *.sql) $(COMPOSE) exec -T db psql -U aoc -d aoc_dev -v ON_ERROR_STOP=1 < "$$dump" ;; \
	  *)     $(COMPOSE) exec -T db pg_restore -U aoc -d aoc_dev --clean --if-exists --no-owner < "$$dump" ;; \
	esac; \
	rc=$$?; \
	if [ $$rc -ne 0 ]; then \
	  echo "RESTORE FAILED (exit $$rc) — the database is NOT a copy of that dump."; \
	  echo "Do not rehearse a migration against it."; \
	  exit $$rc; \
	fi; \
	echo "restored $$(basename "$$dump")"

# ---- front-end assets ------------------------------------------------------
# Built here, COMMITTED to internal/assets/built/, embedded by the binary. Railway never
# runs this: what ships is what was reviewed (DECISIONS.md, 2026-09-16), and `bin/gate api`
# fails if the committed output is missing, empty, ignored or stale.
#
# Tailwind is the STANDALONE binary — no Node, no node_modules, no package.json. Pinned by
# version AND checksum: a build tool that silently changes under us is how a site starts
# looking different for reasons nobody can find.

TAILWIND_VERSION := v4.1.13
TAILWIND_SHA256  := c47681e9948db20026a913a4aca4ee0269b4c0d4ef3f71343cb891dfdc1e97c9
TAILWIND         := .tools/tailwindcss
TAILWIND_URL     := https://github.com/tailwindlabs/tailwindcss/releases/download/$(TAILWIND_VERSION)/tailwindcss-macos-arm64

$(TAILWIND):
	@mkdir -p .tools
	@echo "downloading tailwindcss $(TAILWIND_VERSION)"
	@curl -sL --fail -o $(TAILWIND).tmp "$(TAILWIND_URL)"
	@got="$$(shasum -a 256 $(TAILWIND).tmp | cut -d" " -f1)"; \
	if [ "$$got" != "$(TAILWIND_SHA256)" ]; then \
	  echo "checksum mismatch for tailwindcss:"; echo "  want $(TAILWIND_SHA256)"; echo "  got  $$got"; \
	  rm -f $(TAILWIND).tmp; exit 1; fi
	@mv $(TAILWIND).tmp $(TAILWIND) && chmod +x $(TAILWIND)
	@echo "tailwindcss $(TAILWIND_VERSION) verified"

tools: $(TAILWIND)

assets: $(TAILWIND)
	@mkdir -p internal/assets/built
	@$(TAILWIND) -i web/src/app.css -o internal/assets/built/app.css --minify
	@cp web/src/htmx.min.js internal/assets/built/htmx.min.js
	@for f in internal/assets/built/app.css internal/assets/built/htmx.min.js; do \
	  if [ ! -s "$$f" ]; then echo "asset build produced an EMPTY $$f — that is a failure, not a pass"; exit 1; fi; \
	done
	@echo "assets built:"; ls -l internal/assets/built/ | tail -n +2 | awk '{printf "  %-22s %s bytes\n", $$9, $$5}'

# ---- migrations and generated SQL ------------------------------------------
# goose for migrations, sqlc for typed queries. Both pinned in tools.go so go.mod records
# the versions; installed on PATH so `bin/gate api` can see them.
#
# ⭐ THE GUARD RAIL. There is ONE hosted environment, so nothing here may quietly default
# to a database. Every target below requires DATABASE_URL to be set explicitly and ECHOES
# WHICH HOST it is about to touch before doing anything. The failure being prevented is
# running a migration against production while believing it is local — which, on this
# project, means the only structured copy of the armory data.

MIGRATIONS_DIR := migrations

# Refuse to run without an explicit target, and say which one it is. The host is printed,
# never the whole URL: that carries the password.
define require_db
	@if [ -z "$$DATABASE_URL" ]; then \
	  echo "DATABASE_URL is not set. Refusing to guess which database to touch."; \
	  echo "  local:      export DATABASE_URL=postgres://aoc:aoc@localhost:5433/aoc_dev?sslmode=disable"; \
	  echo "  production: take it from Railway, and read docs/architecture.md § Deploy first."; \
	  exit 1; \
	fi; \
	host="$$(printf '%s' "$$DATABASE_URL" | sed -E 's#^[^@]*@##; s#[/?].*$$##')"; \
	case "$$host" in \
	  localhost*|127.0.0.1*) echo "▶ target: $$host  (local)";; \
	  *) echo "▶ target: $$host  ⚠️  NOT LOCAL — this is a real database";; \
	esac
endef

migrate-up:
	$(require_db)
	@goose -dir $(MIGRATIONS_DIR) postgres "$$DATABASE_URL" up

migrate-down:
	$(require_db)
	@goose -dir $(MIGRATIONS_DIR) postgres "$$DATABASE_URL" down

migrate-status:
	$(require_db)
	@goose -dir $(MIGRATIONS_DIR) postgres "$$DATABASE_URL" status

# Round-trips the newest migration: down, then up. A Down nobody has run is a Down that
# does not work, and it is needed exactly when things are already going wrong.
migrate-redo:
	$(require_db)
	@goose -dir $(MIGRATIONS_DIR) postgres "$$DATABASE_URL" redo

# make migrate-create NAME=add_item_tables
migrate-create:
	@if [ -z "$(NAME)" ]; then echo "usage: make migrate-create NAME=snake_case_name"; exit 1; fi
	@goose -dir $(MIGRATIONS_DIR) create $(NAME) sql

sqlc:
	@sqlc generate
	@echo "sqlc: regenerated internal/db/sqlcgen/ — commit it, the gate checks it is current"

# Regenerates the living schema document from the migrated LOCAL database.
#
# ⚠️ The \restrict / \unrestrict lines are stripped. pg_dump 17 emits them with a RANDOM
# token, so an unfiltered dump differs on every run — the file would show a diff after a
# no-op regeneration, and a doc that changes when nothing changed is a doc people stop
# reading. Measured by dumping twice and comparing.
# docs/database-schema.sql describes where the schema IS, so a reviewer never has to
# replay migrations/ in their head (bin/docs-check api enforces that it moves with them).
schema-dump:
	$(require_db)
	@{ \
	  echo "-- aoc_api — the CURRENT database schema, as a living document."; \
	  echo "--"; \
	  echo "-- GENERATED by \`make schema-dump\` from a migrated database. Do not hand-edit:"; \
	  echo "-- run the migration, then regenerate, and commit both together."; \
	  echo "--"; \
	  echo "-- NOTE for AOC-010, carried from AOC-016 and AOC-017:"; \
	  echo "--   * \`spell_effect\` is its own column, separate from an item's \`stats\`. A build or"; \
	  echo "--     armour calculator sums \`stats\` and must never sum \`spell_effect\`."; \
	  echo "--   * \`vendor\` is its own column, separate from \`boss_or_npc\`."; \
	  echo "--   * \`binding\` is a 3-row lookup table, not a free-text column."; \
	  echo "--   * \`coords\` is overloaded in the source data: on a quest row it is where the"; \
	  echo "--     quest-giver stands, not a dungeon entrance. Label which it is."; \
	  echo ""; \
	  $(COMPOSE) exec -T db pg_dump -U aoc -d aoc_dev --schema-only --no-owner --no-privileges \
	    | grep -vE '^\\(un)?restrict '; \
	} > docs/database-schema.sql
	@echo "schema-dump: wrote docs/database-schema.sql"
