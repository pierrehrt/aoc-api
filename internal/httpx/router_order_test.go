package httpx_test

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/pierrehrt/aoc-api/internal/httpx"
)

// Added by verify round 2 (AOC-002).
//
// TestPanicStillProducesAnAccessLine builds the middleware chain BY HAND, so it asserts
// a property of RequestID(Log(Recover(...))) -- not a property of the router the binary
// serves. Measured: reverting NewRouter to RequestID -> Recover -> Log leaves the whole
// suite green. This drives the PRODUCTION router instead, so the order is asserted where
// it is actually chosen.
func TestProductionRouterLogsAPanickingRequest(t *testing.T) {
	var buf bytes.Buffer
	restore := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&buf, nil)))
	defer slog.SetDefault(restore)

	r := httpx.NewRouter("dev", "none")
	boom := chi.NewRouter()
	boom.Get("/boom", func(http.ResponseWriter, *http.Request) { panic("boom") })
	r.Mount("/verify-order-probe", boom)

	rr := httptest.NewRecorder()
	r.ServeHTTP(rr, httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/verify-order-probe/boom", nil))

	if rr.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", rr.Code)
	}

	var access []map[string]any
	for _, line := range strings.Split(strings.TrimSpace(buf.String()), "\n") {
		var m map[string]any
		if json.Unmarshal([]byte(line), &m) == nil && m["msg"] == "request" {
			access = append(access, m)
		}
	}
	if len(access) != 1 {
		t.Fatalf("a panicking request produced %d access lines, want exactly 1 -- "+
			"NewRouter's middleware order is wrong (Recover must be INSIDE Log)", len(access))
	}
	if got := access[0]["status"]; got != float64(http.StatusInternalServerError) {
		t.Errorf("access line status = %v, want 500", got)
	}
	if rid, _ := access[0]["request_id"].(string); rid == "" {
		t.Error("access line carries no request_id")
	}
}

// The production router must also emit exactly one access line for an ORDINARY request.
// Removing r.Use(Log) from NewRouter leaves the whole suite green otherwise.
func TestProductionRouterLogsEveryRequest(t *testing.T) {
	var buf bytes.Buffer
	restore := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&buf, nil)))
	defer slog.SetDefault(restore)

	rr := httptest.NewRecorder()
	httpx.NewRouter("dev", "none").ServeHTTP(rr, httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/health", nil))

	n := 0
	for _, line := range strings.Split(strings.TrimSpace(buf.String()), "\n") {
		var m map[string]any
		if json.Unmarshal([]byte(line), &m) == nil && m["msg"] == "request" {
			n++
			if got := m["status"]; got != float64(http.StatusOK) {
				t.Errorf("access line status = %v, want 200", got)
			}
			if rid, _ := m["request_id"].(string); rid == "" {
				t.Error("access line carries no request_id")
			}
		}
	}
	if n != 1 {
		t.Fatalf("GET /health produced %d access lines, want exactly 1 -- "+
			"NewRouter is not using the Log middleware", n)
	}
}
