package httpx_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/pierrehrt/aoc-api/internal/httpx"
)

func TestSentinelErrorsMapToStatuses(t *testing.T) {
	for _, tc := range []struct {
		err  error
		want int
		body string
	}{
		{httpx.ErrNotFound, http.StatusNotFound, "not found"},
		{httpx.ErrInvalid, http.StatusBadRequest, "invalid request"},
		{httpx.ErrUnauthorized, http.StatusUnauthorized, "unauthorized"},
		{httpx.ErrForbidden, http.StatusForbidden, "forbidden"},
		{httpx.ErrConflict, http.StatusConflict, "conflict"},
	} {
		t.Run(tc.body, func(t *testing.T) {
			rr := failWith(tc.err)
			if rr.Code != tc.want {
				t.Errorf("status = %d, want %d", rr.Code, tc.want)
			}
			var b httpx.ErrorBody
			if err := json.Unmarshal(rr.Body.Bytes(), &b); err != nil {
				t.Fatalf("body is not JSON: %v", err)
			}
			if b.Error != tc.body {
				t.Errorf("error = %q, want %q", b.Error, tc.body)
			}
		})
	}
}

// A wrapped sentinel must still map, or every call site has to return bare sentinels
// and the log loses all of its context.
func TestWrappedSentinelStillMaps(t *testing.T) {
	rr := failWith(fmt.Errorf("loading item 4648: %w", httpx.ErrNotFound))
	if rr.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404 through the wrap", rr.Code)
	}
}

// The point of the mapper: what we log is detailed, what we SEND is not. A wrapped
// error routinely carries a table name, a column or a query fragment.
func TestInternalDetailNeverReachesTheClient(t *testing.T) {
	rr := failWith(fmt.Errorf(`pq: relation "items_private" does not exist`))

	if rr.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500 for an unmapped error", rr.Code)
	}
	var b httpx.ErrorBody
	if err := json.Unmarshal(rr.Body.Bytes(), &b); err != nil {
		t.Fatalf("body is not JSON: %v", err)
	}
	if b.Error != "internal error" {
		t.Errorf("error = %q, want the flat %q", b.Error, "internal error")
	}
	if got := rr.Body.String(); strings.Contains(got, "items_private") || strings.Contains(got, "pq:") {
		t.Errorf("the response leaked internals: %s", got)
	}
}

// A wrapped 4xx must not leak its context either: the sentinel's text is what the
// client reads, not the wrapper's.
func TestWrappedClientErrorSendsOnlyTheSentinelText(t *testing.T) {
	rr := failWith(fmt.Errorf(`scanning row from items_private: %w`, httpx.ErrInvalid))
	if got := rr.Body.String(); strings.Contains(got, "items_private") {
		t.Errorf("a 4xx leaked its wrapper: %s", got)
	}
}

// Recover must turn a panic into a 500 through the mapper — never a stack trace in
// the body, which is chi's default behaviour.
func TestPanicBecomes500WithNoStackTrace(t *testing.T) {
	r := chi.NewRouter()
	r.Use(httpx.RequestID)
	r.Use(httpx.Recover)
	r.Get("/boom", func(http.ResponseWriter, *http.Request) {
		panic("secret in the panic value: items_private")
	})

	rr := httptest.NewRecorder()
	r.ServeHTTP(rr, httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/boom", nil))

	if rr.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", rr.Code)
	}
	body := rr.Body.String()
	if strings.Contains(body, "items_private") || strings.Contains(body, "goroutine") || strings.Contains(body, ".go:") {
		t.Errorf("panic detail reached the client: %s", body)
	}
	var b httpx.ErrorBody
	if err := json.Unmarshal([]byte(body), &b); err != nil {
		t.Fatalf("panic response is not JSON: %v (%s)", err, body)
	}
	if b.RequestID == "" {
		t.Error("a 500 from a panic carries no request_id — nothing to correlate the log with")
	}
}

func failWith(err error) *httptest.ResponseRecorder {
	r := chi.NewRouter()
	r.Use(httpx.RequestID)
	r.Get("/x", func(w http.ResponseWriter, req *http.Request) { httpx.Fail(w, req, err) })
	rr := httptest.NewRecorder()
	r.ServeHTTP(rr, httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/x", nil))
	return rr
}
