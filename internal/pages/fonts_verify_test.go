package pages_test

import (
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/pierrehrt/aoc-api/internal/templates"
)

// AOC-075 verify round 1. The rules sit in a <style> written by html/template, which escapes by
// context: inside CSS it may rewrite a quote, a '+' or a URL and the browser then drops the rule
// silently, leaving the page in the fallback with every request still green. So every face is
// asserted as the exact rule a browser parses, on every kind of page that carries the shell — not
// only /armory and / — and none of them names a font host.
func TestEveryShellPageDeclaresEachFaceVerbatimAndNoFontHost(t *testing.T) {
	site, armory := router(t), gearRouter(t, gearCorpus())
	pages := []struct {
		h    http.Handler
		path string
	}{{site, "/"}, {site, "/armory/test-item-1"}, {site, "/_smoke"}, {site, "/aa"}, {armory, "/armory"}}
	for _, pg := range pages {
		rr := get(t, pg.h, http.MethodGet, pg.path, nil, "")
		if rr.Code != http.StatusOK {
			t.Fatalf("%s: status %d", pg.path, rr.Code)
		}
		body := rr.Body.String()
		for _, host := range []string{"fonts.googleapis.com", "fonts.gstatic.com", "preconnect"} {
			if strings.Contains(body, host) {
				t.Errorf("%s names %q", pg.path, host)
			}
		}
		style := regexp.MustCompile(`(?s)<style>(.*?)</style>`).FindStringSubmatch(body)
		if style == nil {
			t.Fatalf("%s: no <style> in the shell", pg.path)
		}
		for _, f := range templates.FontFaces {
			stem := regexp.QuoteMeta(strings.TrimSuffix(f.File, ".woff2"))
			rule := `@font-face\{font-family:"` + regexp.QuoteMeta(f.Family) + `";font-style:normal;font-weight:` +
				strconv.Itoa(f.Weight) + `;font-display:swap;src:url\(/assets/` + stem + `\.[0-9a-f]{8}\.woff2\) format\("woff2"\);unicode-range:` +
				regexp.QuoteMeta(string(f.Range)) + `\}`
			if n := len(regexp.MustCompile(rule).FindAllString(style[1], -1)); n != 1 {
				t.Errorf("%s: the rule for %s appears %d times verbatim, want 1", pg.path, f.File, n)
			}
		}
	}
}
