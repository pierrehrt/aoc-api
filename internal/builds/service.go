package builds

import (
	"context"
	"fmt"
	"sort"
	"strconv"

	"github.com/pierrehrt/aoc-api/internal/db/sqlcgen"
	"github.com/pierrehrt/aoc-api/internal/httpx"
	"github.com/pierrehrt/aoc-api/internal/items"
)

// Querier is the slice of the generated API this package uses — named here so the tests run without
// a database.
type Querier interface {
	ListBuildSlots(ctx context.Context) ([]sqlcgen.EquipLocation, error)
	ListBuildHands(ctx context.Context) ([]int32, error)
	ListBuildClasses(ctx context.Context) ([]sqlcgen.ListBuildClassesRow, error)
	ListBuildArmourWeights(ctx context.Context) ([]sqlcgen.ListBuildArmourWeightsRow, error)
	ListBuildItems(ctx context.Context, itemIds []int32) ([]sqlcgen.ListBuildItemsRow, error)
	ListBuildItemSlots(ctx context.Context, itemIds []int32) ([]sqlcgen.ListBuildItemSlotsRow, error)
	ListBuildItemClasses(ctx context.Context, itemIds []int32) ([]sqlcgen.ListBuildItemClassesRow, error)
	ListBuildItemStats(ctx context.Context, itemIds []int32) ([]sqlcgen.ListBuildItemStatsRow, error)
}

// Service holds the build rules both surfaces call.
type Service struct{ q Querier }

func NewService(q Querier) *Service { return &Service{q: q} }

// SumNote is what the builder says its sum is, on the page and in /v1 (EP-08's honesty rule).
const SumNote = "raw sum: no set bonuses, no diminishing returns"

// A slot's status. Only an equipped item is summed.
const (
	StatusEmpty     = "empty"      // nothing in it
	StatusEquipped  = "equipped"   // worn and summed
	StatusUnknown   = "unknown"    // the URL names an id no item has (a 400 on /v1)
	StatusWrongSlot = "wrong_slot" // the item does not go in this slot
	StatusHeld      = "held"       // a two-hander in another hand takes this one (empty or not)
	StatusConflict  = "conflict"   // the picked class cannot wear it
)

// Result is everything the builder shows for a build.
type Result struct {
	// Build is the build as computed: entries in the slots' order, so its Values are its one URL.
	Build     Build     `json:"-"`
	Class     *ClassRef `json:"class"`
	Slots     []SlotRow `json:"slots"`
	Filled    int       `json:"filled"`    // slots with an item in the URL, whatever their status
	Conflicts int       `json:"conflicts"` // items the picked class cannot wear
	// The item page's base lines, summed over the equipped items that have one; nil when none has.
	Armor       *int64 `json:"armor,omitempty"`
	Critigation *int64 `json:"critigation,omitempty"`
	// Stats are every equipped item's stat lines, added per (stat, unit, damage type, PvP) in exact
	// decimals, in the order each first appears slot by slot. Spell effects and DPS are not stats.
	Stats       []items.StatLine `json:"stats"`
	Sum         string           `json:"sum"`
	Attribution string           `json:"attribution"`
	// Classes is the class picker, in classes.sort_order. Page only.
	Classes []ClassRef `json:"-"`

	wear wearRule
}

// ClassRef names a class.
type ClassRef struct {
	Slug      string `json:"slug"`
	Name      string `json:"name"`
	ShortName string `json:"short_name,omitempty"`
	Archetype string `json:"archetype"`
}

// SlotRow is one row of the builder.
type SlotRow struct {
	Slot   items.Term `json:"slot"`
	Status string     `json:"status"`
	ItemID int32      `json:"item_id,omitempty"` // as the URL gave it, even when no item has it
	Item   *ItemRef   `json:"item,omitempty"`
	// HeldBy is the two-hander taking this hand, when Status is held.
	HeldBy *ItemRef `json:"held_by,omitempty"`
}

// ItemRef is an item in a slot.
type ItemRef struct {
	ID                int32  `json:"id"`
	Slug              string `json:"slug"`
	Name              string `json:"name"`
	RarityColourToken string `json:"rarity_colour_token,omitempty"`
	TwoHanded         bool   `json:"two_handed"`
}

// Wears says whether the picked class can wear an item, from what a list row knows of it: its class
// slugs and its armour weight's slug. True when no class is picked. The same rule as a slot's
// conflict, so a dimmed row and a marked slot cannot disagree.
func (r Result) Wears(classes []string, armourWeight string) bool {
	var order *int32
	if o, ok := r.wear.weights[armourWeight]; ok {
		order = &o
	}
	return r.wear.wears(classes, order)
}

// wearRule is the picked class's rule. The zero value lets anyone wear anything (no class picked).
type wearRule struct {
	class   string
	ceiling *int32           // the class's armour ceiling, as an armour_weights.sort_order
	weights map[string]int32 // armour weight slug → sort_order, read only when there is a ceiling
}

// wears: an item can be worn when it lists no class or lists this one, and its armour weight is
// within the class's ceiling when one is recorded (reference/content-model.md: the ceiling is
// Pierre's to supply; none is yet).
func (w wearRule) wears(classes []string, weightOrder *int32) bool {
	if w.class == "" {
		return true
	}
	if len(classes) > 0 && !contains(classes, w.class) {
		return false
	}
	return w.ceiling == nil || weightOrder == nil || *weightOrder <= *w.ceiling
}

// world is what one build needs from the database, loaded in a fixed number of queries.
type world struct {
	slots  []slot
	index  map[string]int // slot slug → position
	class  *sqlcgen.ListBuildClassesRow
	all    []sqlcgen.ListBuildClassesRow
	items  map[int32]*item
	wear   wearRule
	nhands int
}

type slot struct {
	slug, name string
	hand       bool // a slot a one-handed weapon fits: a two-hander takes all of them
}

type item struct {
	row     sqlcgen.ListBuildItemsRow
	slots   []string
	classes []string
	stats   []sqlcgen.ListBuildItemStatsRow
}

func (it *item) ref() *ItemRef {
	return &ItemRef{ID: it.row.ItemID, Slug: it.row.Slug, Name: it.row.Name, RarityColourToken: deref(it.row.RarityColourToken), TwoHanded: it.row.TwoHanded}
}

func (it *item) fits(slot string) bool { return contains(it.slots, slot) }

// load reads the slots, the classes and the build's items (and any extra ids, for Add). A slot or
// class slug no row has is a 400: the URL names something that does not exist.
func (s *Service) load(ctx context.Context, b Build, extra ...int32) (*world, error) {
	w := &world{index: map[string]int{}, items: map[int32]*item{}}
	locs, err := s.q.ListBuildSlots(ctx)
	if err != nil {
		return nil, fmt.Errorf("list build slots: %w", err)
	}
	// The hands matter only to a build with something in it — and most Armory requests carry none, so
	// they do not pay for the derivation (~1.7 ms on the corpus, measured 2026-10-05).
	var hands []int32
	if len(b.Entries) > 0 || len(extra) > 0 {
		if hands, err = s.q.ListBuildHands(ctx); err != nil {
			return nil, fmt.Errorf("list build hands: %w", err)
		}
	}
	for i, l := range locs {
		h := containsID(hands, l.ID)
		if h {
			w.nhands++
		}
		w.slots = append(w.slots, slot{slug: l.Slug, name: l.Name, hand: h})
		w.index[l.Slug] = i
	}
	for _, e := range b.Entries {
		if _, ok := w.index[e.Slot]; !ok {
			return nil, fmt.Errorf("%w: %s: there is no slot %q", httpx.ErrInvalid, ParamGear, e.Slot)
		}
	}

	if w.all, err = s.q.ListBuildClasses(ctx); err != nil {
		return nil, fmt.Errorf("list build classes: %w", err)
	}
	if b.Class != "" {
		for i := range w.all {
			if w.all[i].Slug == b.Class {
				w.class = &w.all[i]
			}
		}
		if w.class == nil {
			return nil, fmt.Errorf("%w: %s: there is no class %q", httpx.ErrInvalid, ParamClass, b.Class)
		}
		w.wear = wearRule{class: w.class.Slug, ceiling: w.class.MaxArmourWeightOrder}
		if w.wear.ceiling != nil {
			weights, err := s.q.ListBuildArmourWeights(ctx)
			if err != nil {
				return nil, fmt.Errorf("list build armour weights: %w", err)
			}
			w.wear.weights = map[string]int32{}
			for _, aw := range weights {
				w.wear.weights[aw.Slug] = aw.SortOrder
			}
		}
	}

	ids := append(b.IDs(), extra...)
	if len(ids) == 0 {
		return w, nil
	}
	rows, err := s.q.ListBuildItems(ctx, ids)
	if err != nil {
		return nil, fmt.Errorf("list build items: %w", err)
	}
	for _, r := range rows {
		w.items[r.ItemID] = &item{row: r}
	}
	slotRows, err := s.q.ListBuildItemSlots(ctx, ids)
	if err != nil {
		return nil, fmt.Errorf("list build item slots: %w", err)
	}
	for _, r := range slotRows {
		if it := w.items[r.ItemID]; it != nil {
			it.slots = append(it.slots, r.Slug)
		}
	}
	classRows, err := s.q.ListBuildItemClasses(ctx, ids)
	if err != nil {
		return nil, fmt.Errorf("list build item classes: %w", err)
	}
	for _, r := range classRows {
		if it := w.items[r.ItemID]; it != nil {
			it.classes = append(it.classes, r.Slug)
		}
	}
	stats, err := s.q.ListBuildItemStats(ctx, ids)
	if err != nil {
		return nil, fmt.Errorf("list build item stats: %w", err)
	}
	for _, r := range stats {
		if it := w.items[r.ItemID]; it != nil {
			it.stats = append(it.stats, r)
		}
	}
	return w, nil
}

func (w *world) wears(it *item) bool { return w.wear.wears(it.classes, it.row.ArmourWeightOrder) }

// normalize puts the entries in the slots' order: one build, one URL.
func (w *world) normalize(b Build) Build {
	out := b
	out.Entries = append([]Entry(nil), b.Entries...)
	sort.SliceStable(out.Entries, func(i, j int) bool { return w.index[out.Entries[i].Slot] < w.index[out.Entries[j].Slot] })
	return out
}

// twoHander is the two-handed item, fitting the hand it is in, that takes `hand` from another hand
// slot — or nil. Only a hand can be taken.
func (w *world) twoHander(b Build, hand string) *item {
	if !w.slots[w.index[hand]].hand {
		return nil
	}
	for _, e := range b.Entries {
		if e.Slot == hand || !w.slots[w.index[e.Slot]].hand {
			continue
		}
		if it := w.items[e.ItemID]; it != nil && it.row.TwoHanded && it.fits(e.Slot) {
			return it
		}
	}
	return nil
}

// allowedBeside: what a two-hander still lets into the other hand (bow → ammunition, Pierre,
// 2026-10-05: item_types.other_hand_type_id).
func allowedBeside(two, it *item) bool {
	return two.row.OtherHandTypeID != nil && it.row.ItemTypeID != nil && *two.row.OtherHandTypeID == *it.row.ItemTypeID
}

// Compute is what the builder shows: every slot's status and the combined stats of what is worn.
func (s *Service) Compute(ctx context.Context, b Build) (Result, error) {
	w, err := s.load(ctx, b)
	if err != nil {
		return Result{}, err
	}
	return w.compute(w.normalize(b)), nil
}

func (w *world) compute(b Build) Result {
	res := Result{Build: b, Sum: SumNote, Attribution: items.Attribution, Stats: []items.StatLine{}, Slots: []SlotRow{}, wear: w.wear}
	for _, c := range w.all {
		ref := ClassRef{Slug: c.Slug, Name: c.Name, ShortName: deref(c.ShortName), Archetype: c.ArchetypeName}
		res.Classes = append(res.Classes, ref)
		if w.class != nil && c.Slug == w.class.Slug {
			cc := ref
			res.Class = &cc
		}
	}

	type key struct {
		stat, unit, damage string
		pvp                bool
	}
	sums := map[key]int{} // key → index in res.Stats
	var centi []int64
	add := func(dst **int64, v *int32) {
		if v == nil {
			return
		}
		if *dst == nil {
			*dst = new(int64)
		}
		**dst += int64(*v)
	}

	for _, sl := range w.slots {
		row := SlotRow{Slot: items.Term{Slug: sl.slug, Name: sl.name}, Status: StatusEmpty}
		two := w.twoHander(b, sl.slug)
		id, ok := b.Item(sl.slug)
		switch {
		case !ok:
			if two != nil {
				row.Status, row.HeldBy = StatusHeld, two.ref()
			}
		case w.items[id] == nil:
			row.ItemID, row.Status = id, StatusUnknown
			res.Filled++
		default:
			it := w.items[id]
			row.ItemID, row.Item = id, it.ref()
			res.Filled++
			switch {
			case !it.fits(sl.slug):
				row.Status = StatusWrongSlot
			case two != nil && !allowedBeside(two, it):
				row.Status, row.HeldBy = StatusHeld, two.ref()
			case !w.wears(it):
				row.Status = StatusConflict
				res.Conflicts++
			default:
				row.Status = StatusEquipped
				add(&res.Armor, it.row.Armor)
				add(&res.Critigation, it.row.Critigation)
				for _, st := range it.stats {
					k := key{st.Stat, st.Unit, deref(st.DamageType), st.Pvp}
					i, seen := sums[k]
					if !seen {
						i = len(res.Stats)
						sums[k] = i
						res.Stats = append(res.Stats, items.StatLine{Stat: st.Stat, Unit: st.Unit, DamageType: st.DamageType, PvP: st.Pvp})
						centi = append(centi, 0)
					}
					centi[i] += st.Centi
				}
			}
		}
		res.Slots = append(res.Slots, row)
	}
	for i, c := range centi {
		res.Stats[i].Value, res.Stats[i].Sign = hundredths(c)
	}
	return res
}

// hundredths prints a signed sum of hundredths the way the column stores a value — "154.00",
// "6.50" — with its sign apart, as items.StatLine carries it. A sum of nothing is 0, signless.
func hundredths(c int64) (string, int16) {
	var sign int16
	switch {
	case c > 0:
		sign = 1
	case c < 0:
		sign, c = -1, -c
	}
	frac := strconv.FormatInt(c%100, 10)
	if len(frac) == 1 {
		frac = "0" + frac
	}
	return strconv.FormatInt(c/100, 10) + "." + frac, sign
}

// Add puts an item in a build by the rules, and returns the build it makes — or the unchanged build
// and the reason it was refused. The rules:
//   - an item goes only in a slot its equip locations list;
//   - the picked class must be able to wear it;
//   - a hand taken by a two-hander in another hand takes nothing but what that two-hander allows
//     there (bow → ammunition);
//   - a two-hander empties every other hand, but for what it allows there;
//   - with no slot named, the first fitting slot that is free, in the slots' order; if none is, the
//     first fitting one that can be taken, replacing what is in it.
func (s *Service) Add(ctx context.Context, b Build, a Add) (Build, string, error) {
	w, err := s.load(ctx, b, a.ItemID)
	if err != nil {
		return Build{}, "", err
	}
	if a.Slot != "" {
		if _, ok := w.index[a.Slot]; !ok {
			return Build{}, "", fmt.Errorf("%w: %s: there is no slot %q", httpx.ErrInvalid, ParamAdd, a.Slot)
		}
	}
	b = w.normalize(b)
	it := w.items[a.ItemID]
	switch {
	case it == nil:
		return b, fmt.Sprintf("There is no item %d.", a.ItemID), nil
	case len(it.slots) == 0:
		return b, it.row.Name + " goes in no equipment slot.", nil
	case !w.wears(it):
		return b, fmt.Sprintf("A %s cannot wear %s.", w.class.Name, it.row.Name), nil
	}

	var candidates []string
	if a.Slot != "" {
		if !it.fits(a.Slot) {
			return b, fmt.Sprintf("%s does not go in the %s slot.", it.row.Name, w.slots[w.index[a.Slot]].name), nil
		}
		candidates = []string{a.Slot}
	} else {
		for _, sl := range w.slots { // the item's slots, in the slots' order
			if it.fits(sl.slug) {
				candidates = append(candidates, sl.slug)
			}
		}
	}
	// A hand is open to this item unless a two-hander in ANOTHER hand takes it. A two-hander in the
	// very slot is simply replaced.
	open := func(sl string) bool {
		two := w.twoHander(b.Without(sl), sl)
		return two == nil || allowedBeside(two, it)
	}
	pick := ""
	for _, c := range candidates {
		if _, taken := b.Item(c); !taken && open(c) {
			pick = c
			break
		}
	}
	if pick == "" {
		for _, c := range candidates {
			if open(c) {
				pick = c
				break
			}
		}
	}
	if pick == "" {
		c := candidates[0]
		two := w.twoHander(b.Without(c), c)
		return b, fmt.Sprintf("The %s slot is taken: %s needs both hands.", w.slots[w.index[c]].name, two.row.Name), nil
	}

	out := b.With(pick, a.ItemID)
	if it.row.TwoHanded {
		for _, sl := range w.slots {
			if !sl.hand || sl.slug == pick {
				continue
			}
			if id, ok := out.Item(sl.slug); ok {
				if other := w.items[id]; other == nil || !allowedBeside(it, other) {
					out = out.Without(sl.slug)
				}
			}
		}
	}
	return w.normalize(out), "", nil
}

func contains(xs []string, x string) bool {
	for _, v := range xs {
		if v == x {
			return true
		}
	}
	return false
}

func containsID(xs []int32, x int32) bool {
	for _, v := range xs {
		if v == x {
			return true
		}
	}
	return false
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}
