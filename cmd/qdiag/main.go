// qdiag times the status-page queries directly against a live cmonit
// database, using the same driver (modernc.org/sqlite) as the running
// server. Use it to find which specific query is slow before changing any
// index or SQL - see docs/troubleshooting.md, "Isolating Slow Dashboard
// Loads".
//
// Opens the database read-only, so it is safe to run against a production
// file while the server is up.
//
//	go build -o qdiag ./cmd/qdiag
//	CGO_ENABLED=0 GOOS=freebsd GOARCH=amd64 go build -o qdiag ./cmd/qdiag  # cross-compile for the deploy target
//	./qdiag /var/run/cmonit/cmonit.db
//	./qdiag -plan /var/run/cmonit/cmonit.db   # also print EXPLAIN QUERY PLAN for each query
package main

import (
	"database/sql"
	"flag"
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

// queries mirrors the status-page queries in internal/web/handlers_status.go.
// Keep in sync if those queries change shape.
var queries = []struct {
	name string
	sql  string
}{
	{"hosts", `SELECT id, hostname, last_seen FROM hosts ORDER BY last_seen DESC`},
	{"services", `SELECT host_id, name, type, status, monitor, pid, cpu_percent, memory_percent, memory_kb, collected_at FROM services ORDER BY host_id, type, name`},
	{"cpu", `
		WITH latest_cpu AS (
			SELECT host_id, MAX(collected_at) AS max_collected
			FROM metrics
			WHERE metric_type = 'cpu'
			GROUP BY host_id
		)
		SELECT m.host_id,
			SUM(CASE WHEN m.metric_name = 'user' THEN m.value ELSE 0 END) +
			SUM(CASE WHEN m.metric_name = 'system' THEN m.value ELSE 0 END) +
			SUM(CASE WHEN m.metric_name = 'nice' THEN m.value ELSE 0 END) +
			SUM(CASE WHEN m.metric_name = 'wait' THEN m.value ELSE 0 END) AS total_cpu
		FROM metrics m
		JOIN latest_cpu lc ON m.host_id = lc.host_id AND m.collected_at = lc.max_collected
		WHERE m.metric_type = 'cpu'
		GROUP BY m.host_id`},
	{"mem", `
		WITH latest_mem AS (
			SELECT host_id, MAX(collected_at) AS max_collected
			FROM metrics
			WHERE metric_type = 'memory' AND metric_name = 'percent'
			GROUP BY host_id
		)
		SELECT m.host_id, m.value
		FROM metrics m
		JOIN latest_mem lm ON m.host_id = lm.host_id AND m.collected_at = lm.max_collected
		WHERE m.metric_type = 'memory' AND m.metric_name = 'percent'`},
	{"events", `SELECT host_id, COUNT(*) FROM events GROUP BY host_id`},
	{"hostgroups", `SELECT hhg.host_id, hg.name FROM host_hostgroups hhg JOIN hostgroups hg ON hhg.hostgroup_id = hg.id`},
	{"allhostgroups", `SELECT name FROM hostgroups ORDER BY name`},
}

func main() {
	showPlan := flag.Bool("plan", false, "also print EXPLAIN QUERY PLAN for each query")
	flag.Parse()

	if flag.NArg() != 1 {
		fmt.Fprintf(os.Stderr, "usage: %s [-plan] <path-to-cmonit.db>\n", os.Args[0])
		os.Exit(1)
	}

	dsn := fmt.Sprintf("file:%s?mode=ro", flag.Arg(0))
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		log.Fatalf("open: %v", err)
	}
	defer db.Close()

	for _, q := range queries {
		start := time.Now()
		rows, err := db.Query(q.sql)
		if err != nil {
			fmt.Printf("%-14s ERROR: %v\n", q.name, err)
			continue
		}
		n := 0
		for rows.Next() {
			n++
		}
		err = rows.Err()
		rows.Close()
		elapsed := time.Since(start)

		if err != nil {
			fmt.Printf("%-14s ERROR: %v\n", q.name, err)
			continue
		}
		fmt.Printf("%-14s %10s  (%d rows)\n", q.name, elapsed.Round(time.Millisecond), n)

		if *showPlan {
			printPlan(db, q.name, q.sql)
		}
	}
}

func printPlan(db *sql.DB, name, query string) {
	rows, err := db.Query("EXPLAIN QUERY PLAN " + query)
	if err != nil {
		fmt.Printf("  plan error: %v\n", err)
		return
	}
	defer rows.Close()

	for rows.Next() {
		var id, parent, notused int
		var detail string
		if err := rows.Scan(&id, &parent, &notused, &detail); err != nil {
			fmt.Printf("  plan scan error: %v\n", err)
			return
		}
		flag := "OK"
		if strings.Contains(detail, "SEARCH") && !strings.Contains(detail, "COVERING INDEX") && !strings.Contains(detail, "INTEGER PRIMARY KEY") {
			flag = "NON-COVERING - may need an index"
		} else if strings.Contains(detail, "SCAN") {
			flag = "FULL SCAN - check for a missing index"
		}
		fmt.Printf("  %s  [%s]\n", detail, flag)
	}
	_ = name
}
