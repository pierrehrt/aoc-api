//go:build corpus

// A corpus test: compiled only with `-tags corpus` (AOC-044) — see item_corpus_test.go.

package pages_test

import (
	"context"
	"html"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strings"
	"testing"
)

// AOC-068 verify round 2. Round 1's F3: a pick by keyboard threw focus out of the tree, because the
// tabs and tree links had no ids for htmx to hand focus back to. The fix gives each one an id. That
// only works if every id is ONE element of the page (the phone sheet repeats the tabs), every tab and
// branch link has one, and the same link has the same id in every answer, full page or htmx. F2 put
// the tabs in the Sources sheet: the sheet's tabs are the search block's, link for link. F5: the
// first tab, named in the URL, is the arrival state, so its page is the same page.

var (
	anyID   = regexp.MustCompile(`\sid="([^"]*)"`)
	anchor  = regexp.MustCompile(`<a\b[^>]*>`)
	idOfA   = regexp.MustCompile(`\sid="([^"]*)"`)
	hrefOfA = regexp.MustCompile(`\shref="([^"]*)"`)
)

func hxGet(t *testing.T, h http.Handler, target string) (int, string) {
	t.Helper()
	rec := httptest.NewRecorder()
	r := httptest.NewRequestWithContext(context.Background(), http.MethodGet, target, nil)
	r.Header.Set("HX-Request", "true")
	h.ServeHTTP(rec, r)
	b, _ := io.ReadAll(rec.Result().Body)
	return rec.Code, string(b)
}

// between is body from the first `from` to the next `to` after it ("" when either is missing).
func between(body, from, to string) string {
	i := strings.Index(body, from)
	if i < 0 {
		return ""
	}
	j := strings.Index(body[i+len(from):], to)
	if j < 0 {
		return ""
	}
	return body[i : i+len(from)+j]
}

type link struct{ id, href string }

func links(part string) []link {
	var out []link
	for _, a := range anchor.FindAllString(part, -1) {
		l := link{}
		if m := idOfA.FindStringSubmatch(a); m != nil {
			l.id = m[1]
		}
		if m := hrefOfA.FindStringSubmatch(a); m != nil {
			l.href = html.UnescapeString(m[1])
		}
		out = append(out, l)
	}
	return out
}

// panelParts is the search block's tabs, the sheet's tabs and the tree, in a full page or an answer.
func panelParts(body string, htmx bool) (tabs, sheet, tree string) {
	if htmx {
		return between(body, `innerHTML:#armory-tabs"`, `innerHTML:#armory-tabs-sheet"`),
			between(body, `innerHTML:#armory-tabs-sheet"`, `innerHTML:#armory-sources-head"`),
			between(body, `innerHTML:#armory-sources-tree"`, `innerHTML:#armory-sources-selected"`)
	}
	return between(body, `id="armory-tabs"`, `</nav>`),
		between(body, `id="armory-tabs-sheet"`, `</nav>`),
		between(body, `id="armory-sources-tree"`, `id="armory-sources-selected"`)
}

// branchOf is the branch a state picks: its source and its half.
func branchOf(target string) string {
	u, _ := url.Parse(target)
	q := u.Query()
	return q.Get("source") + " | " + q.Get("get")
}

func TestEveryTabAndBranchLinkHasOneStableIDInEveryAnswer(t *testing.T) {
	h, _ := corpusRouter(t)
	code, arrival := getPage(t, h, "/armory")
	if code != http.StatusOK {
		t.Fatalf("/armory = %d", code)
	}
	tabs, _, _ := panelParts(arrival, false)
	var starts []string
	for _, l := range links(tabs) {
		starts = append(starts, l.href)
	}
	if len(starts) < 2 {
		t.Fatalf("the search block has %d tabs", len(starts))
	}

	idOfBranch := map[string]string{} // branch → the id of its link
	branchOfID := map[string]string{} // id → the branch its link names
	idOfTab := map[string]string{}    // "block|sheet" + tab href → its id
	claim := func(where, id, branch string) {
		if id == "" {
			t.Errorf("%s: a branch link (%s) has no id, so focus cannot come back to it", where, branch)
			return
		}
		if b, ok := branchOfID[id]; ok && b != branch {
			t.Errorf("%s: id %q names %q here and %q elsewhere", where, id, branch, b)
		}
		if i, ok := idOfBranch[branch]; ok && i != id {
			t.Errorf("%s: branch %q is %q here and %q elsewhere: not stable across answers", where, branch, id, i)
		}
		branchOfID[id], idOfBranch[branch] = branch, id
	}
	check := func(target, body string, htmx bool) []string {
		where := target
		if htmx {
			where += " (htmx)"
		}
		seen := map[string]bool{}
		for _, m := range anyID.FindAllStringSubmatch(body, -1) {
			if seen[m[1]] {
				t.Errorf("%s: id %q is on two elements", where, m[1])
			}
			seen[m[1]] = true
		}
		tabs, sheet, tree := panelParts(body, htmx)
		bl, sl := links(tabs), links(sheet)
		if len(bl) != len(starts) || len(sl) != len(starts) {
			t.Errorf("%s: %d tabs in the search block and %d in the sheet, want %d each", where, len(bl), len(sl), len(starts))
		}
		for i := range bl {
			if i < len(sl) && sl[i].href != bl[i].href {
				t.Errorf("%s: the sheet's tab %d leads to %q, the search block's to %q", where, i, sl[i].href, bl[i].href)
			}
			for _, x := range []struct {
				kind string
				l    link
			}{{"block", bl[i]}, {"sheet", func() link {
				if i < len(sl) {
					return sl[i]
				}
				return link{}
			}()}} {
				if x.l.id == "" {
					t.Errorf("%s: a %s tab (%s) has no id", where, x.kind, x.l.href)
					continue
				}
				// A tab's link drops the pick, so its href names the tab whatever the state: one id each.
				key := x.kind + "|" + x.l.href
				if u, _ := url.Parse(x.l.href); u != nil {
					q := u.Query()
					key = x.kind + "|" + q.Get("tab")
				}
				if old, ok := idOfTab[key]; ok && old != x.l.id {
					t.Errorf("%s: the %s tab %q is %q here and %q elsewhere", where, x.kind, key, x.l.id, old)
				}
				idOfTab[key] = x.l.id
			}
		}
		var next []string
		rows := links(tree)
		if len(rows) == 0 {
			t.Errorf("%s: no tree links", where)
		}
		for _, a := range anchor.FindAllString(tree, -1) {
			l := links(a)[0]
			if strings.Contains(a, `aria-current="true"`) {
				// The picked row may link to the branch above it; its id is still its own branch's.
				claim(where, l.id, branchOf(target))
				continue
			}
			claim(where, l.id, branchOf(l.href))
			next = append(next, l.href)
		}
		return next
	}

	// Breadth first from each tab, the first 15 states of each: the top level, the open levels under
	// it and a set of halves, each top-level branch seen on many answers. (Every branch of every tab
	// is 490 states and two minutes; this is the same check in seconds.)
	const perTab = 15
	visited := map[string]bool{}
	pages := 0
	for _, start := range starts {
		todo := []string{start}
		for n := 0; len(todo) > 0 && n < perTab; {
			target := todo[0]
			todo = todo[1:]
			if visited[target] {
				continue
			}
			visited[target] = true
			code, body := getPage(t, h, target)
			if code != http.StatusOK {
				t.Errorf("%s = %d", target, code)
				continue
			}
			pages++
			n++
			todo = append(todo, check(target, body, false)...)
			code, answer := hxGet(t, h, target)
			if code != http.StatusOK {
				t.Errorf("%s (htmx) = %d", target, code)
				continue
			}
			check(target, answer, true)
			// The same state draws the same ids, page or answer.
			_, _, pageTree := panelParts(body, false)
			_, _, answerTree := panelParts(answer, true)
			var a, b []string
			for _, l := range links(pageTree) {
				a = append(a, l.id)
			}
			for _, l := range links(answerTree) {
				b = append(b, l.id)
			}
			if strings.Join(a, " ") != strings.Join(b, " ") {
				t.Errorf("%s: the page's tree ids differ from the htmx answer's", target)
			}
		}
	}
	if pages < 2*len(starts) {
		t.Errorf("only %d pages walked", pages)
	}
	halves := 0
	for id := range branchOfID {
		if strings.Contains(id, "--") {
			halves++
		}
	}
	if halves == 0 {
		t.Error("no half's branch walked: the walk is too short to test them")
	}
	t.Logf("%d states walked (page and htmx answer each), %d branch ids (%d of halves), %d tab ids", pages, len(idOfBranch), halves, len(idOfTab))
}

func TestTheFirstTabNamedIsTheArrivalStatesPage(t *testing.T) {
	h, pool := corpusRouter(t)
	var first string
	if err := pool.QueryRow(context.Background(), `SELECT slug FROM source_tabs ORDER BY sort_order, id LIMIT 1`).Scan(&first); err != nil {
		t.Fatal(err)
	}
	_, arrival := getPage(t, h, "/armory")
	_, _, tree := panelParts(arrival, false)
	rows := links(tree)
	if len(rows) == 0 {
		t.Fatal("no branch on arrival")
	}
	branch, _ := url.Parse(rows[0].href)
	// Three states with results: arrival, a non-default sort, a branch of the first tab with a filter.
	// (The empty state's "Clear the search" link is left out: verify round 2 records it, O1.)
	for _, q := range []string{"", "sort=name", branch.RawQuery + "&rarity=epic"} {
		plain := "/armory"
		if q != "" {
			plain += "?" + q
		}
		named := "/armory?tab=" + first
		if q != "" {
			named += "&" + q
		}
		for _, htmx := range []bool{false, true} {
			get := getPage
			if htmx {
				get = hxGet
			}
			c1, want := get(t, h, plain)
			c2, got := get(t, h, named)
			if c1 != http.StatusOK || c2 != http.StatusOK {
				t.Fatalf("%s = %d, %s = %d", plain, c1, named, c2)
			}
			if got != want {
				t.Errorf("%s (htmx %v) is not %s's page: one state, one page, one canonical", named, htmx, plain)
			}
			if strings.Contains(got, "tab="+first) {
				t.Errorf("%s (htmx %v) still carries tab=%s in a link or its canonical", named, htmx, first)
			}
		}
	}
}
