package db_test

// AOC-054: the necklace slot, and the one rule that fills it — an item whose tooltip names no slot
// goes in its type's default slot, where item_types has one. The rule is applied in TWO places
// that must agree: the migration's backfill (so production gets it without a re-import) and the
// importer (so a re-import keeps it). One test for each, plus the seed they both read.
//
// ⚠️ EVERY FIXTURE NAME IS OBVIOUSLY FAKE ("Test Pendant Alpha"). The taxonomy VALUES are real
// because they must resolve against the seeds; nothing here is a game fact (CLAUDE.md STEP ZERO).

import (
	"context"
	"database/sql"
	"testing"

	"github.com/pressly/goose/v3"

	"github.com/pierrehrt/aoc-api/internal/items"
)

// AOC-054's migration, and the version just before it — the state production was in when it
// shipped. The backfill test migrates to exactly these two, so a later migration's Up or Down never
// runs against its fixture rows.
const (
	beforeNecklaceSlot = 20260929120000
	necklaceSlot       = 20260930120000
	necklaceMigration  = "20260930120000_necklace_slot.sql"
)

func TestTheNecklaceSlotAndItsRuleAreSeeded(t *testing.T) {
	d, _ := migratedDB(t)
	ctx := context.Background()

	var name string
	if err := d.QueryRowContext(ctx, `SELECT name FROM equip_locations WHERE slug = 'necklace'`).Scan(&name); err != nil {
		t.Fatalf("no necklace slot: %v", err)
	}
	if name != "Necklace" {
		t.Errorf("necklace slot is named %q, want Necklace", name)
	}

	// ⭐ Exactly ONE type has a default slot. A second one is a decision — the reason it is only the
	// necklace is in the migration (two weapon-typed items are really a consumable and a companion,
	// AOC-059) — so adding one must fail here and be made on purpose.
	rows, err := d.QueryContext(ctx, `SELECT t.slug, el.slug FROM item_types t
	                                  JOIN equip_locations el ON el.id = t.default_equip_location_id`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	defaults := map[string]string{}
	for rows.Next() {
		var typ, slot string
		if err := rows.Scan(&typ, &slot); err != nil {
			t.Fatal(err)
		}
		defaults[typ] = slot
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	if len(defaults) != 1 || defaults["necklace"] != "necklace" {
		t.Errorf("item types with a default slot = %v, want exactly necklace -> necklace", defaults)
	}

	// 24 of the 30 types are worn: the 23 that carry a slot in the armory, plus necklace.
	var worn, notWorn int
	if err := d.QueryRowContext(ctx, `SELECT count(*) FILTER (WHERE is_equipment),
	                                         count(*) FILTER (WHERE NOT is_equipment)
	                                  FROM item_types`).Scan(&worn, &notWorn); err != nil {
		t.Fatal(err)
	}
	if worn != 24 || notWorn != 6 {
		t.Errorf("is_equipment: %d worn / %d not, want 24 / 6", worn, notWorn)
	}
	// ⭐ Every type is classified, true or false. The column has no default on purpose: a type
	// added later must be DECIDED, not filed as "not worn" by omission — that omission is this bug.
	var unclassified int
	if err := d.QueryRowContext(ctx, `SELECT count(*) FROM item_types WHERE is_equipment IS NULL`).Scan(&unclassified); err != nil {
		t.Fatal(err)
	}
	if unclassified != 0 {
		t.Errorf("%d item type(s) have no is_equipment — decide whether each is worn, in a migration", unclassified)
	}
	var necklaceWorn bool
	if err := d.QueryRowContext(ctx, `SELECT is_equipment FROM item_types WHERE slug = 'necklace'`).Scan(&necklaceWorn); err != nil || !necklaceWorn {
		t.Errorf("necklace is_equipment = %v (%v), want true — it is the type this ticket exists for", necklaceWorn, err)
	}
}

// ⭐ THE BACKFILL, against rows that exist BEFORE the migration — the only way production meets it.
// A slotless necklace gains the slot; a necklace that already has one keeps exactly what it had; a
// slotless consumable is left alone; a consumable carrying a fit with no rows (a shape the Up never
// makes) keeps its fit through the Down. Then the Down puts every one of them back.
func TestTheMigrationPlacesTheNecklacesAlreadyThereAndItsDownRemovesThem(t *testing.T) {
	url := freshDatabase(t)
	d := openGoose(t, url)
	if err := goose.UpTo(d, migrationsDir, beforeNecklaceSlot); err != nil {
		t.Fatalf("goose up to %d: %v", beforeNecklaceSlot, err)
	}

	for _, it := range []migrationItem{
		{960, "test-pendant-alpha", "Test Pendant Alpha", "necklace", "", nil},
		{961, "test-pendant-beta", "Test Pendant Beta", "necklace", "single", []string{"chest"}},
		{962, "test-tonic-gamma", "Test Tonic Gamma", "consumable", "", nil},
		{963, "test-tonic-delta", "Test Tonic Delta", "consumable", "single", nil},
	} {
		it.seed(t, d)
	}

	if err := goose.UpTo(d, migrationsDir, necklaceSlot); err != nil {
		t.Fatalf("goose up to %d: %v", necklaceSlot, err)
	}
	for id, want := range map[int]slotState{
		960: {slots: "necklace", fit: "single"},
		961: {slots: "chest", fit: "single"}, // its own slot, never replaced by the default
		962: {},
		963: {fit: "single"},
	} {
		if got := slotsOf(t, d, id); got != want {
			t.Errorf("after up, item %d = %+v, want %+v", id, got, want)
		}
	}

	if err := goose.DownTo(d, migrationsDir, beforeNecklaceSlot); err != nil {
		t.Fatalf("goose down to %d: %v", beforeNecklaceSlot, err)
	}
	for id, want := range map[int]slotState{
		960: {},
		961: {slots: "chest", fit: "single"},
		962: {},
		963: {fit: "single"}, // not a defaulted type: the Down leaves its fit alone
	} {
		if got := slotsOf(t, d, id); got != want {
			t.Errorf("after down, item %d = %+v, want %+v", id, got, want)
		}
	}
}

type slotState struct{ slots, fit string }

// migrationItem is an obviously fake item a migration test writes BEFORE the migration under test —
// the way production meets it. One scaffold for every such test, so a column change (AOC-057 drops
// confidence_id and source_note) is one edit. fit "" writes NULL.
type migrationItem struct {
	id              int
	slug, name, typ string
	fit             string
	slots           []string
}

func (it migrationItem) seed(t *testing.T, d *sql.DB) {
	t.Helper()
	ctx := context.Background()
	var fit interface{}
	if it.fit != "" {
		fit = it.fit
	}
	if _, err := d.ExecContext(ctx, `
INSERT INTO items (item_id, slug, name, rarity_id, item_type_id, slot_fit_id, confidence_id, source_note)
SELECT $1, $2, $3, (SELECT id FROM rarities LIMIT 1), (SELECT id FROM item_types WHERE slug = $4),
       (SELECT id FROM slot_fits WHERE slug = $5), (SELECT id FROM confidence_levels WHERE slug = 'unconfirmed'),
       'fixture — a migration test, not a game fact'`, it.id, it.slug, it.name, it.typ, fit); err != nil {
		t.Fatalf("seeding %s: %v", it.name, err)
	}
	for _, sl := range it.slots {
		if _, err := d.ExecContext(ctx, `INSERT INTO item_equip_locations (item_id, equip_location_id)
		                                 VALUES ($1, (SELECT id FROM equip_locations WHERE slug = $2))`, it.id, sl); err != nil {
			t.Fatalf("linking %s to %s: %v", it.name, sl, err)
		}
	}
}

func slotsOf(t *testing.T, d *sql.DB, itemID int) slotState {
	t.Helper()
	var s slotState
	err := d.QueryRowContext(context.Background(), `
SELECT coalesce((SELECT string_agg(el.slug, ',' ORDER BY el.slug)
                 FROM item_equip_locations e JOIN equip_locations el ON el.id = e.equip_location_id
                 WHERE e.item_id = i.item_id), ''),
       coalesce(sf.slug, '')
FROM items i LEFT JOIN slot_fits sf ON sf.id = i.slot_fit_id
WHERE i.item_id = $1`, itemID).Scan(&s.slots, &s.fit)
	if err != nil {
		t.Fatalf("reading item %d's slots: %v", itemID, err)
	}
	return s
}

// The importer half of the rule. A tooltip that names its own slot keeps it — the default is a
// fallback, never an override — and a slotless type with no default stays blank.
const necklaceFixtureJSON = `[
{"item_id":9101,"name":"Test Pendant Alpha","rarity":"Epic","item_type":"Necklace",
 "pvp_source":false,"has_pvp_stats":false,"pvp_penalty":false,"classes":[],
 "armour_weight":null,"equip_location":null,"item_level":80,"requires_level":80,
 "armor":null,"critigation":null,"dps":null,"damage_range":null,
 "stats":[],"spell_effect":[],"set":null,"set_pieces":null,"faction":null,"faction_rank":null,
 "binding":null,"no_longer_available":false,"tooltip_image":null,"tooltip_source_url":null,
 "sources":[]},
{"item_id":9102,"name":"Test Pendant Beta","rarity":"Rare","item_type":"Necklace",
 "pvp_source":false,"has_pvp_stats":false,"pvp_penalty":false,"classes":[],
 "armour_weight":null,"equip_location":"Chest","item_level":70,"requires_level":70,
 "armor":null,"critigation":null,"dps":null,"damage_range":null,
 "stats":[],"spell_effect":[],"set":null,"set_pieces":null,"faction":null,"faction_rank":null,
 "binding":null,"no_longer_available":false,"tooltip_image":null,"tooltip_source_url":null,
 "sources":[]},
{"item_id":9103,"name":"Test Tonic Gamma","rarity":"Rare","item_type":"Consumable",
 "pvp_source":false,"has_pvp_stats":false,"pvp_penalty":false,"classes":[],
 "armour_weight":null,"equip_location":"None","item_level":null,"requires_level":null,
 "armor":null,"critigation":null,"dps":null,"damage_range":null,
 "stats":[],"spell_effect":[],"set":null,"set_pieces":null,"faction":null,"faction_rank":null,
 "binding":null,"no_longer_available":false,"tooltip_image":null,"tooltip_source_url":null,
 "sources":[]}]`

func TestTheImporterPutsASlotlessNecklaceInTheNecklaceSlot(t *testing.T) {
	pool, d := importTarget(t)
	r := runImport(t, pool, necklaceFixtureJSON)
	if r.err != nil {
		t.Fatalf("import: %v", r.err) // includes checkCounts: the default slot's row was expected
	}
	for id, want := range map[int]slotState{
		9101: {slots: "necklace", fit: "single"},
		9102: {slots: "chest", fit: "single"},
		9103: {},
	} {
		if got := slotsOf(t, d, id); got != want {
			t.Errorf("item %d = %+v, want %+v", id, got, want)
		}
	}
	if got := r.rep.TypeDefaultSlots; len(got) != 1 || got["Necklace"] != 1 {
		t.Errorf("report's default-slot counts = %v, want exactly Necklace: 1", got)
	}
}

// ⭐ THE TWO PLACES AGREE — checked here, not only by the one-off comparison on a production dump.
// Production met the rule through the migration's backfill, run over rows an importer WITHOUT
// default slots had written; every later import applies it through slotFit. This replays both from
// one fixture: the old importer (DefaultSlots emptied) followed by the backfill statement read
// verbatim out of the migration file, against the current importer. If the Go rule ever changes,
// this fails and someone decides whether the frozen backfill still describes it.
func TestTheBackfillAndTheImporterAgree(t *testing.T) {
	pool, d := importTarget(t)
	ctx := context.Background()

	old := runImportAdjusted(t, pool, necklaceFixtureJSON, items.Options{},
		func(l *items.Lookups) { l.DefaultSlots = map[string]string{} })
	if old.err != nil {
		t.Fatalf("import as the pre-AOC-054 importer: %v", old.err)
	}
	if got := slotsOf(t, d, 9101); got != (slotState{}) {
		t.Fatalf("replaying the old importer left 9101 = %+v; the replay is not replaying it", got)
	}
	backfill := seedStatementFrom(t, necklaceMigration, "WITH filled AS")
	if _, err := d.ExecContext(ctx, backfill); err != nil {
		t.Fatalf("running the migration's backfill: %v", err)
	}
	viaMigration := allSlots(t, d)

	if r := runImport(t, pool, necklaceFixtureJSON); r.err != nil {
		t.Fatalf("import: %v", r.err)
	}
	viaImporter := allSlots(t, d)

	if len(viaImporter) != 3 {
		t.Fatalf("read %d items back, want the fixture's 3", len(viaImporter))
	}
	for id, want := range viaImporter {
		if got := viaMigration[id]; got != want {
			t.Errorf("item %d: backfill gives %+v, the importer gives %+v", id, got, want)
		}
	}
}

func allSlots(t *testing.T, d *sql.DB) map[int]slotState {
	t.Helper()
	rows, err := d.QueryContext(context.Background(), `SELECT item_id FROM items ORDER BY item_id`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var ids []int
	for rows.Next() {
		var id int
		if err := rows.Scan(&id); err != nil {
			t.Fatal(err)
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	out := map[int]slotState{}
	for _, id := range ids {
		out[id] = slotsOf(t, d, id)
	}
	return out
}
