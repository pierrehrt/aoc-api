package builds_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/pierrehrt/aoc-api/internal/builds"
	"github.com/pierrehrt/aoc-api/internal/httpx"
)

func compute(t *testing.T, f *fakeDB, raw string) builds.Result {
	t.Helper()
	res, err := builds.NewService(f).Compute(context.Background(), parse(t, raw))
	if err != nil {
		t.Fatalf("Compute(%q): %v", raw, err)
	}
	return res
}

func status(res builds.Result) map[string]string {
	m := map[string]string{}
	for _, s := range res.Slots {
		m[s.Slot.Slug] = s.Status
	}
	return m
}

func TestTheRowsAreEverySlotInOrder(t *testing.T) {
	res := compute(t, newFake(), "gear=")
	var got []string
	for _, s := range res.Slots {
		got = append(got, s.Slot.Slug)
		if s.Status != builds.StatusEmpty {
			t.Errorf("%s: %s, want empty", s.Slot.Slug, s.Status)
		}
	}
	if strings.Join(got, " ") != "test-head test-ring-left test-ring-right test-main test-off" {
		t.Errorf("rows %v", got)
	}
	if len(res.Stats) != 0 || res.Armor != nil || res.Filled != 0 || res.Sum != builds.SumNote {
		t.Errorf("an empty build sums nothing: %+v", res)
	}
}

// ⭐ Exact decimals: 10.00 + 2.50 = 12.50 and -3.00 + 3.00 = 0, never 12.499999.
func TestTheSumIsExactAndKeptPerStat(t *testing.T) {
	res := compute(t, newFake(), "gear=test-head:1&gear=test-ring-left:2&gear=test-main:3")
	want := []struct {
		stat, value string
		sign        int16
	}{{"Test Might", "12.50", 1}, {"Test Grace", "0.00", 0}}
	if len(res.Stats) != len(want) {
		t.Fatalf("stats %+v", res.Stats)
	}
	for i, w := range want {
		if s := res.Stats[i]; s.Stat != w.stat || s.Value != w.value || s.Sign != w.sign {
			t.Errorf("line %d = %+v, want %+v", i, s, w)
		}
	}
}

func TestTheSumKeepsDamageTypePvPAndUnitApart(t *testing.T) {
	f := newFake()
	fire, pvp, pct := newFake().items[1].stats[0], newFake().items[1].stats[0], newFake().items[1].stats[0]
	fire.DamageType, pvp.Pvp, pct.Unit = str("Test Fire"), true, "percent"
	it := f.items[2]
	it.stats = append(it.stats, fire, pvp, pct)
	for i := range it.stats {
		it.stats[i].ItemID = 2
	}
	f.items[2] = it
	res := compute(t, f, "gear=test-head:1&gear=test-ring-left:2")
	if len(res.Stats) != 5 {
		t.Fatalf("want Might, Grace, Might (Test Fire), PvP Might, Might %%: %+v", res.Stats)
	}
	if res.Stats[0].Value != "12.50" {
		t.Errorf("the plain line sums only plain lines: %+v", res.Stats[0])
	}
}

func TestStatusesFollowTheRules(t *testing.T) {
	f := newFake()
	res := compute(t, f, "gear=test-head:999&gear=test-ring-left:1&gear=test-main:4&gear=test-off:7")
	got := status(res)
	for slot, want := range map[string]string{
		"test-head":      builds.StatusUnknown,   // no item 999
		"test-ring-left": builds.StatusWrongSlot, // a helm in a ring slot
		"test-main":      builds.StatusEquipped,  // the greatblade
		"test-off":       builds.StatusHeld,      // the shield, beside a two-hander
	} {
		if got[slot] != want {
			t.Errorf("%s = %s, want %s", slot, got[slot], want)
		}
	}
	if res.Filled != 4 {
		t.Errorf("filled %d, want 4 (every slot the URL names)", res.Filled)
	}
	off := res.Slots[4]
	if off.HeldBy == nil || off.HeldBy.ID != 4 {
		t.Errorf("the held hand names its two-hander: %+v", off)
	}
}

func TestAnEmptyHandBesideATwoHanderIsHeld(t *testing.T) {
	res := compute(t, newFake(), "gear=test-main:4")
	if s := res.Slots[4]; s.Status != builds.StatusHeld || s.HeldBy == nil {
		t.Errorf("off hand %+v, want held", s)
	}
	if s := res.Slots[0]; s.Status != builds.StatusEmpty {
		t.Errorf("a slot that is not a hand is never held: %+v", s)
	}
}

func TestABowStillTakesWhatItAllowsInTheOtherHand(t *testing.T) {
	res := compute(t, newFake(), "gear=test-main:5&gear=test-off:6")
	if got := status(res)["test-off"]; got != builds.StatusEquipped {
		t.Errorf("arrows beside the bow: %s, want equipped", got)
	}
	res = compute(t, newFake(), "gear=test-main:5&gear=test-off:7")
	if got := status(res)["test-off"]; got != builds.StatusHeld {
		t.Errorf("a shield beside the bow: %s, want held", got)
	}
}

func TestAClassMarksWhatItCannotWearAndLeavesItOutOfTheSum(t *testing.T) {
	res := compute(t, newFake(), "gear=test-head:8&gear=test-ring-left:2&gear_class=test-class-a")
	if got := status(res)["test-head"]; got != builds.StatusConflict || res.Conflicts != 1 {
		t.Errorf("a robe only class B wears, with class A: %s (%d conflicts)", got, res.Conflicts)
	}
	if res.Class == nil || res.Class.Slug != "test-class-a" {
		t.Errorf("class %+v", res.Class)
	}
	if len(res.Stats) != 1 || res.Stats[0].Value != "2.50" {
		t.Errorf("only the ring is summed: %+v", res.Stats)
	}
	if !res.Wears(nil, "") || res.Wears([]string{"test-class-b"}, "") || !res.Wears([]string{"test-class-a"}, "") {
		t.Error("Wears must read a row's classes the way a slot's conflict does")
	}
}

func TestAClassCeilingMarksHeavierArmour(t *testing.T) {
	res := compute(t, newFake(), "gear=test-head:10&gear_class=test-class-c")
	if got := status(res)["test-head"]; got != builds.StatusConflict {
		t.Errorf("heavy armour under a light ceiling: %s, want conflict", got)
	}
	if res.Wears(nil, "test-heavy") || !res.Wears(nil, "test-light") || !res.Wears(nil, "") {
		t.Error("Wears must apply the ceiling to a row's weight, and none to a row with no weight")
	}
	if res := compute(t, newFake(), "gear=test-head:10&gear_class=test-class-a"); status(res)["test-head"] != builds.StatusEquipped {
		t.Error("no ceiling recorded: any weight is worn")
	}
}

func TestTheBuildComesBackInTheSlotsOrder(t *testing.T) {
	res := compute(t, newFake(), "gear=test-main:3&gear=test-head:1")
	if got := res.Build.Values().Encode(); got != "gear=test-head%3A1&gear=test-main%3A3" {
		t.Errorf("canonical %q", got)
	}
}

func TestUnknownSlotsAndClassesAreA400(t *testing.T) {
	svc := builds.NewService(newFake())
	for _, raw := range []string{"gear=test-elbow:1", "gear_class=test-class-z"} {
		if _, err := svc.Compute(context.Background(), parse(t, raw)); !errors.Is(err, httpx.ErrInvalid) {
			t.Errorf("%s: %v, want ErrInvalid", raw, err)
		}
	}
}

// ⭐ Never a query per item: a full build costs what an empty one does, plus the item reads once.
func TestComputeReadsAFixedNumberOfTimes(t *testing.T) {
	f := newFake()
	compute(t, f, "gear=test-head:1&gear=test-ring-left:2&gear=test-ring-right:2&gear=test-main:5&gear=test-off:6")
	for k, n := range f.calls {
		if n != 1 {
			t.Errorf("%s read %d times, want once", k, n)
		}
	}
}

func TestAnEmptyBuildDoesNotDeriveTheHands(t *testing.T) {
	f := newFake()
	compute(t, f, "q=x")
	if f.calls["hands"] != 0 || f.calls["items"] != 0 {
		t.Errorf("an Armory request with no build read hands %d and items %d times, want 0", f.calls["hands"], f.calls["items"])
	}
}

func add(t *testing.T, f *fakeDB, raw string, a builds.Add) (string, string) {
	t.Helper()
	b, refusal, err := builds.NewService(f).Add(context.Background(), parse(t, raw), a)
	if err != nil {
		t.Fatalf("Add: %v", err)
	}
	return b.Values().Encode(), refusal
}

func TestAddPutsAnItemInItsFirstFreeSlot(t *testing.T) {
	f := newFake()
	if got, r := add(t, f, "", builds.Add{ItemID: 2}); got != "gear=test-ring-left%3A2" || r != "" {
		t.Errorf("first ring: %q %q", got, r)
	}
	if got, _ := add(t, f, "gear=test-ring-left:2", builds.Add{ItemID: 2}); got != "gear=test-ring-left%3A2&gear=test-ring-right%3A2" {
		t.Errorf("second ring: %q", got)
	}
	if got, _ := add(t, f, "gear=test-ring-left:2&gear=test-ring-right:2&gear_class=test-class-a", builds.Add{ItemID: 2}); got != "gear=test-ring-left%3A2&gear=test-ring-right%3A2&gear_class=test-class-a" {
		t.Errorf("both full: the first is replaced, the class kept: %q", got)
	}
}

func TestADropNamesItsSlot(t *testing.T) {
	f := newFake()
	if got, r := add(t, f, "", builds.Add{Slot: "test-ring-right", ItemID: 2}); got != "gear=test-ring-right%3A2" || r != "" {
		t.Errorf("drop on the right: %q %q", got, r)
	}
	if got, r := add(t, f, "gear=test-main:3", builds.Add{Slot: "test-head", ItemID: 2}); got != "gear=test-main%3A3" || !strings.Contains(r, "does not go in the Test head slot") {
		t.Errorf("a ring on the head: %q %q — want refused, build unchanged", got, r)
	}
}

func TestATwoHanderEmptiesTheOtherHandButForWhatItAllows(t *testing.T) {
	f := newFake()
	if got, _ := add(t, f, "gear=test-off:7", builds.Add{ItemID: 4}); got != "gear=test-main%3A4" {
		t.Errorf("greatblade over a shield: %q", got)
	}
	if got, _ := add(t, f, "gear=test-off:6", builds.Add{ItemID: 5}); got != "gear=test-main%3A5&gear=test-off%3A6" {
		t.Errorf("a bow keeps its arrows: %q", got)
	}
	if got, _ := add(t, f, "gear=test-main:5", builds.Add{ItemID: 6}); got != "gear=test-main%3A5&gear=test-off%3A6" {
		t.Errorf("arrows beside a bow: %q", got)
	}
}

func TestAHandTakenByATwoHanderRefusesTheRest(t *testing.T) {
	f := newFake()
	got, r := add(t, f, "gear=test-main:4", builds.Add{ItemID: 7})
	if got != "gear=test-main%3A4" || !strings.Contains(r, "Test Greatblade needs both hands") {
		t.Errorf("a shield beside a greatblade: %q %q", got, r)
	}
	if _, r := add(t, f, "gear=test-main:4", builds.Add{Slot: "test-off", ItemID: 3}); r == "" {
		t.Error("a blade dropped on the held hand must be refused")
	}
	// With no slot named, a one-hander replaces the two-hander rather than being refused.
	if got, r := add(t, f, "gear=test-main:4", builds.Add{ItemID: 3}); got != "gear=test-main%3A3" || r != "" {
		t.Errorf("a blade with a greatblade in hand: %q %q", got, r)
	}
}

func TestAddRefusesWithAReason(t *testing.T) {
	f := newFake()
	for _, c := range []struct {
		raw  string
		a    builds.Add
		want string
	}{
		{"", builds.Add{ItemID: 999}, "There is no item 999"},
		{"", builds.Add{ItemID: 9}, "goes in no equipment slot"},
		{"gear_class=test-class-a", builds.Add{ItemID: 8}, "A Test Class A cannot wear Test Robe"},
	} {
		if _, r := add(t, f, c.raw, c.a); !strings.Contains(r, c.want) {
			t.Errorf("%+v: refusal %q, want it to say %q", c.a, r, c.want)
		}
	}
	if _, _, err := builds.NewService(f).Add(context.Background(), builds.Build{}, builds.Add{Slot: "test-elbow", ItemID: 1}); !errors.Is(err, httpx.ErrInvalid) {
		t.Errorf("an unknown slot is a 400: %v", err)
	}
}
