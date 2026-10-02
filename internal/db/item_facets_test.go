package db_test

// AOC-049 — the rail's counts, against a real migrated database.
//
// ⭐ What is pinned here is the PROMISE a count makes: the number beside a value is the total the
// list reports when that value is picked, with every other filter left as it is. That is checked
// through items.Service — Filters → arguments → SQL — for every value of every facet, so a count
// computed from a different filter set than the rows (the bug the shared CTE exists to prevent)
// fails here, in CI, on fake data. The corpus file runs the same check on the real 4,646 items.
//
// ⚠️ EVERY FIXTURE NAME IS OBVIOUSLY FAKE ("Test Facet Alpha"), and the taxonomy rows are picked by
// position, not by name: nothing here states a fact about the game (CLAUDE.md STEP ZERO).

import (
	"context"
	"database/sql"
	"os"
	"regexp"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/pierrehrt/aoc-api/internal/db/sqlcgen"
	"github.com/pierrehrt/aoc-api/internal/items"
)

// ⛔ The filter rules are written once: every query that filters the list carries the same
// `filtered` CTE, byte for byte. sqlc cannot share a fragment between queries, so this is the
// thing that makes three copies one definition. Only the CTE's header may differ (MATERIALIZED
// in the facet queries, which read it many times; inlined in the list, so its filters push down).
func TestTheFilterCTEIsOneDefinition(t *testing.T) {
	raw, err := os.ReadFile("queries/items.sql")
	if err != nil {
		t.Fatal(err)
	}
	src := string(raw)
	head := regexp.MustCompile(`(?m)^WITH filtered AS (MATERIALIZED )?\($`)
	locs := head.FindAllStringIndex(src, -1)

	bodies := map[string]string{} // query name -> CTE body
	for _, l := range locs {
		rest := src[l[1]:]
		end := strings.Index(rest, "\n)\n")
		if end < 0 {
			t.Fatalf("a filtered CTE at byte %d never closes with a line holding only ')'", l[0])
		}
		name := regexp.MustCompile(`-- name: (\w+)`).FindAllStringSubmatch(src[:l[0]], -1)
		bodies[name[len(name)-1][1]] = rest[:end]
	}
	for _, q := range []string{"ListItems", "CountItemFacets", "ItemFacetTotals"} {
		if _, ok := bodies[q]; !ok {
			t.Errorf("%s does not start with the filtered CTE", q)
		}
	}
	want := bodies["ListItems"]
	for q, b := range bodies {
		if b != want {
			t.Errorf("%s's filtered CTE differs from ListItems' — the filter rules now have two definitions", q)
		}
	}
	if len(locs) != len(bodies) {
		t.Errorf("%d filtered CTEs in %d queries — one query carries two", len(locs), len(bodies))
	}
}

// facetSlug picks taxonomy rows by POSITION, so the fixture names no real class or currency.
func facetSlug(t *testing.T, d *sql.DB, table, order string, offset int) string {
	t.Helper()
	var s string
	// #nosec G202 -- table and order are literals from this file.
	if err := d.QueryRowContext(context.Background(), "SELECT slug FROM "+table+" ORDER BY "+order+" LIMIT 1 OFFSET $1", offset).Scan(&s); err != nil {
		t.Fatalf("%s #%d: %v", table, offset, err)
	}
	return s
}

// seedFacetFixture is a small fake armory with every facet in play: two rarities shared, items with
// and without weight, class, set, level and price, an item priced at one source and free at another,
// an item bought with one currency from two sources (counted once), and an item with no sources.
func seedFacetFixture(t *testing.T, d *sql.DB) {
	t.Helper()
	ctx := context.Background()
	exec := func(q string, args ...any) {
		t.Helper()
		if _, err := d.ExecContext(ctx, q, args...); err != nil {
			t.Fatalf("%v\n%s", err, q)
		}
	}
	const note = "fixture — internal/db/item_facets_test.go, not a game fact"
	for i, name := range []string{"Test Facet Set Alpha", "Test Facet Set Beta", "Test Facet Set Gamma"} {
		exec(`INSERT INTO sets (id, slug, name, confidence_id, source_note)
		      VALUES ($1, $2, $3, (SELECT id FROM confidence_levels WHERE slug = 'unconfirmed'), $4)`,
			9100+i, strings.ToLower(strings.ReplaceAll(name, " ", "-")), name, note)
	}
	type it struct {
		id             int
		name           string
		rarity         int // offset in rarities by sort_order
		weight         any // offset in armour_weights, or nil
		set            any // sets.id or nil
		ilvl, req      any
		pvp            bool
		slots, classes []int   // offsets by id
		sources        [][]int // per source, the currency offsets it costs (none = free)
	}
	items := []it{
		{id: 950, name: "Test Facet Alpha", rarity: 0, weight: 0, set: 9100, ilvl: 80, req: 80, slots: []int{0}, classes: []int{0, 1},
			sources: [][]int{{0}, {}}}, // priced at one source, free at the other
		{id: 951, name: "Test Facet Beta", rarity: 5, weight: 1, set: 9100, ilvl: 70, req: 65, slots: []int{9, 10}, classes: []int{1},
			sources: [][]int{{1}}},
		{id: 952, name: "Test Facet Gamma", rarity: 5}, // nothing but a rarity: no level, no source
		{id: 953, name: "Test Facet Delta", rarity: 3, weight: 0, set: 9101, ilvl: 50, slots: []int{10}, classes: []int{2},
			sources: [][]int{{0, 1}}}, // two currencies at one source
		{id: 954, name: "Test Facet Epsilon", rarity: 3, weight: 4, ilvl: 90, req: 80, slots: []int{0},
			sources: [][]int{{0}, {0}}}, // one currency from two sources: one item
		{id: 955, name: "Test Facet Zeta", rarity: 0, ilvl: 10, req: 10, pvp: true, slots: []int{11, 12}, classes: []int{2},
			sources: [][]int{{1}}},
		// No level at all, and a value of every facet no other item has: picking any of them leaves
		// only level-less items, so both spans must vanish — the case that tells a span computed
		// under the other filters from one that ignores one of them (AOC-049 mutant sweep: dropping
		// any flag from ilvl_n survived until this item existed).
		{id: 956, name: "Test Facet Eta", rarity: 1, weight: 2, set: 9102, slots: []int{3}, classes: []int{3},
			sources: [][]int{{2}}},
		// A required level and no item level — and Delta has the reverse — so each range can leave
		// only items the OTHER span knows nothing about.
		{id: 957, name: "Test Facet Theta", rarity: 0, set: 9101, req: 33, sources: [][]int{{1}}},
		// Unpriced, with low levels: "no price" then moves both ends of both spans (the second sweep's
		// survivors — a span's min or max that ignored the price filter matched every combination).
		{id: 958, name: "Test Facet Iota", rarity: 0, ilvl: 20, req: 20},
	}
	for _, x := range items {
		exec(`INSERT INTO items (item_id, slug, name, rarity_id, armour_weight_id, set_id, item_level, requires_level,
		                         pvp_source, confidence_id, source_note)
		      VALUES ($1, $2, $3,
		              (SELECT id FROM rarities ORDER BY sort_order LIMIT 1 OFFSET $4),
		              -- ⚠️ CASE, not a bare subquery: OFFSET NULL is OFFSET 0 in Postgres, so a weightless
		              -- fixture silently got the first weight (found by the AOC-049 mutant sweep).
		              CASE WHEN $5::int IS NULL THEN NULL
		                   ELSE (SELECT id FROM armour_weights ORDER BY sort_order LIMIT 1 OFFSET $5::int) END,
		              $6, $7, $8, $9,
		              (SELECT id FROM confidence_levels WHERE slug = 'unconfirmed'), $10)`,
			x.id, strings.ToLower(strings.ReplaceAll(x.name, " ", "-")), x.name, x.rarity, x.weight, x.set, x.ilvl, x.req, x.pvp, note)
		for _, s := range x.slots {
			exec(`INSERT INTO item_equip_locations (item_id, equip_location_id)
			      VALUES ($1, (SELECT id FROM equip_locations ORDER BY id LIMIT 1 OFFSET $2))`, x.id, s)
		}
		for _, c := range x.classes {
			exec(`INSERT INTO item_classes (item_id, class_id)
			      VALUES ($1, (SELECT id FROM classes ORDER BY id LIMIT 1 OFFSET $2))`, x.id, c)
		}
		for si, costs := range x.sources {
			var srcID int64
			if err := d.QueryRowContext(ctx, `INSERT INTO item_sources (item_id, confidence_id, source_note)
			      VALUES ($1, (SELECT id FROM confidence_levels WHERE slug = 'unconfirmed'), $2) RETURNING id`,
				x.id, note).Scan(&srcID); err != nil {
				t.Fatalf("source %d of %s: %v", si, x.name, err)
			}
			for _, k := range costs {
				exec(`INSERT INTO item_costs (item_source_id, currency_id, amount)
				      VALUES ($1, (SELECT id FROM currencies ORDER BY id LIMIT 1 OFFSET $2), 3)`, srcID, k)
			}
		}
	}
}

func facetService(t *testing.T) (*items.Service, *sql.DB) {
	t.Helper()
	d, url := migratedDB(t)
	seedFacetFixture(t, d)
	pool, err := pgxpool.New(context.Background(), url)
	if err != nil {
		t.Fatalf("pool: %v", err)
	}
	t.Cleanup(pool.Close)
	return items.NewService(sqlcgen.New(pool)), d
}

// withFacet returns f with the facet named by its parameter set to just slug ("" clears it).
func withFacet(t *testing.T, f items.Filters, param, slug string) items.Filters {
	t.Helper()
	var v []string
	if slug != "" {
		v = []string{slug}
	}
	switch param {
	case "rarity":
		f.Rarities = v
	case "equip_location":
		f.EquipLocations = v
	case "armour_weight":
		f.ArmourWeights = v
	case "class":
		f.Classes = v
	case "currency":
		f.Currencies = v
	case "set":
		f.Sets = v
	default:
		t.Fatalf("no filter for facet %q", param)
	}
	return f
}

func total(t *testing.T, s *items.Service, f items.Filters) int64 {
	t.Helper()
	f.WithFacets, f.Limit, f.Offset = false, 1, 0
	res, err := s.List(context.Background(), f)
	if err != nil {
		t.Fatalf("List(%+v): %v", f, err)
	}
	return res.Total
}

func i32(n int32) *int32 { return &n }

// assertFacetsAreTheirRows checks every number the rail shows for base against the list's own
// total for the state that number promises. It returns how many numbers it checked.
func assertFacetsAreTheirRows(t *testing.T, s *items.Service, base items.Filters, label string) int {
	t.Helper()
	ctx := context.Background()
	f := base
	f.WithFacets, f.Limit = true, 1
	res, err := s.List(ctx, f)
	if err != nil {
		t.Fatalf("%s: List: %v", label, err)
	}
	fc := res.Facets
	if fc == nil {
		t.Fatalf("%s: no facets", label)
	}
	checked := 0
	for _, g := range []struct {
		param string
		group items.FacetGroup
	}{{"rarity", fc.Rarity}, {"equip_location", fc.EquipLocation}, {"armour_weight", fc.ArmourWeight},
		{"class", fc.Class}, {"currency", fc.Currency}, {"set", fc.Set}} {
		if got := total(t, s, withFacet(t, base, g.param, "")); got != g.group.Any {
			t.Errorf("%s: %s Any says %d, the list with %s unset has %d", label, g.param, g.group.Any, g.param, got)
		}
		checked++
		for _, v := range g.group.Values {
			if got := total(t, s, withFacet(t, base, g.param, v.Slug)); got != v.Count {
				t.Errorf("%s: %s=%s says %d, the list has %d", label, g.param, v.Slug, v.Count, got)
			}
			checked++
		}
	}

	nb := base
	nb.Price = nil
	yes, no := true, false
	for _, c := range []struct {
		price *bool
		want  int64
		what  string
	}{{nil, fc.Price.Any, "Any"}, {&yes, fc.Price.Count, "true"}, {&no, fc.Price.Any - fc.Price.Count, "false"}} {
		pf := nb
		pf.Price = c.price
		if got := total(t, s, pf); got != c.want {
			t.Errorf("%s: price %s says %d, the list has %d", label, c.what, c.want, got)
		}
		checked++
	}

	// A span is right when its ends are reached and nothing lies beyond them.
	for _, r := range []struct {
		name string
		span *items.LevelSpan
		set  func(f *items.Filters, lo, hi *int32)
	}{
		{"ilvl", fc.ItemLevel, func(f *items.Filters, lo, hi *int32) { f.ILvlMin, f.ILvlMax = lo, hi }},
		{"reqlvl", fc.RequiresLevel, func(f *items.Filters, lo, hi *int32) { f.ReqLvlMin, f.ReqLvlMax = lo, hi }},
	} {
		open := base
		r.set(&open, nil, nil)
		withLevel := open
		r.set(&withLevel, i32(0), nil)
		n := total(t, s, withLevel)
		if r.span == nil {
			if n != 0 {
				t.Errorf("%s: no %s span, yet %d items under the other filters have one", label, r.name, n)
			}
			checked++
			continue
		}
		inside, below, above, atLo, atHi := open, open, open, open, open
		r.set(&inside, i32(r.span.Min), i32(r.span.Max))
		r.set(&below, i32(0), i32(r.span.Min-1))
		r.set(&above, i32(r.span.Max+1), nil)
		r.set(&atLo, i32(r.span.Min), i32(r.span.Min))
		r.set(&atHi, i32(r.span.Max), i32(r.span.Max))
		if got := total(t, s, inside); got != n {
			t.Errorf("%s: %s span %d–%d holds %d of the %d items with a level", label, r.name, r.span.Min, r.span.Max, got, n)
		}
		if r.span.Min > 0 && total(t, s, below) != 0 {
			t.Errorf("%s: an item sits below the %s span's minimum %d", label, r.name, r.span.Min)
		}
		if total(t, s, above) != 0 {
			t.Errorf("%s: an item sits above the %s span's maximum %d", label, r.name, r.span.Max)
		}
		if total(t, s, atLo) == 0 || total(t, s, atHi) == 0 {
			t.Errorf("%s: no item at one end of the %s span %d–%d", label, r.name, r.span.Min, r.span.Max)
		}
		checked++
	}
	return checked
}

// ⭐ THE CRITERION: every count is the list it promises, under several combinations of other filters.
//
// One combination per filter, each of which excludes some fixture items, so a facet whose condition
// forgets ANY other filter's flag counts an item the list does not show (a count that ignored the
// price filter survived the first version of this test, which had no combination where price
// changed a rarity count).
func TestFacetCountsAreTheRowsTheyPromise(t *testing.T) {
	s, d := facetService(t)
	topRarity := facetSlug(t, d, "rarities", "sort_order DESC", 0)
	firstSlot := facetSlug(t, d, "equip_locations", "id", 0)
	firstWeight := facetSlug(t, d, "armour_weights", "sort_order", 0)
	secondClass := facetSlug(t, d, "classes", "id", 1)
	firstCurrency := facetSlug(t, d, "currencies", "id", 0)
	yes, no := true, false
	// What only Eta has (see the fixture).
	etaRarity := facetSlug(t, d, "rarities", "sort_order", 1)
	etaSlot := facetSlug(t, d, "equip_locations", "id", 3)
	etaWeight := facetSlug(t, d, "armour_weights", "sort_order", 2)
	etaClass := facetSlug(t, d, "classes", "id", 3)
	etaCurrency := facetSlug(t, d, "currencies", "id", 2)

	for label, f := range map[string]items.Filters{
		"no filters":              {},
		"one rarity":              {Rarities: []string{topRarity}},
		"one slot":                {EquipLocations: []string{firstSlot}},
		"one armour weight":       {ArmourWeights: []string{firstWeight}},
		"one class":               {Classes: []string{secondClass}},
		"item level from 60":      {ILvlMin: i32(60)},
		"required level up to 70": {ReqLvlMax: i32(70)},
		"has a price":             {Price: &yes},
		"has no price":            {Price: &no},
		"one currency":            {Currencies: []string{firstCurrency}},
		"one set":                 {Sets: []string{"test-facet-set-alpha"}},
		"a name search":           {Query: "Facet A"},
		"pvp":                     {PvP: &yes},
		"class and price":         {Classes: []string{secondClass}, Price: &yes},
		"set and currency":        {Sets: []string{"test-facet-set-alpha"}, Currencies: []string{firstCurrency}},
		// Each leaves only items with no level of the span's kind: the spans must vanish.
		"only Eta's rarity":       {Rarities: []string{etaRarity}},
		"only Eta's slot":         {EquipLocations: []string{etaSlot}},
		"only Eta's weight":       {ArmourWeights: []string{etaWeight}},
		"only Eta's class":        {Classes: []string{etaClass}},
		"only Eta's currency":     {Currencies: []string{etaCurrency}},
		"only Eta's set":          {Sets: []string{"test-facet-set-gamma"}},
		"only Eta's name":         {Query: "Facet Eta"},
		"only Theta's req level":  {ReqLvlMin: i32(30), ReqLvlMax: i32(40)},
		"only Delta's item level": {ILvlMin: i32(45), ILvlMax: i32(55)},
		// Ranges that move the OTHER span's ends, and a pair that leaves only a level-less item.
		"required level from 60":      {ReqLvlMin: i32(60)},
		"required level up to 30":     {ReqLvlMax: i32(30)},
		"item level up to 30":         {ILvlMax: i32(30)},
		"no price, the top rarity":    {Price: &no, Rarities: []string{topRarity}},
		"a pvp item with a level cap": {PvP: &yes, ILvlMax: i32(50)},
		// AOC-064: several values in a group — any of them — under the other groups.
		"two rarities":                {Rarities: []string{topRarity, etaRarity}},
		"two slots and a class":       {EquipLocations: []string{firstSlot, etaSlot}, Classes: []string{secondClass}},
		"two weights, two currencies": {ArmourWeights: []string{firstWeight, etaWeight}, Currencies: []string{firstCurrency, etaCurrency}},
		"two sets and a rarity":       {Sets: []string{"test-facet-set-alpha", "test-facet-set-gamma"}, Rarities: []string{topRarity}},
	} {
		if n := assertFacetsAreTheirRows(t, s, f, label); n == 0 {
			t.Errorf("%s: nothing was checked", label)
		}
	}

	// And one combination per value of every facet that has items: each moves some count or some
	// span end, so a condition that ignores that facet's flag shows somewhere (the mutant sweep).
	all, err := s.List(context.Background(), items.Filters{WithFacets: true, Limit: 1})
	if err != nil {
		t.Fatal(err)
	}
	for _, g := range []struct {
		param string
		group items.FacetGroup
	}{{"rarity", all.Facets.Rarity}, {"equip_location", all.Facets.EquipLocation}, {"armour_weight", all.Facets.ArmourWeight},
		{"class", all.Facets.Class}, {"currency", all.Facets.Currency}, {"set", all.Facets.Set}} {
		for _, v := range g.group.Values {
			if v.Count > 0 {
				assertFacetsAreTheirRows(t, s, withFacet(t, items.Filters{}, g.param, v.Slug), g.param+"="+v.Slug)
			}
		}
	}
}

// A value nothing matches is still listed, at 0 — and the "has a price" rule is ANY source: the item
// free at one source and priced at another counts as priced; the item bought with one currency from
// two sources counts once.
func TestAZeroIsListedAndPriceMeansAnySource(t *testing.T) {
	s, d := facetService(t)
	res, err := s.List(context.Background(), items.Filters{WithFacets: true})
	if err != nil {
		t.Fatal(err)
	}
	var currencies int
	if err := d.QueryRowContext(context.Background(), "SELECT count(*) FROM currencies").Scan(&currencies); err != nil {
		t.Fatal(err)
	}
	if len(res.Facets.Currency.Values) != currencies {
		t.Errorf("the currency facet lists %d values, the vocabulary has %d — a 0 was hidden", len(res.Facets.Currency.Values), currencies)
	}
	zeros := 0
	for _, v := range res.Facets.Currency.Values {
		if v.Count == 0 {
			zeros++
		}
	}
	if zeros == 0 {
		t.Error("no currency counted 0 — the fixture uses two, so the rest must be listed at 0")
	}
	if res.Facets.Price.Count != 7 || res.Facets.Price.Any != 9 {
		t.Errorf("price = %d of %d, want 7 of 9 (Alpha priced at one source of two; Gamma and Iota have none)", res.Facets.Price.Count, res.Facets.Price.Any)
	}
	first := facetSlug(t, d, "currencies", "id", 0)
	for _, v := range res.Facets.Currency.Values {
		if v.Slug == first && v.Count != 3 {
			t.Errorf("first currency counts %d, want 3 items (Epsilon's two sources are one item)", v.Count)
		}
	}
	if sp := res.Facets.ItemLevel; sp == nil || sp.Min != 10 || sp.Max != 90 {
		t.Errorf("ilvl span = %+v, want 10–90", sp)
	}
	if sp := res.Facets.RequiresLevel; sp == nil || sp.Min != 10 || sp.Max != 80 {
		t.Errorf("reqlvl span = %+v, want 10–80", sp)
	}
}

// AOC-064: a group takes several values, any of them. For a facet that holds one value per item
// (rarity, armour weight, set), ticking several gives EXACTLY the sum of their counts — the number
// beside each box is what ticking it adds. For every facet, exactly the union, by id, of what each
// value gives alone.
func TestSeveralValuesInAGroupAreAnyOfThem(t *testing.T) {
	s, _ := facetService(t)
	ctx := context.Background()
	for _, base := range []items.Filters{{}, {ILvlMin: i32(40)}} {
		res, err := s.List(ctx, items.Filters{WithFacets: true, Limit: 1, ILvlMin: base.ILvlMin})
		if err != nil {
			t.Fatal(err)
		}
		for _, g := range []struct {
			param     string
			group     items.FacetGroup
			exclusive bool
		}{{"rarity", res.Facets.Rarity, true}, {"armour_weight", res.Facets.ArmourWeight, true}, {"set", res.Facets.Set, true},
			{"equip_location", res.Facets.EquipLocation, false}, {"class", res.Facets.Class, false}, {"currency", res.Facets.Currency, false}} {
			var picked []string
			var sum int64
			for _, v := range g.group.Values {
				if v.Count > 0 && len(picked) < 2 {
					picked = append(picked, v.Slug)
					sum += v.Count
				}
			}
			if len(picked) < 2 {
				continue
			}
			f := base
			switch g.param {
			case "rarity":
				f.Rarities = picked
			case "armour_weight":
				f.ArmourWeights = picked
			case "set":
				f.Sets = picked
			case "equip_location":
				f.EquipLocations = picked
			case "class":
				f.Classes = picked
			case "currency":
				f.Currencies = picked
			}
			got := total(t, s, f)
			if g.exclusive && got != sum {
				t.Errorf("%s %v under %+v: %d items, want exactly the sum of their counts, %d", g.param, picked, base, got, sum)
			}
			// Every facet: exactly the union of the items each value alone gives, counted by id — a
			// bound is not enough (a filter that read only the first value passed one, AOC-064 mutant).
			union := map[int32]bool{}
			for _, one := range picked {
				alone := withFacet(t, base, g.param, one)
				alone.Limit = items.MaxLimit // every row, not the first page: the fixture is far smaller
				r, err := s.List(ctx, alone)
				if err != nil {
					t.Fatal(err)
				}
				for _, it := range r.Items {
					union[it.ID] = true
				}
			}
			if got != int64(len(union)) {
				t.Errorf("%s %v under %+v: %d items, want the union of what each gives alone, %d", g.param, picked, base, got, len(union))
			}
		}
	}
}
