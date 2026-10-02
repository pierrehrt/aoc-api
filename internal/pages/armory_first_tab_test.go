package pages

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

// The default tab is no part of a state's URL in canonicalOf either (AOC-068 verify round 2, O1): a
// typed ?tab=<first> is the arrival state, so its next answer replaces the history entry instead
// of pushing a second one for the same state.
func TestCanonicalOfDropsTheFirstTab(t *testing.T) {
	r := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/armory", nil)
	for address, want := range map[string]string{
		"/armory?tab=test-first":                     "/armory",
		"/armory?tab=test-first&rarity=test-rarity":  "/armory?rarity=test-rarity",
		"/armory?tab=test-second":                    "/armory?tab=test-second",
		"/armory?tab=test-second&rarity=test-rarity": "/armory?rarity=test-rarity&tab=test-second",
	} {
		if got := canonicalOf(r, address, "test-first"); got != want {
			t.Errorf("canonicalOf(%q) = %q, want %q", address, got, want)
		}
	}
}
