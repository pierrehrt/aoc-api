package db_test

// ⭐ ADDED BY VERIFY (AOC-009, verify round 1). The gap it closes:
//
// Every seed in this repo ends `ON CONFLICT (slug) DO NOTHING`, which is what makes a re-run safe.
// The same clause also makes a seed that contains TWO rows with the same slug insert only ONE of
// them — silently, with no error and no warning. The generators derive a slug from a name by
// lowercasing and replacing punctuation (`Yakhmar's Cave` -> `yakhmar-s-cave`), so two distinct
// names CAN collapse onto one slug, and the day they do, a place quietly stops existing.
//
// TestTheSeededCountsAreWhatWasMeasured does not catch that on its own: its numbers are written
// down by a person reading the generator's output, so a collision that is present when the numbers
// are updated is simply baked in. This test compares the database against the MIGRATION FILE
// instead — how many value tuples the statement lists versus how many rows the table ends up with —
// so it needs no hardcoded number and cannot be updated into agreement with a defect.
//
// It reuses sqlStatements, so parentheses and semicolons inside string literals cannot confuse the
// count, and the `(SELECT id FROM …)` foreign-key sub-selects sit at depth 2 and are not mistaken
// for tuples.

import (
	"context"
	"os"
	"strings"
	"testing"
)

func TestNoSeededRowIsSilentlyDroppedByOnConflict(t *testing.T) {
	d, _ := migratedDB(t)

	listed := map[string]int{}
	for _, f := range migrationFiles(t) {
		b, err := os.ReadFile(f) // #nosec G304 -- fixed directory listed by migrationFiles
		if err != nil {
			t.Fatalf("reading %s: %v", f, err)
		}
		body := string(b)
		if i := strings.Index(body, "-- +goose Down"); i >= 0 {
			body = body[:i]
		}
		for _, stmt := range sqlStatements(body) {
			table, n, ok := insertedTuples(stmt.code)
			if !ok {
				continue
			}
			if n == 0 {
				t.Errorf("%s: an INSERT into %s lists no value tuples — the counter is broken, "+
					"not the migration", f, table)
			}
			listed[table] += n
		}
	}
	if len(listed) < 15 {
		t.Fatalf("found INSERTs for only %d tables, want every taxonomy and place table — the "+
			"statement scanner has stopped finding them: %v", len(listed), listed)
	}

	for table, want := range listed {
		var got int
		// #nosec G202 -- the table name comes from the repo's own migration files.
		if err := d.QueryRowContext(context.Background(),
			"SELECT count(*) FROM "+table).Scan(&got); err != nil {
			t.Fatalf("counting %s: %v", table, err)
		}
		if got != want {
			t.Errorf("%s: the migration lists %d rows but the table holds %d — ON CONFLICT DO "+
				"NOTHING swallowed %d row(s), almost certainly two names sharing one slug",
				table, want, got, want-got)
		}
	}
}

// insertedTuples reports the table an INSERT targets and how many top-level value tuples it lists.
// It is given the statement's `code`, so string literals are already blanked and no bracket or
// semicolon inside a source_note can reach it.
func insertedTuples(code string) (table string, n int, ok bool) {
	upper := strings.ToUpper(code)
	i := strings.Index(upper, "INSERT INTO ")
	if i < 0 {
		return "", 0, false
	}
	fields := strings.Fields(code[i+len("INSERT INTO "):])
	if len(fields) == 0 {
		return "", 0, false
	}
	table = fields[0]
	if j := strings.IndexByte(table, '('); j >= 0 {
		table = table[:j]
	}

	// The ON CONFLICT clause carries its own parenthesised arbiter, which is not a value tuple.
	body := code
	if j := strings.LastIndex(upper, "ON CONFLICT"); j > i {
		body = code[:j]
	}
	v := strings.Index(strings.ToUpper(body), " VALUES")
	if v < 0 {
		return table, 0, false
	}

	depth := 0
	for _, r := range body[v+len(" VALUES"):] {
		switch r {
		case '(':
			depth++
			if depth == 1 {
				n++
			}
		case ')':
			depth--
		}
	}
	return table, n, true
}
