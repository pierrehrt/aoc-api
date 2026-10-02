package pages_test

import (
	"net/http"
	"testing"
)

// The history rule reads HX-Current-URL with the request's own parser and defaults (canonicalOf), so
// the address bar's spelling of a state does not matter, and an address that names no state, or
// another one, is never taken for the current state. AOC-065 delta verify 7 measured each of these
// on the real corpus: the same state under another spelling replaces the entry; a different state, an
// address a direct load rejects or does not serve, or one that does not parse at all, pushes — and
// none of them panics or errs.
func TestTheHistoryRuleReadsTheCurrentAddressAsAState(t *testing.T) {
	h := router(t)
	const site = "https://aoc-codex.app"
	for _, c := range []struct{ req, cur, push, replace string }{
		// the same state, spelled another way: replace
		{"/armory?rarity=epic", site + "/armory?rarity=epic&p=01", "", "/armory?rarity=epic"},
		{"/armory?rarity=epic", site + "/armory?rarity=epic&limit=7&offset=100&facets=false", "", "/armory?rarity=epic"},
		{"/armory?rarity=epic", site + "/armory?rarity=epic&foo=bar", "", "/armory?rarity=epic"},
		{"/armory?rarity=epic", site + "/armory?rarity=epic,epic&rarity=", "", "/armory?rarity=epic"},
		{"/armory?rarity=epic", site + "/armory?rarity=epic#top", "", "/armory?rarity=epic"},
		{"/armory?rarity=epic", site + "/armory?rarity=epic&sort=ilvl&sort=name", "", "/armory?rarity=epic"}, // the first sort, as a direct load reads it
		{"/armory?q=&rarity=epic&ilvl_min=&ilvl_max=", site + "/armory?q=&rarity=epic&ilvl_min=&ilvl_max=", "", "/armory?rarity=epic"},
		{"/armory?q=%C3%A9", site + "/armory?q=é", "", "/armory?q=%C3%A9"},
		{"/armory?q=a+b", site + "/armory?q=a%20b", "", "/armory?q=a+b"},
		{"/armory?ilvl_min=060", site + "/armory?ilvl_min=60", "", "/armory?ilvl_min=60"},
		{"/armory?pvp=1", site + "/armory?pvp=true", "", "/armory?pvp=true"},
		// a different state: push
		{"/armory?q=a%2Bb", site + "/armory?q=a+b", "/armory?q=a%2Bb", ""}, // "a+b" is not "a b"
		{"/armory?rarity=epic", site + "/armory?rarity=Epic", "/armory?rarity=epic", ""},
		{"/armory?rarity=epic", site + "/armory?rarity=epic;q=x", "/armory?rarity=epic", ""}, // not a parameter: names the whole armory
		{"/armory?rarity=epic", site + "/armory?rarity=%zz", "/armory?rarity=epic", ""},
		// an address a direct load rejects (400) or does not serve (404): push
		{"/armory?rarity=epic", site + "/armory?rarity=epic&p=0", "/armory?rarity=epic", ""},
		{"/armory?rarity=epic", site + "/armory?rarity=epic&p=+1", "/armory?rarity=epic", ""},
		{"/armory?rarity=epic", site + "/armory?rarity=epic&p=99999999999999999999", "/armory?rarity=epic", ""},
		{"/armory?rarity=epic", site + "/armory/?rarity=epic", "/armory?rarity=epic", ""},
		{"/armory?rarity=epic", site + "/ARMORY?rarity=epic", "/armory?rarity=epic", ""},
		// an address that does not parse: push, no panic
		{"/armory?rarity=epic", "%%%", "/armory?rarity=epic", ""},
		{"/armory?rarity=epic", "http://aoc codex/armory?rarity=epic", "/armory?rarity=epic", ""},
		{"/armory?rarity=epic", " ", "/armory?rarity=epic", ""},
	} {
		rr := get(t, h, http.MethodGet, c.req, map[string]string{"HX-Request": "true", "HX-Current-URL": c.cur}, "")
		if rr.Code != http.StatusOK {
			t.Errorf("%s from %q: status %d, want 200", c.req, c.cur, rr.Code)
			continue
		}
		if got, want := [2]string{rr.Header().Get("HX-Push-Url"), rr.Header().Get("HX-Replace-Url")}, [2]string{c.push, c.replace}; got != want {
			t.Errorf("%s from %q: push, replace = %q, want %q", c.req, c.cur, got, want)
		}
	}
}
