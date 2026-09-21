package db_test

// AOC-011 verify round 2 — the allowlist must be an EXACT match, and a test has to say so.
//
// ⚠️ WHY THIS FILE EXISTS. `IsLocalHost` is written as an exact `switch`, and it is correct.
// But round 2 mutation-tested it: replacing the switch with
//
//	strings.Contains(host, "localhost") || strings.Contains(host, "127.0.0.1")
//
// left the whole suite GREEN, because neither `target_test.go` nor `migrate_test.go`'s remote list
// contains a single host that merely *looks* local. That mutation is not exotic — a substring
// match is the obvious "fix" for the next person who wants `localhost:5433` to work with a prefix
// or a suffix, and it would make `localhost.evil.com` and `127.0.0.1.attacker.net` read as a
// developer machine. The guard decides whether nine tables get DELETEd on a live database, so the
// one shape it must never accept is the one that is spelled almost right.
//
// Pure: no database, so it runs everywhere.

import (
	"strings"
	"testing"

	"github.com/pierrehrt/aoc-api/internal/db"
)

// Hosts that contain an allowlisted name but ARE NOT one. A DNS wildcard makes every one of these
// registrable and pointable anywhere — `localtest.me` already resolves to 127.0.0.1 in the wild,
// which is exactly how round 2 proved the guard against a real reachable database.
func TestAHostThatMerelyLooksLocalIsNotLocal(t *testing.T) {
	lookalikes := []string{
		"localhost.evil.com:5432",
		"localhost.evil.com",
		"notlocalhost:5432",
		"my-localhost:5432",
		"localhost-prod.example.com:5432",
		"127.0.0.1.evil.com:5432",
		"db.example.com.localhost.evil.com:5432",
		"postgres.railway.internal:5432", // contains neither, but pins the ordinary case too
		"dbprod:5432",                    // "db" is allowlisted; "dbprod" is not
		"postgresql:5432",                // "postgres" is allowlisted; "postgresql" is not
	}
	for _, h := range lookalikes {
		if db.IsLocalHost(h) {
			t.Errorf("%q was accepted as LOCAL — the importer would DELETE nine tables on it", h)
		}
	}
}

// The same thing one level up, through the door the importer actually uses: a lookalike host must
// be refused by ConfirmTarget, and naming it back must still be required.
func TestConfirmTargetRefusesALookalikeLocalHost(t *testing.T) {
	const url = "postgres://aoc:hunter2@localhost.evil.com:5432/aoc_prod"
	const host = "localhost.evil.com:5432"

	banner, err := db.ConfirmTarget(url, "", "-confirm-host")
	if err == nil {
		t.Fatalf("a lookalike host was accepted with no confirmation, banner %q", banner)
	}
	for _, want := range []string{host, "NOT LOCAL", "Nothing has been written"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("the refusal never mentions %q:\n%v", want, err)
		}
	}
	if strings.Contains(err.Error(), "hunter2") {
		t.Errorf("the refusal leaked the password: %v", err)
	}

	// And it is still reachable deliberately, the same way any other remote host is.
	if _, err := db.ConfirmTarget(url, host, "-confirm-host"); err != nil {
		t.Errorf("an exactly-confirmed lookalike was refused: %v", err)
	}
}

// The allowlist is case-sensitive, and that is the safe direction: an unusual spelling is refused
// rather than accepted. Pinned so nobody "fixes" it by lowercasing both sides, which would quietly
// widen the list.
func TestTheAllowlistIsCaseSensitiveAndFailsClosed(t *testing.T) {
	for _, h := range []string{"LOCALHOST:5433", "LocalHost", "POSTGRES:5432"} {
		if db.IsLocalHost(h) {
			t.Errorf("%q was accepted as local; the allowlist is meant to be exact", h)
		}
	}
}
