package pages_test

import (
	"net/http"
	"regexp"
	"strings"
	"testing"
)

// AOC-047 verify round 2. The phone row (design 1b) joins optional parts with " · "; the
// separator belongs BETWEEN two parts and never leads or trails a line. Round 1 caught the price
// line ("· 15 Gold" on a priced item with no place); the same shape exists on the slot line: an
// item with no slot, no armour weight and no type — item 4532 in the corpus, and every even fake
// item here — reads "· iLvl 80". One guard over every phone line, on every state the page offers.
func TestArmoryNoPhoneLineLeadsOrTrailsWithASeparator(t *testing.T) {
	h := router(t)
	line := regexp.MustCompile(`(?s)<div data-line="(?:phone|where)"[^>]*>\s*(.*?)\s*</div>`)
	tags := regexp.MustCompile(`<[^>]+>`)
	for _, path := range []string{"/armory", "/armory?sort=name", "/armory?q=Item+2", "/armory?p=3"} {
		body := get(t, h, http.MethodGet, path, nil, "").Body.String()
		lines := line.FindAllStringSubmatch(body, -1)
		if len(lines) == 0 {
			t.Fatalf("%s: no phone lines found — the row markup moved, update the regexp", path)
		}
		for _, m := range lines {
			text := strings.TrimSpace(tags.ReplaceAllString(m[1], ""))
			if strings.HasPrefix(text, "·") || strings.HasSuffix(text, "·") {
				t.Errorf("%s: a phone line leads or trails with the separator: %q", path, text)
				break
			}
		}
	}
}
