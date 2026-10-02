//go:build corpus

// A corpus test: compiled only with `-tags corpus` (AOC-044) — see item_corpus_test.go.

package pages_test

import (
	"context"
	"html"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

// AOC-068: the sources panel on the real corpus, through the page, with JavaScript off — every
// branch is a plain link, and what it promises is what it lists.

var (
	treeLink   = regexp.MustCompile(`<li><a href="([^"]+)"([^>]*)>\s*<span aria-hidden="true"[^>]*>[^<]*</span>\s*<span class="min-w-0 flex-1 truncate">([^<]*)</span>\s*<span[^>]*>([0-9,]+)</span>`)
	listCount  = regexp.MustCompile(`<span class="text-\[13px\] font-semibold text-paper">([0-9,]+) items?</span>`)
	currentTab = regexp.MustCompile(`aria-current="true" class="flex h-8[^"]*">([^<]+)</a>`)
)

func getPage(t *testing.T, h http.Handler, target string) (int, string) {
	t.Helper()
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequestWithContext(context.Background(), http.MethodGet, target, nil))
	b, _ := io.ReadAll(rec.Result().Body)
	return rec.Code, string(b)
}

func count(t *testing.T, s string) int64 {
	t.Helper()
	n, err := strconv.ParseInt(strings.ReplaceAll(s, ",", ""), 10, 64)
	if err != nil {
		t.Fatalf("not a count: %q", s)
	}
	return n
}

// Arrival: the first tab active, its tree shown, and the whole armory listed (Pierre, 2026-10-02:
// the tab changes the panel, not the list).
func TestTheArmoryArrivesOnTheFirstTabWithEveryItem(t *testing.T) {
	h, pool := corpusRouter(t)
	var first string
	var items int64
	if err := pool.QueryRow(context.Background(), `SELECT name FROM source_tabs ORDER BY sort_order LIMIT 1`).Scan(&first); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(context.Background(), `SELECT count(*) FROM items`).Scan(&items); err != nil {
		t.Fatal(err)
	}
	code, body := getPage(t, h, "/armory")
	if code != 200 {
		t.Fatalf("/armory = %d", code)
	}
	if m := currentTab.FindStringSubmatch(body); m == nil || html.UnescapeString(m[1]) != first {
		t.Errorf("the active tab is %v, want %q", m, first)
	}
	if m := listCount.FindStringSubmatch(body); m == nil || count(t, m[1]) != items {
		t.Errorf("arrival lists %v, want all %d items", m, items)
	}
	if len(treeLink.FindAllStringSubmatch(body, -1)) == 0 {
		t.Error("arrival shows no tree")
	}
}

// ⭐ Every branch the panel draws, opened all the way down, lists what its row says — for the PvE and
// Region tabs, and once under a filter. Followed as a script-less reader would: by its href.
func TestEveryBranchOfThePanelListsWhatItsRowSays(t *testing.T) {
	h, _ := corpusRouter(t)
	for _, start := range []string{"/armory", "/armory?tab=region", "/armory?tab=region&rarity=epic"} {
		seen := map[string]bool{}
		queue := []string{start}
		checked := 0
		for len(queue) > 0 && checked < 400 {
			page := queue[0]
			queue = queue[1:]
			code, body := getPage(t, h, page)
			if code != 200 {
				t.Fatalf("%s = %d", page, code)
			}
			for _, m := range treeLink.FindAllStringSubmatch(body, -1) {
				href := html.UnescapeString(m[1])
				// The picked row's link closes it (to the branch above): not what its count promises.
				if seen[href] || strings.Contains(m[2], `aria-current="true"`) {
					continue
				}
				seen[href] = true
				want := count(t, m[4])
				code, got := getPage(t, h, href)
				if code != 200 {
					t.Errorf("%s (from %s) = %d", href, page, code)
					continue
				}
				checked++
				c := listCount.FindStringSubmatch(got)
				if c == nil {
					// A branch at 0 lists nothing and draws the empty state.
					if want != 0 {
						t.Errorf("%s: no count on the page; its row said %d", href, want)
					}
					continue
				}
				if n := count(t, c[1]); n != want {
					t.Errorf("%q (%s): its row says %d, the page lists %d", html.UnescapeString(m[3]), href, want, n)
				}
				// Picking opens the branch: its children appear on its own page.
				queue = append(queue, href)
			}
		}
		if checked == 0 {
			t.Errorf("%s: no branch followed", start)
		}
		t.Logf("%s: %d branches followed", start, checked)
	}
}

// The template names no tab, section, half or place: every one comes from the database.
func TestThePanelsTemplatesNameNothingFromTheData(t *testing.T) {
	_, pool := corpusRouter(t)
	rows, err := pool.Query(context.Background(), `
		SELECT name FROM source_tabs UNION SELECT name FROM sections UNION SELECT name FROM acquisition_groups`)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for rows.Next() {
		var n string
		if err := rows.Scan(&n); err != nil {
			t.Fatal(err)
		}
		names = append(names, n)
	}
	files, _ := filepath.Glob("../templates/html/armory*.html")
	if len(files) == 0 {
		t.Fatal("no armory templates found")
	}
	for _, f := range files {
		b, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		for _, n := range names {
			if strings.Contains(string(b), n) || strings.Contains(string(b), html.EscapeString(n)) {
				t.Errorf("%s names %q; it must come from the database", filepath.Base(f), n)
			}
		}
	}
}
