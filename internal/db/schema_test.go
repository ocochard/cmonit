package db

import (
	"database/sql"
	"path/filepath"
	"testing"
	"time"
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

// oldMetricsDDL is the v16 metrics table with its indexes.
var oldMetricsDDL = []string{
	`DROP TABLE metrics`,
	`DROP TABLE metric_series`,
	`CREATE TABLE metrics (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		host_id TEXT NOT NULL,
		service_name TEXT NOT NULL,
		metric_type TEXT NOT NULL,
		metric_name TEXT NOT NULL,
		value REAL NOT NULL,
		collected_at DATETIME NOT NULL
	)`,
	`CREATE INDEX idx_metrics_lookup ON metrics(host_id, service_name, metric_type, metric_name, collected_at)`,
	`CREATE INDEX idx_metrics_host_service_time ON metrics(host_id, service_name, collected_at)`,
	`CREATE INDEX idx_metrics_time ON metrics(collected_at)`,
}

func makeOldMetricsDB(t *testing.T, path string, version int, rows [][]any) {
	t.Helper()
	d, err := InitDB(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, stmt := range oldMetricsDDL {
		if _, err := d.Exec(stmt); err != nil {
			t.Fatalf("%s: %v", stmt, err)
		}
	}
	if _, err := d.Exec(`INSERT INTO hosts (id, hostname, last_seen, poll_interval) VALUES ('h1', 'web01', CURRENT_TIMESTAMP, 30)`); err != nil {
		t.Fatal(err)
	}
	for _, r := range rows {
		if _, err := d.Exec(`INSERT INTO metrics (host_id, service_name, metric_type, metric_name, value, collected_at) VALUES (?, ?, ?, ?, ?, ?)`, r...); err != nil {
			t.Fatal(err)
		}
	}
	rollBackVersionTo(t, d, version)
	d.Close()
}

// The v16 table stored text timestamps in the zone of the machine that wrote
// them; the migration must turn every format into the right unix second.
func TestMigrateV16ConvertsMetricsToSeries(t *testing.T) {
	path := filepath.Join(t.TempDir(), "m.db")
	makeOldMetricsDB(t, path, 16, [][]any{
		{"h1", "svc", "cpu", "user", 1.5, "2026-10-20 00:06:16.187047 +0000 UTC"},
		{"h1", "svc", "cpu", "user", 2.5, "2026-10-20 00:06:46 +0000 UTC"},
		{"h1", "svc", "cpu", "user", 9.0, "2026-10-20 01:06:16.5 +0100 CET"}, // same second as the first, other zone
		{"h1", "svc", "mem", "percent", 40.0, "2026-11-06 16:06:14.589392 +0100 CET"},
		{"h1", "svc", "mem", "percent", 41.0, "2026-11-06 16:06:14.5 +0100 CET m=+12.3"}, // monotonic suffix, same second
		{"h1", "svc", "mem", "percent", 99.0, "not a timestamp"},
	})

	d, err := InitDB(path)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()

	if v, _ := getSchemaVersion(d); v != currentSchemaVersion {
		t.Fatalf("schema version = %d, want %d", v, currentSchemaVersion)
	}
	var old int
	d.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE name IN ('metrics_old', 'idx_metrics_lookup', 'idx_metrics_time', 'idx_metrics_host_service_time')`).Scan(&old)
	if old != 0 {
		t.Fatalf("%d old objects left after migration", old)
	}

	var series, points int
	d.QueryRow(`SELECT COUNT(*) FROM metric_series`).Scan(&series)
	d.QueryRow(`SELECT COUNT(*) FROM metrics`).Scan(&points)
	if series != 2 || points != 3 {
		t.Fatalf("series = %d, points = %d, want 2 and 3 (same-second duplicates merged, bad timestamp skipped)", series, points)
	}

	want := time.Date(2026, 10, 20, 0, 6, 16, 0, time.UTC).Unix()
	var value float64
	err = d.QueryRow(`SELECT m.value FROM metrics m JOIN metric_series s ON s.id = m.series_id
		WHERE s.metric_type = 'cpu' AND s.metric_name = 'user' AND m.ts = ?`, want).Scan(&value)
	if err != nil {
		t.Fatalf("cpu point at %d not found: %v", want, err)
	}
	if value != 9.0 {
		t.Fatalf("merged value = %v, want the later row (9)", value)
	}
	want = time.Date(2026, 11, 6, 15, 6, 14, 0, time.UTC).Unix()
	var n int
	d.QueryRow(`SELECT COUNT(*) FROM metrics m JOIN metric_series s ON s.id = m.series_id
		WHERE s.metric_type = 'mem' AND m.ts = ?`, want).Scan(&n)
	if n != 1 {
		t.Fatalf("mem point at %d: %d rows, want 1 (CET converted to UTC)", want, n)
	}
}

// A v15 database also has the two metric_type-led indexes; v16 drops them and
// v17 replaces the table, so none of the old indexes may survive.
func TestMigrateV15ReachesCurrentVersionWithoutOldIndexes(t *testing.T) {
	path := filepath.Join(t.TempDir(), "m.db")
	makeOldMetricsDB(t, path, 15, [][]any{{"h1", "svc", "cpu", "user", 1.5, "2026-10-20 00:06:16 +0000 UTC"}})
	d, err := InitDB(path) // migrates 15 -> 16 -> 17
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	var idx int
	d.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type = 'index' AND name LIKE 'idx_metrics%'`).Scan(&idx)
	if idx != 0 {
		t.Fatalf("%d idx_metrics indexes present, want none", idx)
	}
	if v, _ := getSchemaVersion(d); v != currentSchemaVersion {
		t.Fatalf("schema version = %d, want %d", v, currentSchemaVersion)
	}
}

func TestMigrateOfMetricsAlreadyInNewShapeIsANoop(t *testing.T) {
	path := filepath.Join(t.TempDir(), "m.db")
	d, err := InitDB(path)
	if err != nil {
		t.Fatal(err)
	}
	rollBackVersionTo(t, d, 16) // new-shape tables with a v16 version row
	d.Close()
	d, err = InitDB(path)
	if err != nil {
		t.Fatalf("migration over new-shape tables failed: %v", err)
	}
	d.Close()
}

func openFresh(t *testing.T) *sql.DB {
	t.Helper()
	d, err := InitDB(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })
	if _, err := d.Exec(`INSERT INTO hosts (id, hostname, last_seen, poll_interval) VALUES ('h1', 'web01', CURRENT_TIMESTAMP, 30)`); err != nil {
		t.Fatal(err)
	}
	return d
}

func count(t *testing.T, d *sql.DB, q string, args ...any) int {
	t.Helper()
	var n int
	if err := d.QueryRow(q, args...).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func TestStoreMetricCreatesSeriesOnceAndReplacesSameSecond(t *testing.T) {
	d := openFresh(t)
	at := time.Unix(1_800_000_000, 0)
	for _, v := range []float64{1, 2} { // same second twice: the second wins
		if err := StoreMetric(d, "h1", "svc", "cpu", "user", v, at); err != nil {
			t.Fatal(err)
		}
	}
	if err := StoreMetric(d, "h1", "svc", "cpu", "user", 3, at.Add(30*time.Second)); err != nil {
		t.Fatal(err)
	}
	if n := count(t, d, `SELECT COUNT(*) FROM metric_series`); n != 1 {
		t.Fatalf("series = %d, want 1", n)
	}
	if n := count(t, d, `SELECT COUNT(*) FROM metrics`); n != 2 {
		t.Fatalf("points = %d, want 2", n)
	}
	var v float64
	d.QueryRow(`SELECT value FROM metrics WHERE ts = ?`, at.Unix()).Scan(&v)
	if v != 2 {
		t.Fatalf("value at %d = %v, want 2", at.Unix(), v)
	}
	var latest float64
	d.QueryRow(`SELECT value FROM latest_metrics WHERE host_id = 'h1'`).Scan(&latest)
	if latest != 3 {
		t.Fatalf("latest_metrics = %v, want 3", latest)
	}
}

func TestPruneMetricsRemovesOldPointsAndEmptySeries(t *testing.T) {
	d := openFresh(t)
	now := time.Now()
	old := now.AddDate(0, 0, -40)
	StoreMetric(d, "h1", "svc", "cpu", "user", 1, old)
	StoreMetric(d, "h1", "svc", "cpu", "user", 2, now)
	StoreMetric(d, "h1", "gone", "cpu", "user", 3, old) // every point is old

	if err := PruneOldData(d, 30); err != nil {
		t.Fatal(err)
	}
	if n := count(t, d, `SELECT COUNT(*) FROM metrics`); n != 1 {
		t.Fatalf("points after prune = %d, want 1", n)
	}
	if n := count(t, d, `SELECT COUNT(*) FROM metric_series`); n != 1 {
		t.Fatalf("series after prune = %d, want 1 (the empty one removed)", n)
	}
	// a removed series comes back when its metric is reported again
	if err := StoreMetric(d, "h1", "gone", "cpu", "user", 4, now); err != nil {
		t.Fatal(err)
	}
	if n := count(t, d, `SELECT COUNT(*) FROM metric_series`); n != 2 {
		t.Fatalf("series after re-report = %d, want 2", n)
	}
}

func TestDeleteHostRemovesItsMetricsAndSeries(t *testing.T) {
	d := openFresh(t)
	if _, err := d.Exec(`INSERT INTO hosts (id, hostname, last_seen, poll_interval) VALUES ('h2', 'db01', CURRENT_TIMESTAMP, 30)`); err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	StoreMetric(d, "h1", "svc", "cpu", "user", 1, now)
	StoreMetric(d, "h2", "svc", "cpu", "user", 2, now)
	if _, err := d.Exec(`UPDATE hosts SET last_seen = datetime('now', '-3 hours') WHERE id = 'h1'`); err != nil {
		t.Fatal(err)
	}

	stats, err := DeleteHost(d, "h1")
	if err != nil {
		t.Fatal(err)
	}
	if stats.Metrics != 1 {
		t.Fatalf("deleted metrics = %d, want 1", stats.Metrics)
	}
	if n := count(t, d, `SELECT COUNT(*) FROM metric_series WHERE host_id = 'h1'`); n != 0 {
		t.Fatalf("series left for the deleted host: %d", n)
	}
	if n := count(t, d, `SELECT COUNT(*) FROM metrics`); n != 1 {
		t.Fatalf("other host's points = %d, want 1", n)
	}
}
