package templates_test

import (
	"testing"

	"github.com/pierrehrt/aoc-api/internal/items"
	"github.com/pierrehrt/aoc-api/internal/templates"
)

// AOC-048 verify round 1: a line is "identical" only when EVERYTHING it shows matches — the flags
// included. Two sources that differ only by the raid or Unchained flag are two lines; folding them
// would hide one of the two places an item comes from. (0 such pairs in the corpus on 2026-09-30,
// which is why nothing else pinned it.) Fixtures obviously fake.
func TestSourcesThatDifferOnlyByAFlagAreTwoLines(t *testing.T) {
	s := func(v string) *string { return &v }
	base := items.SourceRef{AcquisitionTypeName: s("test-type"), Place: s("Test Place"), Boss: s("Test Boss"), TierName: s("Test Tier")}
	for name, other := range map[string]items.SourceRef{
		"raid":      func() items.SourceRef { o := base; o.IsRaid = true; return o }(),
		"Unchained": func() items.SourceRef { o := base; o.Unchained = true; return o }(),
	} {
		d := templates.NewItemData(items.Detail{Sources: []items.SourceRef{base, other}})
		if d.Sources != 2 || len(d.Groups) != 1 || len(d.Groups[0].Rows) != 2 {
			t.Errorf("a source flagged %s and its unflagged twin: %d lines, want 2", name, d.Sources)
		}
	}
}
