// Package version holds the build identity reported by GET /health.
//
// The values are injected at link time (see the Makefile's -ldflags), so a running
// container can always be traced back to a commit. They are vars, not consts,
// precisely because -ldflags -X can only write to a var.
package version

// Version is the semver tag this binary was built from, or "dev".
var Version = "dev"

// Commit is the short git SHA, or "none".
var Commit = "none"
