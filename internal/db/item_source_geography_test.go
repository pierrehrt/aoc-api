package db_test

// AOC-037 — a source may not carry a geography its place already supplies.
//
// ⭐ THE DEFECT THIS PINS, and why a count could never have seen it. 196 item_sources rows carried
// region `cimmeria` while the places row they point at says Stygia, map Kheshatta. Every one of
// them arrived through the container `Acheronian Cache`, whose observed sources straddle two
// regions (Halls of Eternal Frost is in Cimmeria; Scorpion Cave and Caravan Raider Camp are not),
// so the container's geography was resolved ONCE — from the first of them — and stamped onto all
// 98 items it holds. The rows imported, the counts matched, and the api published the wrong one of
// two disagreeing columns.
//
// ⛔ And the wrong value was the one wearing the highest confidence: it was attributed to Pierre's
// reference_geography.json, so the importer elevated it to `verified` while the correct `derived`
// value sat in the sibling rows.
//
// The fix is structural rather than a corrected value — the source writes its own region/map ONLY
// when its place cannot supply them — so these tests assert AGREEMENT, not a region. They would
// pass just as well if every place in the seed moved to a different continent, which is the point:
// nothing here asserts a game fact (CLAUDE.md STEP ZERO).
//
// ⚠️ The fixture deliberately reproduces the real bad row: a source on the Scorpion Cave
// (Unchained) place carrying region "Cimmeria" and map "Atzel's Approach". Against the importer as
// it was, TestASourceNeverCarriesAGeographyItsPlaceAlreadySupplies FAILS.

import (
	"context"
	"strings"
	"testing"
)

// The bad row as the snapshot actually holds it, with an obviously-fake item.
//
// `region_source: "pierre"` is part of the shape — it is what used to buy this row `verified`.
const contradictingGeographyFixtureJSON = `[
{"item_id":9301,"name":"Test Amulet Kappa","rarity":"Epic","item_type":"Necklace",
 "pvp_source":false,"has_pvp_stats":false,"pvp_penalty":false,"classes":[],
 "armour_weight":null,"equip_location":null,"item_level":null,"requires_level":null,
 "armor":null,"critigation":null,"dps":null,"damage_range":null,
 "stats":[],"spell_effect":[],"set":null,"set_pieces":null,"faction":null,"faction_rank":null,
 "binding":null,"no_longer_available":false,"tooltip_image":null,"tooltip_source_url":null,
 "sources":[
  {"acquisition_type":"drop","acquisition_cost":[],"container":"Acheronian Cache",
   "unchained":true,"region":"Cimmeria","region_source":"pierre","map":"Atzel's Approach",
   "instance":"Scorpion Cave","dungeon_or_raid":"Scorpion Cave (Unchained)","boss_or_npc":null,
   "vendor":null,"quest":null,"is_raid":false,"coords":null,"tier":null,
   "section_raw":"fixture","on_hold":false,"on_hold_reason":null}]}]`

// A source with NO place must keep its own region and map — 1,732 rows in the dev corpus have a
// region and no place, so dropping the columns outright would have thrown their geography away.
const placelessGeographyFixtureJSON = `[
{"item_id":9302,"name":"Test Trinket Lambda","rarity":"Rare","item_type":"Consumable",
 "pvp_source":false,"has_pvp_stats":false,"pvp_penalty":false,"classes":[],
 "armour_weight":null,"equip_location":null,"item_level":null,"requires_level":null,
 "armor":null,"critigation":null,"dps":null,"damage_range":null,
 "stats":[],"spell_effect":[],"set":null,"set_pieces":null,"faction":null,"faction_rank":null,
 "binding":null,"no_longer_available":false,"tooltip_image":null,"tooltip_source_url":null,
 "sources":[
  {"acquisition_type":"drop","acquisition_cost":[],"container":"Acheronian Cache",
   "unchained":false,"region":"Cimmeria","region_source":"derived","map":"Atzel's Approach",
   "instance":null,"dungeon_or_raid":null,"boss_or_npc":null,
   "vendor":null,"quest":null,"is_raid":false,"coords":null,"tier":null,
   "section_raw":"fixture","on_hold":false,"on_hold_reason":null}]}]`

func TestASourceNeverCarriesAGeographyItsPlaceAlreadySupplies(t *testing.T) {
	pool, d := importTarget(t)
	if r := runImport(t, pool, contradictingGeographyFixtureJSON); r.err != nil {
		t.Fatalf("import: %v", r.err)
	}

	var placeName string
	var srcRegion, srcMap *string
	var placeRegion, placeMap string
	err := d.QueryRowContext(context.Background(), `
SELECT p.name, sr.slug, sm.slug, pr.slug, pm.slug
FROM item_sources s
JOIN places p        ON p.id  = s.place_id
JOIN regions pr      ON pr.id = p.region_id
JOIN maps pm         ON pm.id = p.map_id
LEFT JOIN regions sr ON sr.id = s.region_id
LEFT JOIN maps sm    ON sm.id = s.map_id
WHERE s.item_id = 9301`).Scan(&placeName, &srcRegion, &srcMap, &placeRegion, &placeMap)
	if err != nil {
		t.Fatalf("the fixture source resolved no place at all: %v", err)
	}

	// The place is the stronger fact and must be untouched by the source's disagreeing copy.
	if srcRegion != nil {
		t.Errorf("the source kept its own region %q while its place %q says %q — two columns "+
			"describing one fact, free to disagree, is the defect AOC-037 exists for",
			*srcRegion, placeName, placeRegion)
	}
	if srcMap != nil {
		t.Errorf("the source kept its own map %q while its place %q says %q — region was fixed "+
			"and map was left wrong on the very same rows", *srcMap, placeName, placeMap)
	}
}

// ⛔ The provenance line must not survive the value it describes. While the row kept a region from
// reference_geography.json the importer marked it `verified` — the pipeline's highest confidence,
// sitting on what turned out to be the wrong value. With the region dropped, both the confidence
// and the note must fall back to the snapshot's own.
func TestDroppingTheRegionAlsoDropsTheClaimThatPierreSuppliedIt(t *testing.T) {
	pool, d := importTarget(t)
	if r := runImport(t, pool, contradictingGeographyFixtureJSON); r.err != nil {
		t.Fatalf("import: %v", r.err)
	}

	var note, confidence string
	err := d.QueryRowContext(context.Background(), `
SELECT s.source_note, c.slug
FROM item_sources s JOIN confidence_levels c ON c.id = s.confidence_id
WHERE s.item_id = 9301`).Scan(&note, &confidence)
	if err != nil {
		t.Fatalf("reading provenance: %v", err)
	}

	if want := "reference_geography.json"; strings.Contains(note, want) {
		t.Errorf("source_note still claims %q (%q) on a row whose region_id was dropped — a "+
			"provenance line describing a value that is not there", want, note)
	}
	if confidence == "verified" {
		t.Errorf("confidence is still %q, bought by a region this row no longer carries", confidence)
	}
}

// The fallback is not theoretical: 1,732 dev rows have a region and no place.
func TestASourceWithNoPlaceKeepsItsOwnGeography(t *testing.T) {
	pool, d := importTarget(t)
	if r := runImport(t, pool, placelessGeographyFixtureJSON); r.err != nil {
		t.Fatalf("import: %v", r.err)
	}

	var region, mapName *string
	err := d.QueryRowContext(context.Background(), `
SELECT r.slug, m.slug
FROM item_sources s
LEFT JOIN regions r ON r.id = s.region_id
LEFT JOIN maps m    ON m.id = s.map_id
WHERE s.item_id = 9302`).Scan(&region, &mapName)
	if err != nil {
		t.Fatalf("reading the placeless source: %v", err)
	}
	if region == nil {
		t.Error("a source with no place lost its region — the only geography it had")
	}
	if mapName == nil {
		t.Error("a source with no place lost its map — the only geography it had")
	}
}

// ⭐ The invariant itself, over every row present. This is the assertion the ticket's Prevention
// section names, and it is the one that would have caught the original 196 rows: it asserts that
// NOTHING disagrees, rather than sampling rows that happen to agree.
func TestNoSourceContradictsItsPlacesGeography(t *testing.T) {
	pool, d := importTarget(t)
	if r := runImport(t, pool, contradictingGeographyFixtureJSON); r.err != nil {
		t.Fatalf("import: %v", r.err)
	}

	rows, err := d.QueryContext(context.Background(), `
SELECT p.name, coalesce(sr.slug,'-'), coalesce(pr.slug,'-'),
                coalesce(sm.slug,'-'), coalesce(pm.slug,'-')
FROM item_sources s
JOIN places p        ON p.id  = s.place_id
LEFT JOIN regions sr ON sr.id = s.region_id
LEFT JOIN regions pr ON pr.id = p.region_id
LEFT JOIN maps sm    ON sm.id = s.map_id
LEFT JOIN maps pm    ON pm.id = p.map_id
WHERE (s.region_id IS NOT NULL AND s.region_id IS DISTINCT FROM p.region_id)
   OR (s.map_id    IS NOT NULL AND s.map_id    IS DISTINCT FROM p.map_id)`)
	if err != nil {
		t.Fatalf("querying: %v", err)
	}
	defer rows.Close()
	for rows.Next() {
		var place, sRegion, pRegion, sMap, pMap string
		if err := rows.Scan(&place, &sRegion, &pRegion, &sMap, &pMap); err != nil {
			t.Fatalf("scanning: %v", err)
		}
		t.Errorf("a source on %s says region=%s map=%s, its place says region=%s map=%s",
			place, sRegion, sMap, pRegion, pMap)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterating: %v", err)
	}
}
