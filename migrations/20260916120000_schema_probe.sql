-- AOC-005: the first migration. It exists to prove the chain works end to end —
-- goose applies it, sqlc reads the resulting schema, and `down` leaves nothing behind.
-- Real tables arrive with AOC-009 (taxonomy, places) and AOC-010 (items).
--
-- CONVENTIONS every later migration copies:
--   * Filename is <UTC timestamp>_<snake_case>.sql. Timestamps, not sequence numbers:
--     two branches picking "003_" merge cleanly and then apply in an order nobody chose.
--   * Every Up has a Down that actually reverses it. A Down nobody has run is a Down
--     that does not work — `make migrate-redo` runs the round trip.
--   * Forward-only in production (docs/architecture.md § Deploy). Down exists so the
--     round trip can be REHEARSED locally, not so production can be rewound.

-- +goose Up
CREATE TABLE schema_probe (
    id         integer     PRIMARY KEY,
    note       text        NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now()
);

-- THE SEED CONVENTION. Seeds live in migrations so they are versioned and ordered like
-- everything else, and they are IDEMPOTENT so re-running one cannot duplicate a row.
--
-- ON CONFLICT DO NOTHING, not "INSERT if not exists": the latter is two statements with
-- a race between them, and goose will one day run against a database something else is
-- also writing to. EP-02 seeds every taxonomy table this way (classes, rarities, slots),
-- and a taxonomy row duplicated by a re-run is a filter that shows the same option twice.
INSERT INTO schema_probe (id, note) VALUES
    (1, 'seed rows are idempotent: re-running this migration must not duplicate them')
ON CONFLICT (id) DO NOTHING;

-- +goose Down
DROP TABLE schema_probe;
