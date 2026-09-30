package httpx

import (
	"fmt"
	"html/template"
	"net/http"
	"net/url"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
)

// NewRouter builds the whole route tree.
//
// Two things here are structural and every later api ticket inherits them:
//
//  1. /health is NOT under /v1. It is operational surface — read by Railway and by a
//     human checking which build is live — not product surface, so it must not be
//     versioned alongside the API contract. When /v2 arrives, /health does not fork.
//
//  2. /v1 is a chi SUB-ROUTER, mounted rather than prefixed. That is what makes rule
//     5c cheap later: a /v2 with different semantics mounts beside it and both serve
//     at once, which is the only way to change a response shape without breaking a
//     browser holding a cached bundle. Domain routers mount INSIDE v1
//     (v1.Mount("/items", items.Routes(...))), never on the root.
//
// V1Routes mounts the JSON domain sub-routers inside /v1. Nil means "no domains yet", which is
// what most of httpx's own tests want — they are about the router's shape, not about items.
//
// It takes the sub-router rather than a concrete handler so that httpx does not import a domain
// package: domains depend on httpx for error mapping, and the arrow must point one way.
type V1Routes func(chi.Router)

// SiteRoutes mounts the HTML surface. Nil means "JSON only", which is what the tests
// of the API surface use and what the service did before AOC-024.
type SiteRoutes func(chi.Router)

// NewRouter keeps the JSON-only shape every existing caller expects.
func NewRouter(b Build) *chi.Mux { return NewRouterWithSite(b, nil, nil) }

// NewRouterWithAPI is NewRouterWithSite plus the /v1 domain routers.
func NewRouterWithAPI(b Build, site SiteRoutes, assets http.Handler, v1 V1Routes, opts ...RouterOption) *chi.Mux {
	return newRouter(b, site, assets, v1, opts...)
}

// RouterOption adjusts the router newRouter builds.
type RouterOption func(*routerOpts)

type routerOpts struct{ canonical *url.URL }

// WithCanonicalHost makes every request under another host a 301 to the same path on this origin
// (AOC-025) — inside the router, so the 404 and 405 pages, /v1 and the assets are covered, and the
// router tests exercise it as it is composed in production.
func WithCanonicalHost(base *url.URL) RouterOption {
	return func(o *routerOpts) { o.canonical = base }
}

// NewRouterWithSite additionally mounts the server-rendered site and its assets.
//
// ⭐ THE 404 PROBLEM, and why it is solved here rather than in a handler.
//
// This service now answers two audiences on one origin. chi has ONE NotFound handler, so
// before this ticket every miss returned JSON — meaning a person who mistyped a URL got
// `{"error":"not found"}` in their browser. Which shape is right depends on who asked:
// anything under /v1 is API surface and must stay JSON forever (a client parses it),
// everything else is the website and should be a readable page.
//
// The test is the PATH, not the Accept header. Accept is a negotiation a bot or a proxy
// can get wrong, while the path is a fact about which contract was addressed.
func NewRouterWithSite(b Build, site SiteRoutes, assets http.Handler) *chi.Mux {
	return newRouter(b, site, assets, nil)
}

func newRouter(b Build, site SiteRoutes, assets http.Handler, mountV1 V1Routes, opts ...RouterOption) *chi.Mux {
	var o routerOpts
	for _, opt := range opts {
		opt(&o)
	}
	r := chi.NewRouter()

	// Order matters, and it is the opposite of what it first looks like.
	//
	// RequestID is outermost so everything downstream can log the id.
	//
	// Log then wraps Recover, NOT the other way round. With Recover outside, a panic
	// unwinds PAST Log before Log can record anything, so a panicking request produces
	// no access line at all -- the one request you most want in the log is the one that
	// vanishes from it. With Recover inside, the panic is caught within Log's call, Log
	// resumes, and the request is logged with its real status of 500.
	//
	// This was measured, not reasoned: verify round 1 of AOC-002 captured slog output
	// under both orders. The original order, and the comment defending it, were wrong.
	//
	// Cache sits between them (AOC-026): inside Log, and OUTSIDE Recover, so the 500 Recover
	// writes for a panic still passes through the policy and leaves as no-store.
	r.Use(RequestID)
	r.Use(Log)
	r.Use(Cache)
	// The host redirect sits INSIDE Cache, so its 301 leaves with the path's policy like any page.
	if o.canonical != nil {
		r.Use(canonicalHost(o.canonical))
	}
	r.Use(Recover)

	// ⭐ HEAD. chi's r.Get registers GET only, so every public page answered HEAD with 405
	// — on a site whose entire purpose is being crawled and linked, where uptime monitors,
	// link checkers and `curl -I` all default to HEAD (AOC-024 verify round 2, measured
	// live). GetHead routes an unmatched HEAD to the GET handler and discards the body,
	// which keeps Content-Length honest rather than faking it per route.
	r.Use(middleware.GetHead)

	// chi's defaults write text/plain. Route them through the central mapper so that
	// every response from this service, success or failure, is the same JSON shape.
	r.NotFound(htmlAwareFor(site != nil, NotFound, http.StatusNotFound, notFoundHTML))
	// ⚠️ 405 follows the SAME path rule as 404. It did not, and that was this ticket's own
	// listed edge case: a fragment route reached by a plain browser GET returned raw JSON
	// to a person (AOC-024 verify round 1). Whichever rejection it is, the shape must be
	// chosen by who asked, not by which chi hook happened to fire.
	r.MethodNotAllowed(htmlAwareFor(site != nil, MethodNotAllowed, http.StatusMethodNotAllowed, methodNotAllowedHTML))

	r.Get("/health", Health(b))

	v1 := chi.NewRouter()
	// Domain sub-routers mount here as they arrive: items (AOC-012), content,
	// moderation, users. Nothing else goes on the root router.
	if mountV1 != nil {
		mountV1(v1)
	}
	r.Mount("/v1", v1)

	if assets != nil {
		r.Handle("/assets/*", assets)
	}
	if site != nil {
		site(r)
	}

	return r
}

// htmlAwareFor returns a rejection handler whose SHAPE follows the request path.
//
// hasSite is false in JSON-only tests and in any deployment without the HTML surface;
// there every rejection is JSON, exactly as before.
//
// The test is the PATH, not the Accept header: a path is a fact about which contract was
// addressed, where Accept is a negotiation a bot or a proxy can get wrong.
func htmlAwareFor(hasSite bool, jsonHandler http.HandlerFunc, status int, body string) http.HandlerFunc {
	if !hasSite {
		return jsonHandler
	}
	return func(w http.ResponseWriter, r *http.Request) {
		if isMachineSurface(r.URL.Path) {
			jsonHandler(w, r)
			return
		}
		// A person is in a browser. Give them something readable, and do NOT render a
		// template for it: these pages must work even when the template engine is the
		// thing that is broken.
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}
}

// RejectHTML answers a page handler's own rejection (a bad query, a page past the end) the way the
// router's 404/405 do: dependency-free HTML on the HTML surface, JSON on a machine surface. Page
// handlers call this rather than Fail, which is JSON-only (AOC-047).
func RejectHTML(w http.ResponseWriter, r *http.Request, status int, title string) {
	if isMachineSurface(r.URL.Path) {
		Fail(w, r, fmt.Errorf("%w: %s", errFor(status), title))
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	_, _ = fmt.Fprintf(w, `<!DOCTYPE html><html lang="en"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width, initial-scale=1"><title>%d — AoC Codex</title></head><body style="font-family:system-ui,sans-serif;background:#1b1a18;color:#eae6df;padding:2rem"><h1>%s</h1><p><a href="/" style="color:#cd975b">Home</a> · <a href="/armory" style="color:#cd975b">Armory</a></p></body></html>`, status, template.HTMLEscapeString(title))
}

// errFor maps a status a page chose back to the sentinel Fail understands, for the JSON shape.
func errFor(status int) error {
	switch status {
	case http.StatusNotFound:
		return ErrNotFound
	default:
		return ErrInvalid
	}
}

// isMachineSurface reports whether a path belongs to a contract something parses.
//
//   - /v1/…    the API contract. A client parses it; it must never become HTML.
//   - /assets/ included even though the asset handler normally answers first: when the
//     site is mounted WITHOUT assets, a stylesheet request must still fail as JSON
//     rather than hand a CSS parser an HTML page.
//   - /health  operational surface, read by Railway and by uptime checks — never by a
//     person in a browser. Adding it was prompted by a 405 test (AOC-024 verify round 1):
//     a rejection there was about to be dressed up as a web page for a monitor.
func isMachineSurface(path string) bool {
	return path == "/health" ||
		path == "/v1" || strings.HasPrefix(path, "/v1/") ||
		strings.HasPrefix(path, "/assets/")
}

// Deliberately dependency-free: no template, no asset, no layout. If this page needed
// the renderer, a broken renderer would have no way to say so.
const methodNotAllowedHTML = `<!DOCTYPE html>
<html lang="en"><head><meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>Not allowed — AoC Codex</title><meta name="robots" content="noindex">
<style>body{background:#0c0a09;color:#e7e5e4;font:16px/1.6 system-ui,sans-serif;
margin:0;display:grid;place-items:center;min-height:100vh}a{color:#fcd34d}</style>
</head><body><main><h1>Not allowed</h1>
<p>That address does not accept this kind of request. <a href="/">Back to the start</a>.</p>
</main></body></html>
`

const notFoundHTML = `<!DOCTYPE html>
<html lang="en"><head><meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>Not found — AoC Codex</title><meta name="robots" content="noindex">
<style>body{background:#0c0a09;color:#e7e5e4;font:16px/1.6 system-ui,sans-serif;
margin:0;display:grid;place-items:center;min-height:100vh}a{color:#fcd34d}</style>
</head><body><main><h1>Not found</h1>
<p>That page does not exist. <a href="/">Back to the start</a>.</p></main></body></html>
`

// compile-time assurance that the router satisfies http.Handler.
var _ http.Handler = (*chi.Mux)(nil)
