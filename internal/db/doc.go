// Package db owns the connection pool and the sqlc-generated query types.
//
// SQL is written by hand in queries/*.sql and compiled to typed Go by sqlc, so the
// schema stays visible and a column rename breaks the build instead of production
// (reference/architecture.md). Nothing in this package knows about HTTP.
//
// AOC-005 brought in the toolchain: goose migrations in migrations/, hand-written SQL in
// queries/ compiled by sqlc into sqlcgen/, and the pgx pool in pool.go. The pool is built
// once in cmd/api and closed on shutdown; nothing consumes it yet — AOC-009 brings the first
// real tables and the first caller.
package db
