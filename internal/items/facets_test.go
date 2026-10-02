package items

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"

	"github.com/pierrehrt/aoc-api/internal/db/sqlcgen"
	"github.com/pierrehrt/aoc-api/internal/httpx"
)

// fillDistinct sets every field of the struct v points at to a non-zero value that differs from
// field to field, so a copy that drops or swaps a field shows. skip names fields to leave zero.
func fillDistinct(t *testing.T, v any, skip ...string) {
	t.Helper()
	rv := reflect.ValueOf(v).Elem()
	skipped := map[string]bool{}
	for _, s := range skip {
		skipped[s] = true
	}
	for i := 0; i < rv.NumField(); i++ {
		name := rv.Type().Field(i).Name
		if skipped[name] {
			continue
		}
		f := rv.Field(i)
		n := i + 1
		switch f.Kind() {
		case reflect.String:
			f.SetString("v" + string(rune('a'+n)))
		case reflect.Bool:
			f.SetBool(true)
		case reflect.Int, reflect.Int32, reflect.Int64:
			f.SetInt(int64(n))
		case reflect.Slice: // []string
			f.Set(reflect.ValueOf([]string{"p" + string(rune('a'+n)), "q" + string(rune('a'+n))}))
		case reflect.Pointer:
			p := reflect.New(f.Type().Elem())
			switch p.Elem().Kind() {
			case reflect.String:
				p.Elem().SetString("v" + string(rune('a'+n)))
			case reflect.Bool:
				p.Elem().SetBool(n%2 == 0) // both values occur across the fields
			case reflect.Int32:
				p.Elem().SetInt(int64(n))
			default:
				t.Fatalf("fillDistinct: no value for *%s (field %s)", p.Elem().Kind(), name)
			}
			f.Set(p)
		default:
			t.Fatalf("fillDistinct: no value for %s (field %s) — teach this helper the new kind", f.Kind(), name)
		}
	}
}

// ⭐ The counts are computed for EXACTLY the filters the rows are. Every list argument except the
// three that are the list's own (order and page) reaches the facet query, and nothing else does:
// a filter added to the CTE that facetParams forgets is a rail whose numbers lie.
func TestTheFacetQueriesTakeEveryListFilter(t *testing.T) {
	var lp sqlcgen.ListItemsParams
	fillDistinct(t, &lp, "SortBy", "PageSize", "PageOffset")
	fp := facetParams(lp)

	listOnly := map[string]bool{"SortBy": true, "PageSize": true, "PageOffset": true}
	lv, fv := reflect.ValueOf(lp), reflect.ValueOf(fp)
	for i := 0; i < lv.NumField(); i++ {
		name := lv.Type().Field(i).Name
		if listOnly[name] {
			continue
		}
		got := fv.FieldByName(name)
		if !got.IsValid() {
			t.Errorf("the list filters on %s and the facet queries have no such argument", name)
			continue
		}
		if !reflect.DeepEqual(got.Interface(), lv.Field(i).Interface()) {
			t.Errorf("facetParams does not carry %s: list %v, facets %v", name, lv.Field(i).Interface(), got.Interface())
		}
	}
	if fv.NumField() != lv.NumField()-len(listOnly) {
		t.Errorf("the facet query takes %d arguments, the list %d filters — one side has a filter the other lacks", fv.NumField(), lv.NumField()-len(listOnly))
	}
}

// Through the service: a request's filters reach the facet queries as the very values they reach
// the list query with.
func TestFacetsAreAskedForTheRowsFilters(t *testing.T) {
	q := sharedItem()
	rec, _ := get(t, q, "/v1/items?facets=1&rarity=epic&class=test-class&ilvl_min=70&price=true&currency=test-token&set=test-set&q=relic&place=test-cave")
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d", rec.Code)
	}
	if len(q.facetArgs) != 1 || len(q.totalsArgs) != 1 {
		t.Fatalf("facet queries called %d and %d times, want once each", len(q.facetArgs), len(q.totalsArgs))
	}
	if want := facetParams(q.firstArgs()); !reflect.DeepEqual(q.facetArgs[0], want) {
		t.Errorf("the facet query got %+v, the list %+v", q.facetArgs[0], want)
	}
	if want := sqlcgen.ItemFacetTotalsParams(facetParams(q.firstArgs())); !reflect.DeepEqual(q.totalsArgs[0], want) {
		t.Errorf("the totals query got %+v", q.totalsArgs[0])
	}
}

// The envelope is the 0.1.0 one unless the caller asks: additive (5c).
func TestFacetsAreOnlyInTheEnvelopeWhenAskedFor(t *testing.T) {
	q := sharedItem()
	_, body := get(t, q, "/v1/items")
	if _, ok := body["facets"]; ok {
		t.Error("facets are in the envelope of a request that did not ask for them")
	}
	if q.facetCalled != 0 {
		t.Error("the facet queries ran for a request that did not ask for them")
	}

	q = sharedItem()
	q.facetRows = []sqlcgen.CountItemFacetsRow{
		{Facet: "rarity", Slug: "epic", Name: "Epic", ColourToken: "rarity-epic", Ord: 1, Items: 1},
		{Facet: "rarity", Slug: "mundane", Name: "Mundane", Ord: 2, Items: 0},
		{Facet: "class", Slug: "test-class", Name: "Test Class", ShortName: "TC", Ord: 1, Items: 1},
	}
	q.totals = sqlcgen.ItemFacetTotalsRow{AnyRarity: 1, AnyClass: 1, AnyPrice: 1, Priced: 0, IlvlN: 1, IlvlLo: 80, IlvlHi: 80}
	_, body = get(t, q, "/v1/items?facets=1")
	fac, ok := body["facets"].(map[string]any)
	if !ok {
		t.Fatalf("facets=1 returned no facets object: %v", body)
	}
	for _, k := range []string{"rarity", "equip_location", "armour_weight", "class", "currency", "set"} {
		g, ok := fac[k].(map[string]any)
		if !ok {
			t.Errorf("facet %q missing", k)
			continue
		}
		if _, ok := g["values"].([]any); !ok {
			t.Errorf("facet %q: values is not an array (an empty vocabulary must still be [])", k)
		}
	}
	vals := fac["rarity"].(map[string]any)["values"].([]any)
	if len(vals) != 2 {
		t.Fatalf("rarity values = %d, want both — a 0 count is listed, never hidden", len(vals))
	}
	zero := vals[1].(map[string]any)
	if zero["slug"] != "mundane" || zero["count"] != float64(0) {
		t.Errorf("the 0-count value reads %v", zero)
	}
	if c := fac["class"].(map[string]any)["values"].([]any)[0].(map[string]any); c["short_name"] != "TC" {
		t.Errorf("a class facet value lost its short name: %v", c)
	}
	if il, ok := fac["ilvl"].(map[string]any); !ok || il["min"] != float64(80) {
		t.Errorf("ilvl span = %v", fac["ilvl"])
	}
	if _, ok := fac["reqlvl"]; ok {
		t.Error("a span with no item behind it (reqlvl_n = 0) was published")
	}
}

// An unknown facet name from the SQL is a programming error, surfaced — not a group silently lost.
func TestAnUnknownFacetFromTheQueryIsAnError(t *testing.T) {
	q := sharedItem()
	q.facetRows = []sqlcgen.CountItemFacetsRow{{Facet: "not-a-facet", Slug: "x"}}
	if _, err := NewService(q).List(context.Background(), Filters{WithFacets: true}); err == nil {
		t.Fatal("an unknown facet name was accepted")
	}
}

// ⭐ Every filter the parser accepts survives a trip through Values: the page builds all of its
// links from Values, so a field it forgets silently drops out of every pager and chip.
func TestFiltersRoundTripThroughValues(t *testing.T) {
	var f Filters
	fillDistinct(t, &f, "Limit", "Offset", "WithFacets")
	f.Sort = SortID // a valid key; fillDistinct's string is not one
	lo, hi := int32(3), int32(9)
	f.ILvlMin, f.ILvlMax, f.ReqLvlMin, f.ReqLvlMax = &lo, &hi, &lo, &hi // ranges must be non-empty

	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/v1/items?"+f.Values().Encode(), nil)
	got, err := ParseFilters(req)
	if err != nil {
		t.Fatalf("Values produced a query the parser rejects: %v (%s)", err, f.Values().Encode())
	}
	if !reflect.DeepEqual(got, f) {
		t.Errorf("round trip lost something:\n sent %+v\n  got %+v\n  via %s", f, got, f.Values().Encode())
	}
	if (Filters{}).Values().Encode() != "" {
		t.Errorf("an empty filter set encodes as %q, want nothing", (Filters{}).Values().Encode())
	}
}

// min above max is refused by the service too, not only by the parser: a caller that builds Filters
// in Go (the page) gets the same answer.
func TestAnEmptyRangeIsRefusedByTheService(t *testing.T) {
	lo, hi := int32(80), int32(70)
	_, err := NewService(sharedItem()).List(context.Background(), Filters{ReqLvlMin: &lo, ReqLvlMax: &hi})
	if !errors.Is(err, httpx.ErrInvalid) {
		t.Errorf("reqlvl 80..70 = %v, want ErrInvalid", err)
	}
}

// AOC-064: an empty facet list is no filter. It must reach SQL as NULL: an empty slice arrives as
// '{}', and `x = ANY('{}')` is false for every row — an unticked group would empty the whole list.
// The page builds lists by removing values (a chip's ×), so an empty non-nil slice is reachable.
func TestAnEmptyFacetListReachesSQLAsNoFilter(t *testing.T) {
	q := sharedItem()
	empty := []string{}
	if _, err := NewService(q).List(context.Background(), Filters{
		Rarities: empty, EquipLocations: empty, ArmourWeights: empty, Classes: empty, Currencies: empty, Sets: empty,
		WithFacets: true,
	}); err != nil {
		t.Fatal(err)
	}
	p := q.firstArgs()
	for name, v := range map[string][]string{"rarities": p.Rarities, "equip_locations": p.EquipLocations, "armour_weights": p.ArmourWeights,
		"classes": p.Classes, "currencies": p.Currencies, "sets": p.Sets} {
		if v != nil {
			t.Errorf("%s reached SQL as %#v, want nil — '{}' matches nothing", name, v)
		}
	}
	if f := q.facetArgs[0]; f.Rarities != nil || f.Sets != nil {
		t.Error("the facet query got an empty list where it needs NULL")
	}
}
