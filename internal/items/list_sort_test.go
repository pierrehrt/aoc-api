package items

import (
	"context"
	"errors"
	"math"
	"math/big"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/pierrehrt/aoc-api/internal/httpx"
)

// AOC-047: the list's sort and id search, at the service and the parser.

func TestSortKeysAreValidatedOnBothSurfaces(t *testing.T) {
	for _, ok := range []string{"", "name", "ilvl", "id"} {
		if !validSort(ok) {
			t.Errorf("%q should be a valid sort", ok)
		}
	}
	for _, bad := range []string{"ILVL", "level", "name asc", "drop table"} {
		if validSort(bad) {
			t.Errorf("%q should not be a valid sort", bad)
		}
		// The service refuses it even if a caller bypasses the parser.
		if _, err := NewService(sharedItem()).List(context.Background(), Filters{Sort: bad}); err == nil {
			t.Errorf("List accepted sort=%q", bad)
		}
		// And the parser answers 400.
		if rec, _ := get(t, sharedItem(), "/v1/items?sort="+url.QueryEscape(bad)); rec.Code != http.StatusBadRequest {
			t.Errorf("sort=%q -> %d, want 400", bad, rec.Code)
		}
	}
	// The key reaches SQL as given, and the default is the name order (unchanged since 0.1.0).
	q := sharedItem()
	_, _ = NewService(q).List(context.Background(), Filters{Sort: SortILvl})
	if got := q.firstArgs().SortBy; got != "ilvl" {
		t.Errorf("SortBy reached SQL as %q", got)
	}
	q = sharedItem()
	_, _ = NewService(q).List(context.Background(), Filters{})
	if got := q.firstArgs().SortBy; got != "" {
		t.Errorf("default SortBy reached SQL as %q, want \"\" (name order in SQL)", got)
	}
}

func TestAWholeNumberQueryAlsoMatchesTheItemID(t *testing.T) {
	for q, want := range map[string]int32{"2183": 2183, "1": 1, "007": 7, "": 0, "21x": 0, "-3": 0, "0": 0, "9999999999": 0, "Skyshear": 0} {
		got := idQuery(q)
		switch {
		case want == 0 && got != nil:
			t.Errorf("idQuery(%q) = %d, want none", q, *got)
		case want != 0 && (got == nil || *got != want):
			t.Errorf("idQuery(%q) = %v, want %d", q, got, want)
		}
	}
	// Both predicates reach SQL: the name query (escaped) AND the id, so "2183" finds the item
	// whose id is 2183 as well as any item whose name contains it.
	fq := sharedItem()
	_, _ = NewService(fq).List(context.Background(), Filters{Query: "2183"})
	a := fq.firstArgs()
	if a.NameQuery == nil || *a.NameQuery != "2183" || a.IDQuery == nil || *a.IDQuery != 2183 {
		t.Errorf("q=2183 reached SQL as name=%v id=%v", a.NameQuery, a.IDQuery)
	}
}

func TestMoneyRendersLikeATooltip(t *testing.T) {
	n := func(s string) pgtype.Numeric {
		var v pgtype.Numeric
		if err := v.Scan(s); err != nil {
			t.Fatal(err)
		}
		return v
	}
	for in, want := range map[string]string{"9.00": "9", "2.50": "2.5", "100": "100", "0.10": "0.1"} {
		// Through the path a price takes: NUMERIC -> numeric -> TrimNumber (Price and the stat lines).
		if got := TrimNumber(numeric(n(in))); got != want {
			t.Errorf("TrimNumber(%s) = %q, want %q", in, got, want)
		}
	}
	_ = big.NewInt // keep the import honest if pgtype changes shape
}

func TestDPSReadsLikeTheTooltip(t *testing.T) {
	for in, want := range map[string]string{"143.00": "143.0", "125.80": "125.8", "": ""} {
		if got := dpsText(in); got != want {
			t.Errorf("dpsText(%q) = %q, want %q", in, got, want)
		}
	}
}

// Price is the ONE spelling of a cost, for the list row and the item page alike (AOC-048 review).
func TestPriceIsOneFormatForBothPages(t *testing.T) {
	if got, want := Price([]CostRef{{CurrencyName: "Test Relic", Amount: "9.00"}, {CurrencyName: "Test Gold", Amount: "2.50"}}), "9 Test Relic + 2.5 Test Gold"; got != want {
		t.Errorf("Price = %q, want %q", got, want)
	}
	if got := Price(nil); got != "" {
		t.Errorf("no costs gave %q", got)
	}
}

func TestIDSpanCountsTheAbsentIDs(t *testing.T) {
	span, err := NewService(sharedItem()).IDSpan(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	// fakeQ: min 1, max 10, total 8 → 2 absent.
	if span.MinID != 1 || span.MaxID != 10 || span.Total != 8 || span.Absent != 2 {
		t.Errorf("span = %+v", span)
	}
	_ = httptest.NewRecorder
}

// Slugs refuses a page it cannot pass to Postgres as it was asked — a negative or oversized offset
// would otherwise wrap in int32 and come back as a 500 (AOC-025 review).
func TestSlugsRefusesAPageItCannotAsk(t *testing.T) {
	s := NewService(sharedItem())
	for _, tc := range []struct{ limit, offset int }{{0, 0}, {-1, 0}, {10, -1}, {10, math.MaxInt32 + 1}, {math.MaxInt32 + 1, 0}} {
		if _, err := s.Slugs(context.Background(), tc.limit, tc.offset); !errors.Is(err, httpx.ErrInvalid) {
			t.Errorf("Slugs(%d, %d) = %v, want ErrInvalid", tc.limit, tc.offset, err)
		}
	}
	if _, err := s.Slugs(context.Background(), 10, 0); err != nil {
		t.Errorf("a valid page failed: %v", err)
	}
}
