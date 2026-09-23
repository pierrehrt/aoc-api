package items

// The rows that hang off an item: stats, spell effects, equip-location slots, classes, sources
// and the costs on those sources.

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5"
)

func insertChildren(ctx context.Context, tx pgx.Tx, its []Item, l *Lookups,
	vendorIDs map[string]int32, conf int32, rep *Report) error {

	var stats, spells, slots, classes [][]any

	for _, it := range its {
		for _, s := range it.Stats {
			stats = append(stats, statRow(it.ItemID, s))
		}
		// Deliberately a different table. A build calculator sums item_stats and can never reach
		// this one, so eight mounts cannot hand every wearer -8% Sprinting Stamina Drain.
		for _, s := range it.SpellEffect {
			spells = append(spells, statRow(it.ItemID, s))
		}
		_, names := slotFit(it, l)
		for _, n := range names {
			id, ok := l.EquipLocations[n]
			if !ok {
				return fmt.Errorf("item %d %q: equip location %q does not resolve — AOC-009 seeds "+
					"only the 13 atomic slots and this is not one of them", it.ItemID, it.Name, n)
			}
			slots = append(slots, []any{it.ItemID, id})
		}
		for _, c := range it.Classes {
			id, ok := l.Classes[c]
			if !ok {
				return fmt.Errorf("item %d %q: class %q does not resolve", it.ItemID, it.Name, c)
			}
			classes = append(classes, []any{it.ItemID, id})
		}
	}

	for _, t := range []struct {
		name string
		cols []string
		rows [][]any
	}{
		{"item_stats", []string{"item_id", "stat", "value", "sign", "unit", "damage_type", "pvp"}, stats},
		{"item_spell_effects", []string{"item_id", "stat", "value", "sign", "unit", "damage_type", "pvp"}, spells},
		{"item_equip_locations", []string{"item_id", "equip_location_id"}, slots},
		{"item_classes", []string{"item_id", "class_id"}, classes},
	} {
		if len(t.rows) == 0 {
			continue
		}
		if _, err := tx.CopyFrom(ctx, pgx.Identifier{t.name}, t.cols, pgx.CopyFromRows(t.rows)); err != nil {
			return fmt.Errorf("copying %s: %w", t.name, err)
		}
	}

	return insertSources(ctx, tx, its, l, vendorIDs, conf, rep)
}

func statRow(itemID int, s Stat) []any {
	return []any{itemID, s.Stat, s.Value, s.Sign, s.Unit, s.DamageType, s.PvP}
}

// insertSources writes item_sources one row at a time because each one's id is needed for its
// costs. 6,571 inserts inside one transaction is a few seconds and buys simple, obviously-correct
// code; CopyFrom would need a second pass to recover the ids.
func insertSources(ctx context.Context, tx pgx.Tx, its []Item, l *Lookups,
	vendorIDs map[string]int32, conf int32, rep *Report) error {

	for _, it := range its {
		live := it.LiveSources()
		if len(live) == 0 {
			rep.ItemsNoSources = append(rep.ItemsNoSources, it.ItemID)
		}
		for _, s := range live {
			placeID, bossID, mapID, questions := resolveSource(s, l, rep)

			var openQ *string
			if len(questions) > 0 {
				j := strings.Join(questions, "; ")
				openQ = &j
			}

			// ⭐ ONE FACT, ONE COLUMN (AOC-037). A source writes its own region/map only when its
			// place cannot supply them. The place is the stronger fact — it has a name, a map and
			// a region, and TestEveryPlacesRegionMatchesItsMap holds it to its map — while the
			// per-source copy has no invariant on it and no reader that needs it.
			//
			// ⛔ This is not a preference between two plausible values. 196 rows published
			// `cimmeria` for two dungeons that are in Kheshatta, in Stygia (Pierre, 2026-09-23,
			// Tier A): every one arrived through the container `Acheronian Cache`, whose observed
			// sources straddle two regions, so the container's geography was resolved ONCE — from
			// the first of them — and stamped onto every item it contains.
			//
			// Leaving the column NULL wherever a place exists also makes the read correct under
			// EITHER spelling of the coalesce, which matters while AOC-012 is unmerged.
			regionID := lookupPtr(s.Region, l.Regions)
			sourceMapID := mapID
			if placeID != nil {
				if geo, ok := l.PlaceGeo[*placeID]; ok {
					if geo.RegionID != nil {
						regionID = nil
					}
					if geo.MapID != nil {
						sourceMapID = nil
					}
				}
			}

			// ⭐ region_source says whether the region came from the armory or from Pierre.
			// Pierre's geography is Tier A; the armory's is derived. That difference belongs in
			// the row's provenance, not thrown away (AOC-010 verify carried this forward).
			//
			// ⚠️ Only while the row actually KEEPS that region. Claiming "region from Pierre's
			// reference_geography.json" on a row whose region_id was just dropped would describe a
			// value that is not there — and it was exactly this elevation that put `verified`, the
			// pipeline's highest confidence, onto the wrong value while the correct `derived` one
			// sat beside it (AOC-037).
			confidence, note := conf, "armory_snapshot/items_clean.json (Tier A*, OCR of AoC>TV)"
			if s.RegionSource != nil && *s.RegionSource == "pierre" && regionID != nil {
				if id, ok := l.Confidence["verified"]; ok {
					confidence = id
				}
				note = "region from Pierre's reference_geography.json (Tier A); rest from the snapshot"
			}

			var sourceID int64
			err := tx.QueryRow(ctx, `
INSERT INTO item_sources (item_id, acquisition_type_id, place_id, boss_id, vendor_id, quest_id,
                          container_id, region_id, map_id, tier_id, is_raid, coords, section_raw,
                          unchained, confidence_id, source_note, open_question)
VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17) RETURNING id`,
				it.ItemID,
				lookupPtr(s.AcquisitionType, l.AcquisitionTypes),
				placeID, bossID,
				lookupPtr(s.Vendor, vendorIDs),
				lookupPtr(s.Quest, l.Quests),
				lookupPtr(s.Container, l.Containers),
				regionID,
				sourceMapID,
				lookupPtr(s.Tier, l.Tiers),
				s.IsRaid, coordsText(s.Coords), s.SectionRaw, s.Unchained,
				confidence, note, openQ,
			).Scan(&sourceID)
			if err != nil {
				return fmt.Errorf("inserting source for item %d: %w", it.ItemID, err)
			}

			for _, c := range s.AcquisitionCost {
				cur, ok := l.Currencies[c.Currency]
				if !ok {
					return fmt.Errorf("item %d: currency %q does not resolve", it.ItemID, c.Currency)
				}
				if _, err := tx.Exec(ctx,
					`INSERT INTO item_costs (item_source_id, currency_id, amount) VALUES ($1,$2,$3)`,
					sourceID, cur, c.Amount); err != nil {
					return fmt.Errorf("inserting cost for item %d: %w", it.ItemID, err)
				}
			}
		}
	}
	return nil
}

// resolveSource turns one source row's names into ids, applying the recorded decisions for the
// four values we know do not resolve. Anything else would already have stopped the import in
// CheckResolvable, which runs before a single write.
func resolveSource(s Source, l *Lookups, rep *Report) (placeID, bossID, mapID *int32, questions []string) {
	inst, dung := deref(s.Instance), deref(s.DungeonOrRaid)
	if inst != "" || dung != "" {
		if id, ok := l.Places[PlaceKey{inst, dung}]; ok {
			placeID = &id
		} else if d, known := DecisionFor("place", placeLabel(inst, dung)); known {
			rep.AppliedNulls["place: "+d.Value]++
			if d.Effect != "" {
				questions = append(questions, d.Effect)
			}
		}
	}
	if b := deref(s.BossOrNPC); b != "" {
		if id, ok := l.Bosses[b]; ok {
			bossID = &id
		} else if d, known := DecisionFor("boss", b); known {
			rep.AppliedNulls["boss: "+d.Value]++
			if d.Effect != "" {
				questions = append(questions, d.Effect)
			}
		}
	}
	if m := deref(s.Map); m != "" {
		if id, ok := l.Maps[m]; ok {
			mapID = &id
		} else if d, known := DecisionFor("map", m); known {
			rep.AppliedNulls["map: "+d.Value]++
			if d.Effect != "" {
				questions = append(questions, d.Effect)
			}
		}
	}
	return placeID, bossID, mapID, questions
}

// coordsText serialises [387, 639] as "387,639".
//
// The column is text because `coords` is OVERLOADED and the schema says so: on a quest row it is
// where the quest-giver stands, not a dungeon entrance (AOC-017). A pair of numbers with two
// meanings should not pretend to be a typed point.
func coordsText(c []int) *string {
	if len(c) == 0 {
		return nil
	}
	parts := make([]string, len(c))
	for i, n := range c {
		parts[i] = strconv.Itoa(n)
	}
	s := strings.Join(parts, ",")
	return &s
}
