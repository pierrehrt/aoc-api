package pages_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/pierrehrt/aoc-api/internal/assets"
	"github.com/pierrehrt/aoc-api/internal/httpx"
	"github.com/pierrehrt/aoc-api/internal/pages"
	"github.com/pierrehrt/aoc-api/internal/templates"
)

const base = "https://aoc-codex.app"

func router(t *testing.T) http.Handler {
	t.Helper()
	set, err := assets.Load()
	if err != nil {
		t.Fatalf("assets.Load: %v", err)
	}
	tpl, err := templates.New(set)
	if err != nil {
		t.Fatalf("templates.New: %v", err)
	}
	h := pages.New(tpl, base)
	return httpx.NewRouterWithSite("1.2.3", "abc1234", h.Routes, set.Handler())
}

func get(t *testing.T, h http.Handler, method, path string, hdr map[string]string, body string) *httptest.ResponseRecorder {
	t.Helper()
	var r *http.Request
	if body != "" {
		r = httptest.NewRequest(method, path, strings.NewReader(body))
		r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	} else {
		r = httptest.NewRequest(method, path, nil)
	}
	for k, v := range hdr {
		r.Header.Set(k, v)
	}
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, r)
	return rr
}

// ⭐ THE CLAIM THE WHOLE ARCHITECTURE RESTS ON: the page is complete in the HTTP
// response, with no JavaScript involved. If this fails, dropping the SPA bought nothing
// (DECISIONS.md, 2026-09-16).
func TestPublicPagesRenderWithoutJavaScript(t *testing.T) {
	h := router(t)
	// Each page asserts its OWN visible text. A shared string would pass on a page that
	// happens to mention it in the layout while its actual content never rendered.
	pagesUnderTest := map[string]string{
		"/":       "A reference for",
		"/_smoke": "Server-rendered marker",
	}
	for path, visible := range pagesUnderTest {
		t.Run(path, func(t *testing.T) {
			rr := get(t, h, http.MethodGet, path, nil, "")
			if rr.Code != http.StatusOK {
				t.Fatalf("status = %d, want 200", rr.Code)
			}
			if ct := rr.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/html") {
				t.Errorf("Content-Type = %q, want text/html", ct)
			}
			body := rr.Body.String()
			// The visible words must be in the bytes, not injected later.
			for _, want := range []string{"<h1", "</main>", visible} {
				if !strings.Contains(body, want) {
					t.Errorf("body does not contain %q — the page is not server-rendered", want)
				}
			}
			if strings.Contains(body, `<div id="root"></div>`) {
				t.Error("body looks like an SPA shell")
			}
		})
	}
}

// Every public page must carry its head contract. A page that forgets it silently
// undoes the reason this service renders HTML at all.
func TestEveryPageCarriesItsHeadContract(t *testing.T) {
	h := router(t)
	for _, path := range []string{"/", "/_smoke"} {
		t.Run(path, func(t *testing.T) {
			body := get(t, h, http.MethodGet, path, nil, "").Body.String()
			for _, want := range []string{
				`<html lang="en">`,
				`<title>`,
				`<meta name="description" content="`,
				`<link rel="canonical" href="` + base + path + `"`,
				`<meta property="og:title"`,
				`<meta property="og:description"`,
				`<meta property="og:url"`,
			} {
				if !strings.Contains(body, want) {
					t.Errorf("missing %q", want)
				}
			}
			if strings.Contains(body, `content=""`) {
				t.Error("a meta tag rendered with an empty value")
			}
		})
	}
}

// The smoke page is machinery. It must not be indexable; the home page must be.
func TestNoIndexOnlyWhereItBelongs(t *testing.T) {
	h := router(t)
	if b := get(t, h, http.MethodGet, "/_smoke", nil, "").Body.String(); !strings.Contains(b, `name="robots" content="noindex"`) {
		t.Error("/_smoke is missing noindex — internal machinery must not reach a search index")
	}
	if b := get(t, h, http.MethodGet, "/", nil, "").Body.String(); strings.Contains(b, "noindex") {
		t.Error("/ carries noindex — that would hide the site from search entirely")
	}
}

// One handler, one query, two renderings — and the no-JS branch must be the full page.
func TestHTMXGetsAFragmentAndABrowserGetsAPage(t *testing.T) {
	h := router(t)

	frag := get(t, h, http.MethodPost, "/_smoke/echo", map[string]string{"HX-Request": "true"}, "say=ping")
	fb := frag.Body.String()
	if strings.Contains(fb, "<html") || strings.Contains(fb, "<title>") {
		t.Errorf("HTMX response is a whole page, not a fragment:\n%s", fb)
	}
	if !strings.Contains(fb, "ping") {
		t.Errorf("fragment does not echo the input: %s", fb)
	}

	full := get(t, h, http.MethodPost, "/_smoke/echo", nil, "say=ping")
	pb := full.Body.String()
	if !strings.Contains(pb, "<html") || !strings.Contains(pb, "<title>") {
		t.Error("a plain form POST did not get a full page — the page does not work without JavaScript")
	}
	if !strings.Contains(pb, "ping") {
		t.Error("full page does not echo the input")
	}

	// ⚠️ Both branches must Vary, or a cache can serve the bare fragment to a browser.
	for name, rr := range map[string]*httptest.ResponseRecorder{"fragment": frag, "page": full} {
		if v := rr.Header().Get("Vary"); !strings.Contains(v, "HX-Request") {
			t.Errorf("%s response has Vary %q, missing HX-Request", name, v)
		}
	}
}

// The API contract stays JSON; a person who mistypes a URL gets a readable page.
func TestNotFoundShapeFollowsThePath(t *testing.T) {
	h := router(t)
	cases := []struct{ path, wantType string }{
		{"/nope", "text/html"},
		{"/deeply/missing/page", "text/html"},
		{"/v1/nope", "application/json"},
		{"/assets/nope.css", "application/json"},
	}
	for _, c := range cases {
		t.Run(c.path, func(t *testing.T) {
			rr := get(t, h, http.MethodGet, c.path, nil, "")
			if rr.Code != http.StatusNotFound {
				t.Fatalf("status = %d, want 404", rr.Code)
			}
			if ct := rr.Header().Get("Content-Type"); !strings.HasPrefix(ct, c.wantType) {
				t.Errorf("Content-Type = %q, want %s", ct, c.wantType)
			}
		})
	}
}

// /health must stay JSON now that HTML shares the origin.
func TestHealthIsStillJSON(t *testing.T) {
	rr := get(t, router(t), http.MethodGet, "/health", nil, "")
	if ct := rr.Header().Get("Content-Type"); !strings.HasPrefix(ct, "application/json") {
		t.Errorf("/health Content-Type = %q, want JSON", ct)
	}
}

func TestAssetsAreHashedAndCacheableForever(t *testing.T) {
	set, err := assets.Load()
	if err != nil {
		t.Fatalf("assets.Load: %v", err)
	}
	h := router(t)

	p, err := set.Path("app.css")
	if err != nil {
		t.Fatalf("no app.css: %v", err)
	}
	if !strings.HasPrefix(p, "/assets/app.") || !strings.HasSuffix(p, ".css") || p == "/assets/app.css" {
		t.Errorf("path %q is not content-hashed", p)
	}

	rr := get(t, h, http.MethodGet, p, nil, "")
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d for %s, want 200", rr.Code, p)
	}
	if cc := rr.Header().Get("Cache-Control"); !strings.Contains(cc, "immutable") {
		t.Errorf("Cache-Control = %q, want immutable", cc)
	}
	if ct := rr.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/css") {
		t.Errorf("Content-Type = %q, want text/css", ct)
	}
	if rr.Body.Len() == 0 {
		t.Error("the stylesheet is empty")
	}

	// A wrong hash must 404. Serving the current bytes under a stale hash would make an
	// out-of-date URL look valid forever, which defeats immutable caching.
	bad := strings.Replace(p, "app.", "app.deadbeef", 1)
	if rr := get(t, h, http.MethodGet, bad, nil, ""); rr.Code != http.StatusNotFound {
		t.Errorf("a wrong hash returned %d, want 404", rr.Code)
	}

	// An unknown logical name must be an error, not an empty string silently rendered
	// into a <link href="">.
	if _, err := set.Path("nope.css"); err == nil {
		t.Error("Path() accepted an asset that does not exist")
	}
}

// The page must reference the hashed asset URLs, not the bare names.
func TestPagesLinkTheHashedAssets(t *testing.T) {
	set, _ := assets.Load()
	css, _ := set.Path("app.css")
	js, _ := set.Path("htmx.min.js")
	body := get(t, router(t), http.MethodGet, "/", nil, "").Body.String()
	for _, want := range []string{`href="` + css + `"`, `src="` + js + `"`} {
		if !strings.Contains(body, want) {
			t.Errorf("page does not reference %s", want)
		}
	}
	if strings.Contains(body, `href="/assets/app.css"`) {
		t.Error("page links the UNHASHED stylesheet — it would be cached stale")
	}
}
