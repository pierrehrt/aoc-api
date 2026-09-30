//go:build corpus

// A corpus test: compiled only with `-tags corpus` (AOC-044) — it reads the real imported armory
// from the local database, like internal/db's corpus tests.

package pages_test

import (
	"context"
	"fmt"
	"html"
	"net/http"
	"os"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/pierrehrt/aoc-api/internal/assets"
	"github.com/pierrehrt/aoc-api/internal/db"
	"github.com/pierrehrt/aoc-api/internal/db/sqlcgen"
	"github.com/pierrehrt/aoc-api/internal/httpx"
	"github.com/pierrehrt/aoc-api/internal/items"
	"github.com/pierrehrt/aoc-api/internal/pages"
	"github.com/pierrehrt/aoc-api/internal/templates"
)

func corpusRouter(t *testing.T) (http.Handler, *pgxpool.Pool) {
	t.Helper()
	url := os.Getenv("TEST_DATABASE_URL")
	if url == "" {
		url = os.Getenv("DATABASE_URL")
	}
	if url == "" {
		t.Skip("no TEST_DATABASE_URL or DATABASE_URL — run `make db-up`, migrate and import")
	}
	if h := db.HostOf(url); !db.IsLocalHost(h) {
		t.Fatalf("%q is not local; corpus tests never run remotely", h)
	}
	pool, err := pgxpool.New(context.Background(), url)
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
	h := pages.New(tpl, set, base, items.NewService(sqlcgen.New(pool)))
	return httpx.NewRouterWithSite(httpx.Build{Version: "test", Commit: "test", Env: "test"}, h.Routes, set.Handler()), pool
}

// The ticket's own example, which is a measurement of the corpus (2026-09-29): 3 stat lines, 1 source.
func TestPilgrimsHelmetRendersItsThreeStatsAndOneSource(t *testing.T) {
	h, _ := corpusRouter(t)
	rr := get(t, h, http.MethodGet, "/armory/pilgrim-s-helmet", nil, "")
	if rr.Code != http.StatusOK {
		t.Fatalf("status %d", rr.Code)
	}
	body := html.UnescapeString(rr.Body.String())
	stats := body[strings.Index(body, `id="stats-h"`):strings.Index(body, `id="src-h"`)]
	if n := strings.Count(stats, "<li>+") + strings.Count(stats, "<li>-"); n != 3 {
		t.Errorf("%d stat lines rendered, want 3", n)
	}
	if !strings.Contains(body, "Sources <span class=\"font-mono\">· 1</span>") {
		t.Error("the source count is not 1")
	}
}

// The edge cases, each found by QUERY so they survive a re-import: the item with the most sources,
// a vendor with no place, an item in no set — every one renders, and shows as many rows as it has.
func TestTheCorpusEdgeCasesRender(t *testing.T) {
	h, pool := corpusRouter(t)
	ctx := context.Background()
	for what, q := range map[string]string{
		"the most sources": `SELECT i.slug, count(*) FROM items i JOIN item_sources s ON s.item_id = i.item_id
		                     GROUP BY i.slug ORDER BY count(*) DESC, i.slug LIMIT 1`,
		"a vendor with no place": `SELECT i.slug, (SELECT count(*) FROM item_sources x WHERE x.item_id = i.item_id)
		                           FROM items i JOIN item_sources s ON s.item_id = i.item_id
		                           WHERE s.vendor_id IS NOT NULL AND s.place_id IS NULL ORDER BY i.slug LIMIT 1`,
		"no set and no source": `SELECT i.slug, 0 FROM items i WHERE i.set_id IS NULL
		                         AND NOT EXISTS (SELECT 1 FROM item_sources s WHERE s.item_id = i.item_id) ORDER BY i.slug LIMIT 1`,
	} {
		var slug string
		var n int
		if err := pool.QueryRow(ctx, q).Scan(&slug, &n); err != nil {
			t.Errorf("%s: no such item in this corpus: %v", what, err)
			continue
		}
		rr := get(t, h, http.MethodGet, "/armory/"+slug, nil, "")
		if rr.Code != http.StatusOK {
			t.Errorf("%s (%s): status %d", what, slug, rr.Code)
			continue
		}
		body := rr.Body.String()
		switch {
		case n == 0 && !strings.Contains(body, "No source recorded."):
			t.Errorf("%s (%s): no honest no-source state", what, slug)
		case n > 0 && !strings.Contains(body, fmt.Sprintf("Sources <span class=\"font-mono\">· %d</span>", n)):
			t.Errorf("%s (%s): the count of %d sources is not shown", what, slug, n)
		}
		if strings.Contains(body, `id="set-h"`) && what == "no set and no source" {
			t.Errorf("%s (%s): a set section for an item in no set", what, slug)
		}
	}
}

// AOC-062, on the real corpus: every page of the list, every row's Type cell is an armour weight's
// name, an item type's name, or a dash — never a slug. Walked in full, row by row (the Type cell is
// each row's second desktop cell — rowRE and cellsRE are armory_columns_test.go's).
func TestEveryTypeCellOnTheRealListIsAName(t *testing.T) {
	h, pool := corpusRouter(t)
	ctx := context.Background()
	names := map[string]bool{"—": true}
	for _, q := range []string{`SELECT name FROM item_types`, `SELECT name FROM armour_weights`} {
		rows, err := pool.Query(ctx, q)
		if err != nil {
			t.Fatal(err)
		}
		for rows.Next() {
			var n string
			if err := rows.Scan(&n); err != nil {
				t.Fatal(err)
			}
			names[n] = true
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			t.Fatal(err)
		}
	}
	seen, bad := 0, 0
	for p := 1; ; p++ {
		rr := get(t, h, http.MethodGet, fmt.Sprintf("/armory?p=%d", p), nil, "")
		if rr.Code == http.StatusNotFound {
			break
		}
		if rr.Code != http.StatusOK {
			t.Fatalf("page %d: %d", p, rr.Code)
		}
		for _, row := range rowRE.FindAllStringSubmatch(rr.Body.String(), -1) {
			cells := cellsRE.FindAllStringSubmatch(row[1], -1)
			if len(cells) < 2 {
				t.Fatalf("page %d: a row with %d desktop cells — the markup moved", p, len(cells))
			}
			seen++
			if txt := text(cells[1][1]); !names[txt] {
				bad++
				if bad <= 5 {
					t.Errorf("page %d: Type cell %q is not a type or weight name", p, txt)
				}
			}
		}
	}
	var total int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM items`).Scan(&total); err != nil {
		t.Fatal(err)
	}
	if seen != total {
		t.Fatalf("read %d rows' Type cells, the database holds %d items", seen, total)
	}
}
