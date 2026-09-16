package templates_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/pierrehrt/aoc-api/internal/templates"
)

type fakeAssets struct{}

func (fakeAssets) Path(name string) (string, error) { return "/assets/" + name, nil }

func good() fstest.MapFS {
	return fstest.MapFS{
		"html/base.html": &fstest.MapFile{Data: []byte(`{{define "base"}}<html><title>{{.View.Title}}</title>{{template "content" .}}</html>{{end}}`)},
		"html/echo.html": &fstest.MapFile{Data: []byte(`{{define "echo"}}<p>{{.Echo}}</p>{{end}}`)},
		"html/ok.html":   &fstest.MapFile{Data: []byte(`{{define "content"}}<p>hello</p>{{end}}`)},
	}
}

// ⭐ A broken template must take the BINARY down at boot, where Railway keeps the
// previous deploy serving and CI has already gone red — not produce a 500 on one page,
// found by a visitor.
func TestNewFSFailsAtStartupOnABrokenTemplate(t *testing.T) {
	t.Run("a template that does not parse", func(t *testing.T) {
		f := good()
		f["html/bad.html"] = &fstest.MapFile{Data: []byte(`{{define "content"}}{{.Unclosed`)}
		if _, err := templates.NewFS(f, map[string]string{"bad": "html/bad.html"}, fakeAssets{}); err == nil {
			t.Fatal("NewFS accepted a template that does not parse")
		}
	})

	// The one the startup probe exists for: this PARSES fine and only fails when run.
	t.Run("a template that parses but cannot execute", func(t *testing.T) {
		f := good()
		f["html/bad.html"] = &fstest.MapFile{Data: []byte(`{{define "content"}}{{call .Data.NotAFunction}}{{end}}`)}
		_, err := templates.NewFS(f, map[string]string{"bad": "html/bad.html"}, fakeAssets{})
		if err == nil {
			t.Fatal("NewFS accepted a template that parses but fails to execute — the startup probe did not run")
		}
		if !strings.Contains(err.Error(), "does not execute") {
			t.Errorf("error = %q, want it to name the execution failure", err)
		}
	})

	t.Run("the good set still loads", func(t *testing.T) {
		if _, err := templates.NewFS(good(), map[string]string{"ok": "html/ok.html"}, fakeAssets{}); err != nil {
			t.Fatalf("NewFS rejected a valid template set: %v", err)
		}
	})
}

// A page must not be able to render without its head fields — SEO is the whole reason
// this service renders HTML, and a page that skips them quietly undoes that.
func TestRenderRefusesAnIncompleteView(t *testing.T) {
	e, err := templates.NewFS(good(), map[string]string{"ok": "html/ok.html"}, fakeAssets{})
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct {
		name string
		v    templates.View
	}{
		{"no title", templates.View{Description: "d", Canonical: "c"}},
		{"no description", templates.View{Title: "t", Canonical: "c"}},
		{"no canonical", templates.View{Title: "t", Description: "d"}},
	} {
		t.Run(c.name, func(t *testing.T) {
			rr := httptest.NewRecorder()
			err := e.Render(rr, httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/", nil), 200, "ok", c.v, nil)
			if err == nil {
				t.Fatal("Render accepted a View with a missing head field")
			}
			if rr.Body.Len() != 0 {
				t.Errorf("Render wrote %d bytes before refusing — a half-page cannot be turned into a 500", rr.Body.Len())
			}
		})
	}
}

// An unknown page name must be an error, not a blank 200.
func TestRenderRefusesAnUnknownPage(t *testing.T) {
	e, _ := templates.NewFS(good(), map[string]string{"ok": "html/ok.html"}, fakeAssets{})
	rr := httptest.NewRecorder()
	v := templates.NewView("t", "d", "https://x/")
	if err := e.Render(rr, httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/", nil), 200, "nope", v, nil); err == nil {
		t.Fatal("Render accepted a page name that does not exist")
	}
}

// ⭐ A template error mid-execution must not become "200 plus half a page". Render builds
// into a buffer first precisely so the status can still be changed; writing straight to the
// ResponseWriter makes that impossible, and that mutant survived (verify round 1).
//
// The template below PASSES the startup probe — probeData has a Marker field — and fails
// only at render time, when a handler passes data that does not. That is the real shape of
// this bug: a page that works until someone adds a second caller.
func TestRenderWritesNothingWhenExecutionFails(t *testing.T) {
	f := good()
	f["html/cond.html"] = &fstest.MapFile{Data: []byte(`{{define "content"}}before{{.Data.Marker}}after{{end}}`)}
	e, err := templates.NewFS(f, map[string]string{"cond": "html/cond.html"}, fakeAssets{})
	if err != nil {
		t.Fatalf("template referencing a probed field should start cleanly: %v", err)
	}

	rr := httptest.NewRecorder()
	v := templates.NewView("t", "d", "https://x/")
	err = e.Render(rr, httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/", nil),
		http.StatusOK, "cond", v, struct{ Other string }{"x"})
	if err == nil {
		t.Fatal("Render succeeded on a template that cannot execute against this data")
	}
	if rr.Body.Len() != 0 {
		t.Errorf("Render wrote %d bytes before failing (%q) — a half-page cannot be turned"+
			" into a 500", rr.Body.Len(), rr.Body.String())
	}
}

// Render must honour the status it is handed. Every caller passes 200 today, so a mutant
// ignoring the argument survived — and the first caller to pass 404 would silently 200.
func TestRenderHonoursTheStatusItIsGiven(t *testing.T) {
	e, err := templates.NewFS(good(), map[string]string{"ok": "html/ok.html"}, fakeAssets{})
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []int{http.StatusOK, http.StatusNotFound, http.StatusGone} {
		rr := httptest.NewRecorder()
		v := templates.NewView("t", "d", "https://x/")
		if err := e.Render(rr, httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/", nil), want, "ok", v, nil); err != nil {
			t.Fatalf("Render: %v", err)
		}
		if rr.Code != want {
			t.Errorf("Render wrote status %d, want %d", rr.Code, want)
		}
	}
}

// A fragment that parses but cannot execute is reached only by an HTMX request, so it
// would otherwise wait in production until someone clicked that one control.
func TestNewFSRejectsABrokenFragment(t *testing.T) {
	f := good()
	f["html/echo.html"] = &fstest.MapFile{Data: []byte(`{{define "echo"}}{{call .Nope}}{{end}}`)}
	if _, err := templates.NewFS(f, map[string]string{"ok": "html/ok.html"}, fakeAssets{}); err == nil {
		t.Fatal("NewFS accepted a fragment that parses but cannot execute")
	}
}

// Without a resolver every asset URL would render empty — <link href=""> — which loads
// the page itself as a stylesheet and looks like a styling bug, not a wiring bug.
func TestNewFSRequiresAnAssetResolver(t *testing.T) {
	if _, err := templates.NewFS(good(), map[string]string{"ok": "html/ok.html"}, nil); err == nil {
		t.Fatal("NewFS accepted a nil AssetResolver")
	}
}
