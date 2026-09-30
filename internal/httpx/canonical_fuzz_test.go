package httpx

import (
	"bufio"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
)

// AOC-025 verify round 2: the host redirect's Location stays on the canonical host for ANY request
// target the server accepts, not only the cases TestEveryOtherHostIsRedirectedToTheCanonicalOne
// lists. The seeds are the targets verify sent to the real binary over a raw socket — opaque forms,
// backslashes, encoded slashes, "@" and "." after the host, absolute-form, "*", very long ones — so
// plain `go test` runs every one of them. To search further:
//
//	go test -run '^$' -fuzz FuzzTheHostRedirectNeverLeavesTheHost ./internal/httpx/
//
// Each target is parsed as the server parses it (http.ReadRequest); one it would refuse is ignored —
// returned from, never skipped, because a skipped test fails the gate as "did not run".
func FuzzTheHostRedirectNeverLeavesTheHost(f *testing.F) {
	for _, s := range []string{
		"x:@evil.example/", "x:.evil.example/phish", "x:@evil.example", "x:evil.example", "x::evil.example",
		"x:@evil.example/?q=1", "javascript:alert(1)", "https:evil.example", "https:@evil.example/",
		"mailto:a@evil.example", "HTTPS:@EVIL.EXAMPLE/", "x:/@evil.example", "x:/..//evil.example",
		"x://evil.example/", "x:////evil.example/", "x:%2F%2Fevil.example", `x:\\evil.example`,
		"x:@evil.example%2F", "x:@evil.example#frag", "a+b-c.d:@evil.example/",
		`/\evil.example/`, `/\\evil.example/`, "//evil.example/x", "///evil.example/x", "/%2F%2Fevil.example/",
		"/%5C%5Cevil.example", "/@evil.example", "/.evil.example", "/:evil.example", "/?@evil.example",
		"/#@evil.example", "/armory?next=//evil.example", "/armory?x=%0d%0aSet-Cookie:a=1", "/..//evil.example",
		"http://evil.example/x", "https://aoc-codex.app@evil.example/", "http://a@evil.example/",
		"http://evil.example", "//aoc-codex.app@evil.example/", "*", "/%0d%0aLocation:%20https://evil.example",
		"/%00", "/%E2%80%AE", "/\xff\xfe", "A:?#%", "/" + strings.Repeat("a", 8000),
		"x:@" + strings.Repeat("a", 8000) + ".evil.example/", "/" + strings.Repeat("%2F", 3000) + "evil.example",
	} {
		f.Add(s)
	}

	base, err := ParsePublicBaseURL("https://aoc-codex.app")
	if err != nil {
		f.Fatal(err)
	}
	site := func(r chi.Router) {
		r.Get("/armory", func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte("page")) })
	}
	h := NewRouterWithAPI(Build{Version: "t", Commit: "t", Env: "test"}, site, nil, func(chi.Router) {}, WithCanonicalHost(base))

	f.Fuzz(func(t *testing.T, target string) {
		for _, method := range []string{http.MethodGet, http.MethodPost} {
			raw := method + " " + target + " HTTP/1.1\r\nHost: aoc-armory-snapshot-production.up.railway.app\r\n\r\n"
			r, err := http.ReadRequest(bufio.NewReader(strings.NewReader(raw)))
			if err != nil {
				return // the server answers 400 before any handler runs
			}
			rr := httptest.NewRecorder()
			h.ServeHTTP(rr, r)
			loc := rr.Header().Get("Location")
			if loc == "" {
				continue
			}
			// The prefix is the property: whatever follows "https://aoc-codex.app/" is a path, under
			// RFC 3986 and the browsers' WHATWG parser alike. The query is the request's own, passed
			// through — a malformed escape in it ("A:?#%", found by fuzzing) can make the Location
			// unparseable to url.Parse, but never moves its host.
			if !strings.HasPrefix(loc, "https://aoc-codex.app/") || strings.ContainsAny(loc, "\r\n") {
				t.Errorf("%s %q → %d Location %q: it leaves the canonical host", method, target, rr.Code, loc)
			}
			if u, err := url.Parse(loc); err == nil && (u.Host != "aoc-codex.app" || u.User != nil) {
				t.Errorf("%s %q → %d Location %q parses to host %q, userinfo %v", method, target, rr.Code, loc, u.Host, u.User)
			}
		}
	})
}
