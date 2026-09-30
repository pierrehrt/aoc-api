package httpx

import (
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"
)

// ParsePublicBaseURL reads PUBLIC_BASE_URL strictly: http or https, a host, nothing else — no path,
// query, fragment, credentials or surrounding whitespace. It is the origin every canonical URL and the host redirect are
// built from, so a malformed one must stop the boot, not quietly point the whole site elsewhere
// (AOC-025 review: " https://aoc-codex.app" parsed to the canonical host "https").
func ParsePublicBaseURL(s string) (*url.URL, error) {
	// url.Parse itself refuses the pasted-with-a-space and trailing-newline spellings (a leading
	// space makes "https:" a first path segment; control and space characters are invalid in a
	// host), so every shape the review measured fails here — TestPublicBaseURLIsParsedStrictly.
	u, err := url.Parse(s)
	if err != nil {
		return nil, fmt.Errorf("PUBLIC_BASE_URL %q: %w", s, err)
	}
	if (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" || (u.Path != "" && u.Path != "/") ||
		u.RawQuery != "" || u.Fragment != "" || u.User != nil {
		return nil, fmt.Errorf("PUBLIC_BASE_URL %q is not an origin like https://aoc-codex.app", s)
	}
	return &url.URL{Scheme: u.Scheme, Host: u.Host}, nil
}

// canonicalHost 301s every request under a host that is not the canonical one to the same path and
// query on it (AOC-025). The service also answers on its Railway domain
// (aoc-armory-snapshot-production.up.railway.app), a crawlable 200 copy of the whole site; a redirect
// folds it into the canonical host outright, where a canonical tag is only a hint and a noindex
// beside a canonical is a mixed signal Google may carry to the target.
//
// ⛔ Never on the canonical host itself. Measured 2026-09-30: requests reach the origin as
// `Host: aoc-codex.app` — Railway routes custom domains by Host — so this rule cannot touch the real
// site. /health is exempt: it is read by monitors on whatever host they were given. Hosts compare
// lowercased, without a port or a trailing dot. 308 for anything but GET/HEAD, which keeps the method.
func canonicalHost(base *url.URL) func(http.Handler) http.Handler {
	want := hostOnly(base.Host)
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if hostOnly(r.Host) == want || r.URL.Path == "/health" {
				next.ServeHTTP(w, r)
				return
			}
			code := http.StatusMovedPermanently
			if r.Method != http.MethodGet && r.Method != http.MethodHead {
				code = http.StatusPermanentRedirect
			}
			// #nosec G710 -- not an open redirect: scheme and host are PUBLIC_BASE_URL's, only the path
			// and query are the request's, so even "//evil.example/x" stays on the canonical host
			// (pinned by TestEveryOtherHostIsRedirectedToTheCanonicalOne).
			http.Redirect(w, r, base.Scheme+"://"+base.Host+r.URL.RequestURI(), code)
		})
	}
}

func hostOnly(h string) string {
	if host, _, err := net.SplitHostPort(h); err == nil {
		h = host
	}
	return strings.ToLower(strings.TrimSuffix(h, "."))
}
