package pages

import (
	"strings"
	"testing"

	"github.com/pierrehrt/aoc-api/internal/builds"
	"github.com/pierrehrt/aoc-api/internal/items"
)

// AOC-068: the panel's drawing rules, on a fake tree (CLAUDE.md STEP ZERO: no real names).

func fakeTree() ([]items.SourceTab, items.Tree) {
	tabs := []items.SourceTab{{Slug: "test-first", Name: "First"}, {Slug: "test-second", Name: "Second"}}
	boss := func(slug, name string, drop, vendor int64) *items.TreeNode {
		n := &items.TreeNode{Kind: "boss", Slug: slug, Name: name, Source: "s:test-tier.p:test-raid.b:" + slug, Count: drop + vendor}
		if drop > 0 {
			n.Groups = append(n.Groups, items.GroupCount{Slug: "drop", Name: "test drops", Count: drop})
		}
		if vendor > 0 {
			n.Groups = append(n.Groups, items.GroupCount{Slug: "vendor", Name: "test vendor", Count: vendor})
		}
		return n
	}
	raid := &items.TreeNode{Kind: "place", Slug: "test-raid", Name: "Test Raid", Source: "s:test-tier.p:test-raid", Count: 5, Coords: "10,20",
		Children: []*items.TreeNode{boss("test-boss-alpha", "Test Boss Alpha", 2, 0), boss("test-boss-beta", "Test Boss Beta", 1, 2)}}
	tier := &items.TreeNode{Kind: "section", Slug: "test-tier", Name: "Test Tier", Source: "s:test-tier", Count: 5, Children: []*items.TreeNode{raid}}
	other := &items.TreeNode{Kind: "section", Slug: "test-other", Name: "Test Other", Source: "s:test-other", Count: 0}
	return tabs, items.Tree{Tab: items.SourceTab{Slug: "test-first", Name: "First", LevelsNote: "test levels"},
		EndPoints: 3, Nodes: []*items.TreeNode{tier, other},
		Halves: []items.Term{{Slug: "drop", Name: "test drops"}, {Slug: "vendor", Name: "test vendor"}}}
}

func pickOf(t *testing.T, raw, group string) items.SourceNode {
	t.Helper()
	s, err := items.ParseSource(raw)
	if err != nil {
		t.Fatal(err)
	}
	s.Group = group
	return s
}

func here(g items.Filters) string { return armoryURL(g, 1, builds.Build{}) }

func TestTheTabsKeepTheFiltersAndDropThePick(t *testing.T) {
	tabs, tree := fakeTree()
	f := items.Filters{Rarities: []string{"test-rarity"}, Source: pickOf(t, "s:test-tier", "")}
	src, _ := buildSources(f, tabs, tree, here)
	if len(src.Tabs) != 2 || !src.Tabs[0].Current || src.Tabs[1].Current {
		t.Fatalf("tabs = %+v", src.Tabs)
	}
	if src.Tabs[0].URL != "/armory?rarity=test-rarity" {
		t.Errorf("the first tab is the default, so it is not in its URL: %q", src.Tabs[0].URL)
	}
	if src.Tabs[1].URL != "/armory?rarity=test-rarity&tab=test-second" {
		t.Errorf("the second tab's URL = %q, want the filters kept and the pick dropped", src.Tabs[1].URL)
	}
	if src.Title != "Source · First" || src.LevelsNote != "test levels" || src.EndPoints != 3 {
		t.Errorf("head = %q %q %d", src.Title, src.LevelsNote, src.EndPoints)
	}
}

func TestNothingPickedShowsTheTopLevelClosed(t *testing.T) {
	tabs, tree := fakeTree()
	src, label := buildSources(items.Filters{}, tabs, tree, here)
	if label != "" || src.Picked || src.Selected != "" {
		t.Errorf("nothing picked: label %q picked %v selected %q", label, src.Picked, src.Selected)
	}
	if len(src.Rows) != 2 || src.Rows[0].Caret != "▸" || src.Rows[1].Caret != "" || src.Rows[1].Count != 0 {
		t.Errorf("rows = %+v; want the two sections, the first closed, the second (0) listed with nothing under it", src.Rows)
	}
	if src.Rows[0].URL != "/armory?source=s:test-tier" || src.Rows[0].Pad != 8 {
		t.Errorf("first row: %q pad %d", src.Rows[0].URL, src.Rows[0].Pad)
	}
}

func TestAPickedRaidOpensItsWayDownAndSplitsItsBossesIntoHalves(t *testing.T) {
	tabs, tree := fakeTree()
	src, label := buildSources(items.Filters{Source: pickOf(t, "s:test-tier.p:test-raid", "")}, tabs, tree, here)
	var got []string
	for _, r := range src.Rows {
		kind := "row"
		if r.Header {
			kind = "head"
		}
		got = append(got, kind+":"+r.Label+":"+r.Caret)
	}
	want := []string{
		"row:Test Tier:▾", "row:Test Raid:▾",
		"head:test drops:", "row:Test Boss Alpha:", "row:Test Boss Beta:",
		"head:test vendor:", "row:Test Boss Beta:",
		"row:Test Other:",
	}
	if strings.Join(got, " | ") != strings.Join(want, " | ") {
		t.Fatalf("rows:\n got %s\nwant %s", strings.Join(got, " | "), strings.Join(want, " | "))
	}
	raid := src.Rows[1]
	if !raid.Selected || raid.URL != "/armory?source=s:test-tier" {
		t.Errorf("the picked raid: selected %v, URL %q (picking it again closes it, to the tier)", raid.Selected, raid.URL)
	}
	beta := src.Rows[6]
	if beta.Count != 2 || beta.URL != "/armory?get=vendor&source=s:test-tier.p:test-raid.b:test-boss-beta" || beta.Pad != 8+15*3 {
		t.Errorf("Beta under its vendor half: count %d URL %q pad %d", beta.Count, beta.URL, beta.Pad)
	}
	if label != "source: Test Raid" || src.Selected != "Test Tier › Test Raid" {
		t.Errorf("label %q, selected %q", label, src.Selected)
	}
	// One point: the box says where (Pierre). The tier has none: it is many places.
	if src.Coords != "10,20" {
		t.Errorf("coords = %q, want the raid's", src.Coords)
	}
	if tier, _ := buildSources(items.Filters{Source: pickOf(t, "s:test-tier", "")}, tabs, tree, here); tier.Coords != "" {
		t.Errorf("the tier's coords = %q, want none", tier.Coords)
	}
}

func TestAPickedHalfIsTheSelectedRowAndThePillSaysWhichHalf(t *testing.T) {
	tabs, tree := fakeTree()
	src, label := buildSources(items.Filters{Source: pickOf(t, "s:test-tier.p:test-raid.b:test-boss-beta", "vendor")}, tabs, tree, here)
	var selected []string
	for _, r := range src.Rows {
		if r.Selected {
			selected = append(selected, r.Label+":"+r.URL)
		}
	}
	if len(selected) != 1 || !strings.Contains(selected[0], "get=vendor") {
		t.Errorf("selected rows = %v, want only Beta's vendor half", selected)
	}
	if label != "source: Test Boss Beta · test vendor" {
		t.Errorf("label = %q", label)
	}
}

// A path no branch has (a stale link): the pill names it as it was asked, and the tree stays closed.
func TestAPickNoBranchHasStillGetsAPill(t *testing.T) {
	tabs, tree := fakeTree()
	src, label := buildSources(items.Filters{Source: pickOf(t, "s:test-gone", "")}, tabs, tree, here)
	if label != "source: s:test-gone" || src.Selected != "" || !src.Picked {
		t.Errorf("label %q selected %q picked %v", label, src.Selected, src.Picked)
	}
	for _, r := range src.Rows {
		if r.Open || r.Selected {
			t.Errorf("row %q open/selected for a pick it is not", r.Label)
		}
	}
}

// The pill's × drops the pick and keeps the tab; "clear all" keeps the tab too (it is not a filter).
func TestThePickRidesInTheFormAndItsPillKeepsTheTab(t *testing.T) {
	f := items.Filters{Tab: "test-second", Source: pickOf(t, "r:test-region", "drop"), Sort: items.SortILvl}
	rail, chips, clearAll := buildRail(f, nil, here, "source: Test Region")
	hidden := map[string]string{}
	for _, h := range rail.Hidden {
		hidden[h.Name] = h.Value
	}
	if hidden["tab"] != "test-second" || hidden["source"] != "r:test-region" || hidden["get"] != "drop" {
		t.Errorf("hidden = %v", hidden)
	}
	if len(chips) != 1 || chips[0].Label != "source: Test Region" || chips[0].URL != "/armory?tab=test-second" {
		t.Errorf("chips = %+v", chips)
	}
	if clearAll != "/armory?tab=test-second" {
		t.Errorf("clear all = %q, want the tab kept", clearAll)
	}
}
