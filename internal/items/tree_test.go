package items

import (
	"context"
	"errors"
	"net/http"
	"reflect"
	"strconv"
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
		"r:test-region.m:test-map.p:test-complex.p:test-hall.p:test-vault",
		"s:test-cat.r:-.p:-.v:test-vendor",
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
		"test-tier",        // no kind
		"x:test-tier",      // unknown kind
		"s:Test-Tier",      // not a slug
		"s:a.s:b",          // a level twice
		"s:a..p:b",         // an empty segment
		"s:-",              // a section is never absent
		"b:-",              // nor a boss
		"p:-.p:test-place", // "no place" stands alone
		"p:test-place.p:-",
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

// One depth rule (verify round 3, F11): the tree draws a row's whole ancestry, however deep, and
// the parser reads any chain it draws — the hierarchy is the only bound.
func TestADeepChainIsDrawnWholeAndItsPathParses(t *testing.T) {
	const depth = 40
	h := map[string]placeInfo{}
	for i := 0; i < depth; i++ {
		info := placeInfo{name: "Test Place " + strconv.Itoa(i)}
		if i > 0 {
			info.parent = "test-place-" + strconv.Itoa(i-1)
		}
		h["test-place-"+strconv.Itoa(i)] = info
	}
	deepest := "test-place-" + strconv.Itoa(depth-1)
	levels := rowPath([]string{"section"}, sqlcgen.ListSourceTreeRowsRow{SectionSlug: "test-tier", PlaceSlug: deepest, PlaceName: "Test Deep"}, h)
	if got := len(levels) - 1; got != depth {
		t.Fatalf("the chain has %d places, want all %d", got, depth)
	}
	if levels[1].slug != "test-place-0" {
		t.Errorf("the chain starts at %q, want the top place", levels[1].slug)
	}
	var segs []string
	for _, l := range levels {
		segs = append(segs, segment[l.kind]+":"+l.slug)
	}
	src, err := ParseSource(strings.Join(segs, "."))
	if err != nil {
		t.Fatalf("the tree's own path for a place %d levels down is refused: %v", depth, err)
	}
	if len(src.Places) != depth {
		t.Errorf("parsed %d places, want %d", len(src.Places), depth)
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
	tr := buildTree(tab, rows, testGroups, nil)

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

// A tab's groups decide its levels. A level a row does not have is not drawn ("Unknown" is never
// shown), but it IS in the path as "-": the branch below it holds only the rows that lack it
// (AOC-050 verify round 1, F1). A place is drawn under every place above it (F2).
func TestATabsGroupsDecideItsLevels(t *testing.T) {
	tab := SourceTab{Slug: "test-tab", Groups: []string{"region", "map"}}
	h := map[string]placeInfo{
		"test-complex": {name: "Test Complex"},
		"test-hall":    {name: "Test Hall", parent: "test-complex"},
		"test-vault":   {name: "Test Vault", parent: "test-hall"},
	}
	rows := []sqlcgen.ListSourceTreeRowsRow{
		treeRow(1, "test-section", func(r *sqlcgen.ListSourceTreeRowsRow) {
			r.RegionSlug, r.RegionName, r.RegionSort = "test-region", "Test Region", 10
			r.MapSlug, r.MapName = "test-map", "Test Map"
			r.PlaceSlug, r.PlaceName = "test-vault", "Test Vault" // two places down
		}),
		// No map: the vendor hangs off the region, and its path says "no map".
		treeRow(2, "test-section", func(r *sqlcgen.ListSourceTreeRowsRow) {
			r.RegionSlug, r.RegionName, r.RegionSort = "test-region", "Test Region", 10
			r.VendorSlug, r.VendorName = "test-vendor", "Test Vendor"
		}),
	}
	tr := buildTree(tab, rows, testGroups, h)
	if len(tr.Nodes) != 1 || tr.Nodes[0].Kind != "region" {
		t.Fatalf("top level = %v, want the one region (the section is not one of this tab's levels)", names(tr.Nodes))
	}
	reg := tr.Nodes[0]
	if len(reg.Children) != 2 {
		t.Fatalf("under the region: %v, want the map and the vendor", names(reg.Children))
	}
	m := reg.Children[0]
	complex := m.Children[0]
	if m.Kind != "map" || complex.Source != "r:test-region.m:test-map.p:test-complex" ||
		complex.Children[0].Source != "r:test-region.m:test-map.p:test-complex.p:test-hall" ||
		complex.Children[0].Children[0].Source != "r:test-region.m:test-map.p:test-complex.p:test-hall.p:test-vault" {
		t.Errorf("map branch = %+v", m)
	}
	if v := reg.Children[1]; v.Kind != "vendor" || v.Source != "r:test-region.m:-.p:-.v:test-vendor" {
		t.Errorf("vendor = %+v, want its path to say no map and no place", v)
	}
	if tr.Attribution != Attribution || len(tr.Halves) != 2 {
		t.Errorf("attribution %q, halves %v", tr.Attribution, tr.Halves)
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
	// With a boss after it, the place is exact: the tree counts those rows under the place itself
	// (verify round 1, F1/F2).
	if !reflect.DeepEqual(p.SourcePlaces, []string{"test-complex"}) {
		t.Errorf("source_places = %v, want the place alone when a boss follows", p.SourcePlaces)
	}

	// A path that ends at the place is the place and everything inside it (AOC-038).
	q = treeQ()
	q.inside = map[string][]string{"test-complex": {"test-wing"}}
	src, _ = ParseSource("s:test-cat.p:test-complex")
	if _, err := NewService(q).List(context.Background(), Filters{Source: src}); err != nil {
		t.Fatal(err)
	}
	if got := q.firstArgs().SourcePlaces; !reflect.DeepEqual(got, []string{"test-complex", "test-wing"}) {
		t.Errorf("source_places = %v, want the place and its wing", got)
	}
}

// A chain whose places are not each other's parents names nothing (verify round 1, F3), and "p:-"
// is "no place"; a half without a source is refused (F6).
func TestAPlaceChainIsCheckedAndAHalfNeedsASource(t *testing.T) {
	for raw, want := range map[string][]string{
		"s:test-cat.p:test-complex.p:test-wing": {"test-wing"},
		"s:test-cat.p:test-wing.p:test-complex": nothing,
		"s:test-cat.p:test-nope.p:test-wing":    nothing,
		"s:test-cat.p:-.b:test-boss-alpha":      {"-"},
	} {
		q := treeQ()
		q.inside = map[string][]string{"test-complex": {"test-wing"}}
		src, err := ParseSource(raw)
		if err != nil {
			t.Fatalf("%s: %v", raw, err)
		}
		if _, err := NewService(q).List(context.Background(), Filters{Source: src}); err != nil {
			t.Fatal(err)
		}
		if got := q.firstArgs().SourcePlaces; !reflect.DeepEqual(got, want) {
			t.Errorf("%s: source_places = %q, want %q", raw, got, want)
		}
	}
	_, err := NewService(treeQ()).List(context.Background(), Filters{Source: SourceNode{Group: "drop"}})
	if !errors.Is(err, httpx.ErrInvalid) {
		t.Errorf("get with no source: err = %v, want ErrInvalid", err)
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
	tr := buildTree(tab, rows, testGroups, nil)
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
	// Its half is listed too, at 0 (verify round 1, F4): a half the filters empty is not hidden.
	if want := []GroupCount{{"drop", "test drops", 0}}; !reflect.DeepEqual(emptied.Children[0].Groups, want) {
		t.Errorf("the emptied raid's halves = %v, want %v", emptied.Children[0].Groups, want)
	}
}

// A branch is one point when every row of it has the same coordinates (Pierre, 2026-10-02): none,
// a row without any, or two different ones, and the branch has no Coords.
func TestABranchHasCoordinatesOnlyWhenItIsOnePoint(t *testing.T) {
	tab := SourceTab{Slug: "test-tab", Groups: []string{"section"}}
	at := func(item int32, place, coords string) sqlcgen.ListSourceTreeRowsRow {
		return treeRow(item, "test-tier", func(r *sqlcgen.ListSourceTreeRowsRow) {
			r.PlaceSlug, r.PlaceName, r.Coords = place, "Test "+place, coords
		})
	}
	tr := buildTree(tab, []sqlcgen.ListSourceTreeRowsRow{
		at(1, "test-one", "1,2"), at(2, "test-one", "1,2"),
		at(3, "test-two", "1,2"), at(4, "test-two", "3,4"),
		at(5, "test-part", "5,6"), at(6, "test-part", ""),
		at(7, "test-none", ""),
	}, testGroups, nil)
	got := map[string]string{}
	for _, n := range tr.Nodes[0].Children {
		got[n.Slug] = n.Coords
	}
	want := map[string]string{"test-one": "1,2", "test-two": "", "test-part": "", "test-none": ""}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("coords = %v, want %v", got, want)
	}
	if tr.Nodes[0].Coords != "" {
		t.Errorf("the tier, many points, has coords %q", tr.Nodes[0].Coords)
	}
}
