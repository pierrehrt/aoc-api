package db_test

// AOC-051, the gear builder: what a two-hander still lets into the other hand (Pierre, 2026-10-05:
// "with bow yes need amo … throw weapon no amo"), and the hands as the builder derives them. Fixture
// names are obviously fake; the item-type slugs are real because they must resolve against the seeds.

import (
	"context"
	"sort"
	"strings"
	"testing"

	"github.com/pressly/goose/v3"

	"github.com/pierrehrt/aoc-api/internal/db/sqlcgen"
)

const (
	beforeOtherHand = 20261002120000 // AOC-050's migration, the one before this
	otherHand       = 20261005120000
)

func TestOnlyTheBowLetsSomethingIntoTheOtherHand(t *testing.T) {
	d, _ := migratedDB(t)
	ctx := context.Background()
	rows, err := d.QueryContext(ctx, `SELECT t.slug, o.slug FROM item_types t JOIN item_types o ON o.id = t.other_hand_type_id`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var got []string
	for rows.Next() {
		var a, b string
		if err := rows.Scan(&a, &b); err != nil {
			t.Fatal(err)
		}
		got = append(got, a+"→"+b)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	if strings.Join(got, ",") != "bow→ammunition" {
		t.Errorf("other_hand_type_id is set for %v, want exactly bow→ammunition (Pierre: thrown takes none)", got)
	}
	// Only a two-hander can allow something beside it: on a one-hander, or a type that is not a weapon,
	// the question means nothing, and the constraint refuses it.
	for _, slug := range []string{"1he", "shield"} {
		_, err := d.ExecContext(ctx, `UPDATE item_types SET other_hand_type_id = (SELECT id FROM item_types WHERE slug = 'ammunition') WHERE slug = $1`, slug)
		if err == nil || !strings.Contains(err.Error(), "item_types_other_hand_only_two_handed") {
			t.Errorf("%s: %v, want the constraint to refuse it", slug, err)
		}
	}
}

func TestTheOtherHandMigrationRoundTrips(t *testing.T) {
	d := openGoose(t, freshDatabase(t))
	ctx := context.Background()
	column := func() bool {
		var n int
		if err := d.QueryRowContext(ctx, `SELECT count(*) FROM information_schema.columns WHERE table_name = 'item_types' AND column_name = 'other_hand_type_id'`).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n == 1
	}
	if err := goose.UpTo(d, migrationsDir, beforeOtherHand); err != nil {
		t.Fatal(err)
	}
	if column() {
		t.Fatal("the column exists before its migration")
	}
	if err := goose.UpTo(d, migrationsDir, otherHand); err != nil {
		t.Fatal(err)
	}
	if !column() {
		t.Fatal("up: no column")
	}
	if err := goose.DownTo(d, migrationsDir, beforeOtherHand); err != nil {
		t.Fatal(err)
	}
	if column() {
		t.Error("down: the column is still there")
	}
	if err := goose.UpTo(d, migrationsDir, otherHand); err != nil {
		t.Fatalf("up again: %v", err)
	}
}

// The builder's hands are the slots a one-handed weapon fits — derived from the items, so no slot
// is named in code. On the importer's fixture, whose one-hander fits both hands, they are exactly
// the main hand and the off hand.
func TestTheHandsAreWhatAOneHanderFits(t *testing.T) {
	pool, d := importTarget(t)
	if r := runImport(t, pool, fixtureJSON); r.err != nil {
		t.Fatalf("import: %v", r.err)
	}
	ids, err := sqlcgen.New(pool).ListBuildHands(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	var slugs []string
	for _, id := range ids {
		var s string
		if err := d.QueryRowContext(context.Background(), `SELECT slug FROM equip_locations WHERE id = $1`, id).Scan(&s); err != nil {
			t.Fatal(err)
		}
		slugs = append(slugs, s)
	}
	sort.Strings(slugs)
	if strings.Join(slugs, ",") != "main-hand,off-hand" {
		t.Errorf("hands = %v, want main-hand,off-hand", slugs)
	}
}
