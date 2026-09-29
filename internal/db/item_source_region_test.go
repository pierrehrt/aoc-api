package db_test

import (
	"context"
	"testing"

	"github.com/pierrehrt/aoc-api/internal/db/sqlcgen"
	"github.com/pierrehrt/aoc-api/internal/items"
)

// AOC-012, verify round 4. The place-first rule reached two of the three queries that publish a
// region, and the third is the one the item page shows.
//
// `ListItems` and `ListItemPlaces` were both moved to coalesce(p.region_id, src.region_id) in
// round 1's B2 fix (DECISIONS.md, 2026-09-21). `ListItemSources` — which feeds
// GET /v1/items/{slug}'s sources[] — still joins `regions rg ON rg.id = src.region_id`, so the two
// public endpoints contradict each other about where the same dungeon is:
//
//	GET /v1/items?place=scorpion-cave-unchained   → place.region "stygia"
//	GET /v1/items/amulet-of-ancient-python        → the same place, region "Cimmeria"
//
// 196 source rows and 98 items are affected, on region and on map alike. One of those two
// statements about the game is wrong whichever way AOC-037 is answered, and a reader gets a
// different answer depending on which endpoint they land on (CLAUDE.md STEP ZERO, rule 13).
//
// Asserted over the whole corpus rather than on the two known rows: the rule is "a source's place
// decides its region", not "these two dungeons are special".
func TestTheItemPageAgreesWithTheListAboutAPlacesRegion(t *testing.T) {
	pool := readPool(t)
	ctx := context.Background()
	q := sqlcgen.New(pool)

	// Every item that has a source whose own region disagrees with its place's. Empty is a pass:
	// it means the corpus no longer diverges, and the assertion still holds trivially.
	rows, err := pool.Query(ctx, `
		SELECT DISTINCT src.item_id
		FROM item_sources src JOIN places p ON p.id = src.place_id
		WHERE src.region_id IS NOT NULL AND src.region_id <> p.region_id
		ORDER BY 1`)
	if err != nil {
		t.Fatal(err)
	}
	var ids []int32
	for rows.Next() {
		var id int32
		if err := rows.Scan(&id); err != nil {
			t.Fatal(err)
		}
		ids = append(ids, id)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	if len(ids) == 0 {
		t.Log("no diverging sources in this corpus; the assertion below is vacuous but still correct")
	}

	for _, id := range ids {
		srcs, err := q.ListItemSources(ctx, id)
		if err != nil {
			t.Fatal(err)
		}
		for _, s := range srcs {
			if s.PlaceName == nil || s.RegionName == nil {
				continue
			}
			var want string
			if err := pool.QueryRow(ctx, `
				SELECT r.name FROM places p JOIN regions r ON r.id = p.region_id
				WHERE p.name = $1`, *s.PlaceName).Scan(&want); err != nil {
				t.Fatalf("resolving the region of %q: %v", *s.PlaceName, err)
			}
			if *s.RegionName != want {
				t.Errorf("item %d: the item page puts %q in %q, the list puts it in %q — "+
					"the place decides the region, in every query that publishes one",
					id, *s.PlaceName, *s.RegionName, want)
			}
		}
	}

	// And the same fact through the service, so the failure is visible at the layer both surfaces
	// call rather than only in the SQL.
	if len(ids) > 0 {
		s := items.NewService(q)
		var slug string
		if err := pool.QueryRow(ctx, `SELECT slug FROM items WHERE item_id = $1`, ids[0]).Scan(&slug); err != nil {
			t.Fatal(err)
		}
		d, err := s.Get(ctx, slug)
		if err != nil {
			t.Fatal(err)
		}
		for _, src := range d.Sources {
			if src.Place == nil || src.Region == nil {
				continue
			}
			var want string
			if err := pool.QueryRow(ctx, `
				SELECT r.name FROM places p JOIN regions r ON r.id = p.region_id
				WHERE p.name = $1`, *src.Place).Scan(&want); err != nil {
				t.Fatal(err)
			}
			if *src.Region != want {
				t.Errorf("Service.Get(%q): source in %q reports region %q, want %q",
					slug, *src.Place, *src.Region, want)
			}
		}
	}
}
