package templates_test

import (
	"testing"

	"github.com/pierrehrt/aoc-api/internal/items"
	"github.com/pierrehrt/aoc-api/internal/templates"
)

// AOC-062: the list row's display rules, each written once in Go. Fixtures are obviously fake; the
// slot, type and weight VALUES look like rows because they must.
func TestTheListRowsDisplayRules(t *testing.T) {
	s := func(v string) *string { return &v }
	lvl := int32(80)
	head := []items.Term{{Slug: "head", Name: "Head"}}
	for _, tc := range []struct {
		what             string
		it               items.ListItem
		slot, typ, phone string
	}{
		{"armour: the weight", items.ListItem{EquipLocations: head, ItemType: s("head"), ItemTypeName: s("Head"), ArmourWeight: &items.Term{Slug: "light", Name: "Light"}, ItemLevel: &lvl},
			"Head", "Light", "Head · Light · iLvl 80"},
		{"a weapon: the type's name", items.ListItem{EquipLocations: []items.Term{{Slug: "main-hand", Name: "Main Hand"}}, ItemType: s("crossbow"), ItemTypeName: s("Crossbow")},
			"Main Hand", "Crossbow", "Main Hand · Crossbow"},
		{"a necklace: both columns, one mention on the phone", items.ListItem{EquipLocations: []items.Term{{Slug: "necklace", Name: "Necklace"}}, ItemType: s("necklace"), ItemTypeName: s("Necklace"), ItemLevel: &lvl},
			"Necklace", "Necklace", "Necklace · iLvl 80"},
		// ⛔ the review's case: classes and nothing before them — the old template led with " · "
		{"classes alone", items.ListItem{Classes: []items.Term{{Slug: "test-class", Name: "Test Class", ShortName: "TC"}, {Slug: "test-other", Name: "Test Other"}}},
			"", "", "TC/Test Other"},
		{"a type with no slot", items.ListItem{ItemType: s("mount"), ItemTypeName: s("Mount"), ItemLevel: &lvl},
			"", "Mount", "Mount · iLvl 80"},
		{"nothing at all", items.ListItem{}, "", "", ""},
	} {
		if got := templates.SlotNames(tc.it); got != tc.slot {
			t.Errorf("%s: SlotNames %q, want %q", tc.what, got, tc.slot)
		}
		if got := templates.TypeLabel(tc.it); got != tc.typ {
			t.Errorf("%s: TypeLabel %q, want %q", tc.what, got, tc.typ)
		}
		if got := templates.PhoneLine(tc.it); got != tc.phone {
			t.Errorf("%s: PhoneLine %q, want %q", tc.what, got, tc.phone)
		}
	}
}
