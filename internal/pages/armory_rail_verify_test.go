package pages_test

// AOC-049 verify round 1 — tests added by the independent verify for criteria and edge cases the
// build's tests left implicit. Fake corpus only (fake_items_test.go): every name is obviously fake,
// and nothing here states a fact about the game.

import (
	"html"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

// Criterion "a value with 0 is shown greyed, never hidden": for EVERY radio on the rail, the label
// is muted exactly when its count is 0 and it is not the reader's own choice — a non-zero value is
// never greyed, and a zero is never dropped.
func TestEveryRadioIsMutedExactlyWhenItsCountIsZero(t *testing.T) {
	for _, path := range []string{"/armory", "/armory?rarity=test-rarity-dull", "/armory?class=test-class&price=false"} {
		body := get(t, router(t), http.MethodGet, path, nil, "").Body.String()
		re := regexp.MustCompile(`(?s)<input type="(?:radio|checkbox)" id="([^"]+)" name="[^"]+" value="[^"]*"( checked)?[^>]*>\s*<label for="[^"]+"[^>]*class="([^"]*)">.*?<span class="font-mono text-xs text-muted">(\d+)</span>`)
		ms := re.FindAllStringSubmatch(body, -1)
		if len(ms) == 0 {
			t.Fatalf("%s: no radio found", path)
		}
		zeros := 0
		for _, m := range ms {
			n, _ := strconv.Atoi(m[4])
			checked := m[2] != ""
			muted := false
			for _, c := range strings.Fields(m[3]) {
				if c == "text-muted" {
					muted = true
				}
			}
			if n == 0 {
				zeros++
			}
			if want := n == 0 && !checked; muted != want {
				t.Errorf("%s: %s (count %d, checked %v) muted=%v, want %v", path, m[1], n, checked, muted, want)
			}
		}
		if zeros == 0 {
			t.Errorf("%s: the fake vocabulary has zero-count values, none was listed", path)
		}
	}
}

// Criterion "v1 lists the 24 currencies flat": the picker is one flat <select> — no <optgroup>, no
// grouping invented in the page — holding Any and then every currency the service returned, in the
// service's order, each with its count.
func TestTheCurrencyPickerIsFlatAndHoldsEveryValueInOrder(t *testing.T) {
	body := get(t, router(t), http.MethodGet, "/armory", nil, "").Body.String()
	sel := regexp.MustCompile(`(?s)<select id="f-currency" name="currency"[^>]*>(.*?)</select>`).FindStringSubmatch(body)
	if sel == nil {
		t.Fatal("no currency select")
	}
	if strings.Contains(sel[1], "<optgroup") {
		t.Error("the currency picker groups its values — no grouping column exists (design open question 4)")
	}
	var got []string
	for _, o := range regexp.MustCompile(`<option value="([^"]*)"[^>]*>[^<]*\(\d+\)</option>`).FindAllStringSubmatch(sel[1], -1) {
		got = append(got, html.UnescapeString(o[1]))
	}
	var want []string
	want = append(want, "") // Any
	for _, r := range fakeFacetRows {
		if r.Facet == "currency" {
			want = append(want, r.Slug)
		}
	}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("currency options = %v, want %v (Any, then the service's values in its order)", got, want)
	}
}

// Criterion "every state is a URL", JS off: a plain form submit sends every field, the untouched
// ones empty (`?q=&equip_location=&…`). That URL must still be the same state — its canonical, its
// pager and its chips are the clean URL of that state, never the raw query string.
func TestAJSOffSubmitWithEmptyFieldsHasTheCleanStatesURLs(t *testing.T) {
	dirty := "/armory?q=&rarity=epic&equip_location=&armour_weight=&class=&price=&ilvl_min=&ilvl_max=&reqlvl_min=&reqlvl_max=&currency=&set="
	rr := get(t, router(t), http.MethodGet, dirty, nil, "")
	if rr.Code != http.StatusOK {
		t.Fatalf("status %d", rr.Code)
	}
	body := rr.Body.String()
	if !strings.Contains(body, `<link rel="canonical" href="https://aoc-codex.app/armory?rarity=epic">`) {
		t.Errorf("the canonical of a dirty submit is not the clean state URL: %v",
			regexp.MustCompile(`<link rel="canonical"[^>]*>`).FindString(body))
	}
	for _, m := range regexp.MustCompile(`href="(/armory[^"]*)"`).FindAllStringSubmatch(body, -1) {
		u := html.UnescapeString(m[1])
		if strings.Contains(u, "=&") || strings.HasSuffix(u, "=") {
			t.Errorf("a link on the page carries an empty field: %s", u)
		}
	}
	if !strings.Contains(body, "Rarity: Test Epic") {
		t.Error("the one real filter of the dirty submit is not a chip")
	}
	if !strings.Contains(body, `<span id="filter-count"> · 1</span>`) {
		t.Error("the dirty submit counts more (or fewer) than its one active filter")
	}
}

// An unknown slug on a RADIO facet (a mistyped link) is not malformed: it stays the chosen value at
// 0, named by a chip, and the page answers 200 — the rule build decision 3 sets for every facet.
func TestAnUnknownRadioSlugStaysChosenAndNamed(t *testing.T) {
	rr := get(t, router(t), http.MethodGet, "/armory?rarity=not-a-rarity", nil, "")
	if rr.Code != http.StatusOK {
		t.Fatalf("status %d, want 200", rr.Code)
	}
	body := rr.Body.String()
	// AOC-064: rarity is a checkbox group; the unknown value stays a ticked box.
	if !strings.Contains(body, `<input type="checkbox" id="f-rarity-unknown-0" name="rarity" value="not-a-rarity" checked`) {
		t.Error("the unknown rarity is not kept as a ticked choice — a submit would silently drop it")
	}
	if strings.Contains(body, `id="f-rarity-any" name="rarity" value="" checked`) {
		t.Error("Any is checked while the URL names a rarity")
	}
	if !strings.Contains(body, "Rarity: not-a-rarity") {
		t.Error("no chip names the unknown rarity")
	}
}

// Pagination boundary with filters on: the last page's links keep the filters, and one page past it
// is a 404 exactly as without filters (120 fake rows, 50 a page: 3 pages).
func TestAFilteredPagerBoundary(t *testing.T) {
	h := router(t)
	last := get(t, h, http.MethodGet, "/armory?rarity=epic&ilvl_min=10&p=3", nil, "")
	if last.Code != http.StatusOK {
		t.Fatalf("p=3: status %d", last.Code)
	}
	links := regexp.MustCompile(`href="(/armory\?[^"]*p=2[^"]*)"`).FindAllStringSubmatch(last.Body.String(), -1)
	if len(links) == 0 {
		t.Fatal("the last page links to no page 2")
	}
	for _, l := range links {
		u := html.UnescapeString(l[1])
		if !strings.Contains(u, "rarity=epic") || !strings.Contains(u, "ilvl_min=10") {
			t.Errorf("a pager link from the last filtered page drops a filter: %s", u)
		}
	}
	if rr := get(t, h, http.MethodGet, "/armory?rarity=epic&ilvl_min=10&p=4", nil, ""); rr.Code != http.StatusNotFound {
		t.Errorf("one page past the end, filtered: %d, want 404", rr.Code)
	}
}
