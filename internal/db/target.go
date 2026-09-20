package db

// Which database am I about to touch?
//
// ⭐ THIS EXISTS BECAUSE THE ANSWER MUST NEVER BE A GUESS. The Makefile's `require_db` echoes the
// target host before every migration, and the migration tests refuse outright to run anywhere but
// a developer machine — both added after AOC-005 verify round 1 found that "hitting production
// takes effort" was not actually true of any path. `cmd/import-armory` then walked straight past
// both: it DELETEs nine tables and rebuilds them, and it chose its target from `DATABASE_URL`
// alone without ever printing the host. AOC-011 verify round 1.
//
// ⚠️ The environment is the trap. The Makefile tells you to `export DATABASE_URL=<production>` to
// run a migration, and that export is still in the shell that runs the next command. A tool whose
// only clue about its target is that variable cannot be operated safely by a person, however
// careful — so the tool says the host out loud, and a remote one has to be named back to it.

import (
	"fmt"
	"strings"
)

// HostOf pulls the host[:port] out of a postgres URL without parsing the credentials, so a
// password can never reach a log line, a terminal or a test failure message.
func HostOf(url string) string {
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

// IsLocalHost accepts only what a developer machine or a CI service container looks like.
//
// Deliberately an allowlist: a denylist of "known production hostnames" is a list someone forgets
// to update exactly once, and the one time it is forgotten is the time it mattered.
func IsLocalHost(hostPort string) bool {
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

// ConfirmTarget names the database a URL points at, and refuses a remote one that the caller has
// not named back.
//
// It returns the banner a command should print BEFORE it writes anything. `confirmed` is whatever
// the operator passed to the command's opt-in flag: for a local target it is ignored, and for a
// remote one it must equal the host exactly. Typing the host is the point — a bare `-yes` is a
// flag people learn to add by reflex, while a hostname has to be read off the URL in front of you.
func ConfirmTarget(url, confirmed, flagName string) (banner string, err error) {
	if strings.TrimSpace(url) == "" {
		return "", fmt.Errorf("DATABASE_URL is not set — refusing to guess which database to write to")
	}
	host := HostOf(url)
	if host == "" {
		return "", fmt.Errorf("DATABASE_URL names no host — refusing to write to it")
	}
	if IsLocalHost(host) {
		return fmt.Sprintf("▶ target: %s  (local)", host), nil
	}
	if confirmed != host {
		if confirmed == "" {
			return "", fmt.Errorf("target: %s  ⚠️  NOT LOCAL — this is a real database, and this "+
				"command DELETEs every item table before it rebuilds them.\n"+
				"Nothing has been written. If that is genuinely what you want, re-run with %s=%s",
				host, flagName, host)
		}
		return "", fmt.Errorf("target: %s  ⚠️  NOT LOCAL — but %s says %q, which is a different "+
			"host.\nNothing has been written. Check which database DATABASE_URL actually points at",
			host, flagName, confirmed)
	}
	return fmt.Sprintf("▶ target: %s  ⚠️  NOT LOCAL — this is a real database, confirmed by %s", host, flagName), nil
}
