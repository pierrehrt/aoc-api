package templates

import (
	"fmt"
	"strings"

	"github.com/pierrehrt/aoc-api/internal/items"
)

// ItemData is everything the item page reads (AOC-048), beside its template for the same reason as
// ArmoryData: the startup probe renders the page with a value of exactly this shape. The handler
// builds it with NewItemData from items.Service.Get — the SAME call /v1/items/{slug} makes.
type ItemData struct {
	Item items.Detail

	// Groups is the item's sources, one group per acquisition type, in the order the sources
	// first name them. The group IS the row's own type (a taxonomy row), never a literal the page
	// knows — so a fourth acquisition type is a fourth group with no change here.
	Groups []SourceGroup
	// Sources is how many there are in all, for the section's count.
	Sources int
	// SetCount is "5 of 7 pieces" when the data holds fewer than the set declares, "7 pieces" when
	// it holds them all — so a partial set is never shown as though that were its size.
	SetCount string
}

// TypeChip is the item type's name when it says something the slot does not: "Crossbow" beside
// "Main Hand", but not "Hands" beside "Hands" — the list's typeIsSlot rule, on the page.
func (d ItemData) TypeChip() string {
	t := d.Item.Display.ItemType
	if t == nil {
		return ""
	}
	for _, s := range d.Item.Display.Slots {
		if strings.EqualFold(s.Slug, t.Slug) || strings.EqualFold(s.Name, t.Name) {
			return ""
		}
	}
	return t.Name
}

// SourceGroup is the sources of one acquisition type.
type SourceGroup struct {
	Name string // the type's name from its row; "" when a source names no type (17 in the corpus)
	Rows []SourceRow
	// HasCost is true when any row in the group carries a cost. The cost column follows the data:
	// no boss drop carries one (0 of 3,436, measured 2026-09-29), so drops get no empty column
	// without the page ever asking which group is the drops (design 1c, open question 7).
	HasCost bool
}

// SourceRow is one source as the page prints it: what it is, where, which tier, what it costs.
type SourceRow struct {
	Main    string // vendor · quest as listed · place — boss · from container, whichever are present
	Context string // map, region
	Tier    string
	Cost    string // "9 Simple Relic I + 2 Gold", the list's price format
	Phone   string // tier · cost, for the phone line under Main, where those columns are hidden
}

// NewItemData groups an item's sources for the page. Pure: every word it prints is a value from
// the item's own rows.
func NewItemData(d items.Detail) ItemData {
	out := ItemData{Item: d, Sources: len(d.Sources), SetCount: setCount(len(d.SetPieces), d.Display.SetDeclaredPieces)}
	index := map[string]int{}
	for _, s := range d.Sources {
		name := deref(s.AcquisitionTypeName)
		i, ok := index[name]
		if !ok {
			i = len(out.Groups)
			index[name] = i
			out.Groups = append(out.Groups, SourceGroup{Name: name})
		}
		row := sourceRow(s)
		out.Groups[i].Rows = append(out.Groups[i].Rows, row)
		if row.Cost != "" {
			out.Groups[i].HasCost = true
		}
	}
	return out
}

func sourceRow(s items.SourceRef) SourceRow {
	var main []string
	for _, p := range []*string{s.Vendor, s.QuestLabel} {
		if v := deref(p); v != "" {
			main = append(main, v)
		}
	}
	switch place, boss := deref(s.Place), deref(s.Boss); {
	case place != "" && boss != "":
		main = append(main, place+" — "+boss)
	case place != "":
		main = append(main, place)
	case boss != "":
		main = append(main, boss)
	}
	if c := deref(s.Container); c != "" {
		main = append(main, "from "+c)
	}
	var context []string
	for _, p := range []*string{s.Map, s.Region} {
		if v := deref(p); v != "" {
			context = append(context, v)
		}
	}
	row := SourceRow{
		Main:    strings.Join(main, " · "),
		Context: strings.Join(context, ", "),
		Tier:    deref(s.TierName),
		Cost:    CostText(s.Costs),
	}
	var phone []string
	for _, v := range []string{row.Tier, row.Cost} {
		if v != "" {
			phone = append(phone, v)
		}
	}
	row.Phone = strings.Join(phone, " · ")
	return row
}

func setCount(held int, declared *int32) string {
	unit := func(n int) string {
		if n == 1 {
			return "piece"
		}
		return "pieces"
	}
	if declared != nil && int(*declared) != held {
		return fmt.Sprintf("%d of %d %s", held, *declared, unit(int(*declared)))
	}
	return fmt.Sprintf("%d %s", held, unit(held))
}

// StatText renders a stat line the way the tooltip prints it: "+74 Magic Damage (Fire)",
// "+258 Combat Rating (Crossbow)", "-8% Sprinting Stamina Drain". PvP stats need nothing extra —
// "PvP" is part of their name in the data.
func StatText(s items.StatLine) string {
	var b strings.Builder
	switch {
	case s.Sign > 0:
		b.WriteString("+")
	case s.Sign < 0:
		b.WriteString("-")
	}
	b.WriteString(items.TrimNumber(s.Value))
	if s.Unit == "percent" {
		b.WriteString("%")
	}
	b.WriteString(" ")
	b.WriteString(s.Stat)
	if dt := deref(s.DamageType); dt != "" {
		b.WriteString(" (" + dt + ")")
	}
	return b.String()
}

// CostText is a source's costs in the list's price format: "9 Simple Relic I + 2 Gold".
func CostText(cs []items.CostRef) string {
	parts := make([]string, 0, len(cs))
	for _, c := range cs {
		parts = append(parts, items.TrimNumber(c.Amount)+" "+c.CurrencyName)
	}
	return strings.Join(parts, " + ")
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

// itemProbe renders every branch of item.html at boot: stats and spell effects, the base lines, a
// set with a declared count that differs from what is held, a typed and an untyped source group,
// a cost. Obviously fake, never a real item (CLAUDE.md STEP ZERO).
func itemProbe() ItemData {
	s := func(v string) *string { return &v }
	lvl, n := int32(80), int32(3)
	armor := int32(100)
	d := items.Detail{
		ID: 1, Slug: "test-item-alpha", Name: "Test Item Alpha", Rarity: "epic",
		ItemLevel: &lvl, RequiresLevel: &lvl, Armor: &armor, Critigation: &armor, DamageRange: s("1-2"),
		Set: s("Test Set Omega"), TooltipImage: s("https://img.aoc-codex.app/armory/test_item_alpha.jpg"),
		Stats:        []items.StatLine{{Stat: "Test Strength", Value: "10.00", Sign: 1, Unit: "flat", DamageType: s("Test")}},
		SpellEffects: []items.StatLine{{Stat: "Test Drain", Value: "8.00", Sign: -1, Unit: "percent"}},
		SetPieces:    []items.SetPiece{{Slug: "test-item-alpha", Name: "Test Item Alpha", RarityColourToken: "rarity-epic"}, {Slug: "test-item-beta", Name: "Test Item Beta"}},
		Sources: []items.SourceRef{
			{AcquisitionTypeName: s("test-type"), Place: s("Test Place"), Boss: s("Test Boss"), Region: s("Test Region"), TierName: s("Test Tier")},
			{Vendor: s("Test Vendor"), Costs: []items.CostRef{{CurrencyName: "Test Token", Amount: "3.00"}}},
		},
		Display: items.DetailDisplay{
			Rarity:   items.Term{Slug: "epic", Name: "Epic", ColourToken: "rarity-epic"},
			ItemType: &items.Term{Slug: "test-type", Name: "Test Type"}, ArmourWeight: &items.Term{Slug: "light", Name: "Light"},
			Binding: &items.Term{Slug: "test-binding", Name: "Test Binding"},
			Slots:   []items.Term{{Slug: "head", Name: "Head"}}, Classes: []items.Term{{Slug: "test-class", Name: "Test Class", ShortName: "TC"}},
			DPS: s("1.5"), SetDeclaredPieces: &n,
		},
	}
	return NewItemData(d)
}
