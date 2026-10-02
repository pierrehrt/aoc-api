package templates_test

import (
	"os"
	"regexp"
	"testing"
)

// AOC-065, the delta verify after round 3 (N3). Where a pane overflows on both axes (the table pane
// at 1024 px), Chrome draws a 9 × 9 corner between the two bars. With no rule it is the browser's
// white (#ffffff, measured at ed46776); DECISIONS.md, 2026-10-01 makes it ink-pane, like the track.
// Measured at 96b563e: the corner is #100f0e and nothing else on the page changed (81 px differ).
//
// Both the source and the BUILT stylesheet are read: the built one is what the binary serves.

var (
	cornerComment = regexp.MustCompile(`(?s)/\*.*?\*/`)
	cornerInkPane = regexp.MustCompile(`::-webkit-scrollbar-corner\s*\{[^}]*background(?:-color)?\s*:\s*var\(--color-ink-pane\)`)
)

func cornerIsInkPane(css string) bool {
	return cornerInkPane.MatchString(cornerComment.ReplaceAllString(css, ""))
}

func TestTheScrollbarCornerIsInkPane(t *testing.T) {
	for _, path := range []string{"../../web/src/app.css", "../assets/built/app.css"} {
		src, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if !cornerIsInkPane(string(src)) {
			t.Errorf("%s: no ::-webkit-scrollbar-corner on ink-pane — the corner between two bars is the browser's white", path)
		}
	}

	// The check itself: it fails the stylesheet before the fix, a corner left in a comment, and a
	// corner of another colour.
	for name, css := range map[string]string{
		"ed46776 (no corner rule)": `::-webkit-scrollbar { width: 9px; height: 9px; }
  ::-webkit-scrollbar-thumb { background: var(--color-line-control); border-radius: 5px; }
  ::-webkit-scrollbar-track { background: var(--color-ink-pane); }`,
		"commented out": `/* ::-webkit-scrollbar-corner { background: var(--color-ink-pane); } */`,
		"white":         `::-webkit-scrollbar-corner { background: #ffffff; }`,
	} {
		if cornerIsInkPane(css) {
			t.Errorf("the check passes %s", name)
		}
	}
}
