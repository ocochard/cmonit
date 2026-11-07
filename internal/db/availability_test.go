package db

import (
	"database/sql"
	"testing"
	"time"

	"github.com/ocochard/cmonit/internal/parser"
)

// ingest runs the real collector write path for a host.
func ingest(t *testing.T, d *sql.DB, id, hostname string) {
	t.Helper()
	st := &parser.MonitStatus{
		Server: parser.Server{ID: id, LocalHostname: hostname, Incarnation: 1, Uptime: 100, Poll: 30,
			HTTPD: parser.HTTPDInfo{Address: "127.0.0.1", Port: 2812}},
		Platform: parser.Platform{Name: "FreeBSD", Release: "16.0", Version: "x", Machine: "amd64", CPU: 4, Memory: 1 << 20, Swap: 1 << 20},
		Services: []parser.Service{
			{Name: hostname, Type: 5, Status: 0, Monitor: 1, CollectedSec: time.Now().Unix()},
		},
	}
	if err := StoreMonitStatus(d, st); err != nil {
		t.Fatal(err)
	}
}

// ageHost makes a host look last seen `ago` ago, formatted by SQLite itself so
// the helper does not depend on the format cmonit writes.
func ageHost(t *testing.T, d *sql.DB, id, ago string) {
	t.Helper()
	if _, err := d.Exec(`UPDATE hosts SET last_seen = strftime('%Y-%m-%d %H:%M:%S', 'now', ?) WHERE id = ?`, ago, id); err != nil {
		t.Fatal(err)
	}
}

func withLocalZone(t *testing.T, name string, offsetSeconds int) {
	t.Helper()
	old := time.Local
	time.Local = time.FixedZone(name, offsetSeconds)
	t.Cleanup(func() { time.Local = old })
}

// Several queries compute with strftime('%s', last_seen). The text cmonit
// writes must be something SQLite can parse, whatever the server's timezone.
func TestIngestWritesLastSeenThatSQLiteCanParse(t *testing.T) {
	for _, z := range []struct {
		name   string
		offset int
	}{{"UTC", 0}, {"CET", 3600}, {"PST", -8 * 3600}} {
		t.Run(z.name, func(t *testing.T) {
			withLocalZone(t, z.name, z.offset)
			d := openFresh(t)
			ingest(t, d, "h9", "web09")

			var epoch sql.NullInt64
			if err := d.QueryRow(`SELECT CAST(strftime('%s', last_seen) AS INTEGER) FROM hosts WHERE id = 'h9'`).Scan(&epoch); err != nil {
				t.Fatal(err)
			}
			if !epoch.Valid {
				var raw string
				d.QueryRow(`SELECT last_seen FROM hosts WHERE id = 'h9'`).Scan(&raw)
				t.Fatalf("strftime cannot parse stored last_seen %q", raw)
			}
			if diff := time.Now().Unix() - epoch.Int64; diff < -5 || diff > 5 {
				t.Fatalf("last_seen epoch is %d s away from now (timezone handling)", diff)
			}
		})
	}
}

func TestIngestRecordsAvailability(t *testing.T) {
	d := openFresh(t)
	ingest(t, d, "h9", "web09")

	var status string
	err := d.QueryRow(`SELECT status FROM host_availability WHERE host_id = 'h9' ORDER BY id DESC LIMIT 1`).Scan(&status)
	if err != nil {
		t.Fatalf("no availability row after ingest: %v", err)
	}
	if status != "green" {
		t.Fatalf("status = %q, want green", status)
	}
}

func TestAvailabilityJobRecordsAllHostsAndMarksSilentOnesRed(t *testing.T) {
	d := openFresh(t)
	ingest(t, d, "h9", "web09")
	ingest(t, d, "h8", "web08")
	ageHost(t, d, "h8", "-2 hours")

	before := count(t, d, `SELECT COUNT(*) FROM host_availability`)
	if err := RecordAvailabilityForAllHosts(d); err != nil {
		t.Fatal(err)
	}
	if got := count(t, d, `SELECT COUNT(*) FROM host_availability`) - before; got < 2 {
		t.Fatalf("job recorded %d rows, want one per host (h1 from openFresh has poll 30 too)", got)
	}
	var status string
	d.QueryRow(`SELECT status FROM host_availability WHERE host_id = 'h8' ORDER BY id DESC LIMIT 1`).Scan(&status)
	if status != "red" {
		t.Fatalf("silent host status = %q, want red", status)
	}
	d.QueryRow(`SELECT status FROM host_availability WHERE host_id = 'h9' ORDER BY id DESC LIMIT 1`).Scan(&status)
	if status != "green" {
		t.Fatalf("live host status = %q, want green", status)
	}
}

func TestDeleteHostRequiresOneHourOfflineAndThenWorks(t *testing.T) {
	d := openFresh(t)
	ingest(t, d, "h9", "web09")

	if _, err := DeleteHost(d, "h9"); err == nil {
		t.Fatal("deleting a host seen seconds ago must fail")
	}
	ageHost(t, d, "h9", "-2 hours")
	if _, err := DeleteHost(d, "h9"); err != nil {
		t.Fatalf("deleting a host offline for 2 hours failed: %v", err)
	}
	if n := count(t, d, `SELECT COUNT(*) FROM hosts WHERE id = 'h9'`); n != 0 {
		t.Fatal("host still present")
	}
}

func TestPruneRemovesOldAvailabilityRows(t *testing.T) {
	d := openFresh(t)
	now := time.Now().Unix()
	old := now - 40*86400
	for _, ts := range []int64{old, now} {
		if _, err := d.Exec(`INSERT INTO host_availability (host_id, timestamp, status, last_seen, poll_interval) VALUES ('h1', ?, 'green', ?, 30)`, ts, ts); err != nil {
			t.Fatal(err)
		}
	}
	if err := PruneOldData(d, 30); err != nil {
		t.Fatal(err)
	}
	if n := count(t, d, `SELECT COUNT(*) FROM host_availability`); n != 1 {
		t.Fatalf("availability rows after prune = %d, want 1", n)
	}
}

// Rows written by earlier versions (Go's time.Time text, with a monotonic
// suffix and the writer's zone) must be rewritten into a format SQLite parses.
func TestMigrateV17NormalizesLegacyLastSeen(t *testing.T) {
	path := t.TempDir() + "/m.db"
	d, err := InitDB(path)
	if err != nil {
		t.Fatal(err)
	}
	legacy := map[string]string{
		"utc": "2026-11-07 15:48:25.003095336 +0000 UTC m=+56603.948056310",
		"cet": "2026-11-07 16:48:25.5 +0100 CET m=+12.3",
		"old": "2026-11-07 15:48:25 +0000 UTC",
	}
	for id, text := range legacy {
		if _, err := d.Exec(`INSERT INTO hosts (id, hostname, last_seen, poll_interval) VALUES (?, ?, ?, 30)`, id, id, text); err != nil {
			t.Fatal(err)
		}
	}
	rollBackVersionTo(t, d, 17)
	d.Close()

	d, err = InitDB(path)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()

	want := time.Date(2026, 11, 7, 15, 48, 25, 0, time.UTC).Unix()
	for id := range legacy {
		var epoch sql.NullInt64
		d.QueryRow(`SELECT CAST(strftime('%s', last_seen) AS INTEGER) FROM hosts WHERE id = ?`, id).Scan(&epoch)
		if !epoch.Valid || epoch.Int64 != want {
			t.Fatalf("%s: epoch = %v, want %d", id, epoch, want)
		}
	}
}
