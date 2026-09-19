package items_test

// Decoding the armory snapshot. These are unit tests on shapes; the import that writes them to a
// database lives in internal/db (integration, needs Postgres).
//
// ⚠️ Fixture names are obviously fake — "Test Blade Alpha", never a real Age of Conan item. A
// fixture that reads like a game fact is one bad grep away from being taken for one (STEP ZERO).

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/pierrehrt/aoc-api/internal/items"
)

const oneItem = `[{
  "item_id": 900, "name": "Test Blade Alpha", "rarity": "Epic", "item_type": "Weapon",
  "pvp_source": false, "has_pvp_stats": false, "pvp_penalty": false,
  "classes": ["Barbarian"], "armour_weight": null, "equip_location": "Main Hand, Off Hand",
  "item_level": 80, "requires_level": 80, "armor": null, "critigation": null,
  "dps": 157.1, "damage_range": "132-173",
  "stats": [{"stat": "Test Regen", "value": 4.5, "sign": 1, "unit": "flat",
             "damage_type": null, "pvp": false}],
  "spell_effect": [], "set": null, "set_pieces": null, "faction": null, "faction_rank": null,
  "binding": "Binds when Picked Up", "no_longer_available": false,
  "tooltip_image": "https://img.aoc-codex.app/armory/test_blade_alpha.jpg",
  "tooltip_source_url": "https://static.is-better-than.tv/armory/test_blade_alpha.jpg",
  "sources": [
    {"acquisition_type": "drop", "acquisition_cost": [], "container": null, "unchained": false,
     "region": "Stygia", "region_source": "derived", "map": null, "instance": null,
     "dungeon_or_raid": "Test Dungeon Beta", "boss_or_npc": "Test Boss Gamma", "vendor": null,
     "quest": null, "is_raid": true, "coords": [387, 639], "tier": "PvE 5",
     "section_raw": "fixture", "on_hold": false, "on_hold_reason": null},
    {"acquisition_type": "drop", "acquisition_cost": [], "container": null, "unchained": false,
     "region": "Stygia", "region_source": "derived", "map": null, "instance": null,
     "dungeon_or_raid": "Test Dungeon Beta", "boss_or_npc": "Vistrix", "vendor": null,
     "quest": null, "is_raid": true, "coords": null, "tier": "PvE 5",
     "section_raw": "fixture", "on_hold": true,
     "on_hold_reason": "fixture: filed under the wrong boss"}
  ]}]`

func TestDecodeKeepsNullsAsUnknownRatherThanZero(t *testing.T) {
	got, err := items.DecodeSnapshot(strings.NewReader(oneItem))
	if err != nil {
		t.Fatalf("decoding: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("got %d items, want 1", len(got))
	}
	it := got[0]
	if it.ArmourWeight != nil {
		t.Errorf("armour_weight was null in the source and decoded to %q — unknown must stay unknown", *it.ArmourWeight)
	}
	if it.DPS == nil || *it.DPS != 157.1 {
		t.Errorf("dps did not survive as a float: %v — an int column would make this 157", it.DPS)
	}
	if len(it.Stats) != 1 || it.Stats[0].Value != 4.5 {
		t.Errorf("fractional stat value did not survive: %+v", it.Stats)
	}
}

// The snapshot is generated in another repository and has gained fields three times. A field this
// struct does not know about must break the import rather than be dropped in silence.
func TestAnUnknownFieldIsRefused(t *testing.T) {
	drifted := strings.Replace(oneItem, `"item_id": 900,`, `"item_id": 900, "brand_new_field": 1,`, 1)
	_, err := items.DecodeSnapshot(strings.NewReader(drifted))
	if err == nil {
		t.Fatal("an unknown field decoded without error — schema drift would arrive silently")
	}
	if !strings.Contains(err.Error(), "brand_new_field") {
		t.Errorf("the error does not name the offending field: %v", err)
	}
}

func TestQuarantinedSourcesAreHeldBackButNotLost(t *testing.T) {
	got, _ := items.DecodeSnapshot(strings.NewReader(oneItem))
	it := got[0]
	if len(it.Sources) != 2 {
		t.Fatalf("the snapshot must still carry both rows, got %d", len(it.Sources))
	}
	live := it.LiveSources()
	if len(live) != 1 {
		t.Fatalf("got %d live sources, want 1 — the held row must not reach the database", len(live))
	}
	if live[0].BossOrNPC == nil || *live[0].BossOrNPC != "Test Boss Gamma" {
		t.Errorf("the wrong row survived: %+v", live[0].BossOrNPC)
	}
}

func TestExcludedReadsTheReasonRatherThanAFlag(t *testing.T) {
	for _, tc := range []struct {
		json string
		want bool
	}{
		{`false`, false},
		{`null`, false},
		{`"the source site records it as 'No longer available'"`, true},
	} {
		src := strings.Replace(oneItem, `"no_longer_available": false`, `"no_longer_available": `+tc.json, 1)
		got, err := items.DecodeSnapshot(strings.NewReader(src))
		if err != nil {
			t.Fatalf("%s: %v", tc.json, err)
		}
		if _, excluded := got[0].Excluded(); excluded != tc.want {
			t.Errorf("no_longer_available=%s -> excluded=%v, want %v", tc.json, excluded, tc.want)
		}
	}
}

// Against the real file when it is next door: CI does not have armory_snapshot, so this skips
// there. It is a canary for the generator changing under us, not a substitute for the counts the
// importer asserts.
func TestTheRealSnapshotStillDecodes(t *testing.T) {
	path := filepath.Join("..", "..", "..", "armory_snapshot", "items_clean.json")
	if _, err := os.Stat(path); err != nil {
		t.Skip("armory_snapshot is not next to this repo — nothing to check")
	}
	got, err := items.LoadSnapshot(path)
	if err != nil {
		t.Fatalf("the real snapshot no longer decodes: %v", err)
	}
	var excluded, live, held int
	for _, i := range got {
		if _, ex := i.Excluded(); ex {
			excluded++
			continue
		}
		live += len(i.LiveSources())
		held += len(i.Sources) - len(i.LiveSources())
	}
	t.Logf("items=%d excluded=%d live_sources=%d held=%d", len(got), excluded, live, held)
	if excluded != 2 {
		t.Errorf("excluded %d items, want 2 (Pierre's 2026-09-13 decision)", excluded)
	}
	if held != 68 {
		t.Errorf("held %d source rows, want 68 (the quarantined attributions)", held)
	}
}
