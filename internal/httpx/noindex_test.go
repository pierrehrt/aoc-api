package httpx

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

// AOC-025: every response under a host that is not the canonical one says noindex — and none under
// the canonical host itself, however its Host header is spelled.
func TestNoIndexOnlyOffTheCanonicalHost(t *testing.T) {
	h := NoIndexOffCanonicalHost("https://aoc-codex.app")(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	for host, want := range map[string]string{
		"aoc-codex.app":     "",
		"AOC-Codex.app":     "",
		"aoc-codex.app:443": "",
		"aoc-codex.app.":    "",
		"aoc-armory-snapshot-production.up.railway.app": "noindex", // the measured duplicate
		"www.aoc-codex.app":                             "noindex", // 301'd at the edge today; noindex if it ever is not
		"nnja20lj.up.railway.app":                       "noindex",
	} {
		r := httptest.NewRequest(http.MethodGet, "/armory", nil)
		r.Host = host
		rr := httptest.NewRecorder()
		h.ServeHTTP(rr, r)
		if got := rr.Header().Get("X-Robots-Tag"); got != want {
			t.Errorf("Host %q: X-Robots-Tag %q, want %q", host, got, want)
		}
	}
	// Local development: the base URL carries a port, the request does too.
	local := NoIndexOffCanonicalHost("http://localhost:8080")(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.Host = "localhost:8080"
	rr := httptest.NewRecorder()
	local.ServeHTTP(rr, r)
	if got := rr.Header().Get("X-Robots-Tag"); got != "" {
		t.Errorf("localhost against a localhost base: X-Robots-Tag %q, want none", got)
	}
}
