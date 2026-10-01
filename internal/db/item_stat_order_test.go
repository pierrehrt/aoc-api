package db_test

import (
	"context"
	"testing"

	"github.com/pierrehrt/aoc-api/internal/db/sqlcgen"
	"github.com/pierrehrt/aoc-api/internal/items"
)

// AOC-048: an item's stats and effects come back in the TOOLTIP's order — the order the importer
// wrote them — not alphabetically. The fixture's blade lists "Test Strength" before "Test Regen",
// which alphabetical order would reverse.
func TestStatsComeBackInTheTooltipsOrder(t *testing.T) {
	pool, _ := importTarget(t)
	if r := runImport(t, pool, fixtureJSON); r.err != nil {
		t.Fatalf("import: %v", r.err)
	}
	d, err := items.NewService(sqlcgen.New(pool)).Get(context.Background(), "test-blade-alpha")
	if err != nil {
		t.Fatal(err)
	}
	if len(d.Stats) != 2 || d.Stats[0].Stat != "Test Strength" || d.Stats[1].Stat != "Test Regen" {
		t.Errorf("stats = %+v, want Test Strength then Test Regen, as the snapshot lists them", d.Stats)
	}
	if len(d.SpellEffects) != 1 || d.SpellEffects[0].Stat != "Test Sprint Drain" {
		t.Errorf("spell effects = %+v", d.SpellEffects)
	}
	if len(d.SetPieces) != 1 || d.SetPieces[0].Slug != "test-blade-alpha" {
		t.Errorf("set pieces = %+v, want the blade itself (its set holds only it here)", d.SetPieces)
	}
}
