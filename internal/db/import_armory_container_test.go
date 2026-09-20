package db_test

// AOC-011 verify round 1 — the two source shapes the test plan lists and nothing pinned.
//
// Both are live in the dev import and both are invisible to every other test:
//
//   * 305 source rows carry a container_id (294 Acheronian Cache, 11 Mystical Excavator's Kit).
//     A container is WHERE THE ITEM ACTUALLY COMES FROM for those rows — the dungeon column held
//     a loot bag, not a place (AOC-009). If the importer stopped writing container_id, the rows
//     would still import, the counts would still match, and 305 items would quietly lose their
//     source. Counts cannot see this; only a column assertion can.
//
//   * 449 source rows are unchained, and each must land on the "(Unchained)" PLACE rather than
//     the base one — they are different encounters with different loot. Measured on the dev
//     import, the agreement is total: 438 unchained sources on unchained places, 3,922 base on
//     base, zero crossed. That property is what this pins.
//
// ⚠️ Fixture names are fake; the taxonomy values ("Acheronian Cache", "Dead Man's Hand") are real
// because they must resolve against AOC-009's seeds. Nothing here asserts a game fact: it asserts
// that the value the snapshot supplied is the value the database ends up holding.

import (
	"context"
	"testing"
)

const containerFixtureJSON = `[
{"item_id":9101,"name":"Test Cache Item Epsilon","rarity":"Rare","item_type":"Consumable",
 "pvp_source":false,"has_pvp_stats":false,"pvp_penalty":false,"classes":[],
 "armour_weight":null,"equip_location":null,"item_level":null,"requires_level":null,
 "armor":null,"critigation":null,"dps":null,"damage_range":null,
 "stats":[],"spell_effect":[],"set":null,"set_pieces":null,"faction":null,"faction_rank":null,
 "binding":null,"no_longer_available":false,"tooltip_image":null,"tooltip_source_url":null,
 "sources":[
  {"acquisition_type":"drop","acquisition_cost":[],"container":"Acheronian Cache",
   "unchained":false,"region":null,"region_source":null,"map":null,"instance":null,
   "dungeon_or_raid":null,"boss_or_npc":null,"vendor":null,"quest":null,"is_raid":false,
   "coords":null,"tier":null,"section_raw":"fixture","on_hold":false,"on_hold_reason":null},
  {"acquisition_type":"drop","acquisition_cost":[],"container":null,"unchained":true,
   "region":null,"region_source":null,"map":null,"instance":"Dead Man's Hand",
   "dungeon_or_raid":"Dead Man's Hand (Unchained)","boss_or_npc":null,"vendor":null,
   "quest":null,"is_raid":false,"coords":null,"tier":null,"section_raw":"fixture",
   "on_hold":false,"on_hold_reason":null}]}]`

func TestAContainerSourceKeepsItsContainer(t *testing.T) {
	pool, d := importTarget(t)
	if r := runImport(t, pool, containerFixtureJSON); r.err != nil {
		t.Fatalf("import: %v", r.err)
	}
	ctx := context.Background()

	var container string
	err := d.QueryRowContext(ctx, `
SELECT c.name FROM item_sources s
JOIN containers c ON c.id = s.container_id
WHERE s.item_id = 9101`).Scan(&container)
	if err != nil {
		t.Fatalf("no source resolved its container — the loot bag the row came from was dropped: %v", err)
	}
	if container != "Acheronian Cache" {
		t.Errorf("container = %q, want %q", container, "Acheronian Cache")
	}

	// A container source names no place, and must not have acquired one on the way in.
	var places int
	if err := d.QueryRowContext(ctx, `
SELECT count(*) FROM item_sources
WHERE item_id = 9101 AND container_id IS NOT NULL AND place_id IS NOT NULL`).Scan(&places); err != nil {
		t.Fatalf("counting places on the container row: %v", err)
	}
	if places != 0 {
		t.Errorf("the container row also carries a place_id (%d rows) — a container is not a place (AOC-009)", places)
	}
}

func TestAnUnchainedSourceLandsOnTheUnchainedPlace(t *testing.T) {
	pool, d := importTarget(t)
	if r := runImport(t, pool, containerFixtureJSON); r.err != nil {
		t.Fatalf("import: %v", r.err)
	}

	var name string
	var sourceUnchained, placeUnchained bool
	err := d.QueryRowContext(context.Background(), `
SELECT p.name, s.unchained, p.unchained FROM item_sources s
JOIN places p ON p.id = s.place_id
WHERE s.item_id = 9101`).Scan(&name, &sourceUnchained, &placeUnchained)
	if err != nil {
		t.Fatalf("the unchained source resolved no place at all: %v", err)
	}
	if name != "Dead Man's Hand (Unchained)" {
		t.Errorf("place = %q, want the Unchained variant — the base dungeon is a different "+
			"encounter with different loot (AOC-009)", name)
	}
	if !sourceUnchained || !placeUnchained {
		t.Errorf("source.unchained=%v place.unchained=%v — both must be true, or a reader "+
			"cannot tell which version of the dungeon dropped it", sourceUnchained, placeUnchained)
	}
}
