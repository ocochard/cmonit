// Package db provides all database operations for cmonit.
//
// This package handles:
// - Database initialization and schema creation
// - Storing host information from Monit agents
// - Storing service status data
// - Storing time-series metrics
// - Storing events (state changes)
//
// Database Technology: SQLite
// - Embedded database (no separate server needed)
// - File-based (stored in cmonit.db)
// - ACID compliant (Atomicity, Consistency, Isolation, Durability)
// - Perfect for small-to-medium deployments (<100 hosts)
//
// Thread Safety:
// - The database/sql package provides connection pooling
// - Safe to use from multiple goroutines (concurrent HTTP requests)
package db

import (
	// Standard library imports

	"database/sql" // SQL database interface (works with any SQL database)
	"fmt"          // Formatted I/O
	"log"          // Logging

	// Third-party imports
	// The underscore (_) means "import for side effects only"
	// We don't use the sqlite3 package directly, but importing it
	// registers the SQLite driver with database/sql
	//
	// This is Go's way of plugin-style architecture:
	// - database/sql provides the interface
	// - mattn/go-sqlite3 provides the SQLite implementation
	// - The registration happens automatically when imported
	_ "github.com/mattn/go-sqlite3"
)

// currentSchemaVersion is the current database schema version.
// Increment this when making schema changes that require migration.
const currentSchemaVersion = 4

// SQL schema for the cmonit database
//
// This schema is designed to:
// 1. Store current status of all hosts and services
// 2. Keep historical metrics for graphing
// 3. Record events (state changes)
//
// Why these tables?
// - schema_version: Tracks database schema version for safe migrations
// - hosts: One row per Monit agent
// - services: One row per monitored service (per host)
// - metrics: Time-series data for graphing (many rows per service)
// - events: State changes (service failed, recovered, etc.)
const (
	// createSchemaVersionTable creates the schema_version table
	//
	// This table stores the current database schema version.
	// It prevents issues when updating the schema in future versions.
	//
	// Why schema versioning?
	// - SQLite's "IF NOT EXISTS" doesn't modify existing tables
	// - Adding/removing columns requires ALTER TABLE or recreation
	// - Version tracking allows safe, automated migrations
	//
	// Columns:
	//   - version: Integer version number (e.g., 1, 2, 3...)
	//   - applied_at: When this schema version was applied
	//
	// This table should have exactly one row at all times.
	createSchemaVersionTable = `
	CREATE TABLE IF NOT EXISTS schema_version (
		version INTEGER PRIMARY KEY,
		applied_at DATETIME DEFAULT CURRENT_TIMESTAMP
	);`
	// createHostsTable creates the hosts table
	//
	// This table stores information about each monitored server/machine.
	// Each server runs a Monit agent that reports to cmonit.
	//
	// Columns:
	//   - id: Unique identifier from Monit (stays same even if hostname changes)
	//   - hostname: Human-readable server name (e.g., "web-server-01")
	//   - incarnation: Unix timestamp when Monit was started (changes on restart)
	//   - version: Monit version string (e.g., "5.35.2")
	//   - http_address: Monit HTTP server address (for remote control)
	//   - http_port: Monit HTTP server port (usually 2812)
	//   - http_ssl: Whether Monit uses HTTPS (0=no, 1=yes)
	//   - http_username: Username for Monit HTTP authentication
	//   - http_password: Password for Monit HTTP authentication
	//   - os_name: Operating system name (FreeBSD, Linux, Darwin, etc.)
	//   - os_release: OS version/release
	//   - os_version: Full OS version string with kernel info
	//   - machine: CPU architecture (amd64, arm64, i386)
	//   - cpu_count: Number of CPU cores
	//   - total_memory: Total RAM in bytes
	//   - total_swap: Total swap space in bytes
	//   - system_uptime: System uptime in seconds
	//   - boottime: Unix timestamp of last boot
	//   - monit_uptime: Monit daemon uptime in seconds (for restart detection)
	//   - last_seen: When we last received data from this host
	//   - created_at: When we first saw this host
	//
	// PRIMARY KEY: id must be unique (enforced by SQLite)
	// UNIQUE: hostname must be unique (one entry per server)
	//
	// DEFAULT CURRENT_TIMESTAMP: Automatically sets time fields to "now"
	//
	// The http_* fields allow cmonit to control Monit agents remotely.
	// These are automatically extracted from the Monit XML status.
	createHostsTable = `
	CREATE TABLE IF NOT EXISTS hosts (
		id TEXT PRIMARY KEY,
		hostname TEXT NOT NULL,
		incarnation INTEGER,
		version TEXT,
		http_address TEXT,
		http_port INTEGER,
		http_ssl INTEGER DEFAULT 0,
		http_username TEXT,
		http_password TEXT,
		os_name TEXT,
		os_release TEXT,
		os_version TEXT,
		machine TEXT,
		cpu_count INTEGER,
		total_memory INTEGER,
		total_swap INTEGER,
		system_uptime INTEGER,
		boottime INTEGER,
		monit_uptime INTEGER,
		last_seen DATETIME DEFAULT CURRENT_TIMESTAMP,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		UNIQUE(hostname)
	);`

	// createServicesTable creates the services table
	//
	// This table stores information about each monitored service.
	// Services are things like: system stats, processes, filesystems, etc.
	//
	// Columns:
	//   - id: Auto-incrementing integer (SQLite generates unique IDs)
	//   - host_id: Which host this service belongs to (foreign key to hosts.id)
	//   - name: Service name (e.g., "nginx", "system", "disk-root")
	//   - type: Service type integer from Monit (0=filesystem, 1=directory, 2=file,
	//           3=process, 4=host, 5=system, 6=fifo, 7=program, 8=net)
	//   - status: Current status (0=running/ok, 1=failed, etc.)
	//   - monitor: Monitoring mode (0=not monitored, 1=monitored, 2=init)
	//   - pid: Process ID (for process services)
	//   - cpu_percent: CPU usage percentage (for process services)
	//   - memory_percent: Memory usage percentage (for process services)
	//   - memory_kb: Memory usage in kilobytes (for process services)
	//   - collected_at: When this status was collected by Monit
	//   - last_seen: When we last received an update for this service
	//
	// INTEGER PRIMARY KEY AUTOINCREMENT: SQLite automatically generates unique IDs
	// FOREIGN KEY: host_id must reference a valid host.id (referential integrity)
	// UNIQUE(host_id, name): Each service name must be unique per host
	//
	// Why AUTOINCREMENT?
	// - We don't have a natural primary key for services
	// - Each service is identified by (host_id, name) combination
	// - AUTOINCREMENT gives us a simple numeric ID for relationships
	createServicesTable = `
	CREATE TABLE IF NOT EXISTS services (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		host_id TEXT NOT NULL,
		name TEXT NOT NULL,
		type INTEGER,
		status INTEGER,
		monitor INTEGER,
		pid INTEGER,
		cpu_percent REAL,
		memory_percent REAL,
		memory_kb INTEGER,
		collected_at DATETIME,
		last_seen DATETIME DEFAULT CURRENT_TIMESTAMP,
		FOREIGN KEY (host_id) REFERENCES hosts(id),
		UNIQUE(host_id, name)
	);`

	// createMetricsTable creates the metrics table (time-series data)
	//
	// This is the largest table - it stores historical metrics for graphing.
	// Every 30 seconds (or whatever interval Monit is configured for),
	// we insert new metrics for each service.
	//
	// Columns:
	//   - id: Auto-incrementing integer
	//   - host_id: Which host this metric is from
	//   - service_name: Which service this metric is for
	//   - metric_type: Category of metric (e.g., "cpu", "memory", "disk")
	//   - metric_name: Specific metric (e.g., "user", "system", "percent")
	//   - value: The numeric value
	//   - collected_at: When Monit collected this data point
	//
	// Example rows:
	//   host123 | system | cpu     | user    | 15.2 | 2025-11-22 20:30:00
	//   host123 | system | cpu     | system  | 8.1  | 2025-11-22 20:30:00
	//   host123 | system | memory  | percent | 45.6 | 2025-11-22 20:30:00
	//
	// Indexes:
	// - idx_metrics_lookup: Fast queries by (host, service, metric, time)
	// - idx_metrics_time: Fast queries by time (for cleanup)
	//
	// Why two indexes?
	// - Lookups are for graphing (show CPU for host X from time A to B)
	// - Time index is for deleting old data (cleanup metrics older than 30 days)
	createMetricsTable = `
	CREATE TABLE IF NOT EXISTS metrics (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		host_id TEXT NOT NULL,
		service_name TEXT NOT NULL,
		metric_type TEXT NOT NULL,
		metric_name TEXT NOT NULL,
		value REAL NOT NULL,
		collected_at DATETIME NOT NULL,
		FOREIGN KEY (host_id) REFERENCES hosts(id)
	);`

	// createMetricsIndexes creates indexes for fast metrics queries
	//
	// Indexes are like a book's index - they help find data quickly.
	// Without indexes, SQLite must scan every row (slow for large tables).
	// With indexes, SQLite can jump directly to relevant rows (fast!).
	//
	// Trade-offs:
	// - Faster queries (good!)
	// - Slower inserts (needs to update index - but still fast enough)
	// - More disk space (index takes space - but worth it)
	//
	// idx_metrics_lookup: Optimizes queries like:
	//   "Show me CPU metrics for host123's system service from 1pm to 2pm"
	//
	// idx_metrics_time: Optimizes queries like:
	//   "Delete all metrics older than 30 days"
	createMetricsIndexes = `
	CREATE INDEX IF NOT EXISTS idx_metrics_lookup
		ON metrics(host_id, service_name, metric_type, metric_name, collected_at);
	CREATE INDEX IF NOT EXISTS idx_metrics_time
		ON metrics(collected_at);`

	// createEventsTable creates the events table
	//
	// Events are state changes: service failed, recovered, started, stopped, etc.
	// These are used for:
	// - Alerting (send email when service fails)
	// - Audit trail (what happened when?)
	// - Debugging (why did this service restart?)
	//
	// Columns:
	//   - id: Auto-incrementing integer
	//   - host_id: Which host this event is from
	//   - service_name: Which service this event is for
	//   - event_type: Type of event (integer from Monit)
	//   - message: Human-readable description
	//   - created_at: When the event occurred
	//
	// Index:
	// - idx_events_time: Fast queries for recent events
	//
	// Events are inserted but rarely updated (append-only table).
	createEventsTable = `
	CREATE TABLE IF NOT EXISTS events (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		host_id TEXT NOT NULL,
		service_name TEXT NOT NULL,
		event_type INTEGER,
		message TEXT,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		FOREIGN KEY (host_id) REFERENCES hosts(id)
	);`

	// createEventsIndex creates index for fast event queries
	//
	// We usually query events in reverse chronological order:
	// "Show me the 50 most recent events"
	//
	// DESC means descending order (newest first)
	createEventsIndex = `
	CREATE INDEX IF NOT EXISTS idx_events_time
		ON events(created_at DESC);`

	// createFilesystemMetricsTable creates the filesystem_metrics table
	//
	// This table stores filesystem-specific metrics (disk space, inodes, I/O).
	// Only populated for filesystem services (type 0).
	//
	// Columns:
	//   - id: Auto-incrementing integer
	//   - host_id: Which host this metric is from
	//   - service_name: Filesystem service name
	//   - fs_type: Filesystem type (zfs, ext4, xfs, btrfs, etc.)
	//   - fs_flags: Mount flags (ro, rw, noatime, etc.)
	//   - mode: Directory permissions (octal)
	//   - uid: User ID owner
	//   - gid: Group ID owner
	//   - block_percent: % of disk space used
	//   - block_usage_mb: Disk space used (MB)
	//   - block_total_mb: Total disk space (MB)
	//   - inode_percent: % of inodes used
	//   - inode_usage: Number of inodes used
	//   - inode_total: Total number of inodes
	//   - read_bytes_total: Total bytes read since boot
	//   - read_ops_total: Total read operations since boot
	//   - write_bytes_total: Total bytes written since boot
	//   - write_ops_total: Total write operations since boot
	//   - collected_at: When this data was collected
	//
	// This is time-series data like the metrics table, allowing us to
	// track filesystem usage trends over time.
	createFilesystemMetricsTable = `
	CREATE TABLE IF NOT EXISTS filesystem_metrics (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		host_id TEXT NOT NULL,
		service_name TEXT NOT NULL,
		fs_type TEXT,
		fs_flags TEXT,
		mode TEXT,
		uid INTEGER,
		gid INTEGER,
		block_percent REAL,
		block_usage_mb REAL,
		block_total_mb REAL,
		inode_percent REAL,
		inode_usage INTEGER,
		inode_total INTEGER,
		read_bytes_total INTEGER,
		read_ops_total INTEGER,
		write_bytes_total INTEGER,
		write_ops_total INTEGER,
		collected_at DATETIME NOT NULL,
		FOREIGN KEY (host_id) REFERENCES hosts(id)
	);`

	// createFilesystemMetricsIndex creates index for fast filesystem metrics queries
	//
	// Optimizes queries like:
	// "Show me disk space usage for /data on host123 over the last 24 hours"
	createFilesystemMetricsIndex = `
	CREATE INDEX IF NOT EXISTS idx_filesystem_metrics_lookup
		ON filesystem_metrics(host_id, service_name, collected_at);`
)

// InitDB initializes the database and creates all tables.
//
// This function:
// 1. Opens a connection to the SQLite database file
// 2. Creates tables if they don't exist
// 3. Creates indexes for performance
// 4. Returns a connection that can be used throughout the application
//
// Parameters:
//   - dbPath: Path to the database file (e.g., "cmonit.db")
//
// Returns:
//   - *sql.DB: Database connection (use this for all queries)
//   - error: nil if successful, error describing problem if failed
//
// Important Notes:
// - If the database file doesn't exist, SQLite creates it automatically
// - IF NOT EXISTS: Safe to call multiple times (won't fail if tables exist)
// - The returned *sql.DB is a connection pool, not a single connection
// - It's safe to use from multiple goroutines (thread-safe)
// - You should call defer db.Close() in main() to clean up when shutting down
func InitDB(dbPath string) (*sql.DB, error) {
	// Open a connection to the SQLite database
	//
	// sql.Open() doesn't actually connect to the database yet!
	// It just validates the driver name and creates a connection pool.
	// The actual connection happens on the first query.
	//
	// Parameters:
	//   - "sqlite3": The driver name (registered by the import)
	//   - dbPath: The database file path
	//
	// Returns:
	//   - *sql.DB: A database connection pool
	//   - error: nil if the driver exists, error if driver not found
	//
	// Why "sqlite3"?
	// - This is the name the driver registered when we imported it
	// - Different drivers use different names (postgres, mysql, etc.)
	db, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		// Failed to open database
		// This usually means the SQLite driver isn't available
		// or there's a problem with the file path
		return nil, fmt.Errorf("failed to open database: %w", err)
	}

	// Test the connection by pinging the database
	//
	// db.Ping() actually tries to connect to the database.
	// This verifies:
	// - The database file can be accessed
	// - The file is a valid SQLite database
	// - We have read/write permissions
	//
	// Why ping?
	// - sql.Open() doesn't actually connect (lazy initialization)
	// - We want to fail early if there's a problem
	// - Better to find out now than on the first query
	err = db.Ping()
	if err != nil {
		// Failed to connect to database
		// This could mean:
		// - File permissions problem
		// - Corrupt database file
		// - Disk full
		db.Close() // Clean up the connection pool
		return nil, fmt.Errorf("failed to ping database: %w", err)
	}

	log.Printf("[INFO] Connected to database: %s", dbPath)

	// Enable foreign key constraints
	//
	// By default, SQLite doesn't enforce foreign keys!
	// This pragma (special command) enables them.
	//
	// With foreign keys enabled:
	// - Can't insert a service with invalid host_id
	// - Can't delete a host that has services (referential integrity)
	//
	// This prevents data inconsistencies.
	_, err = db.Exec("PRAGMA foreign_keys = ON;")
	if err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to enable foreign keys: %w", err)
	}

	// Create schema_version table first
	// This must exist before we can check the version
	_, err = db.Exec(createSchemaVersionTable)
	if err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to create schema_version table: %w", err)
	}

	// Check current schema version
	currentVersion, err := getSchemaVersion(db)
	if err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to get schema version: %w", err)
	}

	// If this is a new database (version = 0), set it to current version
	if currentVersion == 0 {
		log.Printf("[INFO] New database detected, initializing schema version %d", currentSchemaVersion)
		err = setSchemaVersion(db, currentSchemaVersion)
		if err != nil {
			db.Close()
			return nil, fmt.Errorf("failed to set schema version: %w", err)
		}
	} else if currentVersion < currentSchemaVersion {
		// Future migrations would go here
		log.Printf("[INFO] Migrating database from version %d to %d", currentVersion, currentSchemaVersion)
		err = migrateSchema(db, currentVersion, currentSchemaVersion)
		if err != nil {
			db.Close()
			return nil, fmt.Errorf("failed to migrate schema: %w", err)
		}
	} else if currentVersion > currentSchemaVersion {
		// Database is newer than this version of cmonit
		db.Close()
		return nil, fmt.Errorf("database schema version %d is newer than supported version %d - please upgrade cmonit",
			currentVersion, currentSchemaVersion)
	} else {
		log.Printf("[INFO] Database schema version %d is up to date", currentVersion)
	}

	// Enable Write-Ahead Logging (WAL) mode
	//
	// WAL improves concurrency:
	// - Readers don't block writers
	// - Writers don't block readers
	// - Only writers block other writers
	//
	// Default SQLite mode: readers and writers block each other (slow!)
	// WAL mode: much better for concurrent access (perfect for web servers!)
	//
	// Trade-off: Uses two extra files (.db-wal and .db-shm)
	_, err = db.Exec("PRAGMA journal_mode = WAL;")
	if err != nil {
		// WAL not available (very rare)
		// Log warning but continue (not critical)
		log.Printf("[WARN] Failed to enable WAL mode: %v", err)
	}

	// Create all tables
	//
	// IF NOT EXISTS means:
	// - If table exists: do nothing (success)
	// - If table doesn't exist: create it
	//
	// This makes InitDB() idempotent (safe to call multiple times)

	log.Printf("[INFO] Creating database schema...")

	// Create hosts table
	_, err = db.Exec(createHostsTable)
	if err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to create hosts table: %w", err)
	}

	// Create services table
	_, err = db.Exec(createServicesTable)
	if err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to create services table: %w", err)
	}

	// Create metrics table
	_, err = db.Exec(createMetricsTable)
	if err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to create metrics table: %w", err)
	}

	// Create metrics indexes (for fast queries)
	_, err = db.Exec(createMetricsIndexes)
	if err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to create metrics indexes: %w", err)
	}

	// Create events table
	_, err = db.Exec(createEventsTable)
	if err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to create events table: %w", err)
	}

	// Create events index
	_, err = db.Exec(createEventsIndex)
	if err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to create events index: %w", err)
	}

	// Create filesystem_metrics table
	_, err = db.Exec(createFilesystemMetricsTable)
	if err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to create filesystem_metrics table: %w", err)
	}

	// Create filesystem_metrics index
	_, err = db.Exec(createFilesystemMetricsIndex)
	if err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to create filesystem_metrics index: %w", err)
	}

	log.Printf("[INFO] Database schema created successfully")

	// Return the database connection
	// The caller is responsible for closing it with defer db.Close()
	return db, nil
}

// getSchemaVersion returns the current database schema version.
//
// Returns:
//   - version: Current schema version (0 if no version is set)
//   - error: nil if successful, error if query failed
func getSchemaVersion(db *sql.DB) (int, error) {
	var version int
	err := db.QueryRow("SELECT version FROM schema_version ORDER BY version DESC LIMIT 1").Scan(&version)
	if err == sql.ErrNoRows {
		// No version row exists yet, this is a new database
		return 0, nil
	}
	if err != nil {
		return 0, fmt.Errorf("failed to query schema version: %w", err)
	}
	return version, nil
}

// setSchemaVersion sets the database schema version.
//
// Parameters:
//   - db: Database connection
//   - version: Version number to set
//
// Returns:
//   - error: nil if successful, error if update failed
func setSchemaVersion(db *sql.DB, version int) error {
	_, err := db.Exec("INSERT OR REPLACE INTO schema_version (version) VALUES (?)", version)
	if err != nil {
		return fmt.Errorf("failed to set schema version: %w", err)
	}
	log.Printf("[INFO] Set database schema version to %d", version)
	return nil
}

// migrateSchema performs database schema migrations.
//
// This function handles migrations from older schema versions to newer ones.
// Each migration step is a separate case that modifies the database structure.
//
// Parameters:
//   - db: Database connection
//   - fromVersion: Current schema version
//   - toVersion: Target schema version
//
// Returns:
//   - error: nil if migration succeeded, error if it failed
//
// How to add migrations:
// 1. Increment currentSchemaVersion constant
// 2. Add a new case in the switch statement below
// 3. Apply ALTER TABLE or other DDL statements
// 4. Update the version number
//
// Example future migration (version 1 -> 2):
//
//	case 1:
//	    // Add new column to hosts table
//	    _, err = db.Exec("ALTER TABLE hosts ADD COLUMN new_field TEXT")
//	    if err != nil {
//	        return fmt.Errorf("migration v1->v2 failed: %w", err)
//	    }
//	    err = setSchemaVersion(db, 2)
//	    if err != nil {
//	        return err
//	    }
//	    log.Printf("[INFO] Migrated to schema version 2")
//	    fromVersion = 2
func migrateSchema(db *sql.DB, fromVersion, toVersion int) error {
	log.Printf("[INFO] Starting schema migration from v%d to v%d", fromVersion, toVersion)

	// Apply migrations sequentially
	for fromVersion < toVersion {
		switch fromVersion {
		case 1:
			// Migration from version 1 to version 2
			// Add platform information and process metrics
			log.Printf("[INFO] Migrating from v1 to v2: Adding platform info and process metrics")

			// Add platform columns to hosts table
			migrations := []string{
				"ALTER TABLE hosts ADD COLUMN os_name TEXT",
				"ALTER TABLE hosts ADD COLUMN os_release TEXT",
				"ALTER TABLE hosts ADD COLUMN os_version TEXT",
				"ALTER TABLE hosts ADD COLUMN machine TEXT",
				"ALTER TABLE hosts ADD COLUMN cpu_count INTEGER",
				"ALTER TABLE hosts ADD COLUMN total_memory INTEGER",
				"ALTER TABLE hosts ADD COLUMN total_swap INTEGER",
				"ALTER TABLE hosts ADD COLUMN system_uptime INTEGER",
				"ALTER TABLE hosts ADD COLUMN boottime INTEGER",

				// Add process metrics columns to services table
				"ALTER TABLE services ADD COLUMN pid INTEGER",
				"ALTER TABLE services ADD COLUMN cpu_percent REAL",
				"ALTER TABLE services ADD COLUMN memory_percent REAL",
				"ALTER TABLE services ADD COLUMN memory_kb INTEGER",
			}

			for _, migration := range migrations {
				_, err := db.Exec(migration)
				if err != nil {
					return fmt.Errorf("migration v1->v2 failed on '%s': %w", migration, err)
				}
			}

			fromVersion = 2
			err := setSchemaVersion(db, fromVersion)
			if err != nil {
				return err
			}
			log.Printf("[INFO] Successfully migrated to schema version 2")

		case 2:
			// Migration from version 2 to version 3
			// Add Monit daemon uptime tracking for restart detection
			log.Printf("[INFO] Migrating from v2 to v3: Adding monit_uptime column")

			_, err := db.Exec("ALTER TABLE hosts ADD COLUMN monit_uptime INTEGER")
			if err != nil {
				return fmt.Errorf("migration v2->v3 failed: %w", err)
			}

			fromVersion = 3
			err = setSchemaVersion(db, fromVersion)
			if err != nil {
				return err
			}
			log.Printf("[INFO] Successfully migrated to schema version 3")

		case 3:
			// Migration from version 3 to version 4
			// Add filesystem_metrics table for filesystem service support
			log.Printf("[INFO] Migrating from v3 to v4: Adding filesystem_metrics table")

			_, err := db.Exec(createFilesystemMetricsTable)
			if err != nil {
				return fmt.Errorf("migration v3->v4 failed creating table: %w", err)
			}

			_, err = db.Exec(createFilesystemMetricsIndex)
			if err != nil {
				return fmt.Errorf("migration v3->v4 failed creating index: %w", err)
			}

			fromVersion = 4
			err = setSchemaVersion(db, fromVersion)
			if err != nil {
				return err
			}
			log.Printf("[INFO] Successfully migrated to schema version 4")

		default:
			return fmt.Errorf("no migration path from version %d", fromVersion)
		}
	}

	log.Printf("[INFO] Schema migration completed successfully")
	return nil
}
