-- Read queries for the place entities — the browse tree behind the Armory page's location filter
-- and, later, every boss page's breadcrumb (AOC-009).
--
-- ⭐ WHY THESE ARE QUERIES AND NOT A GO MAP. Same reason as taxonomy.sql: region -> map -> place
-- is data, not code. It already disagrees with itself in three places (Kuthchemes, the two
-- Unchained variants the armory files under the wrong map), and those disagreements are ROWS
-- carrying their own open_question — a Go literal could hold the names but never the provenance.
--
-- ⚠️ Every list here returns confidence and open_question with the row. A page that shows a place
-- without being able to show how sure we are cannot honour sourcing-standards § 3, where a
-- `disputed` fact renders with a ⚠️ and both versions. Dropping them from the SELECT is how that
-- rule quietly stops being enforced.

-- name: ListRegions :many
SELECT r.id, r.slug, r.name, r.sort_order, c.slug AS confidence, r.source_note, r.open_question
FROM regions r JOIN confidence_levels c ON c.id = r.confidence_id
ORDER BY r.sort_order;

-- name: ListMaps :many
SELECT m.id, m.slug, m.name, m.region_id, r.name AS region_name,
       c.slug AS confidence, m.source_note, m.open_question
FROM maps m
LEFT JOIN regions r ON r.id = m.region_id
JOIN confidence_levels c ON c.id = m.confidence_id
ORDER BY r.sort_order NULLS LAST, m.name;

-- name: ListPlaces :many
-- The whole browse tree in one read: 86 rows, so the Armory page's filter needs no second query.
SELECT p.id, p.slug, p.name, p.unchained, p.region_id, r.name AS region_name,
       p.map_id, m.name AS map_name, p.parent_place_id, parent.name AS parent_name,
       c.slug AS confidence, p.source_note, p.open_question
FROM places p
JOIN regions r ON r.id = p.region_id
LEFT JOIN maps m ON m.id = p.map_id
LEFT JOIN places parent ON parent.id = p.parent_place_id
JOIN confidence_levels c ON c.id = p.confidence_id
ORDER BY r.sort_order, m.name NULLS FIRST, p.name;

-- name: GetPlaceBySlug :one
SELECT p.id, p.slug, p.name, p.unchained, p.region_id, r.name AS region_name, r.slug AS region_slug,
       p.map_id, m.name AS map_name, p.parent_place_id, parent.name AS parent_name,
       parent.slug AS parent_slug, c.slug AS confidence, p.source_note, p.open_question
FROM places p
JOIN regions r ON r.id = p.region_id
LEFT JOIN maps m ON m.id = p.map_id
LEFT JOIN places parent ON parent.id = p.parent_place_id
JOIN confidence_levels c ON c.id = p.confidence_id
WHERE p.slug = $1;

-- name: ListPlacesInPlace :many
-- The two complexes: House of Crom's page lists its two dungeons, Warmonk Monastery's its three.
SELECT p.id, p.slug, p.name, p.unchained, c.slug AS confidence, p.open_question
FROM places p
JOIN confidence_levels c ON c.id = p.confidence_id
WHERE p.parent_place_id = $1
ORDER BY p.name;

-- name: ListBossesInPlace :many
SELECT b.id, b.slug, b.name, c.slug AS confidence, b.source_note, b.open_question
FROM bosses b
JOIN confidence_levels c ON c.id = b.confidence_id
WHERE b.place_id = $1
ORDER BY b.name;

-- name: ListBosses :many
SELECT b.id, b.slug, b.name, b.place_id, p.name AS place_name, b.map_id, m.name AS map_name,
       b.region_id, r.name AS region_name, c.slug AS confidence, b.source_note, b.open_question
FROM bosses b
LEFT JOIN places p ON p.id = b.place_id
LEFT JOIN maps m ON m.id = b.map_id
LEFT JOIN regions r ON r.id = b.region_id
JOIN confidence_levels c ON c.id = b.confidence_id
ORDER BY b.name;

-- name: ListQuests :many
-- `name` is NULL for all 51 — armory_label is what the source actually recorded.
SELECT q.id, q.slug, q.armory_label, q.name, q.region_id, r.name AS region_name,
       q.map_id, m.name AS map_name, c.slug AS confidence, q.source_note, q.open_question
FROM quests q
LEFT JOIN regions r ON r.id = q.region_id
LEFT JOIN maps m ON m.id = q.map_id
JOIN confidence_levels c ON c.id = q.confidence_id
ORDER BY q.armory_label;

-- name: ListContainers :many
SELECT co.id, co.slug, co.name, c.slug AS confidence, co.source_note, co.open_question
FROM containers co JOIN confidence_levels c ON c.id = co.confidence_id
ORDER BY co.name;

-- name: ListConfidenceLevels :many
SELECT id, slug, name, sort_order FROM confidence_levels ORDER BY sort_order;

-- name: ListOpenQuestions :many
-- ⭐ THE LIST OF THINGS TO ASK PIERRE, in one query rather than six greps. Every entity that
-- carries an unresolved question about the game, with what the question is. AOC-009 seeds ten of
-- these deliberately: a blank field and a recorded doubt are both better than a confident guess.
SELECT 'place' AS entity, slug, name, open_question FROM places WHERE open_question IS NOT NULL
UNION ALL
SELECT 'boss', slug, name, open_question FROM bosses WHERE open_question IS NOT NULL
UNION ALL
SELECT 'map', slug, name, open_question FROM maps WHERE open_question IS NOT NULL
UNION ALL
SELECT 'region', slug, name, open_question FROM regions WHERE open_question IS NOT NULL
UNION ALL
SELECT 'quest', slug, armory_label, open_question FROM quests WHERE open_question IS NOT NULL
UNION ALL
SELECT 'container', slug, name, open_question FROM containers WHERE open_question IS NOT NULL
ORDER BY 1, 2;
