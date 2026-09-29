package db_test

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/pierrehrt/aoc-api/internal/db/sqlcgen"
)

// AOC-039 — one expression per published fact.
//
// AOC-012's five defects were one shape: two descriptions of a single rule, free to disagree. Its
// verify round 5 found `unchained` written three ways and the summarised tier reduced with min().
// On the real corpus they agree — because the importer sets both columns from one source, not
// because the SQL makes them. These fixtures plant data that DISAGREES on purpose, bypassing the
// importer, and assert that every query that publishes the fact still gives one answer. A future
// rewrite of any one query that drifts back to its own spelling fails here, on obviously fake rows.
func TestEveryQueryPublishesTheSameUnchainedAndBlanksAnAmbiguousTier(t *testing.T) {
	_, url := migratedDB(t)
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)

	// The fixture: an unchained PLACE holding a source whose own flag is FALSE. The importer would
	// never write this (TestAnUnchainedSourceLandsOnTheUnchainedPlace pins that); the point is that
	// the queries must not depend on it.
	const itemID = 9401
	_, err = pool.Exec(ctx, `
WITH r AS (SELECT id FROM regions ORDER BY id LIMIT 1),
     c AS (SELECT id FROM confidence_levels WHERE slug = 'unconfirmed'),
     ra AS (SELECT id FROM rarities WHERE slug = 'rare'),
     p AS (
       INSERT INTO places (region_id, slug, name, unchained, confidence_id, source_note)
       SELECT r.id, 'test-cave-omega-unchained', 'Test Cave Omega (Unchained)', true, c.id, 'AOC-039 fixture'
       FROM r, c RETURNING id),
     i AS (
       INSERT INTO items (item_id, slug, name, rarity_id, confidence_id, source_note)
       SELECT $1, 'test-relic-omega', 'Test Relic Omega', ra.id, c.id, 'AOC-039 fixture' FROM ra, c
       RETURNING item_id)
INSERT INTO item_sources (item_id, acquisition_type_id, place_id, tier_id, unchained, confidence_id, source_note)
SELECT i.item_id, (SELECT id FROM acquisition_types WHERE slug = 'drop'), p.id,
       (SELECT id FROM tiers WHERE slug = 'pve-1'), false, c.id, 'AOC-039 fixture: flag false in an unchained place'
FROM i, p, c`, itemID)
	if err != nil {
		t.Fatalf("planting the fixture: %v", err)
	}
	q := sqlcgen.New(pool)
	yes := true

	// 1. The list filter finds it as unchained.
	rows, err := q.ListItems(ctx, sqlcgen.ListItemsParams{Unchained: &yes, PageSize: 50})
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, r := range rows {
		if r.ItemID == itemID {
			found = true
		}
	}
	if !found {
		t.Errorf("unchained=true does not list item %d, whose place is unchained", itemID)
	}

	// 2. The per-place summary says the same.
	places, err := q.ListItemPlaces(ctx, []int32{itemID})
	if err != nil {
		t.Fatal(err)
	}
	if len(places) != 1 {
		t.Fatalf("ListItemPlaces returned %d rows, want 1", len(places))
	}
	if !places[0].Unchained {
		t.Errorf("ListItemPlaces says unchained=false for a source in an unchained place — the list would find it and the summary deny it")
	}
	if places[0].TierSlug != "pve-1" {
		t.Errorf("one tier in the place, summarised as %q, want pve-1", places[0].TierSlug)
	}

	// 3. The item page says the same — this was the bare src.unchained before AOC-039.
	srcs, err := q.ListItemSources(ctx, itemID)
	if err != nil {
		t.Fatal(err)
	}
	if len(srcs) != 1 || !srcs[0].Unchained {
		t.Errorf("ListItemSources says unchained=%v for a source in an unchained place — the item page would contradict the list", srcs[0].Unchained)
	}

	// 4. A second source in the same place with a DIFFERENT tier: the summary must blank the tier,
	// exactly as it blanks an ambiguous boss, rather than pick the lower one.
	_, err = pool.Exec(ctx, `
INSERT INTO item_sources (item_id, acquisition_type_id, place_id, tier_id, unchained, confidence_id, source_note)
SELECT $1, (SELECT id FROM acquisition_types WHERE slug = 'drop'),
       (SELECT id FROM places WHERE slug = 'test-cave-omega-unchained'),
       (SELECT id FROM tiers WHERE slug = 'pve-2'), true,
       (SELECT id FROM confidence_levels WHERE slug = 'unconfirmed'), 'AOC-039 fixture: second tier'`, itemID)
	if err != nil {
		t.Fatalf("planting the second source: %v", err)
	}
	places, err = q.ListItemPlaces(ctx, []int32{itemID})
	if err != nil {
		t.Fatal(err)
	}
	if len(places) != 1 {
		t.Fatalf("ListItemPlaces returned %d rows, want 1 (grouped by place)", len(places))
	}
	if places[0].TierSlug != "" {
		t.Errorf("two tiers in one place summarised as %q — a guess where a blank is honest (STEP ZERO)", places[0].TierSlug)
	}
}
