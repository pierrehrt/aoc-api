package pages_test

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"syscall"
	"testing"

	"github.com/pierrehrt/aoc-api/internal/assets"
	"github.com/pierrehrt/aoc-api/internal/builds"
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
	q := ctxAware{newFakeItems(10)}
	h := pages.New(tpl, set, base, items.NewService(q), builds.NewService(q))
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

// resetWriter is a connection the reader dropped while the page was being written. As net/http's
// does, the failed write cancels the request's context.
type resetWriter struct {
	*httptest.ResponseRecorder
	cancel context.CancelFunc
}

func (w resetWriter) Write([]byte) (int, error) {
	w.cancel()
	return 0, &net.OpError{Op: "write", Net: "tcp", Err: os.NewSyscallError("write", syscall.ECONNRESET)}
}

// The page rendered, and the reader left while it was being written: not a render failure either
// (delta verify 5: one such write in 600 aborted requests was logged as an ERROR).
func TestAPageWhoseReaderLeftMidWriteIsNotAFailure(t *testing.T) {
	var buf bytes.Buffer
	restore := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&buf, nil)))
	defer slog.SetDefault(restore)
	h := router(t)
	for _, hdr := range []map[string]string{nil, {"HX-Request": "true"}} {
		buf.Reset()
		ctx, cancel := context.WithCancel(context.Background())
		r := httptest.NewRequestWithContext(ctx, http.MethodGet, "/armory?rarity=epic", nil)
		for k, v := range hdr {
			r.Header.Set(k, v)
		}
		h.ServeHTTP(resetWriter{httptest.NewRecorder(), cancel}, r)
		cancel()
		if strings.Contains(buf.String(), `"level":"ERROR"`) {
			t.Errorf("HX %v: a reader who left mid-write logged an error: %s", hdr != nil, buf.String())
		}
	}
}
