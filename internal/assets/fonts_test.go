package assets_test

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"testing/fstest"

	"github.com/pierrehrt/aoc-api/internal/assets"
)

// A font is served as font/woff2 (AOC-075): without the type, a browser may refuse a cross-origin
// preload and some proxies sniff it as text. An unknown hash stays a 404, as for every asset.
func TestAFontIsServedAsWoff2(t *testing.T) {
	fsys := fstest.MapFS{"built/Test-Regular.woff2": &fstest.MapFile{Data: []byte("wOF2 test font")}}
	s, err := assets.LoadFS(fsys, "built")
	if err != nil {
		t.Fatal(err)
	}
	p, err := s.Path("Test-Regular.woff2")
	if err != nil {
		t.Fatal(err)
	}
	rr := httptest.NewRecorder()
	s.Handler().ServeHTTP(rr, httptest.NewRequest(http.MethodGet, p, nil))
	if rr.Code != http.StatusOK || rr.Header().Get("Content-Type") != "font/woff2" {
		t.Errorf("%s: %d %q, want 200 font/woff2", p, rr.Code, rr.Header().Get("Content-Type"))
	}
	rr = httptest.NewRecorder()
	s.Handler().ServeHTTP(rr, httptest.NewRequest(http.MethodGet, assets.Prefix+"Test-Regular.00000000.woff2", nil))
	if rr.Code != http.StatusNotFound {
		t.Errorf("an unknown font hash answered %d, want 404", rr.Code)
	}
}
