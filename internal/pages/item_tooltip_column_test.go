package pages_test

import (
	"net/http"
	"regexp"
	"strings"
	"testing"
)

// AOC-048 verify round 2: THE TOOLTIP'S COLUMN HAS A FIXED WIDTH. TestNothingOnTheItemPageSitsBelowTheTooltip
// pins half of the fix — nothing comes after the image. The other half is that, from lg up, the image's
// column does not take its width from the image: the image's size is not in the data, so a column
// sized by its content ("auto", "min-content", "max-content", "fit-content(…)", a fraction) starts
// ~2 px wide and widens when the image arrives, narrowing the text column and rewrapping it.
// Measured with the tooltip served 3 s late and that one column set to `auto`: layout shift 0.27 at
// 1024 px and 0.10 at 1440, the Sources heading moving 24–32 px — against ≤ 0.0002 with the fixed
// column. The track must be a length, and the embedded CSS must carry the rule for it.
func TestTheItemPageTooltipColumnDoesNotTakeItsWidthFromTheImage(t *testing.T) {
	h := router(t)
	body := get(t, h, http.MethodGet, "/armory/test-item-1", nil, "").Body.String()

	class := regexp.MustCompile(`<article class="([^"]*)"`).FindStringSubmatch(body)
	if class == nil {
		t.Fatal("no <article class=…> on the item page")
	}
	fields := strings.Fields(class[1])
	var track string
	grid := false
	for _, c := range fields {
		if c == "lg:grid" {
			grid = true
		}
		if m := regexp.MustCompile(`^lg:grid-cols-\[minmax\(0,1fr\)_([^\]]+)\]$`).FindStringSubmatch(c); m != nil {
			track = m[1]
		}
	}
	if !grid || track == "" {
		t.Fatalf("the article is not a two-column grid from lg up (text, then a tooltip column): class=%q", class[1])
	}
	if !regexp.MustCompile(`^[0-9]+(\.[0-9]+)?(rem|px)$`).MatchString(track) {
		t.Fatalf("the tooltip column is %q — it must be a fixed length, or the image's late arrival resizes the text column", track)
	}

	// The class must be real: the embedded stylesheet carries its rule (a class with no rule is a
	// one-column page that only looks pinned).
	href := regexp.MustCompile(`<link rel="stylesheet" href="(/assets/app\.[^"]+\.css)">`).FindStringSubmatch(body)
	if href == nil {
		t.Fatal("no app stylesheet linked from the item page")
	}
	css := get(t, h, http.MethodGet, href[1], nil, "")
	if css.Code != http.StatusOK {
		t.Fatalf("GET %s: %d", href[1], css.Code)
	}
	if rule := "grid-template-columns:minmax(0,1fr) " + track; !strings.Contains(css.Body.String(), rule) {
		t.Errorf("the embedded CSS has no %q — rebuild the assets (make assets)", rule)
	}
}
