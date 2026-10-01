package pages_test

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/pierrehrt/aoc-api/internal/assets"
	"github.com/pierrehrt/aoc-api/internal/db/sqlcgen"
	"github.com/pierrehrt/aoc-api/internal/httpx"
	"github.com/pierrehrt/aoc-api/internal/items"
	"github.com/pierrehrt/aoc-api/internal/pages"
	"github.com/pierrehrt/aoc-api/internal/templates"
)

// ctxAware fails as the real query does once its request's context is done.
type ctxAware struct{ *fakeItems }

func (c ctxAware) ListItems(ctx context.Context, a sqlcgen.ListItemsParams) ([]sqlcgen.ListItemsRow, error) {
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("list items: %w", err)
	}
	return c.fakeItems.ListItems(ctx, a)
}

// A live armory request the page aborted (a newer one superseded it, hx-sync replace) is a client
// that went away, not a page that failed to render: 499 and no ERROR line (AOC-065 delta verify 4, N6).
func TestAnAbortedArmoryRequestIsNotAPageFailure(t *testing.T) {
	set, err := assets.Load()
	if err != nil {
		t.Fatal(err)
	}
	tpl, err := templates.New(set)
	if err != nil {
		t.Fatal(err)
	}
	h := pages.New(tpl, set, base, items.NewService(ctxAware{newFakeItems(10)}))
	router := httpx.NewRouterWithSite(httpx.Build{Version: "1.2.3", Commit: "abc1234", Env: "test"}, h.Routes, set.Handler())

	var buf bytes.Buffer
	restore := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&buf, nil)))
	defer slog.SetDefault(restore)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	rr := httptest.NewRecorder()
	r := httptest.NewRequestWithContext(ctx, http.MethodGet, "/armory?rarity=epic", nil)
	r.Header.Set("HX-Request", "true")
	router.ServeHTTP(rr, r)
	if rr.Code != httpx.StatusClientClosedRequest {
		t.Errorf("status %d, want 499", rr.Code)
	}
	if strings.Contains(buf.String(), `"level":"ERROR"`) {
		t.Errorf("an aborted request logged an error: %s", buf.String())
	}
}
