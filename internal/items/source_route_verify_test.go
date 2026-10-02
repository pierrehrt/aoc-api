package items

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// AOC-050, verify round 1. /v1/sources/tree through its handler, as a consumer reads it — the test
// server in http_test.go does not mount it, so nothing else reached the route. docs/api-routes.md
// § Attribution: "Every response on every route carries attribution" (the public contract, AOC-055);
// an unknown tab is a 404 and a malformed source a 400, through the central mapping.
func TestTheSourceTreeRouteAnswersLikeEveryV1Route(t *testing.T) {
	h := NewHandler(NewService(treeQ()), fakeTax{})
	mux := http.NewServeMux()
	mux.Handle("/v1/sources/", http.StripPrefix("/v1/sources", h.SourceRoutes()))
	call := func(path string) (int, map[string]any) {
		t.Helper()
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, httptest.NewRequestWithContext(context.Background(), http.MethodGet, path, nil))
		var body map[string]any
		if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
			t.Fatalf("%s: not JSON (%v): %s", path, err, rec.Body.String())
		}
		return rec.Code, body
	}

	code, body := call("/v1/sources/tree?tab=test-first")
	if code != http.StatusOK {
		t.Fatalf("tree -> %d, want 200: %v", code, body)
	}
	if s, ok := body["attribution"].(string); !ok || s != Attribution {
		t.Errorf("tree: attribution = %v, want %q on every /v1 response (docs/api-routes.md § Attribution)", body["attribution"], Attribution)
	}
	if _, ok := body["nodes"].([]any); !ok {
		t.Errorf("tree: nodes = %v, want a list, [] when empty", body["nodes"])
	}

	if code, body = call("/v1/sources/tree?tab=test-nope"); code != http.StatusNotFound || body["error"] != "not found" {
		t.Errorf("unknown tab -> %d %v, want 404 {\"error\":\"not found\"}", code, body)
	}
	if code, body = call("/v1/sources/tree?tab=test-first&source=x:test-thing"); code != http.StatusBadRequest || body["error"] != "invalid request" {
		t.Errorf("malformed source -> %d %v, want 400 {\"error\":\"invalid request\"}", code, body)
	}
}
