package pages_test

import (
	"encoding/json"
	"html"
	"net/http"
	"regexp"
	"strings"
	"testing"
)

// The item page (AOC-048), against the fake corpus in fake_item_detail_test.go.

func TestItemPageRendersTheWholeItemWithoutJavaScript(t *testing.T) {
	h := router(t)
	rr := get(t, h, http.MethodGet, "/armory/test-item-1", nil, "")
	if rr.Code != http.StatusOK {
		t.Fatalf("status %d", rr.Code)
	}
	// Compared as the reader sees it: html/template writes "+" as &#43;, which a browser shows as "+".
	body := html.UnescapeString(rr.Body.String())
	for _, want := range []string{
		// the name in its rarity's colour, and the chips (short class names, full name on hover)
		`<h1 class="text-2xl font-semibold tracking-tight" style="color: var(--color-rarity-epic)">Test Item 1</h1>`,
		">Epic<", ">Test Type<", ">Head<", ">Light<", `<abbr title="Test Class" class="no-underline">TC</abbr>`,
		"Item Level 80", "Requires Level 78", "Test Binding",
		// stats as text, the way a tooltip prints them, beside the tooltip image
		"234 Armor", "146 Critigation Amount", "+40 Test Strength", "+258 Test Rating (Test Element)",
		">Effects<", "-8% Test Drain",
		`src="https://img.aoc-codex.app/armory/test_item_1.jpg"`,
		// the set: the items that share its name, the other one linked
		"Test Set Omega", `href="/armory/test-item-2"`, `<span aria-current="page" class="text-paper">Test Item 1</span>`,
		// sources, grouped by each row's own acquisition type
		"Sources <span class=\"font-mono\">· 4</span>",
		`<span class="capitalize">drop</span>`, `<span class="capitalize">vendor</span>`, `<span class="capitalize">quest</span>`,
		"Type not recorded",
		"Test Place — Test Boss", "Test Tier", "Test Vendor", "3 Test Token + 2.5 Test Coin", "Test Giver",
		// the row's own flags, which its place's name does not say; and a source with nothing in it
		`<span class="rounded border border-line px-1">raid</span>`, `<span class="rounded border border-line px-1">Unchained</span>`,
		"No place recorded",
		// the head
		"<title>Test Item 1 — AoC Codex</title>",
		`<meta name="description" content="Epic Test Type (Head, Light), item level 80, requires level 78 — an Age of Conan item: stats, where it comes from, its set.">`,
		`<link rel="canonical" href="https://aoc-codex.app/armory/test-item-1">`,
		`<meta property="og:image" content="https://img.aoc-codex.app/armory/test_item_1.jpg">`,
		`<meta name="twitter:card" content="summary">`, // a portrait tooltip: the small card, never cropped to 2:1
	} {
		if !strings.Contains(body, want) {
			t.Errorf("missing %q", want)
		}
	}
	// The columns follow the data: only the vendor group has a cost, only the drop group a tier.
	if n := strings.Count(body, `font-medium md:table-cell">Cost</th>`); n != 1 {
		t.Errorf("%d Cost columns, want exactly 1 (the vendor group's)", n)
	}
	if n := strings.Count(body, `font-medium md:table-cell">Tier</th>`); n != 1 {
		t.Errorf("%d Tier columns, want exactly 1 (the drop group's)", n)
	}
	// The vendor listed twice, identically, is one line — and the count says 4, not 5.
	if n := strings.Count(body, "3 Test Token + 2.5 Test Coin</td>"); n != 1 {
		t.Errorf("the repeated vendor line appears %d times, want once", n)
	}
	// No set size: the data's "declared pieces" is not one (AOC-060).
	if set := body[strings.Index(body, `id="set-h"`):strings.Index(body, `id="src-h"`)]; strings.Contains(set, "piece") {
		t.Error("the set section states a size")
	}
	// The main image is above the fold on desktop: never lazy.
	if strings.Contains(body, `loading="lazy"`) {
		t.Error("the tooltip image is lazy-loaded")
	}
	// No confidence marker, provenance or credit anywhere (Pierre, 2026-09-29) — and no drop rate,
	// chance or stack size: none exists in any source.
	for _, banned := range []string{"unconfirmed", "verified", "Kentarii", "Funcom", "AoC>TV", "drop rate", "chance", "stack"} {
		if strings.Contains(strings.ToLower(body), strings.ToLower(banned)) {
			t.Errorf("renders %q", banned)
		}
	}
}

// The honest states, each on an item that has it: no stat lines, no source, no tooltip.
func TestItemPageSaysPlainlyWhatIsNotRecorded(t *testing.T) {
	h := router(t)
	body := get(t, h, http.MethodGet, "/armory/test-item-2", nil, "").Body.String()
	for _, want := range []string{
		"No stat lines are recorded for this item.", "No source recorded.",
		`href="/armory/test-item-1"`,                               // the rest of the record is unaffected
		`<meta name="twitter:card" content="summary_large_image">`, // no tooltip: the site's wide card
	} {
		if !strings.Contains(body, want) {
			t.Errorf("missing %q", want)
		}
	}
	for _, absent := range []string{"<img", " Armor<", "Type not recorded"} {
		if strings.Contains(body, absent) {
			t.Errorf("renders %q for an item that has none", absent)
		}
	}
	// The snippet names only what the page has — no "stats", no "where it comes from".
	desc := regexp.MustCompile(`<meta name="description" content="([^"]*)"`).FindStringSubmatch(body)
	if desc == nil || strings.Contains(desc[1], "stats") || strings.Contains(desc[1], "where it comes from") {
		t.Errorf("description = %q; it must not promise what the item does not have", desc)
	}
	// An item in no set shows no set section; an item with effects and no stat lines does not say
	// "no stat lines" above the effects it then lists.
	three := html.UnescapeString(get(t, h, http.MethodGet, "/armory/test-item-3", nil, "").Body.String())
	if strings.Contains(three, `id="set-h"`) {
		t.Error("an item in no set renders a set section")
	}
	if strings.Contains(three, "No stat lines") || !strings.Contains(three, "-35 Test Stagger") {
		t.Error("an item with effects but no stats says it has no stat lines, or loses its effects")
	}
}

func TestAnUnknownItemIsAShortLivedHTMLNotFound(t *testing.T) {
	h := router(t)
	rr := get(t, h, http.MethodGet, "/armory/test-no-such-item", nil, "")
	if rr.Code != http.StatusNotFound {
		t.Fatalf("status %d, want 404", rr.Code)
	}
	if ct := rr.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/html") {
		t.Errorf("Content-Type %q, want HTML — this is the page surface", ct)
	}
	if cc := rr.Header().Get("Cache-Control"); !strings.Contains(cc, "s-maxage=60") {
		t.Errorf("Cache-Control %q, want the short 404 policy", cc)
	}
}

// ⭐ The structured data PARSES, and says what the page says. A JSON-LD block that is a quoted
// string, or that a stray `<` could close, is worse than none.
func TestItemPageJSONLDParses(t *testing.T) {
	h := router(t)
	for slug, wantImage := range map[string]string{
		"test-item-1": "https://img.aoc-codex.app/armory/test_item_1.jpg",
		"test-item-2": "", // no tooltip: no image claimed
	} {
		body := get(t, h, http.MethodGet, "/armory/"+slug, nil, "").Body.String()
		m := regexp.MustCompile(`(?s)<script type="application/ld\+json">(.*?)</script>`).FindStringSubmatch(body)
		if m == nil {
			t.Fatalf("%s: no JSON-LD block", slug)
		}
		var ld map[string]any
		if err := json.Unmarshal([]byte(m[1]), &ld); err != nil {
			t.Fatalf("%s: JSON-LD does not parse: %v\n%s", slug, err, m[1])
		}
		for k, want := range map[string]string{
			"@context": "https://schema.org", "@type": "Thing",
			"url": "https://aoc-codex.app/armory/" + slug,
		} {
			if ld[k] != want {
				t.Errorf("%s: %s = %v, want %q", slug, k, ld[k], want)
			}
		}
		if ld["name"] == "" || ld["description"] == "" {
			t.Errorf("%s: name/description empty: %v", slug, ld)
		}
		if img, _ := ld["image"].(string); img != wantImage {
			t.Errorf("%s: image = %q, want %q", slug, img, wantImage)
		}
	}
	// Pages that describe no item carry no block.
	if strings.Contains(get(t, h, http.MethodGet, "/armory", nil, "").Body.String(), "application/ld+json") {
		t.Error("the list carries an item's structured data")
	}
}

// ⛔ The page must not depend on who is asking. It is cached at the edge for an hour; a back link
// built from the Referer would hand one reader's search to everyone. The enhancement is the
// browser's job (item.html), and the server's answer is the same with or without a Referer.
func TestItemPageIsTheSameWhoeverAsks(t *testing.T) {
	h := router(t)
	plain := get(t, h, http.MethodGet, "/armory/test-item-1", nil, "").Body.String()
	from := get(t, h, http.MethodGet, "/armory/test-item-1", map[string]string{"Referer": "https://aoc-codex.app/armory?q=secret"}, "").Body.String()
	if plain != from {
		t.Error("the page's bytes change with the Referer — the edge would serve one reader's list to everyone")
	}
	if !strings.Contains(plain, `<a id="back" href="/armory" class="text-link">`) {
		t.Error("the back link is not the plain /armory a reader without JavaScript can follow")
	}
	if strings.Contains(plain, "secret") {
		t.Error("the Referer's query reached the page")
	}
}

// ⭐ NOTHING SITS BELOW THE TOOLTIP (verify round 1). The image's size is not in the data, so the
// browser cannot reserve its box; with text under it, its late arrival moved Set and Sources down
// 260–460 px (CLS 0.19 on a phone). The figure must be the article's LAST child — after the Set and
// Sources sections — and link to itself at full size, since a wide one is scaled to the column.
func TestNothingOnTheItemPageSitsBelowTheTooltip(t *testing.T) {
	h := router(t)
	body := get(t, h, http.MethodGet, "/armory/test-item-1", nil, "").Body.String()
	fig := strings.Index(body, "<figure")
	if fig < 0 {
		t.Fatal("no tooltip figure")
	}
	for _, id := range []string{`id="stats-h"`, `id="set-h"`, `id="src-h"`} {
		if i := strings.Index(body, id); i < 0 || i > fig {
			t.Errorf("%s comes after the tooltip (or is missing) — its position then depends on the image's height", id)
		}
	}
	end := strings.Index(body[fig:], "</figure>")
	if rest := strings.TrimSpace(body[fig+end+len("</figure>") : strings.Index(body, "</article>")]); rest != "" {
		t.Errorf("content follows the tooltip inside the article: %.120q", rest)
	}
	if !regexp.MustCompile(`<a href="https://img\.aoc-codex\.app/armory/test_item_1\.jpg" class="block"><img src="https://img\.aoc-codex\.app/armory/test_item_1\.jpg"`).MatchString(body) {
		t.Error("the tooltip does not link to itself at full size")
	}
}
