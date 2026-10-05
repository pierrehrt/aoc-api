package builds_test

import (
	"errors"
	"net/url"
	"testing"

	"github.com/pierrehrt/aoc-api/internal/builds"
	"github.com/pierrehrt/aoc-api/internal/httpx"
)

func parse(t *testing.T, raw string) builds.Build {
	t.Helper()
	q, err := url.ParseQuery(raw)
	if err != nil {
		t.Fatal(err)
	}
	b, err := builds.Parse(q)
	if err != nil {
		t.Fatalf("Parse(%q): %v", raw, err)
	}
	return b
}

func TestParseReadsSlotsTheClassAndPresence(t *testing.T) {
	b := parse(t, "gear=test-head:1&gear=test-ring-left:2,test-main:3&gear_class=test-class-a&q=ignored")
	if !b.Present || b.Class != "test-class-a" || len(b.Entries) != 3 {
		t.Fatalf("got %+v", b)
	}
	if id, ok := b.Item("test-main"); !ok || id != 3 {
		t.Errorf("test-main = %d %v, want 3", id, ok)
	}
	if b := parse(t, "q=x&rarity=epic"); b.Present || len(b.Values()) != 0 {
		t.Errorf("no builder parameter: present %v, values %v — want neither", b.Present, b.Values())
	}
	if b := parse(t, "gear="); !b.Present || len(b.Entries) != 0 {
		t.Errorf("gear= is an open, empty builder: got %+v", b)
	}
	if got := parse(t, "gear=").Values().Encode(); got != "gear=" {
		t.Errorf("an open, empty builder keeps its one parameter: %q", got)
	}
}

func TestValuesIsParsesInverse(t *testing.T) {
	for _, raw := range []string{
		"gear=test-head%3A1&gear=test-main%3A3&gear_class=test-class-b",
		"gear_class=test-class-a",
		"gear=",
		"gear=test-off%3A7",
	} {
		if got := parse(t, raw).Values().Encode(); got != raw {
			t.Errorf("round trip of %q gave %q", raw, got)
		}
	}
}

func TestParseRefusesWhatItCannotRead(t *testing.T) {
	for _, raw := range []string{
		"gear=test-head",            // no id
		"gear=test-head:abc",        // not a number
		"gear=test-head:0",          // ids start at 1
		"gear=test-head:-4",         // negative
		"gear=test-head:9999999999", // beyond int32
		"gear=Test_Head:1",          // not a slug
		"gear=:1",                   // no slot
		"gear=test-head:1&gear=test-head:2",
		"gear_class=Not A Slug",
	} {
		q, _ := url.ParseQuery(raw)
		if _, err := builds.Parse(q); !errors.Is(err, httpx.ErrInvalid) {
			t.Errorf("Parse(%q) = %v, want ErrInvalid (a 400)", raw, err)
		}
	}
}

func TestParseAddTakesAnIdOrASlotAndAnId(t *testing.T) {
	q, _ := url.ParseQuery("add=7")
	if a, ok, err := builds.ParseAdd(q); err != nil || !ok || a.Slot != "" || a.ItemID != 7 {
		t.Errorf("add=7: %+v %v %v", a, ok, err)
	}
	q, _ = url.ParseQuery("add=test-off:7")
	if a, ok, err := builds.ParseAdd(q); err != nil || !ok || a.Slot != "test-off" || a.ItemID != 7 {
		t.Errorf("add=test-off:7: %+v %v %v", a, ok, err)
	}
	q, _ = url.ParseQuery("q=x")
	if _, ok, err := builds.ParseAdd(q); ok || err != nil {
		t.Errorf("no add: ok %v err %v", ok, err)
	}
	for _, bad := range []string{"add=x", "add=test-off:", "add=0"} {
		q, _ = url.ParseQuery(bad)
		if _, _, err := builds.ParseAdd(q); !errors.Is(err, httpx.ErrInvalid) {
			t.Errorf("%s: %v, want ErrInvalid", bad, err)
		}
	}
}

func TestWithAndWithoutKeepTheBuildPresent(t *testing.T) {
	var b builds.Build
	b = b.With("test-head", 1)
	if !b.Present {
		t.Fatal("With must make the build present")
	}
	b = b.With("test-head", 10)
	if id, _ := b.Item("test-head"); id != 10 || len(b.Entries) != 1 {
		t.Errorf("With replaces: %+v", b)
	}
	if b = b.Without("test-head"); !b.Present || len(b.Entries) != 0 {
		t.Errorf("emptying the build keeps the builder open: %+v", b)
	}
}
