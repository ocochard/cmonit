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
	ID          string    // Unique host ID from Monit
	Hostname    string    // Display name (e.g., "bigone")
	Version     string    // Monit version
	LastSeen    time.Time // Last successful update
	Services    []Service // All services on this host
	IsStale     bool      // True if not seen in 5+ minutes
}

// Service represents a monitored service.
//
// Maps to the services table in the database.
type Service struct {
	Name        string    // Service name (e.g., "sshd", "nginx")
	Type        int       // Service type (3=process, 5=system, 7=program)
	TypeName    string    // Human-readable type ("Process", "System", etc.)
	Status      int       // 0=ok, 1=warning, 2=critical
	StatusName  string    // Human-readable status ("OK", "Warning", "Critical")
	StatusColor string    // CSS color class ("green", "yellow", "red")
	Monitor     int       // 0=not monitored, 1=monitored
	CollectedAt time.Time // When metrics were last collected
}

// =============================================================================
// GLOBAL VARIABLES
// =============================================================================

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
	// Parse all .html files in templates/ directory
	//
	// template.ParseGlob returns:
	// - *template.Template: The parsed templates
	// - error: Any parsing errors
	var err error
	templates, err = template.ParseGlob("templates/*.html")
	if err != nil {
		return err
	}

	log.Printf("[INFO] Loaded HTML templates from templates/")
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
	// - last_seen: Timestamp of last update
	//
	// ORDER BY last_seen DESC: Show most recently seen hosts first
	const hostsQuery = `
		SELECT id, hostname, version, last_seen
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
		// Order must match the SELECT clause:
		// - 1st column (id) -> host.ID
		// - 2nd column (hostname) -> host.Hostname
		// - 3rd column (version) -> host.Version
		// - 4th column (last_seen) -> host.LastSeen
		err := rows.Scan(&host.ID, &host.Hostname, &host.Version, &host.LastSeen)
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
	const servicesQuery = `
		SELECT name, type, status, monitor, collected_at
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

		err := rows.Scan(&svc.Name, &svc.Type, &svc.Status, &svc.Monitor, &svc.CollectedAt)
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
