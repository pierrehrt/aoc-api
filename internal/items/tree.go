package items

import (
	"context"
	"fmt"
	"sort"

	"github.com/pierrehrt/aoc-api/internal/db/sqlcgen"
	"github.com/pierrehrt/aoc-api/internal/httpx"
)

// The Armory's source panel (AOC-050). Pierre, 2026-10-02 (DECISIONS.md): the main categories under
// the search — PVE, PVP, Region, Faction, Onslaught, Other — each hold some of AoC>TV's 39 armory
// sections, and the panel below shows the active one's tree. Which sections a tab holds and which
// levels it draws are rows (source_tabs, sections); this file only follows them.

// SourceTab is one main category, as the panel and /v1/taxonomies show it.
type SourceTab struct {
	Slug       string `json:"slug"`
	Name       string `json:"name"`
	LevelsNote string `json:"levels_note"`
	// Groups is the levels the tab draws above the location (section, region, map); every tab
	// then draws location › boss.
	Groups []string `json:"groups"`
}

// GroupCount is how many of a node's items come from one half of it ("loot / drops", "quest /
// vendor"): the design's split of a location.
type GroupCount struct {
	Slug  string `json:"slug"`
	Name  string `json:"name"`
	Count int64  `json:"count"`
}

// TreeNode is one branch of the panel. Source is the `source` value that picks it; Count is how many
// items choosing it would leave under every other filter (the rail's rule, AOC-049), 0 included.
type TreeNode struct {
	Kind     string       `json:"kind"` // section, region, map, place, boss, vendor, quest, container
	Slug     string       `json:"slug"`
	Name     string       `json:"name"`
	Source   string       `json:"source"`
	Count    int64        `json:"count"`
	Groups   []GroupCount `json:"groups,omitempty"`
	Children []*TreeNode  `json:"children,omitempty"`

	sort  int32
	items map[int32]struct{}
	by    map[string]map[int32]struct{} // group slug -> items
	index map[string]*TreeNode          // child by "kind:slug"
}

// Tree is one tab's panel.
type Tree struct {
	Tab SourceTab `json:"tab"`
	// Total is the distinct items the tab's sections hold under every other filter.
	Total int64 `json:"total"`
	// EndPoints is how many branches have no branch under them — the design's "N end points".
	EndPoints int         `json:"end_points"`
	Nodes     []*TreeNode `json:"nodes"`
}

// Tabs lists the main categories in Pierre's order.
func (s *Service) Tabs(ctx context.Context) ([]SourceTab, error) {
	rows, err := s.q.ListSourceTabs(ctx)
	if err != nil {
		return nil, fmt.Errorf("list source tabs: %w", err)
	}
	out := make([]SourceTab, 0, len(rows))
	for _, r := range rows {
		out = append(out, SourceTab{Slug: r.Slug, Name: r.Name, LevelsNote: r.LevelsNote, Groups: r.Groups})
	}
	return out, nil
}

// tab is the named tab, or the first one when none is named. ok is false for a name no tab has.
func (s *Service) tab(ctx context.Context, slug string) (SourceTab, bool, error) {
	tabs, err := s.Tabs(ctx)
	if err != nil {
		return SourceTab{}, false, err
	}
	for _, t := range tabs {
		if slug == "" || t.Slug == slug {
			return t, true, nil
		}
	}
	return SourceTab{}, false, nil
}

// applySource puts the picked node into the list's arguments. Nothing is picked → nothing is set,
// and the tab alone filters nothing. A node with no tab named is in the first tab.
func (s *Service) applySource(ctx context.Context, f Filters, p *sqlcgen.ListItemsParams) error {
	if f.Source.IsZero() {
		return nil
	}
	tab := f.Tab
	if tab == "" {
		t, ok, err := s.tab(ctx, "")
		if err != nil {
			return err
		}
		if ok {
			tab = t.Slug
		}
	}
	src := f.Source
	p.SourceTab = &tab
	p.SourceSection = ptrIfSet(src.Section)
	p.SourceRegion = ptrIfSet(src.Region)
	p.SourceMap = ptrIfSet(src.Map)
	p.SourceBoss = ptrIfSet(src.Boss)
	p.SourceVendor = ptrIfSet(src.Vendor)
	p.SourceQuest = ptrIfSet(src.Quest)
	p.SourceContainer = ptrIfSet(src.Container)
	p.SourceGroup = ptrIfSet(src.Group)
	// A place node is the place and every place inside it (AOC-038): House of Crom's node is its
	// wings' loot.
	if pl := src.Place(); pl != "" {
		expanded, _, err := s.expandPlaces(ctx, []string{pl})
		if err != nil {
			return err
		}
		p.SourcePlaces = expanded
	}
	return nil
}

// treeParams is the tree query's arguments: the list's, WITHOUT its source selection — a node's
// count is what choosing it would leave, so the panel's own pick must not narrow it. ⛔ Every other
// field, from the list's: TestTheTreeTakesEveryListFilterButTheSource fails on one this misses.
func treeParams(p sqlcgen.ListItemsParams, tab string) sqlcgen.ListSourceTreeRowsParams {
	return sqlcgen.ListSourceTreeRowsParams{
		Tab:            tab,
		ItemType:       p.ItemType,
		NameQuery:      p.NameQuery,
		IDQuery:        p.IDQuery,
		PlaceSlugs:     p.PlaceSlugs,
		Region:         p.Region,
		Tier:           p.Tier,
		Unchained:      p.Unchained,
		Pvp:            p.Pvp,
		Rarities:       p.Rarities,
		EquipLocations: p.EquipLocations,
		ArmourWeights:  p.ArmourWeights,
		Classes:        p.Classes,
		IlvlMin:        p.IlvlMin,
		IlvlMax:        p.IlvlMax,
		ReqlvlMin:      p.ReqlvlMin,
		ReqlvlMax:      p.ReqlvlMax,
		Price:          p.Price,
		Currencies:     p.Currencies,
		Sets:           p.Sets,
	}
}

// ErrNoSuchTab is a tab slug no tab has: the tree endpoint's 404, through the central mapping.
var ErrNoSuchTab = fmt.Errorf("%w: no such source tab", httpx.ErrNotFound)

// Tree is a tab's panel under the filters. Its counts ignore the filters' own source pick (see
// treeParams), so every branch says what picking it would leave.
func (s *Service) Tree(ctx context.Context, f Filters) (Tree, error) {
	t, ok, err := s.tab(ctx, f.Tab)
	if err != nil {
		return Tree{}, err
	}
	if !ok {
		return Tree{}, ErrNoSuchTab
	}
	lp, _, err := s.listParams(ctx, f)
	if err != nil {
		return Tree{}, err
	}
	rows, err := s.q.ListSourceTreeRows(ctx, treeParams(lp, t.Slug))
	if err != nil {
		return Tree{}, fmt.Errorf("list source tree rows: %w", err)
	}
	groups, err := s.q.ListAcquisitionGroups(ctx)
	if err != nil {
		return Tree{}, fmt.Errorf("list acquisition groups: %w", err)
	}
	return buildTree(t, rows, groups), nil
}

// level is one step of a row's path through the tree.
type level struct {
	kind, slug, name string
	sort             int32
}

// rowPath is the levels a row takes in a tab: the tab's groups (each skipped when the row has none),
// then the location — its place, the place's wing, then the boss, or else its vendor, quest giver,
// container or boss — the most specific named thing a source row has.
func rowPath(groups []string, r sqlcgen.ListSourceTreeRowsRow) []level {
	var out []level
	for _, g := range groups {
		switch g {
		case "section":
			out = append(out, level{"section", r.SectionSlug, r.SectionName, r.SectionSort})
		case "region":
			if r.RegionSlug != "" {
				out = append(out, level{"region", r.RegionSlug, r.RegionName, r.RegionSort})
			}
		case "map":
			if r.MapSlug != "" {
				out = append(out, level{"map", r.MapSlug, r.MapName, 0})
			}
		}
	}
	if r.PlaceSlug != "" {
		out = append(out, level{"place", r.PlaceSlug, r.PlaceName, 0})
		if r.WingSlug != "" {
			out = append(out, level{"place", r.WingSlug, r.WingName, 0})
		}
	}
	switch {
	case r.BossSlug != "":
		out = append(out, level{"boss", r.BossSlug, r.BossName, 0})
	case r.VendorSlug != "":
		out = append(out, level{"vendor", r.VendorSlug, r.VendorName, 0})
	case r.QuestSlug != "":
		out = append(out, level{"quest", r.QuestSlug, r.QuestName, 0})
	case r.ContainerSlug != "":
		out = append(out, level{"container", r.ContainerSlug, r.ContainerName, 0})
	}
	return out
}

// segment is a level's part of a `source` value (SourceNode.String's spelling).
var segment = map[string]string{
	"section": "s", "region": "r", "map": "m", "place": "p",
	"boss": "b", "vendor": "v", "quest": "q", "container": "c",
}

func buildTree(t SourceTab, rows []sqlcgen.ListSourceTreeRowsRow, groups []sqlcgen.ListAcquisitionGroupsRow) Tree {
	root := &TreeNode{index: map[string]*TreeNode{}}
	all := map[int32]struct{}{}
	for _, r := range rows {
		if r.Matches {
			all[r.ItemID] = struct{}{}
		}
		n := root
		for _, l := range rowPath(t.Groups, r) {
			key := l.kind + ":" + l.slug
			c := n.index[key]
			if c == nil {
				src := segment[l.kind] + ":" + l.slug
				if n.Source != "" {
					src = n.Source + "." + src
				}
				c = &TreeNode{Kind: l.kind, Slug: l.slug, Name: l.name, Source: src, sort: l.sort,
					items: map[int32]struct{}{}, by: map[string]map[int32]struct{}{}, index: map[string]*TreeNode{}}
				n.index[key] = c
				n.Children = append(n.Children, c)
			}
			// Every row makes the branch exist; only a matching one counts in it (a 0 is listed).
			if !r.Matches {
				n = c
				continue
			}
			c.items[r.ItemID] = struct{}{}
			if r.GroupSlug != "" {
				if c.by[r.GroupSlug] == nil {
					c.by[r.GroupSlug] = map[int32]struct{}{}
				}
				c.by[r.GroupSlug][r.ItemID] = struct{}{}
			}
			n = c
		}
	}
	out := Tree{Tab: t, Total: int64(len(all)), Nodes: root.Children}
	if out.Nodes == nil {
		out.Nodes = []*TreeNode{}
	}
	var finish func(ns []*TreeNode)
	finish = func(ns []*TreeNode) {
		// Sections and regions in their own order, everything else by name: the data has no order
		// for a map, a place or a vendor, and alphabetical is the one a reader can predict.
		sort.SliceStable(ns, func(i, j int) bool {
			if ns[i].sort != ns[j].sort {
				return ns[i].sort < ns[j].sort
			}
			if ns[i].Name != ns[j].Name {
				return ns[i].Name < ns[j].Name
			}
			return ns[i].Slug < ns[j].Slug
		})
		for _, n := range ns {
			n.Count = int64(len(n.items))
			for _, g := range groups {
				if c := len(n.by[g.Slug]); c > 0 {
					n.Groups = append(n.Groups, GroupCount{Slug: g.Slug, Name: g.Name, Count: int64(c)})
				}
			}
			if len(n.Children) == 0 {
				out.EndPoints++
			}
			finish(n.Children)
		}
	}
	finish(out.Nodes)
	return out
}
