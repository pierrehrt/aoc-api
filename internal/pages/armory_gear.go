package pages

import (
	"net/url"
	"strconv"
	"strings"

	"github.com/pierrehrt/aoc-api/internal/builds"
	"github.com/pierrehrt/aoc-api/internal/items"
	"github.com/pierrehrt/aoc-api/internal/templates"
)

// The gear builder's pane (AOC-051). builds.Service decides everything — what fits, what is held, what
// the class cannot wear, what is summed; this turns its Result into what the pane prints, with every
// link built from the whole state (the list's filters and page, and the build), like the rail's.

// gearEmpty is the stats box with nothing in the build: the design's words, and the "+" that is the
// way to add without dragging (on a phone, the only one).
const gearEmpty = "Drag an item row from the list into a slot, or use the + on a row. Combined stats appear here once a piece with recorded stats is equipped."

// buildGear fills the pane, and the list rows' "+" and dimming, for one state.
func buildGear(f items.Filters, page int, res builds.Result, refusal string, rows []items.ListItem, base string) templates.Gear {
	b := res.Build
	at := func(nb builds.Build) string { return armoryURL(f, page, nb) }
	g := templates.Gear{
		Open: b.Present || refusal != "", Present: b.Present,
		Filled: res.Filled, Total: len(res.Slots), Conflicts: res.Conflicts,
		SumNote: res.Sum, Refusal: refusal,
		ClearURL: at(builds.Build{Class: b.Class, ID: b.ID, Present: true}),
		// The build alone: the link to send opens the Armory as it opens for anyone, with this build —
		// and without this browser's id for it (builds.Build.Shared).
		ShareURL: base + armoryURL(items.Filters{}, 1, b.Shared()),
		AddURLs:  map[int32]string{}, Dim: map[int32]bool{},
	}
	for _, v := range b.Values()[builds.ParamGear] {
		g.Hidden = append(g.Hidden, templates.HiddenInput{Name: builds.ParamGear, Value: v})
	}
	if b.ID != "" {
		g.Hidden = append(g.Hidden, templates.HiddenInput{Name: builds.ParamID, Value: b.ID})
	}

	var conflicts []string
	for i, s := range res.Slots {
		row := templates.GearRow{N: i + 1, Slot: s.Slot.Slug, Name: s.Slot.Name, Status: s.Status, ItemID: s.ItemID}
		if s.Item != nil {
			row.ItemName, row.ItemSlug, row.Colour = s.Item.Name, s.Item.Slug, s.Item.RarityColourToken
		}
		if s.ItemID != 0 {
			row.RemoveURL = at(b.Without(s.Slot.Slug))
		}
		switch s.Status {
		case builds.StatusConflict:
			row.Note = "⚠ not " + res.Class.Name
			conflicts = append(conflicts, s.Slot.Slug)
		case builds.StatusWrongSlot:
			row.Note = "⚠ does not go here"
		case builds.StatusUnknown:
			row.Note = "unknown item " + strconv.Itoa(int(s.ItemID))
		case builds.StatusEmpty:
			if s.Allows != nil && s.HeldBy != nil {
				row.Note = s.Allows.Name + " only, beside " + s.HeldBy.Name // bow → ammunition (Pierre)
			}
		case builds.StatusHeld:
			if s.ItemID != 0 {
				row.Note = "⚠ " + s.HeldBy.Name + " needs both hands"
			} else {
				row.Note = "taken: " + s.HeldBy.Name + " needs both hands"
			}
		}
		g.Rows = append(g.Rows, row)
	}
	if len(conflicts) > 0 {
		g.DropConflictsURL = at(b.Without(conflicts...))
	}
	for _, c := range res.Classes {
		g.Classes = append(g.Classes, templates.GearClass{Slug: c.Slug, Label: c.Name + " · " + c.Archetype, Selected: res.Class != nil && res.Class.Slug == c.Slug})
	}

	// The item page's base lines first, then the stat lines in the order the service summed them.
	if res.Armor != nil {
		g.Lines = append(g.Lines, templates.GearLine{Value: templates.Num(*res.Armor), Label: "Armor"})
	}
	if res.Critigation != nil {
		g.Lines = append(g.Lines, templates.GearLine{Value: templates.Num(*res.Critigation), Label: "Critigation Amount"})
	}
	for _, s := range res.Stats {
		g.Lines = append(g.Lines, templates.GearLine{Value: templates.GearValue(s), Label: templates.GearLabel(s)})
	}
	g.EmptyText = gearEmpty
	if res.Filled > 0 {
		g.EmptyText = "None of the pieces worn has recorded stats."
	}

	for _, it := range rows {
		if len(it.EquipLocations) > 0 {
			g.AddURLs[it.ID] = withParam(at(b), builds.ParamAdd, strconv.Itoa(int(it.ID)))
		}
		if res.Class != nil {
			classes := make([]string, 0, len(it.Classes))
			for _, c := range it.Classes {
				classes = append(classes, c.Slug)
			}
			weight := ""
			if it.ArmourWeight != nil {
				weight = it.ArmourWeight.Slug
			}
			if !res.Wears(classes, weight) {
				g.Dim[it.ID] = true
			}
		}
	}
	return g
}

// withParam adds one parameter to a URL the page built.
func withParam(u, k, v string) string {
	sep := "?"
	if strings.Contains(u, "?") {
		sep = "&"
	}
	return u + sep + url.QueryEscape(k) + "=" + url.QueryEscape(v)
}

// gearTitle names a state with a build in it, for the title and description a shared link previews
// with: "Gear build — Armory", and the items in it.
func gearTitle(res builds.Result) (string, string, bool) {
	var names []string
	for _, s := range res.Slots {
		if s.Item != nil {
			names = append(names, s.Item.Name)
		}
	}
	if len(names) == 0 {
		return "", "", false
	}
	return "Gear build — Armory", "A gear build in the Age of Conan armory: " + strings.Join(names, ", ") + ".", true
}
