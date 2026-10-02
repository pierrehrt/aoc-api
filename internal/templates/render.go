package templates

import (
	"bytes"
	"embed"
	"fmt"
	"html/template"
	"io/fs"
	"net/http"

	"github.com/pierrehrt/aoc-api/internal/httpx"
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
	"home":   "html/home.html",
	"smoke":  "html/smoke.html",
	"armory": "html/armory.html",
	"item":   "html/item.html",
	"soon":   "html/soon.html",
}

// fragmentsIn lists the HTMX partials: every html/*.html that is neither base.html nor a page.
// Every page is parsed WITH all of them, so a page can {{template "armory_rows" .Data}} the same
// definition its fragment answer uses — one definition of the rows, two renderings (AOC-047).
// Derived from the filesystem rather than listed, so a fixture FS in a test carries only the
// fragments it holds and the real one cannot forget to register a file.
func fragmentsIn(fsys fs.FS, pageMap map[string]string) ([]string, error) {
	all, err := fs.Glob(fsys, "html/*.html")
	if err != nil {
		return nil, err
	}
	isPage := map[string]bool{"html/base.html": true}
	for _, f := range pageMap {
		isPage[f] = true
	}
	var out []string
	for _, f := range all {
		if !isPage[f] {
			out = append(out, f)
		}
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("no fragment templates under html/")
	}
	return out, nil
}

// pageProbes is the data the startup probe renders each page with, where the shared probeData is
// not enough. A page that reads fields nobody declared here fails at boot — the point.
var pageProbes = map[string]any{
	"armory": armoryProbe(),
	"item":   itemProbe(),
	"soon":   SoonData{Section: "Probe"},
}

// fragmentProbes likewise, by fragment name.
var fragmentProbes = map[string]any{
	"armory_rows":         armoryProbe(),
	"armory_facets":       armoryProbe(),
	"armory_update":       armoryProbe(),
	"armory_filter_count": armoryProbe(),
	"armory_chips":        armoryProbe(),
	"armory_choice":       RailOption{Name: "probe", ID: "f-probe"},
	"armory_active":       armoryProbe(),
	// The sources panel (AOC-068).
	"armory_tabs":             armoryProbe(),
	"armory_tabs_sheet":       armoryProbe(),
	"armory_tab_class":        armoryProbe().Sources.Tabs[0],
	"armory_sources_count":    armoryProbe(),
	"armory_sources_head":     armoryProbe(),
	"armory_sources_tree":     armoryProbe(),
	"armory_sources_selected": armoryProbe(),
	"armory_sources_show":     armoryProbe(),
	"armory_invalid":          InvalidSearch{Reason: "probe"},
	"armory_slot":             armoryProbe().Result.Items[0],
	"armory_type":             armoryProbe().Result.Items[0],
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
		// statText: a stat line as the tooltip prints it (AOC-048).
		"statText": StatText,
		// The list row's three display rules (AOC-062), in Go so each is written once: the Slot cell,
		// the Type cell, and the phone line that joins what is present.
		"slotNames": SlotNames,
		"typeLabel": TypeLabel,
		"phoneLine": PhoneLine,
		// num prints a count the design's way: 4,646 (AOC-065).
		"num": Num,
	}

	fragmentFiles, err := fragmentsIn(fsys, pageMap)
	if err != nil {
		return nil, fmt.Errorf("templates: %w", err)
	}
	frag, err := template.New("fragments").Funcs(funcs).ParseFS(fsys, fragmentFiles...)
	if err != nil {
		return nil, fmt.Errorf("templates: parsing fragments: %w", err)
	}
	e.fragments = frag

	for name, file := range pageMap {
		t, err := template.New(name).Funcs(funcs).ParseFS(fsys, append(append([]string{"html/base.html"}, fragmentFiles...), file)...)
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
		data := any(probeData)
		if p, ok := pageProbes[name]; ok {
			data = p
		}
		if err := e.pages[name].ExecuteTemplate(&bytes.Buffer{}, "base", page{View: probe, Data: data}); err != nil {
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
		data := any(probeData)
		if p, ok := fragmentProbes[name]; ok {
			data = p
		}
		if err := e.fragments.ExecuteTemplate(&bytes.Buffer{}, name, data); err != nil {
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
	return e.FragmentStatus(w, http.StatusOK, name, data)
}

// FragmentStatus is Fragment with a status other than 200 — a page's own rejection of an HTMX
// request, which the client swaps in where the answer would have gone (AOC-049: the armory's
// invalid-range message; base.html's htmx-config is what lets a 400 be swapped at all).
func (e *Engine) FragmentStatus(w http.ResponseWriter, status int, name string, data any) error {
	var buf bytes.Buffer
	if err := e.fragments.ExecuteTemplate(&buf, name, data); err != nil {
		return fmt.Errorf("templates: executing fragment %q: %w", name, err)
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	// ⚠️ Vary on HX-Request. A fragment and a full page share a URL, so without this a
	// shared cache can hand a browser asking for a page the bare fragment it stored for
	// HTMX — a blank-looking site served from cache, which is very hard to diagnose.
	w.Header().Add("Vary", "HX-Request")
	w.WriteHeader(status)
	_, err := buf.WriteTo(w)
	return err
}

// IsHTMX reports whether this request wants a fragment rather than a page. It is httpx.IsHTMX, not a
// copy of it: the cache policy reads the same header to decide that a fragment is never stored, and
// two spellings of one rule are free to disagree (AOC-026).
func IsHTMX(r *http.Request) bool { return httpx.IsHTMX(r) }

// Files exposes the embedded templates for tests that want to inspect them.
func Files() fs.FS { return files }
