package httpx

import (
	"net/http"

	"github.com/go-chi/chi/v5"
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
func NewRouter(ver, commit string) *chi.Mux {
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

	// chi's defaults write text/plain. Route them through the central mapper so that
	// every response from this service, success or failure, is the same JSON shape.
	r.NotFound(NotFound)
	r.MethodNotAllowed(MethodNotAllowed)

	r.Get("/health", Health(ver, commit))

	v1 := chi.NewRouter()
	// Domain sub-routers mount here as they arrive: items (AOC-012), content,
	// moderation, users. Nothing else goes on the root router.
	r.Mount("/v1", v1)

	return r
}

// compile-time assurance that the router satisfies http.Handler.
var _ http.Handler = (*chi.Mux)(nil)
