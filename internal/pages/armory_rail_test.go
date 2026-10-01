package pages_test

import (
	"context"
	"encoding/json"
	"html"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/pierrehrt/aoc-api/internal/assets"
	"github.com/pierrehrt/aoc-api/internal/httpx"
	"github.com/pierrehrt/aoc-api/internal/items"
	"github.com/pierrehrt/aoc-api/internal/pages"
	"github.com/pierrehrt/aoc-api/internal/templates"
)

// The filter rail (AOC-049), against the fake corpus. The counts' correctness is pinned against real
// SQL in internal/db; what is pinned here is that the page prints them, keeps every state in its
// URLs, and works as a plain form.

// everyFilter is a state with every filter the parser accepts set — the page's links and its form
// must carry all of them.
const everyFilter = "q=Item&rarity=epic&item_type=test-type&equip_location=test-slot-head&armour_weight=test-weight-light" +
	"&class=test-class&region=test-region&tier=test-tier&place=test-cave&place=test-lair&pvp=true&unchained=false" +
	"&ilvl_min=10&ilvl_max=90&reqlvl_min=5&reqlvl_max=80&price=true&currency=test-token&set=test-set-omega&sort=name"

func TestTheRailIsAFormThatWorksWithoutJavaScript(t *testing.T) {
	body := get(t, router(t), http.MethodGet, "/armory", nil, "").Body.String()
	for _, want := range []string{
		`<form method="get" action="/armory"`,
		// AOC-064: the facets are checkbox groups (several may be ticked; none ticked is any) —
		// no "Any" choice, nothing ticked by default
		`<input type="checkbox" id="f-rarity-epic" name="rarity" value="epic" data-count="120" class="peer sr-only">`,
		`id="f-equip_location-test-slot-head"`, `id="f-armour_weight-test-weight-light"`, `id="f-class-test-class"`,
		// the item level as two stacked sliders, with the design's note
		`id="f-ilvl_min-range"`, `id="f-ilvl_max-range"`, "items with no recorded level drop out once narrowed",
		// a class shows its short name and says its full name to a screen reader
		`<span aria-hidden="true">TC</span><span class="sr-only">Test Class</span>`,
		// the submit carries the result count (design 1b)
		`>Show 120 items</button>`,
		// fieldsets with legends: a group's semantic container
		`>Class restriction</legend>`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("missing %q", want)
		}
	}
	// ⭐ Exactly the design's five sections, in its order — and nothing else (Pierre, 2026-10-01).
	var legends []string
	for _, m := range regexp.MustCompile(`<legend[^>]*>([A-Za-z ]+)`).FindAllStringSubmatch(body, -1) {
		legends = append(legends, strings.TrimSpace(m[1]))
	}
	if strings.Join(legends, " | ") != "Rarity | Slot | Armour weight | Class restriction | Item level" {
		t.Errorf("the pane's sections are %v, want exactly the design's five", legends)
	}
	for _, gone := range []string{`name="price"`, `name="reqlvl_min"`, `<select`, "Vendor price", "Required level"} {
		if strings.Contains(body, gone) {
			t.Errorf("the pane still carries %q — not in the design", gone)
		}
	}
	// A 0 is listed, never hidden. A rarity keeps its colour at 0, as the design draws "the name in its
	// colour" (AOC-065 verify round 1); a chip at 0 is muted (TestEveryChipIsMutedExactlyWhenItsCountIsZero).
	if !regexp.MustCompile(`(?s)id="f-rarity-test-rarity-dull"[^>]*data-count="0"[^>]*>.*?>Test Rarity Dull</span>\s*<span class="flex-1"></span>\s*<span[^>]*>0</span>`).MatchString(body) {
		t.Error("the 0-count rarity is not listed with its 0")
	}
	if !regexp.MustCompile(`id="f-rarity-epic"[^>]*>(?s:.*?)style="color: var\(--color-rarity-epic\)"`).MatchString(body) {
		t.Error("a rarity row does not carry its colour")
	}
	// The phone sheet's switch has no name: never submitted, never in the URL (design 1b).
	if m := regexp.MustCompile(`<input[^>]*id="filter-sheet"[^>]*>`).FindString(body); m == "" || strings.Contains(m, "name=") {
		t.Errorf("the sheet toggle is %q — it must exist and carry no name", m)
	}
	// The rail is live with HTMX: one request on change, the whole form, into the results.
	if !strings.Contains(body, `hx-get="/armory" hx-trigger="change" hx-include="closest form" hx-target="#results" hx-push-url="true"`) {
		t.Error("the rail does not re-render on change")
	}
	if strings.Contains(body, "<details") {
		t.Error("a <details> is back: its open state would reset on every live swap (build decision 4)")
	}
	// No active filter: no pills, no count on the phone button, the strip reads "0 active".
	if strings.Contains(body, `aria-label="Remove the filter`) || strings.Contains(body, `<span id="filter-count"> · `) ||
		!strings.Contains(body, `<span id="strip-active">0 active</span>`) {
		t.Error("an unfiltered page shows active-filter UI")
	}
}

func TestActiveFiltersAreChipsThatEachRemoveOneFilter(t *testing.T) {
	body := get(t, router(t), http.MethodGet, "/armory?rarity=epic&ilvl_min=70&price=true&region=test-region&sort=name", nil, "").Body.String()
	chips := regexp.MustCompile(`<a href="([^"]+)" hx-get="[^"]+" hx-target="#results" hx-push-url="true" aria-label="Remove the filter ([^"]+)"`).FindAllStringSubmatch(body, -1)
	removes := map[string]string{"rarity: Test Epic": "rarity", "ilvl 70–80": "ilvl_min", "vendor price: yes": "price", "region: test-region": "region"}
	if len(chips) != len(removes) {
		t.Fatalf("got %d chips, want %d: %v", len(chips), len(removes), chips)
	}
	for _, c := range chips {
		label := html.UnescapeString(c[2])
		param, ok := removes[label]
		if !ok {
			t.Errorf("unexpected chip %q", label)
			continue
		}
		u, _ := url.Parse(html.UnescapeString(c[1]))
		q := u.Query()
		if q.Has(param) {
			t.Errorf("chip %q links to %s, which still has %s", label, c[1], param)
		}
		// …and keeps everything else, the sort included.
		for _, keep := range []string{"rarity", "ilvl_min", "price", "region", "sort"} {
			if keep != param && !q.Has(keep) {
				t.Errorf("chip %q drops %s too: %s", label, keep, c[1])
			}
		}
	}
	if m := regexp.MustCompile(`<a href="([^"]+)"[^>]*>clear all</a>`).FindStringSubmatch(body); m == nil || html.UnescapeString(m[1]) != "/armory?sort=name" {
		t.Errorf("clear all = %v, must keep the sort and drop every filter", m)
	}
	if !strings.Contains(body, `<span id="filter-count"> · 4</span>`) {
		t.Error("the phone's Filters button does not carry the active count")
	}
	// The state is in the form: the chosen value ticked, the non-rail filter and the sort hidden.
	for _, want := range []string{
		`id="f-rarity-epic" name="rarity" value="epic" checked`, `<input type="hidden" name="price" value="true">`,
		`name="ilvl_min" value="70"`, `<input type="hidden" name="region" value="test-region">`,
		`<input type="hidden" name="sort" value="name">`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("missing %q", want)
		}
	}
}

// ⭐ Every state is a URL: the pager, the sort links and the canonical carry every filter. Before
// AOC-049 the page's links knew only q and sort, so a filtered page 2 linked to an unfiltered page 3.
func TestEveryFilterSurvivesThePagesLinks(t *testing.T) {
	carries := func(where, link string, want url.Values) {
		t.Helper()
		u, _ := url.Parse(html.UnescapeString(link))
		got := u.Query()
		for k, vs := range want {
			if strings.Join(got[k], ",") != strings.Join(vs, ",") {
				t.Errorf("%s %s: %s = %v, want %v", where, link, k, got[k], vs)
			}
		}
	}
	canonical := regexp.MustCompile(`<link rel="canonical" href="https://aoc-codex.app(/armory\?[^"]+)">`)

	// The canonical, for the full state.
	want, _ := url.ParseQuery(everyFilter)
	if c := canonical.FindStringSubmatch(get(t, router(t), http.MethodGet, "/armory?"+everyFilter+"&p=2", nil, "").Body.String()); c == nil {
		t.Fatal("no canonical")
	} else {
		carries("canonical", c[1], want)
	}

	// The pager and the sort links. Without `place`: naming places makes the list one row per named
	// place, and the fake corpus has none, so that page has no rows to page through.
	state := strings.Replace(everyFilter, "&place=test-cave&place=test-lair", "", 1)
	want, _ = url.ParseQuery(state)
	body := get(t, router(t), http.MethodGet, "/armory?"+state+"&p=2", nil, "").Body.String()
	pager := regexp.MustCompile(`href="(/armory\?[^"]*&amp;p=[13][^"]*)"`).FindAllStringSubmatch(body, -1)
	if len(pager) == 0 {
		t.Fatal("no pager links")
	}
	for _, l := range pager {
		carries("pager", l[1], want)
	}
	sorts := regexp.MustCompile(`href="(/armory\?[^"]*sort=id[^"]*)"`).FindAllStringSubmatch(body, -1)
	if len(sorts) == 0 {
		t.Fatal("no sort link")
	}
	want.Set("sort", "id")
	carries("sort", sorts[0][1], want)
}

// The form carries every filter of the state it shows — through a control or a hidden input — so a
// submit (JS off) or a change (JS on) never drops one the reader did not touch.
func TestTheFormCarriesEveryFilter(t *testing.T) {
	body := get(t, router(t), http.MethodGet, "/armory?"+everyFilter, nil, "").Body.String()
	form := body[strings.Index(body, `<form method="get"`):strings.Index(body, `</form>`)]
	got := url.Values{}
	for _, m := range regexp.MustCompile(`<input type="hidden" name="([^"]+)" value="([^"]*)">`).FindAllStringSubmatch(form, -1) {
		got.Add(m[1], m[2])
	}
	for _, m := range regexp.MustCompile(`<input type="(?:radio|checkbox)" id="[^"]+" name="([^"]+)" value="([^"]*)" checked`).FindAllStringSubmatch(form, -1) {
		if m[2] != "" {
			got.Add(m[1], m[2])
		}
	}
	// A script-less submit (AOC-065): the level sliders' hidden inputs ship disabled and send nothing;
	// the <noscript> number inputs carry the bounds.
	for _, m := range regexp.MustCompile(`type="number" id="[^"]+" name="((?:ilvl|reqlvl)_(?:min|max))" value="(\d+)"`).FindAllStringSubmatch(form, -1) {
		got.Add(m[1], m[2])
	}
	for _, m := range regexp.MustCompile(`(?s)<select id="[^"]+" name="([^"]+)".*?<option value="([^"]+)" selected`).FindAllStringSubmatch(form, -1) {
		got.Add(m[1], m[2])
	}
	if m := regexp.MustCompile(`<input id="q" name="q" type="search" value="([^"]*)"`).FindStringSubmatch(form); m != nil {
		got.Add("q", m[1])
	}
	want, _ := url.ParseQuery(everyFilter)
	for k, vs := range want {
		g := append([]string{}, got[k]...)
		sort.Strings(g)
		w := append([]string{}, vs...)
		sort.Strings(w)
		if strings.Join(g, ",") != strings.Join(w, ",") {
			t.Errorf("the form submits %s=%v, the state has %v", k, got[k], vs)
		}
	}
}

// One HTMX request answers the rows AND the rail, and pushes the state's own URL — not the form's
// raw query string with its empty fields.
func TestTheHTMXAnswerCarriesTheRowsAndTheRail(t *testing.T) {
	rr := get(t, router(t), http.MethodGet, "/armory?q=&rarity=epic&ilvl_min=&ilvl_max=&currency=&set=&price=", map[string]string{"HX-Request": "true"}, "")
	if rr.Code != http.StatusOK {
		t.Fatalf("status %d", rr.Code)
	}
	body := rr.Body.String()
	for _, want := range []string{
		"rows 1–50 of 120", // the rows
		`<div hx-swap-oob="innerHTML:#armory-facets">`, `<div hx-swap-oob="innerHTML:#armory-chips">`, `id="f-rarity-epic" name="rarity" value="epic" checked`,
		`<span hx-swap-oob="innerHTML:#filter-count"> · 1</span>`,
		"rarity: Test Epic", // the chip
	} {
		if !strings.Contains(body, want) {
			t.Errorf("the HTMX answer is missing %q", want)
		}
	}
	if got := rr.Header().Get("HX-Push-Url"); got != "/armory?rarity=epic" {
		t.Errorf("HX-Push-Url = %q, want /armory?rarity=epic", got)
	}
	if strings.Contains(body, "<html") || strings.Contains(body, "<form") {
		t.Error("an HTMX request got the page, not the fragment")
	}
}

// A range that is empty by construction, or a level that is not one, is a 400 on the page as on /v1.
func TestABadRangeIs400OnThePage(t *testing.T) {
	for _, q := range []string{"ilvl_min=80&ilvl_max=70", "reqlvl_min=x", "ilvl_max=-1", "price=maybe"} {
		rr := get(t, router(t), http.MethodGet, "/armory?"+q, nil, "")
		if rr.Code != http.StatusBadRequest || !strings.HasPrefix(rr.Header().Get("Content-Type"), "text/html") {
			t.Errorf("%s: %d %s, want a 400 HTML page", q, rr.Code, rr.Header().Get("Content-Type"))
		}
	}
	// An unknown slug is not malformed: a chip names it, it stays chosen, and the page answers.
	rr := get(t, router(t), http.MethodGet, "/armory?currency=not-a-currency", nil, "")
	if rr.Code != http.StatusOK || !strings.Contains(rr.Body.String(), "currency: not-a-currency") ||
		!strings.Contains(rr.Body.String(), `<input type="hidden" name="currency" value="not-a-currency">`) {
		t.Errorf("an unknown currency: %d, chip or kept value missing", rr.Code)
	}
}

type stubTaxonomies struct{}

func (stubTaxonomies) Taxonomies(context.Context) (items.Taxonomies, error) {
	return items.Taxonomies{}, nil
}

// ⭐ THE CRITERION: /v1/items?facets=1 returns the counts the page shows. One service behind both
// surfaces, as in production; every number the rail prints is compared with the JSON's.
func TestThePageAndTheJSONShowTheSameCounts(t *testing.T) {
	set, err := assets.Load()
	if err != nil {
		t.Fatal(err)
	}
	tpl, err := templates.New(set)
	if err != nil {
		t.Fatal(err)
	}
	svc := items.NewService(newFakeItems(120))
	api := items.NewHandler(svc, stubTaxonomies{})
	h := httpx.NewRouterWithAPI(httpx.Build{Version: "1.2.3", Commit: "abc1234", Env: "test"}, pages.New(tpl, set, base, svc).Routes, set.Handler(),
		func(v1 chi.Router) { v1.Mount("/items", api.Routes()) })

	for _, state := range []string{"", "rarity=epic&price=false&ilvl_min=10"} {
		page := get(t, h, http.MethodGet, "/armory?"+state, nil, "").Body.String()
		var env struct {
			Facets items.Facets `json:"facets"`
		}
		rr := get(t, h, http.MethodGet, "/v1/items?facets=1&"+state, nil, "")
		if err := json.Unmarshal(rr.Body.Bytes(), &env); err != nil {
			t.Fatalf("%s: %v", state, err)
		}
		want := map[string]int64{} // "param=value" -> count, from the JSON — for the design's four groups
		for param, g := range map[string]items.FacetGroup{"rarity": env.Facets.Rarity, "equip_location": env.Facets.EquipLocation,
			"armour_weight": env.Facets.ArmourWeight, "class": env.Facets.Class} {
			for _, v := range g.Values {
				want[param+"="+v.Slug] = v.Count
			}
		}

		got := map[string]int64{}
		for _, m := range regexp.MustCompile(`<input type="(?:radio|checkbox)" id="[^"]+" name="([^"]+)" value="([^"]*)"[^>]*data-count="(\d+)"`).FindAllStringSubmatch(page, -1) {
			n, _ := strconv.ParseInt(m[3], 10, 64)
			got[m[1]+"="+m[2]] = n
		}
		for _, sel := range regexp.MustCompile(`(?s)<select id="[^"]+" name="([^"]+)"[^>]*>(.*?)</select>`).FindAllStringSubmatch(page, -1) {
			for _, o := range regexp.MustCompile(`<option value="([^"]*)"[^>]*>[^<]*\(([\d,]+)\)</option>`).FindAllStringSubmatch(sel[2], -1) {
				n, _ := strconv.ParseInt(strings.ReplaceAll(o[2], ",", ""), 10, 64)
				got[sel[1]+"="+o[1]] = n
			}
		}
		if len(got) != len(want) {
			t.Errorf("%q: the page shows %d counts, the JSON has %d", state, len(got), len(want))
		}
		for k, n := range want {
			if g, ok := got[k]; !ok || g != n {
				t.Errorf("%q: %s — page %d (shown: %v), JSON %d", state, k, g, ok, n)
			}
		}
	}
}

// AOC-049 review: with JavaScript on, a malformed range from the rail used to answer a 400 that htmx
// discarded — the rail looked dead and said nothing. Now the reason comes back as a fragment for
// #results, the rail is not redrawn (the bad value stays to be fixed), and no URL is pushed.
func TestALiveRequestWithABadRangeSaysWhy(t *testing.T) {
	rr := get(t, router(t), http.MethodGet, "/armory?rarity=epic&ilvl_min=80&ilvl_max=60", map[string]string{"HX-Request": "true"}, "")
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("status %d, want 400", rr.Code)
	}
	body := rr.Body.String()
	for _, want := range []string{`role="alert"`, "That search is not valid.", "ilvl_min (80) is above ilvl_max (60)"} {
		if !strings.Contains(body, want) {
			t.Errorf("missing %q in %s", want, body)
		}
	}
	if strings.Contains(body, "hx-swap-oob") || strings.Contains(body, "<html") {
		t.Error("the rejection redraws the rail or is a whole page — the reader's input would be lost")
	}
	if got := rr.Header().Get("HX-Push-Url"); got != "false" {
		t.Errorf("HX-Push-Url = %q, want \"false\" — without it htmx pushes the invalid request's URL", got)
	}
	if v := rr.Header().Get("Vary"); !strings.Contains(v, "HX-Request") {
		t.Errorf("Vary = %q", v)
	}
	// Without JavaScript, the same reason on the rejection page.
	if b := get(t, router(t), http.MethodGet, "/armory?ilvl_min=80&ilvl_max=60", nil, "").Body.String(); !strings.Contains(b, "ilvl_min (80) is above ilvl_max (60)") {
		t.Error("the JS-off rejection page does not say why")
	}
}

// htmx discards every 4xx unless told otherwise: the page tells it to swap a 400 — and nothing else
// that it did not swap before.
func TestHTMXIsToldToSwapA400(t *testing.T) {
	body := get(t, router(t), http.MethodGet, "/armory", nil, "").Body.String()
	m := regexp.MustCompile(`<meta name="htmx-config" content='([^']+)'>`).FindStringSubmatch(body)
	if m == nil {
		t.Fatal("no htmx-config meta")
	}
	var cfg struct {
		RefreshOnHistoryMiss bool `json:"refreshOnHistoryMiss"`
		HistoryCacheSize     *int `json:"historyCacheSize"`
		ResponseHandling     []struct {
			Code  string `json:"code"`
			Swap  bool   `json:"swap"`
			Error bool   `json:"error"`
		} `json:"responseHandling"`
	}
	if err := json.Unmarshal([]byte(html.UnescapeString(m[1])), &cfg); err != nil {
		t.Fatalf("htmx-config is not JSON: %v", err)
	}
	// AOC-063: Back to a state htmx has no snapshot of must reload the page, not re-request it as an
	// HTMX request — every handler answers that with a fragment, and htmx put it in <body>.
	if !cfg.RefreshOnHistoryMiss {
		t.Error("htmx-config does not set refreshOnHistoryMiss — Back after a history miss leaves bare rows")
	}
	// AOC-065: and htmx keeps no snapshot, so every Back is that miss: a snapshot is the live DOM when
	// the next answer lands, which can hold a slider's half-applied value, and Back restored it.
	if cfg.HistoryCacheSize == nil || *cfg.HistoryCacheSize != 0 {
		t.Errorf("htmx-config's historyCacheSize is %v, want 0", cfg.HistoryCacheSize)
	}
	// First match wins in htmx: walk the list as htmx does.
	swaps := func(status string) bool {
		for _, r := range cfg.ResponseHandling {
			if regexp.MustCompile("^" + r.Code + "$").MatchString(status) {
				return r.Swap
			}
		}
		return false
	}
	for status, want := range map[string]bool{"200": true, "204": false, "400": true, "404": false, "500": false} {
		if swaps(status) != want {
			t.Errorf("a %s is swapped=%v, want %v", status, swaps(status), want)
		}
	}
	// A change the reader makes while a request is in flight is never wiped by that request's
	// redraw: every request in the form aborts the one in flight (hx-sync on the form, inherited), and
	// an answer landing mid-drag skips the pane (AOC-065; behaviour measured in a browser).
	if !strings.Contains(body, `<form method="get" action="/armory" class="armory flex flex-col lg:min-h-0 lg:flex-1" hx-sync="this:replace">`) {
		t.Error("the form does not sync its requests with replace")
	}
	for _, want := range []string{`document.addEventListener("htmx:oobBeforeSwap"`, `e.detail.shouldSwap = false`,
		`aocPane.addEventListener("pointerdown"`, `source: "#results"`,
		// a link's or Cancel's request sets the form to its state as it goes (AOC-065, P3)
		`if (e.detail.elt !== aocScroll) aocFormFrom(e.detail.requestConfig.path);`} {
		if !strings.Contains(body, want) {
			t.Errorf("the pane's drag rule is missing %q", want)
		}
	}
}

// AOC-064: several values in a group — both boxes ticked, one chip each, and each chip's × removes
// only its own value (the others, and the other filters, stay).
func TestSeveralValuesAreTickedAndEachIsItsOwnChip(t *testing.T) {
	body := get(t, router(t), http.MethodGet, "/armory?rarity=epic&rarity=test-rarity-dull&class=test-class&sort=name", nil, "").Body.String()
	for _, want := range []string{
		`<input type="checkbox" id="f-rarity-epic" name="rarity" value="epic" checked`,
		`<input type="checkbox" id="f-rarity-test-rarity-dull" name="rarity" value="test-rarity-dull" checked`,
		`<input type="checkbox" id="f-class-test-class" name="class" value="test-class" checked`,
		`<span id="filter-count"> · 3</span>`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("missing %q", want)
		}
	}
	chips := regexp.MustCompile(`<a href="([^"]+)" hx-get="[^"]+" hx-target="#results" hx-push-url="true" aria-label="Remove the filter ([^"]+)"`).FindAllStringSubmatch(body, -1)
	keeps := map[string][]string{
		"rarity: Test Epic":        {"test-rarity-dull"},
		"rarity: Test Rarity Dull": {"epic"},
	}
	seen := 0
	for _, c := range chips {
		label := html.UnescapeString(c[2])
		want, ok := keeps[label]
		if !ok {
			continue
		}
		seen++
		u, _ := url.Parse(html.UnescapeString(c[1]))
		q := u.Query()
		if strings.Join(q["rarity"], ",") != strings.Join(want, ",") {
			t.Errorf("chip %q leaves rarity=%v, want %v", label, q["rarity"], want)
		}
		if q.Get("class") != "test-class" || q.Get("sort") != "name" {
			t.Errorf("chip %q drops another filter: %s", label, c[1])
		}
	}
	if seen != 2 {
		t.Errorf("found %d rarity chips, want one per ticked value (2)", seen)
	}
}

// AOC-065: each level bound has two inputs, and exactly one may submit. The sliders' hidden input
// ships DISABLED (data-js-enable); the number input for a script-less reader ships enabled
// (data-js-disable), visible only without scripts (.no-js-only). The page's script flips both at
// load and for every pane htmx swaps in — so the guarantee holds after a live update, where the
// <noscript> inputs this replaced had become live fields (verify round 1: every bound sent twice).
// The browser half is measured in the ticket's Log; here, the markup and the script that carry it.
func TestTheLevelSlidersNeverDoubleABound(t *testing.T) {
	h := router(t)
	// Each bound has exactly two inputs: the hidden one the script switches on, and the number input it
	// switches off — so one submits in each mode. In every state: also when the other filters leave no
	// item with a level and there are no sliders (AOC-065 delta verify, F16: the hidden inputs were
	// drawn only with the sliders, so with scripts on the bound was dropped). One fake item leaves no
	// item-level span.
	for _, c := range []struct {
		name string
		h    http.Handler
		span bool
	}{{"with a span", h, true}, {"with no span", routerWith(t, newFakeItems(1), 0), false}} {
		for _, hdr := range []map[string]string{nil, {"HX-Request": "true"}} { // the page, and a live update
			body := get(t, c.h, http.MethodGet, "/armory?ilvl_min=10", hdr, "").Body.String()
			if strings.Contains(body, "<noscript>") {
				t.Error("a <noscript> is back: htmx parses it into live fields on a swap")
			}
			for _, name := range []string{"ilvl_min", "ilvl_max"} {
				all := regexp.MustCompile(`<input [^>]*name="`+name+`"[^>]*>`).FindAllString(body, -1)
				hidden := regexp.MustCompile(`<input type="hidden" name="` + name + `" value="([^"]*)"([^>]*)>`).FindStringSubmatch(body)
				if len(all) != 2 || hidden == nil || !strings.Contains(hidden[2], "data-js-enable") || !strings.Contains(hidden[2], "disabled") {
					t.Errorf("%s, HX %v, %s: want the hidden input (disabled, data-js-enable) and the number input, got %q", c.name, hdr != nil, name, all)
					continue
				}
				if want := map[string]string{"ilvl_min": "10", "ilvl_max": ""}[name]; hidden[1] != want {
					t.Errorf("%s, HX %v: the hidden %s carries %q, want %q", c.name, hdr != nil, name, hidden[1], want)
				}
				number := regexp.MustCompile(`<input type="number" id="f-` + name + `" name="` + name + `"[^>]*>`).FindString(body)
				if number == "" || !strings.Contains(number, "data-js-disable") || strings.Contains(number, "disabled") {
					t.Errorf("%s, HX %v: the %s number input must ship enabled and data-js-disable: %q", c.name, hdr != nil, name, number)
				}
			}
			// The legend says the range: closed by the span, or open where there is none.
			wantLabel := map[bool]string{true: "10 – 80", false: "≥ 10"}[c.span]
			if !strings.Contains(body, `<span data-range-label>`+wantLabel+`</span>`) {
				t.Errorf("%s, HX %v: the legend does not read %q", c.name, hdr != nil, wantLabel)
			}
			if got := strings.Contains(body, `data-bound="ilvl_min"`); got != c.span {
				t.Errorf("%s, HX %v: a slider is drawn = %v", c.name, hdr != nil, got)
			}
		}
	}
	page := get(t, h, http.MethodGet, "/armory", nil, "").Body.String()
	for _, want := range []string{`[data-js-disable]").forEach(function (i) { i.disabled = true; })`, `document.addEventListener("htmx:load"`} {
		if !strings.Contains(page, want) {
			t.Errorf("the page's script does not %q", want)
		}
	}
	if !strings.Contains(page, `data-bound="ilvl_min" data-end="min"`) {
		t.Error("the item level slider is missing")
	}
}

// A bound beyond the span the other filters leave (the fake corpus's is 80–80) reads open, never as a
// backwards range: "≥ 85", not "85 – 80". It is what a reader who moved a slider while the span
// narrowed has set, and it applies as set.
func TestABoundBeyondTheSpanReadsOpen(t *testing.T) {
	h := router(t)
	for q, want := range map[string][2]string{
		"ilvl_min=85":             {"≥ 85", "ilvl ≥ 85"},
		"ilvl_max=70":             {"≤ 70", "ilvl ≤ 70"},
		"ilvl_min=60":             {"60 – 80", "ilvl 60–80"},
		"ilvl_min=60&ilvl_max=95": {"60 – 95", "ilvl 60–95"},
	} {
		body := get(t, h, http.MethodGet, "/armory?"+q, nil, "").Body.String()
		if !strings.Contains(body, `<span data-range-label>`+want[0]+`</span>`) {
			t.Errorf("%s: the legend does not read %q", q, want[0])
		}
		if !strings.Contains(body, `aria-label="Remove the filter `+want[1]+`"`) {
			t.Errorf("%s: no pill %q", q, want[1])
		}
	}
}

// Pierre, 2026-10-01: vendor price, required level, currency and set left the pane (the design has
// them not). They still filter: an active one is carried by the form as a hidden input — so a change
// in the pane keeps it — and shows as a pill whose × removes it.
func TestTheOtherFiltersStillApplyAsPills(t *testing.T) {
	body := get(t, router(t), http.MethodGet, "/armory?price=true&reqlvl_min=10&reqlvl_max=50&currency=test-token&set=test-set-omega", nil, "").Body.String()
	for _, want := range []string{
		`<input type="hidden" name="price" value="true">`, `<input type="hidden" name="reqlvl_min" value="10">`,
		`<input type="hidden" name="reqlvl_max" value="50">`, `<input type="hidden" name="currency" value="test-token">`,
		`<input type="hidden" name="set" value="test-set-omega">`,
		`aria-label="Remove the filter vendor price: yes"`, `aria-label="Remove the filter reqlvl 10–50"`,
		`aria-label="Remove the filter currency: Test Token"`, `aria-label="Remove the filter set: Test Set Omega"`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("missing %q", want)
		}
	}
}

// AOC-064 verify F4: a list's values are any of them, in any order — so a state has one canonical URL,
// whatever order its values arrived in.
func TestOneStateHasOneCanonicalWhateverTheOrder(t *testing.T) {
	canon := regexp.MustCompile(`<link rel="canonical" href="([^"]+)">`)
	a := canon.FindStringSubmatch(get(t, router(t), http.MethodGet, "/armory?rarity=test-rarity-dull&rarity=epic&class=test-class-two,test-class", nil, "").Body.String())
	b := canon.FindStringSubmatch(get(t, router(t), http.MethodGet, "/armory?class=test-class&class=test-class-two&rarity=epic,test-rarity-dull", nil, "").Body.String())
	if a == nil || b == nil || a[1] != b[1] {
		t.Errorf("one state, two canonicals: %v / %v", a, b)
	}
}

// The pane ends with its Apply bar — a script-less reader's submit and the phone sheet's way out, with
// Reset beside it — after the sections, never inside one. The F13 clean-up (AOC-065 verify round 1)
// took it out with the dead select block, and the page still rendered and every test still passed;
// the divs balancing, page and fragment, is what shows a lost closing tag.
func TestThePaneEndsWithItsApplyBar(t *testing.T) {
	body := get(t, router(t), http.MethodGet, "/armory?rarity=epic", nil, "").Body.String()
	aside := regexp.MustCompile(`(?s)<aside aria-label="Filters".*?</aside>`).FindString(body)
	bar := regexp.MustCompile(`(?s)</fieldset>\s*</div>\s*<div class="apply-bar[^"]*">(.*?)</div>\s*</div>\s*</div>\s*</aside>`).FindStringSubmatch(aside)
	if bar == nil {
		t.Fatalf("the pane does not end with its Apply bar after the sections:\n%s", aside)
	}
	for _, want := range []string{`<a href="/armory" `, `>Reset</a>`, `<button type="submit"`, `>Show 120 items</button>`,
		// the phone's Cancel (canvas 1b) is a reset…
		`<button type="reset" id="sheet-cancel"`, `>Cancel</button>`} {
		if !strings.Contains(bar[1], want) {
			t.Errorf("the Apply bar is missing %q:\n%s", want, bar[1])
		}
	}
	// …and closes the sheet only because the sheet's checkbox, shipped unticked, is in the same form.
	form := regexp.MustCompile(`(?s)<form method="get" action="/armory".*?</form>`).FindString(body)
	toggle := regexp.MustCompile(`<input type="checkbox" id="filter-sheet"[^>]*>`).FindString(form)
	if toggle == "" || strings.Contains(toggle, "checked") {
		t.Errorf("the sheet's checkbox is %q — it must be in the form, unticked, for Cancel to close the sheet", toggle)
	}
	hx := get(t, router(t), http.MethodGet, "/armory?rarity=epic", map[string]string{"HX-Request": "true"}, "").Body.String()
	for name, b := range map[string]string{"page": body, "HTMX answer": hx} {
		if o, c := strings.Count(b, "<div"), strings.Count(b, "</div>"); o != c {
			t.Errorf("the %s opens %d divs and closes %d", name, o, c)
		}
	}
}
