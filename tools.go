//go:build tools

// Package tools pins the BUILD TOOLS this repo runs, so their versions live in go.mod and
// go.sum alongside every other dependency.
//
// ⭐ WHY THIS FILE EXISTS. The Makefile claimed "both pinned in tools.go so go.mod records
// the versions" and there was no tools.go. Measured consequence (AOC-005 verify round 1):
// `goose` on PATH was Homebrew v3.28.0 while go.mod pinned the goose LIBRARY at v3.24.1, so
// `make migrate-up` and `go test` applied migrations with two different versions of goose;
// and `sqlc` appeared nowhere in go.mod at all, so `bin/gate api`'s `sqlc diff` judged
// committed code against whatever `brew upgrade` last installed.
//
// The Makefile now invokes both through `go run`, so the version that runs is the version
// recorded here. The repo already did this correctly for Tailwind (pinned by version AND
// SHA-256) and for golangci-lint (@v2.13.2 in CI); this closes the gap for the two that
// touch the database.
//
// The build tag keeps these out of the binary — nothing here is imported by the service.
package tools

import (
	_ "github.com/pressly/goose/v3/cmd/goose"
	_ "github.com/sqlc-dev/sqlc/cmd/sqlc"
)
