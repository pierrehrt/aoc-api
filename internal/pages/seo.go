package pages

import (
	"encoding/xml"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/pierrehrt/aoc-api/internal/httpx"
)

// robots.txt and the sitemap (AOC-025): what makes the server-rendered pages FOUND, not just
// crawlable. ~4,650 item pages are a long tail Google will not reach by following links alone.
//
// ⭐ The sitemap is built from the database and the nav, never from a hand-kept list: an import
// that adds items adds their URLs, and a section added to siteNav is in the sitemap the same day.
//
// ⛔ No <lastmod>. No row carries a real modification time — the import is a full replace, so any
// timestamp would be the last import's, on every item at once — and a lastmod that is not
// "consistently and verifiably accurate" is one Google learns to ignore for the whole site.

// sitemapProtocolMax is the protocol's limit per file (sitemaps.org). A chunk never exceeds it; the
// index lists as many chunks as the URLs need.
const sitemapProtocolMax = 50000

// robotsDisallow is every path a crawler is kept out of — the ONE list; robots.txt is built from it.
// Machinery and the JSON contract, never content. /assets stays open: Google renders with our CSS.
//
// ⚠️ /_smoke is ALSO noindex, and disallowing a crawl hides a noindex from Google — the reason the
// Railway host is redirected rather than disallowed. Accepted here: the smoke page is linked from
// nowhere, so the "indexed without a snippet" case needs an outside link to a page due for deletion.
//
// /armory?*add= (AOC-051): the gear builder's "+" is an action, not a page. Each answer is a build URL
// carrying 50 more "+" links, so a crawler following them would fetch builds × filters × pages for
// ever; the canonical keeps those out of the index but not out of the crawl. Wildcards are Google's
// and Bing's robots syntax. The links also carry rel="nofollow".
var robotsDisallow = []string{"/_smoke", "/v1/", "/health", "/armory?*add="}

func (h *Handler) robots(w http.ResponseWriter, r *http.Request) {
	var b strings.Builder
	b.WriteString("User-agent: *\n")
	for _, p := range robotsDisallow {
		b.WriteString("Disallow: " + p + "\n")
	}
	b.WriteString("\nSitemap: " + h.canonical("/sitemap.xml") + "\n")
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	_, _ = w.Write([]byte(b.String()))
}

// sitemapStatic is the pages that are not items: the home page, then every BUILT section in the nav —
// which by rule lists only routes that exist (TestEveryNavLinkIsARegisteredRoute). A Coming Soon
// section is noindex and stays out (Pierre's tabs, 2026-10-01).
func sitemapStatic() []string {
	out := []string{"/"}
	for _, n := range siteNav {
		if !n.Soon {
			out = append(out, n.Path)
		}
	}
	return out
}

type sitemapIndex struct {
	XMLName  xml.Name     `xml:"sitemapindex"`
	Xmlns    string       `xml:"xmlns,attr"`
	Sitemaps []sitemapLoc `xml:"sitemap"`
}

type urlSet struct {
	XMLName xml.Name     `xml:"urlset"`
	Xmlns   string       `xml:"xmlns,attr"`
	URLs    []sitemapLoc `xml:"url"`
}

type sitemapLoc struct {
	Loc string `xml:"loc"`
}

const sitemapNS = "http://www.sitemaps.org/schemas/sitemap/0.9"

// sitemapTotal is how many URLs the sitemap holds: the static pages plus every item.
func (h *Handler) sitemapTotal(r *http.Request) (int, error) {
	span, err := h.items.IDSpan(r.Context())
	if err != nil {
		return 0, err
	}
	return len(sitemapStatic()) + int(span.Total), nil
}

// chunks is how many files the index lists. total is never 0 — the home page is always in it —
// so an empty armory still has one chunk, and a valid index pointing at it.
func (h *Handler) chunks(total int) int { return (total + h.sitemapMax - 1) / h.sitemapMax }

func (h *Handler) sitemap(w http.ResponseWriter, r *http.Request) {
	total, err := h.sitemapTotal(r)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	idx := sitemapIndex{Xmlns: sitemapNS}
	for n := 1; n <= h.chunks(total); n++ {
		idx.Sitemaps = append(idx.Sitemaps, sitemapLoc{Loc: h.canonical(fmt.Sprintf("/sitemaps/%d.xml", n))})
	}
	writeXML(w, r, h, idx)
}

func (h *Handler) sitemapChunk(w http.ResponseWriter, r *http.Request) {
	// The chunk number in its ONE spelling: Atoi also accepts "01", "001" and "+1", which would be
	// three more URLs for chunk 1's content. Checked before any query, so a junk URL costs nothing.
	raw := chi.URLParam(r, "n")
	n, err := strconv.Atoi(raw)
	if err != nil || n < 1 || raw != strconv.Itoa(n) {
		httpx.RejectHTML(w, r, http.StatusNotFound, "There is no such sitemap")
		return
	}
	total, err := h.sitemapTotal(r)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	if n > h.chunks(total) {
		httpx.RejectHTML(w, r, http.StatusNotFound, "There is no such sitemap")
		return
	}
	// This chunk's window [start, end) over the one sequence "static pages, then items in id order".
	start, end := (n-1)*h.sitemapMax, n*h.sitemapMax
	static := sitemapStatic()
	set := urlSet{Xmlns: sitemapNS}
	for i := start; i < end && i < len(static); i++ {
		set.URLs = append(set.URLs, sitemapLoc{Loc: h.canonical(static[i])})
	}
	if end > len(static) {
		first := max(start, len(static)) // the window's first item position in the sequence
		slugs, err := h.items.Slugs(r.Context(), end-first, first-len(static))
		if err != nil {
			h.fail(w, r, err)
			return
		}
		for _, s := range slugs {
			// ⛔ An empty slug would list /armory/, a 404 — and a sitemap may list only 200s. None is
			// empty today (0 of 4,646, measured 2026-09-30), but nothing in the schema forbids one.
			if s == "" {
				continue
			}
			set.URLs = append(set.URLs, sitemapLoc{Loc: h.canonical(itemPath(s))})
		}
	}
	writeXML(w, r, h, set)
}

// writeXML marshals first and writes after, so a marshalling failure is a 500, never half a file.
// encoding/xml escapes every value — html/template's escaping is for HTML, not XML.
func writeXML(w http.ResponseWriter, r *http.Request, h *Handler, v any) {
	body, err := xml.Marshal(v)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	w.Header().Set("Content-Type", "application/xml; charset=utf-8")
	_, _ = w.Write([]byte(xml.Header))
	_, _ = w.Write(body)
}
