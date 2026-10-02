package items

import "testing"

// AOC-050: a source row's AoC>TV section decides its tab in the source panel. A section the database
// was never told about stops the import before any write, like every other unknown name; a row with
// none is in no tab, as a row with no tier is in no tier.
func TestASectionTheDatabaseDoesNotKnowStopsTheImport(t *testing.T) {
	known, unknown, blank := "Test Section", "Test Section Nobody Seeded", ""
	l := &Lookups{Sections: map[string]int32{known: 1}}
	its := []Item{{ItemID: 1, Sources: []Source{
		{SectionRaw: &known},
		{SectionRaw: &unknown},
		{SectionRaw: &blank},
		{SectionRaw: nil},
	}}}
	got, _ := CheckResolvable(its, l)
	want := map[string]int{"Test Section Nobody Seeded": 1}
	seen := map[string]int{}
	for _, u := range got {
		if u.Kind == "section" {
			seen[u.Value] = u.Rows
		}
	}
	for v, n := range want {
		if seen[v] != n {
			t.Errorf("section %q: %d rows reported, want %d (all unresolved: %+v)", v, seen[v], n, got)
		}
	}
	if len(seen) != 1 {
		t.Errorf("sections reported: %v, want only the unknown one (a known one resolves, a blank one is no section)", seen)
	}
}
