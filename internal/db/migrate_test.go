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
		v := os.Getenv(k)
		if v == "" {
			continue
		}
		// ⭐ THE GUARD RAIL APPLIES HERE TOO. These tests issue CREATE DATABASE and
		// DROP DATABASE … WITH (FORCE) — five throwaway databases per run. The Makefile
		// tells you to export a production DATABASE_URL to run a migration, and that is
		// the same shell `make test`, `make check` and `bin/gate api` run in, with the
		// environment inherited unchanged. Nothing stopped a `go test` from creating
		// databases on production's instance. It would not corrupt data, but "hitting
		// production takes effort" was not true of this path.
		// (AOC-005 verify round 1.)
		if h := hostOf(v); !isLocalHost(h) {
			t.Fatalf("%s points at %q, which is not local.\n"+
				"These tests CREATE and DROP databases; they will not do that on a remote server.\n"+
				"Use the local compose database (make db-up), or set TEST_DATABASE_URL to it.", k, h)
		}
		return v
	}
	t.Skip("no TEST_DATABASE_URL or DATABASE_URL — run `make db-up` and export DATABASE_URL, or let CI run this")
	return ""
}

// hostOf pulls the host[:port] out of a postgres URL without parsing credentials, so a
// password can never reach a log line or a test failure message.
func hostOf(url string) string {
	if i := strings.Index(url, "://"); i >= 0 {
		url = url[i+3:]
	}
	if i := strings.LastIndex(url, "@"); i >= 0 {
		url = url[i+1:]
	}
	if i := strings.IndexAny(url, "/?"); i >= 0 {
		url = url[:i]
	}
	return url
}

// isLocalHost accepts only what a developer machine or a CI service container looks like.
// Deliberately an allowlist: a denylist of "known production hostnames" is a list someone
// forgets to update exactly once.
func isLocalHost(hostPort string) bool {
	host := hostPort
	if i := strings.LastIndex(host, ":"); i >= 0 {
		host = host[:i]
	}
	switch host {
	case "localhost", "127.0.0.1", "::1", "[::1]", "postgres", "db", "host.docker.internal":
		return true
	}
	return false
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

	// Reap anything a previous run left behind. t.Cleanup covers Fatal and panic but NOT a
	// timeout or Ctrl-C: `go test -timeout 50ms` leaves an aoc_test_% database orphaned
	// (measured, AOC-005 verify round 1). Nothing else ever cleans these up, so they would
	// accumulate silently on a developer machine.
	reapOrphans(t, sqlDB, ctx)

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

	// ⚠️ This test does NOT pin the migration's own ON CONFLICT clause and must not be
	// described as if it did. `down` drops the table, so `up` can never meet a duplicate —
	// delete the clause from the migration and this stays green. The clause is pinned by
	// TestTheMigrationsOwnSeedIsIdempotent, which lifts the statement out of the file.
	// (AOC-005 verify round 1; docs/architecture.md named the wrong test.)
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
		body := stripSQLComments(string(b))
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

// stripSQLComments removes `-- …` line comments and /* … */ blocks.
//
// ⭐ WHY. The static check below used to search the raw file, and every migration in this
// project explains its own ON CONFLICT clause in a comment directly above the INSERT. That
// comment lands in the same `;`-delimited chunk as the statement, so deleting the REAL
// clause left the check green — it was matching prose. Proved by deleting the clause: the
// check still passed, still logging "checked 1 INSERT statement(s)".
//
// It matters beyond this one file: the convention document tells the next author to copy
// this migration, comment and all, and EP-02 seeds a dozen taxonomy tables.
// (AOC-005 verify round 1.)
func stripSQLComments(s string) string {
	var out strings.Builder
	out.Grow(len(s))
	for i := 0; i < len(s); {
		switch {
		case strings.HasPrefix(s[i:], "--"):
			j := strings.IndexByte(s[i:], '\n')
			if j < 0 {
				return out.String()
			}
			out.WriteByte('\n') // keep line structure so `;` splitting is unchanged
			i += j + 1
		case strings.HasPrefix(s[i:], "/*"):
			j := strings.Index(s[i+2:], "*/")
			if j < 0 {
				return out.String()
			}
			i += 2 + j + 2
		default:
			out.WriteByte(s[i])
			i++
		}
	}
	return out.String()
}

// The stripper is itself load-bearing, so it is pinned: if it stopped removing comments the
// static check would go back to matching prose, silently.
func TestStripSQLCommentsRemovesProse(t *testing.T) {
	cases := []struct{ name, in, wantGone, wantKept string }{
		{"line comment", "-- ON CONFLICT DO NOTHING\nINSERT INTO t VALUES (1);", "ON CONFLICT", "INSERT INTO t"},
		{"trailing comment", "INSERT INTO t VALUES (1); -- ON CONFLICT\n", "ON CONFLICT", "INSERT INTO t"},
		{"block comment", "/* ON CONFLICT */ INSERT INTO t VALUES (1);", "ON CONFLICT", "INSERT INTO t"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := stripSQLComments(c.in)
			if strings.Contains(got, c.wantGone) {
				t.Errorf("comment text survived: %q", got)
			}
			if !strings.Contains(got, c.wantKept) {
				t.Errorf("statement text was removed: %q", got)
			}
		})
	}
	// And a real clause must survive.
	if !strings.Contains(stripSQLComments("INSERT INTO t VALUES (1) ON CONFLICT DO NOTHING;"), "ON CONFLICT") {
		t.Error("the stripper removed a real ON CONFLICT clause")
	}
}

// The guard is only worth having if it actually refuses. Pinned in both directions.
func TestOnlyLocalHostsAreAccepted(t *testing.T) {
	local := []string{
		"postgres://aoc:aoc@localhost:5433/aoc_dev?sslmode=disable",
		"postgres://aoc:aoc@127.0.0.1:5432/aoc_dev",
		"postgres://aoc:aoc@postgres:5432/aoc_dev", // the CI service container
		"postgres://aoc:aoc@db:5432/aoc_dev",       // a compose service name
	}
	remote := []string{
		"postgres://u:p@monorail.proxy.rlwy.net:37421/railway",
		"postgres://u:p@postgres.railway.internal:5432/railway",
		"postgres://u:p@db.example.com:5432/x",
		"postgres://u:p@10.0.0.5:5432/x",
	}
	for _, u := range local {
		if !isLocalHost(hostOf(u)) {
			t.Errorf("%s was rejected; it is local", hostOf(u))
		}
	}
	for _, u := range remote {
		if isLocalHost(hostOf(u)) {
			t.Errorf("%s was ACCEPTED — these tests create and drop databases", hostOf(u))
		}
	}
	// hostOf must never leak the password into a message.
	if h := hostOf("postgres://user:sup3rsecret@localhost:5433/db"); strings.Contains(h, "sup3rsecret") {
		t.Errorf("hostOf leaked credentials: %q", h)
	}
}

// reapOrphans drops leftover throwaway databases. Best effort: a failure here must never
// fail a test, because it says nothing about the code under test.
func reapOrphans(t *testing.T, d *sql.DB, ctx context.Context) {
	t.Helper()
	names, err := orphanNames(ctx, d)
	if err != nil {
		return
	}
	for _, n := range names {
		if _, err := d.ExecContext(ctx, "DROP DATABASE IF EXISTS "+n+" WITH (FORCE)"); err == nil {
			t.Logf("reaped an orphaned test database: %s", n)
		}
	}
}

// orphanNames is split out so the rows are closed by defer on every path — sqlclosecheck
// rightly refuses a bare Close() that a mid-loop return could skip.
func orphanNames(ctx context.Context, d *sql.DB) ([]string, error) {
	rows, err := d.QueryContext(ctx, `SELECT datname FROM pg_database WHERE datname LIKE 'aoc_test_%'`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var names []string
	for rows.Next() {
		var n string
		if err := rows.Scan(&n); err != nil {
			return nil, err
		}
		names = append(names, n)
	}
	return names, rows.Err()
}
