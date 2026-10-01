package httpx_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"syscall"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/pierrehrt/aoc-api/internal/httpx"
)

// A client that went away is not a server failure (AOC-065 delta verify 4, N6: the armory aborts a
// superseded live request, and its query failed with "context canceled" — logged as ERROR and a 500,
// 52 of them in one session, none a reader saw). 499 and an Info line; anything else is still a 500.
func TestAClientThatWentAwayIsNotAServerFailure(t *testing.T) {
	var buf bytes.Buffer
	restore := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&buf, nil)))
	defer slog.SetDefault(restore)

	run := func(cancelRequest bool, err error) int {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		if cancelRequest {
			cancel()
		}
		r := chi.NewRouter()
		r.Get("/x", func(w http.ResponseWriter, req *http.Request) { httpx.Fail(w, req, err) })
		rr := httptest.NewRecorder()
		r.ServeHTTP(rr, httptest.NewRequestWithContext(ctx, http.MethodGet, "/x", nil))
		return rr.Code
	}

	buf.Reset()
	if got := run(true, fmt.Errorf("list items: %w", context.Canceled)); got != httpx.StatusClientClosedRequest {
		t.Errorf("the client went away: status %d, want 499", got)
	}
	if strings.Contains(buf.String(), `"level":"ERROR"`) || !strings.Contains(buf.String(), "client closed request") {
		t.Errorf("the client went away: logged %s", buf.String())
	}
	// The peer reset or closed the connection while the answer was being written: gone too, the
	// request's context alive or not (it is cancelled a moment later).
	for _, errno := range []error{syscall.ECONNRESET, syscall.EPIPE} {
		buf.Reset()
		werr := &net.OpError{Op: "write", Net: "tcp", Err: os.NewSyscallError("write", errno)}
		if got := run(false, fmt.Errorf("templates: %w", werr)); got != httpx.StatusClientClosedRequest {
			t.Errorf("%v while writing: status %d, want 499", errno, got)
		}
		if strings.Contains(buf.String(), `"level":"ERROR"`) {
			t.Errorf("%v while writing: logged %s", errno, buf.String())
		}
	}
	// Not the client: a cancellation from elsewhere while the request is alive, a timeout (the
	// server's, writing too), or any other error with the request cancelled — each is still a 500.
	for name, c := range map[string]struct {
		cancelRequest bool
		err           error
	}{
		"a canceled error, the request alive": {false, context.Canceled},
		"a deadline":                          {true, fmt.Errorf("q: %w", context.DeadlineExceeded)},
		"a write timeout":                     {false, &net.OpError{Op: "write", Net: "tcp", Err: os.ErrDeadlineExceeded}},
		"another error":                       {true, errors.New("boom")},
	} {
		if got := run(c.cancelRequest, c.err); got != http.StatusInternalServerError {
			t.Errorf("%s: status %d, want 500", name, got)
		}
	}
}
