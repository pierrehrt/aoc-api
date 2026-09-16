package db_test

import (
	"context"
	"database/sql"
	"fmt"
	"math/rand"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"

	"github.com/pierrehrt/aoc-api/internal/db"
	"github.com/pierrehrt/aoc-api/internal/db/sqlcgen"
)

const migrationsDir = "../../migrations"

// adminURL returns a connection string to a Postgres we may create databases on.
//
// ⚠️ These tests are INTEGRATION tests: they need a real Postgres, because the thing
// under test is SQL, and SQL that has never met a database is not tested. CI provides one
// as a service container, so they always run there. Locally, `make db-up` provides it.
//
// The skip is deliberate and loud rather than silent — but note it IS a skip, which this
// project treats with suspicion. CI is what guarantees these actually ran.
func adminURL(t *testing.T) string {
	t.Helper()
	for _, k := range []string{"TEST_DATABASE_URL", "DATABASE_URL"} {
		if v := os.Getenv(k); v != "" {
			return v
		}
	}
	t.Skip("no TEST_DATABASE_URL or DATABASE_URL — run `make db-up` and export DATABASE_URL, or let CI run this")
	return ""
}

// freshDatabase creates a throwaway database and drops it afterwards, so a test can never
// disturb the developer's own data and two tests can never collide.
func freshDatabase(t *testing.T) string {
	t.Helper()
	admin := adminURL(t)

	ctx := context.Background()
	sqlDB, err := sql.Open("pgx", admin)
	if err != nil {
		t.Fatalf("connecting to Postgres: %v", err)
	}
	defer sqlDB.Close()

	name := fmt.Sprintf("aoc_test_%d_%d", time.Now().UnixNano(), rand.Intn(1000)) //nolint:gosec // test fixture naming, not security
	if _, err := sqlDB.ExecContext(ctx, "CREATE DATABASE "+name); err != nil {
		t.Fatalf("creating throwaway database: %v", err)
	}
	t.Cleanup(func() {
		d, err := sql.Open("pgx", admin)
		if err != nil {
			return
		}
		defer d.Close()
		_, _ = d.ExecContext(context.Background(), "DROP DATABASE IF EXISTS "+name+" WITH (FORCE)")
	})

	// Swap the database name in the URL.
	return replaceDBName(admin, name)
}

func replaceDBName(url, name string) string {
	// postgres://user:pass@host:port/dbname?params
	slash := -1
	for i := len(url) - 1; i >= 0; i-- {
		if url[i] == '/' {
			slash = i
			break
		}
	}
	rest := ""
	for i := slash; i < len(url); i++ {
		if url[i] == '?' {
			rest = url[i:]
			break
		}
	}
	return url[:slash+1] + name + rest
}

func openGoose(t *testing.T, url string) *sql.DB {
	t.Helper()
	d, err := sql.Open("pgx", url)
	if err != nil {
		t.Fatalf("opening throwaway database: %v", err)
	}
	t.Cleanup(func() { _ = d.Close() })
	if err := goose.SetDialect("postgres"); err != nil {
		t.Fatalf("goose dialect: %v", err)
	}
	return d
}

// ⭐ The round trip. A Down nobody has run is a Down that does not work, and it is needed
// exactly when things are already going wrong.
func TestMigrationsUpThenDownLeaveACleanDatabase(t *testing.T) {
	url := freshDatabase(t)
	d := openGoose(t, url)

	if err := goose.Up(d, migrationsDir); err != nil {
		t.Fatalf("goose up: %v", err)
	}
	if !tableExists(t, d, "schema_probe") {
		t.Fatal("after up, schema_probe does not exist")
	}

	if err := goose.DownTo(d, migrationsDir, 0); err != nil {
		t.Fatalf("goose down: %v", err)
	}
	if tableExists(t, d, "schema_probe") {
		t.Error("after down, schema_probe still exists — the Down does not reverse the Up")
	}
}

// ⭐ The seed convention EP-02 depends on. A taxonomy row duplicated by a re-run is a
// filter that offers the same option twice.
func TestSeedsAreIdempotent(t *testing.T) {
	url := freshDatabase(t)
	d := openGoose(t, url)

	if err := goose.Up(d, migrationsDir); err != nil {
		t.Fatalf("first up: %v", err)
	}
	first := countProbes(t, d)
	if first != 1 {
		t.Fatalf("after one up, %d seed rows, want 1", first)
	}

	// down + up is how a developer rehearses a migration, and the path most likely to
	// double a seed.
	if err := goose.DownTo(d, migrationsDir, 0); err != nil {
		t.Fatalf("down: %v", err)
	}
	if err := goose.Up(d, migrationsDir); err != nil {
		t.Fatalf("second up: %v", err)
	}
	if got := countProbes(t, d); got != 1 {
		t.Errorf("after down+up, %d seed rows, want 1 — the seed is not idempotent", got)
	}

	// And re-running the seed statement directly must also be safe, since a seed may be
	// re-applied by a repair rather than by goose.
	if _, err := d.ExecContext(context.Background(), `INSERT INTO schema_probe (id, note) VALUES (1, 'again') ON CONFLICT (id) DO NOTHING`); err != nil {
		t.Fatalf("re-running the seed: %v", err)
	}
	if got := countProbes(t, d); got != 1 {
		t.Errorf("re-running the seed gave %d rows, want 1", got)
	}
}

// The sqlc chain: generated Go, against a real migrated database.
func TestGeneratedQueriesRunAgainstTheRealSchema(t *testing.T) {
	url := freshDatabase(t)
	d := openGoose(t, url)
	if err := goose.Up(d, migrationsDir); err != nil {
		t.Fatalf("goose up: %v", err)
	}

	ctx := context.Background()
	pool, err := pgxpool.New(ctx, url)
	if err != nil {
		t.Fatalf("pool: %v", err)
	}
	defer pool.Close()

	q := sqlcgen.New(pool)
	row, err := q.GetProbe(ctx, 1)
	if err != nil {
		t.Fatalf("GetProbe: %v", err)
	}
	if row.ID != 1 || row.Note == "" {
		t.Errorf("GetProbe returned %+v, want the seeded row", row)
	}
	n, err := q.CountProbes(ctx)
	if err != nil {
		t.Fatalf("CountProbes: %v", err)
	}
	if n != 1 {
		t.Errorf("CountProbes = %d, want 1", n)
	}
}

// db.New must PING, not just construct. pgxpool is lazy: it succeeds against a completely
// wrong URL because nothing connects until the first query.
func TestNewFailsFastOnAnUnreachableDatabase(t *testing.T) {
	adminURL(t) // keeps the skip behaviour consistent with the other tests
	cfg := db.DefaultConfig("postgres://nobody:nobody@127.0.0.1:1/none?sslmode=disable")
	cfg.ConnectTimeout = 2 * time.Second
	if _, err := db.New(context.Background(), cfg); err == nil {
		t.Fatal("db.New succeeded against an unreachable database — it is not pinging")
	}
}

// The limits are load-bearing, not decoration. Railway's Postgres has a fixed connection
// limit shared with migrations, psql and backups; an unbounded pool exhausts it and the
// symptom is "the site is down", nowhere near the cause.
func TestNewAppliesItsLimits(t *testing.T) {
	url := freshDatabase(t)
	cfg := db.DefaultConfig(url)
	cfg.MaxConns = 3
	cfg.MaxConnLifetime = 42 * time.Minute
	cfg.MaxConnIdleTime = 7 * time.Minute

	pool, err := db.New(context.Background(), cfg)
	if err != nil {
		t.Fatalf("db.New: %v", err)
	}
	defer pool.Close()

	got := pool.Config()
	if got.MaxConns != cfg.MaxConns {
		t.Errorf("MaxConns = %d, want %d — the configured limit was not applied", got.MaxConns, cfg.MaxConns)
	}
	if got.MaxConnLifetime != cfg.MaxConnLifetime {
		t.Errorf("MaxConnLifetime = %v, want %v", got.MaxConnLifetime, cfg.MaxConnLifetime)
	}
	if got.MaxConnIdleTime != cfg.MaxConnIdleTime {
		t.Errorf("MaxConnIdleTime = %v, want %v", got.MaxConnIdleTime, cfg.MaxConnIdleTime)
	}
	if got.ConnConfig.ConnectTimeout != cfg.ConnectTimeout {
		t.Errorf("ConnectTimeout = %v, want %v", got.ConnConfig.ConnectTimeout, cfg.ConnectTimeout)
	}
}

// DefaultConfig must not hand back zero values: a zero MaxConns is "unlimited", which is
// the opposite of what the comment on it promises.
func TestDefaultConfigIsActuallyBounded(t *testing.T) {
	c := db.DefaultConfig("postgres://x/y")
	if c.MaxConns <= 0 {
		t.Errorf("MaxConns = %d — a pool with no ceiling can exhaust Railway's shared limit", c.MaxConns)
	}
	if c.ConnectTimeout <= 0 {
		t.Error("ConnectTimeout is unset — a bad host would make startup hang forever")
	}
	if c.MaxConnLifetime <= 0 {
		t.Error("MaxConnLifetime is unset — the pool would never notice a replaced database")
	}
}

func TestNewRejectsAnEmptyURL(t *testing.T) {
	_, err := db.New(context.Background(), db.DefaultConfig(""))
	if err == nil {
		t.Fatal("db.New accepted an empty DATABASE_URL")
	}
	// ⚠️ Assert WHICH error. Without the explicit guard, pgxpool.ParseConfig("") succeeds
	// — an empty string means "use libpq defaults" — and the failure only surfaces later
	// from the ping, against whatever database those defaults happen to name. The test
	// then passes for entirely the wrong reason, which is how this mutant survived its
	// first run.
	if !strings.Contains(err.Error(), "DATABASE_URL is empty") {
		t.Errorf("error = %q, want it to name the empty DATABASE_URL — an error from"+
			" somewhere else means the guard is gone and libpq defaults were used", err)
	}
}

// ⭐ THE CONVENTION, enforced for every migration this project will ever have.
//
// EP-02 seeds every taxonomy table — classes, rarities, slots, currencies. A seed that can
// double a row is a filter that offers the same option twice, and it will be noticed months
// later by a reader, not by us. This is a STATIC check so it covers migrations nobody has
// thought to write a test for.
func TestEverySeedInsertIsIdempotent(t *testing.T) {
	entries, err := os.ReadDir(migrationsDir)
	if err != nil {
		t.Fatalf("reading %s: %v", migrationsDir, err)
	}
	checked := 0
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".sql") {
			continue
		}
		b, err := os.ReadFile(filepath.Join(migrationsDir, e.Name()))
		if err != nil {
			t.Fatalf("reading %s: %v", e.Name(), err)
		}
		body := string(b)
		up := body
		if i := strings.Index(body, "-- +goose Down"); i >= 0 {
			up = body[:i] // only the Up section seeds
		}
		for _, stmt := range strings.Split(up, ";") {
			l := strings.ToUpper(stmt)
			if !strings.Contains(l, "INSERT INTO") {
				continue
			}
			checked++
			if !strings.Contains(l, "ON CONFLICT") {
				t.Errorf("%s has an INSERT with no ON CONFLICT clause — a re-run would"+
					" duplicate the row:\n%s", e.Name(), strings.TrimSpace(stmt))
			}
		}
	}
	// A static check that examined nothing is not a passing check.
	if checked == 0 {
		t.Fatal("no INSERT statements found in migrations/ — this check asserted nothing")
	}
	t.Logf("checked %d INSERT statement(s)", checked)
}

// And the migration's OWN seed statement, re-executed, must still leave one row. The
// earlier version of this test wrote its own ON CONFLICT clause, so it passed even when
// the migration had none — it was testing the test.
func TestTheMigrationsOwnSeedIsIdempotent(t *testing.T) {
	url := freshDatabase(t)
	d := openGoose(t, url)
	if err := goose.Up(d, migrationsDir); err != nil {
		t.Fatalf("goose up: %v", err)
	}

	stmt := seedStatementFrom(t, "20260916120000_schema_probe.sql", "INSERT INTO schema_probe")
	if _, err := d.ExecContext(context.Background(), stmt); err != nil {
		t.Fatalf("re-running the migration's own seed failed: %v\nstatement: %s", err, stmt)
	}
	if got := countProbes(t, d); got != 1 {
		t.Errorf("after re-running the migration's own seed there are %d rows, want 1", got)
	}
}

// seedStatementFrom lifts a statement verbatim out of a migration file, so the test
// exercises what will actually run rather than a copy that may have drifted.
func seedStatementFrom(t *testing.T, file, startsWith string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(migrationsDir, file))
	if err != nil {
		t.Fatalf("reading %s: %v", file, err)
	}
	body := string(b)
	i := strings.Index(body, startsWith)
	if i < 0 {
		t.Fatalf("%s contains no statement starting %q", file, startsWith)
	}
	j := strings.Index(body[i:], ";")
	if j < 0 {
		t.Fatalf("%s: statement starting %q is not terminated", file, startsWith)
	}
	return body[i : i+j+1]
}

func tableExists(t *testing.T, d *sql.DB, name string) bool {
	t.Helper()
	var exists bool
	if err := d.QueryRowContext(context.Background(), `SELECT to_regclass($1) IS NOT NULL`, name).Scan(&exists); err != nil {
		t.Fatalf("checking for %s: %v", name, err)
	}
	return exists
}

func countProbes(t *testing.T, d *sql.DB) int {
	t.Helper()
	var n int
	if err := d.QueryRowContext(context.Background(), `SELECT count(*) FROM schema_probe`).Scan(&n); err != nil {
		t.Fatalf("counting probes: %v", err)
	}
	return n
}
