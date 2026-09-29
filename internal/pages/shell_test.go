package pages_test

import (
	"net/http"
	"regexp"
	"strings"
	"testing"

	"github.com/pierrehrt/aoc-api/internal/items"
)

// The shell (AOC-046): what every page carries around its content.

// The shell on every page — and NOTHING in it that names a source, a creator or a contact: no
// credit line, no Funcom notice (AOC-055, Pierre 2026-09-29; origins are on the Info page), no
// contact (Pierre, same day). Asserted as absence, so a well-meant line cannot come back quietly.
func TestEveryPageCarriesTheShell(t *testing.T) {
	h := router(t)
	for _, path := range []string{"/", "/_smoke"} {
		body := get(t, h, http.MethodGet, path, nil, "").Body.String()
		for _, want := range []string{
			`<header`, `<nav aria-label="Sections"`, `<main`, `<footer`,
			`href="/"`, // the logo goes home
			"fonts.googleapis.com/css2?family=IBM+Plex+Sans",
		} {
			if !strings.Contains(body, want) {
				t.Errorf("%s: missing %q", path, want)
			}
		}
		for _, banned := range []string{"Kentarii", "AoC&gt;TV", "AoC>TV", "Funcom", "affiliated", "preserved from", "Johar"} {
			if strings.Contains(body, banned) {
				t.Errorf("%s: renders %q — the site names no source and no other creator (DECISIONS.md 2026-09-29)", path, banned)
			}
		}
		if regexp.MustCompile(`(?i)mailto:|contact`).MatchString(body) {
			t.Errorf("%s: the footer carries a contact; none was decided", path)
		}
	}
	if strings.Contains(items.Attribution, "Kentarii") || strings.Contains(items.Attribution, "Funcom") {
		t.Errorf("items.Attribution names another creator: %q", items.Attribution)
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
