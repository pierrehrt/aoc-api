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

// The router's deadline must not turn a reader who left into a server failure, nor the reverse
// (AOC-065 delta verify 8). The deadline is a child of the request's context, so a reader who
// leaves before it cancels the child with context.Canceled: still a 499 at Info. A reader who leaves
// after it finds the deadline already recorded: the server was slow, a 500 and an ERROR. Measured on
// the live branch behind a stalled Postgres: aborts at 9.5 s were 499, every abort from 10.000 s on
// was a 500 — the first cause to arrive decides.
func TestTheDeadlineAndAReaderWhoLeftAreToldApart(t *testing.T) {
	var buf bytes.Buffer
	restore := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&buf, nil)))
	defer slog.SetDefault(restore)

	hang := func(r chi.Router) {
		r.Get("/hang", func(w http.ResponseWriter, req *http.Request) {
			select {
			case <-req.Context().Done(): // the query that never answers, until its context ends
				httpx.Fail(w, req, fmt.Errorf("list items: %w", req.Context().Err()))
			case <-time.After(2 * time.Second): // the test's own bound: a mutant fails, it does not hang
				w.WriteHeader(http.StatusOK)
			}
		})
	}
	// leaveAfter: when the reader goes away; the deadline is 150 ms.
	run := func(leaveAfter time.Duration) int {
		router := httpx.NewRouterWithAPI(httpx.Build{Version: "t"}, nil, nil, hang, httpx.WithRequestDeadline(150*time.Millisecond))
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		time.AfterFunc(leaveAfter, cancel)
		rr := httptest.NewRecorder()
		router.ServeHTTP(rr, httptest.NewRequestWithContext(ctx, http.MethodGet, "/v1/hang", nil))
		return rr.Code
	}

	buf.Reset()
	if got := run(30 * time.Millisecond); got != httpx.StatusClientClosedRequest {
		t.Errorf("a reader who left before the deadline: status %d, want 499", got)
	}
	if strings.Contains(buf.String(), `"level":"ERROR"`) || !strings.Contains(buf.String(), "client closed request") {
		t.Errorf("a reader who left before the deadline must be an Info 499, logged %s", buf.String())
	}

	buf.Reset()
	if got := run(time.Second); got != http.StatusInternalServerError {
		t.Errorf("a request still hanging at the deadline: status %d, want 500", got)
	}
	if !strings.Contains(buf.String(), `"level":"ERROR"`) || strings.Contains(buf.String(), "client closed request") {
		t.Errorf("a request still hanging at the deadline must be an ERROR, logged %s", buf.String())
	}
}
