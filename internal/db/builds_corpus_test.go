//go:build corpus

// A corpus test: compiled only with `-tags corpus` (AOC-044) — see item_read_endpoints_test.go.

package db_test

import (
	"context"
	"fmt"
	"math/rand"
	"sort"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/pierrehrt/aoc-api/internal/builds"
	"github.com/pierrehrt/aoc-api/internal/db/sqlcgen"
)

// ⭐ The gear builder over every real item (AOC-051).
//   - The hands it derives are the main hand and the off hand.
//   - Every two-hander takes the off hand: a shield beside one is held. A bow still takes its
//     ammunition (Pierre, 2026-10-05).
//   - Its sums equal an independent SQL sum over the same items, multiplicity kept (two copies of a
//     ring count twice), on 300 builds drawn from the corpus with a fixed seed.
func TestTheBuilderOnTheRealCorpus(t *testing.T) {
	pool := readPool(t)
	ctx := context.Background()
	svc := builds.NewService(sqlcgen.New(pool))

	var hands []string
	if err := pool.QueryRow(ctx, `SELECT array_agg(el.slug ORDER BY el.slug) FROM equip_locations el
		WHERE el.id = ANY($1::int[])`, mustHands(t, pool)).Scan(&hands); err != nil {
		t.Fatal(err)
	}
	if strings.Join(hands, ",") != "main-hand,off-hand" {
		t.Errorf("hands = %v, want main-hand,off-hand", hands)
	}

	ids := func(q string) []int32 {
		rows, err := pool.Query(ctx, q)
		if err != nil {
			t.Fatal(err)
		}
		defer rows.Close()
		var out []int32
		for rows.Next() {
			var id int32
			if err := rows.Scan(&id); err != nil {
				t.Fatal(err)
			}
			out = append(out, id)
		}
		return out
	}
	byType := func(slug string) []int32 {
		return ids(`SELECT i.item_id FROM items i JOIN item_types t ON t.id = i.item_type_id WHERE t.slug = '` + slug + `' ORDER BY i.item_id`)
	}
	shield, ammo := byType("shield")[0], byType("ammunition")[0]
	// Every two-hander that goes in the main hand. (One polearm-typed item goes in no slot — AOC-054
	// records it — and in the builder it "does not go here" and holds nothing.)
	for _, two := range ids(`SELECT i.item_id FROM items i JOIN item_types t ON t.id = i.item_type_id
		JOIN item_equip_locations e ON e.item_id = i.item_id JOIN equip_locations el ON el.id = e.equip_location_id
		WHERE t.two_handed AND el.slug = 'main-hand' ORDER BY i.item_id`) {
		res, err := svc.Compute(ctx, builds.Build{Present: true, Entries: []builds.Entry{{Slot: "main-hand", ItemID: two}, {Slot: "off-hand", ItemID: shield}}})
		if err != nil {
			t.Fatal(err)
		}
		if got := buildStatus(res, "off-hand"); got != builds.StatusHeld {
			t.Errorf("a shield beside two-hander %d: %s, want held", two, got)
		}
	}
	for _, bow := range byType("bow") {
		res, err := svc.Compute(ctx, builds.Build{Present: true, Entries: []builds.Entry{{Slot: "main-hand", ItemID: bow}, {Slot: "off-hand", ItemID: ammo}}})
		if err != nil {
			t.Fatal(err)
		}
		if got := buildStatus(res, "off-hand"); got != builds.StatusEquipped {
			t.Errorf("ammunition beside bow %d: %s, want equipped", bow, got)
		}
	}

	// 300 builds: each slot filled with a random item of its own, two rings sometimes the same one.
	bySlot := map[string][]int32{}
	rows, err := pool.Query(ctx, `SELECT el.slug, iel.item_id FROM item_equip_locations iel JOIN equip_locations el ON el.id = iel.equip_location_id ORDER BY 1, 2`)
	if err != nil {
		t.Fatal(err)
	}
	for rows.Next() {
		var s string
		var id int32
		if err := rows.Scan(&s, &id); err != nil {
			t.Fatal(err)
		}
		bySlot[s] = append(bySlot[s], id)
	}
	rows.Close()
	var slots []string
	for s := range bySlot {
		slots = append(slots, s)
	}
	sort.Strings(slots)
	r := rand.New(rand.NewSource(51))
	for n := 0; n < 300; n++ {
		b := builds.Build{Present: true}
		for _, s := range slots {
			if r.Intn(3) > 0 {
				b.Entries = append(b.Entries, builds.Entry{Slot: s, ItemID: bySlot[s][r.Intn(len(bySlot[s]))]})
			}
		}
		res, err := svc.Compute(ctx, b)
		if err != nil {
			t.Fatal(err)
		}
		var worn []int32
		for _, s := range res.Slots {
			if s.Status == builds.StatusEquipped {
				worn = append(worn, s.ItemID)
			}
		}
		want := sqlSum(t, pool, worn)
		var got []string
		for _, s := range res.Stats {
			v := s.Value
			if s.Sign < 0 {
				v = "-" + v
			}
			got = append(got, fmt.Sprintf("%s|%s|%s|%v=%s", s.Stat, s.Unit, strOf(s.DamageType), s.PvP, v))
		}
		sort.Strings(got)
		if strings.Join(got, "\n") != strings.Join(want, "\n") {
			t.Fatalf("build %d %v:\n the builder sums\n%s\n SQL sums\n%s", n, b.Entries, strings.Join(got, "\n"), strings.Join(want, "\n"))
		}
		var armor *int64
		if err := pool.QueryRow(ctx, `SELECT sum(i.armor) FROM unnest($1::int[]) AS b(item_id) JOIN items i USING (item_id)`, worn).Scan(&armor); err != nil {
			t.Fatal(err)
		}
		if (armor == nil) != (res.Armor == nil) || (armor != nil && *armor != *res.Armor) {
			t.Fatalf("build %d: armor %v, SQL %v", n, res.Armor, armor)
		}
	}
}

// mustHands is the builder's own derivation of the hands, read through its query.
func mustHands(t *testing.T, pool *pgxpool.Pool) []int32 {
	t.Helper()
	ids, err := sqlcgen.New(pool).ListBuildHands(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	return ids
}

func buildStatus(res builds.Result, slot string) string {
	for _, s := range res.Slots {
		if s.Slot.Slug == slot {
			return s.Status
		}
	}
	return ""
}

// sqlSum is the sum the builder should print, computed apart from it: per (stat, unit, damage type,
// PvP), every row of every worn item, duplicates kept by unnest, in the column's own decimals.
func sqlSum(t *testing.T, pool *pgxpool.Pool, worn []int32) []string {
	t.Helper()
	rows, err := pool.Query(context.Background(), `
		SELECT st.stat, st.unit, COALESCE(st.damage_type, ''), st.pvp, sum(st.sign * st.value)::text
		FROM unnest($1::int[]) AS b(item_id) JOIN item_stats st USING (item_id)
		GROUP BY 1, 2, 3, 4`, worn)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var stat, unit, dt, sum string
		var pvp bool
		if err := rows.Scan(&stat, &unit, &dt, &pvp, &sum); err != nil {
			t.Fatal(err)
		}
		if sum == "0.00" || sum == "-0.00" {
			sum = "0.00"
		}
		out = append(out, fmt.Sprintf("%s|%s|%s|%v=%s", stat, unit, dt, pvp, sum))
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	sort.Strings(out)
	return out
}

func strOf(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}
