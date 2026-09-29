//go:build corpus

// A corpus test: compiled only with `-tags corpus` (AOC-044) — see item_read_endpoints_test.go.

package db_test

import (
	"context"
	"testing"

	"github.com/pierrehrt/aoc-api/internal/items"
)

// AOC-047: the three orders and the id search, against the real corpus. Asserted as relations
// between rows, never as frozen names (the corpus may be re-imported).
func TestTheListOrdersHoldOnTheRealCorpus(t *testing.T) {
	s := svc(t)
	ctx := context.Background()

	// ilvl: descending, no NULL before a value, and the last page ends with the NULLs.
	res, err := s.List(ctx, items.Filters{Sort: items.SortILvl, Limit: 200})
	if err != nil {
		t.Fatal(err)
	}
	seenNil := false
	var prev int32 = 1 << 30
	for _, it := range res.Items {
		if it.ItemLevel == nil {
			seenNil = true
			continue
		}
		if seenNil {
			t.Fatalf("an item with a level (%d) came after one without — NULLs must sort last", *it.ItemLevel)
		}
		if *it.ItemLevel > prev {
			t.Fatalf("ilvl order broken: %d after %d", *it.ItemLevel, prev)
		}
		prev = *it.ItemLevel
	}
	last, err := s.List(ctx, items.Filters{Sort: items.SortILvl, Limit: 60, Offset: int(res.Total) - 60})
	if err != nil {
		t.Fatal(err)
	}
	if n := len(last.Items); n == 0 || last.Items[n-1].ItemLevel != nil {
		t.Errorf("the last item in ilvl order still has a level; the 50 without one should end the list")
	}

	// id: ascending.
	res, err = s.List(ctx, items.Filters{Sort: items.SortID, Limit: 100})
	if err != nil {
		t.Fatal(err)
	}
	for i := 1; i < len(res.Items); i++ {
		if res.Items[i].ID <= res.Items[i-1].ID {
			t.Fatalf("id order broken at %d after %d", res.Items[i].ID, res.Items[i-1].ID)
		}
	}

	// name (the default): the same first row as an explicit sort=name.
	a, _ := s.List(ctx, items.Filters{Limit: 3})
	b, _ := s.List(ctx, items.Filters{Sort: items.SortName, Limit: 3})
	if len(a.Items) == 0 || a.Items[0].ID != b.Items[0].ID {
		t.Errorf("default order and sort=name disagree on the first row")
	}
}

func TestAQueryThatIsAnIDFindsThatItem(t *testing.T) {
	s := svc(t)
	ctx := context.Background()
	first, err := s.List(ctx, items.Filters{Sort: items.SortID, Limit: 1})
	if err != nil || len(first.Items) == 0 {
		t.Fatalf("no first item: %v", err)
	}
	id := first.Items[0].ID
	res, err := s.List(ctx, items.Filters{Query: itoa(id)})
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, it := range res.Items {
		if it.ID == id {
			found = true
		}
	}
	if !found {
		t.Errorf("q=%d did not return item %d", id, id)
	}
	// And the row carries what the page shows: a rarity token (where the rarity has one), slots,
	// classes with short names, a price where a vendor sells it — measured over a page.
	page, err := s.List(ctx, items.Filters{Limit: 200})
	if err != nil {
		t.Fatal(err)
	}
	var withSlots, withClasses, withShort, withPrice, withToken int
	for _, it := range page.Items {
		if len(it.EquipLocations) > 0 {
			withSlots++
		}
		if len(it.Classes) > 0 {
			withClasses++
			if it.Classes[0].ShortName != "" {
				withShort++
			}
		}
		if it.Price != nil {
			withPrice++
		}
		if it.RarityColourToken != "" {
			withToken++
		}
	}
	if withSlots == 0 || withClasses == 0 || withPrice == 0 || withToken == 0 {
		t.Errorf("a 200-row page carried slots=%d classes=%d prices=%d tokens=%d — a batch load is not wired", withSlots, withClasses, withPrice, withToken)
	}
	if withShort != withClasses {
		t.Errorf("%d of %d class-restricted rows carry a short name", withShort, withClasses)
	}
}

func itoa(n int32) string {
	if n == 0 {
		return "0"
	}
	var b []byte
	for n > 0 {
		b = append([]byte{byte('0' + n%10)}, b...)
		n /= 10
	}
	return string(b)
}
