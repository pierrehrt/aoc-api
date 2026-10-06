package pages_test

import (
	"bytes"
	"net/http"
	"regexp"
	"strings"
	"testing"

	"github.com/pierrehrt/aoc-api/internal/templates"
)

// ⭐ THE CRITERION (AOC-075): the first paint waits on no third party. The shell names no font host,
// declares every face once, and each face's file is one of OUR assets, served as font/woff2. Over the
// real embedded assets (gearRouter loads them), so a face whose file was never built fails here too.
func TestTheFontsAreOursAndEveryFaceIsServed(t *testing.T) {
	h := gearRouter(t, gearCorpus())
	for _, page := range []string{"/armory", "/"} {
		body := get(t, h, http.MethodGet, page, nil, "").Body.String()
		for _, host := range []string{"fonts.googleapis.com", "fonts.gstatic.com"} {
			if strings.Contains(body, host) {
				t.Errorf("%s still names %s: a third party on the first paint", page, host)
			}
		}
		if n := strings.Count(body, "@font-face{"); n != len(templates.FontFaces) {
			t.Errorf("%s declares %d faces, want %d", page, n, len(templates.FontFaces))
		}
	}
	body := get(t, h, http.MethodGet, "/armory", nil, "").Body.String()
	urls := regexp.MustCompile(`src:url\((/assets/[^)]+\.woff2)\) format\("woff2"\)`).FindAllStringSubmatch(body, -1)
	if len(urls) != len(templates.FontFaces) {
		t.Fatalf("%d font URLs, want %d", len(urls), len(templates.FontFaces))
	}
	for _, m := range urls {
		rr := get(t, h, http.MethodGet, m[1], nil, "")
		if rr.Code != http.StatusOK || rr.Header().Get("Content-Type") != "font/woff2" || !bytes.HasPrefix(rr.Body.Bytes(), []byte("wOF2")) {
			t.Errorf("%s: %d %q, want 200 font/woff2 and a woff2 body", m[1], rr.Code, rr.Header().Get("Content-Type"))
		}
	}
}
