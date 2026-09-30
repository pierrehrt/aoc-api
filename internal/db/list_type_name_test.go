package db_test

import (
	"context"
	"testing"

	"github.com/pierrehrt/aoc-api/internal/db/sqlcgen"
	"github.com/pierrehrt/aoc-api/internal/items"
)

// AOC-062: a list row carries its item type's NAME from the type's own row, beside the slug `/v1`
// already had — so the page's Type column never prints a slug and never capitalises one itself.
func TestAListRowCarriesItsTypesNameFromItsRow(t *testing.T) {
	pool, _ := importTarget(t)
	if r := runImport(t, pool, fixtureJSON); r.err != nil {
		t.Fatal(r.err)
	}
	res, err := items.NewService(sqlcgen.New(pool)).List(context.Background(), items.Filters{Limit: 50})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Items) == 0 {
		t.Fatal("no rows")
	}
	for _, it := range res.Items {
		if it.ItemType == nil {
			continue
		}
		var name string
		if err := pool.QueryRow(context.Background(), `SELECT name FROM item_types WHERE slug = $1`, *it.ItemType).Scan(&name); err != nil {
			t.Fatal(err)
		}
		if it.ItemTypeName == nil || *it.ItemTypeName != name {
			t.Errorf("%s: item_type %q, item_type_name %v, want %q", it.Slug, *it.ItemType, it.ItemTypeName, name)
		}
	}
}
