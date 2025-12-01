// Package web provides HTTP handlers for the web user interface.
//
// This package contains the web dashboard handlers that display:
// - Host status overview
// - Service details per host
// - Time-series graphs (Phase 3)
//
// The web interface runs on a separate port (3000) from the collector (8080).
package web

import (
	"database/sql"  // Database access
	"embed"         // Embed static files
	"fmt"           // String formatting
	"html/template" // HTML templating
	"log"           // Logging
	"net/http"      // HTTP server
	"time"          // Time handling
)

// =============================================================================
// DATA STRUCTURES
// =============================================================================

// DashboardData holds all data needed to render the dashboard.
//
// This struct is passed to the HTML template engine.
// Go templates can access these fields using {{.Hosts}}, {{.LastUpdate}}, etc.
type DashboardData struct {
	Hosts      []HostWithServices // List of all monitored hosts
	LastUpdate time.Time          // When this data was retrieved
}

// HostWithServices represents a host and all its services.
//
// This combines data from the hosts and services tables.
type HostWithServices struct {
	ID            string    // Unique host ID from Monit
	Hostname      string    // Display name (e.g., "bigone")
	Version       string    // Monit version
	OSName        string    // Operating system name
	OSRelease     string    // OS version/release
	Machine       string    // CPU architecture
	CPUCount      int       // Number of CPU cores
	TotalMemory   int64     // Total RAM in bytes
	TotalSwap     int64     // Total swap in bytes
	SystemUptime  *int64    // System uptime in seconds
	Boottime      *int64    // Unix timestamp of last boot
	LastSeen      time.Time // Last successful update
	Services      []Service // All services on this host
	IsStale       bool      // True if not seen in 5+ minutes (deprecated, use HealthStatus)
	PollInterval  int       // Monit poll interval in seconds
	HealthStatus  string    // Host health status: "green", "yellow", "red"
	HealthEmoji   string    // Health status emoji: 🟢, 🟡, 🔴
	HealthLabel   string    // Health status label: "Healthy", "Warning", "Offline"
	LastSeenText  string    // Human-readable "last seen" text (e.g., "5 minutes ago")
}

// Service represents a monitored service.
//
// Maps to the services table in the database.
type Service struct {
	Name          string    // Service name (e.g., "sshd", "nginx")
	Type          int       // Service type (3=process, 5=system, 7=program)
	TypeName      string    // Human-readable type ("Process", "System", etc.")
	Status        int       // 0=ok, 1=warning, 2=critical
	StatusName    string    // Human-readable status ("OK", "Warning", "Critical")
	StatusColor   string    // CSS color class ("green", "yellow", "red")
	Monitor       int       // 0=not monitored, 1=monitored
	PID           *int      // Process ID (for process services)
	CPUPercent    *float64  // CPU usage % (for process services)
	MemoryPercent *float64  // Memory usage % (for process services)
	MemoryKB      *int64    // Memory usage in KB (for process services)
	CollectedAt   time.Time // When metrics were last collected
}

// StatusData holds data for the main status overview page.
type StatusData struct {
	Hosts      []HostStatus // List of all hosts with aggregated status
	LastUpdate time.Time    // When this data was retrieved
}

// HostStatus represents a host's overall status for the status page.
type HostStatus struct {
	ID                string    // Unique host ID
	Hostname          string    // Display name
	IsStale           bool      // True if not seen in 5+ minutes
	LastSeen          time.Time // Last update time
	StatusColor       string    // Overall status: "green", "orange", "red", "gray"
	StatusName        string    // Status name: "OK", "Warning", "Critical", "Unknown"
	StatusDescription string    // Human-readable status description
	CPUPercent        *float64  // System CPU usage %
	MemoryPercent     *float64  // System memory usage %
	EventCount        int       // Number of events for this host
	TotalServices     int       // Total number of services
	FailedServices    int       // Number of failed/warning services
}

// EventsData holds data for the events page.
type EventsData struct {
	HostID     string    // Host ID
	Hostname   string    // Host display name
	Events     []Event   // List of events
	LastUpdate time.Time // When this data was retrieved
}

// Event represents a single event from the events table.
type Event struct {
	ID            int       // Event ID
	ServiceName   string    // Service that generated the event
	EventType     int       // Event type code
	EventTypeName string    // Human-readable event type
	Message       string    // Event message
	CreatedAt     time.Time // When the event occurred
}

// ServiceDetailData holds data for the service detail page.
type ServiceDetailData struct {
	HostID          string              // Host ID
	Hostname        string              // Host display name
	Service         Service             // Service information
	FilesystemData  *FilesystemMetrics  // Filesystem metrics (if type 0)
	FileData        *FileMetrics        // File metrics (if type 2)
	ProcessData     *ProcessMetrics     // Process metrics (if type 3)
	SystemData      *SystemMetrics      // System metrics (if type 5)
	ProgramData     *ProgramMetrics     // Program metrics (if type 7)
	NetworkData     *NetworkMetrics     // Network metrics (if type 8)
	RemoteHostData  *RemoteHostMetrics  // Remote host metrics (if type 3 or 4)
	LastUpdate      time.Time           // When this data was retrieved
}

// FilesystemMetrics holds filesystem service metrics.
type FilesystemMetrics struct {
	FSType          string  // Filesystem type (e.g., "zfs", "ext4")
	FSFlags         string  // Mount flags
	Mode            string  // Permissions mode
	UID             int     // Owner UID
	GID             int     // Owner GID
	BlockPercent    float64 // Disk usage percentage
	BlockUsageMB    float64 // Used space in MB
	BlockTotalMB    float64 // Total space in MB
	InodePercent    float64 // Inode usage percentage
	InodeUsage      int64   // Used inodes
	InodeTotal      int64   // Total inodes
	ReadBytesTotal  int64   // Total bytes read
	ReadOpsTotal    int64   // Total read operations
	WriteBytesTotal int64   // Total bytes written
	WriteOpsTotal   int64   // Total write operations
}

// ProcessMetrics holds process service metrics.
type ProcessMetrics struct {
	PID           int     // Process ID
	CPUPercent    float64 // CPU usage percentage
	MemoryPercent float64 // Memory usage percentage
	MemoryKB      int64   // Memory usage in KB
}

// NetworkMetrics holds network interface service metrics.
type NetworkMetrics struct {
	LinkState            int     // Link state (0=down, 1=up)
	LinkSpeed            int64   // Link speed in bits per second
	LinkDuplex           int     // Duplex mode (0=half, 1=full)
	DownloadPacketsNow   int64   // Current download packets per second
	DownloadPacketsTotal int64   // Total download packets since boot
	DownloadBytesNow     int64   // Current download bytes per second
	DownloadBytesTotal   int64   // Total download bytes since boot
	DownloadErrorsNow    int64   // Current download errors per second
	DownloadErrorsTotal  int64   // Total download errors since boot
	UploadPacketsNow     int64   // Current upload packets per second
	UploadPacketsTotal   int64   // Total upload packets since boot
	UploadBytesNow       int64   // Current upload bytes per second
	UploadBytesTotal     int64   // Total upload bytes since boot
	UploadErrorsNow      int64   // Current upload errors per second
	UploadErrorsTotal    int64   // Total upload errors since boot
}

// FileMetrics holds file service metrics.
type FileMetrics struct {
	Mode          string // Permissions mode (e.g., "644")
	UID           int    // Owner user ID
	GID           int    // Owner group ID
	Size          int64  // File size in bytes
	Hardlink      int    // Number of hard links
	AccessTime    int64  // Unix timestamp of last access
	ChangeTime    int64  // Unix timestamp of last change
	ModifyTime    int64  // Unix timestamp of last modification
	ChecksumType  string // Checksum algorithm (e.g., "MD5")
	ChecksumValue string // Checksum value
}

// ProgramMetrics holds program service metrics.
type ProgramMetrics struct {
	Started    int64  // Unix timestamp when program started
	ExitStatus int    // Last exit status code
	Output     string // Last output from program
}

// SystemMetrics holds system service metrics (load, CPU, memory, swap).
type SystemMetrics struct {
	// Load Average
	Load1Min  float64 // 1-minute load average
	Load5Min  float64 // 5-minute load average
	Load15Min float64 // 15-minute load average

	// CPU Usage Breakdown (all values are percentages)
	CPUUser     float64 // User CPU %
	CPUSystem   float64 // System CPU %
	CPUNice     float64 // Nice CPU %
	CPUWait     float64 // I/O Wait % (Linux only)
	CPUHardIRQ  float64 // Hard IRQ % (Linux: hardirq, FreeBSD: interrupt)
	CPUSoftIRQ  float64 // Soft IRQ % (Linux only)
	CPUSteal    float64 // Steal % (Linux only, virtualization)
	CPUGuest    float64 // Guest % (Linux only, virtualization)
	CPUGuestNice float64 // Guest Nice % (Linux only, virtualization)

	// Memory Usage
	MemoryPercent float64 // Memory usage percentage
	MemoryKB      int64   // Memory usage in KB

	// Swap Usage
	SwapPercent float64 // Swap usage percentage
	SwapKB      int64   // Swap usage in KB
}

// RemoteHostMetrics holds remote host service metrics (ICMP, Port, Unix socket).
type RemoteHostMetrics struct {
	// ICMP Metrics
	ICMPType           string  // Ping type (e.g., "echo")
	ICMPResponseTimeMs float64 // Response time in milliseconds

	// Port Metrics
	PortHostname       string  // Target hostname for port monitoring
	PortNumber         int     // Port number
	PortProtocol       string  // Protocol (e.g., "TCP" or "UDP")
	PortType           string  // Port type
	PortResponseTimeMs float64 // Response time in milliseconds

	// Unix Socket Metrics
	UnixPath           string  // Unix socket path
	UnixProtocol       string  // Protocol
	UnixResponseTimeMs float64 // Response time in milliseconds
}

// =============================================================================
// GLOBAL VARIABLES
// =============================================================================

//go:embed templates/*.html
var templatesFS embed.FS

// templates holds parsed HTML templates.
//
// template.Template represents a parsed HTML template.
// We load templates at startup and reuse them for each request.
var templates *template.Template

// db holds the database connection for web handlers.
//
// Set this using SetDB() before starting the web server.
var db *sql.DB

// =============================================================================
// INITIALIZATION
// =============================================================================

// SetDB sets the database connection for web handlers.
//
// This must be called before starting the web server.
// It allows the web package to query the database.
//
// Example:
//
//	db, _ := sql.Open("sqlite3", "cmonit.db")
//	web.SetDB(db)
func SetDB(database *sql.DB) {
	db = database
}

// InitTemplates loads and parses HTML templates from the templates directory.
//
// template.Must() panics if template parsing fails, which is appropriate
// at startup time - we can't run without templates.
//
// template.ParseGlob() reads all files matching the pattern.
// The pattern "templates/*.html" matches all .html files in templates/.
//
// Returns an error if templates can't be loaded.
func InitTemplates() error {
	// Create custom template functions
	funcMap := template.FuncMap{
		"divf": func(a, b interface{}) float64 {
			// Convert interfaces to float64
			var af, bf float64
			switch v := a.(type) {
			case float64:
				af = v
			case int:
				af = float64(v)
			case int64:
				af = float64(v)
			}
			switch v := b.(type) {
			case float64:
				bf = v
			case int:
				bf = float64(v)
			case int64:
				bf = float64(v)
			}
			if bf == 0 {
				return 0
			}
			return af / bf
		},
		"sub": func(a, b interface{}) float64 {
			// Convert interfaces to float64
			var af, bf float64
			switch v := a.(type) {
			case float64:
				af = v
			case int:
				af = float64(v)
			case int64:
				af = float64(v)
			}
			switch v := b.(type) {
			case float64:
				bf = v
			case int:
				bf = float64(v)
			case int64:
				bf = float64(v)
			}
			return af - bf
		},
		"formatDuration": func(seconds *int64) string {
			if seconds == nil {
				return "N/A"
			}
			days := *seconds / 86400
			hours := (*seconds % 86400) / 3600
			minutes := (*seconds % 3600) / 60
			if days > 0 {
				return fmt.Sprintf("%dd %dh %dm", days, hours, minutes)
			} else if hours > 0 {
				return fmt.Sprintf("%dh %dm", hours, minutes)
			}
			return fmt.Sprintf("%dm", minutes)
		},
		"formatTimestamp": func(ts *int64) string {
			if ts == nil {
				return "N/A"
			}
			t := time.Unix(*ts, 0)
			return t.Format("Jan 2, 15:04")
		},
		"deref": func(f *float64) float64 {
			if f == nil {
				return 0
			}
			return *f
		},
	}

	// Parse all .html files in templates/ directory with custom functions
	//
	// template.New creates a new template with the given name
	// .Funcs adds custom functions to the template
	// .ParseFS loads all matching files from the embedded filesystem
	var err error
	templates, err = template.New("").Funcs(funcMap).ParseFS(templatesFS, "templates/*.html")
	if err != nil {
		return err
	}

	log.Printf("[INFO] Loaded HTML templates from embedded filesystem")
	return nil
}

// =============================================================================
// HTTP HANDLERS
// =============================================================================

// HandleDashboard serves the main dashboard page.
//
// HTTP Handler function signature:
// - w http.ResponseWriter: Where we write the HTTP response
// - r *http.Request: The incoming HTTP request
//
// This function:
// 1. Queries database for all hosts and services
// 2. Builds DashboardData struct
// 3. Renders the dashboard.html template
func HandleDashboard(w http.ResponseWriter, r *http.Request) {
	// Only allow GET requests
	//
	// r.Method contains the HTTP method ("GET", "POST", etc.)
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Query all hosts and their services from the database
	data, err := getDashboardData()
	if err != nil {
		// Log the error for debugging
		log.Printf("[ERROR] Failed to get dashboard data: %v", err)

		// Return 500 Internal Server Error to client
		http.Error(w, "Failed to load dashboard data", http.StatusInternalServerError)
		return
	}

	// Set response headers
	//
	// Content-Type tells the browser this is HTML
	w.Header().Set("Content-Type", "text/html; charset=utf-8")

	// Render the template
	//
	// templates.ExecuteTemplate() does:
	// 1. Finds the template named "dashboard.html"
	// 2. Executes it with 'data' as input
	// 3. Writes output to 'w' (the HTTP response)
	//
	// Template can access data fields like {{.Hosts}}, {{.LastUpdate}}
	err = templates.ExecuteTemplate(w, "dashboard.html", data)
	if err != nil {
		log.Printf("[ERROR] Failed to render template: %v", err)
		// Can't change HTTP status here - headers already sent
	}
}

// =============================================================================
// DATABASE QUERIES
// =============================================================================

// getDashboardData queries the database and builds DashboardData.
//
// This function:
// 1. Queries all hosts from the hosts table
// 2. For each host, queries its services from the services table
// 3. Builds the data structure for the template
//
// Returns DashboardData and any error encountered.
func getDashboardData() (*DashboardData, error) {
	// Query all hosts
	//
	// SQL query selects:
	// - id: Unique host identifier
	// - hostname: Display name
	// - version: Monit version string
	// - os_name, os_release, machine: Platform info
	// - cpu_count, total_memory, total_swap: Hardware specs
	// - system_uptime, boottime: System timing info
	// - last_seen: Timestamp of last update
	//
	// ORDER BY last_seen DESC: Show most recently seen hosts first
	const hostsQuery = `
		SELECT id, hostname, version, os_name, os_release, machine,
		       cpu_count, total_memory, total_swap, system_uptime, boottime, last_seen
		FROM hosts
		ORDER BY last_seen DESC
	`

	// db.Query() executes the SQL and returns:
	// - *sql.Rows: Result set we can iterate over
	// - error: Any SQL errors
	rows, err := db.Query(hostsQuery)
	if err != nil {
		return nil, err
	}
	// defer ensures rows.Close() runs when function returns
	//
	// This is CRITICAL to:
	// - Release database locks
	// - Free memory
	// - Close database cursors
	defer rows.Close()

	// Build slice to hold all hosts
	//
	// []HostWithServices creates an empty slice
	// We'll append to this as we read rows
	var hosts []HostWithServices

	// Iterate over result rows
	//
	// rows.Next() returns:
	// - true: There's another row to read
	// - false: No more rows (or error occurred)
	for rows.Next() {
		var host HostWithServices

		// Scan copies columns from current row into variables
		//
		// Order must match the SELECT clause
		// Use pointers for optional fields (can be NULL in database)
		err := rows.Scan(
			&host.ID,
			&host.Hostname,
			&host.Version,
			&host.OSName,
			&host.OSRelease,
			&host.Machine,
			&host.CPUCount,
			&host.TotalMemory,
			&host.TotalSwap,
			&host.SystemUptime,
			&host.Boottime,
			&host.LastSeen,
		)
		if err != nil {
			return nil, err
		}

		// Check if host is stale (not seen in 5+ minutes)
		//
		// time.Since() returns duration since the given time
		// A host is stale if last_seen is more than 5 minutes ago
		host.IsStale = time.Since(host.LastSeen) > 5*time.Minute

		// Query services for this host
		host.Services, err = getServicesForHost(host.ID)
		if err != nil {
			// Log error but continue with other hosts
			log.Printf("[ERROR] Failed to get services for host %s: %v", host.ID, err)
			host.Services = []Service{} // Empty slice
		}

		// Append this host to the slice
		//
		// append() adds an element to a slice
		// In Go, we must reassign the result back to the variable
		hosts = append(hosts, host)
	}

	// Check for errors during iteration
	//
	// rows.Err() returns any error that occurred during Next()
	if err = rows.Err(); err != nil {
		return nil, err
	}

	// Build and return the dashboard data
	return &DashboardData{
		Hosts:      hosts,
		LastUpdate: time.Now(),
	}, nil
}

// getServicesForHost queries all services for a given host.
//
// Parameters:
// - hostID: The host's unique identifier
//
// Returns:
// - []Service: Slice of services for this host
// - error: Any database error
func getServicesForHost(hostID string) ([]Service, error) {
	// Query services for this host
	//
	// WHERE clause filters to only this host's services
	// ORDER BY type, name: Group by type, then alphabetically
	// Include process metrics (pid, cpu_percent, memory_percent, memory_kb) for process services
	const servicesQuery = `
		SELECT name, type, status, monitor, pid, cpu_percent, memory_percent, memory_kb, collected_at
		FROM services
		WHERE host_id = ?
		ORDER BY type, name
	`

	// Execute query with hostID as parameter
	//
	// The ? placeholder is replaced with hostID
	// This prevents SQL injection attacks
	rows, err := db.Query(servicesQuery, hostID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var services []Service

	for rows.Next() {
		var svc Service

		// Scan all fields including process metrics (which may be NULL)
		err := rows.Scan(
			&svc.Name,
			&svc.Type,
			&svc.Status,
			&svc.Monitor,
			&svc.PID,
			&svc.CPUPercent,
			&svc.MemoryPercent,
			&svc.MemoryKB,
			&svc.CollectedAt,
		)
		if err != nil {
			return nil, err
		}

		// Convert numeric type to human-readable string
		svc.TypeName = getServiceTypeName(svc.Type)

		// Convert numeric status to human-readable string and color
		svc.StatusName, svc.StatusColor = getServiceStatusInfo(svc.Status)

		services = append(services, svc)
	}

	if err = rows.Err(); err != nil {
		return nil, err
	}

	return services, nil
}

// =============================================================================
// HELPER FUNCTIONS
// =============================================================================

// getServiceTypeName converts Monit service type number to name.
//
// Monit service types:
// - 0: Filesystem
// - 1: Directory
// - 2: File
// - 3: Process
// - 4: Remote host
// - 5: System
// - 6: Fifo
// - 7: Program
// - 8: Network
//
// These constants come from monit-5.35.2/src/monit.h
func getServiceTypeName(serviceType int) string {
	switch serviceType {
	case 0:
		return "Filesystem"
	case 1:
		return "Directory"
	case 2:
		return "File"
	case 3:
		return "Process"
	case 4:
		return "Remote Host"
	case 5:
		return "System"
	case 6:
		return "Fifo"
	case 7:
		return "Program"
	case 8:
		return "Network"
	default:
		return "Unknown"
	}
}

// getServiceStatusInfo converts status number to name and color.
//
// Monit status values:
// - 0: OK (green)
// - 1: Warning (yellow)
// - 2: Critical (red)
//
// Returns:
// - statusName: Human-readable status ("OK", "Warning", "Critical")
// - colorClass: CSS class for styling ("green", "yellow", "red")
func getServiceStatusInfo(status int) (string, string) {
	switch status {
	case 0:
		return "OK", "green"
	case 1:
		return "Warning", "yellow"
	case 2:
		return "Critical", "red"
	default:
		return "Unknown", "gray"
	}
}

// StatusMessage returns a human-readable status message for the service.
//
// Monit status codes (from monit-5.35.2/src/event.h Event_Type enum):
// These are bit flags that can be combined, but typically appear individually.
func (s *Service) StatusMessage() string {
	switch s.Status {
	case 0:
		return "OK"
	case 0x1: // Event_Checksum
		return "Checksum failed"
	case 0x2: // Event_Resource
		return "Resource limit matched"
	case 0x4: // Event_Timeout
		return "Timeout"
	case 0x8: // Event_Timestamp
		return "Timestamp changed"
	case 0x10: // Event_Size
		return "Size changed"
	case 0x20: // Event_Connection
		return "Connection failed"
	case 0x40: // Event_Permission
		return "Permission failed"
	case 0x80: // Event_Uid
		return "UID failed"
	case 0x100: // Event_Gid
		return "GID failed"
	case 0x200: // Event_NonExist
		return "Does not exist"
	case 0x400: // Event_Invalid
		return "Invalid type"
	case 0x800: // Event_Data
		return "Data access error"
	case 0x1000: // Event_Exec
		return "Execution failed"
	case 0x2000: // Event_FsFlag
		return "Filesystem flags changed"
	case 0x4000: // Event_Icmp
		return "ICMP failed"
	case 0x8000: // Event_Content
		return "Content match failed"
	case 0x10000: // Event_Instance
		return "Instance changed"
	case 0x20000: // Event_Action
		return "Action done"
	case 0x40000: // Event_Pid
		return "PID changed"
	case 0x80000: // Event_PPid
		return "PPID changed"
	case 0x100000: // Event_Heartbeat
		return "Heartbeat failed"
	case 0x200000: // Event_Status
		return "Status changed"
	case 0x400000: // Event_Uptime
		return "Uptime failed"
	case 0x800000: // Event_Link
		return "Link down"
	case 0x1000000: // Event_Speed
		return "Speed changed"
	case 0x2000000: // Event_Saturation
		return "Saturation exceeded"
	case 0x4000000: // Event_ByteIn
		return "Download bytes exceeded"
	case 0x8000000: // Event_ByteOut
		return "Upload bytes exceeded"
	case 0x10000000: // Event_PacketIn
		return "Download packets exceeded"
	case 0x20000000: // Event_PacketOut
		return "Upload packets exceeded"
	case 0x40000000: // Event_Exist
		return "Exists"
	default:
		return fmt.Sprintf("Unknown status (%d)", s.Status)
	}
}
