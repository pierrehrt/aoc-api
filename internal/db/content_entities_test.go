package db_test

// The seeded content model — taxonomies (AOC-009, first half) and place entities (second half).
//
// ⚠️ THESE ARE INTEGRATION TESTS, like the rest of this package: the thing under test is SQL, and
// SQL that has never met a database is not tested. They skip loudly without a Postgres; CI always
// has one.
//
// ⭐ WHAT THEY CAN AND CANNOT PIN. The seeds are GENERATED from armory_snapshot/, which is a
// different repository and is not present in CI — so no test here can re-derive the numbers from
// the source data. What they pin instead is (a) the counts, as a regression tripwire: regenerate
// against a newer snapshot and these fail until someone looks at the diff and updates them
// deliberately, and (b) the invariants that must hold WHATEVER the snapshot says — the ones a
// silent seed change would otherwise break on a page months later.

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/pressly/goose/v3"

	"github.com/pierrehrt/aoc-api/internal/db/sqlcgen"
)

// migratedDB brings a throwaway database up to date and hands back a connection to it.
func migratedDB(t *testing.T) (*sql.DB, string) {
	t.Helper()
	url := freshDatabase(t)
	d := openGoose(t, url)
	if err := goose.Up(d, migrationsDir); err != nil {
		t.Fatalf("goose up: %v", err)
	}
	return d, url
}

func count(t *testing.T, d *sql.DB, table string) int {
	t.Helper()
	var n int
	// #nosec G202 -- table names come from the literals in this file, never from input.
	if err := d.QueryRowContext(context.Background(), "SELECT count(*) FROM "+table).Scan(&n); err != nil {
		t.Fatalf("counting %s: %v", table, err)
	}
	return n
}

// ⭐ THE COUNTS, every one of them measured from items_clean.json and reference_geography.json on
// 2026-09-18 by the two generators in scripts/. They are written here so that a regenerated seed
// cannot change the shape of the site without someone saying so out loud.
//
// Several differ from the numbers AOC-009 was planned with in September, because AOC-016 and
// AOC-017 changed the data underneath it: item_types 32 -> 30, equip_locations 37 -> 13 (the two
// compound values are not slots), factions 16 -> 14 (three spellings of Wolves of the Steppes),
// currencies 26 -> 24. The ticket's table was never a source; the snapshot is.
func TestTheSeededCountsAreWhatWasMeasured(t *testing.T) {
	d, _ := migratedDB(t)

	for table, want := range map[string]int{
		// taxonomy
		"archetypes": 4, "classes": 12, "rarities": 6, "armour_weights": 5,
		"item_types": 30, "equip_locations": 13, "currencies": 24, "acquisition_types": 3,
		"tiers": 10, "bindings": 3, "factions": 14,
		// place entities
		"confidence_levels": 4, "regions": 8, "maps": 26, "places": 86,
		"bosses": 44, "quests": 51, "containers": 2,
	} {
		if got := count(t, d, table); got != want {
			t.Errorf("%s has %d rows, want %d — if the snapshot moved, re-measure and update this "+
				"test deliberately", table, got, want)
		}
	}
}

// ⭐ THE IDEMPOTENCY TEST THAT ACTUALLY TESTS IDEMPOTENCY, for every migration rather than one.
//
// TestSeedsAreIdempotent does down+up, which drops the tables — so `up` can never MEET a duplicate
// and the ON CONFLICT clauses could all be deleted with it still green (its own comment says so).
// This re-executes every seed statement against an already-seeded database, which is the thing
// that happens for real: a migration re-applied, a seed copied into a later one.
func TestEverySeedReExecutedChangesNothing(t *testing.T) {
	d, _ := migratedDB(t)
	ctx := context.Background()

	before := map[string]int{}
	for _, table := range seededTables(t) {
		before[table] = count(t, d, table)
	}
	if len(before) < 15 {
		t.Fatalf("only found seeds for %d tables, want every taxonomy and place table — the "+
			"statement scanner has stopped finding them", len(before))
	}

	for _, stmt := range seedStatements(t) {
		if _, err := d.ExecContext(ctx, stmt); err != nil {
			t.Fatalf("re-executing a seed failed: %v\nstatement: %.200s", err, stmt)
		}
	}

	for table, was := range before {
		if got := count(t, d, table); got != was {
			t.Errorf("%s went from %d rows to %d when its seed was re-executed — the seed is not "+
				"idempotent", table, was, got)
		}
	}
}

// seedStatements lifts every INSERT out of every migration's Up section, verbatim, so the test
// exercises what will actually run rather than a copy that may have drifted. It reuses the same
// literal-aware splitter the static check reads with — one scanner, so the two cannot disagree
// about where a statement ends.
func seedStatements(t *testing.T) []string {
	t.Helper()
	var out []string
	for _, f := range migrationFiles(t) {
		b, err := os.ReadFile(f) // #nosec G304 -- fixed directory listed above
		if err != nil {
			t.Fatalf("reading %s: %v", f, err)
		}
		body := string(b)
		if i := strings.Index(body, "-- +goose Down"); i >= 0 {
			body = body[:i]
		}
		for _, stmt := range sqlStatements(body) {
			if strings.Contains(strings.ToUpper(stmt.code), "INSERT INTO") {
				out = append(out, stmt.text)
			}
		}
	}
	if len(out) == 0 {
		t.Fatal("found no seed statements at all — the scanner is broken, not the migrations")
	}
	return out
}

// seededTables is the set of tables those statements insert into.
func seededTables(t *testing.T) []string {
	t.Helper()
	seen := map[string]bool{}
	for _, s := range seedStatements(t) {
		i := strings.Index(strings.ToUpper(s), "INSERT INTO ")
		fields := strings.Fields(s[i+len("INSERT INTO "):])
		if len(fields) == 0 {
			t.Fatalf("cannot read a table name out of %.60s", s)
		}
		name := fields[0]
		if j := strings.IndexByte(name, '('); j >= 0 {
			name = name[:j]
		}
		seen[name] = true
	}
	var out []string
	for name := range seen {
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}

func migrationFiles(t *testing.T) []string {
	t.Helper()
	entries, err := os.ReadDir(migrationsDir)
	if err != nil {
		t.Fatalf("reading %s: %v", migrationsDir, err)
	}
	var out []string
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".sql") {
			out = append(out, filepath.Join(migrationsDir, e.Name()))
		}
	}
	return out
}

// ⭐ A class's max_armour_weight is the CEILING it may wear. It is Pierre-sourced, not derivable
// from the armory, and nobody has asked him — so it is NULL for all twelve and must stay that way
// until someone does. Filling it from an item's own armour_weight would be the same fact wearing
// a different hat, and wrong for every class (DECISIONS.md, 2026-09-12).
func TestNoClassHasAGuessedArmourCeiling(t *testing.T) {
	d, _ := migratedDB(t)

	var classes, withArchetype, withCeiling int
	row := d.QueryRowContext(context.Background(), `
		SELECT count(*), count(archetype_id), count(max_armour_weight) FROM classes`)
	if err := row.Scan(&classes, &withArchetype, &withCeiling); err != nil {
		t.Fatalf("reading classes: %v", err)
	}
	if classes != 12 || withArchetype != 12 {
		t.Errorf("%d classes, %d with an archetype, want 12 and 12", classes, withArchetype)
	}
	if withCeiling != 0 {
		t.Errorf("%d classes carry a max_armour_weight — nobody has asked Pierre, so every one of "+
			"them is a guess", withCeiling)
	}
}

// ⭐ places.region_id is carried even when map_id is set, so "every place in Stygia" is one join.
// That redundancy is only safe while the two agree, and the generator is what keeps them agreeing.
// This is the test the migration's own comment names.
func TestEveryPlacesRegionMatchesItsMap(t *testing.T) {
	d, _ := migratedDB(t)

	rows, err := d.QueryContext(context.Background(), `
		SELECT p.name, r.name, mr.name
		FROM places p
		JOIN maps m  ON m.id = p.map_id
		JOIN regions r  ON r.id = p.region_id
		LEFT JOIN regions mr ON mr.id = m.region_id
		WHERE m.region_id IS DISTINCT FROM p.region_id`)
	if err != nil {
		t.Fatalf("querying: %v", err)
	}
	defer rows.Close()
	for rows.Next() {
		var place, placeRegion string
		var mapRegion *string
		if err := rows.Scan(&place, &placeRegion, &mapRegion); err != nil {
			t.Fatalf("scanning: %v", err)
		}
		got := "no region"
		if mapRegion != nil {
			got = *mapRegion
		}
		t.Errorf("%s is in region %s but its map is in %s", place, placeRegion, got)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterating: %v", err)
	}
}

// ⭐ Some instances hang straight off a region with no map between (Skull Gate Pass, Kuthchemes —
// both entered from the Raid Finder). That is why map_id is nullable, and a seed that lost them
// would make the column look safe to tighten.
func TestSomePlacesHaveNoMapAndThatIsCorrect(t *testing.T) {
	d, _ := migratedDB(t)
	if got := count(t, d, "places WHERE map_id IS NULL"); got != 2 {
		t.Errorf("%d places have no map, want 2 (Skull Gate Pass, Kuthchemes Temple Onslaught) — "+
			"map_id is nullable precisely because of them", got)
	}
}

// ⭐ Region -> map -> instance is not deep enough: House of Crom and Warmonk Monastery CONTAIN
// instances. Flattening that loses 152 and 174 rows of loot off a page that can then never exist.
func TestThePlaceHierarchyIsWellFormed(t *testing.T) {
	d, _ := migratedDB(t)
	ctx := context.Background()

	rows, err := d.QueryContext(ctx, `
		SELECT c.name, p.name, c.region_id = p.region_id
		FROM places c JOIN places p ON p.id = c.parent_place_id`)
	if err != nil {
		t.Fatalf("querying: %v", err)
	}
	defer rows.Close()

	children := map[string]string{}
	for rows.Next() {
		var child, parent string
		var sameRegion bool
		if err := rows.Scan(&child, &parent, &sameRegion); err != nil {
			t.Fatalf("scanning: %v", err)
		}
		children[child] = parent
		if !sameRegion {
			t.Errorf("%s sits inside %s but in a different region", child, parent)
		}
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterating: %v", err)
	}

	want := map[string]string{
		"Threshold of Divinity": "House of Crom",
		"The Vile Nativity":     "House of Crom",
		"Coppice of the Heart":  "Warmonk Monastery",
		"Vortex of the Storm":   "Warmonk Monastery",
		"Reliquary of Flame":    "Warmonk Monastery",
	}
	for child, parent := range want {
		if children[child] != parent {
			t.Errorf("%s has parent %q, want %q", child, children[child], parent)
		}
	}
	if len(children) != len(want) {
		t.Errorf("%d places have a parent, want %d: %v", len(children), len(want), children)
	}

	// ⚠️ The parent is parsed out of the name — "Threshold of Divinity (House of Crom)" — and a
	// name that still carries it would mean the parse silently did not happen.
	var leaked int
	if err := d.QueryRowContext(ctx,
		`SELECT count(*) FROM places WHERE name LIKE '%(House of Crom)%' OR name LIKE '%(Warmonk%'`,
	).Scan(&leaked); err != nil {
		t.Fatalf("querying: %v", err)
	}
	if leaked != 0 {
		t.Errorf("%d place names still carry their parent in parentheses", leaked)
	}

	var cycles int
	if err := d.QueryRowContext(ctx,
		`SELECT count(*) FROM places WHERE parent_place_id = id`).Scan(&cycles); err != nil {
		t.Fatalf("querying: %v", err)
	}
	if cycles != 0 {
		t.Errorf("%d places are their own parent", cycles)
	}
}

// ⭐ An Unchained dungeon is its own place, not a flag on its twin: seven places exist in both
// forms and ZERO items are shared between any pair (Pierre, 2026-09-13). The boolean is kept as a
// filter, but if the two ever collapsed into one row the loot would merge — silently, and wrongly.
func TestEveryUnchainedVariantIsItsOwnPlace(t *testing.T) {
	d, _ := migratedDB(t)
	ctx := context.Background()

	rows, err := d.QueryContext(ctx, `SELECT name, unchained FROM places ORDER BY name`)
	if err != nil {
		t.Fatalf("querying: %v", err)
	}
	defer rows.Close()

	names := map[string]bool{}
	for rows.Next() {
		var name string
		var unchained bool
		if err := rows.Scan(&name, &unchained); err != nil {
			t.Fatalf("scanning: %v", err)
		}
		names[name] = unchained
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterating: %v", err)
	}

	variants := 0
	for name, unchained := range names {
		base, found := strings.CutSuffix(name, " (Unchained)")
		if !found {
			continue
		}
		variants++
		if !unchained {
			t.Errorf("%q is named as an Unchained variant but its flag is false", name)
		}
		if _, ok := names[base]; !ok {
			t.Errorf("%q has no %q to be a variant OF", name, base)
		}
	}
	if variants != 5 {
		t.Errorf("%d places are named as Unchained variants, want 5", variants)
	}
}

// ⭐ Containers are NOT places: they have no region and no map, and the dungeons that drop them
// span several. The armory filed them alongside instances, which is exactly how one ends up with
// a page for a chest (Pierre, 2026-09-13).
func TestNoContainerIsAlsoAPlace(t *testing.T) {
	d, _ := migratedDB(t)
	var clash int
	if err := d.QueryRowContext(context.Background(),
		`SELECT count(*) FROM containers c JOIN places p ON p.name = c.name`).Scan(&clash); err != nil {
		t.Fatalf("querying: %v", err)
	}
	if clash != 0 {
		t.Errorf("%d containers are also seeded as places", clash)
	}
}

// ⭐ RULE 13: every fact on this site carries its provenance. A row with a blank source_note is a
// fact with no source, which is the one thing this product cannot ship.
func TestEverySeededRowSaysWhereItCameFrom(t *testing.T) {
	d, _ := migratedDB(t)
	for _, table := range []string{"regions", "maps", "places", "bosses", "quests", "containers"} {
		var blank int
		// #nosec G202 -- table names are the literals above.
		if err := d.QueryRowContext(context.Background(),
			"SELECT count(*) FROM "+table+" WHERE source_note IS NULL OR btrim(source_note) = ''",
		).Scan(&blank); err != nil {
			t.Fatalf("querying %s: %v", table, err)
		}
		if blank != 0 {
			t.Errorf("%d rows in %s carry no source_note", blank, table)
		}
	}
}

// ⭐ The twenty things we do not know, and the one query that finds them all. An empty field is a
// feature; what would be a defect is a doubt that stops being visible — the row would then read as
// fact. Ten places, four bosses, two maps and four quests, as of 2026-09-18.
func TestTheOpenQuestionsAreStillVisible(t *testing.T) {
	_, url := migratedDB(t)
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, url)
	if err != nil {
		t.Fatalf("pool: %v", err)
	}
	defer pool.Close()

	got, err := sqlcgen.New(pool).ListOpenQuestions(ctx)
	if err != nil {
		t.Fatalf("ListOpenQuestions: %v", err)
	}
	want := map[string]int{"place": 10, "boss": 4, "map": 2, "quest": 4}
	byEntity := map[string]int{}
	for _, q := range got {
		byEntity[q.Entity]++
	}
	for entity, n := range want {
		if byEntity[entity] != n {
			t.Errorf("%d open questions on %ss, want %d — one disappearing means either it was "+
				"answered, which belongs in a Log entry, or it was smoothed over",
				byEntity[entity], entity, n)
		}
	}
	if len(got) != 20 {
		t.Errorf("%d open questions in total, want 20: %v", len(got), byEntity)
	}
	for _, q := range got {
		if q.OpenQuestion == nil || strings.TrimSpace(*q.OpenQuestion) == "" {
			t.Errorf("%s/%s is listed as having an open question but the text is empty", q.Entity, q.Slug)
		}
	}
}

// ⭐ The armory's quest column records the quest GIVER or a hub as often as a quest title, so
// `name` is NULL for every quest and armory_label holds exactly what the source said. A name that
// appeared here without anyone answering that question would be an invented one.
func TestNoQuestHasAnInventedName(t *testing.T) {
	d, _ := migratedDB(t)
	var named int
	if err := d.QueryRowContext(context.Background(),
		`SELECT count(*) FROM quests WHERE name IS NOT NULL`).Scan(&named); err != nil {
		t.Fatalf("querying: %v", err)
	}
	if named != 0 {
		t.Errorf("%d quests have a name — the armory does not record one, so it came from "+
			"somewhere that must be cited", named)
	}
}

// The sqlc chain for the place entities: generated Go against the real migrated schema, and the
// joins actually resolving rather than returning nulls.
func TestTheGeneratedPlaceQueriesReturnTheTree(t *testing.T) {
	_, url := migratedDB(t)
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, url)
	if err != nil {
		t.Fatalf("pool: %v", err)
	}
	defer pool.Close()
	q := sqlcgen.New(pool)

	regions, err := q.ListRegions(ctx)
	if err != nil {
		t.Fatalf("ListRegions: %v", err)
	}
	if len(regions) != 8 || regions[0].Name == "" {
		t.Fatalf("ListRegions returned %d rows", len(regions))
	}
	for i := 1; i < len(regions); i++ {
		if regions[i-1].SortOrder > regions[i].SortOrder {
			t.Errorf("regions came back out of sort_order at %d", i)
		}
	}

	places, err := q.ListPlaces(ctx)
	if err != nil {
		t.Fatalf("ListPlaces: %v", err)
	}
	if len(places) != 86 {
		t.Errorf("ListPlaces returned %d rows, want 86", len(places))
	}
	for _, p := range places {
		if p.RegionName == "" {
			t.Errorf("place %q came back with no region name — the join did not resolve", p.Slug)
		}
		if p.Confidence == "" {
			t.Errorf("place %q came back with no confidence", p.Slug)
		}
	}

	hoc, err := q.GetPlaceBySlug(ctx, "house-of-crom")
	if err != nil {
		t.Fatalf("GetPlaceBySlug(house-of-crom): %v", err)
	}
	if hoc.MapName == nil || *hoc.MapName != "Field of the Dead" {
		t.Errorf("House of Crom's map came back as %v, want Field of the Dead", hoc.MapName)
	}
	inside, err := q.ListPlacesInPlace(ctx, &hoc.ID)
	if err != nil {
		t.Fatalf("ListPlacesInPlace: %v", err)
	}
	if len(inside) != 2 {
		t.Errorf("House of Crom contains %d places, want 2", len(inside))
	}

	tod, err := q.GetPlaceBySlug(ctx, "threshold-of-divinity")
	if err != nil {
		t.Fatalf("GetPlaceBySlug(threshold-of-divinity): %v", err)
	}
	bosses, err := q.ListBossesInPlace(ctx, &tod.ID)
	if err != nil {
		t.Fatalf("ListBossesInPlace: %v", err)
	}
	if len(bosses) != 3 {
		t.Errorf("Threshold of Divinity has %d bosses, want 3", len(bosses))
	}

	quests, err := q.ListQuests(ctx)
	if err != nil {
		t.Fatalf("ListQuests: %v", err)
	}
	if len(quests) != 51 {
		t.Errorf("ListQuests returned %d rows, want 51", len(quests))
	}
	containers, err := q.ListContainers(ctx)
	if err != nil {
		t.Fatalf("ListContainers: %v", err)
	}
	if len(containers) != 2 {
		t.Errorf("ListContainers returned %d rows, want 2", len(containers))
	}
	levels, err := q.ListConfidenceLevels(ctx)
	if err != nil {
		t.Fatalf("ListConfidenceLevels: %v", err)
	}
	if len(levels) != 4 {
		t.Errorf("ListConfidenceLevels returned %d rows, want 4", len(levels))
	}
}
