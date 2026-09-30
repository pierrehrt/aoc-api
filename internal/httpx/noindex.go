package httpx

import (
	"net"
	"net/http"
	"net/url"
	"strings"
)

// NoIndexOffCanonicalHost marks every response served under any host but the canonical one with
// `X-Robots-Tag: noindex` (AOC-025).
//
// ⭐ Why: the service also answers on its Railway domain (aoc-armory-snapshot-production.up.railway.app),
// a crawlable 200 copy of the whole site. Its pages already point rel=canonical at aoc-codex.app,
// but a canonical is a HINT Google may ignore; noindex is a directive. robots.txt cannot do it —
// disallowing the crawl would stop Google from ever SEEING the noindex, which can leave the URL
// indexed without a snippet.
//
// ⛔ Never on the canonical host itself. Measured 2026-09-30: requests reach the origin as
// `Host: aoc-codex.app` (Railway routes custom domains by Host; DNS is a proxied CNAME to its
// per-domain target), and production's PUBLIC_BASE_URL is https://aoc-codex.app. The host is
// compared lowercased and without a port, the way RFC 9110 says hosts compare.
func NoIndexOffCanonicalHost(baseURL string) func(http.Handler) http.Handler {
	canonical := hostOnly(baseURL)
	if u, err := url.Parse(baseURL); err == nil && u.Host != "" {
		canonical = hostOnly(u.Host)
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if hostOnly(r.Host) != canonical {
				w.Header().Set("X-Robots-Tag", "noindex")
			}
			next.ServeHTTP(w, r)
		})
	}
}

func hostOnly(h string) string {
	if host, _, err := net.SplitHostPort(h); err == nil {
		h = host
	}
	return strings.ToLower(strings.TrimSuffix(h, "."))
}
