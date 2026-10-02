package items

import (
	"context"
	"fmt"

	"github.com/pierrehrt/aoc-api/internal/db/sqlcgen"
	"github.com/pierrehrt/aoc-api/internal/httpx"
)

// The rail's counts (AOC-049).
//
// ⭐ A facet value's count is how many items picking it would leave UNDER THE OTHER FILTERS — the
// filter set minus the facet's own — so the number beside a value is the result of clicking it.
// The SQL computes every count from the same per-item flags the list's rows are kept by (the
// `filtered` CTE in items.sql), so a count and the page it promises cannot disagree.
//
// Every key below is the query parameter that facet sets, so a client maps a facet straight onto
// its filter: facets.rarity → ?rarity=, facets.ilvl → ?ilvl_min= / ?ilvl_max=.

// Facets is the rail: one group per filter that has a vocabulary, the price counts and the two
// level spans.
type Facets struct {
	Rarity        FacetGroup `json:"rarity"`
	EquipLocation FacetGroup `json:"equip_location"`
	ArmourWeight  FacetGroup `json:"armour_weight"`
	Class         FacetGroup `json:"class"`
	Currency      FacetGroup `json:"currency"`
	Set           FacetGroup `json:"set"`
	Price         PriceFacet `json:"price"`
	// The level spans the other filters leave: what a range control can usefully ask for. Absent
	// when no item under the other filters has that level at all.
	ItemLevel     *LevelSpan `json:"ilvl,omitempty"`
	RequiresLevel *LevelSpan `json:"reqlvl,omitempty"`
}

// FacetGroup is one facet: EVERY value of its vocabulary, 0 counts included (a hidden value teaches
// nothing; a 0 says why), in the order the rail shows them, and Any — the count with this facet
// unset, which is what ticking none of its values leaves.
type FacetGroup struct {
	Any    int64        `json:"any"`
	Values []FacetValue `json:"values"`
}

// FacetValue is a vocabulary term with its count. The term is the lookup table's row, so a class
// carries its short name and a rarity its colour token, as everywhere else (AOC-046).
type FacetValue struct {
	Term
	Count int64 `json:"count"`
}

// PriceFacet counts the price filter's two choices: Count items have a vendor price on at least
// one source (price=true); Any-Count have none (price=false).
type PriceFacet struct {
	Any   int64 `json:"any"`
	Count int64 `json:"count"`
}

// LevelSpan is the lowest and highest level among the items the other filters leave.
type LevelSpan struct {
	Min int32 `json:"min"`
	Max int32 `json:"max"`
}

// facetParams is the facet queries' arguments, copied from the list query's. ⛔ Every field, and
// only from there: a filter the rows honour and the counts do not is the bug the rail exists to
// make impossible. TestTheFacetQueriesTakeEveryListFilter fails on a field this misses.
func facetParams(p sqlcgen.ListItemsParams) sqlcgen.CountItemFacetsParams {
	return sqlcgen.CountItemFacetsParams{
		ItemType:       p.ItemType,
		NameQuery:      p.NameQuery,
		IDQuery:        p.IDQuery,
		PlaceSlugs:     p.PlaceSlugs,
		Region:         p.Region,
		Tier:           p.Tier,
		Unchained:      p.Unchained,
		Pvp:            p.Pvp,
		Rarities:       p.Rarities,
		EquipLocations: p.EquipLocations,
		ArmourWeights:  p.ArmourWeights,
		Classes:        p.Classes,
		IlvlMin:        p.IlvlMin,
		IlvlMax:        p.IlvlMax,
		ReqlvlMin:      p.ReqlvlMin,
		ReqlvlMax:      p.ReqlvlMax,
		Price:          p.Price,
		Currencies:     p.Currencies,
		Sets:           p.Sets,
	}
}

// facets runs the two facet queries. The totals query takes the SAME struct by conversion: sqlc
// generates its parameters from the same CTE, so the conversion compiles only while the two agree.
func (s *Service) facets(ctx context.Context, p sqlcgen.CountItemFacetsParams) (*Facets, error) {
	rows, err := s.q.CountItemFacets(ctx, p)
	if err != nil {
		return nil, fmt.Errorf("count item facets: %w", err)
	}
	tot, err := s.q.ItemFacetTotals(ctx, sqlcgen.ItemFacetTotalsParams(p))
	if err != nil {
		return nil, fmt.Errorf("item facet totals: %w", err)
	}

	out := &Facets{
		Rarity:        FacetGroup{Any: tot.AnyRarity, Values: []FacetValue{}},
		EquipLocation: FacetGroup{Any: tot.AnyEquipLocation, Values: []FacetValue{}},
		ArmourWeight:  FacetGroup{Any: tot.AnyArmourWeight, Values: []FacetValue{}},
		Class:         FacetGroup{Any: tot.AnyClass, Values: []FacetValue{}},
		Currency:      FacetGroup{Any: tot.AnyCurrency, Values: []FacetValue{}},
		Set:           FacetGroup{Any: tot.AnySet, Values: []FacetValue{}},
		Price:         PriceFacet{Any: tot.AnyPrice, Count: tot.Priced},
	}
	// The facet names the SQL emits are the parameter names.
	groups := map[string]*FacetGroup{
		"rarity": &out.Rarity, "equip_location": &out.EquipLocation, "armour_weight": &out.ArmourWeight,
		"class": &out.Class, "currency": &out.Currency, "set": &out.Set,
	}
	for _, r := range rows {
		g, ok := groups[r.Facet]
		if !ok {
			return nil, fmt.Errorf("count item facets: unknown facet %q", r.Facet)
		}
		g.Values = append(g.Values, FacetValue{
			Term:  Term{Slug: r.Slug, Name: r.Name, ShortName: r.ShortName, ColourToken: r.ColourToken},
			Count: r.Items,
		})
	}
	if tot.IlvlN > 0 {
		out.ItemLevel = &LevelSpan{Min: tot.IlvlLo, Max: tot.IlvlHi}
	}
	if tot.ReqlvlN > 0 {
		out.RequiresLevel = &LevelSpan{Min: tot.ReqlvlLo, Max: tot.ReqlvlHi}
	}
	return out, nil
}

// validRanges rejects a range that can match nothing by construction. "min above max" is a
// malformed request rather than an empty answer: the caller can tell it is wrong without asking the
// database, so it is a 400 on both surfaces (the ticket's edge case), never a silent swap.
func (f Filters) validRanges() error {
	for _, r := range []struct {
		name     string
		min, max *int32
	}{{"ilvl", f.ILvlMin, f.ILvlMax}, {"reqlvl", f.ReqLvlMin, f.ReqLvlMax}} {
		if r.min != nil && *r.min < 0 || r.max != nil && *r.max < 0 {
			return fmt.Errorf("%w: %s bounds cannot be negative", httpx.ErrInvalid, r.name)
		}
		if r.min != nil && r.max != nil && *r.min > *r.max {
			return fmt.Errorf("%w: %s_min (%d) is above %s_max (%d)", httpx.ErrInvalid, r.name, *r.min, r.name, *r.max)
		}
	}
	return nil
}
