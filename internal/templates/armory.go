package templates

import (
	"strconv"
	"strings"

	"github.com/pierrehrt/aoc-api/internal/items"
)

// ArmoryData is everything the Armory list page reads (AOC-047). It lives here, beside the
// template, because the startup probe must execute the page with a filled-in value of exactly this
// shape: a field the template reads that the probe does not supply fails the boot, not a visitor.
// The handler in internal/pages fills it from items.Service — the SAME service the JSON surface
// calls, so a row cannot mean one thing in /v1 and another on the page.
type ArmoryData struct {
	Query string       // the search as typed, echoed into the box
	Sort  string       // the active sort key — the page defaults to items.SortILvl (the handler fills it)
	Sorts []SortOption // the sort choices, in the order the page offers them

	Result items.ListResult // the page of rows, the total, the collapsing mode
	Span   items.IDSpan     // the honest empty state's numbers

	Page  int        // 1-based
	Pages int        // total pages at this page size
	Pager []PageLink // the numbered links, with Current on this page
	Prev  string     // URL of the previous page, "" on the first
	Next  string     // URL of the next page, "" on the last
	Clear string     // URL of the list with the search cleared (the filters kept)

	// The filter rail (AOC-049). Built by the handler from items.Facets; the template only prints.
	Rail     Rail
	Chips    []Chip // the active filters, each with the URL that removes it
	ClearAll string // URL with every filter removed, the search and sort kept; "" when none is active
}

// InvalidSearch is what an HTMX request with a malformed filter gets back, in place of the rows: the
// parser's reason, while the rail — and the reader's input in it — stays as it is to be corrected.
type InvalidSearch struct{ Reason string }

// Active is how many filters are on — the number the phone's Filters button carries.
func (d ArmoryData) Active() int { return len(d.Chips) }

// Rail is the filter form's controls, in the order the rail shows them. Every value and every
// count comes from items.Facets — the database's vocabularies (reference/content-model.md § 0).
type Rail struct {
	Groups  []RailGroup  // one-value-per-group choices, each a radio group led by "Any"
	Levels  []RailRange  // item level, required level
	Selects []RailSelect // the long vocabularies: currency (24), set (368)
	// Hidden carries what the rail has no control for — the sort, and the /v1 filters the page
	// accepts but does not offer (region, tier, place …) — so submitting the form keeps them.
	// Inside the rail on purpose: an HTMX answer re-renders the rail, so these can never go stale.
	Hidden []HiddenInput
}

// RailGroup is a radio group: Options[0] is "Any", then every vocabulary value.
type RailGroup struct {
	Legend  string
	Options []RailOption
}

// RailOption is one choice. Count is how many items choosing it would leave under the other
// filters; a 0 renders muted, never hidden.
type RailOption struct {
	Name        string // the parameter — the input's name
	ID          string // the input's id; stable across renders, so HTMX gives focus back after a swap
	Value       string // the slug; "" for Any
	Label       string // the full name
	Short       string // a class's short name, shown in place of Label (which stays, for screen readers); "" otherwise
	ColourToken string // a rarity's colour token; "" when it has none
	Count       int64
	Checked     bool
}

// RailRange is a level range: two number inputs, with the span the other filters leave as
// placeholders ("" when no item under them has that level).
type RailRange struct {
	Legend             string
	MinName, MaxName   string
	MinValue, MaxValue string // what the URL holds, echoed
	Lo, Hi             string
}

// RailSelect is a vocabulary too long for a row of choices.
type RailSelect struct {
	Name, ID, Label string
	Options         []RailOption // Options[0] is "Any"
}

// HiddenInput is a parameter carried through the form unchanged.
type HiddenInput struct{ Name, Value string }

// Chip is one active filter, removable.
type Chip struct {
	Label string
	URL   string // this state without the filter, back on page 1
}

// SortOption is one entry of the sort control.
type SortOption struct {
	Key     string
	Label   string
	URL     string // this list, sorted by Key, back on page 1
	Current bool
}

// PageLink is one numbered pager entry.
type PageLink struct {
	N       int
	URL     string
	Current bool
}

// SlotNames is a row's slots as the list prints them: "Left Finger, Right Finger"; "" for none.
func SlotNames(it items.ListItem) string {
	names := make([]string, 0, len(it.EquipLocations))
	for _, s := range it.EquipLocations {
		names = append(names, s.Name)
	}
	return strings.Join(names, ", ")
}

// TypeLabel is the list's Type column (AOC-062): the armour weight when the item has one, otherwise
// the item type's name — the order the tooltip's own first line reads, `Light Armor - Hands`,
// `Crossbow - Main Hand`. "" when it has neither. A necklace is "Necklace" here AND in Slot: two
// columns are two facts.
func TypeLabel(it items.ListItem) string {
	if it.ArmourWeight != nil && it.ArmourWeight.Name != "" {
		return it.ArmourWeight.Name
	}
	return deref(it.ItemTypeName)
}

// PhoneLine is a row's one line under its name on a phone (design 1b): slots · type · iLvl · classes,
// ONLY the parts present, joined once — so no part can lead or trail a separator. The template used
// to place each " · " by hand, and got it wrong three times (AOC-047 verify rounds 1 and 2; the
// classes separator, AOC-062 review). One line has no room for a repeat, so the type is left out when
// it only repeats the slot (a necklace: "Necklace · iLvl 80").
func PhoneLine(it items.ListItem) string {
	var parts []string
	if s := SlotNames(it); s != "" {
		parts = append(parts, s)
	}
	if t := TypeLabel(it); t != "" && (it.ArmourWeight != nil || !typeRepeatsSlot(deref(it.ItemType), deref(it.ItemTypeName), it.EquipLocations)) {
		parts = append(parts, t)
	}
	if it.ItemLevel != nil {
		parts = append(parts, "iLvl "+strconv.Itoa(int(*it.ItemLevel)))
	}
	if len(it.Classes) > 0 {
		cs := make([]string, 0, len(it.Classes))
		for _, c := range it.Classes {
			if c.ShortName != "" {
				cs = append(cs, c.ShortName)
			} else {
				cs = append(cs, c.Name)
			}
		}
		parts = append(parts, strings.Join(cs, "/"))
	}
	return strings.Join(parts, " · ")
}

// armoryProbe is what the startup probe renders the Armory page with: every branch of the
// template — a row with and without a level, a class, a slot, a price, a token, a place — and the
// pager, so a template change that reads a new field fails at boot. Obviously fake names, never a
// real item (CLAUDE.md STEP ZERO).
func armoryProbe() ArmoryData {
	lvl := int32(80)
	price := "3 Test Token"
	typ, typName := "test-type", "Test Type"
	anyRarity := RailOption{Name: "rarity", ID: "f-rarity-any", Label: "Any", Count: 3}
	return ArmoryData{
		Query: "probe", Sort: items.SortILvl,
		Rail: Rail{
			Groups: []RailGroup{{Legend: "Rarity", Options: []RailOption{anyRarity,
				{Name: "rarity", ID: "f-rarity-epic", Value: "epic", Label: "Test Epic", ColourToken: "rarity-epic", Count: 1, Checked: true},
				{Name: "rarity", ID: "f-rarity-dull", Value: "dull", Label: "Test Dull", Count: 0}}},
				{Legend: "Class restriction", Options: []RailOption{{Name: "class", ID: "f-class-any", Label: "Any", Count: 3, Checked: true},
					{Name: "class", ID: "f-class-tc", Value: "tc", Label: "Test Class", Short: "TC", Count: 1}}}},
			Levels:  []RailRange{{Legend: "Item level", MinName: "ilvl_min", MaxName: "ilvl_max", MinValue: "70", Lo: "1", Hi: "90"}},
			Selects: []RailSelect{{Name: "currency", ID: "f-currency", Label: "Currency", Options: []RailOption{{Name: "currency", Label: "Any currency", Count: 3, Checked: true}, {Name: "currency", Value: "test-token", Label: "Test Token", Count: 0}}}},
			Hidden:  []HiddenInput{{Name: "sort", Value: "name"}},
		},
		Chips:    []Chip{{Label: "Rarity: Test Epic", URL: "/armory"}},
		ClearAll: "/armory",
		Sorts:    []SortOption{{Key: items.SortILvl, Label: "Item level", URL: "/armory?sort=ilvl", Current: true}, {Key: items.SortName, Label: "Name", URL: "/armory?sort=name"}},
		Result: items.ListResult{
			Items: []items.ListItem{
				{ID: 1, Slug: "test-item-alpha", Name: "Test Item Alpha", Rarity: "epic", RarityColourToken: "rarity-epic", ItemType: &typ, ItemTypeName: &typName, ItemLevel: &lvl, ArmourWeight: &items.Term{Slug: "light", Name: "Light"},
					EquipLocations: []items.Term{{Slug: "head", Name: "Head"}}, Classes: []items.Term{{Slug: "test-class", Name: "Test Class", ShortName: "TC"}}, Price: &price,
					Places: []items.PlaceRef{{Slug: "test-place", Name: "Test Place"}}},
				{ID: 2, Slug: "test-item-beta", Name: "Test Item Beta", Rarity: "mundane"},
				{ID: 3, Slug: "test-item-gamma", Name: "Test Item Gamma", Rarity: "rare", ItemType: &typ, ItemTypeName: &typName},
			},
			Total: 3, Limit: 50, Collapsed: true,
		},
		Span:  items.IDSpan{MinID: 1, MaxID: 3, Total: 3},
		Page:  1,
		Pages: 1,
		Pager: []PageLink{{N: 1, URL: "/armory", Current: true}},
		Clear: "/armory",
	}
}
