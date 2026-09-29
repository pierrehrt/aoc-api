// Package items is the bounded area for the Armory: item records, their stats, their
// sources and the sets they belong to.
//
// It holds its own handlers, its service and its tests: the schema's Go side and the
// importer (AOC-010, AOC-011) and the public /v1 read surface (AOC-012).
//
// What belongs here: item domain rules. What does not: HTTP concerns beyond thin
// handlers (see internal/httpx) and raw SQL (see internal/db/queries).
package items
