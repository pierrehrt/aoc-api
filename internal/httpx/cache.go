package httpx

import (
	"context"
	"net/http"
	"strings"
)

// The whole cache policy is these six values and cachePolicy below. Nothing else in the binary
// sets Cache-Control: a handler that tries has its value replaced, and TestOnlyCacheGoNamesTheHeader
// fails the build if any other Go file so much as names the header (AOC-026).
//
// Why a middleware and not a line per handler: a rule written in forty handlers is enforced in the
// thirty-nine somebody remembered. The dangerous mistake here is not a slow page, it is a response
// that depends on who is asking landing in Cloudflare's shared cache and being served to everyone.
const (
	// Content-hashed files: the URL changes whenever the bytes do, so no cache anywhere can hold a
	// stale asset under a live name, and a year is safe.
	cacheImmutable = "public, max-age=31536000, immutable"
	// Public pages. Browsers recheck after a minute, so a correction reaches a reader who reloads;
	// the edge keeps an hour, which is what stops a viral link from billing us per view, and a
	// purge makes a change immediate.
	cachePage = "public, max-age=60, s-maxage=3600, stale-while-revalidate=86400"
	// The /v1 contract: a shorter edge window, because a client polling it wants fresher data than
	// a reader scrolling a page.
	cacheAPI = "public, max-age=60, s-maxage=600"
	// 404 and 410: cached, briefly, so a burst of requests for a missing URL cannot all reach the
	// origin, and short enough that a newly published page is visible within a minute.
	cacheMissing = "public, max-age=60, s-maxage=60"
	// Everything that must never be stored: /health, errors, writes, HTMX fragments.
	cacheNoStore = "no-store"
	// Anything that depends on who is asking. Wins over every other rule.
	cachePrivate = "private, no-store"
)

// cachePolicy is the whole decision, made once, when the status is written.
//
// ⛔ Private comes first and overrides everything, including a value the handler set itself:
//   - MarkPrivate was called — the response depends on a session, an account, a draft;
//   - the response sets a cookie — if it did, it is someone's;
//   - the request carries Authorization — RFC 9111 lets a shared cache store the answer to an
//     authorised request when the response says `public`, which is exactly what this policy says
//     for pages, so it must never say it here.
//
// The request's Cookie header is deliberately NOT a trigger. Cloudflare's own bot-management
// cookies ride on ordinary requests, and keying on them would switch caching off for everyone.
func cachePolicy(r *http.Request, status int, h http.Header, private bool) string {
	switch {
	case private, len(h.Values("Set-Cookie")) > 0, r.Header.Get("Authorization") != "":
		return cachePrivate
	case r.Method != http.MethodGet && r.Method != http.MethodHead:
		return cacheNoStore
	// A fragment is never stored in a shared cache, under any URL. That closes the dangerous case
	// — a bare fragment handed to a browser that asked for the page — whether or not the edge
	// honours Vary. (templates.IsHTMX is the same test; httpx sits below templates and cannot
	// import it.)
	case strings.EqualFold(r.Header.Get("HX-Request"), "true"):
		return cacheNoStore
	case r.URL.Path == "/health":
		return cacheNoStore
	}

	path := r.URL.Path
	switch status {
	case http.StatusOK, http.StatusNotModified, http.StatusMovedPermanently, http.StatusPermanentRedirect:
		// the path's policy, below
	case http.StatusNotFound, http.StatusGone:
		// ⚠️ Not under /assets/: there a 404 is a stale hash or a deploy racing its own HTML, and
		// caching it would leave a page unstyled at that edge for as long as the entry lives.
		if strings.HasPrefix(path, "/assets/") {
			return cacheNoStore
		}
		return cacheMissing
	default:
		// Every other status — a 400, a 405, a 429, every 5xx — is about this request, not about
		// the resource. An outage must never be cached as though it were the page.
		return cacheNoStore
	}

	switch {
	case strings.HasPrefix(path, "/assets/"):
		return cacheImmutable
	case path == "/v1" || strings.HasPrefix(path, "/v1/"):
		return cacheAPI
	default:
		return cachePage
	}
}

// Cache sets Cache-Control on every response from the policy above.
//
// It sits OUTSIDE Recover (NewRouterWithSite): a panic's 500 is written by Recover, and it must
// pass through here to leave as no-store rather than with no header at all.
func Cache(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		st := &cacheState{}
		cw := &cacheWriter{ResponseWriter: w, r: r, st: st}
		next.ServeHTTP(cw, r.WithContext(context.WithValue(r.Context(), cacheKey{}, st)))
		// A handler that returns without writing gets an implicit 200 from net/http. Send it here
		// instead, so it goes out with the header like every other response.
		if !st.decided {
			cw.WriteHeader(http.StatusOK)
		}
	})
}

// MarkPrivate declares that this response depends on who is asking, so no shared cache may store
// it. ⚠️ EP-06: every code path that reads a session or an account must call it — the session
// accessor is the place, so it cannot be forgotten per handler.
//
// It fails CLOSED, by panicking, in the two cases where it could not do its job: when the Cache
// middleware is not on the route (nothing would read the mark), and when the response has already
// begun (a public header is already on the wire). Both are programming errors a first test finds.
func MarkPrivate(r *http.Request) {
	st, ok := r.Context().Value(cacheKey{}).(*cacheState)
	if !ok {
		panic("httpx.MarkPrivate: the Cache middleware is not installed on this route")
	}
	if st.decided && !st.private {
		panic("httpx.MarkPrivate: called after the response began; its Cache-Control has already been sent")
	}
	st.private = true
}

type cacheKey struct{}

// cacheState is shared by the writer and MarkPrivate through the request context.
type cacheState struct {
	private bool
	decided bool
}

type cacheWriter struct {
	http.ResponseWriter
	r  *http.Request
	st *cacheState
}

func (w *cacheWriter) WriteHeader(code int) {
	// 1xx is informational and can precede the real status; the decision waits for that.
	if !w.st.decided && code >= 200 {
		w.st.decided = true
		w.Header().Set("Cache-Control", cachePolicy(w.r, code, w.Header(), w.st.private))
	}
	w.ResponseWriter.WriteHeader(code)
}

func (w *cacheWriter) Write(b []byte) (int, error) {
	if !w.st.decided {
		w.WriteHeader(http.StatusOK)
	}
	return w.ResponseWriter.Write(b)
}

// FlushError decides the header before a flush can put the response on the wire. A flush sends
// the header map as it stands, so without this method http.ResponseController would follow Unwrap
// to the inner writer and send a handler's own `public` beside its Set-Cookie, or no Cache-Control
// at all — measured by AOC-026's verify round 1. ResponseController asks for FlushError before it
// tries Unwrap, so every flush comes through here.
func (w *cacheWriter) FlushError() error {
	if !w.st.decided {
		w.WriteHeader(http.StatusOK)
	}
	return http.NewResponseController(w.ResponseWriter).Flush()
}

// Flush is FlushError for code that type-asserts http.Flusher instead of using ResponseController.
func (w *cacheWriter) Flush() { _ = w.FlushError() }

// Unwrap keeps Hijack and deadlines reachable through http.ResponseController. ⚠️ A hijacked
// connection is raw bytes the handler writes itself, so it is outside this policy by construction.
func (w *cacheWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }

// Wrote is what Recover asks before writing an error. This writer sits between Log's and
// Recover, so without it Recover would see a writer that cannot answer, assume nothing was sent,
// and append a JSON error to a body that had already begun.
func (w *cacheWriter) Wrote() bool { return w.st.decided }
