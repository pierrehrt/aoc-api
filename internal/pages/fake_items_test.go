package pages_test

import (
	"context"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

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

// Every list row has a page (AOC-025: every sitemap URL must answer 200). Items 1–3 carry the
// detailed fixtures in fake_item_detail_test.go; the rest are their bare list rows.
func (f *fakeItems) GetItemBySlug(_ context.Context, slug string) (int32, error) {
	for id, d := range fakeDetails {
		if d.row.Slug == slug {
			return id, nil
		}
	}
	for _, r := range f.rows {
		if r.Slug == slug {
			return r.ItemID, nil
		}
	}
	return 0, pgx.ErrNoRows
}
func (f *fakeItems) GetItem(_ context.Context, id int32) (sqlcgen.GetItemRow, error) {
	if d, ok := fakeDetails[id]; ok {
		return d.row, nil
	}
	for _, r := range f.rows {
		if r.ItemID == id {
			return sqlcgen.GetItemRow{ItemID: r.ItemID, Slug: r.Slug, Name: r.Name, Rarity: r.Rarity, RarityName: "Test Rarity", Confidence: r.Confidence}, nil
		}
	}
	return sqlcgen.GetItemRow{}, pgx.ErrNoRows
}
func (f *fakeItems) ListItemStats(_ context.Context, id int32) ([]sqlcgen.ListItemStatsRow, error) {
	return fakeDetails[id].stats, nil
}
func (f *fakeItems) ListItemSpellEffects(_ context.Context, id int32) ([]sqlcgen.ListItemSpellEffectsRow, error) {
	return fakeDetails[id].effects, nil
}
func (f *fakeItems) ListItemSources(_ context.Context, id int32) ([]sqlcgen.ListItemSourcesRow, error) {
	return fakeDetails[id].sources, nil
}
func (f *fakeItems) ListItemCosts(_ context.Context, ids []int64) ([]sqlcgen.ListItemCostsRow, error) {
	var out []sqlcgen.ListItemCostsRow
	for _, d := range fakeDetails {
		for _, c := range d.costs {
			for _, id := range ids {
				if c.ItemSourceID == id {
					out = append(out, c)
				}
			}
		}
	}
	return out, nil
}
func (f *fakeItems) ListItemEquipLocations(_ context.Context, id int32) ([]sqlcgen.EquipLocation, error) {
	return fakeDetails[id].slots, nil
}
func (f *fakeItems) ListItemClasses(_ context.Context, id int32) ([]sqlcgen.ListItemClassesRow, error) {
	return fakeDetails[id].classes, nil
}
func (f *fakeItems) ListItemSlugs(_ context.Context, a sqlcgen.ListItemSlugsParams) ([]string, error) {
	var out []string
	for i := int(a.PageOffset); i < len(f.rows) && len(out) < int(a.PageSize); i++ {
		out = append(out, f.rows[i].Slug)
	}
	return out, nil
}
func (f *fakeItems) ListSetPieces(_ context.Context, setID *int32) ([]sqlcgen.ListSetPiecesRow, error) {
	if setID != nil && *setID == setOmega {
		return fakeSetPieces, nil
	}
	return nil, nil
}
func (f *fakeItems) ListItemPageEquipLocations(context.Context, []int32) ([]sqlcgen.ListItemPageEquipLocationsRow, error) {
	return nil, nil
}
func (f *fakeItems) ListItemPageClasses(context.Context, []int32) ([]sqlcgen.ListItemPageClassesRow, error) {
	return nil, nil
}

// Item 5 is sold by a vendor and drops nowhere (1,378 such items in the corpus): the phone row's
// third line must be the price alone, with no leading separator (verify round 1).
func (f *fakeItems) ListItemPageCosts(_ context.Context, ids []int32) ([]sqlcgen.ListItemPageCostsRow, error) {
	for _, id := range ids {
		if id == 5 {
			var amt pgtype.Numeric
			_ = amt.Scan("3")
			return []sqlcgen.ListItemPageCostsRow{{ItemID: 5, ItemSourceID: 500, CurrencyName: "Test Token", Amount: amt}}, nil
		}
	}
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
