package pages_test

import (
	"context"

	"github.com/pierrehrt/aoc-api/internal/db/sqlcgen"
)

// The gear builder's queries over the same fake corpus (AOC-051), so one fake stands behind both
// services as one database does in production. Obviously fake slots and class (CLAUDE.md STEP ZERO);
// an item's slots are the rows' own (fakeItems.slots), so the list's "+" and the builder agree.

var fakeGearSlots = []sqlcgen.EquipLocation{
	{ID: 1, Slug: "test-head", Name: "Test Head"},
	{ID: 2, Slug: "test-main", Name: "Test Main"},
	{ID: 3, Slug: "test-off", Name: "Test Off"},
}

func (f *fakeItems) ListBuildSlots(context.Context) ([]sqlcgen.EquipLocation, error) {
	return fakeGearSlots, nil
}

func (f *fakeItems) ListBuildHands(context.Context) ([]int32, error) { return []int32{2, 3}, nil }

func (f *fakeItems) ListBuildClasses(context.Context) ([]sqlcgen.ListBuildClassesRow, error) {
	return []sqlcgen.ListBuildClassesRow{
		{Slug: "test-class", Name: "Test Class", ArchetypeName: "Test Archetype"},
		{Slug: "test-other", Name: "Test Other", ArchetypeName: "Test Archetype"},
	}, nil
}

func (f *fakeItems) ListBuildArmourWeights(context.Context) ([]sqlcgen.ListBuildArmourWeightsRow, error) {
	return nil, nil
}

func (f *fakeItems) ListBuildItems(_ context.Context, ids []int32) ([]sqlcgen.ListBuildItemsRow, error) {
	var out []sqlcgen.ListBuildItemsRow
	for _, r := range f.rows {
		for _, id := range ids {
			if r.ItemID == id {
				out = append(out, sqlcgen.ListBuildItemsRow{ItemID: r.ItemID, Slug: r.Slug, Name: r.Name, RarityColourToken: r.RarityColourToken, Armor: r.Armor})
				break
			}
		}
	}
	return out, nil
}

func (f *fakeItems) ListBuildItemSlots(_ context.Context, ids []int32) ([]sqlcgen.ListBuildItemSlotsRow, error) {
	var out []sqlcgen.ListBuildItemSlotsRow
	for _, s := range f.slots {
		for _, id := range ids {
			if s.ItemID == id {
				out = append(out, sqlcgen.ListBuildItemSlotsRow{ItemID: s.ItemID, Slug: s.Slug})
			}
		}
	}
	return out, nil
}

func (f *fakeItems) ListBuildItemClasses(_ context.Context, ids []int32) ([]sqlcgen.ListBuildItemClassesRow, error) {
	var out []sqlcgen.ListBuildItemClassesRow
	for _, id := range ids {
		for _, c := range f.gearClasses[id] {
			out = append(out, sqlcgen.ListBuildItemClassesRow{ItemID: id, Slug: c})
		}
	}
	return out, nil
}

func (f *fakeItems) ListBuildItemStats(_ context.Context, ids []int32) ([]sqlcgen.ListBuildItemStatsRow, error) {
	var out []sqlcgen.ListBuildItemStatsRow
	for _, id := range ids {
		out = append(out, f.gearStats[id]...)
	}
	return out, nil
}
