package items

// Writing the snapshot into the database.
//
// ⭐ ONE TRANSACTION, FULL REPLACE. The item tables are emptied and rebuilt rather than diffed.
// The snapshot is regenerated whenever a data-quality ticket lands (AOC-016, AOC-017, AOC-008,
// AOC-011 all changed it), so the import will be re-run — and a diff that is subtly wrong leaves a
// database nobody can reason about, while a replace is either right or it rolls back.
//
// ⭐ NOTHING IS INVENTED. Every id comes from a name the snapshot supplied and the database
// already knew (see resolve.go). Where the snapshot is silent the column is NULL and, when the
// silence is interesting, the row carries an `open_question` saying so. An empty field is a
// feature; a confident guess is a bug that reaches a raid.

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5"
)

// Report is what the import prints. It exists so the numbers are stated, not assumed — every
// count here is read back from the database after the writes, not accumulated as we go.
type Report struct {
	Counts         map[string]int
	ExcludedItems  []string
	HeldSources    int
	AppliedNulls   map[string]int // "kind: value" -> rows nulled under a recorded decision
	ItemsNoSources []int
	Nulls          map[string]int // "table.column" -> null rows
	// TypeDefaultSlots is item type -> items placed by that type's default slot because their
	// tooltip names none (AOC-054). Printed, so the rule's reach is stated rather than assumed.
	TypeDefaultSlots map[string]int
}

// tables in delete order: children first, then items, then the two things items point at.
var deleteOrder = []string{
	"item_costs", "item_sources", "item_stats", "item_spell_effects",
	"item_equip_locations", "item_classes", "items", "vendors", "sets",
}

// ShrinkFloorPercent is how much of the existing corpus an import must at least reproduce before it
// is allowed to replace it. Below this, Import refuses and nothing is deleted.
//
// ⭐ WHY A FLOOR EXISTS AT ALL (AOC-042). The import is a full replace: deleteOrder empties nine
// tables and rebuilds them. That is the right shape — a diff that is subtly wrong leaves a database
// nobody can reason about — but it means the number of items is stated in two places, the snapshot
// and the table, and until this guard nothing compared them. A WELL-FORMED 40-item snapshot would
// delete 4,646 items, insert 40, print a tidy report and exit 0.
//
// ⚠️ The dangerous input is not a truncated file. A file cut mid-JSON fails to decode, and
// DecodeSnapshot already refuses an empty one. The dangerous input is a VALID SHORT file — exactly
// what a `--limit N` smoke test produces (the same trap AOC-031 describes one layer up).
//
// 90 rather than a rounder number is a judgement, and the reason is worth keeping: the armory's item
// count moves by single items when a data-quality ticket lands, never by a tenth of the corpus. A
// drop past 10% is therefore not a smaller dataset, it is a different one.
const ShrinkFloorPercent = 90

// Options are the import's deliberate, typed-out permissions. It is a struct rather than a bare
// bool argument because `Import(ctx, tx, its, l, true)` is a line that deletes nine tables, and it
// should not be possible to read it without knowing what the `true` means.
type Options struct {
	// AllowShrink lets an import proceed that would leave the corpus below ShrinkFloorPercent of
	// what is already there. It exists for the legitimate case — the armory really did lose a large
	// block of items — and it is a thing a person types once, for the same reason -confirm-host is.
	AllowShrink bool
}

// checkFloor refuses an import that would replace the corpus with a fraction of itself.
//
// It reads `items` INSIDE the caller's transaction and BEFORE any delete, so the number it compares
// against is the corpus as it stands. An empty table is always allowed: that is the first import,
// and every test and dev run that starts from nothing.
func checkFloor(ctx context.Context, tx pgx.Tx, incoming int, allowShrink bool) error {
	var existing int
	if err := tx.QueryRow(ctx, "SELECT count(*) FROM items").Scan(&existing); err != nil {
		return fmt.Errorf("counting the items already present, before replacing them: %w", err)
	}
	if existing == 0 || allowShrink {
		return nil
	}
	// Integer arithmetic on purpose — a float boundary here would be one more thing to argue about
	// in a guard whose whole job is to be unambiguous.
	if incoming*100 >= existing*ShrinkFloorPercent {
		return nil
	}
	return fmt.Errorf(
		"REFUSING TO SHRINK THE CORPUS: the database holds %d items and this snapshot would leave "+
			"%d (%d%% of it, floor is %d%%).\nNothing has been deleted. A full replace cannot tell "+
			"a smaller dataset from a broken one, so it asks.\nIf the armory genuinely lost that "+
			"much, re-run with -allow-shrink",
		existing, incoming, incoming*100/existing, ShrinkFloorPercent)
}

// Import loads the snapshot. The caller owns the transaction so a failure anywhere leaves the
// database exactly as it was.
func Import(ctx context.Context, tx pgx.Tx, its []Item, l *Lookups, opt Options) (*Report, error) {
	rep := &Report{
		Counts:           map[string]int{},
		AppliedNulls:     map[string]int{},
		Nulls:            map[string]int{},
		TypeDefaultSlots: map[string]int{},
	}

	confUnconfirmed, ok := l.Confidence["unconfirmed"]
	if !ok {
		return nil, fmt.Errorf("confidence_levels has no 'unconfirmed' row — AOC-009's seed is not present")
	}

	// ⭐ WHICH ITEMS WILL ACTUALLY LAND, computed BEFORE the delete — because the floor below has to
	// compare against it, and after the delete there is nothing left to compare with. Nothing in
	// this loop touches the database; it is a pure read of the snapshot.
	live := make([]Item, 0, len(its))
	for _, it := range its {
		if reason, ex := it.Excluded(); ex {
			rep.ExcludedItems = append(rep.ExcludedItems, fmt.Sprintf("%d %s — %s", it.ItemID, it.Name, reason))
			continue
		}
		rep.HeldSources += len(it.Sources) - len(it.LiveSources())
		live = append(live, it)
	}

	if err := checkFloor(ctx, tx, len(live), opt.AllowShrink); err != nil {
		return nil, err
	}
	if err := checkCompounds(live); err != nil {
		return nil, err
	}

	for _, t := range deleteOrder {
		if _, err := tx.Exec(ctx, "DELETE FROM "+t); err != nil { // #nosec G202 -- literals above
			return nil, fmt.Errorf("clearing %s: %w", t, err)
		}
	}

	setIDs, err := insertSets(ctx, tx, live, l, confUnconfirmed)
	if err != nil {
		return nil, err
	}
	vendorIDs, err := insertVendors(ctx, tx, live, confUnconfirmed)
	if err != nil {
		return nil, err
	}
	if err := insertItems(ctx, tx, live, l, setIDs, confUnconfirmed, rep); err != nil {
		return nil, err
	}
	if err := insertChildren(ctx, tx, live, l, vendorIDs, confUnconfirmed, rep); err != nil {
		return nil, err
	}

	for _, t := range deleteOrder {
		var n int
		if err := tx.QueryRow(ctx, "SELECT count(*) FROM "+t).Scan(&n); err != nil { // #nosec G202
			return nil, fmt.Errorf("counting %s: %w", t, err)
		}
		rep.Counts[t] = n
	}
	if err := countNulls(ctx, tx, rep); err != nil {
		return nil, err
	}
	if err := checkCounts(live, l, rep); err != nil {
		return nil, err
	}
	return rep, nil
}

// checkCounts compares what is in the database against what was in the file.
//
// ⭐ IT COUNTS THE INPUT RATHER THAN A CONSTANT. A hardcoded 4,646 would be wrong the first time
// a data-quality ticket regenerates the snapshot, and somebody would "fix" it by editing the
// number — which is how a check stops checking. Derived from the input it cannot drift: it only
// ever asks "did every row I read arrive?", which stays true whatever the snapshot says next.
func checkCounts(live []Item, l *Lookups, rep *Report) error {
	want := map[string]int{
		"items": len(live), "sets": 0, "vendors": 0,
		"item_stats": 0, "item_spell_effects": 0, "item_sources": 0,
		"item_costs": 0, "item_classes": 0, "item_equip_locations": 0,
	}
	sets, vendors := map[string]bool{}, map[string]bool{}
	for _, it := range live {
		want["item_stats"] += len(it.Stats)
		want["item_spell_effects"] += len(it.SpellEffect)
		want["item_classes"] += len(it.Classes)
		if it.Set != nil && *it.Set != "" {
			sets[*it.Set] = true
		}
		// Slot rows are counted through slotFit — the one reading of equip_location that
		// insertChildren writes from. A second copy of that rule here could only disagree with it
		// (it did, briefly, on the default slot's test — AOC-054 review).
		_, slots, _ := slotFit(it, l)
		want["item_equip_locations"] += len(slots)
		for _, s := range it.LiveSources() {
			want["item_sources"]++
			want["item_costs"] += len(s.AcquisitionCost)
			if s.Vendor != nil && *s.Vendor != "" {
				vendors[*s.Vendor] = true
			}
		}
	}
	want["sets"], want["vendors"] = len(sets), len(vendors)

	var wrong []string
	keys := make([]string, 0, len(want))
	for k := range want {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		if rep.Counts[k] != want[k] {
			wrong = append(wrong, fmt.Sprintf("%s: database has %d, the snapshot had %d",
				k, rep.Counts[k], want[k]))
		}
	}
	if len(wrong) > 0 {
		return fmt.Errorf("rows went missing between the file and the database:\n  %s",
			strings.Join(wrong, "\n  "))
	}
	return nil
}

func insertSets(ctx context.Context, tx pgx.Tx, its []Item, l *Lookups, conf int32) (map[string]int32, error) {
	// declared_piece_count is what the SET says it holds; the count of rows we hold is a
	// different number and must not be confused with it (AOC-010 verify round 1).
	declared := map[string]*int{}
	for _, it := range its {
		if it.Set == nil || *it.Set == "" {
			continue
		}
		if _, seen := declared[*it.Set]; !seen || it.SetPieces != nil {
			declared[*it.Set] = it.SetPieces
		}
	}
	names := make([]string, 0, len(declared))
	for n := range declared {
		names = append(names, n)
	}
	sort.Strings(names)

	ids := map[string]int32{}
	for _, n := range names {
		var id int32
		err := tx.QueryRow(ctx, `
INSERT INTO sets (slug, name, declared_piece_count, confidence_id, source_note)
VALUES ($1, $2, $3, $4, $5) RETURNING id`,
			slugify(n), n, declared[n], conf,
			"armory_snapshot/items_clean.json (Tier A*, OCR of AoC>TV tooltips)").Scan(&id)
		if err != nil {
			return nil, fmt.Errorf("inserting set %q: %w", n, err)
		}
		ids[n] = id
	}
	_ = l
	return ids, nil
}

func insertVendors(ctx context.Context, tx pgx.Tx, its []Item, conf int32) (map[string]int32, error) {
	seen := map[string]bool{}
	for _, it := range its {
		for _, s := range it.LiveSources() {
			if s.Vendor != nil && *s.Vendor != "" {
				seen[*s.Vendor] = true
			}
		}
	}
	names := make([]string, 0, len(seen))
	for n := range seen {
		names = append(names, n)
	}
	sort.Strings(names)

	ids := map[string]int32{}
	for _, n := range names {
		var id int32
		// place_id stays NULL: the snapshot says where a vendor's STOCK comes from, never where
		// the vendor stands. Inferring one from the other would be inventing a location.
		err := tx.QueryRow(ctx, `
INSERT INTO vendors (slug, name, confidence_id, source_note, open_question)
VALUES ($1, $2, $3, $4, $5) RETURNING id`,
			slugify(n), n, conf,
			"armory_snapshot/items_clean.json — vendor column (Tier A*)",
			"where this vendor stands is not in the snapshot").Scan(&id)
		if err != nil {
			return nil, fmt.Errorf("inserting vendor %q: %w", n, err)
		}
		ids[n] = id
	}
	return ids, nil
}

func insertItems(ctx context.Context, tx pgx.Tx, its []Item, l *Lookups,
	setIDs map[string]int32, conf int32, rep *Report) error {

	slugs := assignSlugs(its)
	rows := make([][]any, 0, len(its))
	for _, it := range its {
		var rarityID *int32
		if it.Rarity != nil {
			if id, ok := l.Rarities[*it.Rarity]; ok {
				rarityID = &id
			}
		}
		if rarityID == nil {
			return fmt.Errorf("item %d %q has no resolvable rarity — items.rarity_id is NOT NULL", it.ItemID, it.Name)
		}
		fit, _, _ := slotFit(it, l)
		rows = append(rows, []any{
			it.ItemID, slugs[it.ItemID], it.Name, *rarityID,
			lookupPtr(it.ItemType, l.ItemTypes), fit,
			lookupPtr(it.ArmourWeight, l.ArmourWeights), lookupPtr(it.Binding, l.Bindings),
			it.ItemLevel, it.RequiresLevel, it.Armor, it.Critigation, it.DPS, it.DamageRange,
			setPtr(it.Set, setIDs), factionPtr(it.Faction, l), it.FactionRank,
			it.PvPSource, it.HasPvPStats, it.PvPPenalty,
			nil, // no_longer_available: excluded items never reach here (Pierre, 2026-09-13)
			it.TooltipImage, it.TooltipSourceURL,
			conf, "armory_snapshot/items_clean.json (Tier A*, OCR of AoC>TV tooltips)", nil,
		})
	}
	_, err := tx.CopyFrom(ctx, pgx.Identifier{"items"}, []string{
		"item_id", "slug", "name", "rarity_id", "item_type_id", "slot_fit_id",
		"armour_weight_id", "binding_id", "item_level", "requires_level", "armor",
		"critigation", "dps", "damage_range", "set_id", "faction_id", "faction_rank",
		"pvp_source", "has_pvp_stats", "pvp_penalty", "no_longer_available",
		"tooltip_image", "tooltip_source_url", "confidence_id", "source_note", "open_question",
	}, pgx.CopyFromRows(rows))
	if err != nil {
		return fmt.Errorf("copying items: %w", err)
	}
	_ = rep
	return nil
}

// slotFit turns the snapshot's equip_location into (slot_fit_id, atomic slot names), and says
// whether the slot came from the item's TYPE rather than its tooltip.
//
// ⭐ This is the compound-value handling AOC-010's whole schema shape exists for: one item, two slot
// rows, and a fit that says how to read them. "Main Hand, Off Hand" and "Left/Right Finger" are both
// an item that fits EITHER slot — the first sits on the one-handed weapons (1HB, 1HE, dagger,
// talisman), which Pierre confirmed go in either hand (AOC-058, 2026-09-30). It was read as `both`
// ("occupies both at once") from its shape alone until then. What takes both hands is a fact about
// the TYPE, item_types.two_handed, not about the slot rows.
//
// ⭐ A tooltip that names no slot falls back to its type's default slot (AOC-054): a necklace's
// tooltip says `Necklace` and nothing else, so without this all 146 had no slot. The default is
// item_types data — the migration's backfill applies the same rule to the rows already there.
func slotFit(it Item, l *Lookups) (fit *int32, slots []string, fromType bool) {
	if it.EquipLocation == nil || *it.EquipLocation == "" || *it.EquipLocation == "None" {
		if it.ItemType != nil {
			if s, ok := l.DefaultSlots[*it.ItemType]; ok {
				id := l.SlotFits["single"]
				return &id, []string{s}, true
			}
		}
		return nil, nil, false
	}
	v := *it.EquipLocation
	if c, ok := compoundSlots[v]; ok {
		id := l.SlotFits[c.fit]
		return &id, c.slots, false
	}
	// checkCompounds refused any other compound before anything was written, so this is one slot.
	id := l.SlotFits["single"]
	return &id, []string{v}, false
}

// compoundSlots is every equip_location that names more than one slot, and what each MEANS — a
// recorded decision per value, never a reading of its punctuation. Reading the shape is exactly how
// "Main Hand, Off Hand" came to mean "occupies both" for three weeks (AOC-058). Both are an item that
// fits EITHER slot: the first sits on the one-handed weapons, which Pierre confirmed go in either
// hand (2026-09-30); the second on rings. Measured 2026-09-30: the snapshot holds these two and no
// other (390 and 188 items).
var compoundSlots = map[string]struct {
	fit   string
	slots []string
}{
	"Main Hand, Off Hand": {"either", []string{"Main Hand", "Off Hand"}},
	"Left/Right Finger":   {"either", []string{"Left Finger", "Right Finger"}},
}

// checkCompounds refuses a snapshot that carries a compound equip_location nobody has decided about
// — a ',' or '/' in a value compoundSlots does not know — before the import deletes anything.
func checkCompounds(its []Item) error {
	unknown := map[string]int{}
	for _, it := range its {
		if it.EquipLocation == nil {
			continue
		}
		v := *it.EquipLocation
		if _, ok := compoundSlots[v]; !ok && strings.ContainsAny(v, ",/") {
			unknown[v]++
		}
	}
	if len(unknown) == 0 {
		return nil
	}
	vals := make([]string, 0, len(unknown))
	for v, n := range unknown {
		vals = append(vals, fmt.Sprintf("%q (%d items)", v, n))
	}
	sort.Strings(vals)
	return fmt.Errorf("REFUSING TO GUESS: the snapshot has compound equip locations nobody has decided "+
		"about: %s.\nNothing has been deleted. Say what each means in compoundSlots (internal/items/import.go)",
		strings.Join(vals, ", "))
}

// assignSlugs gives every item a unique slug. Three naive slugs collide, two of them because the
// names are genuinely identical (AOC-010 verify round 1) — so the tie-break is the item id, which
// is the source site's own and stable forever.
func assignSlugs(its []Item) map[int]string {
	byslug := map[string][]int{}
	for _, it := range its {
		s := slugify(it.Name)
		byslug[s] = append(byslug[s], it.ItemID)
	}
	out := map[int]string{}
	for _, it := range its {
		s := slugify(it.Name)
		if len(byslug[s]) > 1 {
			s = s + "-" + strconv.Itoa(it.ItemID)
		}
		out[it.ItemID] = s
	}
	return out
}

func slugify(s string) string {
	var b strings.Builder
	lastDash := true
	for _, r := range strings.ToLower(s) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
			lastDash = false
		default:
			if !lastDash {
				b.WriteByte('-')
				lastDash = true
			}
		}
	}
	return strings.Trim(b.String(), "-")
}

func lookupPtr(v *string, m map[string]int32) *int32 {
	if v == nil || *v == "" {
		return nil
	}
	if id, ok := m[*v]; ok {
		return &id
	}
	return nil
}

func setPtr(v *string, m map[string]int32) *int32 {
	if v == nil || *v == "" {
		return nil
	}
	if id, ok := m[*v]; ok {
		return &id
	}
	return nil
}

func factionPtr(v *string, l *Lookups) *int32 {
	if v == nil || *v == "" {
		return nil
	}
	if id, ok := l.Faction(*v); ok {
		return &id
	}
	return nil
}

func countNulls(ctx context.Context, tx pgx.Tx, rep *Report) error {
	for _, c := range []struct{ table, col string }{
		{"items", "item_type_id"}, {"items", "slot_fit_id"}, {"items", "armour_weight_id"},
		{"items", "set_id"}, {"items", "faction_id"}, {"items", "dps"},
		{"items", "tooltip_image"},
		{"item_sources", "place_id"}, {"item_sources", "boss_id"}, {"item_sources", "vendor_id"},
		{"item_sources", "quest_id"}, {"item_sources", "map_id"}, {"item_sources", "region_id"},
		{"item_sources", "coords"}, {"item_sources", "open_question"},
		// AOC-050: every row's section. 0 on a full import; the line itself tells an importer from
		// before it (which prints none) from this one, on a dry run of the deployed service.
		{"item_sources", "section_id"},
	} {
		var n int
		q := fmt.Sprintf("SELECT count(*) FROM %s WHERE %s IS NULL", c.table, c.col) // #nosec G201
		if err := tx.QueryRow(ctx, q).Scan(&n); err != nil {
			return fmt.Errorf("counting nulls in %s.%s: %w", c.table, c.col, err)
		}
		rep.Nulls[c.table+"."+c.col] = n
	}
	return nil
}
