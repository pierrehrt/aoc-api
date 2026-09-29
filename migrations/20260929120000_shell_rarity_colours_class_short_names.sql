-- +goose Up
-- AOC-046: two kinds of thing the Armory design paints by hand become DATA — a rarity's colour and a
-- class's short name. A colour or an abbreviation in a template is a taxonomy in code
-- (reference/content-model.md § 0): the day a rarity or a class is added, the template is the file
-- nobody updates.
--
-- colour_token is the NAME of a CSS custom property (`rarity-epic` → `--color-rarity-epic`), not a
-- hex: the hex lives in web/src/app.css under that name, where contrast is tested. NULL means "no
-- colour of its own": the design renders the three tiers whose colour it sampled and leaves the
-- rest neutral — so nothing here invents a colour a source never had.
ALTER TABLE rarities ADD COLUMN colour_token varchar(32) NULL;
UPDATE rarities SET colour_token = 'rarity-' || slug WHERE slug IN ('rare', 'epic', 'legendary');

-- The short names players use. Source: Pierre, 2026-09-29, Tier A (DECISIONS.md) — confirmed
-- against the mockup's abbreviations, which on their own would have been a placeholder, not a fact.
ALTER TABLE classes ADD COLUMN short_name varchar(8) NULL;
UPDATE classes SET short_name = v.short_name
FROM (VALUES
    ('conqueror', 'Conq'), ('dark-templar', 'DT'), ('guardian', 'Guard'),
    ('bear-shaman', 'BS'), ('priest-of-mitra', 'PoM'), ('tempest-of-set', 'ToS'),
    ('assassin', 'Sin'), ('barbarian', 'Barb'), ('ranger', 'Ranger'),
    ('demonologist', 'Demo'), ('herald-of-xotli', 'HoX'), ('necromancer', 'Necro')
) AS v(slug, short_name)
WHERE classes.slug = v.slug;

-- +goose Down
ALTER TABLE classes DROP COLUMN short_name;
ALTER TABLE rarities DROP COLUMN colour_token;
