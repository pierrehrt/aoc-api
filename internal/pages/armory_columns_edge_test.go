package pages_test

import (
	"net/http"
	"strings"
	"testing"

	"github.com/pierrehrt/aoc-api/internal/db/sqlcgen"
)

// AOC-062 verify round 1 — two shapes the corpus does not hold today, so only a fake can pin them:
// the test plan's "an item with a weight but no slot", and a row with nothing to say, which must
// render no phone line at all rather than an empty one. Every value is fake, taxonomy included:
// the rules read names from rows and never care which.
func TestAWeightWithNoSlotAndARowWithNothingToSay(t *testing.T) {
	s := func(v string) *string { return &v }
	lvl := int32(80)
	q := &fakeItems{rows: []sqlcgen.ListItemsRow{
		{ItemID: 1, Slug: "test-wrap-zeta", Name: "Test Wrap Zeta", Rarity: "epic", ItemType: s("test-type"), ItemTypeName: s("Test Type"),
			ArmourWeight: s("test-weight"), ArmourWeightName: s("Test Weight"), ItemLevel: &lvl, Confidence: "unconfirmed"},
		{ItemID: 2, Slug: "test-blank-eta", Name: "Test Blank Eta", Rarity: "epic", Confidence: "unconfirmed"},
	}}
	body := get(t, siteOver(t, q), http.MethodGet, "/armory?sort=id", nil, "").Body.String()
	rows := rowRE.FindAllStringSubmatch(body, -1)
	if len(rows) != 2 {
		t.Fatalf("%d rows, want 2", len(rows))
	}
	seen := 0
	for _, r := range rows {
		n := nameRE.FindStringSubmatch(r[1])
		if n == nil {
			t.Fatalf("a row without a name: %.200s", r[1])
		}
		cells := cellsRE.FindAllStringSubmatch(r[1], -1)
		if len(cells) < 2 {
			t.Fatalf("%s: %d desktop cells", n[1], len(cells))
		}
		switch n[1] {
		case "Test Wrap Zeta": // a weight, no slot: the weight is the Type, the phone line starts with it
			seen++
			if got := text(cells[0][1]); got != "—" {
				t.Errorf("Slot %q, want —", got)
			}
			if got := text(cells[1][1]); got != "Test Weight" {
				t.Errorf("Type %q, want the weight's name", got)
			}
			p := phoneRE.FindStringSubmatch(r[1])
			if p == nil || text(p[1]) != "Test Weight · iLvl 80" {
				t.Errorf("phone line %v, want %q", p, "Test Weight · iLvl 80")
			}
		case "Test Blank Eta": // nothing at all: dashes on desktop, and no phone element, not an empty one
			seen++
			if a, b := text(cells[0][1]), text(cells[1][1]); a != "—" || b != "—" {
				t.Errorf("Slot %q, Type %q, want — and —", a, b)
			}
			if strings.Contains(r[1], "md:hidden") {
				t.Errorf("a row with nothing to say renders a phone line element: %.300s", r[1])
			}
		}
	}
	if seen != 2 {
		t.Fatalf("checked %d of the 2 rows", seen)
	}
}
