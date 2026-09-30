package db_test

// AOC-058 verify round 1: the two guards no other test reached. Each was a surviving mutant —
// the Down with its DROP COLUMN deleted, and checkCompounds blind to '/' — and each test below
// fails against it. Fixture values are obviously fake.

import (
	"context"
	"database/sql"
	"strings"
	"testing"

	"github.com/pressly/goose/v3"
)

func twoHandedColumnExists(t *testing.T, d *sql.DB) bool {
	t.Helper()
	var n int
	if err := d.QueryRowContext(context.Background(), `
SELECT count(*) FROM information_schema.columns
WHERE table_schema = 'public' AND table_name = 'item_types' AND column_name = 'two_handed'`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n == 1
}

// `make migrate-redo` on this migration: the Down removes item_types.two_handed and the Up can run
// again. A full DownTo(0) cannot see a Down that forgets the column — the taxonomy migration's Down
// drops item_types whole — so this goes down exactly one step, the way a rollback does.
func TestTheWeaponMigrationRedoes(t *testing.T) {
	url := freshDatabase(t)
	d := openGoose(t, url)
	if err := goose.UpTo(d, migrationsDir, weaponHands); err != nil {
		t.Fatalf("goose up to %d: %v", weaponHands, err)
	}
	if !twoHandedColumnExists(t, d) {
		t.Fatal("after up, item_types.two_handed does not exist")
	}
	if err := goose.DownTo(d, migrationsDir, beforeWeaponHands); err != nil {
		t.Fatalf("goose down to %d: %v", beforeWeaponHands, err)
	}
	if twoHandedColumnExists(t, d) {
		t.Error("after down, item_types.two_handed still exists — the Down does not reverse the Up")
	}
	if err := goose.UpTo(d, migrationsDir, weaponHands); err != nil {
		t.Fatalf("goose up again after down: %v", err)
	}
	var classified int
	if err := d.QueryRowContext(context.Background(),
		`SELECT count(*) FROM item_types WHERE two_handed IS NOT NULL`).Scan(&classified); err != nil {
		t.Fatal(err)
	}
	if classified != 11 {
		t.Errorf("after redo, %d types classified, want 11", classified)
	}
}

// The '/' half of checkCompounds: TestAnUnknownCompoundSlotStopsTheImport only feeds it a ','.
func TestAnUnknownSlashCompoundStopsTheImport(t *testing.T) {
	pool, _ := importTarget(t)
	odd := strings.Replace(fixtureJSON, `"equip_location":"Left/Right Finger"`, `"equip_location":"Head/Chest"`, 1)
	if odd == fixtureJSON {
		t.Fatal("the fixture no longer carries the ring's compound value")
	}
	r := runImport(t, pool, odd)
	if r.err == nil || !strings.Contains(r.err.Error(), "REFUSING TO GUESS") || !strings.Contains(r.err.Error(), `"Head/Chest"`) {
		t.Fatalf("import of an unknown '/' compound = %v, want a refusal naming it", r.err)
	}
}
