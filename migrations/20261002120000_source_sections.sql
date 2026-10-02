-- +goose Up
-- AOC-050: the Armory's sources panel becomes DATA. Pierre, 2026-10-02 (DECISIONS.md): the main
-- categories under the search are PVE, PVP, Region, Faction, Onslaught and Other, one active at a
-- time, and each holds some of AoC>TV's 39 armory sections — HIS regrouping, not AoC>TV's layout.
-- A tab is a kind of thing (he has already added one), so it is a row; a section's tab is a column;
-- moving a section is an UPDATE (reference/content-model.md § 0).

-- The tabs. `groups` is the levels a tab draws ABOVE the location, from a fixed vocabulary over our
-- own columns (section, region, map); every tab then draws location › boss. `levels_note` is the
-- panel's wording, UI copy and not a game fact: where the design names a level the data does not
-- record (Region's "mob", PvP's "instance › bracket"), the note says what is actually drawn.
CREATE TABLE source_tabs (
    id          serial      PRIMARY KEY,
    slug        varchar(32) NOT NULL UNIQUE,
    name        varchar(32) NOT NULL,
    sort_order  integer     NOT NULL,
    levels_note varchar(64) NOT NULL,
    groups      varchar(16)[] NOT NULL,
    CONSTRAINT source_tabs_groups_known CHECK (groups <@ ARRAY['section', 'region', 'map']::varchar[])
);

INSERT INTO source_tabs (slug, name, sort_order, levels_note, groups) VALUES
    ('pve',       'PVE',       10, 'tier › raid › boss',           '{section}'),
    ('pvp',       'PVP',       20, 'tier › vendor',                '{section}'),
    ('region',    'Region',    30, 'region › zone › location',     '{region,map}'),
    ('faction',   'Faction',   40, 'faction › map › instance',     '{section,map}'),
    ('onslaught', 'Onslaught', 50, 'category › region › location', '{section,region}'),
    ('other',     'Other',     60, 'category › region › vendor',   '{section,region}')
ON CONFLICT (slug) DO NOTHING;

-- AoC>TV's sections, `name` VERBATIM as the snapshot spells them (item_sources.section_raw), each in
-- the tab Pierre gave it on 2026-10-02. Order within a tab: the tiers by number, the rest by name.
CREATE TABLE sections (
    id         serial      PRIMARY KEY,
    slug       varchar(64) NOT NULL UNIQUE,
    name       varchar(64) NOT NULL UNIQUE,
    tab_id     integer     NOT NULL REFERENCES source_tabs(id),
    sort_order integer     NOT NULL
);
CREATE INDEX sections_tab_id_idx ON sections (tab_id);

INSERT INTO sections (slug, name, tab_id, sort_order)
SELECT v.slug, v.name, t.id, v.sort_order
FROM (VALUES
    ('pve-tier-1',            'PvE Tier 1',            'pve',       10),
    ('pve-tier-2',            'PvE Tier 2',            'pve',       20),
    ('pve-tier-3',            'PvE Tier 3',            'pve',       30),
    ('pve-tier-3-5',          'PvE Tier 3.5',          'pve',       35),
    ('pve-tier-4',            'PvE Tier 4',            'pve',       40),
    ('pve-tier-5',            'PvE Tier 5',            'pve',       50),
    ('pve-tier-6',            'PvE Tier 6',            'pve',       60),
    ('pvp-tier-1',            'PvP Tier 1',            'pvp',       10),
    ('pvp-tier-2',            'PvP Tier 2',            'pvp',       20),
    ('pvp-tier-3',            'PvP Tier 3',            'pvp',       30),
    -- Region: the five regions, and House of Crom and Dragon's Spine (Pierre). The Region tab's
    -- first level is the row's region, not the section, so their order here is only a tie-break.
    ('aquilonia',             'Aquilonia',             'region',    10),
    ('cimmeria',              'Cimmeria',              'region',    20),
    ('dragon-s-spine',        'Dragon''s Spine',       'region',    30),
    ('house-of-crom',         'House of Crom',         'region',    40),
    ('khitai',                'Khitai',                'region',    50),
    ('stygia',                'Stygia',                'region',    60),
    ('turan',                 'Turan',                 'region',    70),
    ('brittle-blade',         'Brittle Blade',         'faction',   10),
    ('children-of-yag-kosha', 'Children of Yag-kosha', 'faction',   20),
    ('clan-vigdis',           'Clan Vigdis',           'faction',   30),
    ('hyrkanians',            'Hyrkanians',            'faction',   40),
    ('jiang-shi',             'Jiang Shi',             'faction',   50),
    ('last-legion',           'Last Legion',           'faction',   60),
    ('scarlet-circle',        'Scarlet Circle',        'faction',   70),
    ('scholars-of-cheng-ho',  'Scholars of Cheng-Ho',  'faction',   80),
    ('shadows-of-jade',       'Shadows of Jade',       'faction',   90),
    ('tamarin-s-tigers',      'Tamarin''s Tigers',     'faction',  100),
    ('wolves-of-the-steppes', 'Wolves of the Steppes', 'faction',  110),
    ('yellow-priests-of-yun', 'Yellow Priests of Yun', 'faction',  120),
    -- Onslaught: Pierre's new category, next to Faction.
    ('kutchemes-temple',      'Kutchemes Temple',      'onslaught', 10),
    ('skull-gate-pass',       'Skull Gate Pass',       'onslaught', 20),
    -- Other: everything that is not PvE, PvP, Region, Faction or Onslaught (Pierre).
    ('bags',                  'Bags',                  'other',     10),
    ('consumable-books',      'Consumable Books',      'other',     20),
    ('item-store',            'Item Store',            'other',     30),
    ('raidfinder',            'Raidfinder',            'other',     40),
    ('recipes',               'Recipes',               'other',     50),
    ('unchained',             'Unchained',             'other',     60),
    ('unsorted-items',        'Unsorted Items',        'other',     70),
    ('world-boss',            'World Boss',            'other',     80)
) AS v(slug, name, tab, sort_order)
JOIN source_tabs t ON t.slug = v.tab
ON CONFLICT (slug) DO NOTHING;

-- The design's two halves of a location: "loot / drops" and "quest / vendor". Which acquisition type
-- falls in which is a grouping of lookup rows, so it is data too. Nullable, like every lookup here
-- (TestNothingIsNotNullByAccident): a type added later with no group falls in neither half, as the 17
-- source rows with no acquisition type do. The three that exist are checked below.
CREATE TABLE acquisition_groups (
    id         serial      PRIMARY KEY,
    slug       varchar(32) NOT NULL UNIQUE,
    name       varchar(32) NOT NULL,
    sort_order integer     NOT NULL
);
INSERT INTO acquisition_groups (slug, name, sort_order) VALUES
    ('drop',   'loot / drops',   10),
    ('vendor', 'quest / vendor', 20)
ON CONFLICT (slug) DO NOTHING;

ALTER TABLE acquisition_types ADD COLUMN group_id integer NULL REFERENCES acquisition_groups(id);
UPDATE acquisition_types a SET group_id = g.id
FROM (VALUES ('drop', 'drop'), ('vendor', 'vendor'), ('quest', 'vendor')) AS v(type_slug, group_slug)
JOIN acquisition_groups g ON g.slug = v.group_slug
WHERE a.slug = v.type_slug;
-- +goose StatementBegin
DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM acquisition_types WHERE group_id IS NULL) THEN
        RAISE EXCEPTION 'an acquisition type has no group: %',
            (SELECT string_agg(slug, ', ') FROM acquisition_types WHERE group_id IS NULL);
    END IF;
END $$;
-- +goose StatementEnd

-- Every source row points at its section. Backfilled BY NAME from the row's own section_raw; a row
-- whose section is not one of the 39 stops the migration rather than being left without one. The
-- column is nullable like every lookup (TestNothingIsNotNullByAccident): a row with no section is in
-- no tab. The importer refuses a section name it was never told about (resolve.go).
ALTER TABLE item_sources ADD COLUMN section_id integer NULL REFERENCES sections(id);
UPDATE item_sources s SET section_id = sc.id FROM sections sc WHERE sc.name = s.section_raw;
-- +goose StatementBegin
DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM item_sources WHERE section_id IS NULL) THEN
        RAISE EXCEPTION '% item_sources rows have no known section (e.g. %)',
            (SELECT count(*) FROM item_sources WHERE section_id IS NULL),
            (SELECT coalesce(min(section_raw), 'NULL') FROM item_sources WHERE section_id IS NULL);
    END IF;
END $$;
-- +goose StatementEnd
CREATE INDEX item_sources_section_id_idx ON item_sources (section_id);

-- +goose Down
ALTER TABLE item_sources DROP COLUMN section_id;
ALTER TABLE acquisition_types DROP COLUMN group_id;
DROP TABLE acquisition_groups;
DROP TABLE sections;
DROP TABLE source_tabs;
