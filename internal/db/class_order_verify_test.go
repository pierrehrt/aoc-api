package db_test

import (
	"context"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/pierrehrt/aoc-api/internal/db/sqlcgen"
	"github.com/pierrehrt/aoc-api/internal/items"
)

// AOC-065 verify round 1: "every class list reads classes.sort_order" — the filter chips (the facet
// query), the list's class column (ListItemPageClasses), the item page (ListItemClasses) and
// /v1/taxonomies (ListClasses). TestClassesAreOrderedSoldierRoguePriestMage reads the column itself,
// so a query that went back to ordering by name, by id or by archetype would still pass it.
//
// Here the order is REVERSED in a throwaway database, so it agrees with no other order a query could
// fall back on (name, id, archetype then name), and each list must follow it. No class is named:
// the expected order is read from the column.
func TestEveryClassListFollowsSortOrder(t *testing.T) {
	ctx := context.Background()
	d, url := migratedDB(t)
	seedFacetFixture(t, d)
	if _, err := d.ExecContext(ctx, `UPDATE classes SET sort_order = 1000 - sort_order`); err != nil {
		t.Fatal(err)
	}
	var want []string
	rows, err := d.QueryContext(ctx, `SELECT slug FROM classes ORDER BY sort_order`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	for rows.Next() {
		var slug string
		if err := rows.Scan(&slug); err != nil {
			t.Fatal(err)
		}
		want = append(want, slug)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	if len(want) < 2 {
		t.Fatalf("%d classes seeded; the order cannot be tested", len(want))
	}
	wantS := strings.Join(want, " ")

	// One fixture item restricted to every class, so the per-item lists hold the whole vocabulary.
	var itemID int32
	if err := d.QueryRowContext(ctx, `SELECT item_id FROM items ORDER BY item_id LIMIT 1`).Scan(&itemID); err != nil {
		t.Fatal(err)
	}
	if _, err := d.ExecContext(ctx, `DELETE FROM item_classes WHERE item_id = $1`, itemID); err != nil {
		t.Fatal(err)
	}
	if _, err := d.ExecContext(ctx, `INSERT INTO item_classes (item_id, class_id) SELECT $1, id FROM classes ORDER BY id DESC`, itemID); err != nil {
		t.Fatal(err)
	}

	pool, err := pgxpool.New(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	q := sqlcgen.New(pool)
	s := items.NewService(q)

	check := func(what string, got []string) {
		t.Helper()
		if g := strings.Join(got, " "); g != wantS {
			t.Errorf("%s lists classes as %q, want classes.sort_order: %q", what, g, wantS)
		}
	}

	all, err := q.ListClasses(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, c := range all {
		got = append(got, c.Slug)
	}
	check("ListClasses (/v1/taxonomies)", got)

	page, err := q.ListItemClasses(ctx, itemID)
	if err != nil {
		t.Fatal(err)
	}
	got = nil
	for _, c := range page {
		got = append(got, c.Slug)
	}
	check("ListItemClasses (the item page)", got)

	rowsP, err := q.ListItemPageClasses(ctx, []int32{itemID})
	if err != nil {
		t.Fatal(err)
	}
	got = nil
	for _, c := range rowsP {
		got = append(got, c.Slug)
	}
	check("ListItemPageClasses (the list's class column)", got)

	res, err := s.List(ctx, items.Filters{WithFacets: true, Limit: 1})
	if err != nil {
		t.Fatal(err)
	}
	if res.Facets == nil {
		t.Fatal("no facets")
	}
	got = nil
	for _, v := range res.Facets.Class.Values {
		got = append(got, v.Slug)
	}
	check("the class facet (the filter chips)", got)
}
