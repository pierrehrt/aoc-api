package templates_test

import (
	"errors"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/pierrehrt/aoc-api/internal/templates"
)

func TestTruncateCutsOnAWordBoundary(t *testing.T) {
	cases := []struct {
		name, in string
		max      int
		want     string
	}{
		{"short strings are untouched", "Vistrix", 60, "Vistrix"},
		{"exactly at the cap is untouched", "abcde", 5, "abcde"},
		{"one over the cap cuts at the space", "alpha bravo", 10, "alpha…"},
		{"cuts at the LAST space, not the first", "one two three four", 14, "one two three…"},
		{"a single unbreakable word is cut hard", strings.Repeat("x", 30), 10, strings.Repeat("x", 9) + "…"},
		{"runs of whitespace collapse", "a    b", 60, "a b"},
		{"newlines collapse", "a\nb", 60, "a b"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := templates.Truncate(c.in, c.max)
			if got != c.want {
				t.Errorf("Truncate(%q, %d) = %q, want %q", c.in, c.max, got, c.want)
			}
			if utf8.RuneCountInString(got) > c.max {
				t.Errorf("result is %d runes, over the cap of %d", utf8.RuneCountInString(got), c.max)
			}
		})
	}
}

// Boss names carry accented characters. Slicing bytes instead of runes puts U+FFFD in a
// meta tag, which is the kind of thing nobody notices until it is in a search result.
func TestTruncateNeverSplitsARune(t *testing.T) {
	in := strings.Repeat("é", 40)
	got := templates.Truncate(in, 10)
	if strings.ContainsRune(got, '�') {
		t.Fatalf("Truncate split a multi-byte rune: %q", got)
	}
	if utf8.RuneCountInString(got) > 10 {
		t.Fatalf("got %d runes, want <= 10", utf8.RuneCountInString(got))
	}
}

func TestNewViewAppliesTheCaps(t *testing.T) {
	v := templates.NewView(strings.Repeat("title ", 40), strings.Repeat("desc ", 80), "https://x/")
	if n := utf8.RuneCountInString(v.Title); n > templates.MaxTitle {
		t.Errorf("title is %d runes, over MaxTitle %d", n, templates.MaxTitle)
	}
	if n := utf8.RuneCountInString(v.Description); n > templates.MaxDescription {
		t.Errorf("description is %d runes, over MaxDescription %d", n, templates.MaxDescription)
	}
}

// A page must not be able to skip its head fields — that is the contract SEO rests on.
func TestValidRejectsAMissingHeadField(t *testing.T) {
	full := templates.NewView("t", "d", "https://x/")
	for _, c := range []struct {
		name string
		v    templates.View
		want error
	}{
		{"complete view is valid", full, nil},
		{"no title", templates.View{Description: "d", Canonical: "c"}, templates.ErrNoTitle},
		{"no description", templates.View{Title: "t", Canonical: "c"}, templates.ErrNoDescription},
		{"no canonical", templates.View{Title: "t", Description: "d"}, templates.ErrNoCanonical},
		{"whitespace is not a title", templates.View{Title: "   ", Description: "d", Canonical: "c"}, templates.ErrNoTitle},
	} {
		t.Run(c.name, func(t *testing.T) {
			// errors.Is, not !=: Valid may one day wrap its sentinels, and a == check
			// would start passing for the wrong reason without anyone noticing.
			if got := c.v.Valid(); !errors.Is(got, c.want) {
				t.Errorf("Valid() = %v, want %v", got, c.want)
			}
		})
	}
}
