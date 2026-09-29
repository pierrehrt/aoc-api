package version

import (
	"os"
	"regexp"
	"strings"
	"testing"
)

// The Dockerfile builds the version into /health from the repo's VERSION file and refuses anything
// that is not x.y.z (AOC-015). This is the same rule, in CI, so a bad bump fails a PR instead of a
// Railway build.
func TestTheVersionFileIsSemver(t *testing.T) {
	b, err := os.ReadFile("../../VERSION")
	if err != nil {
		t.Fatalf("the VERSION file must exist at the repo root: %v", err)
	}
	v := strings.TrimSuffix(string(b), "\n")
	if !regexp.MustCompile(`^[0-9]+\.[0-9]+\.[0-9]+$`).MatchString(v) {
		t.Errorf("VERSION = %q, want one x.y.z line", string(b))
	}
}
