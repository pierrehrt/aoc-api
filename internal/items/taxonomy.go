package items

import (
	"context"
	"fmt"

	"github.com/pierrehrt/aoc-api/internal/db/sqlcgen"
)

// TaxonomyLister is the vocabulary side of the read surface.
//
// ⭐ It exists so no surface ever hardcodes a game taxonomy. Roles, classes, rarities, tiers,
// armour weights and slots come from the database and nowhere else (reference/content-model.md § 0,
// CLAUDE.md build rule) — a literal list of class names in a template or a filter dropdown is the
// exact bug this model exists to prevent, because the game has twelve classes today and the list
// in the code would be the one nobody updates.
type TaxonomyLister interface {
	Taxonomies(ctx context.Context) (Taxonomies, error)
}

// Term is one vocabulary entry. Slug is what a filter takes; Name is what a person reads.
//
// ShortName (classes) and ColourToken (rarities) are the two presentation facts that are DATA
// rather than template literals (AOC-046): the token names a CSS custom property
// (`rarity-epic` → `--color-rarity-epic`) whose value lives in web/src/app.css. Both are empty
// where the row has none, and omitted from the JSON then.
type Term struct {
	Slug        string `json:"slug"`
	Name        string `json:"name"`
	ShortName   string `json:"short_name,omitempty"`
	ColourToken string `json:"colour_token,omitempty"`
}

// Taxonomies is every filter vocabulary in one call, so a page fetches its whole filter UI once.
type Taxonomies struct {
	Rarities      []Term `json:"rarities"`
	ItemTypes     []Term `json:"item_types"`
	EquipLocation []Term `json:"equip_locations"`
	ArmourWeights []Term `json:"armour_weights"`
	Classes       []Term `json:"classes"`
	Tiers         []Term `json:"tiers"`
	Regions       []Term `json:"regions"`
	Places        []Term `json:"places"`
	Currencies    []Term `json:"currencies"`
	Attribution   string `json:"attribution"`
	// The Armory's main categories (AOC-050), in Pierre's order. Additive: a new key.
	SourceTabs []SourceTab `json:"source_tabs"`
}

// TaxonomyService reads the vocabularies from the database.
type TaxonomyService struct{ q TaxonomyQuerier }

func NewTaxonomyService(q TaxonomyQuerier) *TaxonomyService { return &TaxonomyService{q: q} }

// TaxonomyQuerier is the slice of the generated API the vocabularies need.
type TaxonomyQuerier interface {
	ListRarities(ctx context.Context) ([]sqlcgen.Rarity, error)
	ListItemTypes(ctx context.Context) ([]sqlcgen.ListItemTypesRow, error)
	ListEquipLocations(ctx context.Context) ([]sqlcgen.EquipLocation, error)
	ListArmourWeights(ctx context.Context) ([]sqlcgen.ArmourWeight, error)
	ListClasses(ctx context.Context) ([]sqlcgen.ListClassesRow, error)
	ListTiers(ctx context.Context) ([]sqlcgen.Tier, error)
	ListRegions(ctx context.Context) ([]sqlcgen.ListRegionsRow, error)
	ListPlaces(ctx context.Context) ([]sqlcgen.ListPlacesRow, error)
	ListCurrencies(ctx context.Context) ([]sqlcgen.Currency, error)
	ListSourceTabs(ctx context.Context) ([]sqlcgen.ListSourceTabsRow, error)
}

func (t *TaxonomyService) Taxonomies(ctx context.Context) (Taxonomies, error) {
	out := Taxonomies{
		Rarities: []Term{}, ItemTypes: []Term{}, EquipLocation: []Term{},
		ArmourWeights: []Term{}, Classes: []Term{}, Tiers: []Term{},
		Regions: []Term{}, Places: []Term{}, Currencies: []Term{},
		Attribution: Attribution, SourceTabs: []SourceTab{},
	}

	rarities, err := t.q.ListRarities(ctx)
	if err != nil {
		return Taxonomies{}, fmt.Errorf("list rarities: %w", err)
	}
	for _, v := range rarities {
		out.Rarities = append(out.Rarities, Term{Slug: v.Slug, Name: v.Name, ColourToken: deref(v.ColourToken)})
	}

	itemTypes, err := t.q.ListItemTypes(ctx)
	if err != nil {
		return Taxonomies{}, fmt.Errorf("list item types: %w", err)
	}
	for _, v := range itemTypes {
		out.ItemTypes = append(out.ItemTypes, Term{Slug: v.Slug, Name: v.Name})
	}

	locs, err := t.q.ListEquipLocations(ctx)
	if err != nil {
		return Taxonomies{}, fmt.Errorf("list equip locations: %w", err)
	}
	for _, v := range locs {
		out.EquipLocation = append(out.EquipLocation, Term{Slug: v.Slug, Name: v.Name})
	}

	weights, err := t.q.ListArmourWeights(ctx)
	if err != nil {
		return Taxonomies{}, fmt.Errorf("list armour weights: %w", err)
	}
	for _, v := range weights {
		out.ArmourWeights = append(out.ArmourWeights, Term{Slug: v.Slug, Name: v.Name})
	}

	classes, err := t.q.ListClasses(ctx)
	if err != nil {
		return Taxonomies{}, fmt.Errorf("list classes: %w", err)
	}
	for _, v := range classes {
		out.Classes = append(out.Classes, Term{Slug: v.Slug, Name: v.Name, ShortName: deref(v.ShortName)})
	}

	tiers, err := t.q.ListTiers(ctx)
	if err != nil {
		return Taxonomies{}, fmt.Errorf("list tiers: %w", err)
	}
	for _, v := range tiers {
		out.Tiers = append(out.Tiers, Term{Slug: v.Slug, Name: v.Name})
	}

	regions, err := t.q.ListRegions(ctx)
	if err != nil {
		return Taxonomies{}, fmt.Errorf("list regions: %w", err)
	}
	for _, v := range regions {
		out.Regions = append(out.Regions, Term{Slug: v.Slug, Name: v.Name})
	}

	places, err := t.q.ListPlaces(ctx)
	if err != nil {
		return Taxonomies{}, fmt.Errorf("list places: %w", err)
	}
	for _, v := range places {
		out.Places = append(out.Places, Term{Slug: v.Slug, Name: v.Name})
	}

	currencies, err := t.q.ListCurrencies(ctx)
	if err != nil {
		return Taxonomies{}, fmt.Errorf("list currencies: %w", err)
	}
	for _, v := range currencies {
		out.Currencies = append(out.Currencies, Term{Slug: v.Slug, Name: v.Name})
	}

	tabs, err := t.q.ListSourceTabs(ctx)
	if err != nil {
		return Taxonomies{}, fmt.Errorf("list source tabs: %w", err)
	}
	for _, v := range tabs {
		out.SourceTabs = append(out.SourceTabs, SourceTab{Slug: v.Slug, Name: v.Name, LevelsNote: v.LevelsNote, Groups: v.Groups})
	}

	return out, nil
}
