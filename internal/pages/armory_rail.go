package pages

import (
	"strconv"

	"github.com/pierrehrt/aoc-api/internal/items"
	"github.com/pierrehrt/aoc-api/internal/templates"
)

// The filter rail and its chips (AOC-049), built from items.Facets; drawn and worded as the validated
// design since AOC-065.
//
// ⭐ Presentation only. Every value and every count is the service's — the database's vocabularies,
// counted by the same SQL that keeps the rows. The page's own words are the design's: the pane's
// legends ("Rarity", "Slot"…) and the pills' prefixes ("rarity: Epic", "class: Conq", "ilvl 60–90",
// "q: …"), never a game term.

// railFacet is one vocabulary facet: its parameter, its legend in the pane, its pill's prefix, how the
// design draws it, where its counts are, and how to read and write its list on a Filters (AOC-064).
type railFacet struct {
	param, legend, pill string
	kind                string // "list" (rarity rows), "mono", "sans", "class" chips; "" = no control (not in the design)
	group               func(*items.Facets) items.FacetGroup
	get                 func(items.Filters) []string
	set                 func(*items.Filters, []string)
}

// The design's four groups in its order, then the two facets it has no control for.
var (
	checkFacets = []railFacet{
		{"rarity", "Rarity", "rarity", "list", func(fc *items.Facets) items.FacetGroup { return fc.Rarity },
			func(f items.Filters) []string { return f.Rarities }, func(f *items.Filters, v []string) { f.Rarities = v }},
		{"equip_location", "Slot", "slot", "mono", func(fc *items.Facets) items.FacetGroup { return fc.EquipLocation },
			func(f items.Filters) []string { return f.EquipLocations }, func(f *items.Filters, v []string) { f.EquipLocations = v }},
		{"armour_weight", "Armour weight", "weight", "sans", func(fc *items.Facets) items.FacetGroup { return fc.ArmourWeight },
			func(f items.Filters) []string { return f.ArmourWeights }, func(f *items.Filters, v []string) { f.ArmourWeights = v }},
		{"class", "Class restriction", "class", "class", func(fc *items.Facets) items.FacetGroup { return fc.Class },
			func(f items.Filters) []string { return f.Classes }, func(f *items.Filters, v []string) { f.Classes = v }},
	}
	paneless = []railFacet{
		{"currency", "Currency", "currency", "", func(fc *items.Facets) items.FacetGroup { return fc.Currency },
			func(f items.Filters) []string { return f.Currencies }, func(f *items.Filters, v []string) { f.Currencies = v }},
		{"set", "Set", "set", "", func(fc *items.Facets) items.FacetGroup { return fc.Set },
			func(f items.Filters) []string { return f.Sets }, func(f *items.Filters, v []string) { f.Sets = v }},
	}
)

func contains(xs []string, x string) bool {
	for _, y := range xs {
		if y == x {
			return true
		}
	}
	return false
}

// without is xs minus x, nil when nothing is left (an empty list is no filter, and must not be '{}').
func without(xs []string, x string) []string {
	var out []string
	for _, y := range xs {
		if y != x {
			out = append(out, y)
		}
	}
	return out
}

// checkOptions is a checkbox group: every value, ticked when chosen. A chosen slug the vocabulary does
// not hold (a mistyped link) stays as a ticked choice at 0, so the form does not silently drop it.
func checkOptions(rf railFacet, g items.FacetGroup, chosen []string) []templates.RailOption {
	var out []templates.RailOption
	known := map[string]bool{}
	for _, v := range g.Values {
		known[v.Slug] = true
		out = append(out, templates.RailOption{
			Name: rf.param, ID: "f-" + rf.param + "-" + v.Slug, Value: v.Slug,
			Label: v.Name, Short: v.ShortName, ColourToken: v.ColourToken,
			Count: v.Count, Checked: contains(chosen, v.Slug),
		})
	}
	for i, c := range chosen {
		if !known[c] {
			out = append(out, templates.RailOption{Name: rf.param, ID: "f-" + rf.param + "-unknown-" + strconv.Itoa(i), Value: c, Label: c, Checked: true})
		}
	}
	return out
}

// nameOf is how a pill names a facet's value: the short name where the vocabulary has one (the
// design's "class: Conq"), else the name; the slug itself when the vocabulary lacks it.
func nameOf(g items.FacetGroup, slug string) string {
	for _, v := range g.Values {
		if v.Slug == slug {
			if v.ShortName != "" {
				return v.ShortName
			}
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

// rangePill is a level range as the design's pill reads it, both ends: "ilvl 60–90". An open end is
// the span the other filters leave; with no span, "ilvl ≥ 60" / "ilvl ≤ 80".
func rangePill(prefix string, lo, hi *int32, span *items.LevelSpan) string {
	l, h := levelText(lo), levelText(hi)
	if span != nil {
		if l == "" {
			l = strconv.Itoa(int(span.Min))
		}
		if h == "" {
			h = strconv.Itoa(int(span.Max))
		}
	}
	switch {
	case l != "" && h != "":
		return prefix + " " + l + "–" + h
	case l != "":
		return prefix + " ≥ " + l
	default:
		return prefix + " ≤ " + h
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

	// The search is the design's first pill ("q: …"), and counts as active.
	if f.Query != "" {
		chip("q: "+f.Query, func(g *items.Filters) { g.Query = "" })
	}

	// One chip per chosen value; its × removes that value only.
	valueChips := func(rf railFacet, g items.FacetGroup) {
		for _, c := range rf.get(f) {
			c := c
			chip(rf.pill+": "+nameOf(g, c), func(h *items.Filters) { rf.set(h, without(rf.get(*h), c)) })
		}
	}
	for _, rf := range checkFacets {
		g := rf.group(fc)
		rail.Groups = append(rail.Groups, templates.RailGroup{Legend: rf.legend, Kind: rf.kind, Options: checkOptions(rf, g, rf.get(f))})
		valueChips(rf, g)
	}

	// The item level: the design's two stacked sliders.
	il := templates.RailRange{Legend: "Item level", MinName: "ilvl_min", MaxName: "ilvl_max", MinValue: levelText(f.ILvlMin), MaxValue: levelText(f.ILvlMax)}
	if fc.ItemLevel != nil {
		il.Lo, il.Hi = strconv.Itoa(int(fc.ItemLevel.Min)), strconv.Itoa(int(fc.ItemLevel.Max))
	}
	rail.Levels = append(rail.Levels, il)
	if f.ILvlMin != nil || f.ILvlMax != nil {
		chip(rangePill("ilvl", f.ILvlMin, f.ILvlMax, fc.ItemLevel), func(g *items.Filters) { g.ILvlMin, g.ILvlMax = nil, nil })
	}

	// Vendor price, required level, currency and set are not in the validated design's pane (Pierre,
	// 2026-10-01: "exactly like it is in the design"). They still filter — by link and by /v1 — so an
	// active one rides along as a hidden input and shows as a pill, removable like any other.
	if f.Price != nil {
		label := "vendor price: no"
		if *f.Price {
			label = "vendor price: yes"
		}
		rail.Hidden = append(rail.Hidden, templates.HiddenInput{Name: "price", Value: strconv.FormatBool(*f.Price)})
		chip(label, func(g *items.Filters) { g.Price = nil })
	}
	if f.ReqLvlMin != nil {
		rail.Hidden = append(rail.Hidden, templates.HiddenInput{Name: "reqlvl_min", Value: levelText(f.ReqLvlMin)})
	}
	if f.ReqLvlMax != nil {
		rail.Hidden = append(rail.Hidden, templates.HiddenInput{Name: "reqlvl_max", Value: levelText(f.ReqLvlMax)})
	}
	if f.ReqLvlMin != nil || f.ReqLvlMax != nil {
		chip(rangePill("reqlvl", f.ReqLvlMin, f.ReqLvlMax, fc.RequiresLevel), func(g *items.Filters) { g.ReqLvlMin, g.ReqLvlMax = nil, nil })
	}
	for _, rf := range paneless {
		g := rf.group(fc)
		for _, c := range rf.get(f) {
			rail.Hidden = append(rail.Hidden, templates.HiddenInput{Name: rf.param, Value: c})
		}
		valueChips(rf, g)
	}

	// What else the page accepts but offers no control for rides along the same way. Literal
	// "param: value": the page has no vocabulary for these here.
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

	// The design's "clear all" clears the search too, and keeps the sort.
	return rail, chips, here(items.Filters{Sort: f.Sort})
}
