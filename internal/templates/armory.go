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
	Query     string       // the search as typed, echoed into the box
	Sort      string       // the active sort key — the page defaults to items.SortILvl (the handler fills it)
	SortInURL bool         // true when Sort is not the page default, so a box search keeps it in the URL
	Sorts     []SortOption // the sort choices, in the order the page offers them

	Result items.ListResult // the page of rows, the total, the collapsing mode
	Span   items.IDSpan     // the honest empty state's numbers

	Page  int        // 1-based
	Pages int        // total pages at this page size
	Pager []PageLink // the numbered links, with Current on this page
	Prev  string     // URL of the previous page, "" on the first
	Next  string     // URL of the next page, "" on the last
	Clear string     // URL of the list with the search cleared
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
	return ArmoryData{
		Query: "probe", Sort: items.SortILvl,
		Sorts: []SortOption{{Key: items.SortILvl, Label: "Item level", URL: "/armory?sort=ilvl", Current: true}, {Key: items.SortName, Label: "Name", URL: "/armory?sort=name"}},
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
