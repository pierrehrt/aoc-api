package items

import (
	"context"
	"reflect"
	"testing"

	"github.com/pierrehrt/aoc-api/internal/db/sqlcgen"
)

// AOC-038: a place that CONTAINS places. Asking for it means everything in it, each item once
// (Pierre, 2026-09-29: House of Crom's loot is recorded against its two wings, and clicking House of
// Crom should list all of it). The names are fake on purpose (CLAUDE.md STEP ZERO).

// containerItem is sharedItem's item (in test-crypt, test-cave and test-lair), with test-complex
// holding test-cave and test-lair the way a raid holds its wings.
func containerItem() *fakeQ {
	q := sharedItem()
	q.inside = map[string][]string{"test-complex": {"test-cave", "test-lair"}}
	return q
}

func TestAPlaceThatContainsPlacesListsWhatIsInsideItOnce(t *testing.T) {
	q := containerItem()
	res, err := NewService(q).List(context.Background(), Filters{Places: []string{"test-complex"}})
	if err != nil {
		t.Fatal(err)
	}
	// What reaches SQL is the complex AND its wings: the complex's own rows hold none of the loot.
	if got, want := q.firstArgs().PlaceSlugs, []string{"test-complex", "test-cave", "test-lair"}; !reflect.DeepEqual(got, want) {
		t.Errorf("PlaceSlugs = %v, want %v", got, want)
	}
	if !res.Collapsed {
		t.Error("collapsed = false; a place that contains places is a view of several dungeons")
	}
	if len(res.Items) != 1 {
		t.Fatalf("got %d rows, want the item once", len(res.Items))
	}
	if res.Items[0].Place != nil || len(res.Items[0].Places) != 3 {
		t.Errorf("the collapsed row carries Place=%v and %d places; want no Place and all 3 as context",
			res.Items[0].Place, len(res.Items[0].Places))
	}
}

// The other half of the rule must not move: two wings named are two dungeons, and an item they
// share appears under each (DECISIONS.md 2026-09-13, unchanged by AOC-038).
func TestTwoWingsOfOneComplexStillShowASharedItemUnderEach(t *testing.T) {
	q := containerItem()
	res, err := NewService(q).List(context.Background(), Filters{Places: []string{"test-cave", "test-lair"}})
	if err != nil {
		t.Fatal(err)
	}
	if got, want := q.firstArgs().PlaceSlugs, []string{"test-cave", "test-lair"}; !reflect.DeepEqual(got, want) {
		t.Errorf("PlaceSlugs = %v, want exactly the two wings", got)
	}
	if res.Collapsed || len(res.Items) != 2 {
		t.Fatalf("collapsed=%v with %d rows; want expanded, the item under each wing", res.Collapsed, len(res.Items))
	}
	for i, want := range []string{"test-cave", "test-lair"} {
		if res.Items[i].Place == nil || res.Items[i].Place.Slug != want {
			t.Errorf("row %d is under %v, want %q", i, res.Items[i].Place, want)
		}
	}
}

// A selection that includes a complex contains several dungeons, whatever else is named with it.
func TestAComplexNamedBesideADungeonCollapses(t *testing.T) {
	q := containerItem()
	res, err := NewService(q).List(context.Background(), Filters{Places: []string{"test-complex", "test-crypt"}})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Collapsed || len(res.Items) != 1 {
		t.Errorf("collapsed=%v with %d rows; want the item once", res.Collapsed, len(res.Items))
	}
}

// noPlacesQ is the database's answer for a name no place has: no row at all.
type noPlacesQ struct{ *fakeQ }

func (noPlacesQ) ExpandPlaces(context.Context, []string) ([]sqlcgen.ExpandPlacesRow, error) {
	return nil, nil
}

// ⚠️ The trap this guards: a name that expands to nothing must not become an EMPTY list, because an
// empty list reaches SQL as no filter at all, and place=a-typo would answer the whole armory.
func TestANameNoPlaceHasStillFiltersToNothing(t *testing.T) {
	q := noPlacesQ{sharedItem()}
	res, err := NewService(q).List(context.Background(), Filters{Places: []string{"test-typo"}})
	if err != nil {
		t.Fatal(err)
	}
	if got := q.firstArgs().PlaceSlugs; !reflect.DeepEqual(got, []string{"test-typo"}) {
		t.Errorf("PlaceSlugs = %#v, want [test-typo]: dropping it would remove the filter", got)
	}
	if res.Collapsed {
		t.Error("collapsed = true for one named place")
	}
}

// The rail's counts are computed for the same places as the rows (facetParams), so a complex's
// counts are its wings' too.
func TestTheRailCountsAComplexAsItsWings(t *testing.T) {
	q := containerItem()
	if _, err := NewService(q).List(context.Background(), Filters{Places: []string{"test-complex"}, WithFacets: true}); err != nil {
		t.Fatal(err)
	}
	if len(q.facetArgs) == 0 {
		t.Fatal("the facet query was not called")
	}
	if got, want := q.facetArgs[0].PlaceSlugs, []string{"test-complex", "test-cave", "test-lair"}; !reflect.DeepEqual(got, want) {
		t.Errorf("facet PlaceSlugs = %v, want %v", got, want)
	}
}
