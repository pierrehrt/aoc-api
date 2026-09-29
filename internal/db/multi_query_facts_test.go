package db_test

import (
	"context"
	"testing"
)

// AOC-012, verify round 5. The facts this endpoint publishes from more than one query.
//
// Five defects on this ticket were one shape: two descriptions of a single rule, left free to
// disagree. Four more facts are written in more than one place, and `unchained` is written in
// THREE:
//
//	the list filter   (src.unchained OR coalesce(p.unchained, false))
//	ListItemPlaces    bool_or(src.unchained OR p.unchained)
//	ListItemSources   src.unchained            ← the bare one
//
// Measured over the whole corpus, all three agree: of 449 sources flagged unchained, 438 sit in one
// of the 6 unchained places and 11 have no place at all, and there is NOT ONE source in an
// unchained place whose own flag is false. So the bare expression cannot produce a wrong answer
// today, and this ticket records it as a limit rather than rewriting it (AOC-039).
//
// That agreement is a property of the imported data, not of the SQL — item_sources.unchained comes
// straight from the snapshot (internal/items/import_children.go) while places.unchained is resolved
// separately, so nothing in the queries forces them together. What forces them is the importer,
// and TestAnUnchainedSourceLandsOnTheUnchainedPlace pins that on a fixture.
//
// This pins it on the REAL corpus, so a future snapshot that breaks the agreement fails here
// instead of quietly publishing two answers to one question — the item page saying a drop is not
// from the Unchained version while the list says it is.
func TestTheThreeExpressionsOfUnchainedAgreeOnTheRealCorpus(t *testing.T) {
	pool := readPool(t)
	ctx := context.Background()

	var disagreeing int
	if err := pool.QueryRow(ctx, `
		SELECT count(*) FROM item_sources s JOIN places p ON p.id = s.place_id
		WHERE (s.unchained OR p.unchained) <> s.unchained`).Scan(&disagreeing); err != nil {
		t.Fatal(err)
	}
	if disagreeing != 0 {
		t.Errorf("%d sources where the place is unchained and the source is not: the list would "+
			"call the drop Unchained and the item page would not. Unify the three expressions "+
			"(AOC-039) before this ships", disagreeing)
	}

	// Not a vacuous pass: there must be unchained places with sources in them, or the assertion
	// above proves nothing.
	var unchainedPlaces, sourcesInThem int
	if err := pool.QueryRow(ctx, `
		SELECT (SELECT count(*) FROM places WHERE unchained),
		       (SELECT count(*) FROM item_sources s JOIN places p ON p.id = s.place_id
		        WHERE p.unchained)`).Scan(&unchainedPlaces, &sourcesInThem); err != nil {
		t.Fatal(err)
	}
	if unchainedPlaces == 0 || sourcesInThem == 0 {
		t.Fatalf("%d unchained places holding %d sources — the check above read nothing",
			unchainedPlaces, sourcesInThem)
	}
}

// The other two facts a place row summarises from several source rows.
//
// ListItemPlaces groups by place, so where a place has several sources it must reduce them to one
// answer. It does that two different ways: the boss is BLANKED when it is ambiguous (STEP ZERO — a
// blank beats a guess), while the tier is min(). min() is only honest while a place's sources never
// disagree about the tier, which is true of this corpus and is not guaranteed by anything.
func TestAPlacesSummarisedTierIsNeverAmbiguousInThisCorpus(t *testing.T) {
	pool := readPool(t)
	ctx := context.Background()

	var ambiguous int
	if err := pool.QueryRow(ctx, `
		SELECT count(*) FROM (
			SELECT s.item_id, s.place_id FROM item_sources s
			WHERE s.place_id IS NOT NULL AND s.tier_id IS NOT NULL
			GROUP BY 1, 2 HAVING count(DISTINCT s.tier_id) > 1) x`).Scan(&ambiguous); err != nil {
		t.Fatal(err)
	}
	if ambiguous != 0 {
		t.Errorf("%d (item, place) pairs whose sources disagree about the tier: min() picks one "+
			"of them and states it as fact. Blank it the way the boss is blanked (AOC-039)", ambiguous)
	}
}
