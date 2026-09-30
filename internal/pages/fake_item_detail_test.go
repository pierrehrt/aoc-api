package pages_test

import (
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/pierrehrt/aoc-api/internal/db/sqlcgen"
)

// The item page's fake corpus (AOC-048): three items, every name obviously fake (CLAUDE.md STEP
// ZERO). Their slugs are the list fake's own, so a row's link lands on a page that exists.
//
//	1 test-item-1  everything: stats, an effect, a set of 2 that declares 4, a drop, a vendor with
//	               two currencies, a quest as listed, and a source that names no type
//	2 test-item-2  item 1's set-mate, and nothing else: no stats, no source, no tooltip — the
//	               honest states
//	3 test-item-3  in no set, sold by a vendor
type fakeDetail struct {
	row     sqlcgen.GetItemRow
	stats   []sqlcgen.ListItemStatsRow
	effects []sqlcgen.ListItemSpellEffectsRow
	slots   []sqlcgen.EquipLocation
	classes []sqlcgen.ListItemClassesRow
	sources []sqlcgen.ListItemSourcesRow
	costs   []sqlcgen.ListItemCostsRow
}

func sp(s string) *string { return &s }
func ip(n int32) *int32   { return &n }

func num(s string) pgtype.Numeric {
	var n pgtype.Numeric
	_ = n.Scan(s)
	return n
}

var setOmega = int32(7)

var fakeSetPieces = []sqlcgen.ListSetPiecesRow{
	{ItemID: 1, Slug: "test-item-1", Name: "Test Item 1", RarityColourToken: sp("rarity-epic")},
	{ItemID: 2, Slug: "test-item-2", Name: "Test Item 2"},
}

var fakeDetails = map[int32]fakeDetail{
	1: {
		row: sqlcgen.GetItemRow{
			ItemID: 1, Slug: "test-item-1", Name: "Test Item 1",
			Rarity: "epic", RarityName: "Epic", RarityColourToken: sp("rarity-epic"),
			ItemType: sp("test-type"), ItemTypeName: sp("Test Type"),
			ArmourWeight: sp("light"), ArmourWeightName: sp("Light"),
			Binding: sp("test-binding"), BindingName: sp("Test Binding"),
			ItemLevel: ip(80), RequiresLevel: ip(78), Armor: ip(234), Critigation: ip(146),
			SetID: &setOmega, SetName: sp("Test Set Omega"), DeclaredPieceCount: ip(4),
			TooltipImage: sp("https://img.aoc-codex.app/armory/test_item_1.jpg"),
			Confidence:   "unconfirmed",
		},
		stats: []sqlcgen.ListItemStatsRow{
			{ItemID: 1, Stat: "Test Strength", Value: num("40.00"), Sign: 1, Unit: "flat"},
			{ItemID: 1, Stat: "Test Rating", Value: num("258.00"), Sign: 1, Unit: "flat", DamageType: sp("Test Element")},
		},
		effects: []sqlcgen.ListItemSpellEffectsRow{{ItemID: 1, Stat: "Test Drain", Value: num("8.00"), Sign: -1, Unit: "percent"}},
		slots:   []sqlcgen.EquipLocation{{ID: 1, Slug: "head", Name: "Head"}},
		classes: []sqlcgen.ListItemClassesRow{{ID: 1, Slug: "test-class", Name: "Test Class", ShortName: sp("TC")}},
		sources: []sqlcgen.ListItemSourcesRow{
			{ID: 101, ItemID: 1, AcquisitionType: sp("drop"), AcquisitionTypeName: sp("drop"),
				PlaceName: sp("Test Place"), BossName: sp("Test Boss"), RegionName: sp("Test Region"),
				Tier: sp("test-tier"), TierName: sp("Test Tier"), Confidence: "unconfirmed"},
			{ID: 102, ItemID: 1, AcquisitionType: sp("vendor"), AcquisitionTypeName: sp("vendor"),
				VendorName: sp("Test Vendor"), RegionName: sp("Test Region"), Confidence: "unconfirmed"},
			{ID: 103, ItemID: 1, AcquisitionType: sp("quest"), AcquisitionTypeName: sp("quest"),
				QuestLabel: sp("Test Giver"), Confidence: "unconfirmed"},
			{ID: 104, ItemID: 1, Confidence: "unconfirmed"},
		},
		costs: []sqlcgen.ListItemCostsRow{
			{ItemSourceID: 102, Currency: "test-token", CurrencyName: "Test Token", Amount: num("3.00")},
			{ItemSourceID: 102, Currency: "test-coin", CurrencyName: "Test Coin", Amount: num("2.50")},
		},
	},
	2: {row: sqlcgen.GetItemRow{ItemID: 2, Slug: "test-item-2", Name: "Test Item 2", Rarity: "mundane", RarityName: "Mundane",
		SetID: &setOmega, SetName: sp("Test Set Omega"), DeclaredPieceCount: ip(4), Confidence: "unconfirmed"}},
	3: {
		row: sqlcgen.GetItemRow{ItemID: 3, Slug: "test-item-3", Name: "Test Item 3", Rarity: "mundane", RarityName: "Mundane", Confidence: "unconfirmed"},
		sources: []sqlcgen.ListItemSourcesRow{{ID: 301, ItemID: 3, AcquisitionType: sp("vendor"), AcquisitionTypeName: sp("vendor"),
			VendorName: sp("Test Vendor"), Confidence: "unconfirmed"}},
	},
}
