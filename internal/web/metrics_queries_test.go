package web

import (
	"database/sql"
	"path/filepath"
	"testing"
	"time"

	cdb "github.com/ocochard/cmonit/internal/db"
)

func setupMetricsDB(t *testing.T) *sql.DB {
	t.Helper()
	d, err := cdb.InitDB(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })
	SetDB(d)
	if _, err := d.Exec(`INSERT INTO hosts (id, hostname, last_seen, poll_interval) VALUES ('h1', 'web01', ?, 30)`, time.Now()); err != nil {
		t.Fatal(err)
	}
	return d
}

func storeMetric(t *testing.T, d *sql.DB, service, typ, name string, v float64, at time.Time) {
	t.Helper()
	if err := cdb.StoreMetric(d, "h1", service, typ, name, v, at); err != nil {
		t.Fatal(err)
	}
}

// getSystemMetricsForService reads latest_metrics and must show only the
// newest set: a type that stopped being reported must not linger.
func TestSystemMetricsShowOnlyNewestSet(t *testing.T) {
	d := setupMetricsDB(t)
	t0 := time.Now().Add(-time.Hour)
	t1 := time.Now()

	storeMetric(t, d, "web01", "swap", "percent", 5, t0) // stale: no longer reported
	storeMetric(t, d, "web01", "load", "avg01", 1.5, t0)
	storeMetric(t, d, "web01", "load", "avg01", 2.5, t1)
	storeMetric(t, d, "web01", "memory", "percent", 40, t1)
	storeMetric(t, d, "other", "memory", "percent", 99, t1) // other service

	sm, err := getSystemMetricsForService("h1", "web01")
	if err != nil {
		t.Fatal(err)
	}
	if sm.Load1Min != 2.5 || sm.MemoryPercent != 40 {
		t.Fatalf("load=%v memory=%v, want 2.5 and 40", sm.Load1Min, sm.MemoryPercent)
	}
	if sm.SwapPercent != 0 {
		t.Fatalf("stale swap value shown: %v", sm.SwapPercent)
	}
}

func TestLatestCPUAndMemoryPercentFromLatestMetrics(t *testing.T) {
	d := setupMetricsDB(t)
	storeMetric(t, d, "web01", "system_cpu", "percent", 10, time.Now().Add(-time.Minute))
	storeMetric(t, d, "web01", "system_cpu", "percent", 33, time.Now())
	storeMetric(t, d, "web01", "memory", "percent", 61, time.Now())

	if got := getLatestSystemCPUPercent("h1"); got != 33 {
		t.Fatalf("cpu = %d, want 33", got)
	}
	if got := getLatestSystemMemoryPercent("h1"); got != 61 {
		t.Fatalf("memory = %d, want 61", got)
	}
	if got := getLatestSystemCPUPercent("unknown"); got != 0 {
		t.Fatalf("cpu for unknown host = %d, want 0", got)
	}
}

// Rows now come back ordered by time only; each series must still be
// chronological and contain only points inside the window.
func TestGraphSeriesAreChronologicalWithinWindow(t *testing.T) {
	d := setupMetricsDB(t)
	base := time.Now().Add(-time.Hour).Truncate(time.Second)
	for i := 0; i < 5; i++ {
		at := base.Add(time.Duration(i) * time.Minute)
		storeMetric(t, d, "web01", "cpu", "user", float64(i), at)
		storeMetric(t, d, "web01", "memory", "percent", float64(10+i), at)
	}
	storeMetric(t, d, "web01", "cpu", "user", 99, base.Add(-time.Hour)) // outside the window

	series, err := getMetricsForService("h1", "web01", base.Add(-time.Second), base.Add(10*time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if len(series) != 2 {
		t.Fatalf("series = %d, want 2", len(series))
	}
	for _, s := range series {
		if len(s.Values) != 5 {
			t.Fatalf("%s/%s has %d points, want 5", s.Type, s.Name, len(s.Values))
		}
		for i := 1; i < len(s.Timestamps); i++ {
			if s.Timestamps[i] < s.Timestamps[i-1] {
				t.Fatalf("%s/%s not chronological: %v", s.Type, s.Name, s.Timestamps)
			}
		}
		if s.Name == "user" && (s.Values[0] != 0 || s.Values[4] != 4) {
			t.Fatalf("cpu values = %v, want 0..4", s.Values)
		}
	}
}

// The per-series graph query must be a primary-key range scan, already in
// time order, with no sort.
func TestGraphQueryPlanIsPrimaryKeyRangeWithoutSort(t *testing.T) {
	d := setupMetricsDB(t)
	rows, err := d.Query(`EXPLAIN QUERY PLAN
		SELECT ts, value FROM metrics WHERE series_id = ? AND ts BETWEEN ? AND ? ORDER BY ts`, 1, 0, 1)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var plan string
	for rows.Next() {
		var id, parent, notused int
		var detail string
		if err := rows.Scan(&id, &parent, &notused, &detail); err != nil {
			t.Fatal(err)
		}
		plan += detail + "\n"
	}
	if !contains(plan, "USING PRIMARY KEY (series_id=? AND ts>? AND ts<?)") {
		t.Fatalf("plan is not a primary key range scan:\n%s", plan)
	}
	if contains(plan, "TEMP B-TREE") {
		t.Fatalf("plan sorts:\n%s", plan)
	}
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
