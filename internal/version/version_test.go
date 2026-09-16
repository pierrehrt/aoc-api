package version

import "testing"

// The bug this pins: the first production deploy reported `"commit":""`, because the
// Railway build arg resolved to an empty string. Every case below is a shape that
// actually occurred or could occur on a deploy.
func TestResolve(t *testing.T) {
	cases := []struct {
		name                string
		commit, ver, env    string
		wantCommit, wantVer string
	}{
		{"link-time values win", "abc1234", "1.2.3", "deadbeef", "abc1234", "1.2.3"},
		{"empty commit falls back to the Railway env var", "", "1.2.3", "deadbeef", "deadbeef", "1.2.3"},
		{`"none" also falls back — it is the compiled-in default`, "none", "1.2.3", "deadbeef", "deadbeef", "1.2.3"},
		{"no link-time value and no env var reports unknown, never blank", "", "", "", "unknown", "unknown"},
		{`"none" with no env var reports unknown`, "none", "none", "", "unknown", "unknown"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			Commit, Version = c.commit, c.ver
			t.Setenv("RAILWAY_GIT_COMMIT_SHA", c.env)
			Resolve()
			if Commit != c.wantCommit {
				t.Errorf("Commit = %q, want %q", Commit, c.wantCommit)
			}
			if Version != c.wantVer {
				t.Errorf("Version = %q, want %q", Version, c.wantVer)
			}
		})
	}
}

// An empty commit must never reach /health. This is the whole point of the change.
func TestResolveNeverLeavesABlankField(t *testing.T) {
	for _, start := range []string{"", "none"} {
		Commit, Version = start, start
		t.Setenv("RAILWAY_GIT_COMMIT_SHA", "")
		Resolve()
		if Commit == "" || Version == "" {
			t.Fatalf("Resolve left a blank field from %q: commit=%q version=%q", start, Commit, Version)
		}
	}
}
