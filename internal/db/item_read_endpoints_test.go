package db_test

import (
	"context"
	"os"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/pierrehrt/aoc-api/internal/db"
	"github.com/pierrehrt/aoc-api/internal/db/sqlcgen"
	"github.com/pierrehrt/aoc-api/internal/items"
)

// AOC-012's read surface, against the REAL imported corpus rather than fixtures.
//
// The service tests next to internal/items prove the collapsing DECISION; these prove the SQL
// behind it, which is the half a fake cannot reach. Counts are asserted as ">= 1" or as relations
// between queries, never as frozen magic numbers: the corpus is re-imported from a snapshot that
// may legitimately grow, and a test that fails when more data arrives is a test nobody keeps.
//
// ⚠️ Except one. The three-place case IS asserted exactly, because it is the acceptance criterion.
func readPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	url := os.Getenv("TEST_DATABASE_URL")
	if url == "" {
		url = os.Getenv("DATABASE_URL")
	}
	if url == "" {
		t.Skip("no TEST_DATABASE_URL or DATABASE_URL — run `make db-up`, migrate and import")
	}
	if h := db.HostOf(url); !db.IsLocalHost(h) {
		t.Fatalf("%q is not local; these tests read the armory corpus and will not run remotely", h)
	}
	pool, err := pgxpool.New(context.Background(), url)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(pool.Close)

	var n int
	if err := pool.QueryRow(context.Background(), `SELECT count(*) FROM items`).Scan(&n); err != nil {
		t.Skipf("no items table yet (%v) — run make migrate-up and cmd/import-armory", err)
	}
	if n == 0 {
		t.Skip("items table is empty — run cmd/import-armory against the dev database")
	}
	return pool
}

func svc(t *testing.T) *items.Service {
	return items.NewService(sqlcgen.New(readPool(t)))
}

// ⭐ THE ACCEPTANCE CRITERION, measured against real rows: an aggregate view returns an item once,
// a single dungeon does not collapse, and two dungeons that share an item show it under each.
func TestCollapsingAgainstTheRealCorpus(t *testing.T) {
	s := svc(t)
	ctx := context.Background()

	// An item known to drop in three distinct places. Found by query, not hardcoded by name, so
	// this keeps working when the snapshot changes.
	var slug string
	var p1, p2 string
	err := readPool(t).QueryRow(ctx, `
		SELECT i.slug, min(p.slug), max(p.slug)
		FROM items i
		JOIN item_sources src ON src.item_id = i.item_id
		JOIN places p ON p.id = src.place_id
		GROUP BY i.item_id, i.slug
		HAVING count(DISTINCT p.slug) = 3
		ORDER BY i.slug
		LIMIT 1`).Scan(&slug, &p1, &p2)
	if err != nil {
		t.Skipf("no item with three distinct places in this corpus: %v", err)
	}

	rowsFor := func(res items.ListResult) int {
		n := 0
		for _, it := range res.Items {
			if it.Slug == slug {
				n++
			}
		}
		return n
	}

	agg, err := s.List(ctx, items.Filters{Query: slug, Limit: 200})
	if err != nil {
		t.Fatal(err)
	}
	// Query is a name search, so fall back to a direct scan if the slug is not the name.
	if rowsFor(agg) == 0 {
		agg, err = s.List(ctx, items.Filters{Limit: 200, Region: ""})
		if err != nil {
			t.Fatal(err)
		}
	}
	if got := rowsFor(agg); got > 1 {
		t.Errorf("aggregate view returned %q %d times, want at most once", slug, got)
	}
	if !agg.Collapsed {
		t.Error("a view with no place filter must report collapsed=true")
	}

	one, err := s.List(ctx, items.Filters{Places: []string{p1}, Limit: 200})
	if err != nil {
		t.Fatal(err)
	}
	if one.Collapsed {
		t.Error("naming one place is not an aggregate view")
	}
	for _, it := range one.Items {
		if it.Place == nil {
			t.Fatalf("a place view returned %q with no place attached", it.Slug)
		}
		if it.Place.Slug != p1 {
			t.Errorf("a one-place view returned a row under %q, want only %q", it.Place.Slug, p1)
		}
	}

	two, err := s.List(ctx, items.Filters{Places: []string{p1, p2}, Limit: 200})
	if err != nil {
		t.Fatal(err)
	}
	if got := rowsFor(two); got != 2 {
		t.Errorf("a two-dungeon selection returned %q %d times, want exactly 2 — once under each; "+
			"this is the case where the duplication IS the information", slug, got)
	}
}

// One source row per boss in the same place must not become two rows for that place: the unit of
// the collapsing rule is the PLACE. This is the bug the grouped query fixed, measured.
func TestAPlaceAppearsOnceEvenWithSeveralSourcesInIt(t *testing.T) {
	pool := readPool(t)
	ctx := context.Background()

	var itemID int32
	var placeSlug string
	err := pool.QueryRow(ctx, `
		SELECT src.item_id, p.slug
		FROM item_sources src JOIN places p ON p.id = src.place_id
		GROUP BY src.item_id, p.slug
		HAVING count(*) > 1
		LIMIT 1`).Scan(&itemID, &placeSlug)
	if err != nil {
		t.Skipf("no item with two sources in one place: %v", err)
	}

	rows, err := sqlcgen.New(pool).ListItemPlaces(ctx, []int32{itemID})
	if err != nil {
		t.Fatal(err)
	}
	seen := 0
	for _, r := range rows {
		if r.PlaceSlug == placeSlug {
			seen++
		}
	}
	if seen != 1 {
		t.Errorf("item %d lists place %q %d times, want once — two bosses in one dungeon is still one dungeon",
			itemID, placeSlug, seen)
	}
}

// The filters AOC-012 adds all reach real rows. A filter that silently matches nothing is
// indistinguishable from one that is not wired up.
func TestTheNewFiltersMatchRealRows(t *testing.T) {
	s := svc(t)
	ctx := context.Background()
	yes := true

	for _, tc := range []struct {
		name string
		f    items.Filters
	}{
		{"armour_weight", items.Filters{ArmourWeight: "heavy", Limit: 1}},
		{"tier", items.Filters{Tier: "pve-6", Limit: 1}},
		{"pvp", items.Filters{PvP: &yes, Limit: 1}},
		{"unchained", items.Filters{Unchained: &yes, Limit: 1}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			res, err := s.List(ctx, tc.f)
			if err != nil {
				t.Fatal(err)
			}
			if res.Total == 0 {
				t.Errorf("%s matched nothing — either the corpus changed or the filter is not wired", tc.name)
			}
		})
	}

	// region is asserted against whatever region actually exists, rather than a hardcoded slug.
	var region string
	if err := readPool(t).QueryRow(ctx, `SELECT slug FROM regions ORDER BY slug LIMIT 1`).Scan(&region); err == nil {
		res, err := s.List(ctx, items.Filters{Region: region, Limit: 1})
		if err != nil {
			t.Fatal(err)
		}
		if res.Total == 0 {
			t.Errorf("region=%s matched nothing", region)
		}
	}
}

// A name search must treat LIKE metacharacters as literal text. Before the escape, "%" returned
// every item in the corpus.
func TestANameSearchDoesNotTreatPercentAsAWildcard(t *testing.T) {
	s := svc(t)
	ctx := context.Background()

	all, err := s.List(ctx, items.Filters{Limit: 1})
	if err != nil {
		t.Fatal(err)
	}
	for _, meta := range []string{"%", "_"} {
		res, err := s.List(ctx, items.Filters{Query: meta, Limit: 1})
		if err != nil {
			t.Fatal(err)
		}
		if res.Total == all.Total {
			t.Errorf("q=%q returned the whole corpus (%d) — the metacharacter reached SQL unescaped",
				meta, res.Total)
		}
	}
}

// Paging past the end reports the true total, so a caller can tell it from "nothing matches".
func TestPagingPastTheEndKeepsTheTotal(t *testing.T) {
	s := svc(t)
	ctx := context.Background()

	first, err := s.List(ctx, items.Filters{Limit: 1})
	if err != nil {
		t.Fatal(err)
	}
	past, err := s.List(ctx, items.Filters{Limit: 5, Offset: int(first.Total) + 1000})
	if err != nil {
		t.Fatal(err)
	}
	if len(past.Items) != 0 {
		t.Errorf("got %d items past the end, want 0", len(past.Items))
	}
	if past.Total != first.Total {
		t.Errorf("total past the end = %d, want %d", past.Total, first.Total)
	}
}

// The item page's payload arrives in one response, and an item with no stats is still a 200.
func TestAnItemArrivesWithEverythingItsPageShows(t *testing.T) {
	s := svc(t)
	ctx := context.Background()

	var slug string
	if err := readPool(t).QueryRow(ctx, `
		SELECT i.slug FROM items i
		JOIN item_stats st ON st.item_id = i.item_id
		JOIN item_sources src ON src.item_id = i.item_id
		GROUP BY i.item_id, i.slug ORDER BY count(*) DESC LIMIT 1`).Scan(&slug); err != nil {
		t.Skipf("no item with stats and sources: %v", err)
	}

	it, err := s.Get(ctx, slug)
	if err != nil {
		t.Fatal(err)
	}
	if it.Slug != slug || it.Name == "" || it.Rarity == "" {
		t.Errorf("item came back incomplete: %+v", it)
	}
	if len(it.Stats) == 0 {
		t.Error("expected stats on an item chosen for having them")
	}
	if len(it.Sources) == 0 {
		t.Error("expected sources on an item chosen for having them")
	}
	if it.Attribution == "" {
		t.Error("the item response dropped the attribution")
	}
}
