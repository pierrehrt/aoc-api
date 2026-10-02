package templates_test

import (
	"math"
	"os"
	"regexp"
	"strconv"
	"testing"
)

// ⭐ The palette is tested, not eyeballed (AOC-046). Every text token in web/src/app.css's @theme
// block must pass WCAG AA (4.5:1) against the page colour, because this site is read in a dark room
// at 1 a.m. by someone with three minutes — and the design's own note says the raw game colours
// FAIL (Epic 2.0:1, Rare 3.1:1) and were lightened for exactly this reason. A future hue tweak that
// slips under 4.5 fails here, not on a raid night.
func TestThemeTextTokensPassAA(t *testing.T) {
	src, err := os.ReadFile("../../web/src/app.css")
	if err != nil {
		t.Fatalf("read app.css: %v", err)
	}
	theme := string(src)
	// `@theme static`, so every token reaches the build (verify round 1); either spelling is the block.
	if loc := regexp.MustCompile(`@theme( static)? \{`).FindStringIndex(theme); loc != nil {
		theme = theme[loc[0]:]
	} else {
		t.Fatal("app.css has no @theme block")
	}
	tokens := map[string]string{}
	for _, m := range regexp.MustCompile(`--color-([a-z0-9-]+):\s*(#[0-9a-fA-F]{6})`).FindAllStringSubmatch(theme, -1) {
		tokens[m[1]] = m[2]
	}
	ink, ok := tokens["ink"]
	if !ok {
		t.Fatal("no --color-ink token — the page colour every text colour is measured against")
	}
	// Text tokens: everything that is not a surface or a border. A new surface token must be
	// added here on purpose, so that a text colour cannot be excused by being misnamed.
	surfaces := map[string]bool{"ink": true, "ink-header": true, "ink-pane": true, "ink-strip": true, "ink-hover": true,
		"ink-selected": true, "ink-nav": true, "ink-nav-hover": true, "ink-pill": true, "ink-pane-hover": true, "ink-tree-hover": true,
		"line": true, "line-row": true, "line-control": true, "line-chip": true, "line-soft": true, "line-box": true}
	// AOC-065: text sits on every pane of the full-window layout, not only on the page colour — each
	// pair is measured. (On ink-selected, only paper is ever written; it is measured below.)
	backgrounds := []string{"ink", "ink-header", "ink-pane", "ink-strip", "ink-hover", "ink-pane-hover"}
	if len(tokens) <= len(surfaces) {
		t.Fatalf("only %d colour tokens found; the theme is not where this test looks", len(tokens))
	}
	for name, hex := range tokens {
		if surfaces[name] {
			continue
		}
		for _, bg := range backgrounds {
			b, ok := tokens[bg]
			if !ok {
				t.Fatalf("no --color-%s surface token", bg)
			}
			if r := contrast(hex, b); r < 4.5 {
				t.Errorf("--color-%s %s on %s %s is %.2f:1 — below AA's 4.5:1", name, hex, bg, b, r)
			}
		}
	}
	// The few surfaces that carry only particular text: selected things and the active nav pill carry
	// paper; a nav pill under the pointer carries muted-2 or paper.
	// A source tree row under the pointer (AOC-068) carries its label (muted, or paper when open), and
	// its caret and count in muted-2 — never faint, which is 4.34:1 there. A selected row carries
	// paper and link on ink-selected.
	for _, pair := range [][2]string{{"paper", "ink-selected"}, {"paper", "ink-nav"}, {"paper", "ink-pill"}, {"paper", "ink-nav-hover"}, {"muted-2", "ink-nav-hover"},
		{"muted", "ink-tree-hover"}, {"muted-2", "ink-tree-hover"}, {"paper", "ink-tree-hover"}, {"link", "ink-tree-hover"}, {"link", "ink-selected"}} {
		if r := contrast(tokens[pair[0]], tokens[pair[1]]); r < 4.5 {
			t.Errorf("%s on %s is %.2f:1 — below AA's 4.5:1", pair[0], pair[1], r)
		}
	}
	_ = ink
	for _, must := range []string{"paper", "muted", "link", "rarity-legendary", "rarity-epic", "rarity-rare"} {
		if _, ok := tokens[must]; !ok {
			t.Errorf("--color-%s is missing from @theme; the database names it as a rarity token or the shell uses it", must)
		}
	}
}

// contrast is WCAG 2's contrast ratio between two #rrggbb colours.
func contrast(a, b string) float64 {
	la, lb := luminance(a), luminance(b)
	if la < lb {
		la, lb = lb, la
	}
	return (la + 0.05) / (lb + 0.05)
}

func luminance(hex string) float64 {
	c := func(i int) float64 {
		v, _ := strconv.ParseUint(hex[i:i+2], 16, 8)
		s := float64(v) / 255
		if s <= 0.03928 {
			return s / 12.92
		}
		return math.Pow((s+0.055)/1.055, 2.4)
	}
	return 0.2126*c(1) + 0.7152*c(3) + 0.0722*c(5)
}
