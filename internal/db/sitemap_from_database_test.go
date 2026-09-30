package db_test

import (
	"context"
	"testing"

	"github.com/pierrehrt/aoc-api/internal/db/sqlcgen"
	"github.com/pierrehrt/aoc-api/internal/items"
)

// AOC-025: the sitemap is built FROM THE DATABASE — an import that adds items adds their URLs with
// no code change. The page handler lists exactly what items.Service.Slugs and IDSpan return (pinned
// by the pages tests); this proves those two follow the real tables through two imports. No HTTP
// here: internal/db knows nothing about the surface (doc.go).
func TestTheSitemapsSourceGrowsWithTheImport(t *testing.T) {
	pool, _ := importTarget(t)
	svc := items.NewService(sqlcgen.New(pool))
	ctx := context.Background()
	count := func() (slugs int, total int64) {
		s, err := svc.Slugs(ctx, 50000, 0)
		if err != nil {
			t.Fatal(err)
		}
		span, err := svc.IDSpan(ctx)
		if err != nil {
			t.Fatal(err)
		}
		return len(s), span.Total
	}
	if n, total := count(); n != 0 || total != 0 {
		t.Fatalf("an empty database gives %d slugs, total %d", n, total)
	}
	if r := runImport(t, pool, fixtureJSON); r.err != nil { // 3 items, 1 excluded by Pierre's rule
		t.Fatal(r.err)
	}
	if n, total := count(); n != 2 || total != 2 {
		t.Fatalf("after importing 2 live items: %d slugs, total %d", n, total)
	}
	if r := runImport(t, pool, threeLiveItemsJSON); r.err != nil {
		t.Fatal(r.err)
	}
	if n, total := count(); n != 3 || total != 3 {
		t.Errorf("after an import of 3: %d slugs, total %d, want 3 and 3", n, total)
	}
	// Paged in item-id order, so a chunk boundary never repeats or skips one.
	first, err := svc.Slugs(ctx, 2, 0)
	if err != nil {
		t.Fatal(err)
	}
	rest, err := svc.Slugs(ctx, 2, 2)
	if err != nil {
		t.Fatal(err)
	}
	if got := append(first, rest...); len(got) != 3 || got[0] != "test-map-alpha" || got[2] != "test-map-gamma" {
		t.Errorf("paged slugs = %v, want the three in id order", got)
	}
}

// Three live, obviously fake items with nothing but what the importer requires.
const threeLiveItemsJSON = `[
{"item_id":9201,"name":"Test Map Alpha","rarity":"Rare","item_type":"Consumable","pvp_source":false,"has_pvp_stats":false,"pvp_penalty":false,"classes":[],"armour_weight":null,"equip_location":"None","item_level":null,"requires_level":null,"armor":null,"critigation":null,"dps":null,"damage_range":null,"stats":[],"spell_effect":[],"set":null,"set_pieces":null,"faction":null,"faction_rank":null,"binding":null,"no_longer_available":false,"tooltip_image":null,"tooltip_source_url":null,"sources":[]},
{"item_id":9202,"name":"Test Map Beta","rarity":"Rare","item_type":"Consumable","pvp_source":false,"has_pvp_stats":false,"pvp_penalty":false,"classes":[],"armour_weight":null,"equip_location":"None","item_level":null,"requires_level":null,"armor":null,"critigation":null,"dps":null,"damage_range":null,"stats":[],"spell_effect":[],"set":null,"set_pieces":null,"faction":null,"faction_rank":null,"binding":null,"no_longer_available":false,"tooltip_image":null,"tooltip_source_url":null,"sources":[]},
{"item_id":9203,"name":"Test Map Gamma","rarity":"Rare","item_type":"Consumable","pvp_source":false,"has_pvp_stats":false,"pvp_penalty":false,"classes":[],"armour_weight":null,"equip_location":"None","item_level":null,"requires_level":null,"armor":null,"critigation":null,"dps":null,"damage_range":null,"stats":[],"spell_effect":[],"set":null,"set_pieces":null,"faction":null,"faction_rank":null,"binding":null,"no_longer_available":false,"tooltip_image":null,"tooltip_source_url":null,"sources":[]}]`
