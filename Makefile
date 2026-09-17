# Stable targets so `bin/gate api` in the product_management repo has something
# unchanging to call, whatever the toolchain does underneath.

VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
COMMIT  ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo none)
PKG     := github.com/pierrehrt/aoc-api/internal/version
LDFLAGS := -X '$(PKG).Version=$(VERSION)' -X '$(PKG).Commit=$(COMMIT)'

.PHONY: run build compile test lint fmt tidy check db-up db-down db-reset db-restore db-psql assets tools
.PHONY: migrate-up migrate-down migrate-status migrate-redo migrate-create sqlc sqlc-cmd schema-dump

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

# The major version production runs. Checked on every db-up, because the failure it prevents
# is silent: a client older than the server cannot take a dump at all, and the four-step
# migration loop dies at step 1 (AOC-004 verify round 1).
POSTGRES_MAJOR ?= 18

db-up:
	$(COMPOSE) up -d db
	@echo "waiting for postgres to accept connections..."
	@# ⚠️ BOUNDED, and it checks whether the container is still alive.
	@# An unbounded `until pg_isready` cannot tell "not ready yet" from "dead", so it resolves
	@# that by waiting forever — which is exactly what happened on the 17→18 bump, where the
	@# container exits immediately on a stale volume and the version guard below (written to
	@# explain precisely that) sat AFTER the loop and could never run. The container's own log
	@# says what is wrong; this surfaces it instead of hanging. (AOC-004 verify round 2.)
	@i=0; \
	until $(COMPOSE) exec -T db pg_isready -U aoc -d aoc_dev >/dev/null 2>&1; do \
	  if [ "$$($(COMPOSE) ps -q db 2>/dev/null)" = "" ] || \
	     [ "$$(docker inspect -f '{{.State.Running}}' $$($(COMPOSE) ps -q db) 2>/dev/null)" != "true" ]; then \
	    echo ""; echo "postgres exited instead of starting. Its own log says why:"; echo ""; \
	    $(COMPOSE) logs --no-log-prefix --tail 15 db 2>/dev/null | sed 's/^/    /'; \
	    echo ""; \
	    echo "If this mentions a data directory or an unused mount, the volume was written by a"; \
	    echo "different major version. That is expected on a version bump:  make db-reset"; \
	    exit 1; \
	  fi; \
	  i=$$((i+1)); \
	  if [ $$i -ge 60 ]; then \
	    echo "postgres did not accept connections within 60s and is still running. Last log lines:"; \
	    $(COMPOSE) logs --no-log-prefix --tail 15 db 2>/dev/null | sed 's/^/    /'; \
	    exit 1; \
	  fi; \
	  sleep 1; \
	done
	@got="$$($(COMPOSE) exec -T db psql -U aoc -d aoc_dev -tAc 'SHOW server_version' | cut -d. -f1 | tr -d ' \r')"; \
	if [ "$$got" != "$(POSTGRES_MAJOR)" ]; then \
	  echo "local Postgres is major $$got but production runs $(POSTGRES_MAJOR)."; \
	  echo "A client older than the server cannot take a dump at all, so the migration"; \
	  echo "rehearsal loop dies at step 1. Fix docker-compose.yml, then: make db-reset"; \
	  exit 1; \
	fi; \
	echo "ready on localhost:5433 (db aoc_dev, user aoc, major $$got)"

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
#
# ⚠️ It WIPES the local schema first: a restore onto a populated database fails on the first
# CREATE TABLE under ON_ERROR_STOP, so the loop could not be run twice. Local exists to BE a copy
# of production, so anything already in it is by definition stale.
#
# ⭐ The wipe reproduces what initdb made — COMMENT, OWNER and the PUBLIC grant. Miss any one and
# `make schema-dump` produces a different file after a restore than after a fresh start, which is
# a trap in a repo that commits generated schema artifacts.
#
# Round 2 fixed only the comment, and I recorded that as "byte-identical, measured". It was not:
# I had compared a restored database against a restored database. The OWNER is the other half —
# DROP/CREATE SCHEMA leaves `aoc` where initdb leaves `pg_database_owner`, so pg_dump still
# emitted a 7-line TOC entry. Worse, the committed artifact had by then been regenerated from a
# WIPED database, so a FRESH one — the state of CI and of every new clone — produced a phantom
# 7-line deletion. The finding was sign-flipped, not fixed.
#
# ⭐ FRESH IS CANONICAL: docs/database-schema.sql is generated from `db-reset && migrate-up`,
# because that is what CI and a new clone have. With all four statements a restored database now
# produces the identical 95-line file — measured in BOTH directions this time.
# (AOC-004 verify rounds 2 and 3.)
db-restore: db-up
	@dump="$$(ls -t tmp/dumps/*.dump tmp/dumps/*.sql 2>/dev/null | head -1)"; \
	if [ -z "$$dump" ]; then \
	  echo "no dump in tmp/dumps/ — nothing was restored."; \
	  echo "Pull one from production first; rehearsing against an empty database proves nothing."; \
	  exit 1; \
	fi; \
	echo "restoring $$dump"; \
	echo "  wiping the local schema first — a restore onto a populated database is ambiguous,"; \
	echo "  and the point of this loop is that local IS production's data"; \
	$(COMPOSE) exec -T db psql -U aoc -d aoc_dev -q -v ON_ERROR_STOP=1 \
	  -c "DROP SCHEMA IF EXISTS public CASCADE; CREATE SCHEMA public; \
	      COMMENT ON SCHEMA public IS 'standard public schema'; \
	      ALTER SCHEMA public OWNER TO pg_database_owner; \
	      GRANT USAGE ON SCHEMA public TO PUBLIC;" >/dev/null || exit $$?; \
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
	@cp web/src/og-card.png internal/assets/built/og-card.png
	@for f in internal/assets/built/app.css internal/assets/built/htmx.min.js internal/assets/built/og-card.png; do \
	  if [ ! -s "$$f" ]; then echo "asset build produced an EMPTY $$f — that is a failure, not a pass"; exit 1; fi; \
	done
	@echo "assets built:"; ls -l internal/assets/built/ | tail -n +2 | awk '{printf "  %-22s %s bytes\n", $$9, $$5}'

# ---- migrations and generated SQL ------------------------------------------
# goose for migrations, sqlc for typed queries — both pinned to an exact version HERE and run
# through `go run <pkg>@<version>`, so the version that RUNS is the version written down.
#
# ⚠️ Never `goose` / `sqlc` from PATH. The header here used to claim "both pinned in tools.go"
# and there was no tools.go at all. Measured (AOC-005 verify round 1): PATH goose was Homebrew
# v3.28.0 against a go.mod library pin of v3.24.1 — two different versions of goose applying
# the same migrations — and sqlc was in neither go.mod nor go.sum, so the gate's staleness
# check judged committed code against whatever `brew upgrade` last installed.
#
# ⭐ WHY `@version` AND NOT A tools.go. A tools.go was tried first and reverted, because a
# build tool must not get a vote on the DEPLOYED BINARY's toolchain: sqlc v1.31.1 declares
# `go 1.26.0`, so importing it pushed this module's own go.mod from `go 1.23` to `go 1.26.0`,
# which silently made the Dockerfile's `golang:1.23-alpine` stale — against the rule written
# in the Dockerfile itself. It also dragged pgx, the PRODUCTION driver, from v5.7.2 to v5.9.2
# and grew go.sum from 66 lines to 476 (ClickHouse, MySQL, …). `go run pkg@version` resolves
# outside this module: exact version, verified against the checksum database, zero effect on
# what we ship. It is also the pattern this repo already uses for golangci-lint (@v2.13.2) and
# Tailwind (version + SHA-256). (DECISIONS.md, 2026-09-17.)
#
# ⚠️ GOOSE_VERSION must equal the goose LIBRARY version in go.mod — the CLI applies the
# migrations and the library applies them in tests, and those drifting apart IS the original
# defect. Pinned by TestTheGooseCLIMatchesTheGooseLibrary, so it cannot drift unnoticed.
GOOSE_VERSION := v3.24.1
SQLC_VERSION  := v1.31.1

GOOSE := go run github.com/pressly/goose/v3/cmd/goose@$(GOOSE_VERSION)
SQLC  := go run github.com/sqlc-dev/sqlc/cmd/sqlc@$(SQLC_VERSION)
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
	@$(GOOSE) -dir $(MIGRATIONS_DIR) postgres "$$DATABASE_URL" up

migrate-down:
	$(require_db)
	@$(GOOSE) -dir $(MIGRATIONS_DIR) postgres "$$DATABASE_URL" down

migrate-status:
	$(require_db)
	@$(GOOSE) -dir $(MIGRATIONS_DIR) postgres "$$DATABASE_URL" status

# Round-trips the newest migration: down, then up. A Down nobody has run is a Down that
# does not work, and it is needed exactly when things are already going wrong.
migrate-redo:
	$(require_db)
	@$(GOOSE) -dir $(MIGRATIONS_DIR) postgres "$$DATABASE_URL" redo

# make migrate-create NAME=add_item_tables
migrate-create:
	@if [ -z "$(NAME)" ]; then echo "usage: make migrate-create NAME=snake_case_name"; exit 1; fi
	@$(GOOSE) -dir $(MIGRATIONS_DIR) create $(NAME) sql

sqlc:
	@$(SQLC) generate
	@echo "sqlc: regenerated internal/db/sqlcgen/ — commit it, the gate checks it is current"

# ⭐ HOW `bin/gate api` FINDS THE PINNED sqlc. This target PRINTS the invocation rather than
# running it, and the gate runs what it prints.
#
# Why the indirection, because it looks like one step too many: the gate needs sqlc's OWN exit
# code — `sqlc diff` exits 1 for "generated code is stale" and 2+ for "sqlc itself broke", and
# telling those apart is the whole point (a tool that failed must never be read as a clean
# diff). `make` collapses both to 2 when a recipe fails, so a `sqlc-diff` target would hand the
# gate a verdict it cannot interpret. Printing the command keeps the exit code sqlc's own.
#
# It exists at all because the tool that DECIDES pass/fail was the last unpinned one: sqlc's
# generated output is version-specific, so a PATH binary let the gate flip red, or quietly
# bless different generated code, with no commit in this repo. (AOC-005 verify round 1, ❌2.)
sqlc-cmd:
	@echo "$(SQLC)"

# Regenerates the living schema document from the migrated LOCAL database.
#
# ⭐ Generate it from a FRESH database: `make db-reset && make migrate-up && make schema-dump`.
# That is the state CI and every new clone are in, so it is the state the committed artifact must
# match. A restored database produces the same file, but fresh is the reference if they diverge.
#
# ⚠️ The \restrict / \unrestrict lines are stripped. pg_dump emits them with a RANDOM
# token, so an unfiltered dump differs on every run — the file would show a diff after a
# no-op regeneration, and a doc that changes when nothing changed is a doc people stop
# reading. Measured by dumping twice and comparing.
# docs/database-schema.sql describes where the schema IS, so a reviewer never has to
# replay migrations/ in their head (bin/docs-check api enforces that it moves with them).
# ⚠️ Deliberately does NOT call $(require_db). This target dumps the LOCAL compose container
# and ignores DATABASE_URL entirely, so echoing "target: <prod host> ⚠️ NOT LOCAL" named a
# database it never touches. Safe direction, false message — and the criterion is that it must
# never be possible to mistake which database was hit. (AOC-005 verify round 1.)
schema-dump:
	@echo "▶ target: the local compose database (this target ignores DATABASE_URL)"
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
