package templates

import (
	"strconv"
	"strings"

	"github.com/pierrehrt/aoc-api/internal/items"
)

// Gear is the gear builder's pane (AOC-051), built by the handler from builds.Service; the template
// only prints it. Every slot, class and stat is the service's — nothing here names one.
type Gear struct {
	// Open: the pane opens on load — the URL carries a build, or there is a refusal to show.
	Open    bool
	Present bool // the URL carries a build (even an empty one)
	// Filled is the slots the URL fills, Total all of them ("3/14").
	Filled, Total, Conflicts int
	Rows                     []GearRow
	Classes                  []GearClass
	// Hidden is the build riding in the form, so a filter change keeps it (the rail's pattern).
	// The class rides in its own <select>.
	Hidden []HiddenInput
	// ClearURL empties the build (the builder stays open); DropConflictsURL removes what the class
	// cannot wear; ShareURL is the build alone, absolute — the link to send.
	ClearURL, DropConflictsURL, ShareURL string
	Lines                                []GearLine // the combined stats, base lines first
	SumNote                              string     // "raw sum: …"
	Refusal                              string     // why the last add did not happen, or ""
	EmptyText                            string     // the stats box when there is nothing to sum
	// AddURLs is each list row's "+": this state with `add=<id>`. Only rows that go in a slot.
	AddURLs map[int32]string
	// Dim is the list rows the picked class cannot wear (the design's .34).
	Dim map[int32]bool
}

// GearRow is one slot of the builder.
type GearRow struct {
	N          int
	Slot, Name string // the slot's slug and name
	Status     string // a builds.Status*
	ItemID     int32
	ItemName   string
	ItemSlug   string // the item page; "" for an id no item has
	Colour     string // the item's rarity colour token, "" for none
	Tip        string // the item's tooltip image, "" for none: shown beside the cursor (AOC-074)
	Note       string // what is wrong with it, when something is: "⚠ not Barbarian"
	RemoveURL  string // this state without the slot's item; "" when it is empty
}

// GearClass is one choice of the class picker.
type GearClass struct {
	Slug, Label string
	Selected    bool
}

// GearLine is one line of the combined stats: "+154" "Constitution".
type GearLine struct{ Value, Label string }

// GearValue prints a summed stat's value as the builder shows it: a sign, thousands separators as
// the design's toLocaleString draws them, the decimals the sum has, and "%" for a percentage —
// "+1,300", "+6.5", "-3%", "0". The label is GearLabel: the two halves of templates.StatText.
func GearValue(s items.StatLine) string {
	v := items.TrimNumber(s.Value)
	whole, frac, _ := strings.Cut(v, ".")
	n, err := strconv.ParseInt(whole, 10, 64)
	if err == nil {
		whole = Num(n)
	}
	if frac != "" {
		whole += "." + frac
	}
	switch {
	case s.Sign > 0:
		whole = "+" + whole
	case s.Sign < 0:
		whole = "-" + whole
	}
	if s.Unit == "percent" {
		whole += "%"
	}
	return whole
}

// GearLabel is a summed stat's name as the tooltip prints it: "Magic Damage (Fire)". PvP stats carry
// "PvP" in their own name.
func GearLabel(s items.StatLine) string {
	if dt := deref(s.DamageType); dt != "" {
		return s.Stat + " (" + dt + ")"
	}
	return s.Stat
}

// gearProbe renders every branch of the pane at boot: each status, a class picked and not, a
// refusal, conflicts, lines and the empty box's text. Obviously fake (CLAUDE.md STEP ZERO).
func gearProbe() Gear {
	return Gear{
		Open: true, Present: true, Filled: 4, Total: 6, Conflicts: 1,
		Rows: []GearRow{
			{N: 1, Slot: "test-head", Name: "Test Head", Status: "equipped", ItemID: 1, ItemName: "Test Item Alpha", ItemSlug: "test-item-alpha", Colour: "rarity-epic", RemoveURL: "/armory?gear="},
			{N: 2, Slot: "test-chest", Name: "Test Chest", Status: "empty"},
			{N: 3, Slot: "test-ring", Name: "Test Ring", Status: "conflict", ItemID: 2, ItemName: "Test Item Beta", ItemSlug: "test-item-beta", Note: "⚠ not Test Class", RemoveURL: "/armory?gear="},
			{N: 4, Slot: "test-main", Name: "Test Main", Status: "equipped", ItemID: 3, ItemName: "Test Item Gamma", ItemSlug: "test-item-gamma", RemoveURL: "/armory?gear="},
			{N: 5, Slot: "test-off", Name: "Test Off", Status: "held", Note: "taken: Test Item Gamma needs both hands"},
			{N: 6, Slot: "test-neck", Name: "Test Neck", Status: "unknown", ItemID: 9, Note: "unknown item 9", RemoveURL: "/armory?gear="},
		},
		Classes:  []GearClass{{Slug: "test-class", Label: "Test Class · Test Archetype", Selected: true}, {Slug: "test-other", Label: "Test Other · Test Archetype"}},
		Hidden:   []HiddenInput{{Name: "gear", Value: "test-head:1"}},
		ClearURL: "/armory?gear=", DropConflictsURL: "/armory?gear=test-head:1", ShareURL: "https://example.test/armory?gear=test-head:1",
		Lines:     []GearLine{{Value: "1,211", Label: "Armor"}, {Value: "+10", Label: "Test Strength (Test)"}},
		SumNote:   "raw sum: test",
		Refusal:   "Test Item Delta does not go in the Test Head slot.",
		EmptyText: "Test empty text.",
		AddURLs:   map[int32]string{1: "/armory?add=1"},
		Dim:       map[int32]bool{2: true},
	}
}
