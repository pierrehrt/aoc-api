package items

import (
	"context"
	"errors"
	"net/http"
	"reflect"
	"strings"
	"testing"

	"github.com/pierrehrt/aoc-api/internal/db/sqlcgen"
	"github.com/pierrehrt/aoc-api/internal/httpx"
)

// AOC-050: the Armory's source panel. Fixture names are obviously fake (CLAUDE.md STEP ZERO); the
// real sections and tabs are asserted against the database in internal/db.

func TestASourcePathRoundTrips(t *testing.T) {
	for _, raw := range []string{
		"s:test-tier",
		"s:test-tier.p:test-raid.b:test-boss-alpha",
		"r:test-region.m:test-map.p:test-complex.p:test-wing",
		"s:test-faction.m:test-map.v:test-vendor",
		"s:test-cat.q:test-quest",
		"s:test-cat.c:test-chest",
	} {
		s, err := ParseSource(raw)
		if err != nil {
			t.Errorf("%q: %v", raw, err)
			continue
		}
		if got := s.String(); got != raw {
			t.Errorf("%q came back as %q", raw, got)
		}
	}
	s, _ := ParseSource("r:test-region.p:test-complex.p:test-wing")
	if s.Place() != "test-wing" {
		t.Errorf("Place() = %q, want the most specific, test-wing", s.Place())
	}
	if z, _ := ParseSource("  "); !z.IsZero() {
		t.Error("a blank source picked something")
	}
}

func TestAMalformedSourceIsRefused(t *testing.T) {
	for _, raw := range []string{
		"test-tier",           // no kind
		"x:test-tier",         // unknown kind
		"s:Test-Tier",         // not a slug
		"s:a.s:b",             // a level twice
		"s:a..p:b",            // an empty segment
		"p:a.p:b.p:c",         // more than a location and its wing
		"s:a.p:b%2Cc",         // not a slug
		"s:",                  // no slug
		"s:test tier",         // a space
		"s:test-tier.b:-boss", // a slug cannot start with a dash
	} {
		if _, err := ParseSource(raw); !errors.Is(err, httpx.ErrInvalid) {
			t.Errorf("%q: err = %v, want ErrInvalid", raw, err)
		}
	}
}

func TestTheTabAndTheHalfOfALocationParseAndRefuse(t *testing.T) {
	for _, tc := range []struct {
		query string
		ok    bool
	}{
		{"tab=test-tab&source=s:test-tier&get=test-group", true},
		{"tab=Test%20Tab", false},
		{"get=Not%20A%20Group", false},
		{"source=nope", false},
	} {
		_, err := ParseFilters(mustRequest(t, "/v1/items?"+tc.query))
		if (err == nil) != tc.ok {
			t.Errorf("%s: err = %v, want ok=%v", tc.query, err, tc.ok)
		}
	}
}

func mustRequest(t *testing.T, target string) *http.Request {
	t.Helper()
	r, err := http.NewRequestWithContext(context.Background(), http.MethodGet, target, nil)
	if err != nil {
		t.Fatal(err)
	}
	return r
}

// ⛔ The tree counts under every list filter EXCEPT its own pick: a filter the rows honour and the
// panel's counts ignore would make a branch promise a number the list does not give.
func TestTheTreeTakesEveryListFilterButTheSource(t *testing.T) {
	var lp sqlcgen.ListItemsParams
	fillDistinct(t, &lp, "SortBy", "PageSize", "PageOffset")
	tp := treeParams(lp, "test-tab")
	if tp.Tab != "test-tab" {
		t.Errorf("Tab = %q", tp.Tab)
	}
	lv, tv := reflect.ValueOf(lp), reflect.ValueOf(tp)
	for i := 0; i < lv.NumField(); i++ {
		name := lv.Type().Field(i).Name
		got := tv.FieldByName(name)
		switch {
		case name == "SortBy" || name == "PageSize" || name == "PageOffset":
			continue
		case strings.HasPrefix(name, "Source"):
			if !got.IsValid() {
				t.Errorf("the tree query has no %s argument; the CTE is not the shared one", name)
			} else if !got.IsZero() {
				t.Errorf("treeParams carries %s = %v: the panel's own pick would narrow its counts", name, got.Interface())
			}
		case !got.IsValid():
			t.Errorf("the list filters on %s and the tree query has no such argument", name)
		case !reflect.DeepEqual(got.Interface(), lv.Field(i).Interface()):
			t.Errorf("treeParams does not carry %s: list %v, tree %v", name, lv.Field(i).Interface(), got.Interface())
		}
	}
}

func treeRow(item int32, section string, f func(*sqlcgen.ListSourceTreeRowsRow)) sqlcgen.ListSourceTreeRowsRow {
	r := sqlcgen.ListSourceTreeRowsRow{ItemID: item, SectionSlug: section, SectionName: "Test " + section, GroupSlug: "drop", Matches: true}
	if f != nil {
		f(&r)
	}
	return r
}

var testGroups = []sqlcgen.ListAcquisitionGroupsRow{{Slug: "drop", Name: "test drops"}, {Slug: "vendor", Name: "test vendor"}}

func TestTheTreeCountsDistinctItemsPerBranch(t *testing.T) {
	tab := SourceTab{Slug: "test-tab", Groups: []string{"section"}}
	rows := []sqlcgen.ListSourceTreeRowsRow{
		// Item 1 drops from two bosses of one raid: one item for the raid, one for each boss.
		treeRow(1, "test-tier-b", func(r *sqlcgen.ListSourceTreeRowsRow) {
			r.SectionSort = 20
			r.PlaceSlug, r.PlaceName, r.BossSlug, r.BossName = "test-raid", "Test Raid", "test-boss-alpha", "Test Boss Alpha"
		}),
		treeRow(1, "test-tier-b", func(r *sqlcgen.ListSourceTreeRowsRow) {
			r.SectionSort = 20
			r.PlaceSlug, r.PlaceName, r.BossSlug, r.BossName = "test-raid", "Test Raid", "test-boss-beta", "Test Boss Beta"
		}),
		// Item 2 is sold in the same raid: the raid's quest / vendor half.
		treeRow(2, "test-tier-b", func(r *sqlcgen.ListSourceTreeRowsRow) {
			r.SectionSort, r.GroupSlug = 20, "vendor"
			r.PlaceSlug, r.PlaceName = "test-raid", "Test Raid"
		}),
		// Item 3 is in a section with no location at all: it counts at the section, no deeper.
		treeRow(3, "test-tier-a", func(r *sqlcgen.ListSourceTreeRowsRow) { r.SectionSort = 10 }),
	}
	tr := buildTree(tab, rows, testGroups)

	if tr.Total != 3 {
		t.Errorf("Total = %d, want 3 distinct items", tr.Total)
	}
	if len(tr.Nodes) != 2 || tr.Nodes[0].Slug != "test-tier-a" || tr.Nodes[1].Slug != "test-tier-b" {
		t.Fatalf("sections = %v, want test-tier-a then test-tier-b, by sort order", names(tr.Nodes))
	}
	b := tr.Nodes[1]
	if b.Count != 2 || b.Source != "s:test-tier-b" {
		t.Errorf("tier b: count %d source %q, want 2 and s:test-tier-b", b.Count, b.Source)
	}
	raid := b.Children[0]
	if raid.Count != 2 || raid.Source != "s:test-tier-b.p:test-raid" {
		t.Errorf("raid: count %d source %q", raid.Count, raid.Source)
	}
	if want := []GroupCount{{"drop", "test drops", 1}, {"vendor", "test vendor", 1}}; !reflect.DeepEqual(raid.Groups, want) {
		t.Errorf("raid groups = %v, want %v", raid.Groups, want)
	}
	if len(raid.Children) != 2 || raid.Children[0].Name != "Test Boss Alpha" || raid.Children[1].Source != "s:test-tier-b.p:test-raid.b:test-boss-beta" {
		t.Errorf("bosses = %v", names(raid.Children))
	}
	if a := tr.Nodes[0]; a.Count != 1 || len(a.Children) != 0 {
		t.Errorf("a section with no location: count %d, %d children", a.Count, len(a.Children))
	}
	// End points: tier a, boss alpha, boss beta. The raid has children; item 2's vendor half is a
	// group of the raid, not a branch.
	if tr.EndPoints != 3 {
		t.Errorf("EndPoints = %d, want 3", tr.EndPoints)
	}
}

// A tab's groups decide its levels, and a level a row does not have is skipped, never "Unknown".
func TestATabsGroupsDecideItsLevels(t *testing.T) {
	tab := SourceTab{Slug: "test-tab", Groups: []string{"region", "map"}}
	rows := []sqlcgen.ListSourceTreeRowsRow{
		treeRow(1, "test-section", func(r *sqlcgen.ListSourceTreeRowsRow) {
			r.RegionSlug, r.RegionName, r.RegionSort = "test-region", "Test Region", 10
			r.MapSlug, r.MapName = "test-map", "Test Map"
			r.PlaceSlug, r.PlaceName, r.WingSlug, r.WingName = "test-complex", "Test Complex", "test-wing", "Test Wing"
		}),
		// No map: the vendor hangs off the region.
		treeRow(2, "test-section", func(r *sqlcgen.ListSourceTreeRowsRow) {
			r.RegionSlug, r.RegionName, r.RegionSort = "test-region", "Test Region", 10
			r.VendorSlug, r.VendorName = "test-vendor", "Test Vendor"
		}),
	}
	tr := buildTree(tab, rows, testGroups)
	if len(tr.Nodes) != 1 || tr.Nodes[0].Kind != "region" {
		t.Fatalf("top level = %v, want the one region (the section is not one of this tab's levels)", names(tr.Nodes))
	}
	reg := tr.Nodes[0]
	if len(reg.Children) != 2 {
		t.Fatalf("under the region: %v, want the map and the vendor", names(reg.Children))
	}
	m := reg.Children[0]
	if m.Kind != "map" || m.Children[0].Source != "r:test-region.m:test-map.p:test-complex" ||
		m.Children[0].Children[0].Source != "r:test-region.m:test-map.p:test-complex.p:test-wing" {
		t.Errorf("map branch = %+v", m)
	}
	if v := reg.Children[1]; v.Kind != "vendor" || v.Source != "r:test-region.v:test-vendor" {
		t.Errorf("vendor = %+v", v)
	}
}

func names(ns []*TreeNode) []string {
	var out []string
	for _, n := range ns {
		out = append(out, n.Kind+":"+n.Name)
	}
	return out
}

func treeQ() *fakeQ {
	q := sharedItem()
	q.tabs = []sqlcgen.ListSourceTabsRow{
		{Slug: "test-first", Name: "First", Groups: []string{"section"}},
		{Slug: "test-second", Name: "Second", Groups: []string{"region"}},
	}
	q.acqGroups = testGroups
	return q
}

// The tab alone filters nothing (Pierre, 2026-10-02); a node does, in the named tab or the first.
func TestATabAloneFiltersNothingAndANodeDefaultsToTheFirstTab(t *testing.T) {
	q := treeQ()
	if _, err := NewService(q).List(context.Background(), Filters{Tab: "test-second"}); err != nil {
		t.Fatal(err)
	}
	if p := q.firstArgs(); p.SourceTab != nil {
		t.Errorf("a tab alone reached SQL as source_tab=%q", *p.SourceTab)
	}

	q = treeQ()
	q.inside = map[string][]string{"test-complex": {"test-wing"}}
	src, _ := ParseSource("s:test-cat.p:test-complex.b:test-boss-alpha")
	src.Group = "vendor"
	if _, err := NewService(q).List(context.Background(), Filters{Source: src}); err != nil {
		t.Fatal(err)
	}
	p := q.firstArgs()
	if p.SourceTab == nil || *p.SourceTab != "test-first" {
		t.Errorf("source_tab = %v, want the first tab", p.SourceTab)
	}
	if deref(p.SourceSection) != "test-cat" || deref(p.SourceBoss) != "test-boss-alpha" || deref(p.SourceGroup) != "vendor" {
		t.Errorf("section/boss/group = %v/%v/%v", p.SourceSection, p.SourceBoss, p.SourceGroup)
	}
	// A place node is the place and everything inside it (AOC-038).
	if !reflect.DeepEqual(p.SourcePlaces, []string{"test-complex", "test-wing"}) {
		t.Errorf("source_places = %v", p.SourcePlaces)
	}
}

func TestAnUnknownTabIsNotFound(t *testing.T) {
	_, err := NewService(treeQ()).Tree(context.Background(), Filters{Tab: "test-nope"})
	if !errors.Is(err, httpx.ErrNotFound) {
		t.Errorf("err = %v, want ErrNotFound", err)
	}
}

// The tree ignores the request's own pick: its counts are what picking would leave.
func TestTheTreeIsCountedWithoutItsOwnPick(t *testing.T) {
	q := treeQ()
	src, _ := ParseSource("s:test-cat")
	if _, err := NewService(q).Tree(context.Background(), Filters{Tab: "test-second", Source: src, Rarities: []string{"epic"}}); err != nil {
		t.Fatal(err)
	}
	if len(q.treeArgs) != 1 {
		t.Fatalf("tree query called %d times", len(q.treeArgs))
	}
	a := q.treeArgs[0]
	if a.Tab != "test-second" || a.SourceTab != nil || a.SourceSection != nil {
		t.Errorf("tree args: tab %q, source_tab %v, source_section %v", a.Tab, a.SourceTab, a.SourceSection)
	}
	if !reflect.DeepEqual(a.Rarities, []string{"epic"}) {
		t.Errorf("the tree lost the rarity filter: %v", a.Rarities)
	}
}

// ⭐ A branch the filters empty is listed with 0, never hidden — the rail's rule (AOC-049).
func TestABranchTheFiltersEmptyIsListedWithZero(t *testing.T) {
	tab := SourceTab{Slug: "test-tab", Groups: []string{"section"}}
	rows := []sqlcgen.ListSourceTreeRowsRow{
		treeRow(1, "test-kept", nil),
		treeRow(2, "test-emptied", func(r *sqlcgen.ListSourceTreeRowsRow) {
			r.Matches = false
			r.PlaceSlug, r.PlaceName = "test-raid", "Test Raid"
		}),
	}
	tr := buildTree(tab, rows, testGroups)
	if tr.Total != 1 {
		t.Errorf("Total = %d, want only the matching item", tr.Total)
	}
	var emptied *TreeNode
	for _, n := range tr.Nodes {
		if n.Slug == "test-emptied" {
			emptied = n
		}
	}
	if emptied == nil {
		t.Fatalf("the emptied section is hidden: %v", names(tr.Nodes))
	}
	if emptied.Count != 0 || len(emptied.Children) != 1 || emptied.Children[0].Count != 0 {
		t.Errorf("the emptied branch reads %+v; want 0, its raid listed at 0", emptied)
	}
	// Its half is listed too, at 0 (AOC-068): the panel draws a location under each half it has.
	if want := []GroupCount{{"drop", "test drops", 0}}; !reflect.DeepEqual(emptied.Children[0].Groups, want) {
		t.Errorf("the emptied raid's halves = %v, want %v", emptied.Children[0].Groups, want)
	}
}
