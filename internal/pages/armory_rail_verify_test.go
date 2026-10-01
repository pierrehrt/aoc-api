package pages_test

// AOC-049 verify round 1 — tests added by the independent verify for criteria and edge cases the
// build's tests left implicit. Fake corpus only (fake_items_test.go): every name is obviously fake,
// and nothing here states a fact about the game.

import (
	"html"
	"net/http"
	"regexp"
	"strings"
	"testing"
)

// Criterion "a value with 0 is shown greyed, never hidden": for EVERY radio on the rail, the label
// is muted exactly when its count is 0 and it is not the reader's own choice — a non-zero value is
// never greyed, and a zero is never dropped.
func TestEveryRadioIsMutedExactlyWhenItsCountIsZero(t *testing.T) {
	// AOC-065: the choices are drawn as the design's rows and chips; each input carries its count
	// (data-count), and what follows it up to the next input is how it is drawn. Muted (text-faint)
	// exactly when the count is 0 and it is not chosen — never hidden.
	in := regexp.MustCompile(`<input type="(?:radio|checkbox)" id="([^"]+)" name="[^"]+" value="[^"]*"( checked)? data-count="(\d+)"[^>]*>`)
	for _, path := range []string{"/armory", "/armory?rarity=test-rarity-dull", "/armory?class=test-class&price=false"} {
		body := get(t, router(t), http.MethodGet, path, nil, "").Body.String()
		locs := in.FindAllStringSubmatchIndex(body, -1)
		if len(locs) == 0 {
			t.Fatalf("%s: no choice found", path)
		}
		zeros := 0
		for i, l := range locs {
			id, checked, count := body[l[2]:l[3]], l[4] >= 0, body[l[6]:l[7]]
			end := len(body)
			if i+1 < len(locs) {
				end = locs[i+1][0]
			}
			seg := body[l[1]:end]
			if j := strings.Index(seg, "</fieldset>"); j >= 0 {
				seg = seg[:j]
			}
			muted := strings.Contains(seg, "text-faint")
			if count == "0" && !checked {
				zeros++
			}
			if want := count == "0" && !checked; muted != want {
				t.Errorf("%s: %s (count %s, chosen %v) muted=%v, want %v", path, id, count, checked, muted, want)
			}
		}
		if zeros == 0 {
			t.Errorf("%s: the fake vocabulary has zero-count values, none was listed", path)
		}
	}
}

// Superseded (Pierre, 2026-10-01, AOC-065): the filter pane is exactly the validated design's five
// sections, so the currency picker this test pinned (AOC-049 criterion 3) left the pane. Currency
// still filters by link and /v1 — TestTheOtherFiltersStillApplyAsPills pins that.

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
