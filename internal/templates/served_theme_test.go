package templates_test

import (
	"os"
	"regexp"
	"strings"
	"testing"
)

// ⭐ The theme the READER receives is the built stylesheet, not web/src/app.css (AOC-046 verify
// round 1). Tailwind v4 emits an @theme variable only where a scanned utility uses it, and the
// rarity tokens are reached from the database — `rarities.colour_token` → `var(--color-rarity-epic)`
// in a style attribute — which no scanner can see. Measured on the branch: the source declares
// eight colour tokens, the committed internal/assets/built/app.css carries five, and the three
// rarity tokens the migration names are among the missing. A token that exists only in the source
// is a colour that renders as inherited text on every page that trusts the database.
//
// So: every `--color-*` token the source @theme declares must reach the served file, byte for byte.
func TestServedThemeCarriesEveryDeclaredToken(t *testing.T) {
	declared := colourTokens(t, "../../web/src/app.css", true)
	served := colourTokens(t, "../assets/built/app.css", false)
	if len(declared) == 0 {
		t.Fatal("web/src/app.css declares no --color-* token in @theme; this test is looking in the wrong place")
	}
	for name, hex := range declared {
		got, ok := served[name]
		switch {
		case !ok:
			t.Errorf("--color-%s (%s) is declared in web/src/app.css but absent from the served app.css — "+
				"a template or the database names it and the reader never receives it", name, hex)
		case !strings.EqualFold(got, hex):
			t.Errorf("--color-%s is %s in the source and %s in the served app.css", name, hex, got)
		}
	}
}

// colourTokens reads every `--color-<name>: #rrggbb` from a stylesheet. With themeOnly, it reads
// only from the @theme block on, so a colour used elsewhere in the source cannot pose as a token.
func colourTokens(t *testing.T, path string, themeOnly bool) map[string]string {
	t.Helper()
	src, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	css := string(src)
	if themeOnly {
		i := strings.Index(css, "@theme")
		if i < 0 {
			t.Fatalf("%s has no @theme block", path)
		}
		css = css[i:]
	}
	out := map[string]string{}
	for _, m := range regexp.MustCompile(`--color-([a-z0-9-]+):\s*(#[0-9a-fA-F]{6})`).FindAllStringSubmatch(css, -1) {
		out[m[1]] = m[2]
	}
	return out
}
