package templates

import (
	"bytes"
	"embed"
	"fmt"
	"html/template"
	"io/fs"
	"net/http"
	"strings"
)

//go:embed html/*.html
var files embed.FS

// page is what a template actually receives: the View plus its page data, so a
// template can reach .View.Title and .Data.Whatever without the two colliding.
type page struct {
	View View
	Data any
}

// Engine holds the parsed templates. One per process, built at startup.
type Engine struct {
	pages     map[string]*template.Template // full pages, each with "base"
	fragments *template.Template            // HTMX partials, rendered without a layout
	assets    AssetResolver
}

// AssetResolver maps a logical asset name ("app.css") to its served, content-hashed
// path ("/assets/app.7f3a91c2.css"). Injected so templates never guess a URL and so a
// missing asset is a startup failure rather than a 404 nobody notices.
type AssetResolver interface {
	Path(name string) (string, error)
}

// pageTemplates maps a page name to the file that defines its "content" block.
// Adding a page means adding a line here and a file beside it — nothing else.
var pageTemplates = map[string]string{
	"home":  "html/home.html",
	"smoke": "html/smoke.html",
}

// New parses every template ONCE and fails loudly if any of them is broken.
//
// ⭐ Parsing at startup rather than per request is the whole point: a typo in a template
// takes the binary down at boot, where Railway keeps the previous deploy serving and CI
// has already failed. Parsed lazily, the same typo is a 500 on one page, found by a
// visitor. It also means no parse cost per request.
func New(assets AssetResolver) (*Engine, error) { return NewFS(files, pageTemplates, assets) }

// NewFS is New against any filesystem and page map, so the startup probe and the parse
// failure can be TESTED. With only the real embed.FS they are unreachable from a test.
func NewFS(fsys fs.FS, pageMap map[string]string, assets AssetResolver) (*Engine, error) {
	if assets == nil {
		return nil, fmt.Errorf("templates: an AssetResolver is required")
	}
	e := &Engine{pages: make(map[string]*template.Template), assets: assets}

	funcs := template.FuncMap{
		// asset resolves at RENDER time but is validated at STARTUP by the probe below,
		// so a template referring to an asset that does not exist cannot reach production.
		"asset": func(name string) (string, error) { return assets.Path(name) },
	}

	frag, err := template.New("fragments").Funcs(funcs).ParseFS(fsys, "html/echo.html")
	if err != nil {
		return nil, fmt.Errorf("templates: parsing fragments: %w", err)
	}
	e.fragments = frag

	for name, file := range pageMap {
		t, err := template.New(name).Funcs(funcs).ParseFS(fsys, "html/base.html", "html/echo.html", file)
		if err != nil {
			return nil, fmt.Errorf("templates: parsing page %q: %w", name, err)
		}
		e.pages[name] = t
	}

	// STARTUP PROBE. Render every page into the void with a filled-in View. This is what
	// turns "the template parsed" into "the template executes": a missing field, a call
	// to a function that does not exist, or an asset that was never built all surface
	// here, at boot, instead of on a visitor's screen.
	probe := View{Title: "probe", Description: "probe", Canonical: "https://example.invalid/"}
	for name := range e.pages {
		if err := e.pages[name].ExecuteTemplate(&bytes.Buffer{}, "base", page{View: probe, Data: probeData}); err != nil {
			return nil, fmt.Errorf("templates: page %q parses but does not execute: %w", name, err)
		}
	}
	// Fragments too. They are reached only by an HTMX request, so a fragment that parses
	// but cannot execute would otherwise wait in production until someone clicked the
	// one control that renders it — the least-tested path failing in front of a user.
	// (AOC-024 verify round 1: this was missing.)
	for _, t := range e.fragments.Templates() {
		name := t.Name()
		if name == "" || name == "fragments" {
			continue
		}
		if err := e.fragments.ExecuteTemplate(&bytes.Buffer{}, name, probeData); err != nil {
			return nil, fmt.Errorf("templates: fragment %q parses but does not execute: %w", name, err)
		}
	}
	return e, nil
}

// probeData satisfies every field any page template reads during the startup probe.
// When a new page needs a field, add it here — that is the point: the probe should
// fail until the data it needs is declared.
var probeData = struct {
	Marker string
	Echo   string
}{Marker: "probe", Echo: "probe"}

// Render writes a full page. It refuses a View that is missing its head fields.
func (e *Engine) Render(w http.ResponseWriter, r *http.Request, status int, name string, v View, data any) error {
	if err := v.Valid(); err != nil {
		return fmt.Errorf("templates: refusing to render %q: %w", name, err)
	}
	t, ok := e.pages[name]
	if !ok {
		return fmt.Errorf("templates: no page named %q", name)
	}
	// Render to a buffer first. Writing straight to the ResponseWriter means a template
	// error mid-execution has already sent 200 and half a page, and there is no way to
	// turn that into a 500.
	var buf bytes.Buffer
	if err := t.ExecuteTemplate(&buf, "base", page{View: v, Data: data}); err != nil {
		return fmt.Errorf("templates: executing %q: %w", name, err)
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	_, err := buf.WriteTo(w)
	return err
}

// Fragment writes an HTMX partial — no <html>, no layout.
func (e *Engine) Fragment(w http.ResponseWriter, name string, data any) error {
	var buf bytes.Buffer
	if err := e.fragments.ExecuteTemplate(&buf, name, data); err != nil {
		return fmt.Errorf("templates: executing fragment %q: %w", name, err)
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	// ⚠️ Vary on HX-Request. A fragment and a full page share a URL, so without this a
	// shared cache can hand a browser asking for a page the bare fragment it stored for
	// HTMX — a blank-looking site served from cache, which is very hard to diagnose.
	w.Header().Add("Vary", "HX-Request")
	_, err := buf.WriteTo(w)
	return err
}

// IsHTMX reports whether this request wants a fragment rather than a page.
func IsHTMX(r *http.Request) bool {
	return strings.EqualFold(r.Header.Get("HX-Request"), "true")
}

// Files exposes the embedded templates for tests that want to inspect them.
func Files() fs.FS { return files }
