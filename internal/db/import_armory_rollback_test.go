package db_test

// AOC-011 verify round 2 — a failure AFTER the DELETEs must put the old rows back.
//
// ⚠️ WHY THIS IS NOT THE TEST THAT ALREADY EXISTS. `TestAnUnresolvableValueWritesNothingAtAll`
// pins the *pre-flight* refusal: nothing resolves, so nothing is ever written, and zero rows is
// true trivially. It cannot fail in the interesting direction.
//
// `items.Import` is a FULL REPLACE: it `DELETE`s nine tables and then rebuilds them. The failure
// that would actually hurt is one that happens *after* those DELETEs — the transaction would have
// to roll them back, or a re-import that dies halfway leaves a database with the armory **gone**
// rather than merely unchanged. On dev that is an annoyance; AOC-034 points this same command at
// production. Nothing was asserting it.
//
// The fixture below passes pre-flight (every name resolves) and then fails inside the item insert
// on a duplicate `item_id`, which is the cheapest way to fail late without touching source.

import (
	"context"
	"strings"
	"testing"

	"github.com/pierrehrt/aoc-api/internal/items"
)

func TestAFailureAfterTheDeletesPutsTheOldRowsBack(t *testing.T) {
	pool, d := importTarget(t)

	// 1. A good import, committed. This is the state that must survive.
	if r := runImport(t, pool, fixtureJSON); r.err != nil {
		t.Fatalf("first import: %v", r.err)
	}
	before := snapshotCounts(t, pool, d)
	if before["items"] == 0 {
		t.Fatalf("the first import wrote no items; there is nothing for this test to protect")
	}

	// 2. A snapshot that resolves cleanly but collides on the primary key partway through the
	//    insert — i.e. it gets past pre-flight, past the DELETEs, and then dies.
	duped := strings.Replace(fixtureJSON, `"item_id":9002`, `"item_id":9001`, 1)
	r := runImport(t, pool, duped)
	if r.err == nil {
		t.Fatal("a snapshot with a duplicate item_id was imported successfully; " +
			"this test needs a failure that happens after the DELETEs")
	}
	if strings.Contains(r.err.Error(), "unresolvable") {
		t.Fatalf("the import failed at pre-flight, not after the DELETEs — this test proves "+
			"nothing as written: %v", r.err)
	}

	// 3. The armory is still there. Every table, not just items: the DELETEs hit nine of them.
	after := snapshotCounts(t, pool, d)
	for table, want := range before {
		if got := after[table]; got != want {
			t.Errorf("%s has %d rows after a failed re-import, want %d — the DELETE was not "+
				"rolled back", table, got, want)
		}
	}
}

// The same property stated the other way round, because it is the one a reader will doubt: the
// DELETEs really do run before the inserts, so the rollback above is load-bearing rather than
// incidental. A second import that no longer contains an item must *remove* it — if the import
// only ever added, a mid-flight failure could not lose anything and the test above would be empty
// ceremony.
//
// 📌 An empty snapshot cannot be used to show this: `DecodeSnapshot` refuses a zero-item file
// outright rather than treating it as "delete everything", which is the right refusal and is
// worth knowing about. So the shrunken snapshot keeps one item.
func TestAnItemMissingFromTheNextSnapshotIsRemoved(t *testing.T) {
	pool, d := importTarget(t)
	if r := runImport(t, pool, fixtureJSON); r.err != nil {
		t.Fatalf("first import: %v", r.err)
	}
	if got := count(t, d, "items"); got != 2 {
		t.Fatalf("the fixture imported %d items, want 2 (9003 is excluded)", got)
	}

	const onlyTheRing = `[
{"item_id":9002,"name":"Test Ring Beta","rarity":"Rare","item_type":"Back",
 "pvp_source":false,"has_pvp_stats":false,"pvp_penalty":false,"classes":[],
 "armour_weight":null,"equip_location":"Left/Right Finger","item_level":70,"requires_level":70,
 "armor":null,"critigation":null,"dps":null,"damage_range":null,
 "stats":[],"spell_effect":[],"set":null,"set_pieces":null,"faction":null,"faction_rank":null,
 "binding":null,"no_longer_available":false,"tooltip_image":null,"tooltip_source_url":null,
 "sources":[]}]`

	// ⭐ AllowShrink, and it is not a workaround — it is this test meeting AOC-042's floor honestly.
	// Going from 2 items to 1 is a 50% drop, exactly the shape the floor exists to stop, and this
	// test is the legitimate case the flag was added for: the shrink is the thing being proved.
	// 📌 Worth noticing that the floor caught a real shrink already present in this suite the moment
	// it was added.
	if r := runImportWith(t, pool, onlyTheRing, items.Options{AllowShrink: true}); r.err != nil {
		t.Fatalf("shrunken import: %v", r.err)
	}
	if got := count(t, d, "items"); got != 1 {
		t.Errorf("after a snapshot holding one item the database holds %d — the import is not a "+
			"full replace, so a partial failure could not be caught by rolling back", got)
	}
	var gone int
	if err := d.QueryRowContext(context.Background(), "SELECT count(*) FROM items WHERE item_id = 9001").Scan(&gone); err != nil {
		t.Fatalf("checking for the dropped item: %v", err)
	}
	if gone != 0 {
		t.Errorf("item 9001 survived a snapshot that no longer contains it")
	}
	// Its children went with it — an orphaned stat row would be worse than a stale item.
	if n := count(t, d, "item_stats"); n != 0 {
		t.Errorf("item_stats has %d rows after the only statted item was dropped, want 0", n)
	}
}
