//go:build corpus

// A corpus test: compiled only with `-tags corpus` (AOC-044) — see item_corpus_test.go.

package pages_test

import (
	"context"
	"encoding/json"
	"html"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"regexp"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/pierrehrt/aoc-api/internal/assets"
	"github.com/pierrehrt/aoc-api/internal/db"
	"github.com/pierrehrt/aoc-api/internal/db/sqlcgen"
	"github.com/pierrehrt/aoc-api/internal/httpx"
	"github.com/pierrehrt/aoc-api/internal/items"
	"github.com/pierrehrt/aoc-api/internal/pages"
	"github.com/pierrehrt/aoc-api/internal/templates"
)

// AOC-068 verify round 1. The acceptance criterion is "picking a node lists exactly what
// /v1/items?source=<node> lists". The panel's own corpus test compares a row's count with the page's
// total, and both come from the page. Here every branch link the panel prints (JavaScript off, by its
// href) is compared with the PUBLIC JSON route for the same query: the same total, and the same
// items in the same order on the first page.

var v1ItemLink = regexp.MustCompile(`<a href="/armory/([^"?#/]+)"`)

// siteAndAPI is the application as cmd/api builds it: the site and /v1/items on one router.
func siteAndAPI(t *testing.T) http.Handler {
	t.Helper()
	u := os.Getenv("TEST_DATABASE_URL")
	if u == "" {
		u = os.Getenv("DATABASE_URL")
	}
	if u == "" {
		t.Skip("no TEST_DATABASE_URL or DATABASE_URL — run `make db-up`, migrate and import")
	}
	if h := db.HostOf(u); !db.IsLocalHost(h) {
		t.Fatalf("%q is not local; corpus tests never run remotely", h)
	}
	pool, err := pgxpool.New(context.Background(), u)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	var n int
	if err := pool.QueryRow(context.Background(), `SELECT count(*) FROM items`).Scan(&n); err != nil || n == 0 {
		t.Skipf("no imported items (%v) — run cmd/import-armory against the dev database", err)
	}
	set, err := assets.Load()
	if err != nil {
		t.Fatal(err)
	}
	tpl, err := templates.New(set)
	if err != nil {
		t.Fatal(err)
	}
	q := sqlcgen.New(pool)
	svc := items.NewService(q)
	api := items.NewHandler(svc, items.NewTaxonomyService(q))
	site := pages.New(tpl, set, base, svc)
	return httpx.NewRouterWithAPI(httpx.Build{Version: "test", Commit: "test", Env: "test"}, site.Routes, set.Handler(),
		func(v1 chi.Router) { v1.Mount("/items", api.Routes()) })
}

func fetch(t *testing.T, h http.Handler, target string) (int, string) {
	t.Helper()
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequestWithContext(context.Background(), http.MethodGet, target, nil))
	b, _ := io.ReadAll(rec.Result().Body)
	return rec.Code, string(b)
}

func TestEveryBranchListsExactlyWhatV1ItemsListsForItsSource(t *testing.T) {
	h := siteAndAPI(t)
	for _, start := range []string{"/armory", "/armory?tab=region"} {
		code, body := fetch(t, h, start)
		if code != http.StatusOK {
			t.Fatalf("%s = %d", start, code)
		}
		seen := map[string]bool{}
		bodies := []string{body}
		checked := 0
		for len(bodies) > 0 {
			body := bodies[0]
			bodies = bodies[1:]
			for _, m := range treeLink.FindAllStringSubmatch(body, -1) {
				href := html.UnescapeString(m[1])
				if seen[href] || strings.Contains(m[2], `aria-current="true"`) {
					continue
				}
				seen[href] = true
				code, got := fetch(t, h, href)
				if code != http.StatusOK {
					t.Errorf("%s = %d", href, code)
					continue
				}
				bodies = append(bodies, got) // picking a branch opens it: its children are on its own page
				var pageTotal int64
				if c := listCount.FindStringSubmatch(got); c != nil {
					pageTotal = count(t, c[1])
				}
				var pageItems []string
				for _, s := range v1ItemLink.FindAllStringSubmatch(got, -1) {
					pageItems = append(pageItems, s[1])
				}

				u, _ := url.Parse(href)
				v := u.Query()
				v.Set("sort", "ilvl") // the page's default order
				v.Set("limit", "50")  // the page's page
				code, raw := fetch(t, h, "/v1/items?"+v.Encode())
				if code != http.StatusOK {
					t.Errorf("/v1/items?%s = %d: %s", v.Encode(), code, raw)
					continue
				}
				var list struct {
					Total int64 `json:"total"`
					Items []struct {
						Slug string `json:"slug"`
					} `json:"items"`
				}
				if err := json.Unmarshal([]byte(raw), &list); err != nil {
					t.Fatal(err)
				}
				var apiItems []string
				for _, it := range list.Items {
					apiItems = append(apiItems, it.Slug)
				}
				checked++
				if pageTotal != list.Total {
					t.Errorf("%q (%s): the page lists %d, /v1/items %d", html.UnescapeString(m[3]), href, pageTotal, list.Total)
				}
				if strings.Join(pageItems, " ") != strings.Join(apiItems, " ") {
					t.Errorf("%q (%s): the page's first page is not /v1/items' (%d vs %d items)", html.UnescapeString(m[3]), href, len(pageItems), len(apiItems))
				}
			}
		}
		if checked == 0 {
			t.Errorf("%s: no branch followed", start)
		}
		t.Logf("%s: %d branches against /v1/items", start, checked)
	}
}
