package pages_test

import (
	"net/http"
	"regexp"
	"strings"
	"testing"

	"github.com/pierrehrt/aoc-api/internal/items"
)

// The shell (AOC-046): what every page carries around its content.

// The footer line this site owes on every page carrying Kentarii's data, and the Funcom notice —
// spelled out here rather than imported, so a change to the attribution constant is a change to
// this test too, on purpose.
func TestEveryPageCarriesTheShell(t *testing.T) {
	h := router(t)
	for _, path := range []string{"/", "/_smoke"} {
		body := get(t, h, http.MethodGet, path, nil, "").Body.String()
		for _, want := range []string{
			"Data preserved from AoC&gt;TV by Kentarii", // html/template escapes the >; the browser shows AoC>TV
			"not affiliated with or endorsed by Funcom",
			`<header`, `<nav aria-label="Sections"`, `<footer`,
			`href="/"`, // the logo goes home
			"fonts.googleapis.com/css2?family=IBM+Plex+Sans",
		} {
			if !strings.Contains(body, want) {
				t.Errorf("%s: missing %q", path, want)
			}
		}
		// No contact line, by Pierre's decision (2026-09-29) — nothing that looks like one either.
		if regexp.MustCompile(`(?i)mailto:|contact`).MatchString(body) {
			t.Errorf("%s: the footer carries a contact; none was decided", path)
		}
	}
	if items.Attribution != "Data preserved from AoC>TV by Kentarii" {
		t.Errorf("items.Attribution changed to %q; the footer and this test follow it deliberately", items.Attribution)
	}
}

// ⛔ A nav link to a page that does not exist is a bug. Every nav href, followed through the real
// router, answers 200 — and a nav entry can only be added by adding its route.
func TestEveryNavLinkIsARegisteredRoute(t *testing.T) {
	h := router(t)
	body := get(t, h, http.MethodGet, "/", nil, "").Body.String()
	nav := regexp.MustCompile(`(?s)<nav aria-label="Sections"[^>]*>(.*?)</nav>`).FindStringSubmatch(body)
	if nav == nil {
		t.Fatal("no <nav aria-label=\"Sections\"> in the shell")
	}
	for _, m := range regexp.MustCompile(`href="([^"]+)"`).FindAllStringSubmatch(nav[1], -1) {
		if rr := get(t, h, http.MethodGet, m[1], nil, ""); rr.Code != http.StatusOK {
			t.Errorf("nav link %s answers %d, want 200 — a section is linked before its page exists", m[1], rr.Code)
		}
	}
}
