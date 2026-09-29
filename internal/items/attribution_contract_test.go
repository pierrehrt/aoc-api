package items

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/pierrehrt/aoc-api/internal/db/sqlcgen"
)

// oneItemQ is sharedItem() with a slug that resolves, so /v1/items/{slug} can be read the way a
// consumer reads it. The fixture item is deliberately fake (CLAUDE.md STEP ZERO).
type oneItemQ struct{ *fakeQ }

func (oneItemQ) GetItemBySlug(context.Context, string) (int32, error) { return 1, nil }
func (oneItemQ) GetItem(context.Context, int32) (sqlcgen.GetItemRow, error) {
	return sqlcgen.GetItemRow{ItemID: 1, Slug: "test-relic-alpha", Name: "Test Relic Alpha", Rarity: "epic", Confidence: "unconfirmed"}, nil
}

// AOC-055 verify round 1. /v1 is a public contract (CLAUDE.md 5c): the `attribution` field stays
// on every route — same name, a string — and this is asserted from the JSON body, on all three
// routes, rather than on the Go struct. Its value names AoC Codex and no other site or person
// (Pierre, 2026-09-29).
func TestEveryRouteCarriesTheAttributionFieldAsJSON(t *testing.T) {
	q := oneItemQ{sharedItem()}
	for _, path := range []string{"/v1/items?limit=1", "/v1/items/test-relic-alpha", "/v1/taxonomies"} {
		rec, body := get(t, q, path)
		if rec.Code != http.StatusOK {
			t.Fatalf("%s -> %d, want 200: %s", path, rec.Code, rec.Body.String())
		}
		v, ok := body["attribution"]
		if !ok {
			t.Errorf("%s: the JSON body has no \"attribution\" field — that is a breaking change", path)
			continue
		}
		s, ok := v.(string)
		if !ok {
			t.Errorf("%s: attribution is %T, want a string", path, v)
			continue
		}
		if s != Attribution {
			t.Errorf("%s: attribution %q, want %q", path, s, Attribution)
		}
		for _, banned := range []string{"Kentarii", "AoC>TV", "Johar", "Funcom"} {
			if strings.Contains(s, banned) {
				t.Errorf("%s: attribution names %q — the site names no other site or person", path, banned)
			}
		}
	}
}
