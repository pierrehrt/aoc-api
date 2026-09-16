// Package version holds the build identity reported by GET /health.
//
// The values are injected at link time (see the Makefile's -ldflags), so a running
// container can always be traced back to a commit. They are vars, not consts,
// precisely because -ldflags -X can only write to a var.
package version

import "os"

// Version is the semver tag this binary was built from, or "dev".
var Version = "dev"

// Commit is the git SHA, or "none".
var Commit = "none"

// Resolve fills in anything the linker could not, and is called once from main.
//
// ⭐ WHY THIS EXISTS. On Railway the build args turned out NOT to carry the commit:
// `${{RAILWAY_GIT_COMMIT_SHA}}` used as a service variable resolves to an EMPTY STRING,
// because Railway injects its git variables into the deployed container rather than into
// the variable set that references are resolved against. The first production deploy
// therefore reported `"commit":""` — worse than "none", since an empty string reads as a
// field nobody set rather than one that failed.
//
// So the commit is read at RUNTIME as a fallback. Railway documents these as present
// "if the deploy originated from a GitHub trigger", which every deploy of this service is.
// Link-time still wins when it is set, because a local `make build` knows its own SHA and
// a container may be re-run against a different environment.
func Resolve() {
	if !meaningful(Commit) {
		if sha := os.Getenv("RAILWAY_GIT_COMMIT_SHA"); sha != "" {
			Commit = sha
		}
	}
	// Never report an empty string. A blank field is indistinguishable from a field the
	// serialiser dropped; "unknown" is a claim, and a claim can be investigated.
	if !meaningful(Commit) {
		Commit = "unknown"
	}
	if !meaningful(Version) {
		Version = "unknown"
	}
}

// meaningful reports whether a build-identity value actually identifies anything.
// "none" is the compiled-in default and "" is what an unresolved build arg leaves behind;
// neither tells you which code is running.
func meaningful(v string) bool {
	return v != "" && v != "none"
}
