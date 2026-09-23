// Command import-armory loads armory_snapshot/items_clean.json into the database.
//
// This file is WIRING ONLY, like cmd/api: read flags, open a pool, call internal/items, print the
// report. Every decision about what the data means lives in internal/items.
//
//	go run ./cmd/import-armory -snapshot ../armory_snapshot/items_clean.json -dry-run
//	go run ./cmd/import-armory -snapshot ../armory_snapshot/items_clean.json
//
// ⭐ IT SAYS WHICH DATABASE IT IS WRITING TO, BEFORE IT WRITES. The import DELETEs nine tables and
// rebuilds them, and `DATABASE_URL` is the only thing choosing the target — the same variable the
// Makefile tells you to export to production for a migration, in the same shell. So the host is
// printed first, and a non-local one is refused unless -confirm-host names it back. See
// internal/db/target.go. (AOC-011 verify round 1.)
//
// ⭐ IT REFUSES TO GUESS. Before anything is written it resolves every name in the snapshot
// against the seeded taxonomies; an unrecognised value stops the import and is printed. The
// handful of values we have already decided about are listed in internal/items/resolve.go, each
// with its reason, and a decision that no longer matches anything also stops the import — a stale
// exemption is how a check like this quietly stops checking.
//
// ⭐ ONE TRANSACTION. A failure anywhere leaves the database exactly as it was.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"sort"
	"time"

	"github.com/pierrehrt/aoc-api/internal/db"
	"github.com/pierrehrt/aoc-api/internal/items"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "\nimport-armory: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	snapshot := flag.String("snapshot", "../armory_snapshot/items_clean.json", "path to items_clean.json")
	dryRun := flag.Bool("dry-run", false, "resolve everything and roll back instead of committing")
	confirmHost := flag.String("confirm-host", "", "the non-local host you mean to write to, typed out")
	// ⏱ Why this is a flag rather than the constant it used to be (AOC-040). The importer writes
	// ~49,800 rows one statement at a time, and 10 minutes is not enough for the real corpus
	// anywhere: it reached item 418 of 4,648 through the SSH tunnel and item 921 of 4,648 inside
	// Railway, on the private network. Measured, not estimated — removing the tunnel bought 2.2×,
	// not the order of magnitude the round-trip latency suggested it would.
	//
	// A full import is therefore ≈ 50 minutes beside the database. The default stays at 10 minutes
	// so a dev run against a fixture still fails fast rather than hanging; the production run passes
	// a generous value explicitly, which is also a note to whoever reads the command later.
	timeout := flag.Duration("timeout", 10*time.Minute, "overall deadline for the whole import")
	flag.Parse()

	// ---- which database? before anything else, including reading the snapshot ---------------
	banner, err := db.ConfirmTarget(os.Getenv("DATABASE_URL"), *confirmHost, "-confirm-host")
	if err != nil {
		return err
	}
	fmt.Println(banner)
	url := os.Getenv("DATABASE_URL")

	its, err := items.LoadSnapshot(*snapshot)
	if err != nil {
		return err
	}
	fmt.Printf("snapshot: %s — %d items\n", *snapshot, len(its))

	ctx, cancel := context.WithTimeout(context.Background(), *timeout)
	defer cancel()

	pool, err := db.New(ctx, db.DefaultConfig(url))
	if err != nil {
		return err
	}
	defer pool.Close()

	tx, err := pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }() // no-op after a successful commit

	lookups, err := items.LoadLookups(ctx, tx)
	if err != nil {
		return err
	}

	// ---- pre-flight: nothing is written until every name is accounted for -------------------
	unknown, stale := items.CheckResolvable(its, lookups)
	if len(stale) > 0 {
		for _, s := range stale {
			fmt.Fprintf(os.Stderr, "  STALE DECISION: %s\n", s)
		}
		return fmt.Errorf("%d recorded decision(s) no longer match the snapshot — "+
			"review internal/items/resolve.go before importing", len(stale))
	}
	if len(unknown) > 0 {
		fmt.Fprintln(os.Stderr, "\nvalues the database has never been told about:")
		for _, u := range unknown {
			fmt.Fprintf(os.Stderr, "  %-16s %-46q %d row(s)\n", u.Kind, u.Value, u.Rows)
		}
		return fmt.Errorf("%d unresolvable value(s) — either AOC-009's seed is incomplete or the "+
			"snapshot changed. Both need a person; guessing one is what STEP ZERO forbids", len(unknown))
	}
	fmt.Println("pre-flight: every name resolves, and every recorded decision still applies")

	rep, err := items.Import(ctx, tx, its, lookups)
	if err != nil {
		return err
	}
	printReport(rep)

	if *dryRun {
		fmt.Println("\n-dry-run: rolling back")
		return nil
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("committing: %w", err)
	}
	fmt.Println("\ncommitted")
	return nil
}

func printReport(r *items.Report) {
	fmt.Println("\nrows written")
	keys := make([]string, 0, len(r.Counts))
	for k := range r.Counts {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		fmt.Printf("  %-22s %6d\n", k, r.Counts[k])
	}

	fmt.Printf("\nexcluded items (Pierre, 2026-09-13 — cannot be obtained): %d\n", len(r.ExcludedItems))
	for _, e := range r.ExcludedItems {
		fmt.Printf("  %s\n", e)
	}
	fmt.Printf("\nquarantined source rows held back (Pierre, 2026-09-13): %d\n", r.HeldSources)
	fmt.Printf("items left with no source at all: %d %v\n", len(r.ItemsNoSources), r.ItemsNoSources)

	if len(r.AppliedNulls) > 0 {
		fmt.Println("\nrecorded decisions applied — value nulled, question kept:")
		ks := make([]string, 0, len(r.AppliedNulls))
		for k := range r.AppliedNulls {
			ks = append(ks, k)
		}
		sort.Strings(ks)
		for _, k := range ks {
			fmt.Printf("  %-44s %d row(s)\n", k, r.AppliedNulls[k])
		}
	}

	fmt.Println("\nnulls per nullable column (an empty field is a feature; this is the audit):")
	nk := make([]string, 0, len(r.Nulls))
	for k := range r.Nulls {
		nk = append(nk, k)
	}
	sort.Strings(nk)
	for _, k := range nk {
		fmt.Printf("  %-34s %6d\n", k, r.Nulls[k])
	}
}
