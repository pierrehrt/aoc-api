package pages_test

import (
	"encoding/xml"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"testing"

	"github.com/pierrehrt/aoc-api/internal/assets"
	"github.com/pierrehrt/aoc-api/internal/httpx"
	"github.com/pierrehrt/aoc-api/internal/items"
	"github.com/pierrehrt/aoc-api/internal/pages"
	"github.com/pierrehrt/aoc-api/internal/templates"
)

// robots.txt and the sitemap (AOC-025), against the fake corpus of 120 obviously fake items.

type index struct {
	Locs []string `xml:"sitemap>loc"`
}
type urlset struct {
	XMLName xml.Name
	Locs    []string `xml:"url>loc"`
}

func routerWith(t *testing.T, n int) http.Handler {
	t.Helper()
	set, err := assets.Load()
	if err != nil {
		t.Fatal(err)
	}
	tpl, err := templates.New(set)
	if err != nil {
		t.Fatal(err)
	}
	h := pages.New(tpl, set, base, items.NewService(newFakeItems(n)))
	return httpx.NewRouterWithSite(httpx.Build{Version: "1.2.3", Commit: "abc1234", Env: "test"}, h.Routes, set.Handler())
}

// sitemapURLs follows the index to every chunk and returns every URL, failing on anything invalid.
func sitemapURLs(t *testing.T, h http.Handler) []string {
	t.Helper()
	rr := get(t, h, http.MethodGet, "/sitemap.xml", nil, "")
	if rr.Code != http.StatusOK || !strings.HasPrefix(rr.Header().Get("Content-Type"), "application/xml") {
		t.Fatalf("/sitemap.xml: %d %q", rr.Code, rr.Header().Get("Content-Type"))
	}
	if !strings.Contains(rr.Body.String(), `<sitemapindex xmlns="http://www.sitemaps.org/schemas/sitemap/0.9">`) {
		t.Fatalf("not a sitemap INDEX in the sitemaps.org namespace: %.200s", rr.Body.String())
	}
	var idx index
	if err := xml.Unmarshal(rr.Body.Bytes(), &idx); err != nil || len(idx.Locs) == 0 {
		t.Fatalf("index does not parse or lists nothing: %v", err)
	}
	var all []string
	for _, loc := range idx.Locs {
		u, err := url.Parse(loc)
		if err != nil || u.Scheme != "https" || u.Host != "aoc-codex.app" {
			t.Fatalf("index entry %q is not an absolute https URL on the canonical host", loc)
		}
		cr := get(t, h, http.MethodGet, u.Path, nil, "")
		if cr.Code != http.StatusOK {
			t.Fatalf("%s: %d", u.Path, cr.Code)
		}
		var set urlset
		if err := xml.Unmarshal(cr.Body.Bytes(), &set); err != nil || set.XMLName.Local != "urlset" || set.XMLName.Space != "http://www.sitemaps.org/schemas/sitemap/0.9" {
			t.Fatalf("%s is not a urlset in the sitemaps.org namespace: %v", u.Path, err)
		}
		if strings.Contains(cr.Body.String(), "<lastmod>") {
			t.Errorf("%s carries a <lastmod>; no row has a real modification time", u.Path)
		}
		all = append(all, set.Locs...)
	}
	return all
}

func TestRobotsTxtKeepsCrawlersOutOfMachineryOnly(t *testing.T) {
	rr := get(t, router(t), http.MethodGet, "/robots.txt", nil, "")
	if rr.Code != http.StatusOK || !strings.HasPrefix(rr.Header().Get("Content-Type"), "text/plain") {
		t.Fatalf("status %d, Content-Type %q", rr.Code, rr.Header().Get("Content-Type"))
	}
	// Parsed as rules, never compared as bytes: in production Cloudflare prepends its own managed
	// comment block to this file (measured 2026-09-30).
	var disallow []string
	var sitemap string
	agentAll := false
	for _, line := range strings.Split(rr.Body.String(), "\n") {
		k, v, ok := strings.Cut(strings.TrimSpace(line), ":")
		if !ok || strings.HasPrefix(k, "#") {
			continue
		}
		switch strings.ToLower(strings.TrimSpace(k)) {
		case "user-agent":
			agentAll = agentAll || strings.TrimSpace(v) == "*"
		case "disallow":
			disallow = append(disallow, strings.TrimSpace(v))
		case "sitemap":
			sitemap = strings.TrimSpace(v)
		}
	}
	if !agentAll {
		t.Error("no `User-agent: *` group")
	}
	if sitemap != "https://aoc-codex.app/sitemap.xml" {
		t.Errorf("Sitemap = %q, want the absolute canonical URL", sitemap)
	}
	blocked := func(path string) bool {
		for _, d := range disallow {
			if d != "" && strings.HasPrefix(path, d) {
				return true
			}
		}
		return false
	}
	for _, p := range []string{"/_smoke", "/_smoke/echo", "/v1/items", "/health"} {
		if !blocked(p) {
			t.Errorf("%s is crawlable; it is machinery, not content", p)
		}
	}
	for _, p := range []string{"/", "/armory", "/armory/test-item-1", "/sitemap.xml", "/assets/app.css"} {
		if blocked(p) {
			t.Errorf("%s is disallowed; it is content (or what renders it)", p)
		}
	}
}

// ⭐ Every URL is absolute, https, canonical and answers 200 — walked, all of them, not a sample —
// and none is noindex, a redirect or a 404.
func TestEverySitemapURLIsACanonical200(t *testing.T) {
	h := router(t)
	urls := sitemapURLs(t, h)
	if len(urls) != 2+120 {
		t.Fatalf("%d URLs, want the home page, the Armory and 120 items", len(urls))
	}
	seen := map[string]bool{}
	canon := regexp.MustCompile(`<link rel="canonical" href="([^"]*)">`)
	for _, loc := range urls {
		if seen[loc] {
			t.Errorf("%s is listed twice", loc)
		}
		seen[loc] = true
		u, err := url.Parse(loc)
		if err != nil || u.Scheme != "https" || u.Host != "aoc-codex.app" {
			t.Fatalf("%q is not absolute https on the canonical host", loc)
		}
		rr := get(t, h, http.MethodGet, u.RequestURI(), nil, "")
		if rr.Code != http.StatusOK {
			t.Errorf("%s: %d — a sitemap may list only pages that answer 200", loc, rr.Code)
			continue
		}
		body := rr.Body.String()
		if strings.Contains(body, `content="noindex"`) {
			t.Errorf("%s is noindex", loc)
		}
		if m := canon.FindStringSubmatch(body); m == nil || m[1] != loc {
			t.Errorf("%s names canonical %v, want itself", loc, m)
		}
	}
	if seen["https://aoc-codex.app/_smoke"] {
		t.Error("the noindex smoke page is in the sitemap")
	}
}

// ⭐ The protocol's limit per file, proved at the boundary: a chunk never exceeds it, the index
// lists as many chunks as needed, and together they hold every URL exactly once.
func TestTheSitemapSplitsAtTheLimit(t *testing.T) {
	for _, tc := range []struct{ max, chunks int }{{50, 3}, {61, 2}, {122, 1}, {121, 2}} {
		restore := pages.SetSitemapMaxURLs(tc.max)
		h := router(t)
		var idx index
		rr := get(t, h, http.MethodGet, "/sitemap.xml", nil, "")
		if err := xml.Unmarshal(rr.Body.Bytes(), &idx); err != nil || len(idx.Locs) != tc.chunks {
			t.Errorf("max %d: index lists %d chunks, want %d", tc.max, len(idx.Locs), tc.chunks)
		}
		seen := map[string]bool{}
		for i := range idx.Locs {
			var set urlset
			cr := get(t, h, http.MethodGet, "/sitemaps/"+itoa(i+1)+".xml", nil, "")
			if err := xml.Unmarshal(cr.Body.Bytes(), &set); err != nil {
				t.Fatalf("max %d chunk %d: %v", tc.max, i+1, err)
			}
			if len(set.Locs) > tc.max || len(set.Locs) == 0 {
				t.Errorf("max %d: chunk %d holds %d URLs", tc.max, i+1, len(set.Locs))
			}
			for _, l := range set.Locs {
				if seen[l] {
					t.Errorf("max %d: %s is in two chunks", tc.max, l)
				}
				seen[l] = true
			}
		}
		if len(seen) != 122 {
			t.Errorf("max %d: the chunks hold %d distinct URLs, want all 122", tc.max, len(seen))
		}
		for _, n := range []string{"0", itoa(tc.chunks + 1), "x"} {
			if c := get(t, h, http.MethodGet, "/sitemaps/"+n+".xml", nil, "").Code; c != http.StatusNotFound {
				t.Errorf("max %d: /sitemaps/%s.xml answered %d, want 404", tc.max, n, c)
			}
		}
		restore()
	}
}

// Zero items is a valid sitemap, not a 500: the index points at one chunk holding the static pages.
func TestAnEmptyArmoryStillHasAValidSitemap(t *testing.T) {
	urls := sitemapURLs(t, routerWith(t, 0))
	if len(urls) != 2 {
		t.Errorf("an empty armory's sitemap holds %v, want the home page and the Armory", urls)
	}
}

// Crawled by bots on every visit, so it must come from the edge, not the origin.
func TestTheSitemapIsCachedAtTheEdge(t *testing.T) {
	h := router(t)
	for _, p := range []string{"/robots.txt", "/sitemap.xml", "/sitemaps/1.xml"} {
		if cc := get(t, h, http.MethodGet, p, nil, "").Header().Get("Cache-Control"); !strings.Contains(cc, "s-maxage=3600") {
			t.Errorf("%s: Cache-Control %q, want the page policy", p, cc)
		}
	}
}
