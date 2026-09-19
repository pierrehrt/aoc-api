package db_test

// AOC-010 verify round 1 — the GENERATED item queries, against a real migrated database.
//
// ⭐ WHY THIS FILE EXISTS, separately from item_schema_test.go. That file proves the SHAPE of the
// schema using queries written inside the test file itself. None of the twelve queries in
// internal/db/queries/items.sql was executed by any test: TestGeneratedQueriesRunAgainstTheRealSchema
// only ever ran GetProbe and CountProbes. So the acceptance criterion that the whole schema shape
// exists for — "a query for every Off Hand item returns the two-handers too" — was pinned for a
// query the site will never run, and `ListItems` could have filtered a column instead of the join
// with every test still green. These tests run the shipped queries.
//
// ⚠️ EVERY FIXTURE NAME IS OBVIOUSLY FAKE — "Verify Greatsword Iota", never a real Age of Conan
// item, and every source_note says it is a fixture (CLAUDE.md STEP ZERO).

import (
	"context"
	"database/sql"
	"fmt"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/pierrehrt/aoc-api/internal/db/sqlcgen"
)

// queriesOn migrates a throwaway database, seeds the shared fixture corpus and hands back both a
// sqlc Queries (the shipped code path) and the raw handle (for seeding only).
func queriesOn(t *testing.T) (*sqlcgen.Queries, *sql.DB) {
	t.Helper()
	d, url := migratedDB(t)
	seedFixtureItems(t, d)

	pool, err := pgxpool.New(context.Background(), url)
	if err != nil {
		t.Fatalf("pool: %v", err)
	}
	t.Cleanup(pool.Close)
	return sqlcgen.New(pool), d
}

// numText renders a pgtype.Numeric the way the database wrote it, so a test can say 4.50 and mean
// it rather than comparing floats.
func numText(t *testing.T, n pgtype.Numeric) string {
	t.Helper()
	if !n.Valid {
		return "<null>"
	}
	v, err := n.Value()
	if err != nil {
		t.Fatalf("numeric value: %v", err)
	}
	return fmt.Sprint(v)
}

func names(rows []sqlcgen.ListItemsRow) []string {
	out := make([]string, 0, len(rows))
	for _, r := range rows {
		out = append(out, r.Name)
	}
	return out
}

func strPtr(s string) *string { return &s }

// ⭐ THE CRITERION, against the query the Armory page will actually run.
//
// ListItems filters the slot through EXISTS on item_equip_locations. Point it at a column on
// items instead and the two-hander disappears from the Off Hand list — 390 real items, silently.
func TestListItemsFindsTwoHandersWhenAskedForOffHand(t *testing.T) {
	q, _ := queriesOn(t)

	rows, err := q.ListItems(context.Background(), sqlcgen.ListItemsParams{
		EquipLocation: strPtr("off-hand"),
		PageSize:      50,
	})
	if err != nil {
		t.Fatalf("ListItems: %v", err)
	}
	got := names(rows)
	want := map[string]bool{"Test Blade Alpha": true, "Test Shield Beta": true}
	if len(got) != len(want) {
		t.Fatalf("ListItems(off-hand) = %v, want exactly the two-hander AND the off-hand-only item", got)
	}
	for _, n := range got {
		if !want[n] {
			t.Errorf("unexpected item in the off-hand list: %q", n)
		}
	}
	for _, r := range rows {
		if r.TotalCount != 2 {
			t.Errorf("total_count = %d on row %q, want 2", r.TotalCount, r.Name)
		}
	}

	// The ring is in BOTH finger slots in the join but occupies only one of them: asking for a
	// single finger must still find it, which is what slot_fit 'either' means.
	for _, slot := range []string{"left-finger", "right-finger"} {
		rows, err := q.ListItems(context.Background(), sqlcgen.ListItemsParams{
			EquipLocation: strPtr(slot), PageSize: 50,
		})
		if err != nil {
			t.Fatalf("ListItems(%s): %v", slot, err)
		}
		if len(rows) != 1 || rows[0].Name != "Test Ring Gamma" {
			t.Errorf("ListItems(%s) = %v, want [Test Ring Gamma]", slot, names(rows))
		}
	}

	// And an unknown slug is an empty list, not an error and not everything.
	rows, err = q.ListItems(context.Background(), sqlcgen.ListItemsParams{
		EquipLocation: strPtr("no-such-slot"), PageSize: 50,
	})
	if err != nil {
		t.Fatalf("ListItems(unknown slot): %v", err)
	}
	if len(rows) != 0 {
		t.Errorf("ListItems(unknown slot) = %v, want nothing", names(rows))
	}
}

// Every filter is optional and NULL means "do not filter". A regression here turns an unfiltered
// Armory into an empty one, or a filtered one into the whole corpus.
func TestListItemsTreatsEveryFilterAsOptional(t *testing.T) {
	q, _ := queriesOn(t)
	ctx := context.Background()

	all, err := q.ListItems(ctx, sqlcgen.ListItemsParams{PageSize: 50})
	if err != nil {
		t.Fatalf("ListItems(no filters): %v", err)
	}
	if len(all) != 5 {
		t.Fatalf("ListItems(no filters) returned %d rows (%v), want the 5 fixtures", len(all), names(all))
	}
	if all[0].Name != "Test Blade Alpha" {
		t.Errorf("first row is %q — ORDER BY i.name is not holding", all[0].Name)
	}

	byName, err := q.ListItems(ctx, sqlcgen.ListItemsParams{NameQuery: strPtr("ring"), PageSize: 50})
	if err != nil {
		t.Fatalf("ListItems(name): %v", err)
	}
	if len(byName) != 1 || byName[0].Name != "Test Ring Gamma" {
		t.Errorf("ListItems(name=ring) = %v, want [Test Ring Gamma]", names(byName))
	}

	// The slotless item has a NULL slot_fit and must still come back in an unfiltered list: a
	// LEFT JOIN turned inner join would drop all 340 consumables and quest items.
	var found bool
	for _, r := range all {
		if r.Name == "Test Potion Epsilon" {
			found = true
			if r.SlotFit != nil {
				t.Errorf("the slotless item reports slot_fit=%q, want NULL", *r.SlotFit)
			}
		}
	}
	if !found {
		t.Error("the slotless item is missing from an unfiltered list — the slot_fits join is not LEFT")
	}
}

// The list page is paginated, and total_count must describe the whole filtered set rather than
// the page. A count that follows the LIMIT gives every page the same "5 of 5".
func TestListItemsPaginatesAndCountsTheWholeResult(t *testing.T) {
	q, _ := queriesOn(t)
	ctx := context.Background()

	page1, err := q.ListItems(ctx, sqlcgen.ListItemsParams{PageSize: 2, PageOffset: 0})
	if err != nil {
		t.Fatalf("page 1: %v", err)
	}
	if len(page1) != 2 {
		t.Fatalf("page 1 has %d rows, want 2", len(page1))
	}
	if page1[0].TotalCount != 5 {
		t.Errorf("total_count = %d on a page of 2, want 5 — the count is being limited too", page1[0].TotalCount)
	}

	page3, err := q.ListItems(ctx, sqlcgen.ListItemsParams{PageSize: 2, PageOffset: 4})
	if err != nil {
		t.Fatalf("page 3: %v", err)
	}
	if len(page3) != 1 {
		t.Errorf("last page has %d rows, want the 1 remainder", len(page3))
	}

	past, err := q.ListItems(ctx, sqlcgen.ListItemsParams{PageSize: 2, PageOffset: 99})
	if err != nil {
		t.Fatalf("past the end: %v", err)
	}
	if len(past) != 0 {
		t.Errorf("an offset past the end returned %d rows, want none", len(past))
	}
}

// numeric(6,2) on dps and numeric(8,2) on stat values, read back through the generated code and
// not just through psql. 699 real items have a fractional dps and 480 stat rows are fractional.
func TestTheGeneratedCodeReadsFractionalNumbersBack(t *testing.T) {
	q, d := queriesOn(t)
	ctx := context.Background()

	if _, err := d.ExecContext(ctx,
		`UPDATE items SET dps = 157.1, damage_range = '154-2421' WHERE item_id = 900`); err != nil {
		t.Fatalf("setting dps: %v", err)
	}
	if _, err := d.ExecContext(ctx, `
INSERT INTO item_stats (item_id, stat, value, sign, unit) VALUES
    (900, 'Verify Regen Stat', 1.6, 1, 'flat'),
    (900, 'Verify Whole Stat', 78, 1, 'flat')`); err != nil {
		t.Fatalf("inserting stats: %v", err)
	}

	item, err := q.GetItem(ctx, 900)
	if err != nil {
		t.Fatalf("GetItem: %v", err)
	}
	if got := numText(t, item.Dps); got != "157.10" {
		t.Errorf("dps came back as %s, want 157.10", got)
	}

	stats, err := q.ListItemStats(ctx, 900)
	if err != nil {
		t.Fatalf("ListItemStats: %v", err)
	}
	if len(stats) != 2 {
		t.Fatalf("ListItemStats returned %d rows, want 2", len(stats))
	}
	want := map[string]string{"Verify Regen Stat": "1.60", "Verify Whole Stat": "78.00"}
	for _, s := range stats {
		if got := numText(t, s.Value); got != want[s.Stat] {
			t.Errorf("%s = %s, want %s — an integer column would have truncated it", s.Stat, got, want[s.Stat])
		}
	}
}

// 215 real items (4.6 %) have no stats at all, and 4,639 have no spell effect. Both reads must be
// an empty list, never an error and never a nil the caller has to special-case.
func TestAnItemWithNothingOnItReadsAsEmptyLists(t *testing.T) {
	q, _ := queriesOn(t)
	ctx := context.Background()

	stats, err := q.ListItemStats(ctx, 904)
	if err != nil {
		t.Fatalf("ListItemStats on a bare item: %v", err)
	}
	if len(stats) != 0 {
		t.Errorf("a bare item reports %d stats, want none", len(stats))
	}
	effects, err := q.ListItemSpellEffects(ctx, 904)
	if err != nil {
		t.Fatalf("ListItemSpellEffects on a bare item: %v", err)
	}
	if len(effects) != 0 {
		t.Errorf("a bare item reports %d spell effects, want none", len(effects))
	}
	locs, err := q.ListItemEquipLocations(ctx, 904)
	if err != nil {
		t.Fatalf("ListItemEquipLocations on a slotless item: %v", err)
	}
	if len(locs) != 0 {
		t.Errorf("the slotless item reports %d equip locations, want none", len(locs))
	}
	classes, err := q.ListItemClasses(ctx, 904)
	if err != nil {
		t.Fatalf("ListItemClasses: %v", err)
	}
	if len(classes) != 0 {
		t.Errorf("an unrestricted item reports %d classes, want none", len(classes))
	}
}

// ⭐ The read side of "a stat and a spell effect are different things". ListItemStats and
// ListItemSpellEffects must never see each other's rows, or a build calculator sums a mount's
// -8 % Sprinting Stamina Drain into somebody's gear (AOC-016).
func TestTheGeneratedReadsKeepStatsAndSpellEffectsApart(t *testing.T) {
	q, d := queriesOn(t)
	ctx := context.Background()

	if _, err := d.ExecContext(ctx, `
INSERT INTO item_stats (item_id, stat, value, sign, unit) VALUES (900, 'Verify Strength', 78, 1, 'flat')`); err != nil {
		t.Fatal(err)
	}
	if _, err := d.ExecContext(ctx, `
INSERT INTO item_spell_effects (item_id, stat, value, sign, unit)
VALUES (900, 'Verify Sprint Drain', 8, -1, 'percent')`); err != nil {
		t.Fatal(err)
	}

	stats, err := q.ListItemStats(ctx, 900)
	if err != nil {
		t.Fatal(err)
	}
	if len(stats) != 1 || stats[0].Stat != "Verify Strength" {
		t.Errorf("ListItemStats = %+v, want only the stat", stats)
	}
	effects, err := q.ListItemSpellEffects(ctx, 900)
	if err != nil {
		t.Fatal(err)
	}
	if len(effects) != 1 || effects[0].Stat != "Verify Sprint Drain" {
		t.Errorf("ListItemSpellEffects = %+v, want only the spell effect", effects)
	}
}

// The test plan's source edge cases, through the shipped query: an item with three sources, one of
// which has no region, no boss and no place at all, and one of which carries a price.
func TestListItemSourcesHandlesThreeSourcesAndTheEmptyOne(t *testing.T) {
	q, d := queriesOn(t)
	ctx := context.Background()

	if _, err := d.ExecContext(ctx, `
INSERT INTO item_sources (item_id, place_id, confidence_id, source_note)
SELECT 900, p.id, (SELECT id FROM confidence_levels WHERE slug='unconfirmed'), 'fixture'
FROM places p ORDER BY p.id LIMIT 2`); err != nil {
		t.Fatalf("seeding placed sources: %v", err)
	}
	// The bare one: no place, no boss, no vendor, no region. 1,742 real items have no place at all.
	var bareID int64
	if err := d.QueryRowContext(ctx, `
INSERT INTO item_sources (item_id, confidence_id, source_note)
VALUES (900, (SELECT id FROM confidence_levels WHERE slug='unconfirmed'), 'fixture')
RETURNING id`).Scan(&bareID); err != nil {
		t.Fatalf("seeding the bare source: %v", err)
	}
	if _, err := d.ExecContext(ctx, `
INSERT INTO item_costs (item_source_id, currency_id, amount)
VALUES ($1, (SELECT id FROM currencies ORDER BY id LIMIT 1), 40)`, bareID); err != nil {
		t.Fatalf("seeding the cost: %v", err)
	}

	sources, err := q.ListItemSources(ctx, 900)
	if err != nil {
		t.Fatalf("ListItemSources: %v", err)
	}
	if len(sources) != 3 {
		t.Fatalf("ListItemSources returned %d rows, want 3 — an item is many-to-many with places", len(sources))
	}
	placed := 0
	for _, s := range sources {
		if s.PlaceID != nil {
			placed++
			if s.PlaceName == nil {
				t.Errorf("source %d has a place_id but no place name — the LEFT JOIN is not resolving", s.ID)
			}
		}
	}
	if placed != 2 {
		t.Errorf("%d of 3 sources resolved a place, want 2", placed)
	}
	for _, s := range sources {
		if s.ID != bareID {
			continue
		}
		if s.PlaceID != nil || s.BossID != nil || s.VendorID != nil || s.RegionID != nil {
			t.Errorf("the bare source resolved something it has none of: %+v", s)
		}
		if s.Confidence == "" || s.SourceNote == "" {
			t.Error("a source row came back without its provenance — sourcing-standards § 3")
		}
	}

	ids := make([]int64, 0, len(sources))
	for _, s := range sources {
		ids = append(ids, s.ID)
	}
	costs, err := q.ListItemCosts(ctx, ids)
	if err != nil {
		t.Fatalf("ListItemCosts: %v", err)
	}
	if len(costs) != 1 {
		t.Fatalf("ListItemCosts returned %d rows, want 1 — only one source is priced", len(costs))
	}
	if got := numText(t, costs[0].Amount); got != "40.00" {
		t.Errorf("cost amount = %s, want 40.00", got)
	}
	if costs[0].ItemSourceID != bareID {
		t.Errorf("the cost hung off source %d, want %d", costs[0].ItemSourceID, bareID)
	}
}

// The two reverse lookups the epic is built on: a place page ("what drops here") and a currency
// page ("what does this token buy"). Both are DISTINCT over a many-to-many, which is exactly the
// kind of query that silently returns an item once per source row.
func TestTheReverseLookupsReturnEachItemOnce(t *testing.T) {
	q, d := queriesOn(t)
	ctx := context.Background()

	var placeSlug, currencySlug string
	if err := d.QueryRowContext(ctx, `SELECT slug FROM places ORDER BY id LIMIT 1`).Scan(&placeSlug); err != nil {
		t.Fatal(err)
	}
	if err := d.QueryRowContext(ctx, `SELECT slug FROM currencies ORDER BY id LIMIT 1`).Scan(&currencySlug); err != nil {
		t.Fatal(err)
	}

	// Two source rows in the SAME place for one item — the shape that produces duplicates.
	if _, err := d.ExecContext(ctx, `
INSERT INTO item_sources (item_id, place_id, confidence_id, source_note)
SELECT 900, (SELECT id FROM places WHERE slug = $1),
       (SELECT id FROM confidence_levels WHERE slug='unconfirmed'), 'fixture'
FROM generate_series(1,2)`, placeSlug); err != nil {
		t.Fatalf("seeding duplicate-place sources: %v", err)
	}
	if _, err := d.ExecContext(ctx, `
INSERT INTO item_costs (item_source_id, currency_id, amount)
SELECT s.id, (SELECT id FROM currencies WHERE slug = $1), 40 FROM item_sources s WHERE s.item_id = 900`,
		currencySlug); err != nil {
		t.Fatalf("seeding costs: %v", err)
	}

	byPlace, err := q.ListItemsByPlace(ctx, placeSlug)
	if err != nil {
		t.Fatalf("ListItemsByPlace: %v", err)
	}
	if len(byPlace) != 1 || byPlace[0].Name != "Test Blade Alpha" {
		t.Errorf("ListItemsByPlace returned %d rows, want the one item once (two source rows, one item)", len(byPlace))
	}

	byCurrency, err := q.ListItemsByCurrency(ctx, currencySlug)
	if err != nil {
		t.Fatalf("ListItemsByCurrency: %v", err)
	}
	if len(byCurrency) != 1 || byCurrency[0].Name != "Test Blade Alpha" {
		t.Errorf("ListItemsByCurrency returned %d rows, want the one item once", len(byCurrency))
	}
}

// A set with no class is the common case (a set's armour weight is not a class ceiling — AOC-009),
// and piece_count must count the pieces rather than the sources.
func TestListSetsCountsPiecesAndToleratesNoClass(t *testing.T) {
	q, d := queriesOn(t)
	ctx := context.Background()

	if _, err := d.ExecContext(ctx, `
INSERT INTO sets (slug, name, declared_piece_count, confidence_id, source_note)
VALUES ('verify-fixture-set', 'Verify Fixture Set', 7,
        (SELECT id FROM confidence_levels WHERE slug='unconfirmed'), 'fixture')`); err != nil {
		t.Fatalf("seeding the set: %v", err)
	}
	if _, err := d.ExecContext(ctx, `
UPDATE items SET set_id = (SELECT id FROM sets WHERE slug='verify-fixture-set')
WHERE item_id IN (900, 901, 903)`); err != nil {
		t.Fatalf("attaching pieces: %v", err)
	}

	sets, err := q.ListSets(ctx)
	if err != nil {
		t.Fatalf("ListSets: %v", err)
	}
	if len(sets) != 1 {
		t.Fatalf("ListSets returned %d rows, want 1", len(sets))
	}
	s := sets[0]
	// pieces_held is what we actually hold; declared_piece_count is what the SET says it contains.
	// Conflating them renders a 5-of-7 set as though it were a 5-piece set (AOC-010 verify round 1).
	if s.PiecesHeld != 3 {
		t.Errorf("pieces_held = %d, want 3", s.PiecesHeld)
	}
	if s.DeclaredPieceCount == nil || *s.DeclaredPieceCount != 7 {
		got := "NULL"
		if s.DeclaredPieceCount != nil {
			got = fmt.Sprint(*s.DeclaredPieceCount)
		}
		t.Errorf("declared_piece_count = %s, want 7 — an incomplete set must not report its own size as 3", got)
	}
	if s.Class != nil {
		t.Errorf("the set reports class %q — a set with no class must read as NULL, not be dropped", *s.Class)
	}
	if s.SetArmourWeight != nil {
		t.Errorf("the set reports an armour weight it was never given: %q", *s.SetArmourWeight)
	}
}

// Every open question stays reachable in one read, across all three tables that can hold one.
// "What do we still need to ask Pierre" is a query here, not somebody's memory.
func TestListOpenItemQuestionsSeesAllThreeTables(t *testing.T) {
	q, d := queriesOn(t)
	ctx := context.Background()

	if _, err := d.ExecContext(ctx, `
UPDATE items SET open_question = 'fixture question' WHERE item_id = 900`); err != nil {
		t.Fatal(err)
	}
	if _, err := d.ExecContext(ctx, `
INSERT INTO item_sources (item_id, confidence_id, source_note, open_question)
VALUES (901, (SELECT id FROM confidence_levels WHERE slug='unconfirmed'), 'fixture', 'fixture question')`); err != nil {
		t.Fatal(err)
	}
	if _, err := d.ExecContext(ctx, `
INSERT INTO sets (slug, name, confidence_id, source_note, open_question)
VALUES ('verify-open-set', 'Verify Open Set',
        (SELECT id FROM confidence_levels WHERE slug='unconfirmed'), 'fixture', 'fixture question')`); err != nil {
		t.Fatal(err)
	}

	rows, err := q.ListOpenItemQuestions(ctx)
	if err != nil {
		t.Fatalf("ListOpenItemQuestions: %v", err)
	}
	kinds := map[string]int{}
	for _, r := range rows {
		kinds[r.Kind]++
	}
	for _, k := range []string{"item", "item_source", "set"} {
		if kinds[k] != 1 {
			t.Errorf("open questions of kind %q = %d, want 1 — a whole table is invisible", k, kinds[k])
		}
	}
}

// ⭐ AOC-010 verify round 2. Making items.item_type_id nullable (round 1, finding B) is only half
// the fix: four queries joined item_types with a plain JOIN, and a plain JOIN on a now-nullable
// column drops the row instead of showing it with an empty field. Round 2 changed all four to
// LEFT JOIN — and nothing tested that, so reverting any one of them would have made item 4532
// 'Mini-Pet: The Devourer' vanish from the Armory, from its own page, and from every place and
// currency listing, with every test still green. That is the round-1 failure shape exactly.
func TestTheItemWithNoTypeIsStillReachableEverywhere(t *testing.T) {
	q, d := queriesOn(t)
	ctx := context.Background()

	var placeSlug, currencySlug string
	if err := d.QueryRowContext(ctx, `SELECT slug FROM places ORDER BY id LIMIT 1`).Scan(&placeSlug); err != nil {
		t.Fatal(err)
	}
	if err := d.QueryRowContext(ctx, `SELECT slug FROM currencies ORDER BY id LIMIT 1`).Scan(&currencySlug); err != nil {
		t.Fatal(err)
	}

	// The shape of item 4532: a rarity, no item_type at all.
	if _, err := d.ExecContext(ctx, `
INSERT INTO items (item_id, slug, name, rarity_id, item_type_id, confidence_id, source_note)
VALUES (910, 'verify-typeless-item', 'Verify Typeless Item',
        (SELECT id FROM rarities ORDER BY id LIMIT 1), NULL,
        (SELECT id FROM confidence_levels WHERE slug='unconfirmed'),
        'fixture — the item 4532 shape, not a game fact')`); err != nil {
		t.Fatalf("seeding the typeless item: %v", err)
	}
	var srcID int64
	if err := d.QueryRowContext(ctx, `
INSERT INTO item_sources (item_id, place_id, confidence_id, source_note)
VALUES (910, (SELECT id FROM places WHERE slug = $1),
        (SELECT id FROM confidence_levels WHERE slug='unconfirmed'), 'fixture')
RETURNING id`, placeSlug).Scan(&srcID); err != nil {
		t.Fatalf("seeding its source: %v", err)
	}
	if _, err := d.ExecContext(ctx, `
INSERT INTO item_costs (item_source_id, currency_id, amount)
VALUES ($1, (SELECT id FROM currencies WHERE slug = $2), 40)`, srcID, currencySlug); err != nil {
		t.Fatalf("seeding its cost: %v", err)
	}

	list, err := q.ListItems(ctx, sqlcgen.ListItemsParams{PageSize: 50})
	if err != nil {
		t.Fatalf("ListItems: %v", err)
	}
	var inList *sqlcgen.ListItemsRow
	for i := range list {
		if list[i].Name == "Verify Typeless Item" {
			inList = &list[i]
		}
	}
	if inList == nil {
		t.Fatalf("the typeless item is missing from ListItems (%v) — item_types is joined, not LEFT joined", names(list))
	}
	if inList.ItemType != nil {
		t.Errorf("item_type = %q, want NULL — an empty field is the honest answer", *inList.ItemType)
	}
	if inList.TotalCount != 6 {
		t.Errorf("total_count = %d, want 6 — the typeless item is not being counted either", inList.TotalCount)
	}

	item, err := q.GetItem(ctx, 910)
	if err != nil {
		t.Fatalf("GetItem on the typeless item: %v — its own page 404s", err)
	}
	if item.ItemType != nil {
		t.Errorf("GetItem item_type = %q, want NULL", *item.ItemType)
	}

	byPlace, err := q.ListItemsByPlace(ctx, placeSlug)
	if err != nil {
		t.Fatalf("ListItemsByPlace: %v", err)
	}
	if len(byPlace) != 1 || byPlace[0].Name != "Verify Typeless Item" {
		t.Errorf("ListItemsByPlace = %d rows, want the typeless item — it was dropped from \"what drops here\"", len(byPlace))
	}

	byCurrency, err := q.ListItemsByCurrency(ctx, currencySlug)
	if err != nil {
		t.Fatalf("ListItemsByCurrency: %v", err)
	}
	if len(byCurrency) != 1 || byCurrency[0].Name != "Verify Typeless Item" {
		t.Errorf("ListItemsByCurrency = %d rows, want the typeless item", len(byCurrency))
	}
}

// The other half of round 1's finding A: vendor_id now points at `vendors`, so ListItemSources has
// to resolve a vendor name out of that table. `vendors` ships empty (AOC-011 fills it), so without
// this nothing ever executes the join.
func TestASourceResolvesItsVendorFromTheVendorsTable(t *testing.T) {
	q, d := queriesOn(t)
	ctx := context.Background()

	if _, err := d.ExecContext(ctx, `
INSERT INTO vendors (slug, name, confidence_id, source_note)
VALUES ('verify-fixture-vendor', 'Verify Fixture Vendor',
        (SELECT id FROM confidence_levels WHERE slug='unconfirmed'), 'fixture')`); err != nil {
		t.Fatalf("seeding the vendor: %v", err)
	}
	if _, err := d.ExecContext(ctx, `
INSERT INTO item_sources (item_id, vendor_id, confidence_id, source_note)
VALUES (900, (SELECT id FROM vendors WHERE slug='verify-fixture-vendor'),
        (SELECT id FROM confidence_levels WHERE slug='unconfirmed'), 'fixture')`); err != nil {
		t.Fatalf("seeding the vendor source: %v", err)
	}

	sources, err := q.ListItemSources(ctx, 900)
	if err != nil {
		t.Fatalf("ListItemSources: %v", err)
	}
	if len(sources) != 1 {
		t.Fatalf("ListItemSources returned %d rows, want 1", len(sources))
	}
	s := sources[0]
	if s.VendorName == nil || *s.VendorName != "Verify Fixture Vendor" {
		t.Errorf("vendor_name = %v, want the vendor's name — the join is not resolving", s.VendorName)
	}
	if s.PlaceName != nil {
		t.Errorf("the vendor source also resolved a place (%q) — a vendor is not a place", *s.PlaceName)
	}
	if s.BossID != nil {
		t.Error("the vendor source resolved a boss")
	}

	// And a vendor is still not allowed to be a boss as well.
	_, err = d.ExecContext(ctx, `
INSERT INTO item_sources (item_id, boss_id, vendor_id, confidence_id, source_note)
SELECT 900, (SELECT id FROM bosses LIMIT 1), (SELECT id FROM vendors LIMIT 1),
       (SELECT id FROM confidence_levels WHERE slug='unconfirmed'), 'fixture'`)
	if err == nil {
		t.Error("a source naming both a boss and a vendor was accepted; item_sources_boss_and_vendor_not_both is not doing its job")
	}
}
