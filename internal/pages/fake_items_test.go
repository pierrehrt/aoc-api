package pages_test

import (
	"context"

	"github.com/jackc/pgx/v5"

	"github.com/pierrehrt/aoc-api/internal/db/sqlcgen"
)

// fakeItems is the smallest items.Querier the page tests need: a corpus of obviously fake items
// (CLAUDE.md STEP ZERO), paged and searched the way the real query would, so the page can be
// tested without a database. It never sorts: the ORDER BY is the SQL's job and is pinned by the
// corpus tests; here the rows come back in the order given.
type fakeItems struct {
	rows []sqlcgen.ListItemsRow
}

func newFakeItems(n int) *fakeItems {
	f := &fakeItems{}
	lvl := int32(80)
	tok := "rarity-epic"
	for i := 1; i <= n; i++ {
		row := sqlcgen.ListItemsRow{ItemID: int32(i), Slug: "test-item-" + itoa(i), Name: "Test Item " + itoa(i), Rarity: "epic", RarityColourToken: &tok, Confidence: "unconfirmed"}
		if i%2 == 0 {
			row.ItemLevel = &lvl
		}
		f.rows = append(f.rows, row)
	}
	return f
}

func (f *fakeItems) ListItems(_ context.Context, a sqlcgen.ListItemsParams) ([]sqlcgen.ListItemsRow, error) {
	var match []sqlcgen.ListItemsRow
	for _, r := range f.rows {
		if a.NameQuery != nil && !contains(r.Name, *a.NameQuery) && (a.IDQuery == nil || *a.IDQuery != r.ItemID) {
			continue
		}
		match = append(match, r)
	}
	total := int64(len(match))
	start, end := int(a.PageOffset), int(a.PageOffset)+int(a.PageSize)
	if start > len(match) {
		start = len(match)
	}
	if end > len(match) {
		end = len(match)
	}
	out := make([]sqlcgen.ListItemsRow, 0, end-start)
	for _, r := range match[start:end] {
		r.TotalCount = total
		out = append(out, r)
	}
	return out, nil
}

func (f *fakeItems) ListItemPlaces(context.Context, []int32) ([]sqlcgen.ListItemPlacesRow, error) {
	return nil, nil
}
func (f *fakeItems) GetItemBySlug(context.Context, string) (int32, error) { return 0, pgx.ErrNoRows }
func (f *fakeItems) GetItem(context.Context, int32) (sqlcgen.GetItemRow, error) {
	return sqlcgen.GetItemRow{}, pgx.ErrNoRows
}
func (f *fakeItems) ListItemStats(context.Context, int32) ([]sqlcgen.ListItemStatsRow, error) {
	return nil, nil
}
func (f *fakeItems) ListItemSources(context.Context, int32) ([]sqlcgen.ListItemSourcesRow, error) {
	return nil, nil
}
func (f *fakeItems) ListItemCosts(context.Context, []int64) ([]sqlcgen.ListItemCostsRow, error) {
	return nil, nil
}
func (f *fakeItems) ListItemEquipLocations(context.Context, int32) ([]sqlcgen.EquipLocation, error) {
	return nil, nil
}
func (f *fakeItems) ListItemClasses(context.Context, int32) ([]sqlcgen.ListItemClassesRow, error) {
	return nil, nil
}
func (f *fakeItems) ListItemPageEquipLocations(context.Context, []int32) ([]sqlcgen.ListItemPageEquipLocationsRow, error) {
	return nil, nil
}
func (f *fakeItems) ListItemPageClasses(context.Context, []int32) ([]sqlcgen.ListItemPageClassesRow, error) {
	return nil, nil
}
func (f *fakeItems) ListItemPageCosts(context.Context, []int32) ([]sqlcgen.ListItemPageCostsRow, error) {
	return nil, nil
}
func (f *fakeItems) ItemIDSpan(context.Context) (sqlcgen.ItemIDSpanRow, error) {
	n := int32(len(f.rows))
	return sqlcgen.ItemIDSpanRow{MinID: 1, MaxID: n + 3, Total: int64(n)}, nil
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}

func itoa(n int) string {
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
