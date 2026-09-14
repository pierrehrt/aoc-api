// Package db owns the connection pool and the sqlc-generated query types.
//
// SQL is written by hand in queries/*.sql and compiled to typed Go by sqlc, so the
// schema stays visible and a column rename breaks the build instead of production
// (reference/architecture.md). Nothing in this package knows about HTTP.
//
// Empty until AOC-005 brings in the goose + sqlc toolchain.
package db
