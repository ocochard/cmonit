package db

import (
	"database/sql"
	"path/filepath"
	"testing"
)

// schema_version is append-only and the current version is its maximum.
func rollBackVersionTo14(t *testing.T, d *sql.DB) { rollBackVersionTo(t, d, 14) }

func rollBackVersionTo(t *testing.T, d *sql.DB, v int) {
	t.Helper()
	if _, err := d.Exec(`DELETE FROM schema_version WHERE version > ?`, v); err != nil {
		t.Fatal(err)
	}
	if err := setSchemaVersion(d, v); err != nil {
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

// v15 databases have the two metric_type-led indexes; v16 drops them and
// adds the graph index.
func TestMigrateV15DropsUnusedMetricsIndexes(t *testing.T) {
	path := filepath.Join(t.TempDir(), "m.db")
	d, err := InitDB(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, stmt := range []string{
		`CREATE INDEX idx_metrics_type_host ON metrics(metric_type, host_id, collected_at DESC, metric_name, value)`,
		`CREATE INDEX idx_metrics_type_name_host ON metrics(metric_type, metric_name, host_id, collected_at DESC)`,
		`DROP INDEX idx_metrics_host_service_time`,
	} {
		if _, err := d.Exec(stmt); err != nil {
			t.Fatalf("%s: %v", stmt, err)
		}
	}
	rollBackVersionTo(t, d, 15)
	d.Close()

	d, err = InitDB(path)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()

	has := func(name string) bool {
		var n int
		d.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type = 'index' AND name = ?`, name).Scan(&n)
		return n > 0
	}
	if has("idx_metrics_type_host") || has("idx_metrics_type_name_host") {
		t.Fatal("old indexes still present after migration")
	}
	if !has("idx_metrics_host_service_time") || !has("idx_metrics_lookup") || !has("idx_metrics_time") {
		t.Fatal("expected indexes missing after migration")
	}
	if v, _ := getSchemaVersion(d); v != 16 {
		t.Fatalf("schema version = %d, want 16", v)
	}
}
