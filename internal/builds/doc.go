// Package builds is the gear builder (AOC-051): a build is an item per equipment slot and an
// optional class; this package reads one from a URL, places an item in it, and computes what the
// builder shows — each slot's status and the combined stats.
//
// It holds its own service, its /v1 handler and its tests. The HTML Armory page and
// GET /v1/builds/compute both call Service, so a rule (what fits where, what a two-hander locks,
// what a class cannot wear, what is summed) is written once (CLAUDE.md rule 5b). The browser's
// island never decides where an item goes.
//
// What belongs here: build rules. What does not: item reads (internal/items), HTTP concerns beyond
// a thin handler (internal/httpx) and raw SQL (internal/db/queries/builds.sql). No slot, class or
// weapon type is named in code: they are rows (reference/content-model.md § 0).
package builds
