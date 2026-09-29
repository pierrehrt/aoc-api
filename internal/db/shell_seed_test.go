package db_test

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/pierrehrt/aoc-api/internal/db/sqlcgen"
	"github.com/pierrehrt/aoc-api/internal/items"
)

// AOC-046: a rarity's colour and a class's short name are DATA. The migration seeds both; the
// taxonomy endpoint carries both; a template reads them and never names a rarity or a class itself.
func TestRarityColoursAndClassShortNamesAreSeededAndServed(t *testing.T) {
	_, url := migratedDB(t)
	pool, err := pgxpool.New(context.Background(), url)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	tx, err := items.NewTaxonomyService(sqlcgen.New(pool)).Taxonomies(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	// Every class has a short name (Pierre, 2026-09-29, Tier A), and none is blank.
	if len(tx.Classes) != 12 {
		t.Fatalf("classes = %d, want 12", len(tx.Classes))
	}
	for _, c := range tx.Classes {
		if c.ShortName == "" {
			t.Errorf("class %s has no short_name", c.Slug)
		}
	}

	// Exactly the three sampled rarities carry a token; the token names its own CSS property.
	withToken := map[string]string{}
	for _, r := range tx.Rarities {
		if r.ColourToken != "" {
			withToken[r.Slug] = r.ColourToken
		}
	}
	if len(withToken) != 3 {
		t.Errorf("rarities with a colour token = %v, want exactly rare, epic, legendary", withToken)
	}
	for slug, tok := range withToken {
		if tok != "rarity-"+slug {
			t.Errorf("rarity %s has token %q, want %q", slug, tok, "rarity-"+slug)
		}
	}
}
