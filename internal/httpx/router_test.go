package httpx_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/pierrehrt/aoc-api/internal/httpx"
)

// Added by verify round 1 (AOC-002).
//
// The existing tests reach /v1 only through a request for an unknown path. That cannot
// distinguish "the /v1 sub-router is mounted and empty" from "there is no /v1 at all":
// the root NotFound handler answers both with the same JSON 404. Deleting
// r.Mount("/v1", v1) entirely leaves the whole suite green, which means the ticket's
// headline structural deliverable is asserted by nothing.
//
// This inspects chi's route tree instead of the response, so it fails if the mount is
// removed, and fails if /v1 is ever reintroduced as a bare path prefix rather than a
// mounted sub-router — which is the property CLAUDE.md rule 5c depends on.
func TestV1IsAMountedSubRouter(t *testing.T) {
	var v1 *chi.Route
	for _, rt := range httpx.NewRouter(httpx.Build{Version: "dev", Commit: "none", Env: "test"}).Routes() {
		if strings.HasPrefix(rt.Pattern, "/v1") {
			r := rt
			v1 = &r
			break
		}
	}
	if v1 == nil {
		t.Fatal("no /v1 route in the tree — the sub-router is not mounted")
	}
	if v1.SubRoutes == nil {
		t.Fatalf("/v1 pattern %q has no SubRoutes: it is a path prefix, not a mounted "+
			"sub-router, so a /v2 cannot mount beside it (CLAUDE.md rule 5c)", v1.Pattern)
	}
}

// /health must be a route on the ROOT mux, not inside the /v1 sub-router. Asserting the
// 404 on /v1/health does not prove this either — an unmounted /v1 gives the same 404.
func TestHealthIsRegisteredOnTheRootMux(t *testing.T) {
	found := false
	for _, rt := range httpx.NewRouter(httpx.Build{Version: "dev", Commit: "none", Env: "test"}).Routes() {
		if rt.Pattern == "/health" {
			found = true
			if _, ok := rt.Handlers[http.MethodGet]; !ok {
				t.Errorf("/health exists on the root mux but not for GET")
			}
		}
	}
	if !found {
		t.Error("/health is not a root route — it must not live under /v1")
	}
}

// The existing panic test builds its own chi router and wires Recover by hand, so it
// passes even if NewRouter stops using Recover at all. This exercises the router the
// binary actually serves.
func TestProductionRouterRecoversPanics(t *testing.T) {
	r := httpx.NewRouter(httpx.Build{Version: "dev", Commit: "none", Env: "test"})
	boom := chi.NewRouter()
	boom.Get("/boom", func(http.ResponseWriter, *http.Request) {
		panic("secret in the panic value: items_private")
	})
	r.Mount("/verify-probe", boom)

	rr := httptest.NewRecorder()
	r.ServeHTTP(rr, httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/verify-probe/boom", nil))

	if rr.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500 — the production router is not recovering panics", rr.Code)
	}
	if ct := rr.Header().Get("Content-Type"); ct != "application/json; charset=utf-8" {
		t.Errorf("Content-Type = %q, want JSON on a recovered panic", ct)
	}
	body := rr.Body.String()
	for _, leak := range []string{"items_private", "goroutine", ".go:", "panic("} {
		if strings.Contains(body, leak) {
			t.Errorf("panic detail %q reached the client: %s", leak, body)
		}
	}
	var b httpx.ErrorBody
	if err := json.Unmarshal([]byte(body), &b); err != nil {
		t.Fatalf("panic response is not JSON: %v (%s)", err, body)
	}
	if b.Error != "internal error" {
		t.Errorf("error = %q, want the flat %q", b.Error, "internal error")
	}
	if b.RequestID == "" {
		t.Error("no request_id on a recovered panic — nothing to correlate the log with")
	}
	if rr.Header().Get(httpx.HeaderRequestID) == "" {
		t.Error("no X-Request-Id header on a recovered panic")
	}
}

// Every error response must carry the JSON content type, not just /health.
func TestErrorResponsesAreJSONContentType(t *testing.T) {
	for _, tc := range []struct {
		name, method, path string
		want               int
	}{
		{"404", http.MethodGet, "/v1/nope", http.StatusNotFound},
		{"405", http.MethodPost, "/health", http.StatusMethodNotAllowed},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rr := httptest.NewRecorder()
			httpx.NewRouter(httpx.Build{Version: "dev", Commit: "none", Env: "test"}).ServeHTTP(rr, httptest.NewRequestWithContext(context.Background(), tc.method, tc.path, nil))
			if rr.Code != tc.want {
				t.Fatalf("status = %d, want %d", rr.Code, tc.want)
			}
			if ct := rr.Header().Get("Content-Type"); ct != "application/json; charset=utf-8" {
				t.Errorf("Content-Type = %q, want JSON", ct)
			}
			var b httpx.ErrorBody
			if err := json.Unmarshal(rr.Body.Bytes(), &b); err != nil {
				t.Fatalf("body is not JSON: %v (%s)", err, rr.Body.String())
			}
			if b.RequestID == "" {
				t.Error("error body carries no request_id")
			}
		})
	}
}

// An attacker-supplied request id is echoed and logged verbatim; confirm it cannot break
// out of the JSON body it is written into.
func TestHostileRequestIDCannotBreakTheJSONBody(t *testing.T) {
	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/v1/nope", nil)
	req.Header.Set(httpx.HeaderRequestID, `a","error":"forged`)
	rr := httptest.NewRecorder()
	httpx.NewRouter(httpx.Build{Version: "dev", Commit: "none", Env: "test"}).ServeHTTP(rr, req)

	var b httpx.ErrorBody
	if err := json.Unmarshal(rr.Body.Bytes(), &b); err != nil {
		t.Fatalf("body is not JSON: %v (%s)", err, rr.Body.String())
	}
	if b.Error != "not found" {
		t.Errorf("error = %q — a crafted request id overwrote the error field", b.Error)
	}
}
