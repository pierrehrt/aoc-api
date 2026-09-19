package items

// Turning the snapshot's NAMES into the database's IDS, and refusing to guess.
//
// ⭐ THE RULE: a name this code has never been told about STOPS THE IMPORT. Nothing is created on
// the fly, nothing is fuzzy-matched, nothing is defaulted. An unrecognised taxonomy value means
// either AOC-009's seed is incomplete or the snapshot has changed, and both of those are things a
// person must look at — inventing a row is the failure STEP ZERO exists to prevent.
//
// ⚠️ THE ONE EXCEPTION IS AN EXPLICIT, REASONED LIST, and it is audited. `knownUnresolved` holds
// the handful of values we have already decided about: each says why, each names what it becomes
// (always a NULL plus an open question, never a guess), and **an entry that stops matching
// anything fails the import**. Without that last property the list would rot into exactly the
// "green check that stopped checking" this repo keeps finding (AOC-019, AOC-020, AOC-032).

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/jackc/pgx/v5"
)

// Lookups is every id the importer needs, read once before anything is written.
type Lookups struct {
	Rarities         map[string]int32
	ItemTypes        map[string]int32
	ArmourWeights    map[string]int32
	Bindings         map[string]int32
	EquipLocations   map[string]int32
	Factions         map[string]int32
	Classes          map[string]int32
	AcquisitionTypes map[string]int32
	Tiers            map[string]int32
	Currencies       map[string]int32
	Regions          map[string]int32
	Maps             map[string]int32
	Containers       map[string]int32
	Bosses           map[string]int32
	Quests           map[string]int32 // keyed on armory_label: quests.name is deliberately null
	Places           map[PlaceKey]int32
	SlotFits         map[string]int32
	Confidence       map[string]int32
}

// PlaceKey is how a source row finds its place: the PAIR the armory used, never a name.
// `places_armory_key` is UNIQUE on it with NULLS NOT DISTINCT (AOC-009).
type PlaceKey struct{ Instance, Dungeon string }

// knownDecision records a value we cannot resolve and have already decided what to do about.
type knownDecision struct {
	Value  string // the snapshot value, verbatim
	Why    string // what it becomes, and on whose authority
	Rows   int    // how many rows carried it when the decision was made
	Effect string // the open_question written onto the affected rows
}

// The complete list. Every entry traces to a decision or to an open question already on record.
//
// ⚠️ Adding to this list is a decision, not a convenience. The value becomes NULL and the row
// carries an open_question — the fact is recorded as unknown, never invented.
var knownUnresolved = map[string][]knownDecision{
	"place": {{
		Value: "World Boss",
		Why: "not a place — the armory used the dungeon column for a spawn category. " +
			"place_id stays NULL (AOC-010 made it nullable for exactly this).",
		Rows:   62,
		Effect: "source names 'World Boss', which is a category and not a location",
	}},
	"boss": {{
		Value: "Armsman's Arena",
		Why: "already an open question on the places table: Pierre's geography lists it as an " +
			"instance in Tarantia Noble District, the armory files it as a boss inside " +
			"'Armsman's Tavern'. Same place or two? Until Pierre says, boss_id stays NULL.",
		Rows:   12,
		Effect: "armory filed 'Armsman's Arena' as a boss; Pierre's geography calls it a place",
	}},
	"map": {{
		Value: "Skull Gate Pass",
		Why: "a PLACE, not a map — it is seeded in places with map_id NULL because it hangs " +
			"straight off its region (AOC-009). The armory put it in the map column.",
		Rows:   2,
		Effect: "",
	}, {
		Value: "Border Range",
		Why: "no such map is seeded and nothing says what it is. Possibly a variant of a " +
			"seeded name — but that is a game fact, and guessing one is forbidden.",
		Rows:   2,
		Effect: "source names map 'Border Range', which no seeded map matches",
	}},
}

// factionTypos are OCR damage on ONE faction name, corrected on Pierre's authority — the
// "Wolves of the Steppes typo merge" the board has carried since AOC-009 verify.
// ⚠️ This is the only place in the importer where a snapshot value is rewritten rather than
// resolved or nulled, and it exists because the correct value is *known*, not inferred.
var factionTypos = map[string]string{
	"wolves of the Steppes": "Wolves of the Steppes",
	"walves of the Steppes": "Wolves of the Steppes",
}

// LoadLookups reads every id the import needs in one pass.
func LoadLookups(ctx context.Context, q pgx.Tx) (*Lookups, error) {
	l := &Lookups{}
	byName := []struct {
		table string
		into  *map[string]int32
	}{
		{"rarities", &l.Rarities}, {"item_types", &l.ItemTypes},
		{"armour_weights", &l.ArmourWeights}, {"bindings", &l.Bindings},
		{"equip_locations", &l.EquipLocations}, {"factions", &l.Factions},
		{"classes", &l.Classes}, {"acquisition_types", &l.AcquisitionTypes},
		{"tiers", &l.Tiers}, {"currencies", &l.Currencies}, {"regions", &l.Regions},
		{"maps", &l.Maps}, {"containers", &l.Containers}, {"bosses", &l.Bosses},
	}
	for _, t := range byName {
		m, err := scanNameMap(ctx, q, "SELECT name, id FROM "+t.table) // #nosec G202 -- literals above
		if err != nil {
			return nil, fmt.Errorf("loading %s: %w", t.table, err)
		}
		*t.into = m
	}
	// slot_fits and confidence_levels are addressed by SLUG, not display name ('both', 'verified').
	for _, t := range []struct {
		table string
		into  *map[string]int32
	}{{"slot_fits", &l.SlotFits}, {"confidence_levels", &l.Confidence}} {
		m, err := scanNameMap(ctx, q, "SELECT slug, id FROM "+t.table) // #nosec G202 -- literals above
		if err != nil {
			return nil, fmt.Errorf("loading %s by slug: %w", t.table, err)
		}
		*t.into = m
	}
	// ⭐ quests join on armory_label. `quests.name` is deliberately NULL on all 51 rows: the real
	// quest name is unknown and AOC-009 refused to invent one (TestNoQuestHasAnInventedName).
	qm, err := scanNameMap(ctx, q, "SELECT armory_label, id FROM quests")
	if err != nil {
		return nil, fmt.Errorf("loading quests: %w", err)
	}
	l.Quests = qm

	// ⭐ places join on the PAIR, never a name — the armory's own (instance, dungeon).
	l.Places = map[PlaceKey]int32{}
	rows, err := q.Query(ctx, `SELECT coalesce(armory_instance,''), coalesce(armory_dungeon,''), id
	                           FROM places
	                           WHERE armory_instance IS NOT NULL OR armory_dungeon IS NOT NULL`)
	if err != nil {
		return nil, fmt.Errorf("loading places: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var k PlaceKey
		var id int32
		if err := rows.Scan(&k.Instance, &k.Dungeon, &id); err != nil {
			return nil, err
		}
		l.Places[k] = id
	}
	return l, rows.Err()
}

func scanNameMap(ctx context.Context, q pgx.Tx, sql string) (map[string]int32, error) {
	rows, err := q.Query(ctx, sql)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	m := map[string]int32{}
	for rows.Next() {
		var name string
		var id int32
		if err := rows.Scan(&name, &id); err != nil {
			return nil, err
		}
		m[name] = id
	}
	return m, rows.Err()
}

// Faction applies the recorded typo merge, then resolves.
func (l *Lookups) Faction(name string) (int32, bool) {
	if fixed, ok := factionTypos[name]; ok {
		name = fixed
	}
	id, ok := l.Factions[name]
	return id, ok
}

// Unresolved is one name the import could not turn into an id.
type Unresolved struct {
	Kind, Value string
	Rows        int
}

// CheckResolvable walks the whole snapshot and reports every name that does not resolve and is
// not on the decided list. It runs BEFORE a single row is written.
//
// It also audits the decided list itself: an entry that no longer matches anything is reported,
// because a stale exemption is how a list like this stops meaning what it says.
func CheckResolvable(its []Item, l *Lookups) (unknown []Unresolved, staleDecisions []string) {
	counts := map[string]map[string]int{}
	note := func(kind, value string) {
		if counts[kind] == nil {
			counts[kind] = map[string]int{}
		}
		counts[kind][value]++
	}

	for _, it := range its {
		if _, ex := it.Excluded(); ex {
			continue
		}
		check := func(kind string, v *string, in map[string]int32) {
			if v == nil || *v == "" {
				return
			}
			if _, ok := in[*v]; !ok {
				note(kind, *v)
			}
		}
		check("rarity", it.Rarity, l.Rarities)
		check("item_type", it.ItemType, l.ItemTypes)
		check("armour_weight", it.ArmourWeight, l.ArmourWeights)
		check("binding", it.Binding, l.Bindings)
		if it.Faction != nil && *it.Faction != "" {
			if _, ok := l.Faction(*it.Faction); !ok {
				note("faction", *it.Faction)
			}
		}
		for _, c := range it.Classes {
			if _, ok := l.Classes[c]; !ok {
				note("class", c)
			}
		}
		for _, s := range it.LiveSources() {
			check("acquisition_type", s.AcquisitionType, l.AcquisitionTypes)
			check("tier", s.Tier, l.Tiers)
			check("region", s.Region, l.Regions)
			check("container", s.Container, l.Containers)
			check("map", s.Map, l.Maps)
			check("quest", s.Quest, l.Quests)
			if s.BossOrNPC != nil && *s.BossOrNPC != "" {
				if _, ok := l.Bosses[*s.BossOrNPC]; !ok {
					note("boss", *s.BossOrNPC)
				}
			}
			for _, c := range s.AcquisitionCost {
				if _, ok := l.Currencies[c.Currency]; !ok {
					note("currency", c.Currency)
				}
			}
			inst, dung := deref(s.Instance), deref(s.DungeonOrRaid)
			if inst != "" || dung != "" {
				if _, ok := l.Places[PlaceKey{inst, dung}]; !ok {
					note("place", placeLabel(inst, dung))
				}
			}
		}
	}

	decided := map[string]bool{}
	for kind, ds := range knownUnresolved {
		for _, d := range ds {
			decided[kind+"\x00"+d.Value] = true
			if counts[kind][d.Value] == 0 {
				staleDecisions = append(staleDecisions, fmt.Sprintf(
					"%s %q is on the decided list but no longer appears in the snapshot — "+
						"either it was fixed upstream (delete the entry) or the match broke",
					kind, d.Value))
			}
		}
	}
	for kind, vs := range counts {
		for v, n := range vs {
			if !decided[kind+"\x00"+v] {
				unknown = append(unknown, Unresolved{Kind: kind, Value: v, Rows: n})
			}
		}
	}
	sort.Slice(unknown, func(i, j int) bool {
		if unknown[i].Kind != unknown[j].Kind {
			return unknown[i].Kind < unknown[j].Kind
		}
		return unknown[i].Value < unknown[j].Value
	})
	sort.Strings(staleDecisions)
	return unknown, staleDecisions
}

// DecisionFor returns the recorded decision for an unresolvable value, if there is one.
func DecisionFor(kind, value string) (knownDecision, bool) {
	for _, d := range knownUnresolved[kind] {
		if d.Value == value {
			return d, true
		}
	}
	return knownDecision{}, false
}

func placeLabel(instance, dungeon string) string {
	if instance == "" {
		return dungeon
	}
	if dungeon == "" {
		return instance
	}
	return instance + " / " + dungeon
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return strings.TrimSpace(*s)
}
