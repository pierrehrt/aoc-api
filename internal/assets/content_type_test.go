package assets_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"testing/fstest"

	"github.com/pierrehrt/aoc-api/internal/assets"
)

// Every Content-Type arm of the asset handler, pinned (AOC-028). Each body is plain text on
// purpose: without its arm, the type would be sniffed from the bytes, and `.svg` in particular
// sniffs to text/plain, which no browser renders as an image (measured, AOC-024 verify round 2).
func TestEveryAssetKindIsServedWithItsOwnType(t *testing.T) {
	cases := []struct{ file, want string }{
		{"app.css", "text/css; charset=utf-8"},
		{"island.js", "text/javascript; charset=utf-8"},
		{"card.png", "image/png"},
		{"icon.svg", "image/svg+xml"},
		{"Test-Regular.woff2", "font/woff2"},
	}
	fsys := fstest.MapFS{}
	for _, c := range cases {
		fsys["built/"+c.file] = &fstest.MapFile{Data: []byte("plain text, not a " + c.file)}
	}
	s, err := assets.LoadFS(fsys, "built")
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range cases {
		t.Run(c.file, func(t *testing.T) {
			p, err := s.Path(c.file)
			if err != nil {
				t.Fatal(err)
			}
			rr := httptest.NewRecorder()
			s.Handler().ServeHTTP(rr, httptest.NewRequestWithContext(context.Background(), http.MethodGet, p, nil))
			if rr.Code != http.StatusOK || rr.Header().Get("Content-Type") != c.want {
				t.Errorf("%s: %d %q, want 200 %q", p, rr.Code, rr.Header().Get("Content-Type"), c.want)
			}
		})
	}
}
