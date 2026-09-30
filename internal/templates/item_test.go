package templates_test

import (
	"testing"

	"github.com/pierrehrt/aoc-api/internal/items"
	"github.com/pierrehrt/aoc-api/internal/templates"
)

// AOC-048: the item page's words, all built from the item's own values. Fixtures are obviously fake.

func TestStatTextReadsLikeATooltip(t *testing.T) {
	s := func(v string) *string { return &v }
	for _, tc := range []struct {
		in   items.StatLine
		want string
	}{
		{items.StatLine{Stat: "Test Strength", Value: "40.00", Sign: 1, Unit: "flat"}, "+40 Test Strength"},
		{items.StatLine{Stat: "Test Rating", Value: "258.00", Sign: 1, Unit: "flat", DamageType: s("Test Element")}, "+258 Test Rating (Test Element)"},
		{items.StatLine{Stat: "Test Drain", Value: "8.00", Sign: -1, Unit: "percent"}, "-8% Test Drain"},
		{items.StatLine{Stat: "Test Regen", Value: "4.50", Sign: 1, Unit: "flat"}, "+4.5 Test Regen"},
		{items.StatLine{Stat: "Test Plain", Value: "3.00", Unit: "flat"}, "3 Test Plain"}, // no sign recorded: none printed
	} {
		if got := templates.StatText(tc.in); got != tc.want {
			t.Errorf("StatText(%+v) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestCostTextIsTheListsPriceFormat(t *testing.T) {
	got := templates.CostText([]items.CostRef{{CurrencyName: "Test Relic", Amount: "9.00"}, {CurrencyName: "Test Gold", Amount: "2.50"}})
	if want := "9 Test Relic + 2.5 Test Gold"; got != want {
		t.Errorf("CostText = %q, want %q", got, want)
	}
	if got := templates.CostText(nil); got != "" {
		t.Errorf("no costs gave %q, want nothing", got)
	}
}

// ⭐ Groups follow the rows' own types, in the order the sources first name them, and a group's
// cost column exists only when one of its rows has a cost — no literal "vendor" anywhere.
func TestSourcesGroupByTheirOwnTypeAndCostsFollowTheData(t *testing.T) {
	s := func(v string) *string { return &v }
	d := items.Detail{Sources: []items.SourceRef{
		{AcquisitionTypeName: s("test-b"), Place: s("Test Place"), Boss: s("Test Boss"), Map: s("Test Map"), Region: s("Test Region"), TierName: s("Test Tier")},
		{AcquisitionTypeName: s("test-a"), Vendor: s("Test Vendor"), Costs: []items.CostRef{{CurrencyName: "Test Token", Amount: "3.00"}}},
		{AcquisitionTypeName: s("test-b"), Container: s("Test Cache")},
		{}, // names no type and nothing else
	}}
	got := templates.NewItemData(d)
	if got.Sources != 4 || len(got.Groups) != 3 {
		t.Fatalf("%d sources in %d groups, want 4 in 3", got.Sources, len(got.Groups))
	}
	for i, want := range []struct {
		name    string
		rows    int
		hasCost bool
	}{{"test-b", 2, false}, {"test-a", 1, true}, {"", 1, false}} {
		g := got.Groups[i]
		if g.Name != want.name || len(g.Rows) != want.rows || g.HasCost != want.hasCost {
			t.Errorf("group %d = {%q, %d rows, cost %v}, want {%q, %d, %v}", i, g.Name, len(g.Rows), g.HasCost, want.name, want.rows, want.hasCost)
		}
	}
	first := got.Groups[0].Rows[0]
	if first.Main != "Test Place — Test Boss" || first.Context != "Test Map, Test Region" || first.Tier != "Test Tier" || first.Phone != "Test Tier" {
		t.Errorf("drop row = %+v", first)
	}
	if r := got.Groups[0].Rows[1]; r.Main != "from Test Cache" {
		t.Errorf("container row main = %q", r.Main)
	}
	if r := got.Groups[1].Rows[0]; r.Main != "Test Vendor" || r.Cost != "3 Test Token" || r.Phone != "3 Test Token" {
		t.Errorf("vendor row = %+v", r)
	}
	if r := got.Groups[2].Rows[0]; r.Main != "" || r.Context != "" {
		t.Errorf("an empty source rendered %+v; the template shows a dash", r)
	}
}

func TestSetCountNeverPassesAPartialSetOffAsItsSize(t *testing.T) {
	four, one := int32(4), int32(1)
	piece := items.SetPiece{Slug: "x", Name: "X"}
	for _, tc := range []struct {
		held     int
		declared *int32
		want     string
	}{
		{2, &four, "2 of 4 pieces"},
		{4, &four, "4 pieces"},
		{1, &one, "1 piece"},
		{3, nil, "3 pieces"}, // nothing declared: what is held is all there is to say
	} {
		d := items.Detail{Display: items.DetailDisplay{SetDeclaredPieces: tc.declared}}
		for i := 0; i < tc.held; i++ {
			d.SetPieces = append(d.SetPieces, piece)
		}
		if got := templates.NewItemData(d).SetCount; got != tc.want {
			t.Errorf("held %d, declared %v: %q, want %q", tc.held, tc.declared, got, tc.want)
		}
	}
}

func TestTheTypeChipOnlySaysWhatTheSlotDoesNot(t *testing.T) {
	for _, tc := range []struct {
		typ   string
		slots []items.Term
		want  string
	}{
		{"Test Bow", []items.Term{{Slug: "main-hand", Name: "Main Hand"}}, "Test Bow"},
		{"Hands", []items.Term{{Slug: "hands", Name: "Hands"}}, ""},
		{"Test Charm", nil, "Test Charm"},
	} {
		d := items.Detail{Display: items.DetailDisplay{ItemType: &items.Term{Slug: "t", Name: tc.typ}, Slots: tc.slots}}
		if got := templates.NewItemData(d).TypeChip(); got != tc.want {
			t.Errorf("type %q with %v: chip %q, want %q", tc.typ, tc.slots, got, tc.want)
		}
	}
}
