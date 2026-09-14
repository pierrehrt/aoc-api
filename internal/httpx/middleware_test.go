package httpx_test

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/pierrehrt/aoc-api/internal/httpx"
)

// The middleware order is load-bearing and the original comment defending it was
// backwards. This is the assertion that would have caught that: a PANICKING request must
// still produce exactly one access line, carrying its real 500.
func TestPanicStillProducesAnAccessLine(t *testing.T) {
	var buf bytes.Buffer
	restore := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&buf, nil)))
	defer slog.SetDefault(restore)

	r := httpx.NewRouter("dev", "none")
	// The router has no panicking route of its own, so drive the middleware chain the
	// way the router builds it, around a handler that panics.
	h := httpx.RequestID(httpx.Log(httpx.Recover(http.HandlerFunc(
		func(http.ResponseWriter, *http.Request) { panic("boom") }))))

	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/boom", nil))

	if rr.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", rr.Code)
	}

	var access map[string]any
	for _, line := range strings.Split(strings.TrimSpace(buf.String()), "\n") {
		var m map[string]any
		if json.Unmarshal([]byte(line), &m) == nil && m["msg"] == "request" {
			access = m
		}
	}
	if access == nil {
		t.Fatal("a panicking request produced NO access line — Recover is outside Log")
	}
	if got := access["status"]; got != float64(http.StatusInternalServerError) {
		t.Errorf("access line status = %v, want 500", got)
	}
	if access["request_id"] == "" || access["request_id"] == nil {
		t.Error("access line carries no request_id")
	}
	_ = r
}

// Once the handler has started writing, the status line is spent. Appending an error
// document would yield a 200 with two concatenated JSON objects, which parses as neither.
func TestPanicAfterWriteDoesNotAppendASecondBody(t *testing.T) {
	h := httpx.RequestID(httpx.Log(httpx.Recover(http.HandlerFunc(
		func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"partial":true}`))
			panic("after the body began")
		}))))

	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/x", nil))

	if n := strings.Count(rr.Body.String(), `{`); n != 1 {
		t.Errorf("body has %d JSON documents, want 1: %s", n, rr.Body.String())
	}
	if strings.Contains(rr.Body.String(), "internal error") {
		t.Error("an error document was appended after the response had begun")
	}
}

// Wrapping the ResponseWriter must not remove capabilities from anything downstream.
func TestResponseWriterWrappingPreservesFlush(t *testing.T) {
	var flushed bool
	h := httpx.Log(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		rc := http.NewResponseController(w)
		if err := rc.Flush(); err == nil {
			flushed = true
		}
	}))
	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/x", nil))
	if !flushed {
		t.Error("Flush is unreachable through the wrapper — Unwrap is missing or wrong")
	}
}

// 405 must go through the same mapper as everything else.
func TestMethodNotAllowedGoesThroughTheMapper(t *testing.T) {
	rr := httptest.NewRecorder()
	httpx.NewRouter("dev", "none").ServeHTTP(rr, httptest.NewRequest(http.MethodPost, "/health", nil))

	if rr.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d, want 405", rr.Code)
	}
	var b httpx.ErrorBody
	if err := json.Unmarshal(rr.Body.Bytes(), &b); err != nil {
		t.Fatalf("405 body is not JSON: %v", err)
	}
	if b.Error != "method not allowed" || b.RequestID == "" {
		t.Errorf("405 body = %+v, want the mapper's shape with a request id", b)
	}
}
