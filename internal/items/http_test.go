package items

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/pierrehrt/aoc-api/internal/db/sqlcgen"
)

type fakeTax struct{}

func (fakeTax) Taxonomies(context.Context) (Taxonomies, error) {
	return Taxonomies{Rarities: []Term{{Slug: "epic", Name: "Epic"}}, Attribution: Attribution}, nil
}

func newTestServer(q Querier) http.Handler {
	h := NewHandler(NewService(q), fakeTax{})
	mux := http.NewServeMux()
	mux.Handle("/v1/items", http.StripPrefix("/v1/items", h.Routes()))
	mux.Handle("/v1/items/", http.StripPrefix("/v1/items", h.Routes()))
	mux.Handle("/v1/taxonomies", http.StripPrefix("/v1/taxonomies", h.TaxonomyRoutes()))
	return mux
}

func get(t *testing.T, q Querier, path string) (*httptest.ResponseRecorder, map[string]any) {
	t.Helper()
	rec := httptest.NewRecorder()
	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, path, nil)
	newTestServer(q).ServeHTTP(rec, req)
	var body map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &body)
	return rec, body
}

// Every filter reaches SQL as the value that was asked for. A filter that is silently dropped
// returns a page the caller did not ask for and cannot tell is wrong.
func TestEveryFilterReachesTheQuery(t *testing.T) {
	for _, tc := range []struct {
		query string
		check func(sqlcgen.ListItemsParams) bool
		what  string
	}{
		{"rarity=epic", func(p sqlcgen.ListItemsParams) bool { return p.Rarity != nil && *p.Rarity == "epic" }, "rarity"},
		{"item_type=ring", func(p sqlcgen.ListItemsParams) bool { return p.ItemType != nil && *p.ItemType == "ring" }, "item_type"},
		{"equip_location=off-hand", func(p sqlcgen.ListItemsParams) bool {
			return p.EquipLocation != nil && *p.EquipLocation == "off-hand"
		}, "equip_location"},
		{"armour_weight=heavy", func(p sqlcgen.ListItemsParams) bool {
			return p.ArmourWeight != nil && *p.ArmourWeight == "heavy"
		}, "armour_weight"},
		{"class=conqueror", func(p sqlcgen.ListItemsParams) bool { return p.Class != nil && *p.Class == "conqueror" }, "class"},
		{"region=cimmeria", func(p sqlcgen.ListItemsParams) bool { return p.Region != nil && *p.Region == "cimmeria" }, "region"},
		{"tier=pve-6", func(p sqlcgen.ListItemsParams) bool { return p.Tier != nil && *p.Tier == "pve-6" }, "tier"},
		{"place=test-cave", func(p sqlcgen.ListItemsParams) bool { return p.Place != nil && *p.Place == "test-cave" }, "place"},
		{"q=relic", func(p sqlcgen.ListItemsParams) bool { return p.NameQuery != nil && *p.NameQuery == "relic" }, "q"},
		{"pvp=true", func(p sqlcgen.ListItemsParams) bool { return p.Pvp != nil && *p.Pvp }, "pvp=true"},
		{"pvp=false", func(p sqlcgen.ListItemsParams) bool { return p.Pvp != nil && !*p.Pvp }, "pvp=false"},
		{"unchained=true", func(p sqlcgen.ListItemsParams) bool { return p.Unchained != nil && *p.Unchained }, "unchained"},
		{"limit=7&offset=3", func(p sqlcgen.ListItemsParams) bool { return p.PageSize == 7 && p.PageOffset == 3 }, "paging"},
		// Absence must stay absent: a zero value here would filter on the empty string.
		{"", func(p sqlcgen.ListItemsParams) bool {
			return p.Rarity == nil && p.Pvp == nil && p.Unchained == nil && p.Region == nil
		}, "no filters at all"},
	} {
		t.Run(tc.what, func(t *testing.T) {
			q := sharedItem()
			rec, _ := get(t, q, "/v1/items?"+tc.query)
			if rec.Code != http.StatusOK {
				t.Fatalf("status %d for %q", rec.Code, tc.query)
			}
			if !tc.check(q.firstArgs()) {
				t.Errorf("%s did not reach the query: %+v", tc.what, q.firstArgs())
			}
		})
	}
}

// Filters combine rather than overriding one another.
func TestFiltersCombine(t *testing.T) {
	q := sharedItem()
	get(t, q, "/v1/items?rarity=epic&armour_weight=heavy&class=conqueror&pvp=true&tier=pve-6")
	p := q.firstArgs()
	if p.Rarity == nil || p.ArmourWeight == nil || p.Class == nil || p.Pvp == nil || p.Tier == nil {
		t.Fatalf("a combined query lost a filter: %+v", p)
	}
}

// A malformed parameter is rejected; an unknown VALUE is not. Whether "legendaryy" is a rarity is
// a database question, and the database answers it with an empty page — which is a 200.
func TestAMalformedParameterIsRejectedButAnUnknownValueIsNot(t *testing.T) {
	for _, bad := range []string{"limit=abc", "offset=xyz", "pvp=maybe", "unchained=sometimes", "offset=-1"} {
		rec, body := get(t, sharedItem(), "/v1/items?"+bad)
		if rec.Code != http.StatusBadRequest {
			t.Errorf("%s -> %d, want 400", bad, rec.Code)
		}
		if body["error"] == nil {
			t.Errorf("%s -> no JSON error body", bad)
		}
	}
	rec, _ := get(t, sharedItem(), "/v1/items?rarity=not-a-real-rarity")
	if rec.Code != http.StatusOK {
		t.Errorf("an unknown filter VALUE must be answered by the database, got %d", rec.Code)
	}
}

// Several `place` parameters are one selection, and that selection is what turns collapsing off.
func TestRepeatedAndCommaSeparatedPlacesAreOneSelection(t *testing.T) {
	for _, q := range []string{"place=test-cave&place=test-lair", "place=test-cave,test-lair"} {
		rec, body := get(t, sharedItem(), "/v1/items?"+q)
		if rec.Code != http.StatusOK {
			t.Fatalf("status %d", rec.Code)
		}
		if body["collapsed"] != false {
			t.Errorf("%s: collapsed = %v, want false", q, body["collapsed"])
		}
		if n := len(body["items"].([]any)); n != 2 {
			t.Errorf("%s: got %d rows, want one per named place", q, n)
		}
	}
}

// Anonymous: no auth on any read path, and no header is required to get an answer.
func TestReadsAreAnonymous(t *testing.T) {
	rec, _ := get(t, sharedItem(), "/v1/items")
	if rec.Code != http.StatusOK {
		t.Fatalf("an unauthenticated read got %d", rec.Code)
	}
	if rec.Header().Get("WWW-Authenticate") != "" {
		t.Error("a public read path must not challenge for credentials")
	}
}

// Cache-Control is set deliberately on each route, and the value is justified in docs/api-routes.md.
func TestEachRouteSetsItsCacheControl(t *testing.T) {
	for path, want := range map[string]string{
		"/v1/items":      listCache,
		"/v1/taxonomies": taxCache,
	} {
		rec, _ := get(t, sharedItem(), path)
		if got := rec.Header().Get("Cache-Control"); got != want {
			t.Errorf("%s -> Cache-Control %q, want %q", path, got, want)
		}
	}
}

// The envelope is part of the contract: a client pages with it.
func TestTheListEnvelopeIsComplete(t *testing.T) {
	_, body := get(t, sharedItem(), "/v1/items?limit=5&offset=0")
	for _, k := range []string{"items", "total", "limit", "offset", "collapsed", "attribution"} {
		if _, ok := body[k]; !ok {
			t.Errorf("the envelope is missing %q", k)
		}
	}
}

// An empty result marshals as [] rather than null — a client that iterates would crash on null.
func TestAnEmptyResultMarshalsAsAnArray(t *testing.T) {
	rec, body := get(t, &fakeQ{}, "/v1/items")
	if rec.Code != http.StatusOK {
		t.Fatalf("empty result -> %d, want 200", rec.Code)
	}
	items, ok := body["items"].([]any)
	if !ok {
		t.Fatalf("items is %T, want an array", body["items"])
	}
	if len(items) != 0 {
		t.Errorf("got %d items, want 0", len(items))
	}
}

func TestAnUnknownItemIs404WithAJSONBody(t *testing.T) {
	rec, body := get(t, &fakeQ{}, "/v1/items/no-such-item")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status %d, want 404", rec.Code)
	}
	if body["error"] == nil {
		t.Errorf("404 body was %q, want a JSON error object", rec.Body.String())
	}
	if ct := rec.Header().Get("Content-Type"); ct == "" || ct[:16] != "application/json" {
		t.Errorf("Content-Type %q, want JSON", ct)
	}
}

func TestTaxonomiesCarryTheAttribution(t *testing.T) {
	_, body := get(t, sharedItem(), "/v1/taxonomies")
	if body["attribution"] != Attribution {
		t.Errorf("attribution = %v, want %q", body["attribution"], Attribution)
	}
}
