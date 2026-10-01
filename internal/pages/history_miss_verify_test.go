package pages_test

// AOC-063 verify round 1 — tests added by the independent verify. Fake corpus only
// (fake_items_test.go): every name is obviously fake, and nothing here states a fact about the game.

import (
	"encoding/json"
	"html"
	"net/http"
	"regexp"
	"strings"
	"testing"
)

var htmxConfigMeta = regexp.MustCompile(`<meta name="htmx-config" content='([^']+)'>`)

// The fix is "one line for every HTMX page": the setting lives in the layout, so EVERY page the layout
// renders carries it exactly once — not only /armory, where TestHTMXIsToldToSwapA400 reads it. A
// second htmx-config tag would be ambiguous (htmx reads the first), so exactly one.
func TestEveryLayoutPageTellsHTMXToReloadOnAHistoryMiss(t *testing.T) {
	h := router(t)
	for _, path := range []string{"/", "/_smoke", "/armory", "/armory?p=2", "/armory?class=test-class", "/armory/test-item-1"} {
		rr := get(t, h, http.MethodGet, path, nil, "")
		if rr.Code != http.StatusOK {
			t.Fatalf("%s: status %d", path, rr.Code)
		}
		ms := htmxConfigMeta.FindAllStringSubmatch(rr.Body.String(), -1)
		if len(ms) != 1 {
			t.Errorf("%s: %d htmx-config meta tags, want exactly 1", path, len(ms))
			continue
		}
		var cfg struct {
			RefreshOnHistoryMiss *bool `json:"refreshOnHistoryMiss"`
		}
		if err := json.Unmarshal([]byte(html.UnescapeString(ms[0][1])), &cfg); err != nil {
			t.Errorf("%s: htmx-config is not JSON: %v", path, err)
			continue
		}
		if cfg.RefreshOnHistoryMiss == nil || !*cfg.RefreshOnHistoryMiss {
			t.Errorf("%s: htmx-config does not set refreshOnHistoryMiss: true", path)
		}
	}
}

// What the reload fetches: on a miss the browser re-requests the pushed URL as an ordinary request (no
// HX-Request), and for every kind of URL the armory pushes — a rail change, a pager link, a sort link
// — that answer is the whole page: the site header, the form, the rail and the rows.
func TestEveryPushedArmoryURLIsTheWholePageWithoutHXRequest(t *testing.T) {
	h := router(t)
	for _, path := range []string{"/armory", "/armory?class=test-class", "/armory?p=2", "/armory?sort=name"} {
		rr := get(t, h, http.MethodGet, path, nil, "")
		if rr.Code != http.StatusOK {
			t.Fatalf("%s: status %d", path, rr.Code)
		}
		body := rr.Body.String()
		for _, want := range []string{`<html`, `<header`, `<form method="get" action="/armory"`, `id="results"`, `id="armory-facets"`, `<tbody`} {
			if strings.Count(body, want) != 1 {
				t.Errorf("%s: %q appears %d times, want once", path, want, strings.Count(body, want))
			}
		}
		if !regexp.MustCompile(`(?s)<tbody.*?<tr`).MatchString(body) {
			t.Errorf("%s: no rows", path)
		}
	}
}
