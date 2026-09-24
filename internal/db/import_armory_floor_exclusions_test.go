package db_test

// AOC-042 verify round 1 — the floor must judge WHAT WILL ACTUALLY LAND, not the file's length.
//
// ⭐ WHY THIS TEST EXISTS. Verify re-ran the mutation tests the build session claimed and then tried
// one it had not: change `checkFloor(ctx, tx, len(live), …)` to `len(its)`. The whole suite stayed
// green. That mutant is not cosmetic — it is the ticket's own hazard wearing a different hat: a
// snapshot of 100 items, 80 of them `no_longer_available`, has a perfectly healthy raw length and
// still lands 20 rows over a 100-item corpus. The floor would wave it through and the delete would
// run.
//
// ⚠️ Like the rest of AOC-042's tests, the assertion that matters is THE ROW COUNT AFTERWARDS.

import (
	"fmt"
	"strings"
	"testing"

	"github.com/pierrehrt/aoc-api/internal/items"
)

// excludedItem is minimalItem with the one difference that matters: the source site records it as
// gone, so Import drops it before anything is written. Every name is obviously fake.
func excludedItem(id int) string {
	return fmt.Sprintf(`{"item_id":%d,"name":"Test Filler Item %d","rarity":"Rare","item_type":"Back",
 "pvp_source":false,"has_pvp_stats":false,"pvp_penalty":false,"classes":[],
 "armour_weight":null,"equip_location":"None","item_level":70,"requires_level":70,
 "armor":null,"critigation":null,"dps":null,"damage_range":null,
 "stats":[],"spell_effect":[],"set":null,"set_pieces":null,"faction":null,"faction_rank":null,
 "binding":null,"no_longer_available":"fixture: cannot be obtained any more",
 "tooltip_image":null,"tooltip_source_url":null,"sources":[]}`, id, id)
}

// mixedCorpus builds a snapshot of live+excluded items — a long file that lands a short corpus.
func mixedCorpus(live, excluded int) string {
	parts := make([]string, 0, live+excluded)
	for i := range live {
		parts = append(parts, minimalItem(9100+i))
	}
	for i := range excluded {
		parts = append(parts, excludedItem(9500+i))
	}
	return "[" + strings.Join(parts, ",\n") + "]"
}

func TestTheFloorJudgesWhatLandsNotTheFileLength(t *testing.T) {
	pool, d := importTarget(t)

	if r := runImport(t, pool, corpusOf(100)); r.err != nil {
		t.Fatalf("seeding the corpus: %v", r.err)
	}

	// 100 items in the file — the raw length is unchanged and would clear the floor — but only 20
	// of them are importable.
	snap := mixedCorpus(20, 80)
	its, err := items.DecodeSnapshot(strings.NewReader(snap))
	if err != nil {
		t.Fatalf("decoding: %v", err)
	}
	if len(its) != 100 {
		t.Fatalf("setup: the file holds %d items, want 100 — this test needs a full-length file", len(its))
	}

	r := runImport(t, pool, snap)
	if r.err == nil {
		t.Fatal("a snapshot that lands 20 items over a 100-item corpus was accepted because its " +
			"FILE was long enough — the floor is judging len(its), not what will actually land")
	}
	if got := count(t, d, "items"); got != 100 {
		t.Errorf("items = %d after a refused import, want the original 100", got)
	}
	// The refusal must quote the number that will land, not the file's length.
	if !strings.Contains(r.err.Error(), "20") {
		t.Errorf("the refusal does not name the 20 items that would actually land: %v", r.err)
	}
}

// The mirror image, so the test above cannot pass by refusing everything: exclusions that leave the
// corpus above the floor are still allowed, and the excluded rows really are dropped.
func TestExclusionsAboveTheFloorStillImport(t *testing.T) {
	pool, d := importTarget(t)

	if r := runImport(t, pool, corpusOf(100)); r.err != nil {
		t.Fatalf("seeding the corpus: %v", r.err)
	}
	r := runImport(t, pool, mixedCorpus(95, 20))
	if r.err != nil {
		t.Fatalf("95 landing items over a 100-item corpus was refused: %v", r.err)
	}
	if got := count(t, d, "items"); got != 95 {
		t.Errorf("items = %d, want 95 — the 20 excluded items should not have been written", got)
	}
}
