package items

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

	"github.com/pierrehrt/aoc-api/internal/db/sqlcgen"
	"github.com/pierrehrt/aoc-api/internal/httpx"
)

// ctxQ fails as the real query does once its request's context is done: canceled when the client
// went away, deadline exceeded when a timeout ran out.
type ctxQ struct{ *fakeQ }

func (q ctxQ) ListItems(ctx context.Context, a sqlcgen.ListItemsParams) ([]sqlcgen.ListItemsRow, error) {
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("list items: %w", err)
	}
	return q.fakeQ.ListItems(ctx, a)
}

// The /v1 surface through the router as main.go composes it (AOC-065 delta verify 5). A /v1 request
// whose client went away mid-query is a 499 at Info, like the armory's; a request whose own deadline
// ran out is still the server's failure: a 500 and an ERROR line. The second case is the guard for
// the day a timeout middleware arrives — its context ends with a deadline, never a cancellation.
func TestAV1RequestWhoseClientWentIsA499AndATimeoutStaysA500(t *testing.T) {
	h := NewHandler(NewService(ctxQ{&fakeQ{}}), fakeTax{})
	router := httpx.NewRouterWithAPI(httpx.Build{Version: "t", Commit: "t", Env: "test"}, nil, nil, func(v1 chi.Router) {
		v1.Mount("/items", h.Routes())
	})

	var buf bytes.Buffer
	restore := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&buf, nil)))
	defer slog.SetDefault(restore)

	serve := func(ctx context.Context) int {
		rr := httptest.NewRecorder()
		router.ServeHTTP(rr, httptest.NewRequestWithContext(ctx, http.MethodGet, "/v1/items?rarity=epic", nil))
		return rr.Code
	}

	gone, cancel := context.WithCancel(context.Background())
	cancel()
	buf.Reset()
	if got := serve(gone); got != httpx.StatusClientClosedRequest {
		t.Errorf("the client went away: status %d, want 499", got)
	}
	if strings.Contains(buf.String(), `"level":"ERROR"`) || !strings.Contains(buf.String(), "client closed request") {
		t.Errorf("the client went away: logged %s", buf.String())
	}

	late, cancelLate := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
	defer cancelLate()
	buf.Reset()
	if got := serve(late); got != http.StatusInternalServerError {
		t.Errorf("the request's deadline ran out: status %d, want 500", got)
	}
	if !strings.Contains(buf.String(), `"level":"ERROR"`) || strings.Contains(buf.String(), "client closed request") {
		t.Errorf("the request's deadline ran out: logged %s", buf.String())
	}
}
