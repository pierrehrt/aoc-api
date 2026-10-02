package pages_test

import (
	"net/http"
	"regexp"
	"strings"
	"testing"
)

// Pierre, 2026-10-01: the header carries the design's tabs before their sections exist, each on a
// "Coming Soon" page. Every tab leads to a page that answers; a Coming Soon page is noindex, out of
// the sitemap, and claims nothing about the game.
func TestTheHeaderTabsLeadToComingSoonPages(t *testing.T) {
	h := router(t)
	tabs := regexp.MustCompile(`<a href="([^"]+)"[^>]*>([^<]+)`)
	nav := regexp.MustCompile(`(?s)<nav aria-label="Sections"[^>]*>(.*?)</nav>`).FindStringSubmatch(get(t, h, http.MethodGet, "/armory", nil, "").Body.String())
	if nav == nil {
		t.Fatal("no nav")
	}
	var got []string
	for _, m := range tabs.FindAllStringSubmatch(nav[1], -1) {
		got = append(got, m[2]+" "+m[1])
	}
	want := []string{"Armory /armory", "AA&#39;s /aa", "Feats /feats", "DJ/Raids /dj-raids", "More /more"}
	if strings.Join(got, " | ") != strings.Join(want, " | ") {
		t.Errorf("tabs = %v, want %v", got, want)
	}
	if !strings.Contains(nav[1], `<a href="/armory" aria-current="page"`) {
		t.Error("on /armory the Armory tab is not the current one")
	}

	for path, section := range map[string]string{"/aa": "AA&#39;s", "/feats": "Feats", "/dj-raids": "DJ/Raids", "/more": "More"} {
		rr := get(t, h, http.MethodGet, path, nil, "")
		body := rr.Body.String()
		if rr.Code != http.StatusOK {
			t.Errorf("%s: %d", path, rr.Code)
			continue
		}
		for _, want := range []string{"Coming soon", `<meta name="robots" content="noindex">`,
			`<link rel="canonical" href="https://aoc-codex.app` + path + `">`, `<a href="` + path + `" aria-current="page"`, ">" + section + "<"} {
			if !strings.Contains(body, want) {
				t.Errorf("%s: missing %q", path, want)
			}
		}
	}

	// Out of the sitemap: thin pages must not be offered for indexing.
	sm := get(t, h, http.MethodGet, "/sitemaps/1.xml", nil, "").Body.String()
	if !strings.Contains(sm, "<loc>https://aoc-codex.app/armory</loc>") {
		t.Fatal("the sitemap lost the Armory — the test is not reading what it thinks")
	}
	for _, path := range []string{"/aa", "/feats", "/dj-raids", "/more"} {
		if strings.Contains(sm, "<loc>https://aoc-codex.app"+path+"</loc>") {
			t.Errorf("%s (Coming Soon) is in the sitemap", path)
		}
	}
}
