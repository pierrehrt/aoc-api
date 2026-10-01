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
// how to read and write its list on a Filters (AOC-064: every facet is a list).
type railFacet struct {
	param, legend, anyLabel string
	kind                    string // how the design draws the group: "list", "mono", "sans", "class"
	group                   func(*items.Facets) items.FacetGroup
	get                     func(items.Filters) []string
	set                     func(*items.Filters, []string)
}

// The checkbox groups, then the selects, in the rail's order.
var (
	checkFacets = []railFacet{
		{"rarity", "Rarity", "", "list", func(fc *items.Facets) items.FacetGroup { return fc.Rarity },
			func(f items.Filters) []string { return f.Rarities }, func(f *items.Filters, v []string) { f.Rarities = v }},
		{"equip_location", "Slot", "", "mono", func(fc *items.Facets) items.FacetGroup { return fc.EquipLocation },
			func(f items.Filters) []string { return f.EquipLocations }, func(f *items.Filters, v []string) { f.EquipLocations = v }},
		{"armour_weight", "Armour weight", "", "sans", func(fc *items.Facets) items.FacetGroup { return fc.ArmourWeight },
			func(f items.Filters) []string { return f.ArmourWeights }, func(f *items.Filters, v []string) { f.ArmourWeights = v }},
		{"class", "Class restriction", "", "class", func(fc *items.Facets) items.FacetGroup { return fc.Class },
			func(f items.Filters) []string { return f.Classes }, func(f *items.Filters, v []string) { f.Classes = v }},
	}
	selectFacets = []railFacet{
		{"currency", "Currency", "Any currency", "", func(fc *items.Facets) items.FacetGroup { return fc.Currency },
			func(f items.Filters) []string { return f.Currencies }, func(f *items.Filters, v []string) { f.Currencies = v }},
		{"set", "Set", "Any set", "", func(fc *items.Facets) items.FacetGroup { return fc.Set },
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
			Count: v.Count, Checked: contains(chosen, v.Slug), Multi: true,
		})
	}
	for i, c := range chosen {
		if !known[c] {
			out = append(out, templates.RailOption{Name: rf.param, ID: "f-" + rf.param + "-unknown-" + strconv.Itoa(i), Value: c, Label: c, Checked: true, Multi: true})
		}
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

	// One chip per chosen value; its × removes that value only.
	valueChips := func(rf railFacet, g items.FacetGroup) {
		for _, c := range rf.get(f) {
			c := c
			chip(rf.legend+": "+nameOf(g, c), func(h *items.Filters) { rf.set(h, without(rf.get(*h), c)) })
		}
	}
	for _, rf := range checkFacets {
		g := rf.group(fc)
		rail.Groups = append(rail.Groups, templates.RailGroup{Legend: rf.legend, Kind: rf.kind, Options: checkOptions(rf, g, rf.get(f))})
		valueChips(rf, g)
	}

	// Vendor price, required level, currency and set are not in the validated design's pane (Pierre,
	// 2026-10-01: "exactly like it is in the design"). They still filter — by link and by /v1 — so an
	// active one rides along as a hidden input and shows as a pill, removable like any other.
	if f.Price != nil {
		label := "No vendor price"
		if *f.Price {
			label = "Has a vendor price"
		}
		rail.Hidden = append(rail.Hidden, templates.HiddenInput{Name: "price", Value: strconv.FormatBool(*f.Price)})
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
		if lr.min == "ilvl_min" { // the design's pane has the item level only
			r := templates.RailRange{Legend: lr.legend, MinName: lr.min, MaxName: lr.max, MinValue: levelText(lr.lo), MaxValue: levelText(lr.hi)}
			if lr.span != nil {
				r.Lo, r.Hi = strconv.Itoa(int(lr.span.Min)), strconv.Itoa(int(lr.span.Max))
			}
			rail.Levels = append(rail.Levels, r)
		} else {
			if lr.lo != nil {
				rail.Hidden = append(rail.Hidden, templates.HiddenInput{Name: lr.min, Value: levelText(lr.lo)})
			}
			if lr.hi != nil {
				rail.Hidden = append(rail.Hidden, templates.HiddenInput{Name: lr.max, Value: levelText(lr.hi)})
			}
		}
		if lr.lo != nil || lr.hi != nil {
			chip(rangeLabel(lr.legend, lr.lo, lr.hi), lr.clear)
		}
	}

	for _, rf := range selectFacets {
		g := rf.group(fc)
		for _, c := range rf.get(f) {
			rail.Hidden = append(rail.Hidden, templates.HiddenInput{Name: rf.param, Value: c})
		}
		valueChips(rf, g)
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
