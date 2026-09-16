-- Queries against the probe table. They exist to prove the sqlc chain compiles and runs;
-- AOC-009/AOC-010 replace them with real ones.

-- name: GetProbe :one
SELECT id, note, created_at FROM schema_probe WHERE id = $1;

-- name: CountProbes :one
SELECT count(*) FROM schema_probe;
