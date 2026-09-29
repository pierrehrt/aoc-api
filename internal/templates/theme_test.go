package templates_test

import (
	"math"
	"os"
	"regexp"
	"strconv"
	"strings"
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
	if i := strings.Index(theme, "@theme {"); i >= 0 {
		theme = theme[i:]
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
	surfaces := map[string]bool{"ink": true, "ink-2": true, "line": true}
	if len(tokens) <= len(surfaces) {
		t.Fatalf("only %d colour tokens found; the theme is not where this test looks", len(tokens))
	}
	for name, hex := range tokens {
		if surfaces[name] {
			continue
		}
		if r := contrast(hex, ink); r < 4.5 {
			t.Errorf("--color-%s %s on ink %s is %.2f:1 — below AA's 4.5:1", name, hex, ink, r)
		}
	}
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
