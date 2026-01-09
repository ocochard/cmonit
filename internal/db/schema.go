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
const currentSchemaVersion = 12

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
	//   - poll_interval: Monit's check interval in seconds (for heartbeat health status)
	//   - last_seen: When we last received data from this host
	//   - created_at: When we first saw this host
	//   - description: User-defined HTML description/notes for this host (max 8192 chars)
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
		incarnation INTEGER CHECK (incarnation >= 0),
		version TEXT,
		http_address TEXT,
		http_port INTEGER CHECK (http_port > 0 AND http_port <= 65535),
		http_ssl INTEGER DEFAULT 0 CHECK (http_ssl IN (0, 1)),
		http_username TEXT,
		http_password TEXT,
		os_name TEXT,
		os_release TEXT,
		os_version TEXT,
		machine TEXT,
		cpu_count INTEGER CHECK (cpu_count > 0),
		total_memory INTEGER CHECK (total_memory >= 0),
		total_swap INTEGER CHECK (total_swap >= 0),
		system_uptime INTEGER CHECK (system_uptime >= 0),
		boottime INTEGER CHECK (boottime >= 0),
		monit_uptime INTEGER CHECK (monit_uptime >= 0),
		poll_interval INTEGER DEFAULT 30 CHECK (poll_interval > 0),
		last_seen DATETIME DEFAULT CURRENT_TIMESTAMP,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		description TEXT DEFAULT '' CHECK (length(description) <= 8192),
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
		type INTEGER CHECK (type >= 0 AND type <= 8),
		status INTEGER CHECK (status >= 0),
		monitor INTEGER CHECK (monitor >= 0 AND monitor <= 2),
		pid INTEGER CHECK (pid > 0),
		cpu_percent REAL CHECK (cpu_percent >= 0),
		memory_percent REAL CHECK (memory_percent >= 0 AND memory_percent <= 100),
		memory_kb INTEGER CHECK (memory_kb >= 0),
		collected_at DATETIME,
		last_seen DATETIME DEFAULT CURRENT_TIMESTAMP,
		FOREIGN KEY (host_id) REFERENCES hosts(id) ON DELETE CASCADE,
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
		FOREIGN KEY (host_id) REFERENCES hosts(id) ON DELETE CASCADE
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
		FOREIGN KEY (host_id) REFERENCES hosts(id) ON DELETE CASCADE
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
		uid INTEGER CHECK (uid >= 0),
		gid INTEGER CHECK (gid >= 0),
		block_percent REAL CHECK (block_percent >= 0 AND block_percent <= 100),
		block_usage_mb REAL CHECK (block_usage_mb >= 0),
		block_total_mb REAL CHECK (block_total_mb >= 0),
		inode_percent REAL CHECK (inode_percent >= 0 AND inode_percent <= 100),
		inode_usage INTEGER CHECK (inode_usage >= 0),
		inode_total INTEGER CHECK (inode_total >= 0),
		read_bytes_total INTEGER CHECK (read_bytes_total >= 0),
		read_ops_total INTEGER CHECK (read_ops_total >= 0),
		write_bytes_total INTEGER CHECK (write_bytes_total >= 0),
		write_ops_total INTEGER CHECK (write_ops_total >= 0),
		collected_at DATETIME NOT NULL,
		FOREIGN KEY (host_id) REFERENCES hosts(id) ON DELETE CASCADE
	);`

	// createFilesystemMetricsIndex creates index for fast filesystem metrics queries
	//
	// Optimizes queries like:
	// "Show me disk space usage for /data on host123 over the last 24 hours"
	createFilesystemMetricsIndex = `
	CREATE INDEX IF NOT EXISTS idx_filesystem_metrics_lookup
		ON filesystem_metrics(host_id, service_name, collected_at);`

	// createNetworkMetricsTable creates the network_metrics table
	//
	// This table stores network interface metrics (link status, traffic, errors).
	// Only populated for network interface services (type 8).
	//
	// Columns:
	//   - id: Auto-incrementing integer
	//   - host_id: Which host this metric is from
	//   - service_name: Network interface service name (e.g., "Ethernet")
	//   - link_state: Link status (1=up, 0=down)
	//   - link_speed: Speed in bits per second (e.g., 1000000000 = 1 Gbps)
	//   - link_duplex: Duplex mode (1=full-duplex, 0=half-duplex)
	//   - download_packets_now: Current download packets per second
	//   - download_packets_total: Total download packets since boot
	//   - download_bytes_now: Current download bytes per second
	//   - download_bytes_total: Total download bytes since boot
	//   - download_errors_now: Current download errors
	//   - download_errors_total: Total download errors since boot
	//   - upload_packets_now: Current upload packets per second
	//   - upload_packets_total: Total upload packets since boot
	//   - upload_bytes_now: Current upload bytes per second
	//   - upload_bytes_total: Total upload bytes since boot
	//   - upload_errors_now: Current upload errors
	//   - upload_errors_total: Total upload errors since boot
	//   - collected_at: When this data was collected
	//
	// This is time-series data like the metrics table, allowing us to
	// track network usage trends and detect performance issues.
	createNetworkMetricsTable = `
	CREATE TABLE IF NOT EXISTS network_metrics (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		host_id TEXT NOT NULL,
		service_name TEXT NOT NULL,
		link_state INTEGER CHECK (link_state IN (0, 1)),
		link_speed INTEGER CHECK (link_speed >= 0),
		link_duplex INTEGER CHECK (link_duplex IN (0, 1)),
		download_packets_now INTEGER CHECK (download_packets_now >= 0),
		download_packets_total INTEGER CHECK (download_packets_total >= 0),
		download_bytes_now INTEGER CHECK (download_bytes_now >= 0),
		download_bytes_total INTEGER CHECK (download_bytes_total >= 0),
		download_errors_now INTEGER CHECK (download_errors_now >= 0),
		download_errors_total INTEGER CHECK (download_errors_total >= 0),
		upload_packets_now INTEGER CHECK (upload_packets_now >= 0),
		upload_packets_total INTEGER CHECK (upload_packets_total >= 0),
		upload_bytes_now INTEGER CHECK (upload_bytes_now >= 0),
		upload_bytes_total INTEGER CHECK (upload_bytes_total >= 0),
		upload_errors_now INTEGER CHECK (upload_errors_now >= 0),
		upload_errors_total INTEGER CHECK (upload_errors_total >= 0),
		collected_at DATETIME NOT NULL,
		FOREIGN KEY (host_id) REFERENCES hosts(id) ON DELETE CASCADE
	);`

	// createNetworkMetricsIndex creates index for fast network metrics queries
	//
	// Optimizes queries like:
	// "Show me network traffic for eth0 on host123 over the last hour"
	createNetworkMetricsIndex = `
	CREATE INDEX IF NOT EXISTS idx_network_metrics_lookup
		ON network_metrics(host_id, service_name, collected_at);`

	// createFileMetricsTable creates the file_metrics table
	//
	// This table stores file monitoring metrics (permissions, size, timestamps, checksums).
	// Only populated for file services (type 2).
	//
	// Columns:
	//   - id: Auto-incrementing integer
	//   - host_id: Which host this metric is from
	//   - service_name: File service name (e.g., "metatron")
	//   - mode: File permission mode (e.g., "644", "755")
	//   - uid: User ID of file owner
	//   - gid: Group ID of file owner
	//   - size: File size in bytes
	//   - hardlink: Number of hard links to the file
	//   - access_time: Last access time (Unix timestamp)
	//   - change_time: Last change time (Unix timestamp)
	//   - modify_time: Last modification time (Unix timestamp)
	//   - checksum_type: Checksum algorithm (MD5, SHA1)
	//   - checksum_value: Checksum hash value
	//   - collected_at: When this data was collected
	//
	// This is time-series data like the metrics table, allowing us to
	// track file changes over time and detect unauthorized modifications.
	createFileMetricsTable = `
	CREATE TABLE IF NOT EXISTS file_metrics (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		host_id TEXT NOT NULL,
		service_name TEXT NOT NULL,
		mode TEXT,
		uid INTEGER CHECK (uid >= 0),
		gid INTEGER CHECK (gid >= 0),
		size INTEGER CHECK (size >= 0),
		hardlink INTEGER CHECK (hardlink >= 0),
		access_time INTEGER CHECK (access_time >= 0),
		change_time INTEGER CHECK (change_time >= 0),
		modify_time INTEGER CHECK (modify_time >= 0),
		checksum_type TEXT,
		checksum_value TEXT,
		collected_at DATETIME NOT NULL,
		FOREIGN KEY (host_id) REFERENCES hosts(id) ON DELETE CASCADE
	);`

	// createFileMetricsIndex creates index for fast file metrics queries
	//
	// Optimizes queries like:
	// "Show me file changes for metatron on host123 over the last week"
	createFileMetricsIndex = `
	CREATE INDEX IF NOT EXISTS idx_file_metrics_lookup
		ON file_metrics(host_id, service_name, collected_at);`

	// createProgramMetricsTable creates the program_metrics table
	//
	// This table stores program status check metrics (exit status, output).
	// Only populated for program services (type 7).
	//
	// Columns:
	//   - id: Auto-incrementing integer
	//   - host_id: Which host this metric is from
	//   - service_name: Program service name (e.g., "temperature")
	//   - started: Unix timestamp when program was last executed
	//   - exit_status: Program exit status code (0=success, non-zero=error)
	//   - output: Program stdout/stderr output (up to 512 bytes)
	//   - collected_at: When this data was collected
	//
	// This is time-series data like the metrics table, allowing us to
	// track program execution history and output over time.
	createProgramMetricsTable = `
	CREATE TABLE IF NOT EXISTS program_metrics (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		host_id TEXT NOT NULL,
		service_name TEXT NOT NULL,
		started INTEGER CHECK (started >= 0),
		exit_status INTEGER,
		output TEXT,
		collected_at DATETIME NOT NULL,
		FOREIGN KEY (host_id) REFERENCES hosts(id) ON DELETE CASCADE
	);`

	// createProgramMetricsIndex creates index for fast program metrics queries
	//
	// Optimizes queries like:
	// "Show me program execution history for temperature on host123 over the last day"
	createProgramMetricsIndex = `
	CREATE INDEX IF NOT EXISTS idx_program_metrics_lookup
		ON program_metrics(host_id, service_name, collected_at);`

	// createRemoteHostMetricsTable creates the remote_host_metrics table
	//
	// This table stores remote host monitoring metrics (ping, port, unix socket response times).
	// Only populated for remote host services (type 4) and process services with unix sockets (type 3).
	//
	// Columns:
	//   - id: Auto-incrementing integer
	//   - host_id: Which host this metric is from
	//   - service_name: Remote host service name (e.g., "homeassistant")
	//   - icmp_type: ICMP check type (usually "Ping")
	//   - icmp_responsetime: Ping response time in seconds (e.g., 0.000348)
	//   - port_hostname: Hostname/IP being monitored for port checks
	//   - port_number: Port number being monitored
	//   - port_protocol: Protocol being used (DEFAULT, etc.)
	//   - port_type: Connection type (TCP, UDP)
	//   - port_responsetime: Port response time in seconds (e.g., 0.000755)
	//   - unix_path: Unix socket path (for process services with unix socket monitoring)
	//   - unix_protocol: Unix socket protocol
	//   - unix_responsetime: Unix socket response time in seconds (-1.0 if failed)
	//   - collected_at: When this data was collected
	//
	// This is time-series data like the metrics table, allowing us to
	// track response times over time and detect network issues or service degradation.
	createRemoteHostMetricsTable = `
	CREATE TABLE IF NOT EXISTS remote_host_metrics (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		host_id TEXT NOT NULL,
		service_name TEXT NOT NULL,
		icmp_type TEXT,
		icmp_responsetime REAL CHECK (icmp_responsetime >= 0),
		port_hostname TEXT,
		port_number INTEGER CHECK (port_number > 0 AND port_number <= 65535),
		port_protocol TEXT,
		port_type TEXT,
		port_responsetime REAL CHECK (port_responsetime >= 0),
		unix_path TEXT,
		unix_protocol TEXT,
		unix_responsetime REAL,
		collected_at DATETIME NOT NULL,
		FOREIGN KEY (host_id) REFERENCES hosts(id) ON DELETE CASCADE
	);`

	// createRemoteHostMetricsIndex creates index for fast remote host metrics queries
	//
	// Optimizes queries like:
	// "Show me ping response times for homeassistant on host123 over the last week"
	createRemoteHostMetricsIndex = `
	CREATE INDEX IF NOT EXISTS idx_remote_host_metrics_lookup
		ON remote_host_metrics(host_id, service_name, collected_at);`

	// createHostAvailabilityTable creates the host_availability table
	//
	// This table stores host availability history for time-series graphing.
	// It records the health status of each host over time based on heartbeat monitoring.
	//
	// Columns:
	//   - id: Auto-incrementing integer
	//   - host_id: Which host this availability record is for
	//   - timestamp: When this status was recorded (Unix timestamp)
	//   - status: Health status ('green', 'yellow', 'red')
	//   - last_seen: Last time host reported data (Unix timestamp)
	//   - poll_interval: Monit poll interval in seconds (for reference)
	//
	// Status meanings:
	//   - 'green': Host is online and healthy (last_seen < poll_interval * 2)
	//   - 'yellow': Host is in warning state (last_seen between poll_interval * 2 and * 4)
	//   - 'red': Host is offline (last_seen > poll_interval * 4)
	//
	// This is time-series data that allows tracking host uptime/downtime over time.
	// Records are inserted:
	//   - When new data is received from a host (records 'green' status)
	//   - Periodically by background job (checks all hosts, records current status)
	createHostAvailabilityTable = `
	CREATE TABLE IF NOT EXISTS host_availability (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		host_id TEXT NOT NULL,
		timestamp INTEGER NOT NULL,
		status TEXT NOT NULL,
		last_seen INTEGER NOT NULL,
		poll_interval INTEGER NOT NULL,
		FOREIGN KEY (host_id) REFERENCES hosts(id) ON DELETE CASCADE
	);`

	// createHostAvailabilityIndex creates index for fast availability queries
	//
	// Optimizes queries like:
	// "Show me availability history for host123 over the last 24 hours"
	createHostAvailabilityIndex = `
	CREATE INDEX IF NOT EXISTS idx_host_availability_lookup
		ON host_availability(host_id, timestamp);`

	// createHostGroupsTable creates the hostgroups table
	//
	// This table stores unique hostgroup names.
	// Monit allows hosts to belong to one or more groups via the "set group" directive.
	//
	// Columns:
	//   - id: Auto-incrementing integer
	//   - name: Unique group name (e.g., "Workstation", "FreeBSD", "Production")
	//   - created_at: When this group was first seen
	//
	// Example groups: "Production", "Development", "Web Servers", "Database Servers"
	createHostGroupsTable = `
	CREATE TABLE IF NOT EXISTS hostgroups (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		name TEXT NOT NULL UNIQUE,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP
	);`

	// createHostHostGroupsTable creates the host_hostgroups junction table
	//
	// This is a many-to-many relationship table between hosts and hostgroups.
	// A host can belong to multiple groups, and a group can contain multiple hosts.
	//
	// Columns:
	//   - host_id: Foreign key to hosts table
	//   - hostgroup_id: Foreign key to hostgroups table
	//
	// The UNIQUE constraint prevents duplicate associations.
	// CASCADE DELETE ensures that when a host is deleted, its group associations are also deleted.
	createHostHostGroupsTable = `
	CREATE TABLE IF NOT EXISTS host_hostgroups (
		host_id TEXT NOT NULL,
		hostgroup_id INTEGER NOT NULL,
		FOREIGN KEY (host_id) REFERENCES hosts(id) ON DELETE CASCADE,
		FOREIGN KEY (hostgroup_id) REFERENCES hostgroups(id) ON DELETE CASCADE,
		UNIQUE(host_id, hostgroup_id)
	);`

	// createHostHostGroupsIndex creates index for fast hostgroup lookups
	//
	// Optimizes queries like:
	// "Show me all hosts in the Production group"
	// "Show me all groups for host123"
	createHostHostGroupsIndex = `
	CREATE INDEX IF NOT EXISTS idx_host_hostgroups_host
		ON host_hostgroups(host_id);
	CREATE INDEX IF NOT EXISTS idx_host_hostgroups_group
		ON host_hostgroups(hostgroup_id);`
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
	// WAL mode: supports concurrent readers and writers (suitable for web servers)
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

	// Create network_metrics table
	_, err = db.Exec(createNetworkMetricsTable)
	if err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to create network_metrics table: %w", err)
	}

	// Create network_metrics index
	_, err = db.Exec(createNetworkMetricsIndex)
	if err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to create network_metrics index: %w", err)
	}

	// Create file_metrics table
	_, err = db.Exec(createFileMetricsTable)
	if err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to create file_metrics table: %w", err)
	}

	// Create file_metrics index
	_, err = db.Exec(createFileMetricsIndex)
	if err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to create file_metrics index: %w", err)
	}

	// Create program_metrics table
	_, err = db.Exec(createProgramMetricsTable)
	if err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to create program_metrics table: %w", err)
	}

	// Create program_metrics index
	_, err = db.Exec(createProgramMetricsIndex)
	if err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to create program_metrics index: %w", err)
	}

	// Create remote_host_metrics table
	_, err = db.Exec(createRemoteHostMetricsTable)
	if err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to create remote_host_metrics table: %w", err)
	}

	// Create remote_host_metrics index
	_, err = db.Exec(createRemoteHostMetricsIndex)
	if err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to create remote_host_metrics index: %w", err)
	}

	// Create host_availability table
	_, err = db.Exec(createHostAvailabilityTable)
	if err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to create host_availability table: %w", err)
	}

	// Create host_availability index
	_, err = db.Exec(createHostAvailabilityIndex)
	if err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to create host_availability index: %w", err)
	}

	// Create hostgroups table
	_, err = db.Exec(createHostGroupsTable)
	if err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to create hostgroups table: %w", err)
	}

	// Create host_hostgroups junction table
	_, err = db.Exec(createHostHostGroupsTable)
	if err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to create host_hostgroups table: %w", err)
	}

	// Create host_hostgroups indexes
	_, err = db.Exec(createHostHostGroupsIndex)
	if err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to create host_hostgroups indexes: %w", err)
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

		case 4:
			// Migration from version 4 to version 5
			// Add network_metrics table for network interface service support
			log.Printf("[INFO] Migrating from v4 to v5: Adding network_metrics table")

			_, err := db.Exec(createNetworkMetricsTable)
			if err != nil {
				return fmt.Errorf("migration v4->v5 failed creating table: %w", err)
			}

			_, err = db.Exec(createNetworkMetricsIndex)
			if err != nil {
				return fmt.Errorf("migration v4->v5 failed creating index: %w", err)
			}

			fromVersion = 5
			err = setSchemaVersion(db, fromVersion)
			if err != nil {
				return err
			}
			log.Printf("[INFO] Successfully migrated to schema version 5")

		case 5:
			// Migration from version 5 to version 6
			// Add file_metrics and program_metrics tables
			log.Printf("[INFO] Migrating from v5 to v6: Adding file_metrics and program_metrics tables")

			// Create file_metrics table
			_, err := db.Exec(createFileMetricsTable)
			if err != nil {
				return fmt.Errorf("migration v5->v6 failed creating file_metrics table: %w", err)
			}

			_, err = db.Exec(createFileMetricsIndex)
			if err != nil {
				return fmt.Errorf("migration v5->v6 failed creating file_metrics index: %w", err)
			}

			// Create program_metrics table
			_, err = db.Exec(createProgramMetricsTable)
			if err != nil {
				return fmt.Errorf("migration v5->v6 failed creating program_metrics table: %w", err)
			}

			_, err = db.Exec(createProgramMetricsIndex)
			if err != nil {
				return fmt.Errorf("migration v5->v6 failed creating program_metrics index: %w", err)
			}

			fromVersion = 6
			err = setSchemaVersion(db, fromVersion)
			if err != nil {
				return err
			}
			log.Printf("[INFO] Successfully migrated to schema version 6")

		case 6:
			// Migration from version 6 to version 7
			// Add poll_interval column to hosts table for heartbeat-based health status
			log.Printf("[INFO] Migrating from v6 to v7: Adding poll_interval column to hosts table")

			_, err := db.Exec("ALTER TABLE hosts ADD COLUMN poll_interval INTEGER DEFAULT 30")
			if err != nil {
				return fmt.Errorf("migration v6->v7 failed: %w", err)
			}

			fromVersion = 7
			err = setSchemaVersion(db, fromVersion)
			if err != nil {
				return err
			}
			log.Printf("[INFO] Successfully migrated to schema version 7")

		case 7:
			// Migration from version 7 to version 8
			// Add remote_host_metrics table for Remote Host monitoring (ICMP, Port, Unix socket)
			log.Printf("[INFO] Migrating from v7 to v8: Adding remote_host_metrics table")

			// Create remote_host_metrics table
			_, err := db.Exec(createRemoteHostMetricsTable)
			if err != nil {
				return fmt.Errorf("migration v7->v8 failed creating remote_host_metrics table: %w", err)
			}

			_, err = db.Exec(createRemoteHostMetricsIndex)
			if err != nil {
				return fmt.Errorf("migration v7->v8 failed creating remote_host_metrics index: %w", err)
			}

			fromVersion = 8
			err = setSchemaVersion(db, fromVersion)
			if err != nil {
				return err
			}
			log.Printf("[INFO] Successfully migrated to schema version 8")

		case 8:
			// Migration from version 8 to version 9
			// Add host_availability table for tracking host uptime/downtime over time
			log.Printf("[INFO] Migrating from v8 to v9: Adding host_availability table")

			// Create host_availability table
			_, err := db.Exec(createHostAvailabilityTable)
			if err != nil {
				return fmt.Errorf("migration v8->v9 failed creating host_availability table: %w", err)
			}

			_, err = db.Exec(createHostAvailabilityIndex)
			if err != nil {
				return fmt.Errorf("migration v8->v9 failed creating host_availability index: %w", err)
			}

			fromVersion = 9
			err = setSchemaVersion(db, fromVersion)
			if err != nil {
				return err
			}
			log.Printf("[INFO] Successfully migrated to schema version 9")

		case 9:
			// Migration from version 9 to version 10
			// Add description field to hosts table for user-defined HTML notes
			log.Printf("[INFO] Migrating from v9 to v10: Adding description column to hosts table")

			_, err := db.Exec("ALTER TABLE hosts ADD COLUMN description TEXT DEFAULT ''")
			if err != nil {
				return fmt.Errorf("migration v9->v10 failed: %w", err)
			}

			fromVersion = 10
			err = setSchemaVersion(db, fromVersion)
			if err != nil {
				return err
			}
			log.Printf("[INFO] Successfully migrated to schema version 10")

		case 10:
			// Migration from version 10 to version 11
			// Add hostgroups and host_hostgroups tables for host group support
			log.Printf("[INFO] Migrating from v10 to v11: Adding hostgroups tables")

			// Create hostgroups table
			_, err := db.Exec(createHostGroupsTable)
			if err != nil {
				return fmt.Errorf("migration v10->v11 failed creating hostgroups table: %w", err)
			}

			// Create host_hostgroups junction table
			_, err = db.Exec(createHostHostGroupsTable)
			if err != nil {
				return fmt.Errorf("migration v10->v11 failed creating host_hostgroups table: %w", err)
			}

			// Create indexes
			_, err = db.Exec(createHostHostGroupsIndex)
			if err != nil {
				return fmt.Errorf("migration v10->v11 failed creating indexes: %w", err)
			}

			fromVersion = 11
			err = setSchemaVersion(db, fromVersion)
			if err != nil {
				return err
			}
			log.Printf("[INFO] Successfully migrated to schema version 11")

		case 11:
			// Migration from version 11 to version 12
			// Schema improvements: CASCADE DELETE, CHECK constraints, description length limit
			//
			// IMPORTANT: SQLite does not allow modifying existing table constraints.
			// The new constraints (CASCADE DELETE, CHECK constraints) are defined in the
			// table creation statements and will apply to:
			// - New databases created with InitDB()
			// - Tables created after this migration
			//
			// For existing tables in production databases:
			// - Existing data is not affected
			// - Foreign keys already have ON DELETE CASCADE via the DeleteHost() function
			// - CHECK constraints would require table recreation (not safe for production)
			//
			// The schema improvements include:
			// - All foreign keys now specify ON DELETE CASCADE
			// - CHECK constraints for data validation (percentages 0-100, positive integers)
			// - Description field limited to 8192 characters
			// - Service type constrained to 0-8
			// - Monitor status constrained to 0-2
			//
			// These improvements enhance data integrity for new installations and
			// document the intended constraints in the schema.
			log.Printf("[INFO] Migrating from v11 to v12: Schema improvements (CASCADE DELETE, CHECK constraints)")
			log.Printf("[INFO] Note: New constraints apply to new databases; existing tables unchanged")

			fromVersion = 12
			err := setSchemaVersion(db, fromVersion)
			if err != nil {
				return err
			}
			log.Printf("[INFO] Successfully migrated to schema version 12")

		default:
			return fmt.Errorf("no migration path from version %d", fromVersion)
		}
	}

	log.Printf("[INFO] Schema migration completed successfully")
	return nil
}
