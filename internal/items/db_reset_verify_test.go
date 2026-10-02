package items

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

	"github.com/go-chi/chi/v5"

	"github.com/pierrehrt/aoc-api/internal/db/sqlcgen"
	"github.com/pierrehrt/aoc-api/internal/httpx"
)

// dbResetQ is a database whose connection was reset under the query, as pgx reports it.
type dbResetQ struct {
	*fakeQ
	err error
}

func (q dbResetQ) ListItems(context.Context, sqlcgen.ListItemsParams) ([]sqlcgen.ListItemsRow, error) {
	return nil, fmt.Errorf("list items: %w", q.err)
}

// The /v1 surface through the router as main.go composes it (AOC-065 delta verify 6, N9). The
// database's connection resetting while the client is still connected is the server failing: a 500
// with its JSON body and an ERROR line — not a 499 with no body and "client closed request" at Info,
// which is what 7c0e4a4 answered for every such request measured behind a resetting proxy.
func TestAV1DatabaseConnectionResetIsStillA500(t *testing.T) {
	var buf bytes.Buffer
	restore := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&buf, nil)))
	defer slog.SetDefault(restore)

	for name, dbErr := range map[string]error{
		"read reset":  &net.OpError{Op: "read", Net: "tcp", Err: os.NewSyscallError("read", syscall.ECONNRESET)},
		"broken pipe": &net.OpError{Op: "write", Net: "tcp", Err: os.NewSyscallError("write", syscall.EPIPE)},
	} {
		h := NewHandler(NewService(dbResetQ{&fakeQ{}, dbErr}), fakeTax{})
		router := httpx.NewRouterWithAPI(httpx.Build{Version: "t", Commit: "t", Env: "test"}, nil, nil, func(v1 chi.Router) {
			v1.Mount("/items", h.Routes())
		})
		buf.Reset()
		rr := httptest.NewRecorder()
		router.ServeHTTP(rr, httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/v1/items?rarity=epic", nil))
		if rr.Code != http.StatusInternalServerError {
			t.Errorf("%s: status %d, want 500", name, rr.Code)
		}
		if !strings.Contains(rr.Body.String(), "internal error") {
			t.Errorf("%s: body %q, want the flat internal error", name, rr.Body.String())
		}
		if !strings.Contains(buf.String(), `"level":"ERROR"`) || strings.Contains(buf.String(), "client closed request") {
			t.Errorf("%s: logged %s", name, buf.String())
		}
	}
}
