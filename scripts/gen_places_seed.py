#!/usr/bin/env python3
"""Generate the place-entity seed migration from the armory snapshot and Pierre's geography.

⭐ WHY THIS IS A SCRIPT AND NOT HAND-TYPED SQL (AOC-009, same reason as gen_taxonomy_seed.py).
86 places, 44 bosses and 51 quests are too many to hand-type without typos, and typing them by
hand would also mean typing FACTS by hand. Everything here is MEASURED from one of exactly two
sources, and every row carries which one in its source_note:

  * reference_geography.json — Pierre, Tier A. The region -> map -> instance tree, the two
    complexes, the aliases, the mapless instances and the containers.
  * items_clean.json — Tier A*, our own OCR capture of AoC>TV. Where an item was filed.

⭐ IT REFUSES TO GUESS, LOUDLY. Every place the armory names must resolve to a place Pierre's
geography knows, or be listed in UNRESOLVED_ARMORY_PLACES below with the question it raises.
A snapshot that grows a new instance name fails this script rather than inventing a page for it.
Same for a parenthetical the parser does not recognise, and for a class of row it has never seen.

⚠️ THE TWO SOURCES DISAGREE IN FOUR PLACES AND THOSE ARE NOT SMOOTHED OVER. A row whose sources
conflict is seeded `disputed` with the conflict written into open_question, per
reference/sourcing-standards.md § 3 — the page shows a ⚠️ and both versions rather than a silent
pick. `SELECT * FROM places WHERE open_question IS NOT NULL` is how the next ticket finds them.

Usage:  python3 scripts/gen_places_seed.py <path to armory_snapshot> > migrations/<ts>_places.sql
"""
import collections
import json
import sys
from pathlib import Path

# ---------------------------------------------------------------- sourced constants

# Confidence vocabulary — reference/sourcing-standards.md § 3, verbatim. A lookup table, not an
# ENUM, for the same reason as every other taxonomy (DECISIONS.md, 2026-09-12).
CONFIDENCE = [
    ("verified", "Verified", 10),
    ("corroborated", "Corroborated", 20),
    ("unconfirmed", "Unconfirmed", 30),
    ("disputed", "Disputed", 40),
]

# ⚠️ NOT PLACES. Three values in the armory's dungeon_or_raid column are acquisition buckets the
# source site used as section headers, not dungeons. Seeding them would create three pages for
# things you cannot zone into. The rows keep their region and map; they simply have no place.
NOT_A_PLACE = {
    "Expansion Pack Bonus": "an acquisition bucket, not a dungeon (1 row)",
    "No longer available": "an acquisition bucket, not a dungeon (1 row)",
    "World Boss": "open-world encounters, not an instance (62 rows)",
}

# ⚠️ NOT A BOSS. Pierre's geography lists Armsman's Arena as an INSTANCE in Aquilonia / Tarantia
# Noble District, while the armory files 12 rows with boss_or_npc='Armsman's Arena' inside
# dungeon_or_raid='Armsman's Tavern'. One of the two columns is holding the other's value. The
# place is seeded (from the geography, Tier A) and flagged; no boss row is invented for it.
NOT_A_BOSS = {
    "Armsman's Arena": "Pierre's geography lists this as an instance in Tarantia Noble District; "
                       "the armory files it as a boss inside 'Armsman's Tavern'",
}

# ⭐ THE FIVE THAT ARE BOSSES AND NOT PLACES — Pierre's research, 2026-09-13
# (armory_snapshot/open_questions.md § 2). Our data already had every one of their LOCATIONS
# right; only the field was wrong. They keep their map and region and have no place.
CONFIRMED_BOSSES_WITHOUT_A_PLACE = {
    "Cheng-Ho Battle Commander", "Tamarin Battle Commander", "Gugalanna of Yimsha",
    "Lunn the Warmonger", "Ymir's Son",
}

# ⚠️ ARMORY PLACE NAMES PIERRE'S GEOGRAPHY DOES NOT KNOW. Each looks like a near-miss for a place
# it DOES know, and resolving a near-miss is exactly the guess this project does not make
# (AOC-009: "flagged so a later ticket can find them, not silently resolved"). They are seeded as
# their own places, `unconfirmed`, with the candidate named in the question.
UNRESOLVED_ARMORY_PLACES = {
    "Ardashir Arena": "armory-only name (48 rows, Turan / Coast of Ardashir). Pierre's geography "
                      "knows 'Ardashir Fort' on the same map. Same place or two?",
    "Armsman's Tavern": "armory-only name (12 rows, Aquilonia / Tarantia Noble District). Pierre's "
                        "geography knows 'Armsman's Arena' on the same map, which the armory files "
                        "as a BOSS inside this dungeon. Same place or two?",
    "Caravan Raider Camp": "armory-only name (96 rows, Stygia / Kheshatta). Pierre's geography "
                           "knows \"Caravan Raider's Hideout\" on the same map. Same place or two?",
}

# ⚠️ Observations about boss rows, not claims about the game. Both are things a reader would
# notice and ask about, so they are recorded rather than smoothed away.
BOSS_OPEN_QUESTIONS = {
    "Time Trial": "reads like a challenge mode rather than a boss; the armory files 5 T'ian'an "
                  "District rows under it",
    "Kyllikki": "the armory puts this boss in Otherworldly Junction (Stygia) while a place of the "
                "same name sits in Cimmeria (Kyllikki's Crypt). AOC-017 § Defect 13: these rows "
                "are the Unchained variants",
    "Vistrix": "the armory puts this boss in Otherworldly Junction (Stygia) while a place of the "
               "same name sits in Cimmeria (Vistrix's Lair). AOC-017 § Defect 13: these rows are "
               "the Unchained variants",
    "Yakhmar": "the armory puts this boss in Otherworldly Junction (Stygia) while a place of the "
               "same name sits in Cimmeria (Yakhmar's Cave). AOC-017 § Defect 13: these rows are "
               "the Unchained variants",
}

# The armory's `quest` column holds a mix: quest-giver NPCs, quest hubs and a few real quest
# titles. A label that IS the name of a seeded region, map or place is demonstrably not a quest
# name — computed below rather than listed here, because the list would go stale the moment a
# place is renamed.

UNCHAINED_SUFFIX = " (Unchained)"

# Caps. Measured against the data at the bottom of this script, which fails if anything outgrows
# them (reference/ux-principles.md § 3 — caps are schema, not style).
NAME_CAP = 96
NOTE_CAP = 300


def slug(s: str) -> str:
    out = []
    for ch in s.lower():
        if ch.isalnum():
            out.append(ch)
        elif ch in " -/,'":
            out.append("-")
    return "-".join(filter(None, "".join(out).split("-")))


def q(s):
    if s is None:
        return "NULL"
    return "'" + str(s).replace("'", "''") + "'"


def plural(n: int) -> str:
    """'1 source row', not '1 source rows'. These sentences are shown to readers."""
    return "source row" if n == 1 else "source rows"


def die(msg):
    print(f"ERROR: {msg}", file=sys.stderr)
    sys.exit(1)


def ref(table, value):
    """A foreign key resolved by slug at seed time, never by a hardcoded id."""
    if value is None:
        return "NULL"
    return f"(SELECT id FROM {table} WHERE slug = {q(slug(value))})"


class Row(dict):
    """One seeded entity. `src` is its provenance sentence, `oq` the question it leaves open."""


def main() -> int:
    root = Path(sys.argv[1] if len(sys.argv) > 1 else ".")
    items = json.loads((root / "items_clean.json").read_text())
    geo = json.loads((root / "reference_geography.json").read_text())
    sources = [s for r in items for s in (r.get("sources") or [])]

    GEO_SRC = "reference_geography.json (Pierre, Tier A, 2026-09-13)"

    # ---------------------------------------------------------------- regions
    # Order is the order Pierre listed them in, which runs roughly with the levelling path. It is
    # his file's own order, not a claim of our own about progression.
    regions = []
    for i, name in enumerate(geo["regions"]):
        regions.append(Row(name=name, sort_order=(i + 1) * 10, conf="verified",
                           src=GEO_SRC + " — the region list is complete", oq=None))

    # ---------------------------------------------------------------- maps
    # A map key that mapless_instances names as an INSTANCE is not a map. The file says so itself:
    # "the 'regions' tree above still lists Skull Gate Pass and Kuthchemes as maps because the
    # armory filed them that way". Derived from the file, not hardcoded here.
    mapless_named = {e["instance"] for lst in geo["mapless_instances"].values()
                     if isinstance(lst, list) for e in lst}
    maps, places = [], []
    geo_place_of = {}          # instance name -> (region, map or None)

    for region, region_maps in geo["regions"].items():
        for map_name, instances in region_maps.items():
            if map_name in mapless_named:
                # An instance the armory filed as a map. Its children — or, if it has none, the
                # name itself — become places that hang straight off the region.
                if instances:
                    for inst in instances:
                        geo_place_of[inst] = (region, None)
                        places.append(Row(
                            name=inst, region=region, map=None, parent=None, unchained=False,
                            conf="unconfirmed", src=GEO_SRC,
                            oq=f"Pierre's geography lists this both as the map '{map_name}' and as "
                               f"the instance '{inst}'; the armory spells the section "
                               f"differently again. One place or two?"))
                else:
                    geo_place_of[map_name] = (region, None)
                    places.append(Row(
                        name=map_name, region=region, map=None, parent=None, unchained=False,
                        conf="verified", src=GEO_SRC + "; mapless_instances — entered from the "
                                                       "Raid Finder, no map between it and the region",
                        oq="the name of the instance INSIDE it is unknown — our data says only "
                           "'Wave Boss Random Drop'"))
                continue
            maps.append(Row(name=map_name, region=region, conf="verified", src=GEO_SRC, oq=None))
            for inst in instances:
                geo_place_of[inst] = (region, map_name)
                places.append(Row(name=inst, region=region, map=map_name, parent=None,
                                  unchained=False, conf="verified", src=GEO_SRC, oq=None))

    geo_map_names = {m["name"] for m in maps}

    # ---------------------------------------------------------------- complexes -> parents
    # A complex CONTAINS other instances (House of Crom, Warmonk Monastery). The parent is what
    # makes 152 rows of House of Crom loot reachable from one page instead of hanging off two
    # leaf dungeons with nothing above them.
    complexes = {}
    for name, c in geo["complexes"].items():
        if name.startswith("_"):
            continue
        complexes[name] = c
        if name not in geo_place_of:
            geo_place_of[name] = (c["region"], c["map"])
            places.append(Row(name=name, region=c["region"], map=c["map"], parent=None,
                              unchained=False, conf="verified",
                              src=GEO_SRC + " — complexes: an instance that contains instances",
                              oq=None))

    by_name = {p["name"]: p for p in places}
    for parent, c in complexes.items():
        for child in c["contains"]:
            if child not in by_name:
                die(f"complex {parent!r} names a child the geography does not list: {child!r}")
            by_name[child]["parent"] = parent

    # ⭐ THE PARENTHETICAL PARSER, and the reason it is a whitelist rather than a rule.
    # 'Threshold of Divinity (House of Crom)' is a PARENT. 'Caravan Raider Camp (Unchained)' is a
    # difficulty variant, and parsing that one the same way invents a place called "Unchained".
    # So a trailing parenthetical is only ever read as a parent when it names a known complex, and
    # anything else parenthesised stops this script instead of being guessed at.
    def split_parenthetical(name):
        """-> (base name, parent or None, unchained)."""
        if name.endswith(UNCHAINED_SUFFIX):
            return name[: -len(UNCHAINED_SUFFIX)], None, True
        if name.endswith(")") and " (" in name:
            base, _, tail = name.rpartition(" (")
            tail = tail[:-1]
            if tail in complexes:
                return base, tail, False
            die(f"unrecognised parenthetical in place name {name!r}: {tail!r} is neither a known "
                f"complex nor 'Unchained'. Resolve it deliberately; do not let it guess.")
        return name, None, False

    # The geography spells the two House of Crom dungeons with their parent in the name. The page
    # title is the dungeon's own name; the parent is the link.
    for p in list(places):
        base, parent, unch = split_parenthetical(p["name"])
        if parent:
            p["name"] = base
            p["parent"] = parent
    by_name = {p["name"]: p for p in places}

    # ---------------------------------------------------------------- armory maps
    # Maps the armory knows and Pierre's tree does not. Seeded (A* outranks) but flagged: an
    # extra map nobody confirmed is a filter option that may match nothing.
    geo_instance_names = set(geo_place_of)
    armory_map_rows = collections.Counter()
    for s in sources:
        if s.get("map"):
            armory_map_rows[(s["map"], s.get("region"))] += 1
    for (m, region), n in sorted(armory_map_rows.items()):
        if m in geo_map_names or m in mapless_named:
            continue
        if m in geo_instance_names:
            # The armory files an INSTANCE in its map column. Recorded on the place, below.
            continue
        maps.append(Row(name=m, region=region, conf="unconfirmed",
                        src=f"items_clean.json — {n} {plural(n)} filed under this map (Tier A*)",
                        oq="present in the armory as a map, absent from Pierre's geography — "
                           "confirm it is a map and not an instance"))
    geo_map_names = {m["name"] for m in maps}

    for p in places:
        if p["name"] in armory_map_rows or any(k[0] == p["name"] for k in armory_map_rows):
            n = sum(v for k, v in armory_map_rows.items() if k[0] == p["name"])
            p["oq"] = p["oq"] or (f"the armory files {n} {plural(n)} with this name in its MAP "
                                  f"column; Pierre's geography calls it an instance")

    # ---------------------------------------------------------------- armory places
    aliases = {k: v["canonical"] for k, v in geo["aliases"].items() if not k.startswith("_")}

    pairs = collections.defaultdict(collections.Counter)
    for s in sources:
        i, d = s.get("instance"), s.get("dungeon_or_raid")
        if not i and not d:
            continue
        if not i and d in NOT_A_PLACE:
            continue
        pairs[(i, d)][(s.get("region"), s.get("map"), bool(s.get("unchained")))] += 1

    def canonical(pair):
        """The seeded place a (instance, dungeon_or_raid) pair belongs to."""
        i, d = pair
        raw = i or d
        base, parent, unch = split_parenthetical(raw)
        base = aliases.get(base, base)
        if not unch and d and d.endswith(UNCHAINED_SUFFIX):
            unch = True        # instance drops the qualifier; the section header keeps it
        return base, parent, unch

    for pair in sorted(pairs, key=lambda k: (str(k[0]), str(k[1]))):
        i, d = pair
        base, parent, unch = canonical(pair)
        (region, map_name, _), _ = pairs[pair].most_common(1)[0]
        rows = sum(pairs[pair].values())

        # ⚠️ The source row carries its own `unchained` boolean, and it must not be quietly
        # outvoted by the spelling of the section header. Every row of a pair has to agree, or we
        # do not know which dungeon the items came from.
        flags = {k[2] for k in pairs[pair]}
        if len(flags) > 1:
            die(f"pair {pair!r} carries both unchained=true and unchained=false rows")
        flagged = flags.pop()
        unsuffixed_unchained = flagged and not unch
        unch = unch or flagged

        if base not in by_name and base not in UNRESOLVED_ARMORY_PLACES:
            die(f"the armory names a place the geography does not know and that is not listed in "
                f"UNRESOLVED_ARMORY_PLACES: {base!r} (from {pair!r}). Ask Pierre, then list it.")

        if base in UNRESOLVED_ARMORY_PLACES and base not in by_name:
            p = Row(name=base, region=region, map=map_name, parent=None, unchained=False,
                    conf="unconfirmed",
                    src=f"items_clean.json — {rows} {plural(rows)} (Tier A*)",
                    oq=UNRESOLVED_ARMORY_PLACES[base])
            places.append(p)
            by_name[base] = p

        # A name with no "(Unchained)" in it stays as it is — inventing
        # "Otherworldly Junction (Unchained)" because a column said true would be inventing a
        # place name. The flag is set on the existing place and the oddity is written down.
        target = base + UNCHAINED_SUFFIX if unch and not unsuffixed_unchained else base
        if unch and not unsuffixed_unchained and target not in by_name:
            # ⭐ AN UNCHAINED DUNGEON IS ITS OWN PLACE, not a flag on its twin: seven places exist
            # in both forms and ZERO items are shared between any pair (Pierre, 2026-09-13).
            # It inherits its twin's location, because the name is the twin's name.
            twin = by_name[base]
            p = Row(name=target, region=twin["region"], map=twin["map"], parent=twin["parent"],
                    unchained=True, conf=twin["conf"], inherited=twin["name"],
                    src=f"items_clean.json — {rows} {plural(rows)} (Tier A*); located with its "
                        f"non-Unchained twin, {twin['name']}",
                    oq=None)
            places.append(p)
            by_name[target] = p

        p = by_name[target]
        p["armory_instance"], p["armory_dungeon"] = i, d
        if not p["src"].startswith("items_clean.json"):
            p["src"] = f"{p['src']}; corroborated by {rows} armory {plural(rows)} (Tier A*)"
        if unsuffixed_unchained:
            p["unchained"] = True
            p["oq"] = p["oq"] or (
                f"every one of its {rows} armory rows is flagged Unchained, but neither source "
                f"spells the name that way and it has no non-Unchained twin — is this place "
                f"Unchained-only?")

        # ⚠️ Where the two sources put the place in DIFFERENT locations, both are recorded and the
        # row is `disputed`. sourcing-standards § 3: never a silent pick.
        held = (f"its twin {p['inherited']} is" if p.get("inherited") else "Pierre says it is")
        if region and p["region"] and region != p["region"]:
            p["conf"] = "disputed"
            p["oq"] = (f"sources disagree on where this is: {held} in {p['region']} / "
                       f"{p['map'] or 'no map'}, the armory files its {rows} rows under "
                       f"{region} / {map_name or 'no map'}")
        elif map_name and p["map"] and map_name != p["map"]:
            p["conf"] = "disputed"
            p["oq"] = (f"sources disagree on the map: {held} on {p['map']}, the armory files "
                       f"its {rows} rows under {map_name}")

    # A name the armory files in its boss column while Pierre's geography calls it a place is a
    # question about BOTH rows, so it is written on the place as well as kept out of the bosses.
    for name, why in NOT_A_BOSS.items():
        if name in by_name:
            by_name[name]["oq"] = by_name[name]["oq"] or why
            by_name[name]["conf"] = "disputed"

    # Every place must know its region, or the browse tree has a hole in it.
    for p in places:
        p.setdefault("armory_instance", None)
        p.setdefault("armory_dungeon", None)
        if not p["region"]:
            die(f"place {p['name']!r} has no region")
        if p["map"] and p["map"] not in geo_map_names:
            die(f"place {p['name']!r} references map {p['map']!r}, which is not seeded")

    # No two places may claim the same armory pair, or the importer's join is ambiguous.
    seen = {}
    for p in places:
        key = (p["armory_instance"], p["armory_dungeon"])
        if key == (None, None):
            continue
        if key in seen:
            die(f"places {seen[key]!r} and {p['name']!r} both claim armory pair {key!r}")
        seen[key] = p["name"]

    place_names = {p["name"] for p in places}

    # ---------------------------------------------------------------- bosses
    boss_rows = collections.defaultdict(collections.Counter)
    for s in sources:
        b = s.get("boss_or_npc")
        if not b:
            continue
        i, d = s.get("instance"), s.get("dungeon_or_raid")
        place = None
        if i or d:
            if not (not i and d in NOT_A_PLACE):
                base, _, unch = canonical((i, d))
                place = base + UNCHAINED_SUFFIX if unch and base + UNCHAINED_SUFFIX in place_names else base
        boss_rows[b][(place, s.get("map"), s.get("region"))] += 1

    bosses = []
    for name in sorted(boss_rows):
        if name in NOT_A_BOSS:
            continue
        spots = boss_rows[name]
        (place, map_name, region), _ = spots.most_common(1)[0]
        n = sum(spots.values())
        if map_name and map_name not in geo_map_names:
            map_name = None
        confirmed = name in CONFIRMED_BOSSES_WITHOUT_A_PLACE
        oq = BOSS_OPEN_QUESTIONS.get(name)
        if len({s[0] for s in spots}) > 1:
            oq = oq or ("the armory files this boss in more than one place: "
                        + ", ".join(sorted(str(s[0]) for s in spots)))
        bosses.append(Row(
            name=name, place=place, map=map_name, region=region,
            conf="verified" if confirmed else "unconfirmed",
            src=("Pierre's research, 2026-09-13 (open_questions.md § 2) — confirmed a boss, not a "
                 f"place; located by {n} armory {plural(n)} (Tier A*)") if confirmed
                else f"items_clean.json — {n} {plural(n)} name it (Tier A*)",
            oq=oq))

    # ---------------------------------------------------------------- quests
    quest_rows = collections.defaultdict(collections.Counter)
    for s in sources:
        if s.get("quest"):
            quest_rows[s["quest"]][(s.get("map"), s.get("region"))] += 1

    region_names = {r["name"] for r in regions}
    quests = []
    for label in sorted(quest_rows):
        (map_name, region), _ = quest_rows[label].most_common(1)[0]
        n = sum(quest_rows[label].values())
        # ⚠️ No blanket caveat per row. Every quest here has a NULL name for the same reason, and
        # repeating one sentence 51 times teaches a reader to skip the column that is supposed to
        # hold the things worth reading. The caveat lives in the table comment; `WHERE name IS
        # NULL` is the query. open_question is for what is specific to THIS row.
        oq = None
        if label in place_names or label in geo_map_names or label in region_names:
            what = ("a place" if label in place_names
                    else "a map" if label in geo_map_names else "a region")
            oq = f"this label is the name of {what}, so it is not the quest's name"
        if map_name and map_name not in geo_map_names:
            oq = (f"the armory files it under map '{map_name}', which is not a seeded map — "
                  f"Pierre's geography calls that an instance")
            map_name = None
        quests.append(Row(label=label, map=map_name, region=region, conf="unconfirmed",
                          src=f"items_clean.json — {n} {plural(n)} (Tier A*)", oq=oq))

    # ---------------------------------------------------------------- containers
    # ⚠️ NOT places. They have no region or map of their own: the dungeons that drop them are on
    # the item's own row and may span regions (Pierre, 2026-09-13).
    armory_containers = collections.Counter(s["container"] for s in sources if s.get("container"))
    containers = []
    for name, c in geo["containers"].items():
        if name.startswith("_"):
            continue
        if name not in armory_containers:
            die(f"the geography lists container {name!r}, which no armory row mentions")
        containers.append(Row(name=name, conf="verified",
                              src=f"{GEO_SRC}; {armory_containers[name]} armory {plural(armory_containers[name])}",
                              oq=None))
    for name in armory_containers:
        if name not in {c["name"] for c in containers}:
            die(f"the armory names container {name!r}, which the geography does not list")

    # ---------------------------------------------------------------- caps
    for label, rows, key in (("place", places, "name"), ("boss", bosses, "name"),
                             ("quest", quests, "label"), ("map", maps, "name"),
                             ("region", regions, "name"), ("container", containers, "name")):
        for r in rows:
            if len(r[key]) > NAME_CAP:
                die(f"{label} name is {len(r[key])} chars, over the {NAME_CAP} cap: {r[key]!r}")
            for f in ("src", "oq"):
                if r.get(f) and len(r[f]) > NOTE_CAP:
                    die(f"{label} {r[key]!r} {f} is {len(r[f])} chars, over the {NOTE_CAP} cap")

    # ---------------------------------------------------------------- emit
    o = print
    o("-- GENERATED by scripts/gen_places_seed.py — do not hand-edit.")
    o("-- Regenerate against a newer snapshot and diff; zero diff means the places have not moved.")
    o("--")
    o("-- ⭐ PLACES ARE ENTITIES WITH PAGES, NOT STRINGS ON AN ITEM (DECISIONS.md, 2026-09-13), and")
    o("-- they are a HIERARCHY: region -> map -> place -> place. Some instances contain instances")
    o("-- (House of Crom, Warmonk Monastery) and some hang straight off a region with no map at all")
    o("-- (Skull Gate Pass, Kuthchemes) — which is why parent_place_id exists and map_id is NULL-able.")
    o("--")
    o("-- ⭐ AN UNCHAINED DUNGEON IS ITS OWN ROW, not a flag on its twin: seven places exist in both")
    o("-- forms and ZERO items are shared between any pair (Pierre, 2026-09-13). The boolean is kept")
    o("-- as a cheap filter — 'show me every Unchained dungeon' — but the PLACE is the truth.")
    o("--")
    o("-- ⚠️ EVERY ROW CARRIES ITS PROVENANCE: source_note says which of the two sources it came")
    o("-- from, confidence follows reference/sourcing-standards.md § 3, and open_question holds what")
    o("-- is still unknown. `WHERE open_question IS NOT NULL` is the list of things to ask Pierre.")
    o("-- An empty field is a feature; a confident guess is a bug that reaches a raid.")
    o("--")
    o("-- ⚠️ EVERY SEED IS `ON CONFLICT DO NOTHING` (AOC-005's convention, pinned by")
    o("-- TestEverySeedInsertIsIdempotent).")
    o("")
    o("-- +goose Up")
    o("")

    o("-- The confidence vocabulary of reference/sourcing-standards.md § 3, as rows rather than an")
    o("-- ENUM, because a fifth value must be an INSERT and not a migration of every content table.")
    o("CREATE TABLE confidence_levels (")
    o("    id         serial PRIMARY KEY,")
    o("    slug       varchar(64) NOT NULL UNIQUE,")
    o("    name       varchar(64) NOT NULL UNIQUE,")
    o("    sort_order integer     NOT NULL")
    o(");")
    o("")

    def provenance(indent=4):
        pad = " " * indent
        o(f"{pad}confidence_id integer       NOT NULL REFERENCES confidence_levels(id),")
        o(f"{pad}source_note   varchar({NOTE_CAP}) NOT NULL,")
        o(f"{pad}open_question varchar({NOTE_CAP}) NULL")

    o(f"-- {len(regions)} regions. Pierre confirmed the list is COMPLETE (Tier A, 2026-09-13); the")
    o("-- armory knows only 6 of them and would silently lose Tortage and Kush.")
    o("CREATE TABLE regions (")
    o("    id            serial PRIMARY KEY,")
    o("    slug          varchar(64)  NOT NULL UNIQUE,")
    o("    name          varchar(64)  NOT NULL UNIQUE,")
    o("    sort_order    integer      NOT NULL,")
    provenance()
    o(");")
    o("")

    o(f"-- {len(maps)} maps — the playfield an instance's entrance sits in.")
    o("-- region_id is NULL-able on purpose: a map the armory knows and Pierre's tree does not may")
    o("-- arrive before anyone can say which region it belongs to.")
    o("CREATE TABLE maps (")
    o("    id            serial PRIMARY KEY,")
    o("    region_id     integer      NULL REFERENCES regions(id),")
    o(f"    slug          varchar(64)  NOT NULL UNIQUE,")
    o(f"    name          varchar({NAME_CAP})  NOT NULL UNIQUE,")
    provenance()
    o(");")
    o("")

    o(f"-- {len(places)} places: the dungeons, raids and arenas you zone into.")
    o("--")
    o("-- ⚠️ map_id is NULL-able and region_id is NOT — not the other way round. Skull Gate Pass and")
    o("-- Kuthchemes are entered from the Raid Finder and have no map between them and their region,")
    o("-- so a NOT NULL map_id would have no value to put there but a made-up one.")
    o("--")
    o("-- ⚠️ region_id is carried even when map_id is set. It is redundant by design: 'every place in")
    o("-- Stygia' is then one join rather than a COALESCE over two paths, and the generator is the")
    o("-- only writer, so the two cannot drift. Pinned by TestEveryPlacesRegionMatchesItsMap.")
    o("--")
    o("-- armory_instance / armory_dungeon are the source spellings this place was filed under, kept")
    o("-- so AOC-011 can join 6,641 item source rows onto these rows without re-deriving the mapping")
    o("-- from scratch. They are the alias record too: 'The Pit Master's Arena' is how the armory")
    o("-- spells Fight Club Arena.")
    o("CREATE TABLE places (")
    o("    id              serial PRIMARY KEY,")
    o("    region_id       integer      NOT NULL REFERENCES regions(id),")
    o("    map_id          integer      NULL REFERENCES maps(id),")
    o("    parent_place_id integer      NULL REFERENCES places(id),")
    o(f"    slug            varchar({NAME_CAP})  NOT NULL UNIQUE,")
    o(f"    name            varchar({NAME_CAP})  NOT NULL UNIQUE,")
    o("    unchained       boolean      NOT NULL DEFAULT false,")
    o(f"    armory_instance varchar({NAME_CAP})  NULL,")
    o(f"    armory_dungeon  varchar({NAME_CAP})  NULL,")
    provenance()
    o(");")
    o("")
    o("-- The importer's join key. Partial, because the places Pierre knows and the armory never")
    o("-- filed an item under share (NULL, NULL) and must not collide with each other.")
    o("CREATE UNIQUE INDEX places_armory_key ON places (armory_instance, armory_dungeon)")
    o("    NULLS NOT DISTINCT")
    o("    WHERE armory_instance IS NOT NULL OR armory_dungeon IS NOT NULL;")
    o("CREATE INDEX places_region_id_idx ON places (region_id);")
    o("CREATE INDEX places_map_id_idx ON places (map_id);")
    o("CREATE INDEX places_parent_place_id_idx ON places (parent_place_id);")
    o("")

    o(f"-- {len(bosses)} bosses. place_id is NULL-able: five of them were confirmed by Pierre as")
    o("-- bosses the armory had filed as places, and they keep their map and region but have no")
    o("-- instance recorded. EP-03 hangs the mechanics — the actual product — off these rows.")
    o("CREATE TABLE bosses (")
    o("    id            serial PRIMARY KEY,")
    o("    place_id      integer      NULL REFERENCES places(id),")
    o("    map_id        integer      NULL REFERENCES maps(id),")
    o("    region_id     integer      NULL REFERENCES regions(id),")
    o(f"    slug          varchar({NAME_CAP})  NOT NULL UNIQUE,")
    o(f"    name          varchar({NAME_CAP})  NOT NULL UNIQUE,")
    provenance()
    o(");")
    o("CREATE INDEX bosses_place_id_idx ON bosses (place_id);")
    o("")

    o(f"-- {len(quests)} quests — and `name` is NULL for every one of them, on purpose.")
    o("-- The armory's quest column holds the quest GIVER ('Muriela'), a hub ('Conall's Valley') or")
    o("-- a bucket ('Destiny quests') as often as a quest title, so armory_label records exactly what")
    o("-- the source said and `name` waits for someone who knows. An empty field is a feature.")
    o("CREATE TABLE quests (")
    o("    id            serial PRIMARY KEY,")
    o("    region_id     integer      NULL REFERENCES regions(id),")
    o("    map_id        integer      NULL REFERENCES maps(id),")
    o(f"    slug          varchar({NAME_CAP})  NOT NULL UNIQUE,")
    o(f"    armory_label  varchar({NAME_CAP})  NOT NULL UNIQUE,")
    o(f"    name          varchar({NAME_CAP})  NULL,")
    provenance()
    o(");")
    o("")

    o(f"-- {len(containers)} containers. ⚠️ NOT places, which is the whole reason they have their own")
    o("-- table: they have no region and no map, and the dungeons that drop them span several.")
    o("CREATE TABLE containers (")
    o("    id            serial PRIMARY KEY,")
    o(f"    slug          varchar({NAME_CAP})  NOT NULL UNIQUE,")
    o(f"    name          varchar({NAME_CAP})  NOT NULL UNIQUE,")
    provenance()
    o(");")
    o("")

    o(f"-- {len(CONFIDENCE)} rows")
    o("INSERT INTO confidence_levels (slug, name, sort_order) VALUES")
    o(",\n".join(f"    ({q(s)}, {q(n)}, {so})" for s, n, so in CONFIDENCE))
    o("ON CONFLICT (slug) DO NOTHING;")
    o("")

    def prov_values(r):
        return f"{ref('confidence_levels', r['conf'])}, {q(r['src'])}, {q(r['oq'])}"

    o(f"-- {len(regions)} rows")
    o("INSERT INTO regions (slug, name, sort_order, confidence_id, source_note, open_question) VALUES")
    o(",\n".join(f"    ({q(slug(r['name']))}, {q(r['name'])}, {r['sort_order']}, {prov_values(r)})"
                 for r in regions))
    o("ON CONFLICT (slug) DO NOTHING;")
    o("")

    o(f"-- {len(maps)} rows")
    o("INSERT INTO maps (region_id, slug, name, confidence_id, source_note, open_question) VALUES")
    o(",\n".join(f"    ({ref('regions', m['region'])}, {q(slug(m['name']))}, {q(m['name'])}, "
                 f"{prov_values(m)})" for m in sorted(maps, key=lambda m: m["name"])))
    o("ON CONFLICT (slug) DO NOTHING;")
    o("")

    # ⚠️ Parents go in their own statement, BEFORE their children. A sub-select inside a single
    # multi-row INSERT sees the table as it was when the statement started, so a child in the same
    # statement as its parent resolves parent_place_id to NULL — silently.
    roots = sorted((p for p in places if not p["parent"]), key=lambda p: p["name"])
    children = sorted((p for p in places if p["parent"]), key=lambda p: p["name"])

    def place_values(p):
        return (f"    ({ref('regions', p['region'])}, {ref('maps', p['map'])}, "
                f"{ref('places', p['parent'])}, {q(slug(p['name']))}, {q(p['name'])}, "
                f"{'true' if p['unchained'] else 'false'}, {q(p['armory_instance'])}, "
                f"{q(p['armory_dungeon'])}, {prov_values(p)})")

    cols = ("region_id, map_id, parent_place_id, slug, name, unchained, armory_instance, "
            "armory_dungeon, confidence_id, source_note, open_question")
    o(f"-- {len(roots)} rows — every place that is not inside another one")
    o(f"INSERT INTO places ({cols}) VALUES")
    o(",\n".join(place_values(p) for p in roots))
    o("ON CONFLICT (slug) DO NOTHING;")
    o("")
    o(f"-- {len(children)} rows — the two complexes' dungeons, in a SEPARATE statement so the")
    o("-- parent_place_id sub-select can see the rows above.")
    o(f"INSERT INTO places ({cols}) VALUES")
    o(",\n".join(place_values(p) for p in children))
    o("ON CONFLICT (slug) DO NOTHING;")
    o("")

    o(f"-- {len(bosses)} rows")
    o("INSERT INTO bosses (place_id, map_id, region_id, slug, name, confidence_id, source_note, "
      "open_question) VALUES")
    o(",\n".join(f"    ({ref('places', b['place'])}, {ref('maps', b['map'])}, "
                 f"{ref('regions', b['region'])}, {q(slug(b['name']))}, {q(b['name'])}, "
                 f"{prov_values(b)})" for b in bosses))
    o("ON CONFLICT (slug) DO NOTHING;")
    o("")

    o(f"-- {len(quests)} rows. name stays NULL — see the table comment.")
    o("INSERT INTO quests (region_id, map_id, slug, armory_label, confidence_id, source_note, "
      "open_question) VALUES")
    o(",\n".join(f"    ({ref('regions', t['region'])}, {ref('maps', t['map'])}, "
                 f"{q(slug(t['label']))}, {q(t['label'])}, {prov_values(t)})" for t in quests))
    o("ON CONFLICT (slug) DO NOTHING;")
    o("")

    o(f"-- {len(containers)} rows")
    o("INSERT INTO containers (slug, name, confidence_id, source_note, open_question) VALUES")
    o(",\n".join(f"    ({q(slug(c['name']))}, {q(c['name'])}, {prov_values(c)})"
                 for c in containers))
    o("ON CONFLICT (slug) DO NOTHING;")
    o("")

    o("-- +goose Down")
    o("DROP TABLE IF EXISTS containers;")
    o("DROP TABLE IF EXISTS quests;")
    o("DROP TABLE IF EXISTS bosses;")
    o("DROP TABLE IF EXISTS places;")
    o("DROP TABLE IF EXISTS maps;")
    o("DROP TABLE IF EXISTS regions;")
    o("DROP TABLE IF EXISTS confidence_levels;")

    print(f"seeded: {len(regions)} regions, {len(maps)} maps, {len(places)} places "
          f"({len(children)} with a parent, {sum(1 for p in places if p['unchained'])} unchained, "
          f"{sum(1 for p in places if p['oq'])} with an open question), {len(bosses)} bosses, "
          f"{len(quests)} quests, {len(containers)} containers", file=sys.stderr)
    return 0


if __name__ == "__main__":
    sys.exit(main())
