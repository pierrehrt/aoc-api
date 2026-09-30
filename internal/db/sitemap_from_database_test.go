package db_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/pierrehrt/aoc-api/internal/assets"
	"github.com/pierrehrt/aoc-api/internal/db/sqlcgen"
	"github.com/pierrehrt/aoc-api/internal/httpx"
	"github.com/pierrehrt/aoc-api/internal/items"
	"github.com/pierrehrt/aoc-api/internal/pages"
	"github.com/pierrehrt/aoc-api/internal/templates"
)

// AOC-025: the sitemap is built FROM THE DATABASE — an import that adds items adds their URLs with
// no code change. Proved against a real migrated database, twice imported.
func TestTheSitemapGrowsWithTheImport(t *testing.T) {
	pool, _ := importTarget(t)
	set, err := assets.Load()
	if err != nil {
		t.Fatal(err)
	}
	tpl, err := templates.New(set)
	if err != nil {
		t.Fatal(err)
	}
	site := pages.New(tpl, set, "https://aoc-codex.app", items.NewService(sqlcgen.New(pool)))
	h := httpx.NewRouterWithSite(httpx.Build{Version: "t", Commit: "t", Env: "test"}, site.Routes, set.Handler())

	count := func() int {
		rr := httptest.NewRecorder()
		h.ServeHTTP(rr, httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/sitemaps/1.xml", nil))
		if rr.Code != http.StatusOK {
			t.Fatalf("/sitemaps/1.xml: %d", rr.Code)
		}
		return strings.Count(rr.Body.String(), "<loc>https://aoc-codex.app/armory/")
	}

	if n := count(); n != 0 {
		t.Fatalf("an empty database lists %d item URLs", n)
	}
	if r := runImport(t, pool, fixtureJSON); r.err != nil { // 3 items, 1 excluded by Pierre's rule
		t.Fatal(r.err)
	}
	if n := count(); n != 2 {
		t.Fatalf("after importing 2 live items the sitemap lists %d", n)
	}
	if r := runImport(t, pool, threeLiveItemsJSON); r.err != nil {
		t.Fatal(r.err)
	}
	if n := count(); n != 3 {
		t.Errorf("after an import of 3 the sitemap lists %d item URLs, want 3", n)
	}
}

// Three live, obviously fake items with nothing but what the importer requires.
const threeLiveItemsJSON = `[
{"item_id":9201,"name":"Test Map Alpha","rarity":"Rare","item_type":"Consumable","pvp_source":false,"has_pvp_stats":false,"pvp_penalty":false,"classes":[],"armour_weight":null,"equip_location":"None","item_level":null,"requires_level":null,"armor":null,"critigation":null,"dps":null,"damage_range":null,"stats":[],"spell_effect":[],"set":null,"set_pieces":null,"faction":null,"faction_rank":null,"binding":null,"no_longer_available":false,"tooltip_image":null,"tooltip_source_url":null,"sources":[]},
{"item_id":9202,"name":"Test Map Beta","rarity":"Rare","item_type":"Consumable","pvp_source":false,"has_pvp_stats":false,"pvp_penalty":false,"classes":[],"armour_weight":null,"equip_location":"None","item_level":null,"requires_level":null,"armor":null,"critigation":null,"dps":null,"damage_range":null,"stats":[],"spell_effect":[],"set":null,"set_pieces":null,"faction":null,"faction_rank":null,"binding":null,"no_longer_available":false,"tooltip_image":null,"tooltip_source_url":null,"sources":[]},
{"item_id":9203,"name":"Test Map Gamma","rarity":"Rare","item_type":"Consumable","pvp_source":false,"has_pvp_stats":false,"pvp_penalty":false,"classes":[],"armour_weight":null,"equip_location":"None","item_level":null,"requires_level":null,"armor":null,"critigation":null,"dps":null,"damage_range":null,"stats":[],"spell_effect":[],"set":null,"set_pieces":null,"faction":null,"faction_rank":null,"binding":null,"no_longer_available":false,"tooltip_image":null,"tooltip_source_url":null,"sources":[]}]`
