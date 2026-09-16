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
	"github.com/pierrehrt/aoc-api/internal/templates"
)

// Handler renders the public site.
type Handler struct {
	tpl *templates.Engine
	// baseURL is the origin canonical URLs are built from, e.g. "https://aoc-codex.app".
	//
	// ⚠️ Deliberately NOT derived from the request. Behind Railway the request host is
	// whatever hostname was used, so the same page reached by two hostnames would declare
	// two different canonicals — which is precisely the duplicate-content problem the
	// canonical tag exists to solve. One configured origin, one canonical.
	baseURL string
}

func New(tpl *templates.Engine, baseURL string) *Handler {
	return &Handler{tpl: tpl, baseURL: strings.TrimRight(baseURL, "/")}
}

func (h *Handler) canonical(path string) string { return h.baseURL + path }

// Routes mounts the HTML surface on the root router.
func (h *Handler) Routes(r chi.Router) {
	r.Get("/", h.home)
	r.Get("/_smoke", h.smoke)
	r.Post("/_smoke/echo", h.echo)
}

func (h *Handler) home(w http.ResponseWriter, r *http.Request) {
	v := templates.NewView(
		"AoC Codex — Age of Conan reference",
		"Boss mechanics, loot and builds for Age of Conan: Hyborian Adventures, written to be read in the three minutes before a pull.",
		h.canonical("/"),
	)
	h.render(w, r, http.StatusOK, "home", v, nil)
}

// smokeData is what the proving page shows. It has no meaning beyond proving the
// pipeline end to end.
type smokeData struct {
	Marker string
	Echo   string
}

func (h *Handler) smoke(w http.ResponseWriter, r *http.Request) {
	v := templates.NewView(
		"Rendering smoke page",
		"Internal page proving the rendering pipeline. Not content, not indexed, deleted by a later ticket.",
		h.canonical("/_smoke"),
	)
	// noindex because this is machinery, not content. An internal page in a search index
	// is a small embarrassment that is very hard to get back out again.
	v.NoIndex = true
	h.render(w, r, http.StatusOK, "smoke", v, smokeData{Marker: "server-rendered"})
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

	v := templates.NewView(
		"Rendering smoke page",
		"Internal page proving the rendering pipeline. Not content, not indexed, deleted by a later ticket.",
		h.canonical("/_smoke"),
	)
	v.NoIndex = true
	// Vary even on the full-page branch: this URL's body depends on the header, so a
	// cache must key on it whichever branch answered.
	w.Header().Add("Vary", "HX-Request")
	h.render(w, r, http.StatusOK, "smoke", v, data)
}

func (h *Handler) render(w http.ResponseWriter, r *http.Request, status int, name string, v templates.View, data any) {
	if err := h.tpl.Render(w, r, status, name, v, data); err != nil {
		h.fail(w, r, err)
	}
}

// fail logs and sends a plain 500. It cannot render the error as HTML, because the
// thing that just failed is the HTML renderer.
func (h *Handler) fail(w http.ResponseWriter, r *http.Request, err error) {
	slog.ErrorContext(r.Context(), "page render failed", "path", r.URL.Path, "error", err)
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(http.StatusInternalServerError)
	_, _ = w.Write([]byte("internal server error\n"))
}
