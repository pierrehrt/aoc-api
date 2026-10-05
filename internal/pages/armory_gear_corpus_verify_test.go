//go:build corpus

// A corpus test: compiled only with `-tags corpus` (AOC-044) — it reads the real imported armory
// from the local database, like item_corpus_test.go.

package pages_test

// AOC-051 verify round 1: the gear builder on the real corpus, through the real router. Each build
// is drawn at random (fixed seed) — items in slots they fit and slots they do not, weapons in both
// hands, a class half the time — and three answers must agree:
//   - the page's builder (the slot rows' statuses, the counts, the summed lines);
//   - /v1/builds/compute for the same query;
//   - an independent derivation in SQL, written here from the ticket's rules and nothing in
//     internal/builds: fit is item_equip_locations; the hands are the slots a one-handed weapon fits;
//     a two_handed item that fits the hand it is in takes every other hand, except for its type's
//     other_hand_type_id; with a class, an item that lists classes and not that one is a conflict;
//     only what is left is summed — Armor, Critigation and every item_stats line per (stat, unit,
//     damage type, PvP), in exact decimals, spell effects never.
// No game fact is written here: every value comes from the database.

import (
	"context"
	"encoding/json"
	"fmt"
	"html"
	"math/big"
	"math/rand"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/pierrehrt/aoc-api/internal/assets"
	"github.com/pierrehrt/aoc-api/internal/builds"
	"github.com/pierrehrt/aoc-api/internal/db/sqlcgen"
	"github.com/pierrehrt/aoc-api/internal/httpx"
	"github.com/pierrehrt/aoc-api/internal/items"
	"github.com/pierrehrt/aoc-api/internal/pages"
	"github.com/pierrehrt/aoc-api/internal/templates"
)

// verifyGearWorld is what the independent derivation reads, straight from the tables.
type verifyGearWorld struct {
	slots      []string           // equip_locations, in id order
	hands      map[string]bool    // slots a one-handed (two_handed = false) item fits
	fits       map[int32][]string // item → the slots its equip locations list
	twoHanded  map[int32]bool
	itemType   map[int32]int32 // item → item_type_id (0 when none)
	otherHand  map[int32]int32 // item → its type's other_hand_type_id (0 when none)
	classes    map[int32][]string
	allClasses []string
	ceiling    map[string]int32 // class → its max_armour_weight's sort_order, when one is recorded
	weight     map[int32]int32  // item → its armour weight's sort_order, when it has one
	equippable []int32
	weapons    []int32 // items whose type is a weapon (two_handed known), a shield or ammunition
	bySlot     map[string][]int32
	allowing   []int32           // two-handers whose type allows a type in the other hand
	allowedIn  map[int32][]int32 // item type → its items
}

func loadVerifyGearWorld(t *testing.T, pool *pgxpool.Pool) *verifyGearWorld {
	t.Helper()
	ctx := context.Background()
	w := &verifyGearWorld{hands: map[string]bool{}, fits: map[int32][]string{}, twoHanded: map[int32]bool{},
		itemType: map[int32]int32{}, otherHand: map[int32]int32{}, classes: map[int32][]string{},
		ceiling: map[string]int32{}, weight: map[int32]int32{}, bySlot: map[string][]int32{}, allowedIn: map[int32][]int32{}}
	each := func(q string, scan func(rows interface{ Scan(...any) error }) error) {
		rows, err := pool.Query(ctx, q)
		if err != nil {
			t.Fatal(err)
		}
		defer rows.Close()
		for rows.Next() {
			if err := scan(rows); err != nil {
				t.Fatal(err)
			}
		}
		if err := rows.Err(); err != nil {
			t.Fatal(err)
		}
	}
	each(`SELECT slug FROM equip_locations ORDER BY id`, func(r interface{ Scan(...any) error }) error {
		var s string
		err := r.Scan(&s)
		w.slots = append(w.slots, s)
		return err
	})
	each(`SELECT DISTINCT el.slug FROM item_equip_locations x JOIN equip_locations el ON el.id = x.equip_location_id
	      JOIN items i ON i.item_id = x.item_id JOIN item_types t ON t.id = i.item_type_id WHERE t.two_handed = false`,
		func(r interface{ Scan(...any) error }) error {
			var s string
			err := r.Scan(&s)
			w.hands[s] = true
			return err
		})
	each(`SELECT x.item_id, el.slug FROM item_equip_locations x JOIN equip_locations el ON el.id = x.equip_location_id ORDER BY x.item_id, el.id`,
		func(r interface{ Scan(...any) error }) error {
			var id int32
			var s string
			err := r.Scan(&id, &s)
			if len(w.fits[id]) == 0 {
				w.equippable = append(w.equippable, id)
			}
			w.fits[id] = append(w.fits[id], s)
			return err
		})
	each(`SELECT i.item_id, COALESCE(i.item_type_id, 0), COALESCE(t.two_handed, false), COALESCE(t.other_hand_type_id, 0),
	             COALESCE(t.two_handed IS NOT NULL OR t.slug IN ('shield', 'ammunition'), false)
	      FROM items i LEFT JOIN item_types t ON t.id = i.item_type_id`,
		func(r interface{ Scan(...any) error }) error {
			var id, typ, other int32
			var two, weapon bool
			err := r.Scan(&id, &typ, &two, &other, &weapon)
			w.itemType[id], w.twoHanded[id], w.otherHand[id] = typ, two, other
			if weapon && len(w.fits[id]) > 0 {
				w.weapons = append(w.weapons, id)
			}
			return err
		})
	each(`SELECT ic.item_id, c.slug FROM item_classes ic JOIN classes c ON c.id = ic.class_id`, func(r interface{ Scan(...any) error }) error {
		var id int32
		var s string
		err := r.Scan(&id, &s)
		w.classes[id] = append(w.classes[id], s)
		return err
	})
	each(`SELECT slug FROM classes ORDER BY sort_order`, func(r interface{ Scan(...any) error }) error {
		var s string
		err := r.Scan(&s)
		w.allClasses = append(w.allClasses, s)
		return err
	})
	each(`SELECT c.slug, aw.sort_order FROM classes c JOIN armour_weights aw ON aw.id = c.max_armour_weight`, func(r interface{ Scan(...any) error }) error {
		var s string
		var o int32
		err := r.Scan(&s, &o)
		w.ceiling[s] = o
		return err
	})
	each(`SELECT i.item_id, aw.sort_order FROM items i JOIN armour_weights aw ON aw.id = i.armour_weight_id`, func(r interface{ Scan(...any) error }) error {
		var id, o int32
		err := r.Scan(&id, &o)
		w.weight[id] = o
		return err
	})
	for _, id := range w.equippable {
		for _, s := range w.fits[id] {
			w.bySlot[s] = append(w.bySlot[s], id)
		}
		if w.itemType[id] != 0 {
			w.allowedIn[w.itemType[id]] = append(w.allowedIn[w.itemType[id]], id)
		}
		if w.twoHanded[id] && w.otherHand[id] != 0 {
			w.allowing = append(w.allowing, id)
		}
	}
	if len(w.hands) < 2 || len(w.weapons) == 0 {
		t.Skipf("the corpus has %d hands and %d weapons — not imported?", len(w.hands), len(w.weapons))
	}
	return w
}

func (w *verifyGearWorld) fit(id int32, slot string) bool {
	for _, s := range w.fits[id] {
		if s == slot {
			return true
		}
	}
	return false
}

// status is the independent derivation of one slot's status.
func (w *verifyGearWorld) status(build map[string]int32, class, slot string) string {
	// the two-hander, in another hand it fits, that takes this hand
	var two int32
	if w.hands[slot] {
		for s, id := range build {
			if s != slot && w.hands[s] && w.twoHanded[id] && w.fit(id, s) {
				two = id
			}
		}
	}
	id, ok := build[slot]
	allowed := func(it int32) bool { return w.otherHand[two] != 0 && w.otherHand[two] == w.itemType[it] }
	switch {
	case !ok && two != 0 && w.otherHand[two] == 0:
		return builds.StatusHeld
	case !ok:
		return builds.StatusEmpty
	case !w.fit(id, slot):
		return builds.StatusWrongSlot
	case two != 0 && !allowed(id):
		return builds.StatusHeld
	}
	if class == "" {
		return builds.StatusEquipped
	}
	if len(w.classes[id]) > 0 {
		listed := false
		for _, c := range w.classes[id] {
			listed = listed || c == class
		}
		if !listed {
			return builds.StatusConflict
		}
	}
	if c, ok := w.ceiling[class]; ok {
		if o, ok := w.weight[id]; ok && o > c {
			return builds.StatusConflict
		}
	}
	return builds.StatusEquipped
}

func verifyGearRouter(t *testing.T) (http.Handler, *pgxpool.Pool) {
	t.Helper()
	_, pool := corpusRouter(t) // its database checks: local only, imported
	set, err := assets.Load()
	if err != nil {
		t.Fatal(err)
	}
	tpl, err := templates.New(set)
	if err != nil {
		t.Fatal(err)
	}
	q := sqlcgen.New(pool)
	gear := builds.NewService(q)
	h := pages.New(tpl, set, base, items.NewService(q), gear)
	return httpx.NewRouterWithAPI(httpx.Build{Version: "test", Commit: "test", Env: "test"}, h.Routes, set.Handler(),
		func(v1 chi.Router) { v1.Mount("/builds", builds.NewHandler(gear).Routes()) }), pool
}

var (
	verifyGearRowRe  = regexp.MustCompile(`<li data-gear-slot="([^"]+)" data-status="([a-z_]+)"`)
	verifyGearLineRe = regexp.MustCompile(`<li class="grid grid-cols-\[66px_1fr\][^"]*"><span[^>]*>([^<]+)</span> <span[^>]*>([^<]+)</span>`)
	verifyGearFillRe = regexp.MustCompile(`(\d+)/(\d+) slots(?: · (\d+) conflicts?)?`)
)

func TestVerifyGearBuilderPageJSONAndSQLAgreeOnTheCorpus(t *testing.T) {
	h, pool := verifyGearRouter(t)
	w := loadVerifyGearWorld(t, pool)
	ctx := context.Background()
	rng := rand.New(rand.NewSource(51))
	seen := map[string]int{}

	for n := 0; n < 160; n++ {
		// Mostly items that fit their slot (so the sum has something in it), some that do not.
		build := map[string]int32{}
		for _, i := range rng.Perm(len(w.slots))[:1+rng.Intn(12)] {
			s := w.slots[i]
			switch r := rng.Intn(10); {
			case r < 7 && len(w.bySlot[s]) > 0:
				build[s] = w.bySlot[s][rng.Intn(len(w.bySlot[s]))]
			case w.hands[s] && r < 9:
				build[s] = w.weapons[rng.Intn(len(w.weapons))]
			default:
				build[s] = w.equippable[rng.Intn(len(w.equippable))]
			}
		}
		// A quarter of the builds hold a two-hander whose type allows something in the other hand, with
		// that something (or, half the time, anything else) in the other hand — wherever the data puts them.
		if n%4 == 0 && len(w.allowing) > 0 {
			two := w.allowing[rng.Intn(len(w.allowing))]
			at := w.fits[two][0]
			for h := range w.hands {
				delete(build, h)
			}
			build[at] = two
			for _, h := range w.slots { // in slot order, so the draw is the same on every run
				if !w.hands[h] || h == at {
					continue
				}
				if ok := w.allowedIn[w.otherHand[two]]; len(ok) > 0 && rng.Intn(2) == 0 {
					build[h] = ok[rng.Intn(len(ok))]
				} else if len(w.bySlot[h]) > 0 {
					build[h] = w.bySlot[h][rng.Intn(len(w.bySlot[h]))]
				}
			}
		}
		q := url.Values{}
		for _, s := range w.slots {
			if id, ok := build[s]; ok {
				q.Add(builds.ParamGear, fmt.Sprintf("%s:%d", s, id))
			}
		}
		class := ""
		if rng.Intn(2) == 0 {
			class = w.allClasses[rng.Intn(len(w.allClasses))]
			q.Set(builds.ParamClass, class)
		}
		raw := q.Encode()

		// /v1
		rr := get(t, h, http.MethodGet, "/v1/builds/compute?"+raw, nil, "")
		if rr.Code != http.StatusOK {
			t.Fatalf("%s: /v1 %d %s", raw, rr.Code, rr.Body.String())
		}
		var res struct {
			Slots []struct {
				Slot   items.Term `json:"slot"`
				Status string     `json:"status"`
			} `json:"slots"`
			Filled, Conflicts int
			Armor             *int64           `json:"armor"`
			Critigation       *int64           `json:"critigation"`
			Stats             []items.StatLine `json:"stats"`
			Sum               string           `json:"sum"`
		}
		if err := json.Unmarshal(rr.Body.Bytes(), &res); err != nil {
			t.Fatal(err)
		}
		// the page
		page := get(t, h, http.MethodGet, "/armory?"+raw, nil, "")
		if page.Code != http.StatusOK {
			t.Fatalf("%s: page %d", raw, page.Code)
		}
		body := page.Body.String()

		// 1. statuses: page = /v1 = the independent rules
		rows := verifyGearRowRe.FindAllStringSubmatch(body, -1)
		if len(rows) != len(w.slots) || len(res.Slots) != len(w.slots) {
			t.Fatalf("%s: %d page rows, %d /v1 slots, %d equip_locations", raw, len(rows), len(res.Slots), len(w.slots))
		}
		equipped := map[string]int32{}
		conflicts := 0
		for i, s := range w.slots {
			want := w.status(build, class, s)
			if rows[i][1] != s || res.Slots[i].Slot.Slug != s {
				t.Fatalf("%s: row %d is %s on the page and %s on /v1, want %s", raw, i, rows[i][1], res.Slots[i].Slot.Slug, s)
			}
			if rows[i][2] != want || res.Slots[i].Status != want {
				t.Errorf("%s: %s is %q on the page and %q on /v1; the rules say %q", raw, s, rows[i][2], res.Slots[i].Status, want)
			}
			seen[want]++
			if want == builds.StatusEquipped {
				equipped[s] = build[s]
			}
			if want == builds.StatusConflict {
				conflicts++
			}
		}
		m := verifyGearFillRe.FindStringSubmatch(body)
		if m == nil || m[1] != fmt.Sprint(len(build)) || res.Filled != len(build) || res.Conflicts != conflicts || (conflicts > 0 && m[3] != fmt.Sprint(conflicts)) {
			t.Errorf("%s: counts page %v, /v1 filled %d conflicts %d; want filled %d conflicts %d", raw, m, res.Filled, res.Conflicts, len(build), conflicts)
		}
		if res.Sum != builds.SumNote || !strings.Contains(body, builds.SumNote) {
			t.Errorf("%s: the sum is not labelled a raw sum", raw)
		}

		// 2. the sum: an independent SQL sum over the slots the rules leave equipped
		var vals []string
		for s, id := range equipped {
			pos := 0
			for i, x := range w.slots {
				if x == s {
					pos = i
				}
			}
			vals = append(vals, fmt.Sprintf("(%d::bigint, %d)", pos, id))
		}
		var wantLines []string
		var armor, crit *int64
		if len(vals) > 0 {
			v := strings.Join(vals, ",")
			if err := pool.QueryRow(ctx, `SELECT sum(i.armor)::bigint, sum(i.critigation)::bigint FROM (VALUES `+v+`) b(pos, item_id) JOIN items i ON i.item_id = b.item_id`).Scan(&armor, &crit); err != nil {
				t.Fatal(err)
			}
			sums, err := pool.Query(ctx, `SELECT st.stat, st.unit, COALESCE(st.damage_type, ''), (sum(st.sign * st.value))::text
				FROM (VALUES `+v+`) b(pos, item_id) JOIN item_stats st ON st.item_id = b.item_id
				GROUP BY st.stat, st.unit, st.damage_type, st.pvp ORDER BY min(b.pos * 1000000000000 + st.id)`)
			if err != nil {
				t.Fatal(err)
			}
			for sums.Next() {
				var stat, unit, dt, total string
				if err := sums.Scan(&stat, &unit, &dt, &total); err != nil {
					t.Fatal(err)
				}
				r, ok := new(big.Rat).SetString(total)
				if !ok {
					t.Fatalf("not a number: %q", total)
				}
				label := stat
				if dt != "" {
					label += " (" + dt + ")"
				}
				wantLines = append(wantLines, r.FloatString(2)+" "+unit+" "+label)
			}
			sums.Close()
		}
		if (armor == nil) != (res.Armor == nil) || (armor != nil && *armor != *res.Armor) || (crit == nil) != (res.Critigation == nil) || (crit != nil && *crit != *res.Critigation) {
			t.Errorf("%s: /v1 armor %v critigation %v, SQL %v %v", raw, res.Armor, res.Critigation, armor, crit)
		}
		var gotLines []string
		for _, s := range res.Stats {
			r, _ := new(big.Rat).SetString(s.Value)
			r.Mul(r, big.NewRat(int64(s.Sign), 1))
			label := s.Stat
			if s.DamageType != nil && *s.DamageType != "" {
				label += " (" + *s.DamageType + ")"
			}
			gotLines = append(gotLines, r.FloatString(2)+" "+s.Unit+" "+label)
		}
		if strings.Join(gotLines, " | ") != strings.Join(wantLines, " | ") {
			t.Errorf("%s:\n /v1 %v\n SQL %v", raw, gotLines, wantLines)
		}

		// 3. the page prints exactly /v1's lines: the base lines, then each stat
		var pageLines, apiLines []string
		for _, l := range verifyGearLineRe.FindAllStringSubmatch(body, -1) {
			pageLines = append(pageLines, html.UnescapeString(l[1]+" "+l[2]))
		}
		if res.Armor != nil {
			apiLines = append(apiLines, templates.Num(*res.Armor)+" Armor")
		}
		if res.Critigation != nil {
			apiLines = append(apiLines, templates.Num(*res.Critigation)+" Critigation Amount")
		}
		for _, s := range res.Stats {
			apiLines = append(apiLines, templates.GearValue(s)+" "+templates.GearLabel(s))
		}
		if strings.Join(pageLines, " | ") != strings.Join(apiLines, " | ") {
			t.Errorf("%s:\n page %v\n /v1  %v", raw, pageLines, apiLines)
		}
		// DPS is a weapon's rate and is never a line of the sum
		for _, l := range pageLines {
			if strings.Contains(l, "DPS") {
				t.Errorf("%s: a DPS line in the sum: %q", raw, l)
			}
		}
	}
	// The draw must reach every status, or the test proves less than it says.
	for _, s := range []string{builds.StatusEmpty, builds.StatusEquipped, builds.StatusWrongSlot, builds.StatusHeld, builds.StatusConflict} {
		if seen[s] == 0 {
			t.Errorf("no slot was %q in the draw: %v", s, seen)
		}
	}
}

// A state with no build answers as it did before the builder: an empty gear_class (which the class
// picker sends with every plain form submit) names no build, so the canonical and the htmx history
// header are the list's own.
func TestVerifyGearEmptyClassIsNoBuildOnTheCorpus(t *testing.T) {
	h, _ := verifyGearRouter(t)
	for _, c := range []struct{ with, without string }{
		{"/armory?gear_class=", "/armory"},
		{"/armory?q=a&rarity=epic&gear_class=", "/armory?q=a&rarity=epic"},
	} {
		a := get(t, h, http.MethodGet, c.with, nil, "")
		b := get(t, h, http.MethodGet, c.without, nil, "")
		canon := regexp.MustCompile(`<link rel="canonical" href="([^"]+)"`)
		if canon.FindString(a.Body.String()) != canon.FindString(b.Body.String()) {
			t.Errorf("%s: canonical %q, want %q", c.with, canon.FindString(a.Body.String()), canon.FindString(b.Body.String()))
		}
		if strings.Contains(a.Body.String(), `id="gear-open" class="sr-only max-lg:hidden" aria-label="Open the gear builder" checked`) {
			t.Errorf("%s: the builder opens on an empty class", c.with)
		}
		hx := map[string]string{"HX-Request": "true", "HX-Current-URL": "http://x" + c.without}
		ha := get(t, h, http.MethodGet, c.with, hx, "")
		hb := get(t, h, http.MethodGet, c.without, hx, "")
		for _, k := range []string{"HX-Push-Url", "HX-Replace-Url", "Cache-Control"} {
			if ha.Header().Get(k) != hb.Header().Get(k) {
				t.Errorf("%s: %s %q, want %q", c.with, k, ha.Header().Get(k), hb.Header().Get(k))
			}
		}
	}
}
