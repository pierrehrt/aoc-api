package pages_test

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/pierrehrt/aoc-api/internal/assets"
	"github.com/pierrehrt/aoc-api/internal/builds"
	"github.com/pierrehrt/aoc-api/internal/db/sqlcgen"
	"github.com/pierrehrt/aoc-api/internal/httpx"
	"github.com/pierrehrt/aoc-api/internal/items"
	"github.com/pierrehrt/aoc-api/internal/pages"
	"github.com/pierrehrt/aoc-api/internal/templates"
)

// refusingItems is the fake corpus whose list refuses every request as invalid: the service saying
// no to something the parser let through.
type refusingItems struct{ *fakeItems }

func (refusingItems) ListItems(context.Context, sqlcgen.ListItemsParams) ([]sqlcgen.ListItemsRow, error) {
	return nil, fmt.Errorf("%w: test-refusal", httpx.ErrInvalid)
}

// AOC-050, verify round 3. Round 2's F10 was fixed twice: the parser now refuses `get` without a
// `source` itself, and "any ErrInvalid from the service is now a 400 on the page, not a 500". The
// first half makes TestAHalfWithNoSourceIsA400OnTheArmoryToo pass without the second, so nothing
// held the page's own branch: removing it failed no test. Here the service refuses a request the
// parser accepted; the page must answer 400, as plain HTML or as the armory_invalid fragment to
// htmx with no URL pushed, never a 500 and an ERROR line.
func TestAServiceRefusalIsA400OnTheArmory(t *testing.T) {
	set, err := assets.Load()
	if err != nil {
		t.Fatalf("assets.Load: %v", err)
	}
	tpl, err := templates.New(set)
	if err != nil {
		t.Fatalf("templates.New: %v", err)
	}
	q := refusingItems{newFakeItems(3)}
	site := pages.New(tpl, set, base, items.NewService(q), builds.NewService(q))
	h := httpx.NewRouterWithSite(httpx.Build{Version: "1.2.3", Commit: "abc1234", Env: "test"}, site.Routes, set.Handler())
	for _, hdr := range []map[string]string{nil, {"HX-Request": "true"}} {
		rr := get(t, h, http.MethodGet, "/armory?q=test", hdr, "")
		if rr.Code != http.StatusBadRequest {
			t.Errorf("HX-Request %v: status %d, want 400", hdr != nil, rr.Code)
			continue
		}
		if !strings.Contains(rr.Body.String(), "test-refusal") {
			t.Errorf("HX-Request %v: the 400 does not name the reason", hdr != nil)
		}
		if hdr != nil && rr.Header().Get("HX-Push-Url") != "false" {
			t.Errorf("HX-Request: HX-Push-Url = %q, want \"false\" (the bad URL must not be pushed)", rr.Header().Get("HX-Push-Url"))
		}
	}
}
