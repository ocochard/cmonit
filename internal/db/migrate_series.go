package db

import (
	"database/sql"
	"fmt"
	"log"
	"strings"
	"time"
)

// storedTimeLayouts are the formats found in the old metrics.collected_at
// column. The driver wrote time.Time values as text including the zone of the
// host that ran cmonit, so offsets differ between installations.
var storedTimeLayouts = []string{
	"2006-01-02 15:04:05 -0700 MST", // fractional seconds are accepted when parsing
	"2006-01-02 15:04:05 -0700 -0700",
	"2006-01-02 15:04:05-07:00",
	"2006-01-02T15:04:05Z07:00",
	"2006-01-02 15:04:05",
}

func parseStoredTime(v any) (time.Time, bool) {
	var s string
	switch x := v.(type) {
	case time.Time:
		return x, true
	case string:
		s = x
	case []byte:
		s = string(x)
	default:
		return time.Time{}, false
	}
	if i := strings.Index(s, " m="); i >= 0 { // monotonic clock suffix of time.Now()
		s = s[:i]
	}
	for _, layout := range storedTimeLayouts {
		if t, err := time.Parse(layout, s); err == nil {
			return t, true
		}
	}
	return time.Time{}, false
}

type seriesKey struct{ host, service, mtype, name string }

// migrateMetricsToSeries converts the v16 metrics table (one text row per
// point: host, service, type, name, value, text timestamp) into metric_series
// plus the clustered metrics(series_id, ts, value) table, in one transaction.
// A crash leaves the old table untouched. Afterwards the old pages are
// returned to the filesystem with VACUUM when most of the file is free.
func migrateMetricsToSeries(db *sql.DB) error {
	var oldShape int
	err := db.QueryRow(`SELECT COUNT(*) FROM pragma_table_info('metrics') WHERE name = 'host_id'`).Scan(&oldShape)
	if err != nil {
		return fmt.Errorf("inspecting metrics: %w", err)
	}
	if oldShape == 0 {
		// Already the new shape, or no table yet: make sure both exist.
		if _, err := db.Exec(createMetricSeriesTable); err != nil {
			return err
		}
		_, err := db.Exec(createMetricsTable)
		return err
	}

	start := time.Now()
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	for _, stmt := range []string{
		`ALTER TABLE metrics RENAME TO metrics_old`,
		createMetricSeriesTable,
		createMetricsTable,
	} {
		if _, err := tx.Exec(stmt); err != nil {
			return err
		}
	}

	insSeries, err := tx.Prepare(`INSERT OR IGNORE INTO metric_series (host_id, service_name, metric_type, metric_name) VALUES (?, ?, ?, ?)`)
	if err != nil {
		return err
	}
	defer insSeries.Close()
	getSeries, err := tx.Prepare(`SELECT id FROM metric_series WHERE host_id = ? AND service_name = ? AND metric_type = ? AND metric_name = ?`)
	if err != nil {
		return err
	}
	defer getSeries.Close()
	insPoint, err := tx.Prepare(`INSERT OR REPLACE INTO metrics (series_id, ts, value) VALUES (?, ?, ?)`)
	if err != nil {
		return err
	}
	defer insPoint.Close()

	rows, err := tx.Query(`SELECT host_id, service_name, metric_type, metric_name, value, collected_at FROM metrics_old`)
	if err != nil {
		return err
	}
	defer rows.Close()

	ids := map[seriesKey]int64{}
	var copied, skipped int64
	for rows.Next() {
		var k seriesKey
		var value float64
		var raw any
		if err := rows.Scan(&k.host, &k.service, &k.mtype, &k.name, &value, &raw); err != nil {
			return err
		}
		t, ok := parseStoredTime(raw)
		if !ok {
			skipped++
			continue
		}
		id, known := ids[k]
		if !known {
			if _, err := insSeries.Exec(k.host, k.service, k.mtype, k.name); err != nil {
				return err
			}
			if err := getSeries.QueryRow(k.host, k.service, k.mtype, k.name).Scan(&id); err != nil {
				return err
			}
			ids[k] = id
		}
		if _, err := insPoint.Exec(id, t.Unix(), value); err != nil {
			return err
		}
		copied++
		if copied%1000000 == 0 {
			log.Printf("[INFO] Metrics migration: %d rows copied (%s)", copied, time.Since(start).Round(time.Second))
		}
	}
	if err := rows.Err(); err != nil {
		return err
	}
	rows.Close()

	var kept int64
	if err := tx.QueryRow(`SELECT COUNT(*) FROM metrics`).Scan(&kept); err != nil {
		return err
	}
	if _, err := tx.Exec(`DROP TABLE metrics_old`); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	log.Printf("[INFO] Metrics migration: %d rows read, %d kept in %d series (%d same-second duplicates merged), %d skipped with unreadable timestamps, in %s",
		copied, kept, len(ids), copied-kept, skipped, time.Since(start).Round(time.Second))

	reclaimSpace(db)
	return nil
}

// reclaimSpace truncates the WAL and runs VACUUM when more than a quarter of
// the database file is free pages, so the file shrinks after a big migration.
func reclaimSpace(db *sql.DB) {
	if _, err := db.Exec(`PRAGMA wal_checkpoint(TRUNCATE)`); err != nil {
		log.Printf("[WARN] wal_checkpoint after migration: %v", err)
	}
	var pages, free int64
	if db.QueryRow(`PRAGMA page_count`).Scan(&pages) != nil || db.QueryRow(`PRAGMA freelist_count`).Scan(&free) != nil {
		return
	}
	if pages == 0 || free*4 < pages {
		return
	}
	log.Printf("[INFO] Reclaiming %d of %d pages with VACUUM (this can take a minute)", free, pages)
	t := time.Now()
	if _, err := db.Exec(`VACUUM`); err != nil {
		log.Printf("[WARN] VACUUM after migration failed (the database is fine, only larger than needed): %v", err)
		return
	}
	db.Exec(`PRAGMA wal_checkpoint(TRUNCATE)`)
	log.Printf("[INFO] VACUUM finished in %s", time.Since(t).Round(time.Second))
}
