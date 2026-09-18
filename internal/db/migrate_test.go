package db_test

import (
	"context"
	"database/sql"
	"fmt"
	"math/rand"
	"os"
	"path/filepath"
	"regexp"
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

// seedScan is what a static pass over a migrations directory found: how many INSERT
// statements it examined, and which of them a re-run would duplicate.
type seedScan struct {
	checked   int      // INSERT statements examined, across every file
	offenders []string // "<file>: <statement>", one per INSERT with no ON CONFLICT clause
}

// checkSeedsAreIdempotent reports every INSERT in a migration's Up section that carries no
// ON CONFLICT clause.
//
// ⚠️ THE TWO STEPS ARE ORDERED, and each order was chosen after the other one broke.
//
//  1. The Up/Down split happens on the RAW text, because `-- +goose Down` is itself a `--`
//     comment. Strip first and the marker is gone, the split silently never fires, and the
//     Down section is scanned as though it seeded — so a rollback that writes an audit row
//     would be reported as a defect. Measured, not reasoned about.
//  2. The Up section is then walked by sqlStatements, which splits on semicolons and reads
//     the clause from CODE ONLY — comments removed and string literals blanked. Searching raw
//     text matched the PROSE: deleting the real clause left the check green (AOC-005). Splitting
//     raw text on `;` broke the other way once the seeds carried sentences: AOC-009's
//     source_note values contain semicolons and apostrophes, so every place row was reported
//     as an offender although the clause was right there (AOC-009, 2026-09-18).
//
// It takes a directory rather than reading migrationsDir itself so that it can be pointed at
// synthetic fixtures — see TestTheSeedCheckReadsSQLNotProse for why that is the whole point.
func checkSeedsAreIdempotent(dir string) (seedScan, error) {
	var scan seedScan
	entries, err := os.ReadDir(dir)
	if err != nil {
		return scan, fmt.Errorf("reading %s: %w", dir, err)
	}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".sql") {
			continue
		}
		b, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			return scan, fmt.Errorf("reading %s: %w", e.Name(), err)
		}
		up := string(b)
		if i := strings.Index(up, "-- +goose Down"); i >= 0 {
			up = up[:i] // only the Up section seeds
		}
		for _, stmt := range sqlStatements(up) {
			u := strings.ToUpper(stmt.code)
			if !strings.Contains(u, "INSERT INTO") {
				continue
			}
			scan.checked++
			if !strings.Contains(u, "ON CONFLICT") {
				scan.offenders = append(scan.offenders,
					fmt.Sprintf("%s: %s", e.Name(), strings.TrimSpace(stmt.text)))
			}
		}
	}
	return scan, nil
}

// ⭐ THE CONVENTION, enforced for every migration this project will ever have.
//
// EP-02 seeds every taxonomy table — classes, rarities, slots, currencies. A seed that can
// double a row is a filter that offers the same option twice, and it will be noticed months
// later by a reader, not by us. This is a STATIC check so it covers migrations nobody has
// thought to write a test for.
func TestEverySeedInsertIsIdempotent(t *testing.T) {
	scan, err := checkSeedsAreIdempotent(migrationsDir)
	if err != nil {
		t.Fatal(err)
	}
	for _, o := range scan.offenders {
		t.Errorf("an INSERT with no ON CONFLICT clause — a re-run would duplicate the row:\n%s", o)
	}
	// A static check that examined nothing is not a passing check.
	if scan.checked == 0 {
		t.Fatal("no INSERT statements found in migrations/ — this check asserted nothing")
	}
	t.Logf("checked %d INSERT statement(s)", scan.checked)
}

// ⭐ THE CHECK ITSELF IS PINNED — against synthetic migrations, not the repo's own.
//
// WHY THIS TEST EXISTS, because it is not obvious and it cost a verify round. The check above
// reads the real migrations/ directory, and every file in there has BOTH a real ON CONFLICT
// clause AND a comment explaining it. Against that input the check passes whether or not it
// strips comments first — the two possible readings agree. So when the stripper was disabled
// the entire suite stayed green: the property the stripper exists for was never pinned, only
// the stripper's own behaviour was.
//
// Prose and SQL have to DISAGREE for the difference to be observable, and the repo contains
// no such migration on purpose. Hence fixtures.
func TestTheSeedCheckReadsSQLNotProse(t *testing.T) {
	cases := []struct {
		name        string
		sql         string
		wantChecked int
		wantFlagged bool
	}{
		{
			// The exact shape of the real migration, with the clause deleted: the CREATE
			// TABLE ends the previous chunk, so the explanatory comment and the INSERT share
			// one. This is the original bug, reproduced as a fixture.
			name: "clause survives only in a line comment",
			sql: `-- +goose Up
CREATE TABLE widget (id int PRIMARY KEY, label text NOT NULL);

-- ON CONFLICT DO NOTHING, not "insert if not exists" — a re-run must leave one row.
INSERT INTO widget (id, label) VALUES (1, 'alpha');
-- +goose Down
DROP TABLE widget;
`,
			wantChecked: 1,
			wantFlagged: true,
		},
		{
			name: "clause survives only in a block comment",
			sql: `-- +goose Up
/* ON CONFLICT DO NOTHING keeps this idempotent. */
INSERT INTO widget (id, label) VALUES (1, 'alpha');
-- +goose Down
DROP TABLE widget;
`,
			wantChecked: 1,
			wantFlagged: true,
		},
		{
			name: "a real clause and no comment at all",
			sql: `-- +goose Up
INSERT INTO widget (id, label) VALUES (1, 'alpha') ON CONFLICT (id) DO NOTHING;
-- +goose Down
DROP TABLE widget;
`,
			wantChecked: 1,
			wantFlagged: false,
		},
		{
			// How every migration in this repo is actually written. Must stay green, or the
			// fix for the bug above would just have inverted it.
			name: "a real clause with the comment that explains it",
			sql: `-- +goose Up
CREATE TABLE widget (id int PRIMARY KEY, label text NOT NULL);

-- ON CONFLICT DO NOTHING, not "insert if not exists" — a re-run must leave one row.
INSERT INTO widget (id, label) VALUES (1, 'alpha') ON CONFLICT (id) DO NOTHING;
-- +goose Down
DROP TABLE widget;
`,
			wantChecked: 1,
			wantFlagged: false,
		},
		{
			// Pins step 1 of the ordering. A Down section does not seed, so an INSERT there
			// needs no ON CONFLICT and must not be counted. If the split is performed after
			// comment-stripping, `-- +goose Down` has been erased and this fixture reports a
			// phantom offender.
			name: "an INSERT in the Down section is not a seed",
			sql: `-- +goose Up
CREATE TABLE widget (id int PRIMARY KEY, label text NOT NULL);
-- +goose Down
INSERT INTO widget_audit (note) VALUES ('rolled back');
DROP TABLE widget;
`,
			wantChecked: 0,
			wantFlagged: false,
		},
		{
			name: "a migration that seeds nothing is examined and found empty",
			sql: `-- +goose Up
CREATE TABLE widget (id int PRIMARY KEY, label text NOT NULL);
-- +goose Down
DROP TABLE widget;
`,
			wantChecked: 0,
			wantFlagged: false,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			// One directory per case, so `checked` describes this fixture alone.
			dir := t.TempDir()
			if err := os.WriteFile(filepath.Join(dir, "20260101000000_fixture.sql"), []byte(c.sql), 0o600); err != nil {
				t.Fatalf("writing fixture: %v", err)
			}
			scan, err := checkSeedsAreIdempotent(dir)
			if err != nil {
				t.Fatal(err)
			}
			if scan.checked != c.wantChecked {
				t.Errorf("examined %d INSERT statement(s), want %d", scan.checked, c.wantChecked)
			}
			if flagged := len(scan.offenders) > 0; flagged != c.wantFlagged {
				t.Errorf("flagged = %v, want %v (offenders: %v)", flagged, c.wantFlagged, scan.offenders)
			}
		})
	}
}

// Non-.sql files and subdirectories are ignored, so a stray README in migrations/ can neither
// break the check nor pad its count.
func TestTheSeedCheckIgnoresNonMigrations(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte("INSERT INTO widget VALUES (1);"), 0o600); err != nil {
		t.Fatalf("writing fixture: %v", err)
	}
	if err := os.Mkdir(filepath.Join(dir, "archive"), 0o750); err != nil {
		t.Fatalf("making subdirectory: %v", err)
	}
	scan, err := checkSeedsAreIdempotent(dir)
	if err != nil {
		t.Fatal(err)
	}
	if scan.checked != 0 || len(scan.offenders) != 0 {
		t.Errorf("examined %d and flagged %v, want a directory with no migrations to yield nothing",
			scan.checked, scan.offenders)
	}
}

// A directory that is not there is an error, not a silently empty scan — the shape that lets
// a renamed folder turn a guard into a no-op.
func TestTheSeedCheckFailsOnAMissingDirectory(t *testing.T) {
	if _, err := checkSeedsAreIdempotent(filepath.Join(t.TempDir(), "nope")); err == nil {
		t.Error("scanning a missing directory returned no error")
	}
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

// sqlStatement is one statement out of a migration, twice over: `text` verbatim — what would
// actually run, comments and all — and `code`, the same statement with comments removed and every
// string literal blanked. Every DECISION is made on `code`; `text` is only ever shown or executed.
type sqlStatement struct {
	text string
	code string
}

// sqlStatements splits SQL on the semicolons that end a statement, which means the ones that are
// outside both comments and string literals.
//
// ⭐ WHY IT WALKS THE TEXT INSTEAD OF CALLING strings.Split, because both cheaper readings have
// now been tried and both were wrong in a way that took a real migration to expose:
//
//   - Searching RAW text for "ON CONFLICT" matched the COMMENT that explains the clause, so
//     deleting the real clause left the check green (AOC-005 verify round 1).
//   - Splitting on every `;` and stripping comments with no idea what a string literal is broke
//     the moment a seed carried prose: AOC-009's source_note values contain both semicolons
//     ("…2026-09-13); corroborated by 35 armory source rows") and apostrophes, so the statements
//     were chopped mid-sentence and 4 of 8 reported as offenders with the clause plainly present.
//     A guard that cries wolf on correct SQL gets switched off, which is the same outcome as one
//     that never fires.
//
// Blanking literals rather than deleting them also closes the mirror image of the AOC-005 bug: a
// seed whose TEXT contains the words "ON CONFLICT" can no longer vouch for itself.
func sqlStatements(s string) []sqlStatement {
	var out []sqlStatement
	var text, code strings.Builder
	flush := func() {
		if strings.TrimSpace(code.String()) != "" {
			out = append(out, sqlStatement{text: text.String(), code: code.String()})
		}
		text.Reset()
		code.Reset()
	}
	for i := 0; i < len(s); {
		switch {
		case s[i] == '\'':
			// A string literal, '' being an escaped quote inside one. Copied verbatim into the
			// statement and blanked in the code, so neither a `;` nor a `--` inside it is read
			// as SQL.
			j := i + 1
			for j < len(s) {
				if s[j] != '\'' {
					j++
					continue
				}
				if j+1 < len(s) && s[j+1] == '\'' {
					j += 2
					continue
				}
				j++
				break
			}
			text.WriteString(s[i:j])
			code.WriteString("''")
			i = j
		case strings.HasPrefix(s[i:], "--"):
			j := strings.IndexByte(s[i:], '\n')
			if j < 0 {
				text.WriteString(s[i:])
				i = len(s)
				continue
			}
			text.WriteString(s[i : i+j+1])
			code.WriteByte('\n') // keep line structure, drop the prose
			i += j + 1
		case strings.HasPrefix(s[i:], "/*"):
			j := strings.Index(s[i+2:], "*/")
			if j < 0 {
				text.WriteString(s[i:])
				i = len(s)
				continue
			}
			text.WriteString(s[i : i+2+j+2])
			i += 2 + j + 2
		case s[i] == ';':
			text.WriteByte(';')
			code.WriteByte(';')
			flush()
			i++
		default:
			text.WriteByte(s[i])
			code.WriteByte(s[i])
			i++
		}
	}
	flush()
	return out
}

// The splitter is load-bearing — it is what the static check reads — so it is pinned in both
// directions. Replaces TestStripSQLCommentsRemovesProse, whose three cases are the first three
// here; the rest are the string-literal cases that test could not express.
func TestSQLStatementsSeparatesCodeFromProse(t *testing.T) {
	cases := []struct {
		name           string
		in             string
		wantStatements int
		codeHas        []string
		codeLacks      []string
	}{
		{"line comment", "-- ON CONFLICT DO NOTHING\nINSERT INTO t VALUES (1);", 1,
			[]string{"INSERT INTO t"}, []string{"ON CONFLICT"}},
		{"trailing comment", "INSERT INTO t VALUES (1); -- ON CONFLICT\n", 1,
			[]string{"INSERT INTO t"}, []string{"ON CONFLICT"}},
		{"block comment", "/* ON CONFLICT */ INSERT INTO t VALUES (1);", 1,
			[]string{"INSERT INTO t"}, []string{"ON CONFLICT"}},
		{"a real clause survives", "INSERT INTO t VALUES (1) ON CONFLICT DO NOTHING;", 1,
			[]string{"ON CONFLICT"}, nil},
		{"a semicolon inside a literal does not end the statement",
			"INSERT INTO t VALUES ('a; b') ON CONFLICT DO NOTHING;", 1,
			[]string{"INSERT INTO t", "ON CONFLICT"}, []string{"a; b"}},
		{"an escaped quote does not end the literal",
			"INSERT INTO t VALUES ('Pierre''s note; and more') ON CONFLICT DO NOTHING;", 1,
			[]string{"ON CONFLICT"}, []string{"and more"}},
		{"a comment marker inside a literal is not a comment",
			"INSERT INTO t VALUES ('a -- b') ON CONFLICT DO NOTHING;", 1,
			[]string{"ON CONFLICT"}, []string{"a -- b"}},
		{"the words ON CONFLICT inside a literal do not count",
			"INSERT INTO t VALUES ('ON CONFLICT DO NOTHING');", 1,
			[]string{"INSERT INTO t"}, []string{"ON CONFLICT"}},
		{"two statements", "INSERT INTO t VALUES (1); INSERT INTO u VALUES (2);", 2,
			[]string{"INSERT INTO"}, nil},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := sqlStatements(c.in)
			if len(got) != c.wantStatements {
				t.Fatalf("split into %d statement(s), want %d: %+v", len(got), c.wantStatements, got)
			}
			all := ""
			for _, s := range got {
				all += s.code
			}
			for _, want := range c.codeHas {
				if !strings.Contains(all, want) {
					t.Errorf("code lost %q: %q", want, all)
				}
			}
			for _, unwanted := range c.codeLacks {
				if strings.Contains(all, unwanted) {
					t.Errorf("code kept %q, which is prose or a literal: %q", unwanted, all)
				}
			}
			// `text` is what would run, so each statement must be a verbatim slice of the
			// input — not reassembled, not re-quoted. (A trailing comment after the last `;`
			// is not a statement and is deliberately dropped, so this checks containment
			// rather than a full round trip.)
			for _, st := range got {
				if !strings.Contains(c.in, st.text) {
					t.Errorf("statement text is not verbatim from the input: %q", st.text)
				}
			}
		})
	}
}

// ⭐ AND THE SAME SHAPE AS AOC-005's FIXTURE TEST, for the literal-aware half: a seed whose SQL
// and whose PROSE disagree. Without these the fix above could have been "stop looking", and
// nothing would have noticed.
func TestTheSeedCheckStillFlagsASeedThatOnlyTALKSAboutTheClause(t *testing.T) {
	dir := t.TempDir()
	sql := `-- +goose Up
CREATE TABLE widget (id int PRIMARY KEY, label text NOT NULL);

INSERT INTO widget (id, label) VALUES (1, 'a; label mentioning ON CONFLICT DO NOTHING');
-- +goose Down
DROP TABLE widget;
`
	if err := os.WriteFile(filepath.Join(dir, "20260101000000_fixture.sql"), []byte(sql), 0o600); err != nil {
		t.Fatalf("writing fixture: %v", err)
	}
	scan, err := checkSeedsAreIdempotent(dir)
	if err != nil {
		t.Fatal(err)
	}
	if scan.checked != 1 {
		t.Errorf("examined %d statement(s), want 1 — a semicolon inside a literal must not split it",
			scan.checked)
	}
	if len(scan.offenders) != 1 {
		t.Errorf("flagged %v, want the one INSERT: its clause exists only inside a string",
			scan.offenders)
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

// ⭐ THE CLI AND THE LIBRARY MUST BE THE SAME GOOSE.
//
// `make migrate-up` applies migrations with the goose CLI; the tests in this file apply them
// with the goose LIBRARY. Defect ❌2 of AOC-005 verify round 1 was exactly these two drifting:
// the CLI on PATH was Homebrew v3.28.0 while go.mod pinned the library at v3.24.1, so the
// migration that was tested and the migration that was run were applied by different code.
//
// The CLI is now pinned in the Makefile (GOOSE_VERSION) rather than taken from PATH, and this
// test is what stops the two pins drifting apart again — a comment asking people to keep two
// numbers in sync is not a mechanism.
func TestTheGooseCLIMatchesTheGooseLibrary(t *testing.T) {
	makefile := readRepoFile(t, "Makefile")
	gomod := readRepoFile(t, "go.mod")

	cli := findSubmatch(t, makefile, `(?m)^GOOSE_VERSION\s*:?=\s*(v[0-9][^\s]*)`,
		"GOOSE_VERSION is not set in the Makefile")
	lib := findSubmatch(t, gomod, `(?m)^\s*github\.com/pressly/goose/v3\s+(v[0-9][^\s]*)`,
		"go.mod does not require github.com/pressly/goose/v3")

	if cli != lib {
		t.Errorf("the goose CLI and the goose library are different versions:\n"+
			"  Makefile GOOSE_VERSION = %s  (applies migrations for `make migrate-up`)\n"+
			"  go.mod    library       = %s  (applies migrations in these tests)\n"+
			"Migrations would be applied by different code in test and in production.", cli, lib)
	}
}

// And the tool the GATE runs must be the pinned one too, not a PATH binary.
func TestTheGateHasAPinnedSQLCToRun(t *testing.T) {
	makefile := readRepoFile(t, "Makefile")
	if !regexp.MustCompile(`(?m)^sqlc-cmd:`).MatchString(makefile) {
		t.Error("no `sqlc-cmd` target: bin/gate api falls back to whatever sqlc is on PATH, " +
			"and sqlc's generated output is version-specific")
	}
	findSubmatch(t, makefile, `(?m)^SQLC_VERSION\s*:?=\s*(v[0-9][^\s]*)`,
		"SQLC_VERSION is not pinned in the Makefile")
	if !strings.Contains(makefile, "cmd/sqlc@$(SQLC_VERSION)") {
		t.Error("the sqlc invocation does not use the pinned SQLC_VERSION")
	}
}

func readRepoFile(t *testing.T, name string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("../..", name))
	if err != nil {
		t.Fatalf("reading %s: %v", name, err)
	}
	return string(b)
}

func findSubmatch(t *testing.T, haystack, pattern, absent string) string {
	t.Helper()
	m := regexp.MustCompile(pattern).FindStringSubmatch(haystack)
	if m == nil {
		t.Fatal(absent)
	}
	return m[1]
}

// ⭐ THE DOCKERFILE AND go.mod MUST NAME THE SAME GO.
//
// Lives beside the other two pin tests rather than in a package of its own: the pattern this
// diff established is "the thing that stops two written-down versions drifting apart is a test,
// and they all live together". A third location would be a third place to look.
//
// WHY IT EXISTS. The Dockerfile pins `golang:1.23-alpine` to match `go 1.23`, and says so in its
// own header: "Bumping Go means bumping BOTH, in the same commit." That comment failed on the
// very day it was being relied on — a tools.go added to pin sqlc pulled sqlc's own `go 1.26.0`
// into this module's go.mod, so the image became a minor version too old to build the module it
// exists to build, and nothing anywhere went red. A comment asking people to keep two numbers in
// sync is not a mechanism. (AOC-005 verify round 2.)
func TestTheDockerfileGoVersionMatchesGoMod(t *testing.T) {
	gomod := readRepoFile(t, "go.mod")
	dockerfile := readRepoFile(t, "Dockerfile")

	// Only the major.minor line matters: `golang:1.23-alpine` tracks every 1.23.x patch, so
	// go.mod saying `go 1.23.4` is agreement, not drift.
	declared := findSubmatch(t, gomod, `(?m)^go\s+([0-9]+\.[0-9]+)`,
		"go.mod has no `go` directive")
	image := findSubmatch(t, dockerfile, `(?m)^FROM\s+golang:([0-9]+\.[0-9]+)`,
		"the Dockerfile has no `FROM golang:<version>` build stage")

	if declared != image {
		t.Errorf("the build image and the module declare different Go versions:\n"+
			"  go.mod     go %s   (what the module requires)\n"+
			"  Dockerfile golang:%s-alpine   (what Railway actually builds with)\n"+
			"Bump both in the same commit, or production is built by a toolchain the module "+
			"does not expect.", declared, image)
	}
}
