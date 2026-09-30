package items

import (
	"context"
	"net/http"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/pierrehrt/aoc-api/internal/db/sqlcgen"
)

// AOC-048: the item page reads Detail. Two fields are added to /v1 (spell_effects, set_pieces) and
// everything else it needs rides Display, which /v1 must never serialise. Fixtures obviously fake.
type setItemQ struct{ *fakeQ }

var setID = int32(7)

func (setItemQ) GetItemBySlug(context.Context, string) (int32, error) { return 1, nil }
func (setItemQ) GetItem(context.Context, int32) (sqlcgen.GetItemRow, error) {
	tok, typ, typName, empty := "rarity-epic", "test-type", "Test Type", ""
	var dps pgtype.Numeric
	_ = dps.Scan("125.80")
	return sqlcgen.GetItemRow{ItemID: 1, Slug: "test-relic-alpha", Name: "Test Relic Alpha", Rarity: "epic",
		RarityName: "Epic", RarityColourToken: &tok, ItemType: &typ, ItemTypeName: &typName, Dps: dps,
		SetID: &setID, SetName: &typName, TooltipImage: &empty, Confidence: "unconfirmed"}, nil
}
func (setItemQ) ListItemClasses(context.Context, int32) ([]sqlcgen.ListItemClassesRow, error) {
	short := "TC"
	return []sqlcgen.ListItemClassesRow{{Slug: "test-class", Name: "Test Class", ShortName: &short}}, nil
}
func (setItemQ) ListItemSpellEffects(context.Context, int32) ([]sqlcgen.ListItemSpellEffectsRow, error) {
	var v pgtype.Numeric
	_ = v.Scan("8.00")
	return []sqlcgen.ListItemSpellEffectsRow{{ItemID: 1, Stat: "Test Drain", Value: v, Sign: -1, Unit: "percent"}}, nil
}
func (setItemQ) ListSetPieces(_ context.Context, id *int32) ([]sqlcgen.ListSetPiecesRow, error) {
	if id == nil || *id != setID {
		return nil, nil
	}
	return []sqlcgen.ListSetPiecesRow{{Slug: "test-relic-alpha", Name: "Test Relic Alpha"}, {Slug: "test-relic-beta", Name: "Test Relic Beta"}}, nil
}

func TestGetFillsThePagesDisplayFromTheSameRows(t *testing.T) {
	d, err := NewService(setItemQ{sharedItem()}).Get(context.Background(), "test-relic-alpha")
	if err != nil {
		t.Fatal(err)
	}
	if d.Display.Rarity.Name != "Epic" || d.Display.Rarity.ColourToken != "rarity-epic" {
		t.Errorf("rarity display = %+v", d.Display.Rarity)
	}
	if d.Display.ItemType == nil || d.Display.ItemType.Name != "Test Type" {
		t.Errorf("item type display = %+v", d.Display.ItemType)
	}
	if len(d.Display.Classes) != 1 || d.Display.Classes[0].ShortName != "TC" || d.Classes[0] != "test-class" {
		t.Errorf("classes: display %+v, contract %v", d.Display.Classes, d.Classes)
	}
	if d.Display.DPS == nil || *d.Display.DPS != "125.8" {
		t.Errorf("DPS = %v, want 125.8", d.Display.DPS)
	}
	// An empty tooltip string is no tooltip — so the page's <img> and its og:image agree.
	if d.TooltipImage != nil {
		t.Errorf("tooltip_image %q survived as a value; an empty string must be nil", *d.TooltipImage)
	}
	if len(d.SetPieces) != 2 || len(d.SpellEffects) != 1 || d.SpellEffects[0].Unit != "percent" {
		t.Errorf("set pieces %v, spell effects %v", d.SetPieces, d.SpellEffects)
	}
}

// ⭐ The contract, read from the JSON body the way a consumer reads it (CLAUDE.md 5c): `set` is still
// a string, the two new fields are arrays, and nothing the page reads beside the contract leaks.
func TestTheDetailContractGainsTwoArraysAndNothingElse(t *testing.T) {
	for name, q := range map[string]Querier{"in a set": setItemQ{sharedItem()}, "in no set": oneItemQ{sharedItem()}} {
		rec, body := get(t, q, "/v1/items/test-relic-alpha")
		if rec.Code != http.StatusOK {
			t.Fatalf("%s: status %d", name, rec.Code)
		}
		for _, k := range []string{"spell_effects", "set_pieces"} {
			if _, ok := body[k].([]any); !ok {
				t.Errorf("%s: %s = %#v, want an array (empty, never null)", name, k, body[k])
			}
		}
		if set, ok := body["set"]; ok {
			if _, isString := set.(string); !isString {
				t.Errorf("%s: set = %#v — retyping it breaks /v1", name, set)
			}
		}
		for _, leaked := range []string{"Display", "display", "dps", "acquisition_type_name", "quest_label", "tier_name"} {
			if _, ok := body[leaked]; ok {
				t.Errorf("%s: /v1 serialises %q, a page-only field", name, leaked)
			}
		}
		if srcs, _ := body["sources"].([]any); len(srcs) > 0 {
			for _, leaked := range []string{"AcquisitionTypeName", "acquisition_type_name", "quest_label", "tier_name"} {
				if _, ok := srcs[0].(map[string]any)[leaked]; ok {
					t.Errorf("%s: a source serialises %q", name, leaked)
				}
			}
		}
	}
}
