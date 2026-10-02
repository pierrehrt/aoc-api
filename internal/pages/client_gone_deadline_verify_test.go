package pages_test

import (
	"bytes"
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/pierrehrt/aoc-api/internal/assets"
	"github.com/pierrehrt/aoc-api/internal/httpx"
	"github.com/pierrehrt/aoc-api/internal/items"
	"github.com/pierrehrt/aoc-api/internal/pages"
	"github.com/pierrehrt/aoc-api/internal/templates"
)

// An armory request whose own deadline ran out is the server being slow, not a client that left:
// still a 500 and an ERROR line, for the live fragment and the full page alike (AOC-065 delta
// verify 5). Only a cancellation is a 499 (TestAnAbortedArmoryRequestIsNotAPageFailure).
func TestAnArmoryRequestThatTimedOutIsStillAPageFailure(t *testing.T) {
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

	for _, htmx := range []bool{true, false} {
		ctx, cancel := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
		buf.Reset()
		rr := httptest.NewRecorder()
		r := httptest.NewRequestWithContext(ctx, http.MethodGet, "/armory?rarity=epic", nil)
		if htmx {
			r.Header.Set("HX-Request", "true")
		}
		router.ServeHTTP(rr, r)
		cancel()
		if rr.Code != http.StatusInternalServerError {
			t.Errorf("HX-Request %v: status %d, want 500", htmx, rr.Code)
		}
		if !strings.Contains(buf.String(), `"level":"ERROR"`) || strings.Contains(buf.String(), "client closed request") {
			t.Errorf("HX-Request %v: a timeout logged %s", htmx, buf.String())
		}
	}
}
