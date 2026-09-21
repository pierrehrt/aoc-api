package items

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/pierrehrt/aoc-api/internal/db/sqlcgen"
	"github.com/pierrehrt/aoc-api/internal/httpx"
)

// Attribution is carried on every response derived from the armory.
//
// Kentarii's release was unconditional, which is exactly why this is a constant in the service
// rather than a line in a template: an unconditional release is what makes a credit easy to
// quietly drop later (DECISIONS.md, 2026-09-13). The JSON surface and the HTML surface read the
// same constant, so neither can drift from the other.
const Attribution = "Data preserved from AoC>TV by Kentarii"

// Page bounds. limit=0 and limit=10000 are both clamped rather than rejected: a caller that omits
// the parameter and a caller that asks for everything are both making a reasonable request, and
// answering 4,646 rows in one response is the thing the bound exists to prevent.
const (
	DefaultLimit = 50
	MaxLimit     = 200
)

// Querier is the slice of the generated API this package uses. Naming it here rather than taking
// *sqlcgen.Queries is what lets the tests run without a database.
type Querier interface {
	ListItems(ctx context.Context, arg sqlcgen.ListItemsParams) ([]sqlcgen.ListItemsRow, error)
	ListItemPlaces(ctx context.Context, itemIds []int32) ([]sqlcgen.ListItemPlacesRow, error)
	GetItemBySlug(ctx context.Context, slug string) (int32, error)
	GetItem(ctx context.Context, itemID int32) (sqlcgen.GetItemRow, error)
	ListItemStats(ctx context.Context, itemID int32) ([]sqlcgen.ListItemStatsRow, error)
	ListItemSources(ctx context.Context, itemID int32) ([]sqlcgen.ListItemSourcesRow, error)
	ListItemCosts(ctx context.Context, sourceIDs []int64) ([]sqlcgen.ListItemCostsRow, error)
	ListItemEquipLocations(ctx context.Context, itemID int32) ([]sqlcgen.EquipLocation, error)
	ListItemClasses(ctx context.Context, itemID int32) ([]sqlcgen.ListItemClassesRow, error)
}

// Service holds the read logic both surfaces call. The HTML armory page and the /v1 JSON handlers
// call THESE functions, not each other's -- which is the whole reason the layer exists
// (CLAUDE.md rule 5b): a rule implemented in a handler is implemented once out of two.
type Service struct{ q Querier }

func NewService(q Querier) *Service { return &Service{q: q} }

// Filters is a validated list query. Every field is a taxonomy slug the taxonomy endpoint returned
// or a free-text name search -- never a value a client invented for a taxonomy field.
type Filters struct {
	Rarity        string
	ItemType      string
	EquipLocation string
	ArmourWeight  string
	Class         string
	Region        string
	Tier          string
	Query         string

	// Places are the specific places the caller named. One or more of these is what makes the
	// view a PLACE view rather than an aggregate one, and that is what decides collapsing.
	Places []string

	PvP       *bool
	Unchained *bool

	Limit  int
	Offset int
}

// Aggregate reports whether this view is one that CONTAINS several places rather than being one.
//
// ⭐ Pierre's rule, DECISIONS.md 2026-09-13: one dungeon shows its loot as it is; anything that
// contains several dungeons shows each item once. So the mode is a consequence of the filters, not
// a parameter -- there is no flag for a caller to get wrong, which is the point, because a client
// that forgets it renders a visibly broken page (CLAUDE.md rule 5b).
func (f Filters) Aggregate() bool { return len(f.Places) == 0 }

// Place is where an item comes from, as shown next to it in a list.
type PlaceRef struct {
	Slug      string  `json:"slug"`
	Name      string  `json:"name"`
	Region    *string `json:"region,omitempty"`
	Tier      *string `json:"tier,omitempty"`
	Boss      *string `json:"boss,omitempty"`
	Unchained bool    `json:"unchained"`
}

// ListItem is one row of the armory list.
type ListItem struct {
	ID           int32   `json:"id"`
	Slug         string  `json:"slug"`
	Name         string  `json:"name"`
	Rarity       string  `json:"rarity"`
	ItemType     *string `json:"item_type,omitempty"`
	SlotFit      *string `json:"slot_fit,omitempty"`
	ItemLevel    *int32  `json:"item_level,omitempty"`
	RequiresLvl  *int32  `json:"requires_level,omitempty"`
	Armor        *int32  `json:"armor,omitempty"`
	TooltipImage *string `json:"tooltip_image,omitempty"`
	Confidence   string  `json:"confidence"`

	// Place is set only when the caller named specific places: there the item appears once per
	// named place, because that duplication IS the information. In an aggregate view it is nil
	// and Places carries the context instead.
	Place  *PlaceRef  `json:"place,omitempty"`
	Places []PlaceRef `json:"places,omitempty"`
}

// ListResult is the paginated envelope. An empty result is a 200 with an empty array and this
// envelope, never a 404 -- "no items match" is an answer, not a missing resource.
type ListResult struct {
	Items       []ListItem `json:"items"`
	Total       int64      `json:"total"`
	Limit       int        `json:"limit"`
	Offset      int        `json:"offset"`
	Collapsed   bool       `json:"collapsed"`
	Attribution string     `json:"attribution"`
}

func ptrIfSet(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

// List answers the armory list, applying the collapsing rule described on Filters.Aggregate.
func (s *Service) List(ctx context.Context, f Filters) (ListResult, error) {
	limit, offset := f.Limit, f.Offset
	if limit <= 0 {
		limit = DefaultLimit
	}
	if limit > MaxLimit {
		limit = MaxLimit
	}
	if offset < 0 {
		offset = 0
	}

	// ⭐ ONE definition of "no places", not two. Aggregate() decides the collapsing mode; this
	// decides the SQL predicate; and when they disagreed, an EMPTY (non-nil) slice reached Postgres
	// as '{}' rather than NULL, so `p.slug = ANY('{}')` was false for every row: the service
	// reported collapsed=true and returned nothing (verify round 2).
	//
	// Deriving the parameter FROM the predicate is what makes that unrepresentable, rather than a
	// second length check sitting somewhere else waiting to drift from the first.
	//
	// It is reachable from the surface this layer exists for: the HTML armory page builds
	// Filters{Places: selected} from a multi-select, and an empty multi-select is its default
	// state — first paint would have shown zero items.
	var placeSlugs []string
	if !f.Aggregate() {
		placeSlugs = f.Places
	}

	params := sqlcgen.ListItemsParams{
		Rarity:        ptrIfSet(f.Rarity),
		ItemType:      ptrIfSet(f.ItemType),
		NameQuery:     ptrIfSet(escapeLike(f.Query)),
		EquipLocation: ptrIfSet(f.EquipLocation),
		Class:         ptrIfSet(f.Class),
		PlaceSlugs:    placeSlugs,
		ArmourWeight:  ptrIfSet(f.ArmourWeight),
		Region:        ptrIfSet(f.Region),
		Tier:          ptrIfSet(f.Tier),
		Unchained:     f.Unchained,
		Pvp:           f.PvP,
		PageSize:      int32(limit),
		PageOffset:    int32(offset),
	}

	rows, err := s.q.ListItems(ctx, params)
	if err != nil {
		return ListResult{}, fmt.Errorf("list items: %w", err)
	}

	out := ListResult{
		Items:       make([]ListItem, 0, len(rows)),
		Limit:       limit,
		Offset:      offset,
		Collapsed:   f.Aggregate(),
		Attribution: Attribution,
	}
	if len(rows) > 0 {
		out.Total = rows[0].TotalCount
	} else if offset > 0 {
		// The total rides on the rows (count(*) OVER ()), so an EMPTY page carries no total and
		// reported 0 — making "you paged past the end" indistinguishable from "nothing matches",
		// which is the difference a pager is built out of. One extra query, only on that case.
		//
		// Re-running the same filters is deliberate: a second COUNT query would be a second copy
		// of a ten-filter WHERE clause, and two copies drift.
		probe := params
		probe.PageOffset, probe.PageSize = 0, 1
		if first, err := s.q.ListItems(ctx, probe); err != nil {
			return ListResult{}, fmt.Errorf("list items (total probe): %w", err)
		} else if len(first) > 0 {
			out.Total = first[0].TotalCount
		}
	}

	ids := make([]int32, 0, len(rows))
	for _, r := range rows {
		ids = append(ids, r.ItemID)
	}

	// One round trip for the whole page's place context, not one per row.
	byItem := map[int32][]PlaceRef{}
	if len(ids) > 0 {
		pl, err := s.q.ListItemPlaces(ctx, ids)
		if err != nil {
			return ListResult{}, fmt.Errorf("list item places: %w", err)
		}
		for _, p := range pl {
			byItem[p.ItemID] = append(byItem[p.ItemID], PlaceRef{
				Slug: p.PlaceSlug, Name: p.PlaceName,
				Region: ptrIfSet(p.RegionSlug), Tier: ptrIfSet(p.TierSlug), Boss: ptrIfSet(p.BossName),
				Unchained: p.Unchained,
			})
		}
	}

	named := map[string]bool{}
	for _, p := range f.Places {
		named[p] = true
	}

	for _, r := range rows {
		base := ListItem{
			ID: r.ItemID, Slug: r.Slug, Name: r.Name,
			Rarity: r.Rarity, ItemType: r.ItemType, SlotFit: r.SlotFit,
			ItemLevel: r.ItemLevel, RequiresLvl: r.RequiresLevel, Armor: r.Armor,
			TooltipImage: r.TooltipImage, Confidence: r.Confidence,
		}
		places := byItem[r.ItemID]

		if f.Aggregate() {
			// Collapsed: the item appears ONCE, and the places it drops in ride along as context.
			base.Places = places
			out.Items = append(out.Items, base)
			continue
		}

		// Expanded: one row per NAMED place this item is in, so an item shared by two selected
		// dungeons appears under both.
		for _, p := range places {
			if !named[p.Slug] {
				continue
			}
			row := base
			pc := p
			row.Place = &pc
			out.Items = append(out.Items, row)
		}
		// ⛔ NO FALLBACK when nothing matched, deliberately. An earlier version emitted the item
		// once with no place, reasoning that losing a row is worse than showing one without its
		// place. Its only live effect was to HIDE a bug: when the place filter silently vanished,
		// it turned "the query returned all 4,646 items" into 196 plausible-looking rows instead
		// of an obviously wrong page (verify round 1).
		//
		// It is also unreachable: the SQL matched this item through `p.slug = ANY(...)` over the
		// same join ListItemPlaces uses, so a row that reaches here HAS a named place. If that
		// ever stops being true, the item should disappear from a place view it does not belong
		// to -- which is visible -- rather than appear without a place, which is not.
	}

	return out, nil
}

// Detail is the full item, as /v1/items/{slug} returns it.
type Detail struct {
	ID            int32    `json:"id"`
	Slug          string   `json:"slug"`
	Name          string   `json:"name"`
	Rarity        string   `json:"rarity"`
	ItemType      *string  `json:"item_type,omitempty"`
	SlotFit       *string  `json:"slot_fit,omitempty"`
	ArmourWeight  *string  `json:"armour_weight,omitempty"`
	Binding       *string  `json:"binding,omitempty"`
	ItemLevel     *int32   `json:"item_level,omitempty"`
	RequiresLevel *int32   `json:"requires_level,omitempty"`
	Armor         *int32   `json:"armor,omitempty"`
	Critigation   *int32   `json:"critigation,omitempty"`
	DamageRange   *string  `json:"damage_range,omitempty"`
	Set           *string  `json:"set,omitempty"`
	TooltipImage  *string  `json:"tooltip_image,omitempty"`
	SourceURL     *string  `json:"tooltip_source_url,omitempty"`
	Confidence    string   `json:"confidence"`
	SourceNote    string   `json:"source_note"`
	OpenQuestion  *string  `json:"open_question,omitempty"`
	PvPSource     bool     `json:"pvp_source"`
	HasPvPStats   bool     `json:"has_pvp_stats"`
	PvPPenalty    bool     `json:"pvp_penalty"`
	EquipLocation []string `json:"equip_locations"`
	Classes       []string `json:"classes"`

	Stats       []StatLine  `json:"stats"`
	Sources     []SourceRef `json:"sources"`
	Costs       []CostRef   `json:"costs"`
	Attribution string      `json:"attribution"`
}

// Get returns one item by slug, with everything the item page shows, in one response.
func (s *Service) Get(ctx context.Context, slug string) (Detail, error) {
	slug = strings.TrimSpace(slug)
	if slug == "" {
		return Detail{}, fmt.Errorf("%w: empty slug", httpx.ErrInvalid)
	}

	id, err := s.q.GetItemBySlug(ctx, slug)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			// Through the central mapper, so the body is the same JSON shape as every other
			// rejection rather than a bare string.
			return Detail{}, fmt.Errorf("%w: item %q", httpx.ErrNotFound, slug)
		}
		return Detail{}, fmt.Errorf("get item by slug: %w", err)
	}
	return s.hydrate(ctx, id)
}

// StatLine is one stat on an item. Value is rendered as a string because the column is NUMERIC:
// turning 12.5 into a float to serialise it back is a lossy round trip for no gain.
type StatLine struct {
	Stat       string  `json:"stat"`
	Value      string  `json:"value"`
	Sign       int16   `json:"sign"`
	Unit       string  `json:"unit"`
	DamageType *string `json:"damage_type,omitempty"`
	PvP        bool    `json:"pvp"`
}

// SourceRef is one place an item comes from, with everything the item page shows beside it.
//
// ⭐ Names AND slugs. The name is what a person reads; the slug is what the list filters take, so
// an item page can link "everything else from here" straight back into /v1/items?place=… . Without
// the slug that link is a dead end: `region=Cimmeria` matches nothing, only `region=cimmeria` does
// (verify round 4). Additive, and nothing consumes this yet.
type SourceRef struct {
	ID              int64     `json:"id"`
	AcquisitionType *string   `json:"acquisition_type,omitempty"`
	Place           *string   `json:"place,omitempty"`
	PlaceSlug       *string   `json:"place_slug,omitempty"`
	Boss            *string   `json:"boss,omitempty"`
	Vendor          *string   `json:"vendor,omitempty"`
	Quest           *string   `json:"quest,omitempty"`
	Container       *string   `json:"container,omitempty"`
	Region          *string   `json:"region,omitempty"`
	RegionSlug      *string   `json:"region_slug,omitempty"`
	Map             *string   `json:"map,omitempty"`
	MapSlug         *string   `json:"map_slug,omitempty"`
	Tier            *string   `json:"tier,omitempty"`
	IsRaid          bool      `json:"is_raid"`
	Unchained       bool      `json:"unchained"`
	Confidence      string    `json:"confidence"`
	SourceNote      string    `json:"source_note"`
	OpenQuestion    *string   `json:"open_question,omitempty"`
	Costs           []CostRef `json:"costs,omitempty"`
}

// CostRef is what a vendor asks for an item.
type CostRef struct {
	Currency     string `json:"currency"`
	CurrencyName string `json:"currency_name"`
	Amount       string `json:"amount"`
}

// numeric renders a NUMERIC column without going through a float.
func numeric(n pgtype.Numeric) string {
	if !n.Valid {
		return ""
	}
	v, err := n.Value()
	if err != nil || v == nil {
		return ""
	}
	if s, ok := v.(string); ok {
		return s
	}
	return fmt.Sprint(v)
}

// hydrate assembles the item page's whole payload. Costs are fetched for every source in ONE
// query rather than per source, because an item bought from three vendors should not be three
// round trips.
func (s *Service) hydrate(ctx context.Context, id int32) (Detail, error) {
	row, err := s.q.GetItem(ctx, id)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Detail{}, fmt.Errorf("%w: item %d", httpx.ErrNotFound, id)
		}
		return Detail{}, fmt.Errorf("get item: %w", err)
	}

	out := Detail{
		ID: row.ItemID, Slug: row.Slug, Name: row.Name, Rarity: row.Rarity,
		ItemType: row.ItemType, SlotFit: row.SlotFit, ArmourWeight: row.ArmourWeight,
		Binding: row.Binding, ItemLevel: row.ItemLevel, RequiresLevel: row.RequiresLevel,
		Armor: row.Armor, Critigation: row.Critigation, DamageRange: row.DamageRange,
		Set: row.SetName, TooltipImage: row.TooltipImage, SourceURL: row.TooltipSourceUrl,
		Confidence: row.Confidence, SourceNote: row.SourceNote, OpenQuestion: row.OpenQuestion,
		PvPSource: row.PvpSource, HasPvPStats: row.HasPvpStats, PvPPenalty: row.PvpPenalty,
		EquipLocation: []string{}, Classes: []string{},
		Stats: []StatLine{}, Sources: []SourceRef{}, Costs: []CostRef{},
		Attribution: Attribution,
	}

	locs, err := s.q.ListItemEquipLocations(ctx, id)
	if err != nil {
		return Detail{}, fmt.Errorf("list equip locations: %w", err)
	}
	for _, l := range locs {
		out.EquipLocation = append(out.EquipLocation, l.Slug)
	}

	classes, err := s.q.ListItemClasses(ctx, id)
	if err != nil {
		return Detail{}, fmt.Errorf("list classes: %w", err)
	}
	for _, c := range classes {
		out.Classes = append(out.Classes, c.Slug)
	}

	stats, err := s.q.ListItemStats(ctx, id)
	if err != nil {
		return Detail{}, fmt.Errorf("list stats: %w", err)
	}
	for _, st := range stats {
		out.Stats = append(out.Stats, StatLine{
			Stat: st.Stat, Value: numeric(st.Value), Sign: st.Sign,
			Unit: st.Unit, DamageType: st.DamageType, PvP: st.Pvp,
		})
	}

	srcs, err := s.q.ListItemSources(ctx, id)
	if err != nil {
		return Detail{}, fmt.Errorf("list sources: %w", err)
	}
	ids := make([]int64, 0, len(srcs))
	for _, sr := range srcs {
		ids = append(ids, sr.ID)
	}
	costsBySource := map[int64][]CostRef{}
	if len(ids) > 0 {
		costs, err := s.q.ListItemCosts(ctx, ids)
		if err != nil {
			return Detail{}, fmt.Errorf("list costs: %w", err)
		}
		for _, c := range costs {
			cr := CostRef{Currency: c.Currency, CurrencyName: c.CurrencyName, Amount: numeric(c.Amount)}
			costsBySource[c.ItemSourceID] = append(costsBySource[c.ItemSourceID], cr)
			out.Costs = append(out.Costs, cr)
		}
	}
	for _, sr := range srcs {
		out.Sources = append(out.Sources, SourceRef{
			ID: sr.ID, AcquisitionType: sr.AcquisitionType,
			Place: sr.PlaceName, PlaceSlug: sr.PlaceSlug,
			Boss: sr.BossName, Vendor: sr.VendorName,
			Quest: sr.QuestName, Container: sr.ContainerName,
			Region: sr.RegionName, RegionSlug: sr.RegionSlug,
			Map: sr.MapName, MapSlug: sr.MapSlug, Tier: sr.Tier,
			IsRaid: sr.IsRaid, Unchained: sr.Unchained,
			Confidence: sr.Confidence, SourceNote: sr.SourceNote,
			OpenQuestion: sr.OpenQuestion, Costs: costsBySource[sr.ID],
		})
	}

	return out, nil
}

// escapeLike neutralises the ILIKE metacharacters in a free-text name query. Without it "%"
// matched all 4,646 items and "_" matched any single character — the search box returning the
// whole armory for one keystroke. The backslash itself goes first, or it would escape the escapes.
func escapeLike(s string) string {
	if s == "" {
		return ""
	}
	r := strings.NewReplacer(`\`, `\\`, "%", `\%`, "_", `\_`)
	return r.Replace(s)
}
