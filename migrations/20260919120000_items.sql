-- AOC-010 — the item schema: 4,648 items, 23,074 stat values, 6,641 item×source rows.
--
-- Shapes deliberately taken from the cleaned snapshot (armory_snapshot/items_clean.json) rather
-- than invented and translated at import time, because the import is the risky step and every
-- translation is somewhere a fact can change shape without anyone noticing.
--
-- ⭐ items.item_id KEEPS THE SOURCE SITE'S OWN ID. It is stable, it is the join back to the
-- snapshot, and after the source host lapses (~Feb 2027) it is the only key the original data and
-- ours still share. It is NOT re-numbered, so there is no `serial` here.
--
-- ⭐ EQUIP LOCATION IS A JOIN, NOT A COLUMN. Two of the snapshot's 16 values are COMPOUND:
-- `Main Hand, Off Hand` (390 items — a two-hander occupying BOTH slots) and `Left/Right Finger`
-- (188 items — a ring occupying EITHER slot). AOC-009 seeded only the 13 atomic slots on purpose.
-- Flattened into one column, "show me every Off Hand item" silently misses 390 two-handers, which
-- is the kind of wrong answer that reaches a raid. `items.slot_fit_id` says how to read an item's
-- rows; the qualifier lives on the ITEM because it describes the whole set and two join rows could
-- otherwise contradict each other.
--
-- ⭐ SPELL EFFECTS ARE A SEPARATE TABLE FROM STATS, not a flag on one. A build or armour
-- calculator must sum `item_stats` and never `item_spell_effects`, or eight mounts hand every
-- wearer -8% Sprinting Stamina Drain (AOC-016). Structure beats a rule somebody has to remember —
-- the same argument this schema already makes for sets.set_armour_weight_id.
--
-- ⚠️ STAT VALUES ARE numeric(8,2), NOT integer. The 2026-09-13 decision said "integers" meaning
-- arithmetic-not-text, and 480 rows disprove the literal reading: Natural Mana Regen (314),
-- Natural Stamina Regen (131) and Natural Health Regen (35) carry values like 4.5, 1.6 and 2.4.
-- An integer column would truncate 4.5 to 4 and quietly wrong every regen number on the site.
-- numeric, not float, because the armoury sums these and binary floating point does not add up.
--
-- ⚠️ sets.set_armour_weight_id IS WHAT THAT SET'S PIECES WEIGH. It is NOT a class ceiling
-- (classes.max_armour_weight is). These two have been conflated once already; separate columns on
-- separate tables is where that stops being possible (AOC-009).
--
-- ⚠️ boss_id AND vendor_id ARE SEPARATE, and a source row uses at most one. The snapshot's single
-- `boss_or_npc` field conflated real bosses with vendor names and with `Unchained` (a difficulty,
-- not an NPC). AOC-017 split them at the source; this schema must not re-merge them.
--
-- ⚠️ EVERY GAME-FACT ROW CARRIES ITS PROVENANCE — confidence_id, source_note, open_question
-- (DECISIONS.md 2026-09-18; reference/content-model.md § 6 names `items` and the loot join
-- explicitly). A provenance that lives only in a dossier cannot be rendered, and § 6 requires a
-- `disputed` fact to show a ⚠️ and both versions, which a page can only do if the row says so.

-- +goose Up

-- Trigram search for the Armory's name box. Production's Postgres runs as `postgres` (checked, so
-- this is not a hopeful CREATE EXTENSION that fails on first deploy).
CREATE EXTENSION IF NOT EXISTS pg_trgm;

-- How to read an item's equip-location rows. A table and not an ENUM, like every other taxonomy:
-- a fourth value must be an INSERT, not a migration of the table that references it.
CREATE TABLE slot_fits (
    id         serial       PRIMARY KEY,
    slug       varchar(64)  NOT NULL UNIQUE,
    name       varchar(64)  NOT NULL UNIQUE,
    sort_order integer      NOT NULL
);

INSERT INTO slot_fits (slug, name, sort_order) VALUES
    ('single', 'One slot',              1),
    ('both',   'Both slots at once',    2),
    ('either', 'Either slot, not both', 3)
ON CONFLICT (slug) DO NOTHING;

-- 368 distinct sets in the snapshot.
CREATE TABLE sets (
    id                   serial       PRIMARY KEY,
    slug                 varchar(120) NOT NULL UNIQUE,
    name                 varchar(120) NOT NULL,
    class_id             integer      REFERENCES classes(id),
    -- what the PIECES weigh. Not a class ceiling. See the header.
    set_armour_weight_id integer      REFERENCES armour_weights(id),
    confidence_id        integer      NOT NULL REFERENCES confidence_levels(id),
    source_note          varchar(300) NOT NULL,
    open_question        varchar(300)
);

CREATE TABLE items (
    -- the source site's own id. Never re-numbered; see the header.
    item_id             integer      PRIMARY KEY,
    slug                varchar(160) NOT NULL,
    name                varchar(160) NOT NULL,
    rarity_id           integer      NOT NULL REFERENCES rarities(id),
    item_type_id        integer      NOT NULL REFERENCES item_types(id),
    -- null for the 340 items with no slot at all (consumables, quest items, mounts)
    slot_fit_id         integer      REFERENCES slot_fits(id),
    armour_weight_id    integer      REFERENCES armour_weights(id),
    binding_id          integer      REFERENCES bindings(id),
    item_level          integer,
    requires_level      integer,
    armor               integer,
    critigation         integer,
    -- 16.5 .. 157.1 in the snapshot — fractional, so not an integer column
    dps                 numeric(6,2),
    damage_range        varchar(32),
    set_id              integer      REFERENCES sets(id),
    faction_id          integer      REFERENCES factions(id),
    faction_rank        integer,
    pvp_source          boolean      NOT NULL DEFAULT false,
    has_pvp_stats       boolean      NOT NULL DEFAULT false,
    pvp_penalty         boolean      NOT NULL DEFAULT false,
    -- 2 items carry the site's reason string. Whether the import skips them is AOC-011's call;
    -- the column exists so the schema does not decide it by omission.
    no_longer_available varchar(300),
    -- rewritten to our own host by AOC-008; the source URL is the provenance trail and after the
    -- origin lapses it is the only record of where the image came from. Never overwrite it.
    tooltip_image       varchar(500),
    tooltip_source_url  varchar(500),
    confidence_id       integer      NOT NULL REFERENCES confidence_levels(id),
    source_note         varchar(300) NOT NULL,
    open_question       varchar(300)
);

-- The join the compound equip_location values need. 390 items have two rows with fit `both`,
-- 188 have two rows with fit `either`, 3,730 have exactly one, and 340 have none.
CREATE TABLE item_equip_locations (
    item_id           integer NOT NULL REFERENCES items(item_id) ON DELETE CASCADE,
    equip_location_id integer NOT NULL REFERENCES equip_locations(id),
    PRIMARY KEY (item_id, equip_location_id)
);

-- 48 distinct stat names, 23,074 rows. `stat` stays text here rather than a lookup because the
-- name is what the tooltip said and a new stat must not require a migration; the Armory filters on
-- it, so it is indexed. sign is -1 or 1; unit is 'flat' or 'percent'.
CREATE TABLE item_stats (
    id          bigserial     PRIMARY KEY,
    item_id     integer       NOT NULL REFERENCES items(item_id) ON DELETE CASCADE,
    stat        varchar(64)   NOT NULL,
    value       numeric(8,2)  NOT NULL,
    sign        smallint      NOT NULL,
    unit        varchar(16)   NOT NULL,
    damage_type varchar(32),
    pvp         boolean       NOT NULL DEFAULT false,
    CONSTRAINT item_stats_sign_check CHECK (sign IN (-1, 1))
);

-- Same shape as item_stats, deliberately a different table. 19 rows on 9 items today.
CREATE TABLE item_spell_effects (
    id          bigserial     PRIMARY KEY,
    item_id     integer       NOT NULL REFERENCES items(item_id) ON DELETE CASCADE,
    stat        varchar(64)   NOT NULL,
    value       numeric(8,2)  NOT NULL,
    sign        smallint      NOT NULL,
    unit        varchar(16)   NOT NULL,
    damage_type varchar(32),
    pvp         boolean       NOT NULL DEFAULT false,
    CONSTRAINT item_spell_effects_sign_check CHECK (sign IN (-1, 1))
);

-- 6,641 rows: one per item × where it comes from. item → place is MANY-TO-MANY (422 items link to
-- more than one place: 193 to two, 229 to three) and this table carries that naturally, one row
-- each, never a merged name like 'Halls of Eternal Frost; Scorpion Cave'.
CREATE TABLE item_sources (
    id                  bigserial    PRIMARY KEY,
    item_id             integer      NOT NULL REFERENCES items(item_id) ON DELETE CASCADE,
    acquisition_type_id integer      REFERENCES acquisition_types(id),
    place_id            integer      REFERENCES places(id),
    -- at most one of these two is set; see the header
    boss_id             integer      REFERENCES bosses(id),
    vendor_id           integer      REFERENCES places(id),
    quest_id            integer      REFERENCES quests(id),
    container_id        integer      REFERENCES containers(id),
    region_id           integer      REFERENCES regions(id),
    map_id              integer      REFERENCES maps(id),
    tier_id             integer      REFERENCES tiers(id),
    is_raid             boolean      NOT NULL DEFAULT false,
    -- on a quest row this is where the QUEST-GIVER stands, not a dungeon entrance (AOC-017)
    coords              varchar(64),
    section_raw         varchar(300),
    unchained           boolean      NOT NULL DEFAULT false,
    confidence_id       integer      NOT NULL REFERENCES confidence_levels(id),
    source_note         varchar(300) NOT NULL,
    open_question       varchar(300),
    CONSTRAINT item_sources_boss_xor_vendor CHECK (boss_id IS NULL OR vendor_id IS NULL)
);

-- 2,588 of the 6,641 source rows carry a price. A source can cost more than one token, so this is
-- its own table and not two columns.
CREATE TABLE item_costs (
    id             bigserial     PRIMARY KEY,
    item_source_id bigint        NOT NULL REFERENCES item_sources(id) ON DELETE CASCADE,
    currency_id    integer       NOT NULL REFERENCES currencies(id),
    amount         numeric(12,2) NOT NULL
);

-- 4,259 rows. Empty for an item with no class restriction, which is most of them.
CREATE TABLE item_classes (
    item_id  integer NOT NULL REFERENCES items(item_id) ON DELETE CASCADE,
    class_id integer NOT NULL REFERENCES classes(id),
    PRIMARY KEY (item_id, class_id)
);

-- Indexes. Each one is here for a query the Armory page actually runs, not for completeness.

-- "every Epic item", "every Chest piece" — the two filters the list page opens with
CREATE INDEX items_rarity_id_idx    ON items (rarity_id);
CREATE INDEX items_item_type_id_idx ON items (item_type_id);
-- set pages, and "other pieces of this set" on an item page
CREATE INDEX items_set_id_idx       ON items (set_id) WHERE set_id IS NOT NULL;
-- the name search box
CREATE INDEX items_name_trgm_idx    ON items USING gin (name gin_trgm_ops);
-- the list page's stable sort, so pagination does not need a full sort each page
CREATE INDEX items_name_idx         ON items (name);
CREATE UNIQUE INDEX items_slug_idx  ON items (slug);

-- "show me every Off Hand item" — reads the join, which is the whole point of it existing
CREATE INDEX item_equip_locations_equip_location_id_idx ON item_equip_locations (equip_location_id);

-- an item page reads all of its stats; the Armory filters "items with Strength"
CREATE INDEX item_stats_item_id_idx ON item_stats (item_id);
CREATE INDEX item_stats_stat_idx    ON item_stats (stat);
CREATE INDEX item_spell_effects_item_id_idx ON item_spell_effects (item_id);

-- an item page reads its sources; a PLACE page reads the reverse — "what drops here"
CREATE INDEX item_sources_item_id_idx  ON item_sources (item_id);
CREATE INDEX item_sources_place_id_idx ON item_sources (place_id) WHERE place_id IS NOT NULL;
CREATE INDEX item_sources_boss_id_idx  ON item_sources (boss_id)  WHERE boss_id IS NOT NULL;
-- a CURRENCY page: "what does this token buy?" (DECISIONS.md 2026-09-14 — currency is an entity)
CREATE INDEX item_costs_currency_id_idx    ON item_costs (currency_id);
CREATE INDEX item_costs_item_source_id_idx ON item_costs (item_source_id);

-- "every item a Barbarian can wear"
CREATE INDEX item_classes_class_id_idx ON item_classes (class_id);

-- +goose Down
DROP TABLE IF EXISTS item_classes;
DROP TABLE IF EXISTS item_costs;
DROP TABLE IF EXISTS item_sources;
DROP TABLE IF EXISTS item_spell_effects;
DROP TABLE IF EXISTS item_stats;
DROP TABLE IF EXISTS item_equip_locations;
DROP TABLE IF EXISTS items;
DROP TABLE IF EXISTS sets;
DROP TABLE IF EXISTS slot_fits;
-- pg_trgm is left installed: it is cheap, it is shared, and dropping an extension another
-- migration may later depend on is a worse failure than leaving it.
