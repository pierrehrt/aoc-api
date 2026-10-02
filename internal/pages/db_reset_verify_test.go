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
	"github.com/pierrehrt/aoc-api/internal/db/sqlcgen"
	"github.com/pierrehrt/aoc-api/internal/httpx"
	"github.com/pierrehrt/aoc-api/internal/items"
	"github.com/pierrehrt/aoc-api/internal/pages"
	"github.com/pierrehrt/aoc-api/internal/templates"
)

// dbResetItems is a database whose connection was reset under the query: the error pgx returns
// when Postgres restarts or the network drops it ("read tcp …: read: connection reset by peer"),
// or when the query is written to a connection that is already gone ("write: broken pipe").
type dbResetItems struct {
	*fakeItems
	err error
}

func (d dbResetItems) ListItems(context.Context, sqlcgen.ListItemsParams) ([]sqlcgen.ListItemsRow, error) {
	return nil, fmt.Errorf("list items: %w", d.err)
}

// The database's connection resetting is the server failing, not the reader leaving: a 500 and an
// ERROR line, for the live fragment and the full page alike, while the reader is still connected
// (AOC-065 delta verify 6, N9: measured behind a proxy that resets the app's Postgres connection,
// 40 of 40 such requests were answered 499 with an empty body and logged "client closed request"
// at Info; c427071 answered 500 and logged ERROR). Only the reader's own connection counts as gone.
func TestADatabaseConnectionResetIsStillAPageFailure(t *testing.T) {
	set, err := assets.Load()
	if err != nil {
		t.Fatal(err)
	}
	tpl, err := templates.New(set)
	if err != nil {
		t.Fatal(err)
	}

	var buf bytes.Buffer
	restore := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&buf, nil)))
	defer slog.SetDefault(restore)

	for name, dbErr := range map[string]error{
		"read reset":  &net.OpError{Op: "read", Net: "tcp", Err: os.NewSyscallError("read", syscall.ECONNRESET)},
		"broken pipe": &net.OpError{Op: "write", Net: "tcp", Err: os.NewSyscallError("write", syscall.EPIPE)},
	} {
		h := pages.New(tpl, set, base, items.NewService(dbResetItems{newFakeItems(10), dbErr}))
		router := httpx.NewRouterWithSite(httpx.Build{Version: "1.2.3", Commit: "abc1234", Env: "test"}, h.Routes, set.Handler())
		for _, htmx := range []bool{true, false} {
			buf.Reset()
			rr := httptest.NewRecorder()
			r := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/armory?rarity=epic", nil)
			if htmx {
				r.Header.Set("HX-Request", "true")
			}
			router.ServeHTTP(rr, r)
			if rr.Code != http.StatusInternalServerError {
				t.Errorf("%s, HX-Request %v: status %d, want 500", name, htmx, rr.Code)
			}
			if !strings.Contains(buf.String(), `"level":"ERROR"`) || strings.Contains(buf.String(), "client closed request") {
				t.Errorf("%s, HX-Request %v: logged %s", name, htmx, buf.String())
			}
		}
	}
}
