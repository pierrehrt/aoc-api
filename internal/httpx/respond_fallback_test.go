package httpx_test

import (
	"context"
	"math"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/pierrehrt/aoc-api/internal/httpx"
)

// Added by AOC-028 verify round 4. docs/api-routes.md § Conventions names one error body on /v1 that
// is not {"error","request_id"}: the fallback Respond writes when it cannot encode its own response.
// Nothing pinned it, so the sentence was "read, not measured". This holds what the sentence says,
// through the production router: a 500, JSON, exactly {"error":"internal error"} with no request_id —
// and, like every response the router writes, an X-Request-Id and a no-store Cache-Control.
// A NaN is the value JSON cannot encode (a float64 field the database could hand back).
func TestRespondFallbackIsTheDocumentedShape(t *testing.T) {
	r := httpx.NewRouterWithAPI(httpx.Build{Version: "dev", Commit: "none", Env: "test"}, nil, nil, func(v1 chi.Router) {
		v1.Get("/nan", func(w http.ResponseWriter, r *http.Request) {
			httpx.Respond(w, r, http.StatusOK, map[string]float64{"value": math.NaN()})
		})
	})

	rr := httptest.NewRecorder()
	r.ServeHTTP(rr, httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/v1/nan", nil))

	if rr.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want 500", rr.Code)
	}
	if ct := rr.Header().Get("Content-Type"); ct != "application/json; charset=utf-8" {
		t.Errorf("Content-Type = %q, want JSON", ct)
	}
	if got, want := rr.Body.String(), `{"error":"internal error"}`; got != want {
		t.Errorf("body = %s, want %s (the shape docs/api-routes.md names)", got, want)
	}
	if rr.Header().Get(httpx.HeaderRequestID) == "" {
		t.Error("the fallback 500 carries no X-Request-Id")
	}
	if cc := rr.Header().Get("Cache-Control"); cc != "no-store" {
		t.Errorf("Cache-Control = %q, want no-store for a 500", cc)
	}
}
