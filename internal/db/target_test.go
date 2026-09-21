package db_test

// AOC-011 verify round 1, F2 — the importer must never be able to rebuild a database it did not
// say the name of first.
//
// These are pure: no database, so they run everywhere, including in the gate with nothing up.
// What they pin is the SHAPE of the refusal, because the failure this prevents is not a bug in
// the code — it is a person with a production DATABASE_URL still exported in their shell.

import (
	"strings"
	"testing"

	"github.com/pierrehrt/aoc-api/internal/db"
)

const flagName = "-confirm-host"

func TestALocalTargetNeedsNoConfirmationAndSaysSo(t *testing.T) {
	for _, url := range []string{
		"postgres://aoc:aoc@localhost:5433/aoc_dev?sslmode=disable",
		"postgres://aoc:aoc@127.0.0.1:5433/aoc_dev",
		"postgres://aoc:aoc@postgres:5432/aoc_dev", // the CI service container
		"postgres://aoc:aoc@db:5432/aoc_dev",       // a compose service name
	} {
		banner, err := db.ConfirmTarget(url, "", flagName)
		if err != nil {
			t.Errorf("%s was refused: %v", db.HostOf(url), err)
			continue
		}
		if !strings.Contains(banner, db.HostOf(url)) {
			t.Errorf("the banner for %s does not name the host: %q", db.HostOf(url), banner)
		}
		if !strings.Contains(banner, "(local)") {
			t.Errorf("the banner for %s does not say it is local: %q", db.HostOf(url), banner)
		}
	}
}

func TestARemoteTargetIsRefusedUntilItIsNamedBack(t *testing.T) {
	const url = "postgres://u:p@monorail.proxy.rlwy.net:37421/railway"
	const host = "monorail.proxy.rlwy.net:37421"

	// 1. No confirmation at all — the common case, and the dangerous one.
	banner, err := db.ConfirmTarget(url, "", flagName)
	if err == nil {
		t.Fatalf("a production URL was accepted with no confirmation, banner %q", banner)
	}
	for _, want := range []string{host, "NOT LOCAL", "Nothing has been written", flagName} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("the refusal never mentions %q:\n%v", want, err)
		}
	}

	// 2. Confirmed, but a DIFFERENT host — someone changed DATABASE_URL and reused the command
	//    from their shell history. That is the near-miss this catches.
	if _, err := db.ConfirmTarget(url, "some-other-host:5432", flagName); err == nil {
		t.Error("a mismatched -confirm-host was accepted")
	} else if !strings.Contains(err.Error(), "different host") {
		t.Errorf("the mismatch is not explained: %v", err)
	}

	// 3. Named back exactly — allowed, and still loud about what it is.
	banner, err = db.ConfirmTarget(url, host, flagName)
	if err != nil {
		t.Fatalf("an exactly-confirmed host was still refused: %v", err)
	}
	if !strings.Contains(banner, "NOT LOCAL") || !strings.Contains(banner, host) {
		t.Errorf("the confirmed banner is not loud enough: %q", banner)
	}
}

func TestAnEmptyOrHostlessURLIsRefused(t *testing.T) {
	for _, url := range []string{"", "   ", "postgres://"} {
		if _, err := db.ConfirmTarget(url, "", flagName); err == nil {
			t.Errorf("%q was accepted as a target", url)
		}
	}
}

// A refusal is printed. It must never be the place a password turns up.
func TestNoRefusalEverPrintsTheCredentials(t *testing.T) {
	const url = "postgres://admin:sup3rsecret@db.example.com:5432/prod"
	for _, confirmed := range []string{"", "wrong-host", "db.example.com:5432"} {
		banner, err := db.ConfirmTarget(url, confirmed, flagName)
		got := banner
		if err != nil {
			got = err.Error()
		}
		if strings.Contains(got, "sup3rsecret") || strings.Contains(got, "admin") {
			t.Errorf("credentials leaked into the output for confirmed=%q: %q", confirmed, got)
		}
	}
}
