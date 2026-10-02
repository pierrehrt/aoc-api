package pages_test

// AOC-065 delta verify 2 — the server half of F16 for every combination of level bounds in a state
// where the other filters leave no item with a level (one fake item, which has none). Fake corpus
// only (fake_items_test.go); nothing here states a fact about the game.

import (
	"html"
	"net/http"
	"regexp"
	"strings"
	"testing"
)

// With no span there are no sliders, and each bound must still ride along: exactly one hidden input
// (shipped disabled, switched on by the script) and one number input (shipped enabled, switched off
// by the script), both carrying the URL's value — so exactly one submits with scripts and one
// without. The legend says the open range, and the pill says the same bound. The build's test covers
// a lower bound only; an upper bound alone, both, and neither are pinned here, on the page and on a
// live update.
func TestANoSpanStateCarriesEachBoundOnce(t *testing.T) {
	h := routerWith(t, newFakeItems(1), 0)
	for _, c := range []struct{ query, min, max, label, pill string }{
		{"", "", "", "", ""},
		{"ilvl_min=60", "60", "", "≥ 60", "ilvl ≥ 60"},
		{"ilvl_max=70", "", "70", "≤ 70", "ilvl ≤ 70"},
		{"ilvl_min=60&ilvl_max=85", "60", "85", "60 – 85", "ilvl 60–85"},
	} {
		for _, hdr := range []map[string]string{nil, {"HX-Request": "true"}} {
			where := "/armory?" + c.query
			if hdr != nil {
				where += " (live update)"
			}
			body := get(t, h, http.MethodGet, "/armory?"+c.query, hdr, "").Body.String()
			if strings.Contains(body, `type="range"`) {
				t.Errorf("%s: a slider is drawn, but no item here has a level", where)
			}
			for name, want := range map[string]string{"ilvl_min": c.min, "ilvl_max": c.max} {
				var hidden, number int
				for _, in := range regexp.MustCompile(`<input [^>]*name="`+name+`"[^>]*>`).FindAllString(body, -1) {
					value := regexp.MustCompile(`value="([^"]*)"`).FindStringSubmatch(in)
					if value == nil || value[1] != want {
						t.Errorf("%s: %s input carries %v, want %q: %s", where, name, value, want, in)
					}
					switch {
					case strings.HasPrefix(in, `<input type="hidden"`) && strings.Contains(in, "data-js-enable") && strings.Contains(in, " disabled"):
						hidden++
					case strings.HasPrefix(in, `<input type="number"`) && strings.Contains(in, "data-js-disable") && !strings.Contains(in, " disabled"):
						number++
					default:
						t.Errorf("%s: an unexpected %s input: %s", where, name, in)
					}
				}
				if hidden != 1 || number != 1 {
					t.Errorf("%s: %s has %d hidden and %d number inputs, want exactly one of each", where, name, hidden, number)
				}
			}
			text := html.UnescapeString(body)
			if !strings.Contains(text, `<span data-range-label>`+c.label+`</span>`) {
				t.Errorf("%s: the legend does not read %q", where, c.label)
			}
			if got := strings.Contains(text, "no item here has one"); got != (c.min == "" && c.max == "") {
				t.Errorf("%s: the \"no item here has one\" note shown = %v; it belongs only to a state with no bound", where, got)
			}
			hasPill := strings.Contains(text, `aria-label="Remove the filter ilvl`)
			if c.pill == "" && hasPill {
				t.Errorf("%s: an item-level pill with no bound", where)
			}
			if c.pill != "" && !strings.Contains(text, `aria-label="Remove the filter `+c.pill+`"`) {
				t.Errorf("%s: no pill %q", where, c.pill)
			}
		}
	}
}
