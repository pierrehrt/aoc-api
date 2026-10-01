package pages

import (
	"strconv"

	"github.com/pierrehrt/aoc-api/internal/items"
	"github.com/pierrehrt/aoc-api/internal/templates"
)

// The filter rail and its chips (AOC-049), built from items.Facets.
//
// ⭐ Presentation only. Every value and every count is the service's — the database's vocabularies,
// counted by the same SQL that keeps the rows — and the page's own words here are the rail's labels
// ("Rarity", "Any"), never a game term. One value per group (DECISIONS.md, AOC-049): the number
// beside a value is then exactly what choosing it gives.

// railFacet is one vocabulary facet: its parameter, its words on the rail, where its counts are, and
// how to read and clear it on a Filters.
type railFacet struct {
	param, legend, anyLabel string
	group                   func(*items.Facets) items.FacetGroup
	get                     func(items.Filters) string
	clear                   func(*items.Filters)
}

// The radio groups, then the selects, in the rail's order.
var (
	radioFacets = []railFacet{
		{"rarity", "Rarity", "Any", func(fc *items.Facets) items.FacetGroup { return fc.Rarity },
			func(f items.Filters) string { return f.Rarity }, func(f *items.Filters) { f.Rarity = "" }},
		{"equip_location", "Slot", "Any", func(fc *items.Facets) items.FacetGroup { return fc.EquipLocation },
			func(f items.Filters) string { return f.EquipLocation }, func(f *items.Filters) { f.EquipLocation = "" }},
		{"armour_weight", "Armour weight", "Any", func(fc *items.Facets) items.FacetGroup { return fc.ArmourWeight },
			func(f items.Filters) string { return f.ArmourWeight }, func(f *items.Filters) { f.ArmourWeight = "" }},
		{"class", "Class restriction", "Any", func(fc *items.Facets) items.FacetGroup { return fc.Class },
			func(f items.Filters) string { return f.Class }, func(f *items.Filters) { f.Class = "" }},
	}
	selectFacets = []railFacet{
		{"currency", "Currency", "Any currency", func(fc *items.Facets) items.FacetGroup { return fc.Currency },
			func(f items.Filters) string { return f.Currency }, func(f *items.Filters) { f.Currency = "" }},
		{"set", "Set", "Any set", func(fc *items.Facets) items.FacetGroup { return fc.Set },
			func(f items.Filters) string { return f.Set }, func(f *items.Filters) { f.Set = "" }},
	}
)

// options is a facet's choices: Any, then every value. A selected slug the vocabulary does not hold
// (a mistyped link) is kept as a checked choice at 0, so the form does not silently drop it and the
// empty state explains the page.
func options(rf railFacet, g items.FacetGroup, selected string) []templates.RailOption {
	out := []templates.RailOption{{Name: rf.param, ID: "f-" + rf.param + "-any", Label: rf.anyLabel, Count: g.Any, Checked: selected == ""}}
	found := selected == ""
	for _, v := range g.Values {
		out = append(out, templates.RailOption{
			Name: rf.param, ID: "f-" + rf.param + "-" + v.Slug, Value: v.Slug,
			Label: v.Name, Short: v.ShortName, ColourToken: v.ColourToken,
			Count: v.Count, Checked: v.Slug == selected,
		})
		found = found || v.Slug == selected
	}
	if !found {
		out = append(out, templates.RailOption{Name: rf.param, ID: "f-" + rf.param + "-unknown", Value: selected, Label: selected, Checked: true})
	}
	return out
}

// nameOf is the display name of a facet's selected slug; the slug itself when the vocabulary lacks it.
func nameOf(g items.FacetGroup, slug string) string {
	for _, v := range g.Values {
		if v.Slug == slug {
			return v.Name
		}
	}
	return slug
}

func levelText(n *int32) string {
	if n == nil {
		return ""
	}
	return strconv.Itoa(int(*n))
}

// rangeLabel is a level range as a chip reads: "Item level 70–80", "Item level ≥ 70", "… ≤ 80".
func rangeLabel(legend string, lo, hi *int32) string {
	switch {
	case lo != nil && hi != nil:
		return legend + " " + levelText(lo) + "–" + levelText(hi)
	case lo != nil:
		return legend + " ≥ " + levelText(lo)
	default:
		return legend + " ≤ " + levelText(hi)
	}
}

// buildRail is the rail, the active filters as chips, and the "clear all" URL for state f. here
// is the URL of a state, back on page 1.
func buildRail(f items.Filters, fc *items.Facets, here func(items.Filters) string) (templates.Rail, []templates.Chip, string) {
	if fc == nil {
		fc = &items.Facets{}
	}
	var rail templates.Rail
	var chips []templates.Chip
	chip := func(label string, without func(*items.Filters)) {
		g := f
		without(&g)
		chips = append(chips, templates.Chip{Label: label, URL: here(g)})
	}

	for _, rf := range radioFacets {
		g, sel := rf.group(fc), rf.get(f)
		rail.Groups = append(rail.Groups, templates.RailGroup{Legend: rf.legend, Options: options(rf, g, sel)})
		if sel != "" {
			chip(rf.legend+": "+nameOf(g, sel), rf.clear)
		}
	}

	// The price: three choices, counted (price=false is every item the other filters leave, minus
	// the priced ones).
	price := func(want *bool) bool {
		if f.Price == nil || want == nil {
			return f.Price == nil && want == nil
		}
		return *f.Price == *want
	}
	yes, no := true, false
	rail.Groups = append(rail.Groups, templates.RailGroup{Legend: "Vendor price", Options: []templates.RailOption{
		{Name: "price", ID: "f-price-any", Label: "Any", Count: fc.Price.Any, Checked: price(nil)},
		{Name: "price", ID: "f-price-true", Value: "true", Label: "Has a price", Count: fc.Price.Count, Checked: price(&yes)},
		{Name: "price", ID: "f-price-false", Value: "false", Label: "No price", Count: fc.Price.Any - fc.Price.Count, Checked: price(&no)},
	}})
	if f.Price != nil {
		label := "No vendor price"
		if *f.Price {
			label = "Has a vendor price"
		}
		chip(label, func(g *items.Filters) { g.Price = nil })
	}

	// The two level ranges: they differ on 234 items, so they are two controls (the ticket).
	for _, lr := range []struct {
		legend, min, max string
		lo, hi           *int32
		span             *items.LevelSpan
		clear            func(*items.Filters)
	}{
		{"Item level", "ilvl_min", "ilvl_max", f.ILvlMin, f.ILvlMax, fc.ItemLevel, func(g *items.Filters) { g.ILvlMin, g.ILvlMax = nil, nil }},
		{"Required level", "reqlvl_min", "reqlvl_max", f.ReqLvlMin, f.ReqLvlMax, fc.RequiresLevel, func(g *items.Filters) { g.ReqLvlMin, g.ReqLvlMax = nil, nil }},
	} {
		r := templates.RailRange{Legend: lr.legend, MinName: lr.min, MaxName: lr.max, MinValue: levelText(lr.lo), MaxValue: levelText(lr.hi)}
		if lr.span != nil {
			r.Lo, r.Hi = strconv.Itoa(int(lr.span.Min)), strconv.Itoa(int(lr.span.Max))
		}
		rail.Levels = append(rail.Levels, r)
		if lr.lo != nil || lr.hi != nil {
			chip(rangeLabel(lr.legend, lr.lo, lr.hi), lr.clear)
		}
	}

	for _, rf := range selectFacets {
		g, sel := rf.group(fc), rf.get(f)
		rail.Selects = append(rail.Selects, templates.RailSelect{Name: rf.param, ID: "f-" + rf.param, Label: rf.legend, Options: options(rf, g, sel)})
		if sel != "" {
			chip(rf.legend+": "+nameOf(g, sel), rf.clear)
		}
	}

	// What the rail has no control for rides along as hidden inputs — and as chips, so it can be
	// seen and removed. Literal "param: value": the page has no vocabulary for these here.
	if f.Sort != "" && f.Sort != items.SortILvl {
		rail.Hidden = append(rail.Hidden, templates.HiddenInput{Name: "sort", Value: f.Sort})
	}
	for _, o := range []struct {
		param, value string
		clear        func(*items.Filters)
	}{
		{"item_type", f.ItemType, func(g *items.Filters) { g.ItemType = "" }},
		{"region", f.Region, func(g *items.Filters) { g.Region = "" }},
		{"tier", f.Tier, func(g *items.Filters) { g.Tier = "" }},
	} {
		if o.value != "" {
			rail.Hidden = append(rail.Hidden, templates.HiddenInput{Name: o.param, Value: o.value})
			chip(o.param+": "+o.value, o.clear)
		}
	}
	for i, p := range f.Places {
		rail.Hidden = append(rail.Hidden, templates.HiddenInput{Name: "place", Value: p})
		chip("place: "+p, func(g *items.Filters) {
			g.Places = append(append([]string{}, f.Places[:i]...), f.Places[i+1:]...)
			if len(g.Places) == 0 {
				g.Places = nil
			}
		})
	}
	for _, o := range []struct {
		param string
		value *bool
		clear func(*items.Filters)
	}{
		{"pvp", f.PvP, func(g *items.Filters) { g.PvP = nil }},
		{"unchained", f.Unchained, func(g *items.Filters) { g.Unchained = nil }},
	} {
		if o.value != nil {
			v := strconv.FormatBool(*o.value)
			rail.Hidden = append(rail.Hidden, templates.HiddenInput{Name: o.param, Value: v})
			chip(o.param+": "+v, o.clear)
		}
	}

	clearAll := ""
	if len(chips) > 0 {
		clearAll = here(items.Filters{Query: f.Query, Sort: f.Sort})
	}
	return rail, chips, clearAll
}
