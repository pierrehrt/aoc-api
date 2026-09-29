package pages

import (
	"fmt"
	"net/http"
	"net/url"
	"strconv"

	"github.com/pierrehrt/aoc-api/internal/httpx"
	"github.com/pierrehrt/aoc-api/internal/items"
	"github.com/pierrehrt/aoc-api/internal/templates"
)

// The Armory list page (AOC-047): /armory?q=&sort=&p=.
//
// ⭐ ONE parser, ONE service. The query string is read by items.ParseFilters — the same function the
// JSON handler uses — and the rows come from the same items.Service.List, so nothing here can mean
// one thing on the page and another in /v1 (CLAUDE.md 5b). The page owns only what a page owns:
// `p` (1-based, 50 rows), the URLs it links to, and the two renderings (full page, or the rows
// fragment for an HTMX request).

const armoryPageSize = 50

// sortLabels is presentation: the words on the sort control for the keys items.Sorts defines.
var sortLabels = map[string]string{items.SortILvl: "Item level", items.SortName: "Name", items.SortID: "Id"}

func (h *Handler) armory(w http.ResponseWriter, r *http.Request) {
	f, err := items.ParseFilters(r)
	if err != nil {
		httpx.RejectHTML(w, r, http.StatusBadRequest, "That search is not valid")
		return
	}
	page := 1
	if p := r.URL.Query().Get("p"); p != "" {
		if page, err = strconv.Atoi(p); err != nil || page < 1 {
			httpx.RejectHTML(w, r, http.StatusBadRequest, "That page number is not valid")
			return
		}
	}
	// The page's defaults: 50 rows, item level first (the design's default; /v1 keeps name).
	f.Limit = armoryPageSize
	f.Offset = (page - 1) * armoryPageSize
	if f.Sort == "" {
		f.Sort = items.SortILvl
	}

	res, err := h.items.List(r.Context(), f)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	span, err := h.items.IDSpan(r.Context())
	if err != nil {
		h.fail(w, r, err)
		return
	}
	pages := int((res.Total + armoryPageSize - 1) / armoryPageSize)
	if pages == 0 {
		pages = 1
	}
	if page > pages {
		// Past the end is a URL that names nothing — 404, not an empty 200 that looks like "no
		// items match" (the pager is built out of that difference).
		httpx.RejectHTML(w, r, http.StatusNotFound, "There is no page "+strconv.Itoa(page))
		return
	}

	link := func(sort string, p int) string { return armoryURL(f.Query, sort, p) }
	d := templates.ArmoryData{
		Query:  f.Query,
		Sort:   f.Sort,
		Result: res,
		Span:   span,
		Page:   page,
		Pages:  pages,
		Clear:  armoryURL("", f.Sort, 1),
	}
	for _, k := range items.Sorts {
		d.Sorts = append(d.Sorts, templates.SortOption{Key: k, Label: sortLabels[k], URL: link(k, 1), Current: k == f.Sort})
	}
	if page > 1 {
		d.Prev = link(f.Sort, page-1)
	}
	if page < pages {
		d.Next = link(f.Sort, page+1)
	}
	for _, n := range pagerWindow(page, pages) {
		d.Pager = append(d.Pager, templates.PageLink{N: n, URL: link(f.Sort, n), Current: n == page})
	}

	// Both branches vary on the header: this URL's body depends on it.
	w.Header().Add("Vary", "HX-Request")
	if templates.IsHTMX(r) {
		if err := h.tpl.Fragment(w, "armory_rows", d); err != nil {
			h.fail(w, r, err)
		}
		return
	}

	title, desc := "Armory — every item in Age of Conan", fmt.Sprintf("Search and sort the %d items of the Age of Conan armory: slot, item level, class, where it drops and what it costs.", span.Total)
	if f.Query != "" {
		title = fmt.Sprintf("“%s” — Armory", f.Query)
		desc = fmt.Sprintf("%d items match “%s” in the Age of Conan armory.", res.Total, f.Query)
	}
	if page > 1 {
		title = fmt.Sprintf("%s — page %d", title, page)
	}
	// The canonical is this state without a redundant p=1, so the first page has one URL.
	v := h.view(title, desc, link(f.Sort, page))
	h.render(w, r, http.StatusOK, "armory", v, d)
}

// armoryURL builds the list's own URLs: only what differs from the default is in the query
// string, so the same state always has the same URL (and the canonical never carries p=1).
func armoryURL(q, sort string, page int) string {
	v := url.Values{}
	if q != "" {
		v.Set("q", q)
	}
	if sort != "" && sort != items.SortILvl {
		v.Set("sort", sort)
	}
	if page > 1 {
		v.Set("p", strconv.Itoa(page))
	}
	if len(v) == 0 {
		return "/armory"
	}
	return "/armory?" + v.Encode()
}

// pagerWindow is the numbered links to show: the first, the last, and two either side of the
// current page — 93 pages of 50 must not become 93 links.
func pagerWindow(page, pages int) []int {
	var out []int
	for n := 1; n <= pages; n++ {
		if n == 1 || n == pages || (n >= page-2 && n <= page+2) {
			out = append(out, n)
		}
	}
	return out
}
