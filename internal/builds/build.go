package builds

import (
	"fmt"
	"math"
	"net/url"
	"regexp"
	"strconv"
	"strings"

	"github.com/pierrehrt/aoc-api/internal/httpx"
)

// The build's parameters. They ride on the Armory's own URL beside the list's filters, so their
// names must never be one of the list's (items.ParseFilters ignores them, and they ignore its).
const (
	ParamGear  = "gear"       // repeated: <slot slug>:<item id>
	ParamClass = "gear_class" // a classes.slug
	// ParamAdd is an ACTION, not state: <item id>, or <slot slug>:<item id> for a drop on that slot.
	// A page answers it with the state's own URL, so no URL keeps it (DECISIONS.md 2026-10-05).
	ParamAdd = "add"
)

// Build is a gear build as a URL carries it.
type Build struct {
	// Class is a classes.slug, or "" for any class.
	Class string
	// Entries hold at most one item per slot. Compute and Add return them in the slots' order, so
	// one build has one URL.
	Entries []Entry
	// Present says the URL names a build: a `gear` parameter, even an empty one, or a class. The page
	// opens its pane on load for a present build; `gear=` alone is an open, empty builder. An EMPTY
	// `gear_class` is not a build: the class picker sits in the Armory's form, so every filter change
	// sends one, and it would open the builder on every change.
	Present bool
}

// Entry is one slot of a build.
type Entry struct {
	Slot   string // an equip_locations.slug
	ItemID int32  // an items.item_id
}

// Add is the action of putting an item in a build: in a named slot (a drop), or wherever the rules
// put it (Slot "").
type Add struct {
	Slot   string
	ItemID int32
}

var slugRe = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,63}$`)

// Parse reads a build from a query string. Only the SHAPE is checked here — a slot or class slug no
// row has is the service's to refuse, as items.ParseFilters leaves values to the database. A
// malformed value is a 400 on both surfaces: silently dropping it would show a build the link did
// not name.
func Parse(q url.Values) (Build, error) {
	var b Build
	b.Class = strings.TrimSpace(q.Get(ParamClass))
	if b.Class != "" && !slugRe.MatchString(b.Class) {
		return Build{}, fmt.Errorf("%w: %s must be a class slug, got %q", httpx.ErrInvalid, ParamClass, b.Class)
	}
	_, gear := q[ParamGear]
	b.Present = gear || b.Class != ""
	seen := map[string]bool{}
	for _, raw := range q[ParamGear] {
		for _, v := range strings.Split(raw, ",") {
			v = strings.TrimSpace(v)
			if v == "" {
				continue // `gear=` is the open, empty builder
			}
			e, err := parseEntry(ParamGear, v, true)
			if err != nil {
				return Build{}, err
			}
			if seen[e.Slot] {
				return Build{}, fmt.Errorf("%w: %s names the slot %q twice", httpx.ErrInvalid, ParamGear, e.Slot)
			}
			seen[e.Slot] = true
			b.Entries = append(b.Entries, Entry(e))
		}
	}
	return b, nil
}

// ParseAdd reads the `add` action, if the query has one.
func ParseAdd(q url.Values) (Add, bool, error) {
	v := strings.TrimSpace(q.Get(ParamAdd))
	if v == "" {
		return Add{}, false, nil
	}
	a, err := parseEntry(ParamAdd, v, false)
	if err != nil {
		return Add{}, false, err
	}
	return a, true, nil
}

// parseEntry reads "<slot>:<id>", or "<id>" alone when the slot is optional.
func parseEntry(param, v string, slotRequired bool) (Add, error) {
	slot, id, hasSlot := strings.Cut(v, ":")
	if !hasSlot {
		slot, id = "", v
		if slotRequired {
			return Add{}, fmt.Errorf("%w: %s must be <slot>:<item id>, got %q", httpx.ErrInvalid, param, v)
		}
	} else if !slugRe.MatchString(slot) {
		return Add{}, fmt.Errorf("%w: %s must be <slot>:<item id>, got %q", httpx.ErrInvalid, param, v)
	}
	n, err := strconv.ParseInt(id, 10, 64)
	if err != nil || n < 1 || n > math.MaxInt32 {
		return Add{}, fmt.Errorf("%w: %s needs an item id (a whole number from 1), got %q", httpx.ErrInvalid, param, v)
	}
	return Add{Slot: slot, ItemID: int32(n)}, nil
}

// Values is Parse's inverse: the build's own parameters, entries in the order held (the slots'
// order once Compute or Add has returned the build). Empty when the build is not present.
func (b Build) Values() url.Values {
	v := url.Values{}
	if !b.Present {
		return v
	}
	for _, e := range b.Entries {
		v.Add(ParamGear, e.Slot+":"+strconv.Itoa(int(e.ItemID)))
	}
	if b.Class != "" {
		v.Set(ParamClass, b.Class)
	}
	if len(v) == 0 {
		v.Set(ParamGear, "") // present and empty: the builder is open with nothing in it
	}
	return v
}

// Item is the item in a slot.
func (b Build) Item(slot string) (int32, bool) {
	for _, e := range b.Entries {
		if e.Slot == slot {
			return e.ItemID, true
		}
	}
	return 0, false
}

// With puts an item in a slot, replacing what was there. The build becomes present.
func (b Build) With(slot string, id int32) Build {
	out := b.Without(slot)
	out.Entries = append(out.Entries, Entry{Slot: slot, ItemID: id})
	return out
}

// Without empties the slots named. The build stays present: emptying it does not close the builder.
func (b Build) Without(slots ...string) Build {
	drop := map[string]bool{}
	for _, s := range slots {
		drop[s] = true
	}
	out := Build{Class: b.Class, Present: true}
	for _, e := range b.Entries {
		if !drop[e.Slot] {
			out.Entries = append(out.Entries, e)
		}
	}
	return out
}

// WithClass picks a class ("" for any). The build becomes present.
func (b Build) WithClass(class string) Build {
	out := b.Without()
	out.Class = class
	return out
}

// IDs are the build's item ids.
func (b Build) IDs() []int32 {
	ids := make([]int32, 0, len(b.Entries))
	for _, e := range b.Entries {
		ids = append(ids, e.ItemID)
	}
	return ids
}
