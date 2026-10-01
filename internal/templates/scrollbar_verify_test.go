package templates_test

import (
	"os"
	"regexp"
	"strings"
	"testing"
)

// AOC-065 verify round 3. The guideline's scrollbars (reference/ui-guidelines.md § 3: 9px, a
// line-control thumb with radius 5px on an ink-pane track) are drawn with ::-webkit-scrollbar.
// Chrome ignores those rules on any element whose standard scrollbar-width or scrollbar-color is set,
// so one global `* { scrollbar-width: thin }` hid every bar of the design (verify round 2, F14).
// DECISIONS.md, 2026-10-01: the standard pair is for browsers without ::-webkit-scrollbar (Firefox)
// only, under `@supports not selector(::-webkit-scrollbar)`. The only other standard declaration
// allowed is `scrollbar-width: none`, which hides a bar on purpose (the phone tab row).
//
// Both the source and the BUILT stylesheet are read: the built one is what the binary serves.

var (
	cssComment     = regexp.MustCompile(`(?s)/\*.*?\*/`)
	firefoxOnly    = regexp.MustCompile(`@supports\s+not\s+selector\(\s*::-webkit-scrollbar\s*\)\s*\{`)
	scrollbarWidth = regexp.MustCompile(`scrollbar-width\s*:\s*([^;}]+)`)
	scrollbarColor = regexp.MustCompile(`scrollbar-color\s*:`)
	webkitBar      = regexp.MustCompile(`::-webkit-scrollbar\s*\{\s*width\s*:\s*9px\s*;\s*height\s*:\s*9px\s*;?\s*\}`)
	webkitThumb    = regexp.MustCompile(`::-webkit-scrollbar-thumb\s*\{[^}]*background\s*:\s*var\(--color-line-control\)[^}]*border-radius\s*:\s*5px`)
	webkitTrack    = regexp.MustCompile(`::-webkit-scrollbar-track\s*\{[^}]*background\s*:\s*var\(--color-ink-pane\)`)
)

// splitFirefoxOnly returns the stylesheet without its `@supports not selector(::-webkit-scrollbar)`
// blocks, and those blocks' bodies.
func splitFirefoxOnly(css string) (rest string, blocks []string) {
	for {
		loc := firefoxOnly.FindStringIndex(css)
		if loc == nil {
			return css, blocks
		}
		depth, end := 1, -1
		for i := loc[1]; i < len(css) && end < 0; i++ {
			switch css[i] {
			case '{':
				depth++
			case '}':
				depth--
				if depth == 0 {
					end = i
				}
			}
		}
		if end < 0 {
			return css, append(blocks, "unbalanced")
		}
		blocks = append(blocks, css[loc[1]:end])
		css = css[:loc[0]] + css[end+1:]
	}
}

// scrollbarProblems lists every way a stylesheet would stop Chrome drawing the design's bars, or
// would leave Firefox without them.
func scrollbarProblems(css string) []string {
	var out []string
	rest, blocks := splitFirefoxOnly(cssComment.ReplaceAllString(css, ""))
	for _, m := range scrollbarWidth.FindAllStringSubmatch(rest, -1) {
		if v := strings.TrimSpace(m[1]); v != "none" {
			out = append(out, "scrollbar-width: "+v+" outside the Firefox-only block — Chrome then ignores ::-webkit-scrollbar there")
		}
	}
	if n := len(scrollbarColor.FindAllString(rest, -1)); n > 0 {
		out = append(out, "scrollbar-color outside the Firefox-only block — Chrome then ignores ::-webkit-scrollbar there")
	}
	if !webkitBar.MatchString(rest) {
		out = append(out, "no ::-webkit-scrollbar { width: 9px; height: 9px } in force")
	}
	if !webkitThumb.MatchString(rest) {
		out = append(out, "no ::-webkit-scrollbar-thumb on line-control with a 5px radius")
	}
	if !webkitTrack.MatchString(rest) {
		out = append(out, "no ::-webkit-scrollbar-track on ink-pane")
	}
	firefox := strings.Join(blocks, "\n")
	if !scrollbarColor.MatchString(firefox) || !strings.Contains(firefox, "--color-line-control") || !strings.Contains(firefox, "--color-ink-pane") {
		out = append(out, "Firefox gets no line-control / ink-pane scrollbar-color")
	}
	return out
}

func TestTheDesignsScrollbarsAreNotSwitchedOffInChrome(t *testing.T) {
	for _, path := range []string{"../../web/src/app.css", "../assets/built/app.css"} {
		src, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", path, err)
		}
		for _, p := range scrollbarProblems(string(src)) {
			t.Errorf("%s: %s", path, p)
		}
	}

	// The two colours the bar is drawn in are the guideline's (§ 1): line-control #2b2723, ink-pane #100f0e.
	src, err := os.ReadFile("../../web/src/app.css")
	if err != nil {
		t.Fatal(err)
	}
	for token, hex := range map[string]string{"line-control": "#2b2723", "ink-pane": "#100f0e"} {
		if !regexp.MustCompile(`--color-` + token + `:\s*` + hex + `\b`).Match(src) {
			t.Errorf("--color-%s is not the guideline's %s", token, hex)
		}
	}

	// The check itself catches round 2's bug: the global pair, set beside the webkit rules.
	round2 := `@layer base {
  * { scrollbar-width: thin; scrollbar-color: var(--color-line-control) var(--color-ink-pane); }
  ::-webkit-scrollbar { width: 9px; height: 9px; }
  ::-webkit-scrollbar-thumb { background: var(--color-line-control); border-radius: 5px; }
  ::-webkit-scrollbar-track { background: var(--color-ink-pane); }
}`
	if len(scrollbarProblems(round2)) == 0 {
		t.Error("the check passes verify round 2's stylesheet, which hid every bar in Chrome")
	}
}

// F10 must not come back: the phone header's tab row scrolls sideways with no bar under the tabs, in
// Chrome/Safari (::-webkit-scrollbar hidden) and in Firefox (scrollbar-width: none) alike.
func TestThePhoneTabRowHidesItsBarInEveryEngine(t *testing.T) {
	base, err := os.ReadFile("html/base.html")
	if err != nil {
		t.Fatal(err)
	}
	m := regexp.MustCompile(`<nav aria-label="Sections" class="([^"]*)"`).FindSubmatch(base)
	if m == nil {
		t.Fatal("no Sections nav in base.html")
	}
	classes := " " + string(m[1]) + " "
	for _, want := range []string{" overflow-x-auto ", " [scrollbar-width:none] ", " [&::-webkit-scrollbar]:hidden "} {
		if !strings.Contains(classes, want) {
			t.Errorf("the tab row lacks %q (classes: %s)", strings.TrimSpace(want), m[1])
		}
	}
	built, err := os.ReadFile("../assets/built/app.css")
	if err != nil {
		t.Fatal(err)
	}
	for _, rule := range []string{`.\[scrollbar-width\:none\]{scrollbar-width:none}`, `.\[\&\:\:-webkit-scrollbar\]\:hidden::-webkit-scrollbar{display:none}`} {
		if !strings.Contains(string(built), rule) {
			t.Errorf("the built stylesheet lacks %s", rule)
		}
	}
}
