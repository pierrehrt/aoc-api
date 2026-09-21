package items

import (
	"context"
	"errors"
	"testing"

	"github.com/jackc/pgx/v5"

	"github.com/pierrehrt/aoc-api/internal/db/sqlcgen"
	"github.com/pierrehrt/aoc-api/internal/httpx"
)

// fakeQ answers from fixtures, so the collapsing rule is tested as LOGIC rather than as SQL. The
// database-backed half lives in internal/db; what matters here is the decision the service makes.
//
// Item names are obviously fake on purpose: a fixture that names a real boss with invented
// mechanics leaks into screenshots and ends up believed (CLAUDE.md STEP ZERO).
type fakeQ struct {
	items  []sqlcgen.ListItemsRow
	places []sqlcgen.ListItemPlacesRow
	// args records EVERY call, not the last: an empty page makes a second "total probe" call, and
	// keeping only the last one recorded the probe's parameters instead of the request's.
	args  []sqlcgen.ListItemsParams
	calls int
}

func (f *fakeQ) ListItems(_ context.Context, a sqlcgen.ListItemsParams) ([]sqlcgen.ListItemsRow, error) {
	f.args = append(f.args, a)
	f.calls++
	lo := int(a.PageOffset)
	if lo >= len(f.items) {
		return nil, nil
	}
	hi := lo + int(a.PageSize)
	if hi > len(f.items) {
		hi = len(f.items)
	}
	return f.items[lo:hi], nil
}

func (f *fakeQ) ListItemPlaces(context.Context, []int32) ([]sqlcgen.ListItemPlacesRow, error) {
	return f.places, nil
}

func (f *fakeQ) GetItemBySlug(context.Context, string) (int32, error) { return 0, pgx.ErrNoRows }
func (f *fakeQ) GetItem(context.Context, int32) (sqlcgen.GetItemRow, error) {
	return sqlcgen.GetItemRow{}, pgx.ErrNoRows
}
func (f *fakeQ) ListItemStats(context.Context, int32) ([]sqlcgen.ListItemStatsRow, error) {
	return nil, nil
}
func (f *fakeQ) ListItemSources(context.Context, int32) ([]sqlcgen.ListItemSourcesRow, error) {
	return nil, nil
}
func (f *fakeQ) ListItemCosts(context.Context, []int64) ([]sqlcgen.ListItemCostsRow, error) {
	return nil, nil
}
func (f *fakeQ) ListItemEquipLocations(context.Context, int32) ([]sqlcgen.EquipLocation, error) {
	return nil, nil
}
func (f *fakeQ) ListItemClasses(context.Context, int32) ([]sqlcgen.ListItemClassesRow, error) {
	return nil, nil
}

// firstArgs is what the REQUEST asked for, before any follow-up probe.
func (f *fakeQ) firstArgs() sqlcgen.ListItemsParams {
	if len(f.args) == 0 {
		return sqlcgen.ListItemsParams{}
	}
	return f.args[0]
}

// sharedItem is one item that drops in three places — the shape Pierre's rule is about.
func sharedItem() *fakeQ {
	return &fakeQ{
		items: []sqlcgen.ListItemsRow{{
			ItemID: 1, Slug: "test-relic-alpha", Name: "Test Relic Alpha",
			Rarity: "epic", Confidence: "unconfirmed", TotalCount: 1,
		}},
		places: []sqlcgen.ListItemPlacesRow{
			{ItemID: 1, PlaceSlug: "test-crypt", PlaceName: "Test Crypt"},
			{ItemID: 1, PlaceSlug: "test-cave", PlaceName: "Test Cave"},
			{ItemID: 1, PlaceSlug: "test-lair", PlaceName: "Test Lair"},
		},
	}
}

// ⭐ THE CRITERION THIS TICKET EXISTS FOR (DECISIONS.md 2026-09-13). One dungeon shows its loot as
// it is; anything that CONTAINS several dungeons shows each item once; two dungeons that share an
// item show it under each, because there the duplication is the information.
func TestTheCollapsingRuleFollowsTheViewNotAFlag(t *testing.T) {
	for _, tc := range []struct {
		name      string
		places    []string
		wantRows  int
		wantUnder []string
		collapsed bool
	}{
		{"an aggregate view collapses to one row", nil, 1, nil, true},
		{"one dungeon shows it once, under that dungeon", []string{"test-cave"}, 1, []string{"test-cave"}, false},
		{"two dungeons that share it show it under BOTH", []string{"test-cave", "test-lair"}, 2, []string{"test-cave", "test-lair"}, false},
		// ⛔ Was "still yields the item once, never zero" until verify round 1. That fallback
		// existed to avoid losing a row, and its only live effect was to HIDE the place filter
		// vanishing: 196 unrelated items came back with no place instead of an obviously wrong
		// page. A row that does not belong in a place view must be absent, which is visible.
		{"an item in none of the named places is absent, not place-less", []string{"test-elsewhere"}, 0, nil, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			res, err := NewService(sharedItem()).List(context.Background(), Filters{Places: tc.places})
			if err != nil {
				t.Fatal(err)
			}
			if len(res.Items) != tc.wantRows {
				t.Fatalf("got %d rows, want %d", len(res.Items), tc.wantRows)
			}
			if res.Collapsed != tc.collapsed {
				t.Errorf("collapsed = %v, want %v", res.Collapsed, tc.collapsed)
			}
			if tc.collapsed {
				// Collapsed rows carry every place as CONTEXT rather than as extra rows.
				if got := len(res.Items[0].Places); got != 3 {
					t.Errorf("collapsed row carries %d places, want all 3 as context", got)
				}
			}
			for i, want := range tc.wantUnder {
				if res.Items[i].Place == nil {
					t.Fatalf("row %d has no place; expected %q", i, want)
				}
				if got := res.Items[i].Place.Slug; got != want {
					t.Errorf("row %d is under %q, want %q", i, got, want)
				}
			}
		})
	}
}

// A page bound that rejects is a page bound a caller trips over; one that clamps is one they never
// notice. Both ends are clamped, and never returns the whole armory.
func TestLimitIsClampedAtBothEnds(t *testing.T) {
	for _, tc := range []struct{ in, want int }{
		{0, DefaultLimit}, {-5, DefaultLimit}, {10, 10}, {10000, MaxLimit}, {MaxLimit + 1, MaxLimit},
	} {
		res, err := NewService(sharedItem()).List(context.Background(), Filters{Limit: tc.in})
		if err != nil {
			t.Fatal(err)
		}
		if res.Limit != tc.want {
			t.Errorf("limit %d became %d, want %d", tc.in, res.Limit, tc.want)
		}
	}
}

// The total rides on the rows, so an empty page carried no total and reported 0 — making "you
// paged past the end" look identical to "nothing matches". They are different answers.
func TestAPagePastTheEndStillReportsTheTrueTotal(t *testing.T) {
	q := sharedItem()
	q.items[0].TotalCount = 1
	res, err := NewService(q).List(context.Background(), Filters{Offset: 500})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Items) != 0 {
		t.Fatalf("got %d items past the end, want 0", len(res.Items))
	}
	if res.Total != 1 {
		t.Errorf("total = %d, want 1 — the caller cannot page back without it", res.Total)
	}
	if q.calls != 2 {
		t.Errorf("made %d queries, want 2 (the page, then the total probe)", q.calls)
	}
}

// An empty result is an ANSWER, not a missing resource: 200, an empty array, and the envelope.
func TestAnEmptyResultIsAnEnvelopeNotAnError(t *testing.T) {
	res, err := NewService(&fakeQ{}).List(context.Background(), Filters{})
	if err != nil {
		t.Fatalf("an empty result must not be an error: %v", err)
	}
	if res.Items == nil {
		t.Error("items is nil — it must marshal as [], not null")
	}
	if len(res.Items) != 0 || res.Total != 0 {
		t.Errorf("got %d items / total %d, want 0 / 0", len(res.Items), res.Total)
	}
	if res.Attribution != Attribution {
		t.Errorf("attribution = %q, want %q", res.Attribution, Attribution)
	}
}

// Kentarii's release was unconditional, which is exactly why the credit is asserted rather than
// assumed (DECISIONS.md 2026-09-13).
func TestEveryResponseCarriesTheAttribution(t *testing.T) {
	if Attribution != "Data preserved from AoC>TV by Kentarii" {
		t.Fatalf("attribution text changed to %q — DECISIONS.md fixes this wording", Attribution)
	}
	res, _ := NewService(sharedItem()).List(context.Background(), Filters{})
	if res.Attribution != Attribution {
		t.Error("list response dropped the attribution")
	}
}

// An unknown slug goes through the central mapper, so the body is the same JSON shape as every
// other rejection rather than a bare string.
func TestAnUnknownSlugIsANotFoundThroughTheMapper(t *testing.T) {
	_, err := NewService(&fakeQ{}).Get(context.Background(), "no-such-item")
	if !errors.Is(err, httpx.ErrNotFound) {
		t.Fatalf("got %v, want it to wrap httpx.ErrNotFound", err)
	}
}

func TestAnEmptySlugIsRejectedRatherThanQueried(t *testing.T) {
	q := &fakeQ{}
	if _, err := NewService(q).Get(context.Background(), "   "); !errors.Is(err, httpx.ErrInvalid) {
		t.Fatalf("got %v, want httpx.ErrInvalid", err)
	}
}

// "%" alone matched all 4,646 items and "_" matched any single character — a search box that
// returns the whole armory for one keystroke. Measured against the real data before it was fixed.
func TestTheNameQueryEscapesLikeMetacharacters(t *testing.T) {
	for _, tc := range []struct{ in, want string }{
		{"", ""},
		{"Devotion", "Devotion"},
		{"50%", `50\%`},
		{"a_b", `a\_b`},
		{`back\slash`, `back\\slash`},
		{"100%_x", `100\%\_x`},
		{"O'Brien", "O'Brien"}, // an apostrophe is an ordinary character in a name, not a wildcard
	} {
		if got := escapeLike(tc.in); got != tc.want {
			t.Errorf("escapeLike(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}

	q := sharedItem()
	if _, err := NewService(q).List(context.Background(), Filters{Query: "50%"}); err != nil {
		t.Fatal(err)
	}
	if q.firstArgs().NameQuery == nil || *q.firstArgs().NameQuery != `50\%` {
		t.Errorf("the query reached SQL as %v, want the escaped form", q.firstArgs().NameQuery)
	}
}

// Aggregate() is the whole collapsing decision, and it is a function of the FILTERS. There is no
// parameter for a caller to get wrong, which is the point (CLAUDE.md rule 5b).
func TestAggregateIsDecidedByTheFiltersAndNotByTheCaller(t *testing.T) {
	if !(Filters{}).Aggregate() {
		t.Error("no place filter is an aggregate view")
	}
	if !(Filters{Region: "cimmeria"}).Aggregate() {
		t.Error("a region is an aggregate view — it contains several places")
	}
	if !(Filters{Tier: "pve-6"}).Aggregate() {
		t.Error("a tier is an aggregate view")
	}
	if (Filters{Places: []string{"test-cave"}}).Aggregate() {
		t.Error("naming a place is not an aggregate view")
	}
}

// ⭐ Aggregate() and the SQL parameter must never disagree about what "no places" means.
//
// They did: Aggregate() said an EMPTY selection is an aggregate view, while the empty (non-nil)
// slice reached Postgres as '{}' and matched nothing — so the service reported collapsed=true and
// returned zero items. Unreachable through /v1, which drops blanks, but reachable from the HTML
// armory page, whose multi-select starts empty: first paint would have shown nothing.
//
// This asserts the two agree by construction rather than by coincidence.
func TestAnEmptyPlaceSelectionIsNotAFilter(t *testing.T) {
	for _, tc := range []struct {
		name   string
		places []string
	}{
		{"nil places", nil},
		{"an empty but non-nil selection", []string{}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			q := sharedItem()
			res, err := NewService(q).List(context.Background(), Filters{Places: tc.places})
			if err != nil {
				t.Fatal(err)
			}
			if !res.Collapsed {
				t.Errorf("collapsed = false; an empty selection is an aggregate view")
			}
			// The predicate and the parameter are the same decision: if Aggregate() is true, no
			// place predicate may reach SQL.
			if got := q.firstArgs().PlaceSlugs; got != nil {
				t.Errorf("PlaceSlugs reached the query as %#v; an aggregate view must send none", got)
			}
			if len(res.Items) != 1 {
				t.Errorf("got %d items, want the item — an empty selection filters nothing", len(res.Items))
			}
		})
	}

	// And the converse: a real selection must reach SQL, or the filter silently vanishes (B1).
	q := sharedItem()
	if _, err := NewService(q).List(context.Background(), Filters{Places: []string{"test-cave", "test-lair"}}); err != nil {
		t.Fatal(err)
	}
	if got := q.firstArgs().PlaceSlugs; len(got) != 2 {
		t.Errorf("PlaceSlugs reached the query as %#v, want both named places", got)
	}
}
