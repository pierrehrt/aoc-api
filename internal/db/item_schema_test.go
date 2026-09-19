package db_test

// The item schema (AOC-010). Integration tests, like the rest of this package: the thing under
// test is SQL, and SQL that has never met a database is not tested.
//
// ⭐ WHAT THESE PIN. The importer is AOC-011, so there is no real item data here yet and nothing
// below counts rows in a seed. What they pin is the SHAPE — specifically the three places where a
// simpler schema would have given a confidently wrong answer:
//
//  1. a two-hander must appear when you ask for Off Hand items (the join, not a column);
//  2. a fractional stat must survive a round trip (numeric, not integer);
//  3. a spell effect must not be reachable from the stats table (separate tables, not a flag).
//
// ⚠️ EVERY FIXTURE NAME IS OBVIOUSLY FAKE. "Test Blade Alpha", never a real Age of Conan item:
// a fixture that looks like a game fact is one bad query away from being read as one
// (CLAUDE.md STEP ZERO, workflows/3-build.md).

import (
	"context"
	"database/sql"
	"testing"
)

// seedFixtureItems inserts a small, deliberately fake corpus:
//
//	900 Test Blade Alpha    two-hander  -> main-hand + off-hand, fit 'both'
//	901 Test Shield Beta    off-hand only                       fit 'single'
//	902 Test Ring Gamma     either finger                       fit 'either'
//	903 Test Helm Delta     head                                fit 'single'
//	904 Test Potion Epsilon no slot at all                      fit NULL
func seedFixtureItems(t *testing.T, d *sql.DB) {
	t.Helper()
	ctx := context.Background()
	const q = `
INSERT INTO items (item_id, slug, name, rarity_id, item_type_id, slot_fit_id,
                   confidence_id, source_note)
SELECT $1, $2, $3,
       (SELECT id FROM rarities LIMIT 1),
       (SELECT id FROM item_types LIMIT 1),
       (SELECT id FROM slot_fits WHERE slug = $4),
       (SELECT id FROM confidence_levels WHERE slug = 'unconfirmed'),
       'fixture — internal/db/item_schema_test.go, not a game fact'`
	items := []struct {
		id   int
		slug string
		name string
		fit  interface{}
	}{
		{900, "test-blade-alpha", "Test Blade Alpha", "both"},
		{901, "test-shield-beta", "Test Shield Beta", "single"},
		{902, "test-ring-gamma", "Test Ring Gamma", "either"},
		{903, "test-helm-delta", "Test Helm Delta", "single"},
		{904, "test-potion-epsilon", "Test Potion Epsilon", nil},
	}
	for _, it := range items {
		if _, err := d.ExecContext(ctx, q, it.id, it.slug, it.name, it.fit); err != nil {
			t.Fatalf("seeding %s: %v", it.name, err)
		}
	}

	const link = `
INSERT INTO item_equip_locations (item_id, equip_location_id)
VALUES ($1, (SELECT id FROM equip_locations WHERE slug = $2))`
	links := [][2]interface{}{
		{900, "main-hand"}, {900, "off-hand"}, // occupies BOTH
		{901, "off-hand"},
		{902, "left-finger"}, {902, "right-finger"}, // EITHER
		{903, "head"},
		// 904 gets no rows at all — it has no slot
	}
	for _, l := range links {
		if _, err := d.ExecContext(ctx, link, l[0], l[1]); err != nil {
			t.Fatalf("linking %v to %v: %v", l[0], l[1], err)
		}
	}
}

// The criterion this schema shape exists for. With equip_location as a single column on items,
// the two-hander is filed under something like "Main Hand, Off Hand" and this query returns only
// the shield — 390 real two-handers missing from the Off Hand list, silently.
func TestAskingForOffHandItemsReturnsTwoHandersToo(t *testing.T) {
	d, _ := migratedDB(t)
	seedFixtureItems(t, d)

	names := itemNamesInSlot(t, d, "off-hand")
	want := map[string]bool{"Test Blade Alpha": true, "Test Shield Beta": true}
	if len(names) != len(want) {
		t.Fatalf("off-hand items = %v, want exactly %d (the two-hander AND the shield)", names, len(want))
	}
	for _, n := range names {
		if !want[n] {
			t.Errorf("unexpected item in off-hand: %q", n)
		}
	}

	// And the two-hander is in main-hand as well — it occupies both at once, not either.
	if got := itemNamesInSlot(t, d, "main-hand"); len(got) != 1 || got[0] != "Test Blade Alpha" {
		t.Errorf("main-hand items = %v, want [Test Blade Alpha]", got)
	}
}

// A ring is in both finger slots in the join, but `either` says it occupies only one of them.
// The distinction is invisible in the rows and lives entirely in slot_fit — so it is worth a test
// that says out loud which one each fixture is.
func TestSlotFitDistinguishesBothFromEither(t *testing.T) {
	d, _ := migratedDB(t)
	seedFixtureItems(t, d)

	for _, tc := range []struct {
		item string
		fit  string
		rows int
	}{
		{"Test Blade Alpha", "both", 2},
		{"Test Ring Gamma", "either", 2},
		{"Test Helm Delta", "single", 1},
	} {
		var fit string
		var rows int
		err := d.QueryRowContext(context.Background(), `
SELECT coalesce(sf.slug, ''), (SELECT count(*) FROM item_equip_locations WHERE item_id = i.item_id)
FROM items i LEFT JOIN slot_fits sf ON sf.id = i.slot_fit_id
WHERE i.name = $1`, tc.item).Scan(&fit, &rows)
		if err != nil {
			t.Fatalf("%s: %v", tc.item, err)
		}
		if fit != tc.fit || rows != tc.rows {
			t.Errorf("%s: fit=%q rows=%d, want fit=%q rows=%d", tc.item, fit, rows, tc.fit, tc.rows)
		}
	}

	// An item with no slot has no join rows and no fit — not a row pointing at 'none'.
	var rows int
	if err := d.QueryRowContext(context.Background(),
		`SELECT count(*) FROM item_equip_locations WHERE item_id = 904`).Scan(&rows); err != nil {
		t.Fatal(err)
	}
	if rows != 0 {
		t.Errorf("the slotless item has %d equip-location rows, want 0", rows)
	}
}

// 480 real stat values are fractional — Natural Mana/Stamina/Health Regen carry 4.5, 1.6, 2.4.
// An integer column would have truncated every one of them to a quietly wrong whole number.
func TestFractionalStatValuesSurviveARoundTrip(t *testing.T) {
	d, _ := migratedDB(t)
	seedFixtureItems(t, d)
	ctx := context.Background()

	if _, err := d.ExecContext(ctx, `
INSERT INTO item_stats (item_id, stat, value, sign, unit) VALUES
    (900, 'Test Regen Stat', 4.5, 1, 'flat'),
    (900, 'Test Whole Stat', 78,  1, 'flat')`); err != nil {
		t.Fatalf("inserting stats: %v", err)
	}

	var got string
	if err := d.QueryRowContext(ctx,
		`SELECT value::text FROM item_stats WHERE item_id = 900 AND stat = 'Test Regen Stat'`).Scan(&got); err != nil {
		t.Fatal(err)
	}
	if got != "4.50" {
		t.Errorf("fractional stat came back as %q, want \"4.50\" — an integer column truncates it to 4", got)
	}

	// And the column still adds up exactly, which is why it is numeric and not a float.
	var sum string
	if err := d.QueryRowContext(ctx,
		`SELECT sum(value)::text FROM item_stats WHERE item_id = 900`).Scan(&sum); err != nil {
		t.Fatal(err)
	}
	if sum != "82.50" {
		t.Errorf("sum(value) = %q, want \"82.50\"", sum)
	}
}

// A build calculator sums item_stats. If a spell effect could reach that table, eight mounts would
// hand every wearer -8%% Sprinting Stamina Drain (AOC-016). Separate tables make it impossible
// rather than forbidden.
func TestSpellEffectsAreNotReachableFromTheStatsTable(t *testing.T) {
	d, _ := migratedDB(t)
	seedFixtureItems(t, d)
	ctx := context.Background()

	if _, err := d.ExecContext(ctx, `
INSERT INTO item_stats (item_id, stat, value, sign, unit) VALUES (900, 'Test Strength', 78, 1, 'flat')`); err != nil {
		t.Fatal(err)
	}
	if _, err := d.ExecContext(ctx, `
INSERT INTO item_spell_effects (item_id, stat, value, sign, unit)
VALUES (900, 'Test Sprint Drain', 8, -1, 'percent')`); err != nil {
		t.Fatal(err)
	}

	var n int
	if err := d.QueryRowContext(ctx,
		`SELECT count(*) FROM item_stats WHERE item_id = 900`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Errorf("item_stats holds %d rows for the fixture, want 1 — the spell effect leaked in", n)
	}

	var sum string
	if err := d.QueryRowContext(ctx,
		`SELECT sum(value * sign)::text FROM item_stats WHERE item_id = 900`).Scan(&sum); err != nil {
		t.Fatal(err)
	}
	if sum != "78.00" {
		t.Errorf("summing item_stats gives %q, want \"78.00\" — the spell effect was counted", sum)
	}
}

// The schema must never force an importer to invent a value. Every column the snapshot has holes
// in is nullable; a NOT NULL here would mean AOC-011 guessing, which is the one thing this
// product cannot do (CLAUDE.md STEP ZERO).
func TestColumnsTheDataHasHolesInAreNullable(t *testing.T) {
	d, _ := migratedDB(t)

	nullable := map[string][]string{
		"items": {"item_type_id", "slot_fit_id", "armour_weight_id", "binding_id", "item_level", "requires_level",
			"armor", "critigation", "dps", "damage_range", "set_id", "faction_id", "faction_rank",
			"no_longer_available", "tooltip_image", "tooltip_source_url", "open_question"},
		"item_sources": {"acquisition_type_id", "place_id", "boss_id", "vendor_id", "quest_id",
			"container_id", "region_id", "map_id", "tier_id", "coords", "section_raw", "open_question"},
		"sets":       {"class_id", "set_armour_weight_id", "open_question"},
		"item_stats": {"damage_type"},
	}
	for table, cols := range nullable {
		for _, col := range cols {
			var isNullable string
			err := d.QueryRowContext(context.Background(), `
SELECT is_nullable FROM information_schema.columns
WHERE table_schema = 'public' AND table_name = $1 AND column_name = $2`, table, col).Scan(&isNullable)
			if err != nil {
				t.Errorf("%s.%s: %v", table, col, err)
				continue
			}
			if isNullable != "YES" {
				t.Errorf("%s.%s is NOT NULL — an import would have to invent a value", table, col)
			}
		}
	}
}

// A source row names a boss or a vendor, never both. The snapshot's single boss_or_npc field
// conflated them (and `Unchained`, which is a difficulty); AOC-017 split them and this is what
// stops them being re-merged by an importer in a hurry.
func TestASourceCannotBeBothABossAndAVendor(t *testing.T) {
	d, _ := migratedDB(t)
	seedFixtureItems(t, d)

	_, err := d.ExecContext(context.Background(), `
INSERT INTO item_sources (item_id, boss_id, vendor_id, confidence_id, source_note)
SELECT 900, (SELECT id FROM bosses LIMIT 1), (SELECT id FROM places LIMIT 1),
       (SELECT id FROM confidence_levels WHERE slug = 'unconfirmed'), 'fixture'`)
	if err == nil {
		t.Fatal("a row with BOTH a boss and a vendor was accepted; the CHECK constraint is not doing its job")
	}
}

// The item id is the source site's own. Nothing may hand out a new one, because after the origin
// host lapses it is the only key our data and the original still share.
func TestItemIdIsNotGenerated(t *testing.T) {
	d, _ := migratedDB(t)

	var def *string
	if err := d.QueryRowContext(context.Background(), `
SELECT column_default FROM information_schema.columns
WHERE table_schema='public' AND table_name='items' AND column_name='item_id'`).Scan(&def); err != nil {
		t.Fatal(err)
	}
	if def != nil {
		t.Errorf("items.item_id has a default (%q) — it must carry the source site's id, not a generated one", *def)
	}
}

// The converse of the test above, and the reason it exists: a hand-listed set of NULLABLE columns
// can only catch a column that was *supposed* to be nullable and is not. It cannot catch a NOT NULL
// column nobody thought about — which is the only direction that blocks an import. So this pins the
// NOT NULL set exactly: adding one becomes a deliberate act with a test to update, not a default.
//
// Verify round 1 found precisely this: items.item_type_id was NOT NULL and item 4532
// 'Mini-Pet: The Devourer' has no item_type in the snapshot, so AOC-011 would have had to invent
// one. The old test could not have caught it, and did not.
func TestNothingIsNotNullByAccident(t *testing.T) {
	d, _ := migratedDB(t)

	want := map[string][]string{
		// item_id/slug/name identify the row; rarity is on every item in the snapshot; the three
		// pvp booleans default false; provenance is required by DECISIONS.md 2026-09-18.
		"items": {"item_id", "slug", "name", "rarity_id", "pvp_source", "has_pvp_stats",
			"pvp_penalty", "confidence_id", "source_note"},
		"item_sources":         {"id", "item_id", "is_raid", "unchained", "confidence_id", "source_note"},
		"item_stats":           {"id", "item_id", "stat", "value", "sign", "unit", "pvp"},
		"item_spell_effects":   {"id", "item_id", "stat", "value", "sign", "unit", "pvp"},
		"sets":                 {"id", "slug", "name", "confidence_id", "source_note"},
		"vendors":              {"id", "slug", "name", "confidence_id", "source_note"},
		"item_costs":           {"id", "item_source_id", "currency_id", "amount"},
		"item_equip_locations": {"item_id", "equip_location_id"},
		"item_classes":         {"item_id", "class_id"},
	}

	for table, expected := range want {
		rows, err := d.QueryContext(context.Background(), `
SELECT column_name FROM information_schema.columns
WHERE table_schema='public' AND table_name=$1 AND is_nullable='NO' ORDER BY column_name`, table)
		if err != nil {
			t.Fatalf("%s: %v", table, err)
		}
		got := notNullColumns(t, rows)

		for _, c := range expected {
			if !got[c] {
				t.Errorf("%s.%s should be NOT NULL and is not", table, c)
			}
			delete(got, c)
		}
		for c := range got {
			t.Errorf("%s.%s is NOT NULL and nothing says it should be — if the snapshot ever "+
				"lacks this value the import cannot proceed without inventing one", table, c)
		}
	}
}

// A vendor is not a place. 23 distinct vendors across 1,869 source rows, none of which matches any
// of the 86 seeded places: pointing vendor_id at places(id) would have put `Minigames` and
// `Loyalty Rewards` in the browse tree and made "what drops here" answer with vendor stock.
func TestVendorIdPointsAtVendorsNotPlaces(t *testing.T) {
	d, _ := migratedDB(t)

	var target string
	err := d.QueryRowContext(context.Background(), `
SELECT ccu.table_name
FROM information_schema.table_constraints tc
JOIN information_schema.key_column_usage kcu
  ON kcu.constraint_name = tc.constraint_name AND kcu.table_schema = tc.table_schema
JOIN information_schema.constraint_column_usage ccu
  ON ccu.constraint_name = tc.constraint_name AND ccu.table_schema = tc.table_schema
WHERE tc.constraint_type = 'FOREIGN KEY' AND tc.table_name = 'item_sources'
  AND kcu.column_name = 'vendor_id'`).Scan(&target)
	if err != nil {
		t.Fatalf("looking up the vendor_id foreign key: %v", err)
	}
	if target != "vendors" {
		t.Errorf("item_sources.vendor_id references %q, want \"vendors\"", target)
	}
}

func notNullColumns(t *testing.T, rows *sql.Rows) map[string]bool {
	t.Helper()
	defer func() { _ = rows.Close() }()

	got := map[string]bool{}
	for rows.Next() {
		var c string
		if err := rows.Scan(&c); err != nil {
			t.Fatal(err)
		}
		got[c] = true
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return got
}

func itemNamesInSlot(t *testing.T, d *sql.DB, slot string) []string {
	t.Helper()
	rows, err := d.QueryContext(context.Background(), `
SELECT i.name FROM items i
JOIN item_equip_locations iel ON iel.item_id = i.item_id
JOIN equip_locations el ON el.id = iel.equip_location_id
WHERE el.slug = $1 ORDER BY i.name`, slot)
	if err != nil {
		t.Fatalf("querying slot %s: %v", slot, err)
	}
	defer func() { _ = rows.Close() }()

	var out []string
	for rows.Next() {
		var n string
		if err := rows.Scan(&n); err != nil {
			t.Fatal(err)
		}
		out = append(out, n)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return out
}
