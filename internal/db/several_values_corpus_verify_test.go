//go:build corpus

// A corpus test (AOC-044): compiled only with `-tags corpus` — see item_read_endpoints_test.go.

package db_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sort"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/pierrehrt/aoc-api/internal/items"
)

// AOC-064 verify round 1: several values in a group, on the REAL corpus — the criterion says "tested
// on fixtures and the corpus", and the build's corpus test checks the rail's numbers under a
// several-value state but not the sum or the union themselves. Every value is picked FROM the data
// (the two busiest and the smallest non-zero value of each facet), never typed here, so a re-import
// cannot make it stale.

// withValues returns f with the facet named by its parameter set to vals.
func withValues(t *testing.T, f items.Filters, param string, vals []string) items.Filters {
	t.Helper()
	switch param {
	case "rarity":
		f.Rarities = vals
	case "equip_location":
		f.EquipLocations = vals
	case "armour_weight":
		f.ArmourWeights = vals
	case "class":
		f.Classes = vals
	case "currency":
		f.Currencies = vals
	case "set":
		f.Sets = vals
	default:
		t.Fatalf("no filter for facet %q", param)
	}
	return f
}

// allIDs is every item id the list holds for f, paged through to the end.
func allIDs(t *testing.T, s *items.Service, f items.Filters) map[int32]bool {
	t.Helper()
	out := map[int32]bool{}
	f.WithFacets, f.Limit = false, items.MaxLimit
	for f.Offset = 0; ; f.Offset += items.MaxLimit {
		res, err := s.List(context.Background(), f)
		if err != nil {
			t.Fatalf("List(%+v): %v", f, err)
		}
		for _, it := range res.Items {
			out[it.ID] = true
		}
		if int64(f.Offset+items.MaxLimit) >= res.Total {
			return out
		}
	}
}

// pick is the two busiest values of a group and its smallest non-zero one, or nil when it has fewer
// than three values with items.
func pick(g items.FacetGroup) ([]string, []int64) {
	var vs []items.FacetValue
	for _, v := range g.Values {
		if v.Count > 0 {
			vs = append(vs, v)
		}
	}
	if len(vs) < 3 {
		return nil, nil
	}
	sort.SliceStable(vs, func(i, j int) bool { return vs[i].Count > vs[j].Count })
	chosen := []items.FacetValue{vs[0], vs[1], vs[len(vs)-1]}
	var slugs []string
	var counts []int64
	for _, v := range chosen {
		slugs, counts = append(slugs, v.Slug), append(counts, v.Count)
	}
	return slugs, counts
}

func groups(fc *items.Facets) []struct {
	param     string
	group     items.FacetGroup
	exclusive bool // one value per item: rarity, armour weight, set
} {
	return []struct {
		param     string
		group     items.FacetGroup
		exclusive bool
	}{{"rarity", fc.Rarity, true}, {"armour_weight", fc.ArmourWeight, true}, {"set", fc.Set, true},
		{"equip_location", fc.EquipLocation, false}, {"class", fc.Class, false}, {"currency", fc.Currency, false}}
}

func TestSeveralValuesAreTheSumOrTheUnionOnTheRealCorpus(t *testing.T) {
	s := svc(t)
	ctx := context.Background()
	root, err := s.List(ctx, items.Filters{WithFacets: true, Limit: 1})
	if err != nil {
		t.Fatal(err)
	}
	busiestClass, _ := pick(root.Facets.Class)
	if busiestClass == nil {
		t.Skip("the corpus has fewer than three classes with items")
	}
	checked := 0
	for label, base := range map[string]items.Filters{
		"no other filter":       {},
		"item level from 60":    {ILvlMin: i32(60)},
		"the busiest class too": {Classes: busiestClass[:1]},
	} {
		b := base
		b.WithFacets, b.Limit = true, 1
		res, err := s.List(ctx, b)
		if err != nil {
			t.Fatal(err)
		}
		for _, g := range groups(res.Facets) {
			if g.param == "class" && base.Classes != nil {
				continue
			}
			slugs, counts := pick(g.group)
			if slugs == nil {
				continue
			}
			several := withValues(t, base, g.param, slugs)
			got := total(t, s, several)

			var sum int64
			for _, c := range counts {
				sum += c
			}
			if g.exclusive && got != sum {
				t.Errorf("%s, %s %v: %d items, want exactly the sum of their counts %v = %d", label, g.param, slugs, got, counts, sum)
			}

			union := map[int32]bool{}
			for _, one := range slugs {
				for id := range allIDs(t, s, withValues(t, base, g.param, []string{one})) {
					union[id] = true
				}
			}
			rows := allIDs(t, s, several)
			if int64(len(rows)) != got || len(rows) != len(union) {
				t.Errorf("%s, %s %v: total %d, %d rows, want the union of what each gives alone, %d", label, g.param, slugs, got, len(rows), len(union))
			}
			for id := range rows {
				if !union[id] {
					t.Errorf("%s, %s %v: item %d is listed but no single value lists it", label, g.param, slugs, id)
					break
				}
			}

			// A group's own counts are unchanged by ticking its values: each still counts the items
			// with that value under the OTHER groups.
			sv := several
			sv.WithFacets, sv.Limit = true, 1
			after, err := s.List(ctx, sv)
			if err != nil {
				t.Fatal(err)
			}
			if after.Total != got {
				t.Errorf("%s, %s %v: total %d with facets, %d without", label, g.param, slugs, after.Total, got)
			}
			for _, ag := range groups(after.Facets) {
				if ag.param != g.param {
					continue
				}
				before := map[string]int64{}
				for _, v := range g.group.Values {
					before[v.Slug] = v.Count
				}
				for _, v := range ag.group.Values {
					if before[v.Slug] != v.Count {
						t.Errorf("%s, %s %v: %s's own count moved from %d to %d", label, g.param, slugs, v.Slug, before[v.Slug], v.Count)
					}
				}
			}
			checked++
		}
	}
	if checked < 10 {
		t.Errorf("only %d several-value states were checked", checked)
	}
	t.Logf("%d several-value states checked", checked)
}

type noTaxonomies struct{}

func (noTaxonomies) Taxonomies(context.Context) (items.Taxonomies, error) {
	return items.Taxonomies{}, nil
}

// The same through /v1/items on the real corpus: the repeated, comma-separated and mixed spellings
// (with a duplicate and an empty element) are one request; an unknown value beside known ones matches
// the known ones; an empty value is no filter.
func TestSeveralValuesThroughV1OnTheRealCorpus(t *testing.T) {
	s := svc(t)
	r := chi.NewRouter()
	r.Mount("/v1/items", items.NewHandler(s, noTaxonomies{}).Routes())
	totalOf := func(query string) int64 {
		t.Helper()
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/v1/items?limit=1&"+query, nil))
		if rec.Code != http.StatusOK {
			t.Fatalf("%s: status %d: %s", query, rec.Code, rec.Body.String())
		}
		var env struct {
			Total int64 `json:"total"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
			t.Fatal(err)
		}
		return env.Total
	}

	root, err := s.List(context.Background(), items.Filters{WithFacets: true, Limit: 1})
	if err != nil {
		t.Fatal(err)
	}
	for _, g := range groups(root.Facets) {
		slugs, _ := pick(g.group)
		if slugs == nil {
			continue
		}
		a, b, c := slugs[0], slugs[1], slugs[2]
		want := total(t, s, withValues(t, items.Filters{}, g.param, slugs))
		for _, q := range []string{
			g.param + "=" + a + "&" + g.param + "=" + b + "&" + g.param + "=" + c,
			g.param + "=" + a + "," + b + "," + c,
			g.param + "=" + a + "&" + g.param + "=" + b + ",," + c + "," + a + "&" + g.param + "=",
		} {
			if got := totalOf(q); got != want {
				t.Errorf("%s: %d items, the service gives %d for %v", q, got, want, slugs)
			}
		}
		one := total(t, s, withValues(t, items.Filters{}, g.param, []string{a}))
		if got := totalOf(g.param + "=" + a + "&" + g.param + "=no-such-value-aoc064"); got != one {
			t.Errorf("%s with an unknown value beside %s: %d items, want %s's %d", g.param, a, got, a, one)
		}
		if got := totalOf(g.param + "=&" + g.param + "=,"); got != root.Total {
			t.Errorf("%s empty: %d items, want every item, %d", g.param, got, root.Total)
		}
	}
}
