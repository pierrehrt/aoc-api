package db_test

// AOC-058: a one-handed weapon fits EITHER hand; a two-handed one takes both, and that is a fact about
// its item TYPE (item_types.two_handed). Pierre, 2026-09-30. Fixture names are obviously fake; the
// item-type values are real because they must resolve against the seeds.

import (
	"context"
	"strings"
	"testing"

	"github.com/pressly/goose/v3"
)

const (
	beforeWeaponHands = necklaceSlot // AOC-054's migration, the one before this
	weaponHands       = 20260930130000
	weaponMigration   = "20260930130000_weapon_hands.sql"
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
	for _, it := range []migrationItem{
		{970, "test-sword-alpha", "Test Sword Alpha", "1he", "both", []string{"main-hand", "off-hand"}},
		{971, "test-band-beta", "Test Band Beta", "ring", "either", []string{"left-finger", "right-finger"}},
		{972, "test-maul-gamma", "Test Maul Gamma", "2hb", "single", []string{"main-hand"}},
		// a `both` of another shape (one row): not what the Up is for, so it stays `both` both ways
		{973, "test-odd-delta", "Test Odd Delta", "1he", "both", []string{"main-hand"}},
	} {
		it.seed(t, d)
	}

	if err := goose.UpTo(d, migrationsDir, weaponHands); err != nil {
		t.Fatalf("goose up to %d: %v", weaponHands, err)
	}
	for id, want := range map[int]slotState{
		970: {slots: "main-hand,off-hand", fit: "either"},
		971: {slots: "left-finger,right-finger", fit: "either"},
		972: {slots: "main-hand", fit: "single"},
		973: {slots: "main-hand", fit: "both"},
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
		973: {slots: "main-hand", fit: "both"},
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

// ⭐ The migration and the importer apply ONE rule — checked here, not only by the one-off comparison
// on a production dump (the AOC-054 pattern). The fixture is imported by the current importer; its
// one-hander is put back to `both`, the way production holds it before this migration; the
// migration's own UPDATE, read out of the file, must land it where the importer did.
func TestTheWeaponMigrationAndTheImporterAgree(t *testing.T) {
	pool, d := importTarget(t)
	if r := runImport(t, pool, fixtureJSON); r.err != nil {
		t.Fatalf("import: %v", r.err)
	}
	viaImporter := allSlots(t, d)
	ctx := context.Background()
	if _, err := d.ExecContext(ctx, `UPDATE items SET slot_fit_id = (SELECT id FROM slot_fits WHERE slug = 'both') WHERE item_id = 9001`); err != nil {
		t.Fatal(err)
	}
	if _, err := d.ExecContext(ctx, seedStatementFrom(t, weaponMigration, "UPDATE items SET slot_fit_id")); err != nil {
		t.Fatalf("running the migration's UPDATE: %v", err)
	}
	for id, want := range viaImporter {
		if got := allSlots(t, d)[id]; got != want {
			t.Errorf("item %d: the migration gives %+v, the importer %+v", id, got, want)
		}
	}
}

// A compound slot value nobody has decided about stops the import BEFORE anything is deleted —
// punctuation is never read as meaning again (AOC-058 review).
func TestAnUnknownCompoundSlotStopsTheImport(t *testing.T) {
	pool, d := importTarget(t)
	if r := runImport(t, pool, fixtureJSON); r.err != nil {
		t.Fatalf("first import: %v", r.err)
	}
	before := count(t, d, "items")
	odd := strings.Replace(fixtureJSON, `"equip_location":"Left/Right Finger"`, `"equip_location":"Head, Chest"`, 1)
	if odd == fixtureJSON {
		t.Fatal("the fixture no longer carries the ring's compound value")
	}
	r := runImport(t, pool, odd)
	if r.err == nil || !strings.Contains(r.err.Error(), "REFUSING TO GUESS") || !strings.Contains(r.err.Error(), `"Head, Chest"`) {
		t.Fatalf("import of an unknown compound = %v, want a refusal naming it", r.err)
	}
	if after := count(t, d, "items"); after != before {
		t.Errorf("items went from %d to %d — the refusal must come before the delete", before, after)
	}
}
