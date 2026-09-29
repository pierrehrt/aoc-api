package items

import (
	"context"
	"math/big"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
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
		if got := money(n(in)); got != want {
			t.Errorf("money(%s) = %q, want %q", in, got, want)
		}
	}
	_ = big.NewInt // keep the import honest if pgtype changes shape
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
