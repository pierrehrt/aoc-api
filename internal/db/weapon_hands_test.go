package db_test

// AOC-058: a one-handed weapon fits EITHER hand; a two-handed one takes both, and that is a fact about
// its item TYPE (item_types.two_handed). Pierre, 2026-09-30. Fixture names are obviously fake; the
// item-type values are real because they must resolve against the seeds.

import (
	"context"
	"testing"

	"github.com/pressly/goose/v3"
)

const (
	beforeWeaponHands = 20260930120000
	weaponHands       = 20260930130000
)

func TestTwoHandedIsSeededForEveryMainHandTypePierreAnswered(t *testing.T) {
	d, _ := migratedDB(t)
	rows, err := d.QueryContext(context.Background(), `SELECT slug, two_handed FROM item_types WHERE two_handed IS NOT NULL`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	got := map[string]bool{}
	for rows.Next() {
		var slug string
		var two bool
		if err := rows.Scan(&slug, &two); err != nil {
			t.Fatal(err)
		}
		got[slug] = two
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	want := map[string]bool{
		"2hb": true, "2he": true, "staff": true, "bow": true, "polearm": true, "thrown": true,
		"1hb": false, "1he": false, "dagger": false, "talisman": false, "crossbow": false,
	}
	if len(got) != len(want) {
		t.Errorf("classified types = %v, want exactly Pierre's 11", got)
	}
	for slug, two := range want {
		if g, ok := got[slug]; !ok || g != two {
			t.Errorf("%s: two_handed = %v (set %v), want %v", slug, g, ok, two)
		}
	}
}

// ⭐ THE CORRECTION, against rows that exist BEFORE the migration — the way production meets it. A
// one-hander (main + off hand, fit both) becomes `either`; a ring (left + right finger, `either`
// already) and a two-hander (main hand alone, `single`) are untouched; the Down moves back only the
// one-hander.
func TestTheMigrationMakesOneHandersEitherAndItsDownOnlyThem(t *testing.T) {
	url := freshDatabase(t)
	d := openGoose(t, url)
	if err := goose.UpTo(d, migrationsDir, beforeWeaponHands); err != nil {
		t.Fatalf("goose up to %d: %v", beforeWeaponHands, err)
	}
	ctx := context.Background()
	const insert = `
INSERT INTO items (item_id, slug, name, rarity_id, item_type_id, slot_fit_id, confidence_id, source_note)
SELECT $1, $2, $3, (SELECT id FROM rarities LIMIT 1), (SELECT id FROM item_types WHERE slug = $4),
       (SELECT id FROM slot_fits WHERE slug = $5), (SELECT id FROM confidence_levels WHERE slug = 'unconfirmed'),
       'fixture — internal/db/weapon_hands_test.go, not a game fact'`
	for _, it := range []struct {
		id              int
		slug, name, typ string
		fit             string
		slots           []string
	}{
		{970, "test-sword-alpha", "Test Sword Alpha", "1he", "both", []string{"main-hand", "off-hand"}},
		{971, "test-band-beta", "Test Band Beta", "ring", "either", []string{"left-finger", "right-finger"}},
		{972, "test-maul-gamma", "Test Maul Gamma", "2hb", "single", []string{"main-hand"}},
	} {
		if _, err := d.ExecContext(ctx, insert, it.id, it.slug, it.name, it.typ, it.fit); err != nil {
			t.Fatalf("seeding %s: %v", it.name, err)
		}
		for _, sl := range it.slots {
			if _, err := d.ExecContext(ctx, `INSERT INTO item_equip_locations (item_id, equip_location_id)
			                                 VALUES ($1, (SELECT id FROM equip_locations WHERE slug = $2))`, it.id, sl); err != nil {
				t.Fatal(err)
			}
		}
	}

	if err := goose.UpTo(d, migrationsDir, weaponHands); err != nil {
		t.Fatalf("goose up to %d: %v", weaponHands, err)
	}
	for id, want := range map[int]slotState{
		970: {slots: "main-hand,off-hand", fit: "either"},
		971: {slots: "left-finger,right-finger", fit: "either"},
		972: {slots: "main-hand", fit: "single"},
	} {
		if got := slotsOf(t, d, id); got != want {
			t.Errorf("after up, item %d = %+v, want %+v", id, got, want)
		}
	}

	if err := goose.DownTo(d, migrationsDir, beforeWeaponHands); err != nil {
		t.Fatalf("goose down to %d: %v", beforeWeaponHands, err)
	}
	for id, want := range map[int]slotState{
		970: {slots: "main-hand,off-hand", fit: "both"},
		971: {slots: "left-finger,right-finger", fit: "either"}, // was either before the Up: stays
		972: {slots: "main-hand", fit: "single"},
	} {
		if got := slotsOf(t, d, id); got != want {
			t.Errorf("after down, item %d = %+v, want %+v", id, got, want)
		}
	}
}

// The importer half: the fixture's one-hander carries "Main Hand, Off Hand" and must land as
// `either`, with both of its rows.
func TestTheImporterReadsMainHandOffHandAsEither(t *testing.T) {
	pool, d := importTarget(t)
	if r := runImport(t, pool, fixtureJSON); r.err != nil {
		t.Fatalf("import: %v", r.err)
	}
	if got, want := slotsOf(t, d, 9001), (slotState{slots: "main-hand,off-hand", fit: "either"}); got != want {
		t.Errorf("Test Blade Alpha = %+v, want %+v", got, want)
	}
	if got, want := slotsOf(t, d, 9002), (slotState{slots: "left-finger,right-finger", fit: "either"}); got != want {
		t.Errorf("Test Ring Beta = %+v, want %+v", got, want)
	}
}
