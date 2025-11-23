// Package db - storage.go contains functions for storing Monit data in the database.
//
// This file provides functions to:
// - Store host information
// - Store service status
// - Store time-series metrics
// - Store events
package db

import (
	"database/sql" // SQL database interface
	"fmt"          // Formatted I/O
	"log"          // Logging
	"time"         // Time operations

	"github.com/ocochard/cmonit/internal/parser" // Our XML parser
)

// StoreHost saves or updates a host record in the database.
//
// This function is called every time we receive status data from a Monit agent.
// If the host already exists (matched by ID), it updates the record.
// If it's a new host, it creates a new record.
//
// Parameters:
//   - db: Database connection (from InitDB)
//   - server: Server information from parsed XML
//
// Returns:
//   - error: nil if successful, error describing problem if failed
//
// How it works:
// 1. Use INSERT OR REPLACE to upsert the host
// 2. Update last_seen to current time
// 3. Preserve created_at for existing hosts
//
// Thread-safety: Safe to call from multiple goroutines (database/sql handles locking)
func StoreHost(db *sql.DB, server *parser.Server) error {
	// Generate an ID if Monit doesn't provide one
	//
	// Monit only sends an <id> field if "set idfile" is configured.
	// If no ID is provided, we generate one using hostname + incarnation.
	// This ensures each Monit instance gets a unique, stable ID.
	//
	// Why use hostname + incarnation?
	// - hostname: identifies the machine
	// - incarnation: timestamp when Monit started
	// - Together they create a unique ID that stays same across status updates
	//   but changes if Monit restarts or hostname changes
	hostID := server.ID
	if hostID == "" {
		// Generate ID from hostname (use incarnation as suffix to ensure uniqueness)
		// Example: "webserver-01-1763842004"
		hostID = fmt.Sprintf("%s-%d", server.LocalHostname, server.Incarnation)
		log.Printf("[INFO] Generated host ID: %s (no idfile configured in Monit)", hostID)
	}

	// SQL query to insert or update the host record
	//
	// INSERT OR REPLACE is SQLite's "upsert" operation:
	// - If a row with this ID exists: replace it (update)
	// - If no row with this ID exists: insert new row
	//
	// The COALESCE trick preserves created_at:
	// - If host exists: use its existing created_at (from subquery)
	// - If host is new: use the current timestamp (?)
	//
	// Why preserve created_at?
	// - It tells us when we first discovered this host
	// - last_seen tells us when we last heard from it
	// - Together they show: "Known since X, last contact Y"
	const query = `
		INSERT OR REPLACE INTO hosts (
			id,
			hostname,
			incarnation,
			version,
			http_address,
			http_port,
			http_ssl,
			http_username,
			http_password,
			last_seen,
			created_at
		) VALUES (
			?,
			?,
			?,
			?,
			?,
			?,
			?,
			?,
			?,
			?,
			COALESCE(
				(SELECT created_at FROM hosts WHERE id = ?),
				?
			)
		)
	`

	// Get the current time
	// time.Now() returns the current moment as a time.Time
	// We'll use this for last_seen and created_at
	now := time.Now()

	// Execute the SQL query
	//
	// db.Exec() runs a query that doesn't return rows (INSERT, UPDATE, DELETE)
	//
	// The ? placeholders are replaced with the values in order:
	//   1st ? = hostID (generated or from server.ID)
	//   2nd ? = server.LocalHostname
	//   3rd ? = server.Incarnation
	//   4th ? = server.Version
	//   5th ? = server.HTTPD.Address (Monit HTTP server address)
	//   6th ? = server.HTTPD.Port (Monit HTTP server port)
	//   7th ? = server.HTTPD.SSL (SSL enabled flag)
	//   8th ? = server.Credentials.Username (HTTP auth username)
	//   9th ? = server.Credentials.Password (HTTP auth password)
	//   10th ? = now (last_seen)
	//   11th ? = hostID (for the COALESCE subquery)
	//   12th ? = now (created_at if new host)
	//
	// Why use placeholders instead of string formatting?
	// - Prevents SQL injection attacks
	// - Handles special characters correctly
	// - Easier to read and maintain
	//
	// Returns:
	//   - sql.Result: contains info like rows affected, last insert ID
	//   - error: nil if successful, error if failed
	//
	// We use _ to ignore the Result (we don't need it)
	_, err := db.Exec(
		query,
		hostID,
		server.LocalHostname,
		server.Incarnation,
		server.Version,
		server.HTTPD.Address,
		server.HTTPD.Port,
		server.HTTPD.SSL,
		server.Credentials.Username,
		server.Credentials.Password,
		now,
		hostID,
		now,
	)

	// Check if the query failed
	if err != nil {
		// Log the error with context
		// log.Printf() writes to stderr with timestamp
		// %s formats a string, %v formats any value
		log.Printf("[ERROR] Failed to store host %s: %v", server.LocalHostname, err)

		// Return the error to the caller
		// fmt.Errorf() creates a formatted error message
		// %w wraps the original error (preserves error chain)
		return fmt.Errorf("failed to store host: %w", err)
	}

	// Success!
	// Log for debugging (helps track what's happening)
	log.Printf("[DEBUG] Stored host: %s (ID: %s)", server.LocalHostname, hostID)

	// Return nil (no error)
	return nil
}

// StoreService saves or updates a service record in the database.
//
// This function stores the current status of a monitored service.
// Services are things like: system stats, processes, files, etc.
//
// Parameters:
//   - db: Database connection
//   - hostID: The ID of the host this service belongs to
//   - service: Service information from parsed XML
//
// Returns:
//   - error: nil if successful, error if failed
//
// Note: This only stores the service status, not the metrics.
// Metrics (CPU%, memory%, etc.) are stored separately in StoreMetrics.
func StoreService(db *sql.DB, hostID string, service *parser.Service) error {
	// SQL query to insert or update the service record
	//
	// INSERT OR REPLACE:
	// - If (host_id, name) exists: update the row
	// - If (host_id, name) doesn't exist: insert new row
	//
	// Why UNIQUE(host_id, name)?
	// - Each host can have a service named "nginx"
	// - Different hosts can have different "nginx" services
	// - Same host can't have two services with the same name
	const query = `
		INSERT OR REPLACE INTO services (
			host_id,
			name,
			type,
			status,
			monitor,
			collected_at,
			last_seen
		) VALUES (?, ?, ?, ?, ?, ?, ?)
	`

	// Get the collection timestamp from the service
	// service.GetCollectedTime() converts the Unix timestamp to time.Time
	// This is when Monit collected the data (not when we received it)
	collectedAt := service.GetCollectedTime()

	// Current time for last_seen
	now := time.Now()

	// Execute the query
	_, err := db.Exec(
		query,
		hostID,              // Which host this service belongs to
		service.Name,        // Service name (e.g., "nginx", "system", "sshd")
		service.Type,        // Service type (0-8, see parser.Service docs)
		service.Status,      // Current status (0=OK, 1=failed, etc.)
		service.Monitor,     // Monitoring state (0=not monitored, 1=monitored, 2=init)
		collectedAt,         // When Monit collected this data
		now,                 // When we received/processed it
	)

	if err != nil {
		log.Printf("[ERROR] Failed to store service %s/%s: %v", hostID, service.Name, err)
		return fmt.Errorf("failed to store service: %w", err)
	}

	log.Printf("[DEBUG] Stored service: %s/%s (type %d, status %d)",
		hostID, service.Name, service.Type, service.Status)

	return nil
}

// StoreMetric stores a single metric data point in the time-series table.
//
// Metrics are numeric values that change over time: CPU%, memory%, load, etc.
// We store these in a separate table so we can graph them later.
//
// Parameters:
//   - db: Database connection
//   - hostID: Which host this metric is from
//   - serviceName: Which service this metric is for
//   - metricType: Category (e.g., "cpu", "memory", "load")
//   - metricName: Specific metric (e.g., "user", "system", "percent")
//   - value: The numeric value
//   - collectedAt: When this data point was collected
//
// Returns:
//   - error: nil if successful, error if failed
//
// Example usage:
//   StoreMetric(db, "host123", "system", "cpu", "user", 25.5, time.Now())
//   StoreMetric(db, "host123", "system", "memory", "percent", 45.2, time.Now())
func StoreMetric(db *sql.DB, hostID, serviceName, metricType, metricName string, value float64, collectedAt time.Time) error {
	// SQL query to insert a metric data point
	//
	// Note: We use INSERT (not INSERT OR REPLACE) because:
	// - Each metric is a new data point in time
	// - We want to keep all historical values
	// - Time-series data is append-only (we don't update old values)
	const query = `
		INSERT INTO metrics (
			host_id,
			service_name,
			metric_type,
			metric_name,
			value,
			collected_at
		) VALUES (?, ?, ?, ?, ?, ?)
	`

	// Execute the query
	_, err := db.Exec(
		query,
		hostID,
		serviceName,
		metricType,
		metricName,
		value,
		collectedAt,
	)

	if err != nil {
		// Don't log every metric error (would be very noisy)
		// Just return the error
		return fmt.Errorf("failed to store metric: %w", err)
	}

	// Success - don't log individual metrics (too verbose)
	// We'll log summary statistics instead
	return nil
}

// StoreSystemMetrics extracts and stores all metrics from a system service.
//
// System services (type 5) contain lots of metrics:
// - Load average (3 values)
// - CPU usage (user, system, nice, wait)
// - Memory usage (percent, kilobytes)
// - Swap usage (percent, kilobytes)
//
// This function extracts all these metrics and stores them individually
// so we can graph them later.
//
// Parameters:
//   - db: Database connection
//   - hostID: Which host these metrics are from
//   - service: The system service containing the metrics
//
// Returns:
//   - error: nil if all metrics stored successfully, error if any failed
func StoreSystemMetrics(db *sql.DB, hostID string, service *parser.Service) error {
	// Check if this is actually a system service
	if service.Type != 5 {
		// Not a system service, nothing to do
		return nil
	}

	// Check if system metrics are present
	if service.System == nil {
		// No system metrics in this service (shouldn't happen for type 5, but be safe)
		return nil
	}

	// Get the collection timestamp
	// This is when Monit measured these values
	collectedAt := service.GetCollectedTime()

	// Store load average metrics
	//
	// Load average is the average number of processes in the run queue.
	// Three values represent different time windows: 1min, 5min, 15min.
	//
	// Why store all three?
	// - Shows trends: sudden spike vs. sustained load
	// - 1min: very responsive to changes
	// - 5min: recent trend
	// - 15min: longer-term trend
	err := StoreMetric(db, hostID, service.Name, "load", "avg01", service.System.Load.Avg01, collectedAt)
	if err != nil {
		return err
	}

	err = StoreMetric(db, hostID, service.Name, "load", "avg05", service.System.Load.Avg05, collectedAt)
	if err != nil {
		return err
	}

	err = StoreMetric(db, hostID, service.Name, "load", "avg15", service.System.Load.Avg15, collectedAt)
	if err != nil {
		return err
	}

	// Store CPU usage metrics
	//
	// CPU time is divided into categories:
	// - user: time running user processes (applications)
	// - system: time running kernel code (OS operations)
	// - nice: time running low-priority processes
	// - wait: time waiting for I/O (disk, network)
	//
	// These percentages add up to 100% (all CPU time)
	err = StoreMetric(db, hostID, service.Name, "cpu", "user", service.System.CPU.User, collectedAt)
	if err != nil {
		return err
	}

	err = StoreMetric(db, hostID, service.Name, "cpu", "system", service.System.CPU.System, collectedAt)
	if err != nil {
		return err
	}

	err = StoreMetric(db, hostID, service.Name, "cpu", "nice", service.System.CPU.Nice, collectedAt)
	if err != nil {
		return err
	}

	err = StoreMetric(db, hostID, service.Name, "cpu", "wait", service.System.CPU.Wait, collectedAt)
	if err != nil {
		return err
	}

	// Store memory usage metrics
	//
	// We store both percentage and absolute values:
	// - percent: easy to understand (45% used)
	// - kilobyte: exact amount (useful for capacity planning)
	err = StoreMetric(db, hostID, service.Name, "memory", "percent", service.System.Memory.Percent, collectedAt)
	if err != nil {
		return err
	}

	err = StoreMetric(db, hostID, service.Name, "memory", "kilobyte", float64(service.System.Memory.Kilobyte), collectedAt)
	if err != nil {
		return err
	}

	// Store swap usage metrics
	//
	// Swap is disk space used as "virtual memory" when RAM is full.
	// High swap usage indicates insufficient RAM.
	err = StoreMetric(db, hostID, service.Name, "swap", "percent", service.System.Swap.Percent, collectedAt)
	if err != nil {
		return err
	}

	err = StoreMetric(db, hostID, service.Name, "swap", "kilobyte", float64(service.System.Swap.Kilobyte), collectedAt)
	if err != nil {
		return err
	}

	// All metrics stored successfully!
	log.Printf("[DEBUG] Stored %d system metrics for %s/%s", 12, hostID, service.Name)
	return nil
}

// StoreProcessMetrics extracts and stores metrics from a process service.
//
// Process services (type 3) contain:
// - Memory usage (percent, kilobytes)
// - CPU usage (percent)
//
// Parameters:
//   - db: Database connection
//   - hostID: Which host this process is on
//   - service: The process service containing the metrics
//
// Returns:
//   - error: nil if successful, error if failed
func StoreProcessMetrics(db *sql.DB, hostID string, service *parser.Service) error {
	// Check if this is a process service
	if service.Type != 3 {
		return nil
	}

	// Get collection timestamp
	collectedAt := service.GetCollectedTime()

	// Store memory metrics (if present)
	//
	// We check if Memory is nil because:
	// - Process might have just started (no stats yet)
	// - Process might have terminated
	// - Stats collection might have failed
	//
	// nil check prevents panic (accessing fields on nil pointer crashes)
	if service.Memory != nil {
		// Store memory percentage
		// How much of total system RAM this process uses
		err := StoreMetric(db, hostID, service.Name, "process_memory", "percent",
			service.Memory.Percent, collectedAt)
		if err != nil {
			return err
		}

		// Store memory in kilobytes
		// Absolute amount of RAM this process uses
		err = StoreMetric(db, hostID, service.Name, "process_memory", "kilobyte",
			float64(service.Memory.Kilobyte), collectedAt)
		if err != nil {
			return err
		}

		// Store total memory (process + children)
		// Useful for parent processes like nginx master -> workers
		err = StoreMetric(db, hostID, service.Name, "process_memory", "total_percent",
			service.Memory.PercentTotal, collectedAt)
		if err != nil {
			return err
		}
	}

	// Store CPU metrics (if present)
	if service.CPU != nil {
		// Store CPU percentage
		// How much CPU time this process is using
		// 100% = using 1 full CPU core
		// 200% = using 2 full CPU cores (multi-threaded)
		err := StoreMetric(db, hostID, service.Name, "process_cpu", "percent",
			service.CPU.Percent, collectedAt)
		if err != nil {
			return err
		}

		// Store total CPU (process + children)
		err = StoreMetric(db, hostID, service.Name, "process_cpu", "total_percent",
			service.CPU.PercentTotal, collectedAt)
		if err != nil {
			return err
		}
	}

	log.Printf("[DEBUG] Stored process metrics for %s/%s", hostID, service.Name)
	return nil
}

// StoreMonitStatus processes a complete Monit status update and stores all data.
//
// This is the main function that:
// 1. Stores the host information
// 2. Stores all services
// 3. Stores all metrics from all services
//
// This function is called from the /collector HTTP handler after parsing the XML.
//
// Parameters:
//   - db: Database connection
//   - status: Complete parsed status from Monit
//
// Returns:
//   - error: nil if everything stored successfully, error if any part failed
func StoreMonitStatus(db *sql.DB, status *parser.MonitStatus) error {
	// Generate host ID (same logic as in StoreHost)
	//
	// We generate the ID here so we can pass it to all storage functions.
	// If Monit provides an ID, use it. Otherwise, generate one from hostname + incarnation.
	hostID := status.Server.ID
	if hostID == "" {
		hostID = fmt.Sprintf("%s-%d", status.Server.LocalHostname, status.Server.Incarnation)
		log.Printf("[INFO] Generated host ID: %s (no idfile configured in Monit)", hostID)
	}

	// Step 1: Store the host information
	//
	// This creates or updates the host record in the hosts table.
	// The host record contains: ID, hostname, version, incarnation, last_seen.
	err := StoreHost(db, &status.Server)
	if err != nil {
		// If we can't store the host, don't bother with services/metrics
		return fmt.Errorf("failed to store host: %w", err)
	}

	// Step 2: Store all services
	//
	// Loop through each service in the status update.
	// For each service:
	// - Store service status in services table
	// - Extract and store metrics in metrics table
	for i := range status.Services {
		service := &status.Services[i]

		// Store service status (use generated hostID)
		err = StoreService(db, hostID, service)
		if err != nil {
			// Log the error but continue with other services
			// We don't want one bad service to break everything
			log.Printf("[WARN] Failed to store service %s: %v", service.Name, err)
			continue
		}

		// Store metrics based on service type
		switch service.Type {
		case 5: // System service
			err = StoreSystemMetrics(db, hostID, service)
			if err != nil {
				log.Printf("[WARN] Failed to store system metrics for %s: %v", service.Name, err)
			}

		case 3: // Process service
			err = StoreProcessMetrics(db, hostID, service)
			if err != nil {
				log.Printf("[WARN] Failed to store process metrics for %s: %v", service.Name, err)
			}

		// TODO: Add handlers for other service types:
		// case 0: // Filesystem
		// case 2: // File
		// case 7: // Program
		// etc.
		}
	}

	// Success!
	log.Printf("[INFO] Stored status for host %s: %d services",
		status.Server.LocalHostname, len(status.Services))

	return nil
}
