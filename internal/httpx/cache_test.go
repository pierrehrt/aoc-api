package httpx_test

import (
	"context"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/pierrehrt/aoc-api/internal/httpx"
)

// The policy's values, spelled out here rather than imported: a test that compared the middleware
// against its own constants would pass whatever the constants said (AOC-026).
const (
	wantImmutable = "public, max-age=31536000, immutable"
	wantPage      = "public, max-age=60, s-maxage=3600, stale-while-revalidate=86400"
	wantAPI       = "public, max-age=60, s-maxage=600"
	wantMissing   = "public, max-age=60, s-maxage=60"
	wantNoStore   = "no-store"
	wantPrivate   = "private, no-store"
)

type cacheCase struct {
	name    string
	method  string
	path    string
	req     map[string]string // request headers
	status  int
	set     map[string]string // headers the handler sets itself — including a planted Cache-Control
	private bool              // the handler calls MarkPrivate
	want    string
}

func TestCachePolicy(t *testing.T) {
	cases := []cacheCase{
		// the path classes, on a 200
		{name: "home page", path: "/", status: 200, want: wantPage},
		{name: "a content page", path: "/items/test-item-alpha", status: 200, want: wantPage},
		{name: "HEAD of a page", method: http.MethodHead, path: "/", status: 200, want: wantPage},
		{name: "a /v1 list", path: "/v1/items", status: 200, want: wantAPI},
		{name: "/v1 itself", path: "/v1", status: 200, want: wantAPI},
		{name: "/v1x is not the API", path: "/v1x", status: 200, want: wantPage},
		{name: "a hashed asset", path: "/assets/app.0123abcd.css", status: 200, want: wantImmutable},
		{name: "/health", path: "/health", status: 200, want: wantNoStore},
		{name: "HEAD /health", method: http.MethodHead, path: "/health", status: 200, want: wantNoStore},
		{name: "/health failing", path: "/health", status: 503, want: wantNoStore},

		// statuses
		{name: "404 page", path: "/items/test-item-missing", status: 404, want: wantMissing},
		{name: "410 page", path: "/items/test-item-gone", status: 410, want: wantMissing},
		{name: "404 on /v1", path: "/v1/items/test-item-missing", status: 404, want: wantMissing},
		{name: "404 on /assets is not cached", path: "/assets/app.deadbeef.css", status: 404, want: wantNoStore},
		{name: "301 keeps the page policy", path: "/old-slug", status: 301, want: wantPage},
		{name: "308 keeps the page policy", path: "/old-slug", status: 308, want: wantPage},
		{name: "304 page", path: "/", status: 304, want: wantPage},
		{name: "304 /v1", path: "/v1/items", status: 304, want: wantAPI},
		{name: "304 asset", path: "/assets/app.0123abcd.css", status: 304, want: wantImmutable},
		{name: "302", path: "/", status: 302, want: wantNoStore},
		{name: "303", path: "/", status: 303, want: wantNoStore},
		{name: "307", path: "/", status: 307, want: wantNoStore},
		{name: "400", path: "/", status: 400, want: wantNoStore},
		{name: "405", path: "/", status: 405, want: wantNoStore},
		{name: "429", path: "/", status: 429, want: wantNoStore},
		{name: "500", path: "/", status: 500, want: wantNoStore},
		{name: "502 on /v1", path: "/v1/items", status: 502, want: wantNoStore},
		{name: "503 on an asset", path: "/assets/app.0123abcd.css", status: 503, want: wantNoStore},

		// methods
		{name: "POST", method: http.MethodPost, path: "/", status: 200, want: wantNoStore},
		{name: "PUT", method: http.MethodPut, path: "/v1/items", status: 200, want: wantNoStore},
		{name: "PATCH", method: http.MethodPatch, path: "/", status: 200, want: wantNoStore},
		{name: "DELETE", method: http.MethodDelete, path: "/", status: 200, want: wantNoStore},
		{name: "OPTIONS", method: http.MethodOptions, path: "/", status: 200, want: wantNoStore},

		// HTMX
		{name: "HTMX fragment", path: "/", req: map[string]string{"HX-Request": "true"}, status: 200, want: wantNoStore},
		{name: "HTMX header in capitals", path: "/", req: map[string]string{"HX-Request": "TRUE"}, status: 200, want: wantNoStore},
		{name: "HX-Request false is a page", path: "/", req: map[string]string{"HX-Request": "false"}, status: 200, want: wantPage},

		// ⛔ private overrides everything
		{name: "Authorization on a page", path: "/", req: map[string]string{"Authorization": "Bearer test"}, status: 200, want: wantPrivate},
		{name: "Authorization on /v1", path: "/v1/items", req: map[string]string{"Authorization": "Bearer test"}, status: 200, want: wantPrivate},
		{name: "Authorization on an asset", path: "/assets/app.0123abcd.css", req: map[string]string{"Authorization": "Bearer test"}, status: 200, want: wantPrivate},
		{name: "Set-Cookie on a page", path: "/", status: 200, set: map[string]string{"Set-Cookie": "sid=test"}, want: wantPrivate},
		{name: "Set-Cookie on an asset", path: "/assets/app.0123abcd.css", status: 200, set: map[string]string{"Set-Cookie": "sid=test"}, want: wantPrivate},
		{name: "Set-Cookie on a 404", path: "/nope", status: 404, set: map[string]string{"Set-Cookie": "sid=test"}, want: wantPrivate},
		{name: "MarkPrivate on a page", path: "/", status: 200, private: true, want: wantPrivate},
		{name: "MarkPrivate on /v1", path: "/v1/items", status: 200, private: true, want: wantPrivate},
		{name: "MarkPrivate on a 404", path: "/nope", status: 404, private: true, want: wantPrivate},
		{name: "MarkPrivate on a POST", method: http.MethodPost, path: "/", status: 200, private: true, want: wantPrivate},
		{name: "a request Cookie alone is not private", path: "/", req: map[string]string{"Cookie": "__cf_bm=test"}, status: 200, want: wantPage},

		// ⛔ the planted offenders: a handler setting the header by hand never wins
		{name: "planted: public on a private response", path: "/", status: 200, private: true,
			set: map[string]string{"Cache-Control": "public, max-age=31536000"}, want: wantPrivate},
		{name: "planted: public beside a Set-Cookie", path: "/", status: 200,
			set: map[string]string{"Cache-Control": "public, max-age=31536000", "Set-Cookie": "sid=test"}, want: wantPrivate},
		{name: "planted: public on an authorised request", path: "/v1/items", status: 200,
			req: map[string]string{"Authorization": "Bearer test"}, set: map[string]string{"Cache-Control": "public, s-maxage=86400"}, want: wantPrivate},
		{name: "planted: a hand-set value on a page is replaced", path: "/", status: 200,
			set: map[string]string{"Cache-Control": "public, max-age=999"}, want: wantPage},
		{name: "planted: a hand-set value on a 500 is replaced", path: "/", status: 500,
			set: map[string]string{"Cache-Control": "public, max-age=999"}, want: wantNoStore},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h := httpx.Cache(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if tc.private {
					httpx.MarkPrivate(r)
				}
				for k, v := range tc.set {
					w.Header().Set(k, v)
				}
				w.WriteHeader(tc.status)
			}))
			method := tc.method
			if method == "" {
				method = http.MethodGet
			}
			req := httptest.NewRequestWithContext(context.Background(), method, tc.path, nil)
			for k, v := range tc.req {
				req.Header.Set(k, v)
			}
			rr := httptest.NewRecorder()
			h.ServeHTTP(rr, req)
			// Result().Header is the snapshot taken when the status went out — what a client sees.
			// rr.Header() is the live map, which would also show a header set too late.
			if got := rr.Result().Header.Values("Cache-Control"); len(got) != 1 || got[0] != tc.want {
				t.Errorf("Cache-Control = %q, want exactly %q", got, tc.want)
			}
			if rr.Code != tc.status {
				t.Errorf("status = %d, want %d — the middleware must not change it", rr.Code, tc.status)
			}
		})
	}
}

// A handler that writes a body without WriteHeader, or writes nothing at all, still gets the
// header: net/http's implicit 200 would otherwise go out bare.
func TestCacheHeaderOnImplicitStatus(t *testing.T) {
	for name, handler := range map[string]http.HandlerFunc{
		"body only":      func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte("test body")) },
		"nothing at all": func(http.ResponseWriter, *http.Request) {},
	} {
		t.Run(name, func(t *testing.T) {
			rr := httptest.NewRecorder()
			httpx.Cache(handler).ServeHTTP(rr, httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/", nil))
			if rr.Code != http.StatusOK {
				t.Errorf("status = %d, want 200", rr.Code)
			}
			if got := rr.Result().Header.Get("Cache-Control"); got != wantPage {
				t.Errorf("Cache-Control = %q, want %q", got, wantPage)
			}
		})
	}
}

// An informational status (103 Early Hints) can precede the real one; the decision must wait for
// the real one. A recorder cannot show a 1xx followed by a 200, so this uses a real server.
func TestCacheDecidesOnTheFinalStatusNotA1xx(t *testing.T) {
	srv := httptest.NewServer(httpx.Cache(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusEarlyHints)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("test page"))
	})))
	defer srv.Close()
	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, srv.URL+"/", nil)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := srv.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	if got := resp.Header.Get("Cache-Control"); got != wantPage {
		t.Errorf("Cache-Control = %q, want %q — the policy was decided on the 103", got, wantPage)
	}
}

// MarkPrivate fails closed: it panics rather than silently doing nothing.
func TestMarkPrivateFailsClosed(t *testing.T) {
	t.Run("without the middleware", func(t *testing.T) {
		defer func() {
			if recover() == nil {
				t.Error("MarkPrivate without the Cache middleware did not panic — the mark would be read by nothing")
			}
		}()
		httpx.MarkPrivate(httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/", nil))
	})
	t.Run("after the response began", func(t *testing.T) {
		var recovered any
		h := httpx.Cache(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
			defer func() { recovered = recover() }()
			httpx.MarkPrivate(r)
		}))
		rr := httptest.NewRecorder()
		h.ServeHTTP(rr, httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/", nil))
		if recovered == nil {
			t.Error("MarkPrivate after the header was sent did not panic — a public header is already on the wire")
		}
	})
	t.Run("twice is fine", func(t *testing.T) {
		h := httpx.Cache(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			httpx.MarkPrivate(r)
			w.WriteHeader(http.StatusOK)
			httpx.MarkPrivate(r)
		}))
		rr := httptest.NewRecorder()
		h.ServeHTTP(rr, httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/", nil))
		if got := rr.Result().Header.Get("Cache-Control"); got != wantPrivate {
			t.Errorf("Cache-Control = %q, want %q", got, wantPrivate)
		}
	})
}

// The production router, not a hand-built chain: this is where the order is chosen.
func TestProductionRouterAppliesThePolicy(t *testing.T) {
	site := func(r chi.Router) {
		r.Get("/test-page", func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte("test page")) })
		r.Get("/test-boom", func(http.ResponseWriter, *http.Request) { panic("test panic") })
		r.Get("/test-partial", func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"partial":true}`))
			panic("after the body began")
		})
	}
	r := httpx.NewRouterWithSite(httpx.Build{Version: "dev", Commit: "none", Env: "test"}, site, nil)

	for _, tc := range []struct {
		method, path string
		status       int
		want         string
	}{
		{http.MethodGet, "/test-page", 200, wantPage},
		{http.MethodHead, "/test-page", 200, wantPage},
		{http.MethodGet, "/health", 200, wantNoStore},
		{http.MethodHead, "/health", 200, wantNoStore},
		{http.MethodGet, "/no-such-page", 404, wantMissing},
		{http.MethodGet, "/v1/no-such-thing", 404, wantMissing},
		{http.MethodPost, "/test-page", 405, wantNoStore},
		// ⭐ Recover writes this 500. It is no-store only because Cache sits OUTSIDE Recover.
		{http.MethodGet, "/test-boom", 500, wantNoStore},
	} {
		rr := httptest.NewRecorder()
		r.ServeHTTP(rr, httptest.NewRequestWithContext(context.Background(), tc.method, tc.path, nil))
		if rr.Code != tc.status {
			t.Errorf("%s %s: status = %d, want %d", tc.method, tc.path, rr.Code, tc.status)
		}
		if got := rr.Result().Header.Get("Cache-Control"); got != tc.want {
			t.Errorf("%s %s: Cache-Control = %q, want %q", tc.method, tc.path, got, tc.want)
		}
	}

	// Recover asks its writer whether the response began. The Cache writer sits between Log's and
	// Recover, so it must answer — or a JSON error is appended to a body already on the wire.
	rr := httptest.NewRecorder()
	r.ServeHTTP(rr, httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/test-partial", nil))
	if body := rr.Body.String(); body != `{"partial":true}` {
		t.Errorf("a panic after the body began changed the body to %q — Recover cannot see through the Cache writer", body)
	}
}

// Wrapping must not remove Flush from anything downstream.
func TestCacheWriterPreservesFlush(t *testing.T) {
	var flushed bool
	h := httpx.Cache(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		flushed = http.NewResponseController(w).Flush() == nil
	}))
	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/", nil))
	if !flushed {
		t.Error("Flush is unreachable through the Cache writer — Unwrap is missing or wrong")
	}
}

// ⛔ "No handler sets the header by hand", enforced rather than remembered: no Go file but
// internal/httpx/cache.go may name the header in a string literal. The runtime override above
// already makes a hand-set value harmless; this keeps anyone from believing it does something.
func TestOnlyCacheGoNamesTheHeader(t *testing.T) {
	root, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, "go.mod")); err != nil {
		t.Fatalf("%s is not the module root: %v", root, err)
	}
	if found := cacheHeaderNamers(t, root); len(found) != 0 {
		t.Errorf("Cache-Control is named outside internal/httpx/cache.go — set it through the policy there instead:\n  %s",
			strings.Join(found, "\n  "))
	}
}

// The scan above proves nothing unless it can see an offender, so plant some.
func TestHeaderScanSeesAPlantedHandler(t *testing.T) {
	root := t.TempDir()
	plant := func(rel, src string) {
		p := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(src), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	plant("internal/pages/offender.go", "package pages\n\nimport \"net/http\"\n\n"+
		"func offender(w http.ResponseWriter, _ *http.Request) {\n"+
		"\tw.Header().Set(\"Cache-Control\", \"public, max-age=31536000\")\n}\n")
	plant("internal/items/raw.go", "package items\n\nconst cc = `cache-control`\n")
	plant("internal/items/comment_only.go", "package items\n\n// Mentions \"Cache-Control\" in prose only.\nconst x = 1\n")
	plant("internal/httpx/cache.go", "package httpx\n\nconst v = \"Cache-Control\"\n")
	plant("internal/pages/offender_test.go", "package pages\n\nconst v = \"Cache-Control\"\n")

	found := cacheHeaderNamers(t, root)
	want := []string{"internal/items/raw.go:3", "internal/pages/offender.go:6"}
	if strings.Join(found, ",") != strings.Join(want, ",") {
		t.Errorf("scan found %q, want %q (the two offenders, not the comment, cache.go or a test file)", found, want)
	}
}

// cacheHeaderNamers lists "path:line" for every string literal equal to Cache-Control, in any case,
// in the non-test Go files under root — except internal/httpx/cache.go, which is the policy.
// It parses the Go rather than grepping lines, so prose in a comment is not a finding.
func cacheHeaderNamers(t *testing.T, root string) []string {
	t.Helper()
	var found []string
	fset := token.NewFileSet()
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if strings.HasPrefix(d.Name(), ".") && p != root {
				return filepath.SkipDir
			}
			return nil
		}
		rel, _ := filepath.Rel(root, p)
		rel = filepath.ToSlash(rel)
		if !strings.HasSuffix(rel, ".go") || strings.HasSuffix(rel, "_test.go") || rel == "internal/httpx/cache.go" {
			return nil
		}
		f, err := parser.ParseFile(fset, p, nil, parser.SkipObjectResolution)
		if err != nil {
			return err
		}
		ast.Inspect(f, func(n ast.Node) bool {
			lit, ok := n.(*ast.BasicLit)
			if !ok || lit.Kind != token.STRING {
				return true
			}
			if s, err := strconv.Unquote(lit.Value); err == nil && strings.EqualFold(s, "Cache-Control") {
				found = append(found, rel+":"+strconv.Itoa(fset.Position(lit.Pos()).Line))
			}
			return true
		})
		return nil
	})
	if err != nil {
		t.Fatalf("scanning %s: %v", root, err)
	}
	return found
}
