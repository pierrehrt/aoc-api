package pages

import (
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"

	"github.com/pierrehrt/aoc-api/internal/httpx"
	"github.com/pierrehrt/aoc-api/internal/items"
	"github.com/pierrehrt/aoc-api/internal/templates"
)

// The Armory list page (AOC-047): /armory?q=&sort=&p=, and the filter rail (AOC-049).
//
// ⭐ ONE parser, ONE service. The query string is read by items.ParseFilters — the same function the
// JSON handler uses — and the rows come from the same items.Service.List, so nothing here can mean
// one thing on the page and another in /v1 (CLAUDE.md 5b). The page owns only what a page owns:
// `p` (1-based, 50 rows), the URLs it links to, and the two renderings (full page, or the rows
// fragment for an HTMX request).

const armoryPageSize = 50

// sortLabels is presentation: the words on the sort control for the keys items.Sorts defines, with
// the direction each one runs (the design's "Item level ↓").
var sortLabels = map[string]string{items.SortILvl: "Item level ↓", items.SortName: "Name ↑", items.SortID: "Id ↑"}

func (h *Handler) armory(w http.ResponseWriter, r *http.Request) {
	f, err := items.ParseFilters(r)
	if err != nil {
		// The parser's reason is shown: it names only parameters and the reader's own input
		// ("ilvl_min (80) is above ilvl_max (60)"), never anything internal.
		reason := strings.TrimPrefix(err.Error(), httpx.ErrInvalid.Error()+": ")
		if templates.IsHTMX(r) {
			// A live change from the rail (AOC-049 review): a 400 that HTMX swapped nowhere left the
			// rail silently dead. The message goes where the rows were; the rail is not redrawn, so
			// the bad value stays where the reader can fix it, and no URL is pushed — "false" says so
			// explicitly, or htmx pushes the request's own URL (measured in a browser).
			w.Header().Set("HX-Push-Url", "false")
			if err := h.tpl.FragmentStatus(w, http.StatusBadRequest, "armory_invalid", templates.InvalidSearch{Reason: reason}); err != nil {
				h.fail(w, r, err)
			}
			return
		}
		httpx.RejectHTML(w, r, http.StatusBadRequest, "That search is not valid: "+reason)
		return
	}
	page := 1
	if p := r.URL.Query().Get("p"); p != "" {
		if page, err = strconv.Atoi(p); err != nil || page < 1 {
			httpx.RejectHTML(w, r, http.StatusBadRequest, "That page number is not valid")
			return
		}
	}
	// The page's defaults: 50 rows, item level first (the design's default; /v1 keeps name), and
	// always the rail's counts (AOC-049).
	f.Limit = armoryPageSize
	f.Offset = (page - 1) * armoryPageSize
	if f.Sort == "" {
		f.Sort = items.SortILvl
	}
	f.WithFacets = true

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

	// ⭐ Every link on the page is built from the whole state (items.Filters.Values), so no filter can
	// fall out of a pager, a sort or a chip.
	here := func(g items.Filters) string { return armoryURL(g, 1) }
	withoutQuery := f
	withoutQuery.Query = ""
	rail, chips, clearAll := buildRail(f, res.Facets, here)
	d := templates.ArmoryData{
		Query:    f.Query,
		Sort:     f.Sort,
		Result:   res,
		Span:     span,
		Page:     page,
		Pages:    pages,
		Clear:    here(withoutQuery),
		Rail:     rail,
		Chips:    chips,
		ClearAll: clearAll,
	}
	for i, k := range items.Sorts {
		g := f
		g.Sort = k
		d.Sorts = append(d.Sorts, templates.SortOption{Key: k, Label: sortLabels[k], URL: here(g), Current: k == f.Sort})
		if k == f.Sort {
			// The design's one sort button: it shows this order and leads to the next one.
			next := f
			next.Sort = items.Sorts[(i+1)%len(items.Sorts)]
			d.SortLabel, d.NextSort = sortLabels[k], here(next)
		}
	}
	d.URL = armoryURL(f, page)
	if n := int64(len(res.Items)); n > 0 {
		d.RowsFrom = int64(f.Offset) + 1
		d.RowsTo = int64(f.Offset) + int64(armoryPageSize)
		if d.RowsTo > res.Total {
			d.RowsTo = res.Total
		}
	}
	if page > 1 {
		d.Prev = armoryURL(f, page-1)
	}
	if page < pages {
		d.Next = armoryURL(f, page+1)
	}
	for _, n := range pagerWindow(page, pages) {
		d.Pager = append(d.Pager, templates.PageLink{N: n, URL: armoryURL(f, n), Current: n == page})
	}

	if templates.IsHTMX(r) {
		// The rows, plus the rail out of band: one request re-renders both, so the counts always
		// describe the rows beside them. HX-Push-Url is this state's own URL, not the form's raw
		// query string with its empty fields — one URL per state (AOC-049).
		w.Header().Set("HX-Push-Url", armoryURL(f, page))
		// Fragment adds Vary: HX-Request itself.
		if err := h.tpl.Fragment(w, "armory_update", d); err != nil {
			h.fail(w, r, err)
		}
		return
	}
	// The full page varies too: this URL's body depends on the header.
	w.Header().Add("Vary", "HX-Request")

	title, desc := "Armory — every item in Age of Conan", fmt.Sprintf("Search and sort the %d items of the Age of Conan armory: slot, item level, class, where it drops and what it costs.", span.Total)
	if f.Query != "" {
		title = fmt.Sprintf("“%s” — Armory", f.Query)
		desc = fmt.Sprintf("%s match “%s” in the Age of Conan armory.", itemCount(res.Total), f.Query)
	}
	if page > 1 {
		title = fmt.Sprintf("%s — page %d", title, page)
	}
	// The canonical is this state without a redundant p=1, so the first page has one URL.
	v := h.view(title, desc, armoryURL(f, page))
	v.App = true // the validated design's full-window app, from lg up (AOC-065)
	h.render(w, r, "armory", v, d)
}

// itemCount is "1 item", "7 items" — the meta description said "1 items" (AOC-047 verify round 3).
func itemCount(n int64) string {
	if n == 1 {
		return "1 item"
	}
	return strconv.FormatInt(n, 10) + " items"
}

// armoryURL builds the list's own URLs from the whole state: only what differs from the default is
// in the query string, so the same state always has the same URL (and the canonical never carries
// p=1 or the page's default sort).
func armoryURL(f items.Filters, page int) string {
	v := f.Values()
	// A list means any of its values, in any order, so one state has ONE URL: each list sorted
	// (AOC-064 verify F4 — ?rarity=rare&rarity=epic and ?rarity=epic&rarity=rare were two canonicals).
	for _, k := range []string{"rarity", "equip_location", "armour_weight", "class", "currency", "set", "place"} {
		sort.Strings(v[k])
	}
	if v.Get("sort") == items.SortILvl {
		v.Del("sort")
	}
	if page > 1 {
		v.Set("p", strconv.Itoa(page))
	}
	if len(v) == 0 {
		return "/armory"
	}
	return "/armory?" + v.Encode()
}

// pagerWindow is the numbered links to show: five consecutive pages around the current one, as the
// design draws them (‹ Prev 1 2 3 4 5 Next ›) — 93 pages of 50 must not become 93 links.
func pagerWindow(page, pages int) []int {
	from := page - 2
	if from > pages-4 {
		from = pages - 4
	}
	if from < 1 {
		from = 1
	}
	to := from + 4
	if to > pages {
		to = pages
	}
	var out []int
	for n := from; n <= to; n++ {
		out = append(out, n)
	}
	return out
}
