package pages_test

import (
	"html"
	"net/http"
	"net/url"
	"regexp"
	"testing"

	"github.com/pierrehrt/aoc-api/internal/db/sqlcgen"
)

// AOC-025 verify round 2: the sitemap entry and the page's own canonical are the SAME bytes for a
// slug outside slugify's [a-z0-9-] too — not merely equivalent under RFC 3986. html/template escapes
// a raw "/armory/"+slug with lower-case hex and url.PathEscape with upper-case, so the two agreed on
// every slug the importer makes and disagreed on "test-é" (verify round 1, N2). One itemPath for both
// fixes it by construction; this keeps it fixed. The slugs are obviously fake (CLAUDE.md STEP ZERO).
func TestTheSitemapAndThePageSpellANonASCIISlugTheSameWay(t *testing.T) {
	// Ids past the detailed fixtures (1–3 in fake_item_detail_test.go), which would answer for these.
	q := &fakeItems{rows: []sqlcgen.ListItemsRow{
		{ItemID: 9001, Slug: "test-é", Name: "Test Accent", Rarity: "epic", Confidence: "unconfirmed"},
		{ItemID: 9002, Slug: "test item+ü", Name: "Test Mixed", Rarity: "epic", Confidence: "unconfirmed"},
	}}
	h := routerWith(t, q, 0)
	canon := regexp.MustCompile(`<link rel="canonical" href="([^"]*)">`)
	ogURL := regexp.MustCompile(`<meta property="og:url" content="([^"]*)">`)
	items := 0
	for _, loc := range sitemapURLs(t, h) {
		u, err := url.Parse(loc)
		if err != nil {
			t.Fatalf("%q: %v", loc, err)
		}
		if u.Path == "/" || u.Path == "/armory" {
			continue
		}
		items++
		rr := get(t, h, http.MethodGet, u.RequestURI(), nil, "")
		if rr.Code != http.StatusOK {
			t.Errorf("%s: %d", loc, rr.Code)
			continue
		}
		// An attribute is compared as the value a parser reads: html/template writes "+" as "&#43;".
		body := rr.Body.String()
		if m := canon.FindStringSubmatch(body); m == nil || html.UnescapeString(m[1]) != loc {
			t.Errorf("sitemap lists %q, the page names canonical %v", loc, m)
		}
		if m := ogURL.FindStringSubmatch(body); m == nil || html.UnescapeString(m[1]) != loc {
			t.Errorf("sitemap lists %q, the page names og:url %v", loc, m)
		}
	}
	if items != 2 {
		t.Errorf("%d item URLs in the sitemap, want 2", items)
	}
}
