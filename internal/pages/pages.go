// Package pages holds the HTML surface: handlers that build a View and render a template.
//
// Handlers here are THIN by rule (CLAUDE.md 5b). They assemble a view struct and hand it
// to the template engine. Domain logic lives in internal/<domain>/ and is shared with the
// JSON handlers under /v1, so the two surfaces can never drift.
package pages

import (
	"log/slog"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/pierrehrt/aoc-api/internal/httpx"
	"github.com/pierrehrt/aoc-api/internal/items"
	"github.com/pierrehrt/aoc-api/internal/templates"
)

// Handler renders the public site.
type Handler struct {
	tpl    *templates.Engine
	assets templates.AssetResolver
	// baseURL is the origin canonical URLs are built from, e.g. "https://aoc-codex.app".
	//
	// ⚠️ Deliberately NOT derived from the request. Behind Railway the request host is
	// whatever hostname was used, so the same page reached by two hostnames would declare
	// two different canonicals — which is precisely the duplicate-content problem the
	// canonical tag exists to solve. One configured origin, one canonical.
	baseURL string
	// items is THE armory service — the same *items.Service the JSON handlers hold (CLAUDE.md 5b).
	items *items.Service
	// sitemapMax is how many URLs one sitemap file may hold: the protocol's 50,000. A field, not a
	// package global, so a test can prove the boundary on its own handler without racing another.
	sitemapMax int
}

func New(tpl *templates.Engine, assets templates.AssetResolver, baseURL string, svc *items.Service) *Handler {
	return &Handler{tpl: tpl, assets: assets, baseURL: strings.TrimRight(baseURL, "/"), items: svc, sitemapMax: sitemapProtocolMax}
}

func (h *Handler) canonical(path string) string { return h.baseURL + path }

// view builds a View with the site-wide defaults already applied.
//
// ⭐ Every page gets an og:image through here. Before this, OGImage was a field nothing
// ever set, so the guard in base.html never fired and pages shipped a
// `twitter:card: summary_large_image` with no image — a broken social card on the very
// ticket whose point is that link previews work (AOC-024 verify round 1).
//
// ⚠️ The image is the built stylesheet's sibling in internal/assets/built/, so it is
// content-hashed and embedded like everything else. A later ticket can replace the
// artwork without touching a handler.
func (h *Handler) view(title, description, path string) templates.View {
	v := templates.NewView(title, description, h.canonical(path))
	if p, err := h.assets.Path(ogImageAsset); err == nil {
		v.OGImage = h.baseURL + p
	}
	v.Nav = make([]templates.NavItem, len(siteNav))
	for i, n := range siteNav {
		n.Current = path == n.Path || strings.HasPrefix(path, n.Path+"/") || strings.HasPrefix(path, n.Path+"?")
		v.Nav[i] = n
	}
	return v
}

// siteNav is the header's section links (AOC-046). ⛔ Every one leads to a route that EXISTS — a link
// to a 404 is a bug, and TestEveryNavLinkIsARegisteredRoute walks it against the real router. The
// design's AA's / Feats / DJ-Raids / More tabs are shown before their sections exist (Pierre,
// 2026-10-01): Soon gives each a "Coming Soon" page (noindex, kept out of the sitemap). A section that
// ships drops Soon and takes over its URL — or 301s it, if it moves (CLAUDE.md 5c).
var siteNav = []templates.NavItem{
	{Label: "Armory", Path: "/armory"},
	{Label: "AA's", Path: "/aa", Soon: true},
	{Label: "Feats", Path: "/feats", Soon: true},
	{Label: "DJ/Raids", Path: "/dj-raids", Soon: true},
	{Label: "More", Path: "/more", Soon: true, Menu: true},
}

// ogImageAsset is the social-card image. Kept as a constant so a missing one is a single
// obvious edit rather than a string repeated across handlers.
const ogImageAsset = "og-card.png"

// Routes mounts the HTML surface on the root router.
func (h *Handler) Routes(r chi.Router) {
	r.Get("/", h.home)
	r.Get("/armory", h.armory)
	r.Get("/armory/{slug}", h.item)
	for _, n := range siteNav {
		if n.Soon {
			r.Get(n.Path, h.soon(n.Label))
		}
	}
	r.Get("/robots.txt", h.robots)
	r.Get("/sitemap.xml", h.sitemap)
	r.Get("/sitemaps/{n}.xml", h.sitemapChunk)
	r.Get("/_smoke", h.smoke)
	r.Post("/_smoke/echo", h.echo)
}

func (h *Handler) home(w http.ResponseWriter, r *http.Request) {
	v := h.view(
		"AoC Codex — Age of Conan reference",
		"Boss mechanics, loot and builds for Age of Conan: Hyborian Adventures, written to be read in the three minutes before a pull.",
		"/",
	)
	h.render(w, r, "home", v, nil)
}

// soon answers a section the header names but that is not built: a Coming Soon page, noindex
// (thin content must not be indexed) and absent from the sitemap (sitemapStatic skips Soon).
func (h *Handler) soon(section string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		v := h.view(section+" — coming soon", "The "+section+" section of AoC Codex is not built yet.", r.URL.Path)
		v.NoIndex = true
		h.render(w, r, "soon", v, templates.SoonData{Section: section})
	}
}

// smokeData is what the proving page shows. It has no meaning beyond proving the
// pipeline end to end.
type smokeData struct {
	Marker string
	Echo   string
}

func (h *Handler) smoke(w http.ResponseWriter, r *http.Request) {
	v := h.view(
		"Rendering smoke page",
		"Internal page proving the rendering pipeline. Not content, not indexed, deleted by a later ticket.",
		"/_smoke",
	)
	// noindex because this is machinery, not content. An internal page in a search index
	// is a small embarrassment that is very hard to get back out again.
	v.NoIndex = true
	h.render(w, r, "smoke", v, smokeData{Marker: "server-rendered"})
}

// echo is the HTMX target: one handler, one source of truth, two renderings.
//
// ⭐ It answers a plain form POST with a FULL PAGE and an HTMX request with a FRAGMENT.
// That is the progressive-enhancement rule made concrete (CLAUDE.md 5b): the page works
// with JavaScript disabled, and HTMX only removes the reload.
func (h *Handler) echo(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		h.fail(w, r, err)
		return
	}
	said := strings.TrimSpace(r.PostFormValue("say"))
	data := smokeData{Marker: "server-rendered", Echo: said}

	if templates.IsHTMX(r) {
		if err := h.tpl.Fragment(w, "echo", data); err != nil {
			h.fail(w, r, err)
		}
		return
	}

	v := h.view(
		"Rendering smoke page",
		"Internal page proving the rendering pipeline. Not content, not indexed, deleted by a later ticket.",
		"/_smoke",
	)
	v.NoIndex = true
	// Vary even on the full-page branch: this URL's body depends on the header, so a
	// cache must key on it whichever branch answered.
	w.Header().Add("Vary", "HX-Request")
	h.render(w, r, "smoke", v, data)
}

// render writes a full page as 200. A page's own rejections do not come through here: they use
// httpx.RejectHTML, dependency-free (a bad query, a page past the end — AOC-047).
func (h *Handler) render(w http.ResponseWriter, r *http.Request, name string, v templates.View, data any) {
	if err := h.tpl.Render(w, r, http.StatusOK, name, v, data); err != nil {
		h.fail(w, r, err)
	}
}

// fail logs and sends a plain 500. It cannot render the error as HTML, because the
// thing that just failed is the HTML renderer. A client that went away (a superseded live request,
// aborted) is not a failure: 499, logged as such (httpx.ClientGone).
func (h *Handler) fail(w http.ResponseWriter, r *http.Request, err error) {
	if httpx.ClientGone(r, err) {
		slog.InfoContext(r.Context(), "client closed request", "path", r.URL.Path, "status", httpx.StatusClientClosedRequest)
		w.WriteHeader(httpx.StatusClientClosedRequest)
		return
	}
	slog.ErrorContext(r.Context(), "page render failed", "path", r.URL.Path, "error", err)
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(http.StatusInternalServerError)
	_, _ = w.Write([]byte("internal server error\n"))
}
