package httpx

import (
	"net/http"
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
// SiteRoutes mounts the HTML surface. Nil means "JSON only", which is what the tests
// of the API surface use and what the service did before AOC-024.
type SiteRoutes func(chi.Router)

// NewRouter keeps the JSON-only shape every existing caller expects.
func NewRouter(ver, commit string) *chi.Mux { return NewRouterWithSite(ver, commit, nil, nil) }

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
func NewRouterWithSite(ver, commit string, site SiteRoutes, assets http.Handler) *chi.Mux {
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
	r.Use(RequestID)
	r.Use(Log)
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

	r.Get("/health", Health(ver, commit))

	v1 := chi.NewRouter()
	// Domain sub-routers mount here as they arrive: items (AOC-012), content,
	// moderation, users. Nothing else goes on the root router.
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
