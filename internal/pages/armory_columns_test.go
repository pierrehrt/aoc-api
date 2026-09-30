package pages_test

import (
	"html"
	"net/http"
	"regexp"
	"strings"
	"testing"

	"github.com/pierrehrt/aoc-api/internal/assets"
	"github.com/pierrehrt/aoc-api/internal/db/sqlcgen"
	"github.com/pierrehrt/aoc-api/internal/httpx"
	"github.com/pierrehrt/aoc-api/internal/items"
	"github.com/pierrehrt/aoc-api/internal/pages"
	"github.com/pierrehrt/aoc-api/internal/templates"
)

// AOC-062 (Pierre: "Slot / type should be in two different column"). Five obviously fake items, one
// per shape the list meets — the taxonomy VALUES are real (they must look like rows), the items are not.
func columnsFake() *fakeItems {
	s := func(v string) *string { return &v }
	return &fakeItems{
		rows: []sqlcgen.ListItemsRow{
			{ItemID: 1, Slug: "test-cuffs-alpha", Name: "Test Cuffs Alpha", Rarity: "epic", ItemType: s("head"), ItemTypeName: s("Head"), ArmourWeight: s("light"), ArmourWeightName: s("Light"), Confidence: "unconfirmed"},
			{ItemID: 2, Slug: "test-bolt-beta", Name: "Test Bolt Beta", Rarity: "epic", ItemType: s("crossbow"), ItemTypeName: s("Crossbow"), Confidence: "unconfirmed"},
			{ItemID: 3, Slug: "test-band-gamma", Name: "Test Band Gamma", Rarity: "epic", ItemType: s("ring"), ItemTypeName: s("Ring"), Confidence: "unconfirmed"},
			{ItemID: 4, Slug: "test-chain-delta", Name: "Test Chain Delta", Rarity: "epic", ItemType: s("necklace"), ItemTypeName: s("Necklace"), Confidence: "unconfirmed"},
			{ItemID: 5, Slug: "test-oddment-epsilon", Name: "Test Oddment Epsilon", Rarity: "epic", Confidence: "unconfirmed"},
		},
		slots: []sqlcgen.ListItemPageEquipLocationsRow{
			{ItemID: 1, Slug: "head", Name: "Head"},
			{ItemID: 2, Slug: "main-hand", Name: "Main Hand"},
			{ItemID: 3, Slug: "left-finger", Name: "Left Finger"}, {ItemID: 3, Slug: "right-finger", Name: "Right Finger"},
			{ItemID: 4, Slug: "necklace", Name: "Necklace"},
		},
	}
}

var (
	rowRE   = regexp.MustCompile(`(?s)<tr class="border-b border-line align-top">(.*?)</tr>`)
	nameRE  = regexp.MustCompile(`>(Test [A-Za-z]+ [A-Za-z]+)</a>`)
	cellsRE = regexp.MustCompile(`(?s)<td class="hidden py-2 pr-3 font-mono text-xs md:table-cell">(.*?)</td>`)
	phoneRE = regexp.MustCompile(`(?s)<div class="mt-0\.5 font-mono text-xs text-muted md:hidden">\s*(.*?)\s*</div>`)
	tagsRE  = regexp.MustCompile(`<[^>]+>`)
)

// siteOver is router(t) over a given fake. Local to this file on purpose: AOC-025's branch reshapes
// router(t) into routerWith, and the two merge into one helper when both land.
func siteOver(t *testing.T, q *fakeItems) http.Handler {
	t.Helper()
	set, err := assets.Load()
	if err != nil {
		t.Fatal(err)
	}
	tpl, err := templates.New(set)
	if err != nil {
		t.Fatal(err)
	}
	h := pages.New(tpl, set, base, items.NewService(q))
	return httpx.NewRouterWithSite(httpx.Build{Version: "t", Commit: "t", Env: "test"}, h.Routes, set.Handler())
}

func text(s string) string {
	return strings.TrimSpace(html.UnescapeString(tagsRE.ReplaceAllString(s, "")))
}

func TestTheListHasASlotColumnAndATypeColumn(t *testing.T) {
	body := get(t, siteOver(t, columnsFake()), http.MethodGet, "/armory?sort=id", nil, "").Body.String()
	if !strings.Contains(body, `md:table-cell">Slot</th>`) || !strings.Contains(body, `md:table-cell">Type</th>`) {
		t.Fatal("the table has no separate Slot and Type headers")
	}
	if strings.Contains(body, "Slot / type") {
		t.Error("the joined Slot / type column is still there")
	}
	// Desktop: Slot, Type as the tooltip's own line reads them. Phone: one line, never a slug.
	want := map[string]struct{ slot, typ, phone string }{
		"Test Cuffs Alpha":     {"Head", "Light", "Head · Light"},                 // armour: the weight
		"Test Bolt Beta":       {"Main Hand", "Crossbow", "Main Hand · Crossbow"}, // a weapon: the type's NAME
		"Test Band Gamma":      {"Left Finger, Right Finger", "Ring", "Left Finger, Right Finger · Ring"},
		"Test Chain Delta":     {"Necklace", "Necklace", "Necklace"}, // two facts on desktop; the phone line does not repeat itself
		"Test Oddment Epsilon": {"—", "—", ""},
	}
	rows := rowRE.FindAllStringSubmatch(body, -1)
	if len(rows) != len(want) {
		t.Fatalf("%d rows, want %d", len(rows), len(want))
	}
	for _, r := range rows {
		n := nameRE.FindStringSubmatch(r[1])
		if n == nil {
			t.Fatalf("a row without a name: %.200s", r[1])
		}
		w := want[n[1]]
		cells := cellsRE.FindAllStringSubmatch(r[1], -1)
		if len(cells) < 2 {
			t.Fatalf("%s: %d desktop cells", n[1], len(cells))
		}
		if got := text(cells[0][1]); got != w.slot {
			t.Errorf("%s: Slot %q, want %q", n[1], got, w.slot)
		}
		if got := text(cells[1][1]); got != w.typ {
			t.Errorf("%s: Type %q, want %q", n[1], got, w.typ)
		}
		p := phoneRE.FindStringSubmatch(r[1])
		if p == nil || text(p[1]) != w.phone {
			t.Errorf("%s: phone line %v, want %q", n[1], p, w.phone)
		}
	}
	// A slug is never printed as a type (the list printed "crossbow" before AOC-062).
	for _, slug := range []string{">crossbow<", ">ring<", ">necklace<", " crossbow<", " ring<"} {
		if strings.Contains(body, slug) {
			t.Errorf("a type slug %q reached the page", slug)
		}
	}
}
