package pages_test

import (
	"encoding/json"
	"net/http"
	"regexp"
	"strings"
	"testing"
)

// The tooltip beside the cursor (AOC-074): an item's name, in the list and in a builder slot, carries
// the item's own tooltip image as data-tip, which tip.js shows; an item without one carries none, so
// no card can show for it. Over gearCorpus, with obviously fake URLs (CLAUDE.md STEP ZERO): items 1
// and 3 have a tooltip, item 2 has none.
func tipCorpus() *fakeItems {
	f := gearCorpus()
	for i := range f.rows {
		if id := f.rows[i].ItemID; id == 1 || id == 3 {
			u := "https://img.aoc-codex.app/armory/test_item_" + itoa(int(id)) + ".jpg"
			f.rows[i].TooltipImage = &u
		}
	}
	return f
}

// nameLink is the list's name link of one item: its whole opening tag.
func nameLink(t *testing.T, body, slug string) string {
	t.Helper()
	m := regexp.MustCompile(`<a href="/armory/` + slug + `"[^>]*>`).FindString(body)
	if m == "" {
		t.Fatalf("no name link for %s", slug)
	}
	return m
}

func TestAListNameCarriesItsOwnTooltipAndOnlyItsOwn(t *testing.T) {
	body := get(t, gearRouter(t, tipCorpus()), http.MethodGet, "/armory", nil, "").Body.String()
	if l := nameLink(t, body, "test-item-1"); !strings.Contains(l, ` data-tip="https://img.aoc-codex.app/armory/test_item_1.jpg"`) {
		t.Errorf("item 1's name lacks its tooltip: %s", l)
	}
	if l := nameLink(t, body, "test-item-3"); !strings.Contains(l, ` data-tip="https://img.aoc-codex.app/armory/test_item_3.jpg"`) {
		t.Errorf("item 3's name lacks its tooltip: %s", l)
	}
	if l := nameLink(t, body, "test-item-2"); strings.Contains(l, "data-tip") {
		t.Errorf("item 2 has no tooltip, so its name must carry none: %s", l)
	}
	if n := strings.Count(body, "data-tip="); n != 2 {
		t.Errorf("%d names carry a tooltip, want 2 (items 1 and 3): never a stand-in", n)
	}
}

func TestASlotsItemNameCarriesItsOwnTooltip(t *testing.T) {
	body := get(t, gearRouter(t, tipCorpus()), http.MethodGet, "/armory?gear=test-head:1&gear=test-main:2", nil, "").Body.String()
	if r := slotRow(t, body, "test-head"); !strings.Contains(r, ` data-tip="https://img.aoc-codex.app/armory/test_item_1.jpg">Test Item 1</a>`) {
		t.Errorf("the head slot's item name lacks its tooltip: %s", r)
	}
	if r := slotRow(t, body, "test-main"); strings.Contains(r, "data-tip") {
		t.Errorf("item 2 has no tooltip, so its slot must carry none: %s", r)
	}
}

// The JSON says what the page shows (one service): a slot's item names its tooltip, and an item
// without one leaves the field out. Additive to /v1/builds/compute.
func TestTheBuildsJSONNamesASlotItemsTooltip(t *testing.T) {
	rec := get(t, gearRouter(t, tipCorpus()), http.MethodGet, "/v1/builds/compute?gear=test-head:1&gear=test-main:2", nil, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d", rec.Code)
	}
	var got struct {
		Slots []struct {
			Slot struct{ Slug string } `json:"slot"`
			Item map[string]any        `json:"item"`
		} `json:"slots"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	seen := 0
	for _, s := range got.Slots {
		switch s.Slot.Slug {
		case "test-head":
			seen++
			if s.Item["tooltip_image"] != "https://img.aoc-codex.app/armory/test_item_1.jpg" {
				t.Errorf("head item's tooltip_image = %v", s.Item["tooltip_image"])
			}
		case "test-main":
			seen++
			if _, ok := s.Item["tooltip_image"]; ok {
				t.Errorf("item 2 has no tooltip, so the field must be absent: %v", s.Item)
			}
		}
	}
	if seen != 2 {
		t.Fatalf("saw %d of the two filled slots", seen)
	}
}

// The island is on the page, as gear.js is: a fingerprinted module.
func TestTheArmoryLoadsTheTooltipIsland(t *testing.T) {
	body := get(t, gearRouter(t, tipCorpus()), http.MethodGet, "/armory", nil, "").Body.String()
	if !regexp.MustCompile(`<script type="module" src="/assets/tip\.[0-9a-f]+\.js"></script>`).MatchString(body) {
		t.Error("the Armory does not load tip.js")
	}
}
