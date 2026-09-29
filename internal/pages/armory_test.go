package pages_test

import (
	"net/http"
	"regexp"
	"strings"
	"testing"
)

// The Armory list page (AOC-047), against a fake corpus of 120 obviously fake items.

func TestArmoryRendersTheTableWithoutJavaScript(t *testing.T) {
	h := router(t)
	rr := get(t, h, http.MethodGet, "/armory", nil, "")
	if rr.Code != http.StatusOK {
		t.Fatalf("status %d", rr.Code)
	}
	body := rr.Body.String()
	for _, want := range []string{
		"<table", "Test Item 1", `<form method="get" action="/armory"`, "120 items", "of 120",
		`href="/armory?p=2"`, "page 1 of 3", `aria-current="page"`,
		`style="color: var(--color-rarity-epic)"`, // the rarity comes as a token, never a slug in a class
		`<link rel="canonical" href="https://aoc-codex.app/armory">`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("missing %q", want)
		}
	}
	// No confidence marker, no provenance anywhere on the page (Pierre, 2026-09-29).
	for _, banned := range []string{"unconfirmed", "verified", "Kentarii", "Funcom"} {
		if strings.Contains(body, banned) {
			t.Errorf("renders %q — the site shows no provenance", banned)
		}
	}
	// Rows link to nothing until the item page exists (AOC-048): a link to a 404 is a bug.
	if regexp.MustCompile(`href="/armory/test-item`).MatchString(body) {
		t.Error("rows link to item pages that do not exist yet")
	}
}

// A vendor-sold item with no drop location: the price stands alone on the phone row, no "· " in
// front of it; and one match reads "1 item" (verify round 1).
func TestArmoryPhoneRowNeverLeadsWithASeparator(t *testing.T) {
	h := router(t)
	one := get(t, h, http.MethodGet, "/armory?q=Item+120", nil, "").Body.String()
	if got := regexp.MustCompile(`<strong class="text-paper">([^<]*)</strong>`).FindStringSubmatch(one); got == nil || got[1] != "1 item" {
		t.Errorf("one match is written as %q, want \"1 item\"", got)
	}
	body := get(t, h, http.MethodGet, "/armory?q=5", nil, "").Body.String()
	if !strings.Contains(body, "3 Test Token") {
		t.Fatal("item 5's price is not on the page")
	}
	if regexp.MustCompile(`>\s*· 3 Test Token`).MatchString(body) {
		t.Error("the phone row leads with a stray separator before the price")
	}
	// The default sort stays out of a box search's URL: no hidden sort input on the default.
	if strings.Contains(body, `name="sort"`) {
		t.Error("the search form carries the default sort as a hidden input — two URLs per state")
	}
	if !strings.Contains(get(t, h, http.MethodGet, "/armory?sort=name", nil, "").Body.String(), `<input type="hidden" name="sort" value="name">`) {
		t.Error("a non-default sort is not kept by the search form")
	}
	// Vary once on the fragment.
	rr := get(t, h, http.MethodGet, "/armory", map[string]string{"HX-Request": "true"}, "")
	if n := len(rr.Header().Values("Vary")); n != 1 {
		t.Errorf("the fragment sends Vary %d times, want once", n)
	}
}

func TestArmoryEveryStateIsAURL(t *testing.T) {
	h := router(t)
	for path, want := range map[string][]string{
		"/armory?p=2":               {"page 2 of 3", `href="/armory"`, `href="/armory?p=3"`, `href="https://aoc-codex.app/armory?p=2"`},
		"/armory?p=1":               {`href="https://aoc-codex.app/armory">`}, // p=1 canonicalises to no p
		"/armory?q=Item+7":          {"Test Item 7", "Test Item 70", "clear search", `value="Item 7"`},
		"/armory?q=7":               {"Test Item 7"}, // an id
		"/armory?sort=name":         {`href="https://aoc-codex.app/armory?sort=name">`, `aria-current="true"`},
		"/armory?q=zzz-nothing-zzz": {"No items match", "Item ids run 1–123", "3 ids in that range are absent", "Clear the search"},
	} {
		rr := get(t, h, http.MethodGet, path, nil, "")
		if rr.Code != http.StatusOK {
			t.Errorf("%s: status %d", path, rr.Code)
			continue
		}
		for _, w := range want {
			if !strings.Contains(rr.Body.String(), w) {
				t.Errorf("%s: missing %q", path, w)
			}
		}
	}
}

func TestArmoryRejectsBadInputHonestly(t *testing.T) {
	h := router(t)
	for path, status := range map[string]int{
		"/armory?p=0": 400, "/armory?p=x": 400, "/armory?sort=level": 400,
		"/armory?p=99": 404, // past the end: a URL that names nothing
	} {
		rr := get(t, h, http.MethodGet, path, nil, "")
		if rr.Code != status {
			t.Errorf("%s: status %d, want %d", path, rr.Code, status)
		}
		if ct := rr.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/html") {
			t.Errorf("%s: a person in a browser got %q", path, ct)
		}
	}
}

func TestArmoryHTMXGetsTheRowsFragment(t *testing.T) {
	h := router(t)
	rr := get(t, h, http.MethodGet, "/armory?p=2", map[string]string{"HX-Request": "true"}, "")
	if rr.Code != http.StatusOK {
		t.Fatalf("status %d", rr.Code)
	}
	body := rr.Body.String()
	if strings.Contains(body, "<html") || strings.Contains(body, "<form") {
		t.Error("an HTMX request got the whole page, not the rows fragment")
	}
	if !strings.Contains(body, "page 2 of 3") {
		t.Error("the fragment is not the requested page")
	}
	if v := rr.Header().Get("Vary"); !strings.Contains(v, "HX-Request") {
		t.Errorf("Vary = %q, want HX-Request", v)
	}
	// The full page varies too, and the nav now has the Armory.
	full := get(t, h, http.MethodGet, "/armory", nil, "")
	if v := full.Header().Get("Vary"); !strings.Contains(v, "HX-Request") {
		t.Errorf("full page Vary = %q", v)
	}
	if !strings.Contains(get(t, h, http.MethodGet, "/", nil, "").Body.String(), `href="/armory"`) {
		t.Error("the nav does not link the Armory")
	}
}
