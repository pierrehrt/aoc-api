package builds_test

import (
	"context"

	"github.com/pierrehrt/aoc-api/internal/db/sqlcgen"
)

// fakeDB is a builds.Querier over an obviously fake world (CLAUDE.md STEP ZERO): fake slots, fake
// classes, fake items. The slot slugs are not the real ones on purpose — a rule that only works
// for "main-hand" would be a slot named in code, and these tests would fail it.
type fakeDB struct {
	slots   []sqlcgen.EquipLocation
	hands   []int32
	classes []sqlcgen.ListBuildClassesRow
	weights []sqlcgen.ListBuildArmourWeightsRow
	items   map[int32]fakeItem
	calls   map[string]int
}

type fakeItem struct {
	row     sqlcgen.ListBuildItemsRow
	slots   []string
	classes []string
	stats   []sqlcgen.ListBuildItemStatsRow
}

func i32(v int32) *int32   { return &v }
func str(v string) *string { return &v }

// The fake types: 1 a blade, 2 a greatblade (two-handed), 3 a bow (two-handed, allows type 4),
// 4 arrows, 5 a shield.
const (
	typeBlade, typeGreat, typeBow, typeArrows, typeShield = 1, 2, 3, 4, 5
)

func newFake() *fakeDB {
	f := &fakeDB{calls: map[string]int{}, items: map[int32]fakeItem{}}
	for i, s := range []string{"test-head", "test-ring-left", "test-ring-right", "test-main", "test-off"} {
		f.slots = append(f.slots, sqlcgen.EquipLocation{ID: int32(i + 1), Slug: s, Name: "Test " + s[5:]})
	}
	f.hands = []int32{4, 5} // what a one-handed blade fits, as ListBuildHands derives it
	f.classes = []sqlcgen.ListBuildClassesRow{
		{Slug: "test-class-a", Name: "Test Class A", ArchetypeName: "Test Archetype"},
		{Slug: "test-class-b", Name: "Test Class B", ArchetypeName: "Test Archetype", ShortName: str("TCB")},
		{Slug: "test-class-c", Name: "Test Class C", ArchetypeName: "Test Archetype", MaxArmourWeightOrder: i32(20)},
	}
	f.weights = []sqlcgen.ListBuildArmourWeightsRow{{Slug: "test-light", SortOrder: 20}, {Slug: "test-heavy", SortOrder: 40}}
	tok := "rarity-epic"
	add := func(id int32, name string, typ int32, two bool, slots []string, classes []string, stats ...sqlcgen.ListBuildItemStatsRow) *fakeItem {
		it := fakeItem{row: sqlcgen.ListBuildItemsRow{ItemID: id, Slug: "test-item-" + name, Name: "Test " + name, RarityColourToken: &tok, TwoHanded: two}, slots: slots, classes: classes}
		if typ != 0 {
			it.row.ItemTypeID = i32(typ)
		}
		for _, s := range stats {
			s.ItemID = id
			it.stats = append(it.stats, s)
		}
		f.items[id] = it
		return &it
	}
	stat := func(name string, centi int64) sqlcgen.ListBuildItemStatsRow {
		return sqlcgen.ListBuildItemStatsRow{Stat: name, Unit: "flat", Centi: centi}
	}
	add(1, "Helm", 0, false, []string{"test-head"}, nil, stat("Test Might", 1000), stat("Test Grace", -300))
	add(2, "Ring", 0, false, []string{"test-ring-left", "test-ring-right"}, nil, stat("Test Might", 250))
	add(3, "Blade", typeBlade, false, []string{"test-main", "test-off"}, nil, stat("Test Grace", 300))
	add(4, "Greatblade", typeGreat, true, []string{"test-main"}, nil)
	bow := add(5, "Bow", typeBow, true, []string{"test-main"}, nil)
	bow.row.OtherHandTypeID = i32(typeArrows)
	f.items[5] = *bow
	add(6, "Arrows", typeArrows, false, []string{"test-off"}, nil)
	add(7, "Shield", typeShield, false, []string{"test-off"}, nil)
	add(8, "Robe", 0, false, []string{"test-head"}, []string{"test-class-b"})
	add(9, "Mount", 0, false, nil, nil)
	heavy := add(10, "Plate Hat", 0, false, []string{"test-head"}, nil)
	heavy.row.ArmourWeightOrder = i32(40)
	f.items[10] = *heavy
	return f
}

func (f *fakeDB) ListBuildSlots(context.Context) ([]sqlcgen.EquipLocation, error) {
	f.calls["slots"]++
	return f.slots, nil
}
func (f *fakeDB) ListBuildHands(context.Context) ([]int32, error) {
	f.calls["hands"]++
	return f.hands, nil
}
func (f *fakeDB) ListBuildClasses(context.Context) ([]sqlcgen.ListBuildClassesRow, error) {
	f.calls["classes"]++
	return f.classes, nil
}
func (f *fakeDB) ListBuildArmourWeights(context.Context) ([]sqlcgen.ListBuildArmourWeightsRow, error) {
	f.calls["weights"]++
	return f.weights, nil
}
func (f *fakeDB) ListBuildItems(_ context.Context, ids []int32) ([]sqlcgen.ListBuildItemsRow, error) {
	f.calls["items"]++
	var out []sqlcgen.ListBuildItemsRow
	for _, id := range ids {
		if it, ok := f.items[id]; ok {
			out = append(out, it.row)
		}
	}
	return out, nil
}
func (f *fakeDB) ListBuildItemSlots(_ context.Context, ids []int32) ([]sqlcgen.ListBuildItemSlotsRow, error) {
	f.calls["item-slots"]++
	var out []sqlcgen.ListBuildItemSlotsRow
	for _, id := range ids {
		for _, s := range f.items[id].slots {
			out = append(out, sqlcgen.ListBuildItemSlotsRow{ItemID: id, Slug: s})
		}
	}
	return out, nil
}
func (f *fakeDB) ListBuildItemClasses(_ context.Context, ids []int32) ([]sqlcgen.ListBuildItemClassesRow, error) {
	f.calls["item-classes"]++
	var out []sqlcgen.ListBuildItemClassesRow
	for _, id := range ids {
		for _, c := range f.items[id].classes {
			out = append(out, sqlcgen.ListBuildItemClassesRow{ItemID: id, Slug: c})
		}
	}
	return out, nil
}
func (f *fakeDB) ListBuildItemStats(_ context.Context, ids []int32) ([]sqlcgen.ListBuildItemStatsRow, error) {
	f.calls["item-stats"]++
	var out []sqlcgen.ListBuildItemStatsRow
	for _, id := range ids {
		out = append(out, f.items[id].stats...)
	}
	return out, nil
}
