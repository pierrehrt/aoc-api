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

// sitemapMaxURLs is the protocol's limit per file (sitemaps.org). A chunk never exceeds it; the
// index lists as many chunks as the URLs need. A variable only so a test can prove the boundary
// without 50,000 rows.
var sitemapMaxURLs = 50000

// robotsDisallow is every path a crawler is kept out of — the ONE list; robots.txt is built from it.
// Machinery and the JSON contract, never content. /assets stays open: Google renders with our CSS.
var robotsDisallow = []string{"/_smoke", "/v1/", "/health"}

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

// sitemapStatic is the pages that are not items: the home page, then every section in the nav —
// which by rule lists only routes that exist (TestEveryNavLinkIsARegisteredRoute).
func sitemapStatic() []string {
	out := []string{"/"}
	for _, n := range siteNav {
		out = append(out, n.Path)
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
func chunks(total int) int { return (total + sitemapMaxURLs - 1) / sitemapMaxURLs }

func (h *Handler) sitemap(w http.ResponseWriter, r *http.Request) {
	total, err := h.sitemapTotal(r)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	idx := sitemapIndex{Xmlns: sitemapNS}
	for n := 1; n <= chunks(total); n++ {
		idx.Sitemaps = append(idx.Sitemaps, sitemapLoc{Loc: h.canonical(fmt.Sprintf("/sitemaps/%d.xml", n))})
	}
	writeXML(w, r, h, idx)
}

func (h *Handler) sitemapChunk(w http.ResponseWriter, r *http.Request) {
	n, err := strconv.Atoi(chi.URLParam(r, "n"))
	total, terr := h.sitemapTotal(r)
	if terr != nil {
		h.fail(w, r, terr)
		return
	}
	if err != nil || n < 1 || n > chunks(total) {
		httpx.RejectHTML(w, r, http.StatusNotFound, "There is no such sitemap")
		return
	}
	// This chunk's window over the one sequence "static pages, then items in id order".
	start, end := (n-1)*sitemapMaxURLs, n*sitemapMaxURLs
	static := sitemapStatic()
	set := urlSet{Xmlns: sitemapNS}
	for i := start; i < end && i < len(static); i++ {
		set.URLs = append(set.URLs, sitemapLoc{Loc: h.canonical(static[i])})
	}
	if end > len(static) {
		offset := start - len(static)
		if offset < 0 {
			offset = 0
		}
		limit := end - len(static) - offset
		if room := sitemapMaxURLs - len(set.URLs); limit > room {
			limit = room
		}
		slugs, err := h.items.Slugs(r.Context(), limit, offset)
		if err != nil {
			h.fail(w, r, err)
			return
		}
		for _, s := range slugs {
			set.URLs = append(set.URLs, sitemapLoc{Loc: h.canonical("/armory/" + s)})
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
