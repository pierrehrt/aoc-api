package pages_test

// AOC-064 verify round 1: the rail with several values in a group, beyond what the build tested —
// a select (currency, set) carries its further values as hidden inputs and chips; every chip's URL
// is the state minus exactly its own value; "clear all" drops every value of every group; a JS-off
// submit of a several-value state is that same state; a comma list with an empty element reads like
// the repeated form. Slugs are the fake corpus's test vocabulary (fake_items_test.go).

import (
	"html"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strings"
	"testing"
)

const severalValues = "/armory?rarity=epic&rarity=test-rarity-dull&equip_location=test-slot-head&equip_location=test-slot-feet" +
	"&class=test-class&currency=test-token&currency=test-coin&sort=name"

var chipRe = regexp.MustCompile(`<a href="([^"]+)" hx-get="[^"]+" hx-target="#results" hx-push-url="true" aria-label="Remove the filter ([^"]+)"`)

// pairs is a query as a sorted list of "k=v", empties dropped — a multiset, so a duplicated or a lost
// value shows.
func pairs(q url.Values) []string {
	var out []string
	for k, vs := range q {
		for _, v := range vs {
			if v != "" {
				out = append(out, k+"="+v)
			}
		}
	}
	sort.Strings(out)
	return out
}

// minus is a minus b as multisets.
func minus(a, b []string) []string {
	left := map[string]int{}
	for _, x := range b {
		left[x]++
	}
	var out []string
	for _, x := range a {
		if left[x] > 0 {
			left[x]--
			continue
		}
		out = append(out, x)
	}
	return out
}

func TestEveryChipIsTheStateMinusExactlyItsOwnValue(t *testing.T) {
	h := router(t)
	body := get(t, h, http.MethodGet, severalValues, nil, "").Body.String()
	state, _ := url.Parse(severalValues)
	want := pairs(state.Query())

	chips := chipRe.FindAllStringSubmatch(body, -1)
	// One chip per filter value: 2 rarities + 2 slots + 1 class + 2 currencies. The sort is not a filter.
	if len(chips) != 7 {
		t.Fatalf("%d chips, want 7 — one per chosen value", len(chips))
	}
	removed := map[string]bool{}
	for _, c := range chips {
		u, err := url.Parse(html.UnescapeString(c[1]))
		if err != nil {
			t.Fatal(err)
		}
		gone := minus(want, pairs(u.Query()))
		extra := minus(pairs(u.Query()), want)
		if len(gone) != 1 || len(extra) != 0 {
			t.Errorf("chip %q: its URL drops %v and adds %v, want exactly one value dropped", html.UnescapeString(c[2]), gone, extra)
			continue
		}
		if removed[gone[0]] {
			t.Errorf("two chips both remove %s", gone[0])
		}
		removed[gone[0]] = true
		// The chip's URL is a state of its own: it loads, with one chip fewer.
		next := get(t, h, http.MethodGet, u.String(), nil, "")
		if next.Code != http.StatusOK {
			t.Errorf("chip %q leads to %d", c[2], next.Code)
		} else if n := len(chipRe.FindAllStringSubmatch(next.Body.String(), -1)); n != 6 {
			t.Errorf("chip %q leads to a page with %d chips, want 6", c[2], n)
		}
	}
	if len(removed) != 7 {
		t.Errorf("the chips remove %d distinct values, want 7: %v", len(removed), removed)
	}

	// "clear all" drops every value of every group, and keeps the sort.
	m := regexp.MustCompile(`<a href="([^"]+)"[^>]*>clear all</a>`).FindStringSubmatch(body)
	if m == nil {
		t.Fatal("no clear all")
	}
	u, _ := url.Parse(html.UnescapeString(m[1]))
	if got := pairs(u.Query()); strings.Join(got, "&") != "sort=name" {
		t.Errorf("clear all leaves %v, want only sort=name", got)
	}
}

// A select holds one value. The rest of a URL's values for it must not be lost on the next submit:
// they ride along as hidden inputs, and each is its own chip.
func TestASelectsFurtherValuesRideAlongAsHiddenInputsAndChips(t *testing.T) {
	// AOC-065 (Pierre, 2026-10-01): currency left the pane — the design has no currency control — so
	// EVERY chosen currency rides along as a hidden input (once each), and each is its own chip.
	body := get(t, router(t), http.MethodGet, "/armory?currency=test-token&currency=test-coin&sort=name", nil, "").Body.String()
	form := body[strings.Index(body, `<form method="get"`):strings.Index(body, `</form>`)]
	if strings.Contains(form, `<select`) {
		t.Error("a currency select is back in the pane — the design has none")
	}
	for _, v := range []string{"test-token", "test-coin"} {
		if n := strings.Count(form, `<input type="hidden" name="currency" value="`+v+`">`); n != 1 {
			t.Errorf("currency %s is carried %d times, want once — a submit would drop or double it", v, n)
		}
	}
	labels := map[string]bool{}
	for _, c := range chipRe.FindAllStringSubmatch(body, -1) {
		labels[html.UnescapeString(c[2])] = true
	}
	for _, want := range []string{"currency: Test Token", "currency: Test Coin"} {
		if !labels[want] {
			t.Errorf("no chip %q (chips: %v)", want, labels)
		}
	}
}

// Criterion "every state is a URL", JS off, several values: what a browser submits from the rendered
// form, untouched, is the same state — every ticked box, the select's value and its hidden extras.
func TestAJSOffSubmitOfSeveralValuesIsTheSameState(t *testing.T) {
	body := get(t, router(t), http.MethodGet, severalValues, nil, "").Body.String()
	form := body[strings.Index(body, `<form method="get"`):strings.Index(body, `</form>`)]
	sent := url.Values{}
	for _, tag := range regexp.MustCompile(`<input [^>]*>`).FindAllString(form, -1) {
		attr := func(k string) string {
			m := regexp.MustCompile(` ` + k + `="([^"]*)"`).FindStringSubmatch(tag)
			if m == nil {
				return ""
			}
			return html.UnescapeString(m[1])
		}
		name, typ := attr("name"), attr("type")
		if name == "" || typ == "submit" || typ == "button" {
			continue
		}
		if (typ == "checkbox" || typ == "radio") && !strings.Contains(tag, " checked") {
			continue
		}
		sent.Add(name, attr("value"))
	}
	for _, s := range regexp.MustCompile(`(?s)<select [^>]*name="([^"]+)"[^>]*>(.*?)</select>`).FindAllStringSubmatch(form, -1) {
		if m := regexp.MustCompile(`<option value="([^"]*)" selected`).FindStringSubmatch(s[2]); m != nil {
			sent.Add(s[1], html.UnescapeString(m[1]))
		} else if m := regexp.MustCompile(`<option value="([^"]*)"`).FindStringSubmatch(s[2]); m != nil {
			sent.Add(s[1], html.UnescapeString(m[1]))
		}
	}
	state, _ := url.Parse(severalValues)
	if got, want := pairs(sent), pairs(state.Query()); strings.Join(got, "&") != strings.Join(want, "&") {
		t.Errorf("a JS-off submit sends\n  %v\nwant the state\n  %v", got, want)
	}
}

// A comma list with an empty element reads like the repeated form: both boxes ticked, one chip each,
// and the canonical repeats the parameter.
func TestACommaListWithAnEmptyElementIsTheRepeatedForm(t *testing.T) {
	h := router(t)
	comma := get(t, h, http.MethodGet, "/armory?rarity=epic,,test-rarity-dull", nil, "").Body.String()
	for _, want := range []string{
		`<input type="checkbox" id="f-rarity-epic" name="rarity" value="epic" checked`,
		`<input type="checkbox" id="f-rarity-test-rarity-dull" name="rarity" value="test-rarity-dull" checked`,
		`<link rel="canonical" href="` + base + `/armory?rarity=epic&amp;rarity=test-rarity-dull">`,
	} {
		if !strings.Contains(comma, want) {
			t.Errorf("missing %q", want)
		}
	}
	if n := len(chipRe.FindAllStringSubmatch(comma, -1)); n != 2 {
		t.Errorf("%d chips, want 2", n)
	}
	if strings.Contains(comma, `id="f-rarity-unknown`) {
		t.Error("the empty element became an unknown value")
	}
}
