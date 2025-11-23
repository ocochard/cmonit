// Package web provides M/Monit-compatible API handlers.
//
// This file implements M/Monit HTTP API endpoints for compatibility with
// existing tools and integrations that expect M/Monit's REST API.
//
// API Documentation: https://mmonit.com/documentation/http-api/
package web

import (
	"database/sql" //SQL database
	"encoding/json" // JSON encoding/decoding
	"log"           // Logging
	"net/http"      // HTTP server
	"strings"       // String manipulation
	"time"          // Time handling
)

// =============================================================================
// M/MONIT API DATA STRUCTURES
// =============================================================================

// MMHostSummary represents a host in the M/Monit API format.
//
// This matches the M/Monit JSON structure for host listings.
type MMHostSummary struct {
	ID          string `json:"id"`          // Host unique identifier
	Hostname    string `json:"hostname"`    // Host name
	Status      int    `json:"status"`      // Overall status (0=OK, 1=warning, 2=critical, 3=unknown)
	Services    int    `json:"services"`    // Number of services
	Platform    string `json:"platform"`    // Operating system
	LastSeen    string `json:"lastseen"`    // Last contact time (ISO 8601)
	MonitUptime int64  `json:"monituptime"` // Monit daemon uptime in seconds
	CPUPercent  int    `json:"cpupercent"`  // CPU usage percentage
	MemPercent  int    `json:"mempercent"`  // Memory usage percentage
}

// MMHostDetail represents detailed host information in M/Monit API format.
//
// This is returned by GET /status/hosts/:id
type MMHostDetail struct {
	ID              string           `json:"id"`
	Hostname        string           `json:"hostname"`
	Status          int              `json:"status"`
	Platform        string           `json:"platform"`
	PlatformVersion string           `json:"platformversion,omitempty"`
	CPUCount        int              `json:"cpucount,omitempty"`
	Memory          int64            `json:"memory,omitempty"` // Total memory in bytes
	Uptime          int64            `json:"uptime,omitempty"` // System uptime in seconds
	MonitUptime     int64            `json:"monituptime"`
	MonitVersion    string           `json:"monitversion,omitempty"`
	LastSeen        string           `json:"lastseen"`
	Services        []MMServiceBrief `json:"services,omitempty"`
}

// MMServiceBrief represents a service in brief format for host detail view.
type MMServiceBrief struct {
	Name    string `json:"name"`
	Type    int    `json:"type"`
	Status  int    `json:"status"`
	Monitor int    `json:"monitor"`
}

// MMServiceDetail represents detailed service information.
//
// This is returned by GET /status/hosts/:id/services/:name
type MMServiceDetail struct {
	Name         string                 `json:"name"`
	Type         int                    `json:"type"`
	Status       int                    `json:"status"`
	Monitor      int                    `json:"monitor"`
	PendingAction int                   `json:"pendingaction,omitempty"`
	CPU          *MMServiceCPU          `json:"cpu,omitempty"`
	Memory       *MMServiceMemory       `json:"memory,omitempty"`
	System       *MMServiceSystem       `json:"system,omitempty"`
	Collected    string                 `json:"collected,omitempty"`
}

// MMServiceCPU represents CPU metrics for a service.
type MMServiceCPU struct {
	Percent float64 `json:"percent"`
	Total   float64 `json:"total,omitempty"`
}

// MMServiceMemory represents memory metrics for a service.
type MMServiceMemory struct {
	Kilobyte        int64   `json:"kilobyte"`
	Percent         float64 `json:"percent"`
	PercentTotal    float64 `json:"percenttotal,omitempty"`
}

// MMServiceSystem represents system-level metrics.
type MMServiceSystem struct {
	CPU    *MMSystemCPU    `json:"cpu,omitempty"`
	Memory *MMSystemMemory `json:"memory,omitempty"`
	Load   *MMSystemLoad   `json:"load,omitempty"`
}

// MMSystemCPU represents system-wide CPU metrics.
type MMSystemCPU struct {
	User   float64 `json:"user"`
	System float64 `json:"system"`
	Wait   float64 `json:"wait,omitempty"`
}

// MMSystemMemory represents system-wide memory metrics.
type MMSystemMemory struct {
	Percent int64 `json:"percent"`
	Kilobyte int64 `json:"kilobyte"`
}

// MMSystemLoad represents system load averages.
type MMSystemLoad struct {
	Avg01 float64 `json:"avg01"` // 1-minute load average
	Avg05 float64 `json:"avg05"` // 5-minute load average
	Avg15 float64 `json:"avg15"` // 15-minute load average
}

// MMEvent represents an event in M/Monit API format.
type MMEvent struct {
	ID          int64  `json:"id"`
	HostID      string `json:"hostid"`
	Hostname    string `json:"hostname"`
	Service     string `json:"service"`
	Type        int    `json:"type"`
	Message     string `json:"message"`
	Timestamp   string `json:"timestamp"`   // ISO 8601 format
}

// MMEventsResponse represents the events list API response.
type MMEventsResponse struct {
	Records int        `json:"records"` // Total number of records
	Events  []MMEvent  `json:"events"`  // Array of events
}

// MMAdminHostsResponse represents the admin hosts list API response.
type MMAdminHostsResponse struct {
	Records int             `json:"records"` // Total number of records
	Hosts   []MMHostSummary `json:"hosts"`   // Array of hosts
}

// MMErrorResponse represents an error response.
type MMErrorResponse struct {
	Error   string `json:"error"`
	Message string `json:"message,omitempty"`
}

// =============================================================================
// STATUS API HANDLERS
// =============================================================================

// HandleMMStatusHosts returns a list of all hosts.
//
// GET /status/hosts
//
// This is equivalent to M/Monit's GET /status/hosts/list API.
// Returns a summary of all monitored hosts with their current status.
func HandleMMStatusHosts(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		respondMMError(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	hosts, err := getMMHostsSummary()
	if err != nil {
		log.Printf("[ERROR] Failed to get hosts summary: %v", err)
		respondMMError(w, "Failed to retrieve hosts", http.StatusInternalServerError)
		return
	}

	respondJSON(w, hosts, http.StatusOK)
}

// HandleMMStatusHost returns detailed information about a specific host.
//
// GET /status/hosts/{hostid}
//
// Returns detailed host information including all services.
func HandleMMStatusHost(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		respondMMError(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Extract host ID from URL path
	// URL format: /status/hosts/{hostid}
	path := strings.TrimPrefix(r.URL.Path, "/status/hosts/")
	hostID := strings.Split(path, "/")[0]

	if hostID == "" {
		respondMMError(w, "Missing host ID", http.StatusBadRequest)
		return
	}

	host, err := getMMHostDetail(hostID)
	if err != nil {
		if err == sql.ErrNoRows {
			respondMMError(w, "Host not found", http.StatusNotFound)
			return
		}
		log.Printf("[ERROR] Failed to get host detail: %v", err)
		respondMMError(w, "Failed to retrieve host", http.StatusInternalServerError)
		return
	}

	respondJSON(w, host, http.StatusOK)
}

// HandleMMStatusServices returns all services for a host.
//
// GET /status/hosts/{hostid}/services
//
// Returns a list of all services for the specified host.
func HandleMMStatusServices(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		respondMMError(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Extract host ID from URL path
	path := strings.TrimPrefix(r.URL.Path, "/status/hosts/")
	parts := strings.Split(path, "/")
	if len(parts) < 2 {
		respondMMError(w, "Invalid URL format", http.StatusBadRequest)
		return
	}
	hostID := parts[0]

	services, err := getMMServicesForHost(hostID)
	if err != nil {
		log.Printf("[ERROR] Failed to get services: %v", err)
		respondMMError(w, "Failed to retrieve services", http.StatusInternalServerError)
		return
	}

	respondJSON(w, services, http.StatusOK)
}

// =============================================================================
// EVENTS API HANDLERS
// =============================================================================

// HandleMMEventsList returns a list of events.
//
// GET /events/list
//
// Query parameters:
//   - hostid: Filter by host ID (optional)
//   - limit: Maximum number of events to return (default: 100)
//   - offset: Offset for pagination (default: 0)
func HandleMMEventsList(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		respondMMError(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	query := r.URL.Query()
	hostID := query.Get("hostid")
	limit := 100
	offset := 0

	events, totalRecords, err := getMMEvents(hostID, limit, offset)
	if err != nil {
		log.Printf("[ERROR] Failed to get events: %v", err)
		respondMMError(w, "Failed to retrieve events", http.StatusInternalServerError)
		return
	}

	response := MMEventsResponse{
		Records: totalRecords,
		Events:  events,
	}

	respondJSON(w, response, http.StatusOK)
}

// HandleMMEventsGet returns a specific event by ID.
//
// GET /events/get/{id}
func HandleMMEventsGet(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		respondMMError(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Extract event ID from URL path
	path := strings.TrimPrefix(r.URL.Path, "/events/get/")
	eventIDStr := strings.Split(path, "/")[0]

	if eventIDStr == "" {
		respondMMError(w, "Missing event ID", http.StatusBadRequest)
		return
	}

	event, err := getMMEventByID(eventIDStr)
	if err != nil {
		if err == sql.ErrNoRows {
			respondMMError(w, "Event not found", http.StatusNotFound)
			return
		}
		log.Printf("[ERROR] Failed to get event: %v", err)
		respondMMError(w, "Failed to retrieve event", http.StatusInternalServerError)
		return
	}

	respondJSON(w, event, http.StatusOK)
}

// =============================================================================
// ADMIN API HANDLERS
// =============================================================================

// HandleMMAdminHosts handles host administration endpoints.
//
// GET /admin/hosts - List all hosts
// POST /admin/hosts - Add a new host (not implemented in collector mode)
// DELETE /admin/hosts/{id} - Remove a host (not implemented in collector mode)
func HandleMMAdminHosts(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		handleMMAdminHostsList(w, r)
	case http.MethodPost:
		respondMMError(w, "Adding hosts manually not supported in collector mode", http.StatusNotImplemented)
	case http.MethodDelete:
		respondMMError(w, "Deleting hosts not supported in collector mode", http.StatusNotImplemented)
	default:
		respondMMError(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

// handleMMAdminHostsList returns a list of all hosts for administration.
//
// GET /admin/hosts
func handleMMAdminHostsList(w http.ResponseWriter, r *http.Request) {
	hosts, err := getMMHostsSummary()
	if err != nil {
		log.Printf("[ERROR] Failed to get hosts: %v", err)
		respondMMError(w, "Failed to retrieve hosts", http.StatusInternalServerError)
		return
	}

	response := MMAdminHostsResponse{
		Records: len(hosts),
		Hosts:   hosts,
	}

	respondJSON(w, response, http.StatusOK)
}

// =============================================================================
// DATABASE QUERY FUNCTIONS
// =============================================================================

// getMMHostsSummary retrieves a summary of all hosts.
func getMMHostsSummary() ([]MMHostSummary, error) {
	const query = `
		SELECT id, hostname, os_name, os_release, machine, version,
		       last_seen, monit_uptime
		FROM hosts
		ORDER BY hostname
	`

	rows, err := db.Query(query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var hosts []MMHostSummary
	for rows.Next() {
		var h MMHostSummary
		var lastSeen time.Time
		var monitUptime sql.NullInt64
		var version sql.NullString
		var osName, osRelease, machine sql.NullString

		err := rows.Scan(&h.ID, &h.Hostname, &osName, &osRelease, &machine, &version,
			&lastSeen, &monitUptime)
		if err != nil {
			return nil, err
		}

		// Build platform string from os_name, os_release, and machine
		h.Platform = buildPlatformString(osName, osRelease, machine)

		h.LastSeen = lastSeen.Format(time.RFC3339)
		if monitUptime.Valid {
			h.MonitUptime = monitUptime.Int64
		}

		// Get latest CPU and memory percentages from metrics table
		h.CPUPercent = getLatestSystemCPUPercent(h.ID)
		h.MemPercent = getLatestSystemMemoryPercent(h.ID)

		// Get service count for this host
		serviceCount, _ := getServiceCount(h.ID)
		h.Services = serviceCount

		// Determine overall status based on service statuses
		h.Status = getOverallHostStatus(h.ID)

		hosts = append(hosts, h)
	}

	return hosts, rows.Err()
}

// getMMHostDetail retrieves detailed information about a specific host.
func getMMHostDetail(hostID string) (*MMHostDetail, error) {
	const query = `
		SELECT id, hostname, platform, platform_version, cpu_count, memory,
		       uptime, monit_uptime, version, last_seen
		FROM hosts
		WHERE id = ?
	`

	var h MMHostDetail
	var lastSeen time.Time
	var platformVersion, cpuCount, memory, uptime, monitUptime sql.NullString
	var version sql.NullString

	err := db.QueryRow(query, hostID).Scan(
		&h.ID, &h.Hostname, &h.Platform, &platformVersion, &cpuCount,
		&memory, &uptime, &monitUptime, &version, &lastSeen,
	)
	if err != nil {
		return nil, err
	}

	h.LastSeen = lastSeen.Format(time.RFC3339)
	if platformVersion.Valid {
		h.PlatformVersion = platformVersion.String
	}
	if version.Valid {
		h.MonitVersion = version.String
	}

	// Get services for this host
	services, err := getMMServicesForHost(hostID)
	if err != nil {
		return nil, err
	}
	h.Services = services
	h.Status = getOverallHostStatus(hostID)

	return &h, nil
}

// getMMServicesForHost retrieves all services for a host.
func getMMServicesForHost(hostID string) ([]MMServiceBrief, error) {
	const query = `
		SELECT name, type, status, monitor
		FROM services
		WHERE host_id = ?
		ORDER BY name
	`

	rows, err := db.Query(query, hostID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var services []MMServiceBrief
	for rows.Next() {
		var s MMServiceBrief
		err := rows.Scan(&s.Name, &s.Type, &s.Status, &s.Monitor)
		if err != nil {
			return nil, err
		}
		services = append(services, s)
	}

	return services, rows.Err()
}

// getServiceCount returns the number of services for a host.
func getServiceCount(hostID string) (int, error) {
	const query = `SELECT COUNT(*) FROM services WHERE host_id = ?`

	var count int
	err := db.QueryRow(query, hostID).Scan(&count)
	return count, err
}

// getOverallHostStatus determines overall host status based on service statuses.
//
// Returns: 0=OK, 1=warning, 2=critical, 3=unknown
func getOverallHostStatus(hostID string) int {
	const query = `
		SELECT status
		FROM services
		WHERE host_id = ?
	`

	rows, err := db.Query(query, hostID)
	if err != nil {
		return 3 // Unknown
	}
	defer rows.Close()

	hasError := false
	hasWarning := false

	for rows.Next() {
		var status int
		if err := rows.Scan(&status); err != nil {
			continue
		}

		// Status values: 0=running/OK, other values indicate problems
		if status != 0 {
			hasError = true
		}
	}

	if hasError {
		return 2 // Critical
	}
	if hasWarning {
		return 1 // Warning
	}
	return 0 // OK
}

// getMMEvents retrieves events with optional filtering.
func getMMEvents(hostID string, limit, offset int) ([]MMEvent, int, error) {
	var query string
	var args []interface{}

	if hostID != "" {
		query = `
			SELECT id, host_id, service_name, event_type, message, created_at
			FROM events
			WHERE host_id = ?
			ORDER BY created_at DESC
			LIMIT ? OFFSET ?
		`
		args = []interface{}{hostID, limit, offset}
	} else {
		query = `
			SELECT id, host_id, service_name, event_type, message, created_at
			FROM events
			ORDER BY created_at DESC
			LIMIT ? OFFSET ?
		`
		args = []interface{}{limit, offset}
	}

	rows, err := db.Query(query, args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	var events []MMEvent
	for rows.Next() {
		var e MMEvent
		var createdAt time.Time
		err := rows.Scan(&e.ID, &e.HostID, &e.Service, &e.Type, &e.Message, &createdAt)
		if err != nil {
			return nil, 0, err
		}

		e.Timestamp = createdAt.Format(time.RFC3339)

		// Get hostname for this event
		hostname, _ := getHostname(e.HostID)
		e.Hostname = hostname

		events = append(events, e)
	}

	// Get total count
	var countQuery string
	var countArgs []interface{}
	if hostID != "" {
		countQuery = `SELECT COUNT(*) FROM events WHERE host_id = ?`
		countArgs = []interface{}{hostID}
	} else {
		countQuery = `SELECT COUNT(*) FROM events`
		countArgs = []interface{}{}
	}

	var totalRecords int
	err = db.QueryRow(countQuery, countArgs...).Scan(&totalRecords)
	if err != nil {
		totalRecords = len(events)
	}

	return events, totalRecords, rows.Err()
}

// getMMEventByID retrieves a specific event by ID.
func getMMEventByID(eventIDStr string) (*MMEvent, error) {
	const query = `
		SELECT id, host_id, service_name, event_type, message, created_at
		FROM events
		WHERE id = ?
	`

	var e MMEvent
	var createdAt time.Time
	err := db.QueryRow(query, eventIDStr).Scan(
		&e.ID, &e.HostID, &e.Service, &e.Type, &e.Message, &createdAt,
	)
	if err != nil {
		return nil, err
	}

	e.Timestamp = createdAt.Format(time.RFC3339)

	// Get hostname
	hostname, _ := getHostname(e.HostID)
	e.Hostname = hostname

	return &e, nil
}

// =============================================================================
// HELPER FUNCTIONS
// =============================================================================

// buildPlatformString constructs a platform string from OS components.
//
// Combines os_name, os_release, and machine into a single platform string
// like "FreeBSD 16.0-CURRENT (amd64)".
func buildPlatformString(osName, osRelease, machine sql.NullString) string {
	parts := []string{}

	if osName.Valid && osName.String != "" {
		parts = append(parts, osName.String)
	}
	if osRelease.Valid && osRelease.String != "" {
		parts = append(parts, osRelease.String)
	}

	platform := ""
	if len(parts) > 0 {
		platform = parts[0]
		if len(parts) > 1 {
			platform += " " + parts[1]
		}
	}

	if machine.Valid && machine.String != "" {
		if platform != "" {
			platform += " (" + machine.String + ")"
		} else {
			platform = machine.String
		}
	}

	return platform
}

// getLatestSystemCPUPercent retrieves the latest system CPU percentage from metrics.
//
// Queries the metrics table for the most recent system_cpu percent value
// for the given host. Returns 0 if no data is available.
func getLatestSystemCPUPercent(hostID string) int {
	const query = `
		SELECT value
		FROM metrics
		WHERE host_id = ? AND metric_type = 'system_cpu' AND metric_name = 'percent'
		ORDER BY collected_at DESC
		LIMIT 1
	`

	var percent float64
	err := db.QueryRow(query, hostID).Scan(&percent)
	if err != nil {
		return 0 // No data or error
	}

	return int(percent)
}

// getLatestSystemMemoryPercent retrieves the latest system memory percentage from metrics.
//
// Queries the metrics table for the most recent memory percent value
// for the given host. Returns 0 if no data is available.
func getLatestSystemMemoryPercent(hostID string) int {
	const query = `
		SELECT value
		FROM metrics
		WHERE host_id = ? AND metric_type = 'memory' AND metric_name = 'percent'
		ORDER BY collected_at DESC
		LIMIT 1
	`

	var percent float64
	err := db.QueryRow(query, hostID).Scan(&percent)
	if err != nil {
		return 0 // No data or error
	}

	return int(percent)
}

// respondMMError sends an M/Monit-compatible error response.
func respondMMError(w http.ResponseWriter, message string, statusCode int) {
	response := MMErrorResponse{
		Error:   http.StatusText(statusCode),
		Message: message,
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)
	json.NewEncoder(w).Encode(response)
}
