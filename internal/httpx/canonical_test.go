package httpx

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
)

// AOC-025: a malformed PUBLIC_BASE_URL stops the boot. The review measured the failure it prevents:
// " https://aoc-codex.app" parsed to the canonical host "https" and would have redirected everything.
func TestPublicBaseURLIsParsedStrictly(t *testing.T) {
	for _, ok := range []string{"https://aoc-codex.app", "https://aoc-codex.app/", "http://localhost:8080"} {
		if u, err := ParsePublicBaseURL(ok); err != nil || u.Path != "" {
			t.Errorf("%q: %v, %v — want it accepted as a bare origin", ok, u, err)
		}
	}
	for _, bad := range []string{"", " https://aoc-codex.app", "https://aoc-codex.app\n", "aoc-codex.app",
		"aoc-codex.app/", "https://", "ftp://aoc-codex.app", "https://aoc-codex.app/armory", "https://aoc-codex.app?x=1",
		"https://user@aoc-codex.app"} {
		if u, err := ParsePublicBaseURL(bad); err == nil {
			t.Errorf("%q accepted as %v; it must stop the boot", bad, u)
		}
	}
}

// ⭐ The redirect as it is composed in production — inside the router, so /v1, the 404 page and
// the assets are covered — and never on the canonical host or /health.
func TestEveryOtherHostIsRedirectedToTheCanonicalOne(t *testing.T) {
	base, err := ParsePublicBaseURL("https://aoc-codex.app")
	if err != nil {
		t.Fatal(err)
	}
	site := func(r chi.Router) {
		r.Get("/armory", func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte("page")) })
		r.Post("/armory", func(w http.ResponseWriter, _ *http.Request) {})
	}
	h := NewRouterWithAPI(Build{Version: "t", Commit: "t", Env: "test"}, site, nil, func(chi.Router) {}, WithCanonicalHost(base))

	for _, tc := range []struct {
		method, host, target string
		code                 int
		location             string
	}{
		{"GET", "aoc-codex.app", "/armory?q=helm", 200, ""},
		{"GET", "AOC-Codex.app:443", "/armory", 200, ""},
		{"GET", "aoc-codex.app.", "/armory", 200, ""},
		{"GET", "aoc-armory-snapshot-production.up.railway.app", "/armory?q=helm&sort=name", 301, "https://aoc-codex.app/armory?q=helm&sort=name"},
		{"HEAD", "aoc-armory-snapshot-production.up.railway.app", "/armory", 301, "https://aoc-codex.app/armory"},
		{"GET", "aoc-armory-snapshot-production.up.railway.app", "/no-such-page", 301, "https://aoc-codex.app/no-such-page"},
		{"GET", "aoc-armory-snapshot-production.up.railway.app", "/v1/items", 301, "https://aoc-codex.app/v1/items"},
		{"POST", "aoc-armory-snapshot-production.up.railway.app", "/armory", 308, "https://aoc-codex.app/armory"},
		{"GET", "aoc-armory-snapshot-production.up.railway.app", "/health", 200, ""}, // monitors, on any host
		{"GET", "www.aoc-codex.app", "/", 301, "https://aoc-codex.app/"},
		// not an open redirect: a path that looks like a host stays a path on the canonical host
		{"GET", "aoc-armory-snapshot-production.up.railway.app", "//evil.example/x", 301, "https://aoc-codex.app//evil.example/x"},
	} {
		r := httptest.NewRequestWithContext(context.Background(), tc.method, tc.target, nil)
		r.Host = tc.host
		rr := httptest.NewRecorder()
		h.ServeHTTP(rr, r)
		if rr.Code != tc.code || rr.Header().Get("Location") != tc.location {
			t.Errorf("%s %s%s: %d %q, want %d %q", tc.method, tc.host, tc.target, rr.Code, rr.Header().Get("Location"), tc.code, tc.location)
		}
		if tc.code == 301 && !strings.Contains(rr.Header().Get("Cache-Control"), "public") {
			t.Errorf("%s%s: the 301 left without the page cache policy (%q)", tc.host, tc.target, rr.Header().Get("Cache-Control"))
		}
	}

	// Without the option — every test router, local dev without PUBLIC_BASE_URL — nothing redirects.
	plain := NewRouterWithAPI(Build{Version: "t", Commit: "t", Env: "test"}, site, nil, func(chi.Router) {})
	r := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/armory", nil)
	r.Host = "example.com"
	rr := httptest.NewRecorder()
	plain.ServeHTTP(rr, r)
	if rr.Code != http.StatusOK {
		t.Errorf("no canonical host configured, yet %d", rr.Code)
	}
}
