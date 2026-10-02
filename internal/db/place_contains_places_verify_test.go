package db_test

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/pierrehrt/aoc-api/internal/db/sqlcgen"
	"github.com/pierrehrt/aoc-api/internal/items"
)

// AOC-038, verify round 1. What the corpus cannot show and the unit fake does not reach.
//
// The real hierarchy is one level deep (two raids, five wings) and no item is shared by two wings,
// so the corpus tests prove neither "at any depth" (docs/api-routes.md, docs/architecture.md) nor
// "a shared item under each named wing" against the real SQL; and the unit fake for ExpandPlaces
// expands one level by itself, so it cannot tell a recursive query from a one-level join. This
// plants a three-level hierarchy, a shared item and a cycle on a throwaway database and asks the
// service, which reaches the real ExpandPlaces and ListItems. All names are fake (STEP ZERO).
//
//	test-keep-zeta
//	├── test-hall-zeta      holds 9503
//	│   └── test-vault-zeta holds 9501  (two levels below the keep)
//	├── test-annex-zeta     holds 9502
//	└── test-crypt-zeta     holds 9502  (shared by two sibling wings)
//
//	test-loop-a-zeta <-> test-loop-b-zeta, a cycle; test-loop-b-zeta holds 9504
func TestAPlaceHierarchyExpandsAtAnyDepthAgainstTheRealQueries(t *testing.T) {
	_, url := migratedDB(t)
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)

	_, err = pool.Exec(ctx, `
WITH r AS (SELECT id FROM regions ORDER BY id LIMIT 1),
     c AS (SELECT id FROM confidence_levels WHERE slug = 'unconfirmed')
INSERT INTO places (region_id, slug, name, confidence_id, source_note)
SELECT r.id, v.slug, v.name, c.id, 'AOC-038 verify fixture'
FROM r, c, (VALUES ('test-keep-zeta', 'Test Keep Zeta'), ('test-hall-zeta', 'Test Hall Zeta'),
                   ('test-vault-zeta', 'Test Vault Zeta'), ('test-annex-zeta', 'Test Annex Zeta'),
                   ('test-crypt-zeta', 'Test Crypt Zeta'), ('test-loop-a-zeta', 'Test Loop A Zeta'),
                   ('test-loop-b-zeta', 'Test Loop B Zeta')) AS v(slug, name)`)
	if err != nil {
		t.Fatalf("planting the places: %v", err)
	}
	_, err = pool.Exec(ctx, `
UPDATE places c SET parent_place_id = p.id
FROM (VALUES ('test-hall-zeta', 'test-keep-zeta'), ('test-vault-zeta', 'test-hall-zeta'),
             ('test-annex-zeta', 'test-keep-zeta'), ('test-crypt-zeta', 'test-keep-zeta'),
             ('test-loop-a-zeta', 'test-loop-b-zeta'), ('test-loop-b-zeta', 'test-loop-a-zeta')) AS v(child, parent)
JOIN places p ON p.slug = v.parent
WHERE c.slug = v.child`)
	if err != nil {
		t.Fatalf("planting the hierarchy: %v", err)
	}
	_, err = pool.Exec(ctx, `
WITH c AS (SELECT id FROM confidence_levels WHERE slug = 'unconfirmed'),
     ra AS (SELECT id FROM rarities WHERE slug = 'rare')
INSERT INTO items (item_id, slug, name, rarity_id, confidence_id, source_note)
SELECT v.id, v.slug, v.name, ra.id, c.id, 'AOC-038 verify fixture'
FROM c, ra, (VALUES (9501, 'test-relic-zeta-deep', 'Test Relic Zeta Deep'),
                    (9502, 'test-relic-zeta-shared', 'Test Relic Zeta Shared'),
                    (9503, 'test-relic-zeta-hall', 'Test Relic Zeta Hall'),
                    (9504, 'test-relic-zeta-loop', 'Test Relic Zeta Loop')) AS v(id, slug, name)`)
	if err != nil {
		t.Fatalf("planting the items: %v", err)
	}
	_, err = pool.Exec(ctx, `
INSERT INTO item_sources (item_id, acquisition_type_id, place_id, confidence_id, source_note)
SELECT v.item_id, (SELECT id FROM acquisition_types WHERE slug = 'drop'), p.id,
       (SELECT id FROM confidence_levels WHERE slug = 'unconfirmed'), 'AOC-038 verify fixture'
FROM (VALUES (9501, 'test-vault-zeta'), (9502, 'test-annex-zeta'), (9502, 'test-crypt-zeta'),
             (9503, 'test-hall-zeta'), (9504, 'test-loop-b-zeta')) AS v(item_id, place)
JOIN places p ON p.slug = v.place`)
	if err != nil {
		t.Fatalf("planting the sources: %v", err)
	}

	s := items.NewService(sqlcgen.New(pool))
	list := func(t *testing.T, places ...string) items.ListResult {
		t.Helper()
		// A deadline, because the failure a cycle produces is a query that never returns.
		ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
		defer cancel()
		res, err := s.List(ctx, items.Filters{Places: places, Limit: items.MaxLimit})
		if err != nil {
			t.Fatalf("place=%v: %v", places, err)
		}
		return res
	}
	ids := func(res items.ListResult) map[int32]int {
		out := map[int32]int{}
		for _, it := range res.Items {
			out[it.ID]++
		}
		return out
	}

	t.Run("the top place reaches two levels down, each item once", func(t *testing.T) {
		res := list(t, "test-keep-zeta")
		got := ids(res)
		if !res.Collapsed {
			t.Error("collapsed = false for a place that contains places")
		}
		if got[9501] != 1 {
			t.Errorf("the item two levels below the keep appears %d times, want once: the expansion stopped at the first level", got[9501])
		}
		if got[9502] != 1 {
			t.Errorf("the item shared by two wings appears %d times, want once", got[9502])
		}
		if got[9503] != 1 || res.Total != 3 || len(res.Items) != 3 {
			t.Errorf("total=%d rows=%d %v, want 9501, 9502 and 9503, each once", res.Total, len(res.Items), got)
		}
	})

	t.Run("a middle place is a container too", func(t *testing.T) {
		res := list(t, "test-hall-zeta")
		got := ids(res)
		if !res.Collapsed || res.Total != 2 || got[9501] != 1 || got[9503] != 1 {
			t.Errorf("collapsed=%v total=%d %v, want collapsed with 9501 and 9503", res.Collapsed, res.Total, got)
		}
	})

	t.Run("two sibling wings still show a shared item under each", func(t *testing.T) {
		res := list(t, "test-annex-zeta", "test-crypt-zeta")
		if res.Collapsed {
			t.Error("collapsed = true for two named wings; two dungeons stay expanded")
		}
		under := map[string]bool{}
		for _, it := range res.Items {
			if it.ID != 9502 || it.Place == nil {
				t.Errorf("unexpected row %d under %v", it.ID, it.Place)
				continue
			}
			under[it.Place.Slug] = true
		}
		if len(res.Items) != 2 || !under["test-annex-zeta"] || !under["test-crypt-zeta"] {
			t.Errorf("%d rows under %v, want the shared item once under each wing", len(res.Items), under)
		}
	})

	t.Run("a cycle in parent_place_id ends", func(t *testing.T) {
		res := list(t, "test-loop-a-zeta")
		if got := ids(res); !res.Collapsed || res.Total != 1 || got[9504] != 1 {
			t.Errorf("collapsed=%v total=%d %v, want the loop's one item, once", res.Collapsed, res.Total, got)
		}
	})
}
