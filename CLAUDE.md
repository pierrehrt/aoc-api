# aoc_api — working rules

The backend for the **Age of Conan Codex** (`aoc-codex.app`). Go 1.23+, chi, pgx, sqlc, goose,
Postgres on Railway.

**Planning happens elsewhere.** Every piece of work here comes from a ticket in the
`product_management` repo (`../product_management`), and that repo's `CLAUDE.md` is the operating
manual. Do not invent work here; do not change ticket status from here.

---

## STEP ZERO — you do not know this game

This service serves facts about a 17-year-old MMO whose information is scattered and often describes
a version that no longer exists. **An LLM that "knows" Age of Conan from training data is the single
biggest threat to this product.**

Never write a game fact — a boss mechanic, a class ability, a zone name, an item, a number — into a
seed, a fixture, a migration, a test or a comment unless you can point at where it came from. Test
data uses obviously fake names (`Test Boss Alpha`), never a real boss with plausible mechanics:
fixtures leak into demos and screenshots and end up believed.

An empty field is a feature. A confident guess is a bug that reaches a raid.

Architecture is different — there you are the expert and should have opinions.

---

## The rules that are specific to this repo

1. **`/v1` is a mounted sub-router.** Breaking changes ship as `/v2` beside it, never in place. A
   removed, renamed or retyped field, or a changed response shape, is breaking — even with one
   client, because a deploy of api and web is never simultaneous and browsers hold a cached bundle.
2. **`http.Error` is banned.** Every error response goes through `httpx.Fail`. Services return
   sentinels wrapped with `%w`; the mapper decides the status.
3. **What is logged and what is sent are different.** The client gets the sentinel's text or a flat
   `"internal error"`. Wrapped detail — table names, columns, query fragments — goes to the log with
   the request id, never into a response body.
4. **No hardcoded game taxonomy.** Classes, roles, formats, difficulties, slots, rarities and
   binding values come from the database, not from a Go literal. A list of class names in a handler
   is a bug, not a shortcut.
5. **Content correctness, permissions and moderation live here, never in the SPA.** A client cannot
   be trusted to enforce who may edit what.
6. **Every list endpoint is paginated.** No exceptions, however small the table looks today.
7. **SQL is hand-written in `internal/db/queries/*.sql` and compiled by sqlc.** No ORM. A column
   rename must break the build, not production.
8. **Migrations run against dev first, always.** Never production first.
9. **Docs move with code in the same commit** — routes → `docs/api-routes.md`, schema →
   `docs/database-schema.sql`, package structure → `docs/architecture.md`. `bin/docs-check api` in
   the pm repo says what a diff still owes.
10. **Every production deploy is a release**: semver bump, a `CHANGELOG.md` section (one line per
    ticket, ending with its AOC id), a git tag, and a GitHub release on that tag.
11. **No goroutine without a clear lifetime**, and every `context.Context` propagated to the query.
12. **Branch per ticket, `AOC-NNN-slug`.** (The pm repo is the exception — it works on `main`.)

## Layout

See `docs/architecture.md`, which describes the code **as it is**. Each `internal/` package has a
`doc.go` stating what belongs in it. If new code does not fit an existing package's `doc.go`, that is
a signal to think, not to widen the doc.

## Commands

```
make run      # local server on :8080
make test     # go test ./...
make lint     # go vet ./...
make fmt      # gofmt -w .
make check    # what the gate runs: fmt + vet + test
```

The real gate is `bin/gate api` from the `product_management` repo. It must exit 0 before a ticket
reaches review.

## This is a hobby project with no revenue

Optimise for Pierre's time, low running cost, and not breaking. When two designs are close, pick the
one that is less work to maintain in six months.
