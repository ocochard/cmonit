package db

import (
	"database/sql"
	"path/filepath"
	"testing"
)

// schema_version is append-only and the current version is its maximum.
func rollBackVersionTo14(t *testing.T, d *sql.DB) {
	t.Helper()
	if _, err := d.Exec(`DELETE FROM schema_version WHERE version > 14`); err != nil {
		t.Fatal(err)
	}
	if err := setSchemaVersion(d, 14); err != nil {
		t.Fatal(err)
	}
}

// A v14 database has alert_rules without the exclusion columns; opening it
// must add them and reach the current version.
func TestMigrateV14AddsRuleExclusionColumns(t *testing.T) {
	path := filepath.Join(t.TempDir(), "m.db")
	d, err := InitDB(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, col := range []string{"exclude_host_pattern", "exclude_service_pattern"} {
		if _, err := d.Exec("ALTER TABLE alert_rules DROP COLUMN " + col); err != nil {
			t.Fatal(err)
		}
	}
	rollBackVersionTo14(t, d)
	d.Close()

	d, err = InitDB(path)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()

	var n int
	d.QueryRow(`SELECT COUNT(*) FROM pragma_table_info('alert_rules') WHERE name IN ('exclude_host_pattern', 'exclude_service_pattern')`).Scan(&n)
	if n != 2 {
		t.Fatalf("exclusion columns after migration = %d, want 2", n)
	}
	if v, _ := getSchemaVersion(d); v != currentSchemaVersion {
		t.Fatalf("schema version = %d, want %d", v, currentSchemaVersion)
	}
	if _, err := d.Exec(`INSERT INTO alert_rules (name) VALUES ('x')`); err != nil {
		t.Fatalf("insert with defaults: %v", err)
	}
}

// Migrating through v13 after the columns joined createAlertTables must not
// fail on duplicate columns.
func TestMigrateV14IsIdempotentWhenColumnsExist(t *testing.T) {
	path := filepath.Join(t.TempDir(), "m.db")
	d, err := InitDB(path)
	if err != nil {
		t.Fatal(err)
	}
	rollBackVersionTo14(t, d)
	d.Close()

	d, err = InitDB(path)
	if err != nil {
		t.Fatalf("migration with existing columns failed: %v", err)
	}
	d.Close()
}
