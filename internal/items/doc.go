// Package items is the bounded area for the Armory: item records, their stats, their
// sources and the sets they belong to.
//
// It holds its own handlers, its service and its tests. It is empty until AOC-010
// defines the schema and AOC-012 adds the public read endpoints; the directory and
// this file exist so the first ticket to need it finds the shape already decided,
// rather than inventing a second one.
//
// What belongs here: item domain rules. What does not: HTTP concerns beyond thin
// handlers (see internal/httpx) and raw SQL (see internal/db/queries).
package items
