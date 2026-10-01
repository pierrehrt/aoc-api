package templates

import (
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
	// Sources is how many distinct lines the section shows, for its count.
	Sources int
}

// TypeChip is the item type's name when it says something the slot does not: "Crossbow" beside
// "Main Hand", but not "Hands" beside "Hands" — typeRepeatsSlot, the rule the list uses too.
func (d ItemData) TypeChip() string {
	t := d.Item.Display.ItemType
	if t == nil || typeRepeatsSlot(t.Slug, t.Name, d.Item.Display.Slots) {
		return ""
	}
	return t.Name
}

// typeRepeatsSlot is true when an item type only repeats one of the item's slots ("Hands" type,
// "Hands" slot). ONE predicate for the list row and the item page, so the two cannot disagree about
// when the type is worth printing. Either side may be compared by slug or by name.
func typeRepeatsSlot(slug, name string, slots []items.Term) bool {
	for _, s := range slots {
		for _, v := range []string{slug, name} {
			if v != "" && (strings.EqualFold(s.Slug, v) || strings.EqualFold(s.Name, v)) {
				return true
			}
		}
	}
	return false
}

// SourceGroup is the sources of one acquisition type.
type SourceGroup struct {
	Name string // the type's name from its row; "" when a source names no type (17 in the corpus)
	Rows []SourceRow
	// HasCost and HasTier are true when any row in the group carries one. The columns follow the
	// data: no boss drop carries a cost (0 of 3,436, measured 2026-09-29) and 228 of 229 quest
	// sources no tier, so those groups get no column of dashes — without the page ever asking which
	// group is which (design 1c, open question 7).
	HasCost bool
	HasTier bool
}

// SourceRow is one source as the page prints it: what it is, where, which tier, what it costs.
type SourceRow struct {
	Main    string // vendor · quest as listed · place — boss · from container, whichever are present
	Context string // map, region
	Tier    string
	Cost    string   // "9 Simple Relic I + 2 Gold" — items.Price, the list's own format
	Phone   string   // tier · cost, for the phone line under Main, where those columns are hidden
	Flags   []string // "raid", "Unchained" — the row's own flags, which its names may not say
}

// NewItemData groups an item's sources for the page. Pure: every word it prints is a value from
// the item's own rows.
//
// ⭐ A line identical to one already in its group is shown once. 42 items carry source rows that
// match in every column but the id (measured 2026-09-30: barrier-of-clouded-frost lists one
// vendor line three times); printed as-is they read as a broken page. /v1 keeps every row.
func NewItemData(d items.Detail) ItemData {
	out := ItemData{Item: d}
	index := map[string]int{}
	seen := map[string]bool{}
	for _, s := range d.Sources {
		name := deref(s.AcquisitionTypeName)
		row := sourceRow(s)
		key := name + "\x00" + row.Main + "\x00" + row.Context + "\x00" + row.Tier + "\x00" + row.Cost + "\x00" + strings.Join(row.Flags, ",")
		if seen[key] {
			continue
		}
		seen[key] = true
		i, ok := index[name]
		if !ok {
			i = len(out.Groups)
			index[name] = i
			out.Groups = append(out.Groups, SourceGroup{Name: name})
		}
		out.Groups[i].Rows = append(out.Groups[i].Rows, row)
		out.Groups[i].HasCost = out.Groups[i].HasCost || row.Cost != ""
		out.Groups[i].HasTier = out.Groups[i].HasTier || row.Tier != ""
		out.Sources++
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
		Cost:    items.Price(s.Costs),
	}
	if s.IsRaid {
		row.Flags = append(row.Flags, "raid")
	}
	// Unchained is the row's flag (its own or its place's — AOC-039), and most Unchained places say
	// so in their name. Not all: 109 sources at "Otherworldly Junction" do not (measured
	// 2026-09-30), so the flag is printed whenever the name does not already carry it.
	if s.Unchained && !strings.Contains(strings.ToLower(deref(s.Place)), "unchained") {
		row.Flags = append(row.Flags, "Unchained")
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

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

// itemProbe renders every branch of item.html at boot: stats and spell effects, the base lines, a
// set, a typed group with a raid and Unchained row, a group-less row with nothing in it, an untyped
// vendor with a cost. Obviously fake, never a real item (CLAUDE.md STEP ZERO).
func itemProbe() ItemData {
	s := func(v string) *string { return &v }
	lvl := int32(80)
	armor := int32(100)
	d := items.Detail{
		ID: 1, Slug: "test-item-alpha", Name: "Test Item Alpha", Rarity: "epic",
		ItemLevel: &lvl, RequiresLevel: &lvl, Armor: &armor, Critigation: &armor, DamageRange: s("1-2"),
		Set: s("Test Set Omega"), TooltipImage: s("https://img.aoc-codex.app/armory/test_item_alpha.jpg"),
		Stats:        []items.StatLine{{Stat: "Test Strength", Value: "10.00", Sign: 1, Unit: "flat", DamageType: s("Test")}},
		SpellEffects: []items.StatLine{{Stat: "Test Drain", Value: "8.00", Sign: -1, Unit: "percent"}},
		SetPieces:    []items.SetPiece{{Slug: "test-item-alpha", Name: "Test Item Alpha", RarityColourToken: "rarity-epic"}, {Slug: "test-item-beta", Name: "Test Item Beta"}},
		Sources: []items.SourceRef{
			{AcquisitionTypeName: s("test-type"), Place: s("Test Place"), Boss: s("Test Boss"), Region: s("Test Region"), TierName: s("Test Tier"), IsRaid: true, Unchained: true},
			{AcquisitionTypeName: s("test-type")}, // nothing recorded but its type
			{Vendor: s("Test Vendor"), Costs: []items.CostRef{{CurrencyName: "Test Token", Amount: "3.00"}}},
		},
		Display: items.DetailDisplay{
			Rarity:   items.Term{Slug: "epic", Name: "Epic", ColourToken: "rarity-epic"},
			ItemType: &items.Term{Slug: "test-type", Name: "Test Type"}, ArmourWeight: &items.Term{Slug: "light", Name: "Light"},
			Binding: &items.Term{Slug: "test-binding", Name: "Test Binding"},
			Slots:   []items.Term{{Slug: "head", Name: "Head"}}, Classes: []items.Term{{Slug: "test-class", Name: "Test Class", ShortName: "TC"}},
			DPS: s("1.5"),
		},
	}
	return NewItemData(d)
}
