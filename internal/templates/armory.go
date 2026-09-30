package templates

import "github.com/pierrehrt/aoc-api/internal/items"

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
			Total: 2, Limit: 50, Collapsed: true,
		},
		Span:  items.IDSpan{MinID: 1, MaxID: 2, Total: 2},
		Page:  1,
		Pages: 1,
		Pager: []PageLink{{N: 1, URL: "/armory", Current: true}},
		Clear: "/armory",
	}
}
