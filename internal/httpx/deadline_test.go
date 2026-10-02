package httpx_test

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/pierrehrt/aoc-api/internal/httpx"
)

// A request whose work hangs — a database that stops answering without resetting — ends at the
// router's deadline as a 500 with an ERROR line, not as a "client closed request" when the reader
// or Cloudflare gives up (AOC-065 delta verify 7, N11: the hang never reached the error log).
func TestAHungRequestEndsAtTheDeadlineAsAnError(t *testing.T) {
	var buf bytes.Buffer
	restore := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&buf, nil)))
	defer slog.SetDefault(restore)

	hang := func(r chi.Router) {
		r.Get("/hang", func(w http.ResponseWriter, req *http.Request) {
			select {
			case <-req.Context().Done(): // the query that never answers, until its context ends
				httpx.Fail(w, req, fmt.Errorf("list items: %w", req.Context().Err()))
			case <-time.After(time.Second): // no deadline: the test's own bound, so a mutant fails, not hangs
				w.WriteHeader(http.StatusOK)
			}
		})
	}
	router := httpx.NewRouterWithAPI(httpx.Build{Version: "t"}, nil, nil, hang, httpx.WithRequestDeadline(50*time.Millisecond))
	rr := httptest.NewRecorder()
	start := time.Now()
	router.ServeHTTP(rr, httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/v1/hang", nil))
	if took := time.Since(start); took > 2*time.Second {
		t.Fatalf("the request was not bounded: %v", took)
	}
	if rr.Code != http.StatusInternalServerError {
		t.Errorf("status %d, want 500", rr.Code)
	}
	if !strings.Contains(buf.String(), `"level":"ERROR"`) || strings.Contains(buf.String(), "client closed request") {
		t.Errorf("a hang must be an ERROR, not a reader gone: %s", buf.String())
	}
	if httpx.RequestDeadline <= 0 || httpx.RequestDeadline >= 100*time.Second {
		t.Errorf("RequestDeadline %v must be set and inside Cloudflare's 100 s", httpx.RequestDeadline)
	}
}
