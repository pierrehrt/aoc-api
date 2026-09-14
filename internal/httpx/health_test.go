package httpx_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/pierrehrt/aoc-api/internal/httpx"
)

func TestHealthReportsTheBuild(t *testing.T) {
	rr := do(t, httpx.NewRouter("1.2.3", "abc1234"), http.MethodGet, "/health")

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rr.Code)
	}
	if ct := rr.Header().Get("Content-Type"); ct != "application/json; charset=utf-8" {
		t.Errorf("Content-Type = %q, want JSON", ct)
	}

	var body httpx.HealthBody
	if err := json.Unmarshal(rr.Body.Bytes(), &body); err != nil {
		t.Fatalf("body is not valid JSON: %v (%s)", err, rr.Body.String())
	}
	if body.Status != "ok" || body.Version != "1.2.3" || body.Commit != "abc1234" {
		t.Errorf("body = %+v, want the version and commit it was built with", body)
	}
}

// /health must stay OFF /v1: it is operational surface, not the API contract, and it
// must not fork when /v2 arrives.
func TestHealthIsNotVersioned(t *testing.T) {
	rr := do(t, httpx.NewRouter("dev", "none"), http.MethodGet, "/v1/health")
	if rr.Code != http.StatusNotFound {
		t.Errorf("GET /v1/health = %d, want 404 — health must not live under /v1", rr.Code)
	}
}

// The /v1 mount must EXIST even while empty, so an unknown path under it is a 404 from
// our mapper rather than a 500 or chi's plain text.
func TestUnknownPathUnderV1Is404JSON(t *testing.T) {
	rr := do(t, httpx.NewRouter("dev", "none"), http.MethodGet, "/v1/nope")

	if rr.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rr.Code)
	}
	var body httpx.ErrorBody
	if err := json.Unmarshal(rr.Body.Bytes(), &body); err != nil {
		t.Fatalf("404 body is not JSON: %v (%s)", err, rr.Body.String())
	}
	if body.Error != "not found" {
		t.Errorf("error = %q, want %q", body.Error, "not found")
	}
	if body.RequestID == "" {
		t.Error("404 carries no request_id — a user cannot quote it in a bug report")
	}
}

func TestWrongMethodIs405(t *testing.T) {
	rr := do(t, httpx.NewRouter("dev", "none"), http.MethodPost, "/health")
	if rr.Code != http.StatusMethodNotAllowed {
		t.Errorf("POST /health = %d, want 405", rr.Code)
	}
}

func TestEveryResponseCarriesARequestID(t *testing.T) {
	rr := do(t, httpx.NewRouter("dev", "none"), http.MethodGet, "/health")
	if rr.Header().Get(httpx.HeaderRequestID) == "" {
		t.Error("no X-Request-Id on a successful response")
	}
}

// An inbound id is echoed for correlation, but an absurd one is replaced rather than
// written into our logs at whatever length the caller chose.
func TestInboundRequestIDIsEchoedButCapped(t *testing.T) {
	r := httpx.NewRouter("dev", "none")

	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	req.Header.Set(httpx.HeaderRequestID, "trace-from-the-edge")
	rr := httptest.NewRecorder()
	r.ServeHTTP(rr, req)
	if got := rr.Header().Get(httpx.HeaderRequestID); got != "trace-from-the-edge" {
		t.Errorf("id = %q, want it echoed", got)
	}

	long := make([]byte, 200)
	for i := range long {
		long[i] = 'x'
	}
	req = httptest.NewRequest(http.MethodGet, "/health", nil)
	req.Header.Set(httpx.HeaderRequestID, string(long))
	rr = httptest.NewRecorder()
	r.ServeHTTP(rr, req)
	if got := rr.Header().Get(httpx.HeaderRequestID); len(got) > 64 {
		t.Errorf("id length = %d, want an over-long inbound id replaced", len(got))
	}
}

func do(t *testing.T, h http.Handler, method, path string) *httptest.ResponseRecorder {
	t.Helper()
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, httptest.NewRequest(method, path, nil))
	return rr
}
