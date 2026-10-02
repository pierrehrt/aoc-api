package pages

import (
	"strings"

	"github.com/pierrehrt/aoc-api/internal/items"
	"github.com/pierrehrt/aoc-api/internal/templates"
)

// The sources panel (AOC-068), drawn as the validated design: the main categories under the search,
// one active at a time (Pierre, 2026-10-02), and the active one's tree on the left. Presentation
// only: the tabs, the branches, their names and counts are items.Tree's (AOC-050). What this file
// decides is how the design draws them — which branches are open, the indent, the two halves'
// headers — and the links, built from the whole state like every link on the page.

// buildSources is the panel for state f, and the label of the picked branch's pill ("" when none).
func buildSources(f items.Filters, tabs []items.SourceTab, tree items.Tree, here func(items.Filters) string) (templates.Sources, string) {
	out := templates.Sources{
		Title:      "Source · " + tree.Tab.Name,
		LevelsNote: tree.Tab.LevelsNote,
		EndPoints:  tree.EndPoints,
		Picked:     !f.Source.IsZero(),
	}

	// A tab is a link to the same state in that tab, with no branch picked: a branch belongs to its
	// tab's tree (the prototype's rule). The first tab is the page's default, so it is left out of
	// the URL, and one state has one URL.
	for i, t := range tabs {
		g := f
		g.Tab, g.Source = t.Slug, items.SourceNode{}
		if i == 0 {
			g.Tab = ""
		}
		out.Tabs = append(out.Tabs, templates.SourceTabLink{Slug: t.Slug, Label: t.Name, URL: here(g), Current: t.Slug == tree.Tab.Slug})
	}

	picked := f.Source
	picked.Group = ""
	path := picked.String()
	// pick is the state with branch src (and half group) picked, in this tab.
	pick := func(src, group string) string {
		g := f
		node, _ := items.ParseSource(src) // the tree wrote it; it parses
		node.Group = group
		g.Source = node
		return here(g)
	}

	// The picked branch and every branch above it are open; the open branches derive from the pick,
	// never from a stored state, so a link and a reload draw the same tree.
	var trail []string
	var coords string
	var find func(ns []*items.TreeNode, above []string) bool
	find = func(ns []*items.TreeNode, above []string) bool {
		for _, n := range ns {
			names := append(append([]string{}, above...), n.Name)
			if n.Source == path {
				trail, coords = names, n.Coords
				return true
			}
			if strings.HasPrefix(path, n.Source+".") && find(n.Children, names) {
				return true
			}
		}
		return false
	}
	found := path != "" && find(tree.Nodes, nil)
	isOpen := func(n *items.TreeNode) bool {
		return path != "" && (n.Source == path || strings.HasPrefix(path, n.Source+"."))
	}

	pad := func(depth int) int { return 8 + 15*depth }
	var walk func(ns []*items.TreeNode, depth int, parent string)
	walk = func(ns []*items.TreeNode, depth int, parent string) {
		for _, n := range ns {
			open := isOpen(n) && len(n.Children) > 0
			row := templates.TreeRow{ID: rowID(n.Source, ""), Label: n.Name, Count: n.Count, Pad: pad(depth), Open: open,
				Selected: n.Source == path && f.Source.Group == "", URL: pick(n.Source, "")}
			switch {
			case open:
				row.Caret = "▾"
			case len(n.Children) > 0:
				row.Caret = "▸"
			}
			// The picked branch, open, closes when picked again: the design's toggle, as a link to
			// the branch above it (or to no branch at all).
			if row.Selected && open {
				if parent == "" {
					g := f
					g.Source = items.SourceNode{}
					row.URL = here(g)
				} else {
					row.URL = pick(parent, "")
				}
			}
			out.Rows = append(out.Rows, row)
			if !open {
				continue
			}
			if leavesOnly(n.Children) {
				halves(n.Children, tree.Halves, depth+1, pad, pick, path, f.Source.Group, &out.Rows)
			} else {
				walk(n.Children, depth+1, n.Source)
			}
		}
	}
	walk(tree.Nodes, 0, "")

	label := ""
	if out.Picked {
		label = "source: " + path
		if found {
			out.Selected = strings.Join(trail, " › ")
			out.Coords = coords
			label = "source: " + trail[len(trail)-1]
		}
		if f.Source.Group != "" {
			label += " · " + groupName(tree, f.Source.Group)
		}
	}
	return out, label
}

// leavesOnly reports whether every branch has nothing under it: the design splits such a set into
// its two halves ("loot / drops", "quest / vendor").
func leavesOnly(ns []*items.TreeNode) bool {
	for _, n := range ns {
		if len(n.Children) > 0 {
			return false
		}
	}
	return len(ns) > 0
}

// halves draws a set of end branches the design's way: a header per acquisition group, in the
// groups' order, with every branch that has that half under it, counted for that half alone. A
// branch with no half at all (a row with no acquisition type) follows, plain.
func halves(ns []*items.TreeNode, order []items.Term, depth int, pad func(int) int, pick func(string, string) string,
	path, group string, rows *[]templates.TreeRow) {

	for _, g := range order {
		has := false
		for _, n := range ns {
			for _, ng := range n.Groups {
				has = has || ng.Slug == g.Slug
			}
		}
		if !has {
			continue
		}
		*rows = append(*rows, templates.TreeRow{Label: g.Name, Pad: pad(depth), Header: true})
		for _, n := range ns {
			for _, ng := range n.Groups {
				if ng.Slug == g.Slug {
					*rows = append(*rows, templates.TreeRow{ID: rowID(n.Source, g.Slug), Label: n.Name, Count: ng.Count, Pad: pad(depth + 1),
						URL: pick(n.Source, g.Slug), Selected: n.Source == path && group == g.Slug})
				}
			}
		}
	}
	for _, n := range ns {
		if len(n.Groups) == 0 {
			*rows = append(*rows, templates.TreeRow{ID: rowID(n.Source, ""), Label: n.Name, Count: n.Count, Pad: pad(depth),
				URL: pick(n.Source, ""), Selected: n.Source == path && group == ""})
		}
	}
}

// groupName is a half's name, or its slug when the tree lists no such half.
func groupName(tree items.Tree, slug string) string {
	for _, g := range tree.Halves {
		if g.Slug == slug {
			return g.Name
		}
	}
	return slug
}

// rowID is a tree link's id: the branch's source path (and its half), spelled with what an id and a
// CSS selector take without escaping. Slugs hold no "_", so ":" → "_" and "." → "__" stay one-to-one.
// The same branch has the same id in every answer, so htmx gives focus back to the row the reader
// used (AOC-068 verify round 1, F3).
func rowID(source, group string) string {
	id := "src-" + strings.ReplaceAll(strings.ReplaceAll(source, ".", "__"), ":", "_")
	if group != "" {
		id += "--" + group
	}
	return id
}
