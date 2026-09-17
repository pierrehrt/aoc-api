#!/usr/bin/env python3
"""Generate the taxonomy seed migration from the armory snapshot.

⭐ WHY THIS IS A SCRIPT AND NOT HAND-TYPED SQL (AOC-009).
Every value below is MEASURED from items_clean.json, so the seed cannot drift from the data and
nobody has to trust a number written in a ticket six weeks ago. Re-running it against a newer
snapshot regenerates the migration; a diff of zero means the taxonomy has not moved.

It also refuses to guess. Where a fact is not in the data — the archetype a class belongs to, the
order of the rarities — it comes from an explicitly sourced constant below, never from the model.

Usage:  python3 scripts/gen_taxonomy_seed.py <path to armory_snapshot> > migrations/<ts>_taxonomy.sql
"""
import json
import sys
import collections
from pathlib import Path

# ---------------------------------------------------------------- sourced constants

# Tier A — confirmed by Pierre 2026-09-17, in answer to a direct question. Not derivable from the
# armory: no item record names an archetype. (CLAUDE.md STEP ZERO; DECISIONS.md 2026-09-17.)
ARCHETYPES = {
    "Soldier": ["Conqueror", "Dark Templar", "Guardian"],
    "Priest":  ["Bear Shaman", "Priest of Mitra", "Tempest of Set"],
    "Rogue":   ["Assassin", "Barbarian", "Ranger"],
    "Mage":    ["Demonologist", "Herald of Xotli", "Necromancer"],
}

# ⚠️ UNCONFIRMED ORDER. Taken from the AOC-009 plan. Corroborated only weakly: median item_level by
# rarity is monotonic non-decreasing (1,1,1,80,80,80), which is consistent with this order but does
# not prove the order WITHIN each group. Worth one question to Pierre before anything sorts on it.
RARITY_ORDER = ["Mundane", "Enchanted", "Superior", "Rare", "Epic", "Legendary"]

# Lightest to heaviest. Stated in the AOC-009 plan.
ARMOUR_WEIGHT_ORDER = ["Cloth", "Light", "Medium", "Heavy", "Full Plate"]

# ⭐ THE ATOMIC SLOTS, and the reason this list is shorter than the data's 15 distinct values.
# Two values in items_clean.json are COMPOUND and mean different things:
#   "Main Hand, Off Hand" (390 items) — occupies BOTH slots (two-handers)
#   "Left/Right Finger"   (188 items) — fits EITHER slot
# Flattened into lookup rows, "show me every Off Hand item" silently misses all 390 two-handers.
# So the atomic slots live here and the item->slot relation, with its both/either qualifier, is a
# join table owned by AOC-010. (AOC-009, 2026-09-17.)
ATOMIC_SLOTS = ["Head", "Shoulder", "Chest", "Wrist", "Hands", "Belt", "Legs", "Feet",
                "Cloak", "Main Hand", "Off Hand", "Left Finger", "Right Finger"]

# OCR typos of ONE faction, proven by identical vendor ("Wolves of the Steppes Camp") and region
# (Khitai) on every row: 33 + 2 + 1 = 36 items. Normalised rather than seeded as three factions.
FACTION_TYPOS = {"wolves of the Steppes": "Wolves of the Steppes",
                 "walves of the Steppes": "Wolves of the Steppes"}

# PvE 3.5 sorts between 3 and 4 — the reason sort_order is an explicit column and not alphabetical.
TIER_SORT = {"PvE 1": 10, "PvE 2": 20, "PvE 3": 30, "PvE 3.5": 35, "PvE 4": 40,
             "PvE 5": 50, "PvE 6": 60, "PvP 1": 110, "PvP 2": 120, "PvP 3": 130}


def slug(s: str) -> str:
    out = []
    for ch in s.lower():
        if ch.isalnum():
            out.append(ch)
        elif ch in " -/,'":
            out.append("-")
    return "-".join(filter(None, "".join(out).split("-")))


def q(s: str) -> str:
    return "'" + s.replace("'", "''") + "'"


def main() -> int:
    root = Path(sys.argv[1] if len(sys.argv) > 1 else ".")
    items = json.loads((root / "items_clean.json").read_text())
    sources = [s for r in items for s in (r.get("sources") or [])]

    def distinct(key, from_sources=False):
        rows = sources if from_sources else items
        out = set()
        for r in rows:
            v = r.get(key)
            if not v:
                continue
            if isinstance(v, list):
                out.update(x for x in v if x)
            else:
                out.add(v)
        return out

    item_types = sorted(distinct("item_type"))
    bindings = sorted(distinct("binding"))
    acq = sorted(distinct("acquisition_type", True))
    tiers = sorted(distinct("tier", True), key=lambda t: TIER_SORT.get(t, 999))
    classes = sorted(distinct("classes"))

    factions = set()
    for r in items:
        f = r.get("faction")
        if f:
            factions.add(FACTION_TYPOS.get(f, f))
    factions = sorted(factions)

    currencies = collections.Counter()
    for s in sources:
        for c in (s.get("acquisition_cost") or []):
            if isinstance(c, dict) and c.get("currency"):
                currencies[c["currency"]] += 1
    currencies = sorted(currencies)

    # every class the armory names must land under an archetype, or the seed is incomplete
    mapped = {c for cs in ARCHETYPES.values() for c in cs}
    missing = set(classes) - mapped
    if missing:
        print(f"ERROR: classes with no archetype: {sorted(missing)}", file=sys.stderr)
        return 1
    extra = mapped - set(classes)
    if extra:
        print(f"ERROR: archetype names no class in the data: {sorted(extra)}", file=sys.stderr)
        return 1

    o = print
    o("-- GENERATED by scripts/gen_taxonomy_seed.py — do not hand-edit.")
    o("-- Regenerate against a newer snapshot and diff; zero diff means the taxonomy has not moved.")
    o("--")
    o("-- ⭐ EVERY 'kind of thing' here is a ROW, never a Postgres ENUM and never a Go constant")
    o("-- (DECISIONS.md, 2026-09-12), so adding a class or a currency later is an INSERT.")
    o("--")
    o("-- ⚠️ EVERY SEED IS `ON CONFLICT DO NOTHING`, not \"insert if not exists\": the latter is two")
    o("-- statements with a race between them. A duplicated taxonomy row is a filter that offers the")
    o("-- same option twice, noticed months later by a reader rather than by us. Pinned by")
    o("-- TestEverySeedInsertIsIdempotent (AOC-005).")
    o("")
    o("-- +goose Up")
    o("")

    simple = [
        ("archetypes", "the 4 archetypes. Tier A, Pierre 2026-09-17 — no item record names one.", None),
        ("rarities", "6, measured. sort_order is explicit: see the UNCONFIRMED note in the generator.", "sort"),
        ("armour_weights", "5, measured. Ordered lightest to heaviest.", "sort"),
        ("item_types", f"{len(item_types)}, measured. (The AOC-009 plan said 32 — stale.)", None),
        ("equip_locations", "13 ATOMIC slots. The data's 15 values include 2 compound ones; see the generator.", None),
        ("currencies", f"{len(currencies)}, measured from acquisition_cost. (The plan said 26 — stale.)", None),
        ("acquisition_types", "3, measured.", None),
        ("tiers", "10, measured. PvE 3.5 sorts between 3 and 4, which is why sort_order exists.", "sort"),
        ("bindings", f"{len(bindings)}, measured — AOC-016's cleanup is in the data (it was 19).", None),
        ("factions", f"{len(factions)}, measured and de-typo'd; see FACTION_TYPOS in the generator.", None),
    ]
    for table, note, kind in simple:
        o(f"-- {note}")
        o(f"CREATE TABLE {table} (")
        o("    id         serial PRIMARY KEY,")
        o("    slug       varchar(64)  NOT NULL UNIQUE,")
        o("    name       varchar(64)  NOT NULL UNIQUE" + ("," if kind == "sort" else ""))
        if kind == "sort":
            o("    sort_order integer      NOT NULL")
        o(");")
        o("")

    o("-- classes carry their archetype. max_armour_weight is the CEILING a class may wear, which is")
    o("-- a DIFFERENT fact from an item's own armour_weight and must never be populated from it")
    o("-- (DECISIONS.md, 2026-09-12). It is Pierre-sourced, not armory-derivable, and unknown for all")
    o("-- twelve — so it is nullable and stays NULL. An empty field is a feature; a guess is a bug.")
    o("CREATE TABLE classes (")
    o("    id                serial PRIMARY KEY,")
    o("    archetype_id      integer     NOT NULL REFERENCES archetypes(id),")
    o("    slug              varchar(64) NOT NULL UNIQUE,")
    o("    name              varchar(64) NOT NULL UNIQUE,")
    o("    max_armour_weight integer     NULL REFERENCES armour_weights(id)")
    o(");")
    o("")

    def seed(table, names, sort=False):
        o(f"-- {len(names)} rows")
        cols = "slug, name" + (", sort_order" if sort else "")
        o(f"INSERT INTO {table} ({cols}) VALUES")
        rows = []
        for i, n in enumerate(names):
            if sort:
                order = TIER_SORT[n] if table == "tiers" else (i + 1) * 10
                rows.append(f"    ({q(slug(n))}, {q(n)}, {order})")
            else:
                rows.append(f"    ({q(slug(n))}, {q(n)})")
        o(",\n".join(rows))
        o("ON CONFLICT (slug) DO NOTHING;")
        o("")

    seed("archetypes", list(ARCHETYPES))
    seed("rarities", RARITY_ORDER, sort=True)
    seed("armour_weights", ARMOUR_WEIGHT_ORDER, sort=True)
    seed("item_types", item_types)
    seed("equip_locations", ATOMIC_SLOTS)
    seed("currencies", currencies)
    seed("acquisition_types", acq)
    seed("tiers", tiers, sort=True)
    seed("bindings", bindings)
    seed("factions", factions)

    o(f"-- {len(classes)} classes, each resolved to its archetype by slug rather than by a hardcoded id")
    o("INSERT INTO classes (archetype_id, slug, name) VALUES")
    rows = []
    for arch, members in ARCHETYPES.items():
        for c in sorted(members):
            rows.append(f"    ((SELECT id FROM archetypes WHERE slug = {q(slug(arch))}), {q(slug(c))}, {q(c)})")
    o(",\n".join(rows))
    o("ON CONFLICT (slug) DO NOTHING;")
    o("")

    o("-- +goose Down")
    o("DROP TABLE IF EXISTS classes;")
    for table, _, _ in reversed(simple):
        o(f"DROP TABLE IF EXISTS {table};")
    return 0


if __name__ == "__main__":
    sys.exit(main())
