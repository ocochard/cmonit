// Package web provides API handlers for metrics data.
//
// This file contains REST API endpoints that return JSON data
// for use by the dashboard's JavaScript charts.
package web

import (
	"encoding/json" // JSON encoding/decoding
	"log"           // Logging
	"net/http"      // HTTP server
	"strconv"       // String conversion (string to int, etc.)
	"time"          // Time handling

	"github.com/ocochard/cmonit/internal/control" // Monit control API client
)

// =============================================================================
// DATA STRUCTURES FOR JSON RESPONSES
// =============================================================================

// MetricsResponse is the JSON response for metrics API.
//
// This structure will be converted to JSON and sent to the client.
// The client's JavaScript will parse this and create charts.
//
// JSON field names use lowercase for consistency with JavaScript conventions.
type MetricsResponse struct {
	// Host information
	HostID   string `json:"host_id"`   // Unique host identifier
	Hostname string `json:"hostname"`  // Human-readable hostname
	Service  string `json:"service"`   // Service name (e.g., "bigone" for system)

	// Time range information
	StartTime time.Time `json:"start_time"` // Start of time range
	EndTime   time.Time `json:"end_time"`   // End of time range

	// Metrics data
	// Each MetricSeries contains timestamps and values for one metric
	Metrics []MetricSeries `json:"metrics"`
}

// MetricSeries represents time-series data for a single metric.
//
// Example: CPU usage over time
// - Name: "cpu_user"
// - Timestamps: [t1, t2, t3, ...]
// - Values: [10.5, 12.3, 15.7, ...]
type MetricSeries struct {
	Name       string    `json:"name"`       // Metric name (e.g., "load_avg01")
	Type       string    `json:"type"`       // Metric type (e.g., "load", "cpu")
	Timestamps []string  `json:"timestamps"` // ISO 8601 timestamps
	Values     []float64 `json:"values"`     // Metric values
}

// MetricPoint represents a single data point.
//
// Used internally while querying the database.
// We'll group these into MetricSeries before returning JSON.
type MetricPoint struct {
	Timestamp time.Time // When the metric was collected
	Value     float64   // The metric value
}

// =============================================================================
// API HANDLERS
// =============================================================================

// HandleMetricsAPI serves metrics data as JSON.
//
// URL format:
//   /api/metrics?host_id=xxx&service=xxx&range=1h
//
// Query parameters:
//   - host_id (required): Host identifier
//   - service (required): Service name
//   - range (optional): Time range (1h, 6h, 24h, 7d, 30d), default: 24h
//
// Returns JSON with timestamps and values for all metrics of the service.
func HandleMetricsAPI(w http.ResponseWriter, r *http.Request) {
	// Only allow GET requests
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Parse query parameters
	//
	// r.URL.Query() returns a map of query parameters
	// .Get("key") retrieves a parameter value, returns "" if not present
	query := r.URL.Query()
	hostID := query.Get("host_id")
	service := query.Get("service")
	rangeStr := query.Get("range")

	// Validate required parameters
	if hostID == "" {
		http.Error(w, "Missing host_id parameter", http.StatusBadRequest)
		return
	}
	if service == "" {
		http.Error(w, "Missing service parameter", http.StatusBadRequest)
		return
	}

	// Default to 24 hours if range not specified
	if rangeStr == "" {
		rangeStr = "24h"
	}

	// Parse time range
	//
	// parseTimeRange() converts strings like "1h" to a duration
	// Returns error if the format is invalid
	duration, err := parseTimeRange(rangeStr)
	if err != nil {
		http.Error(w, "Invalid range parameter", http.StatusBadRequest)
		return
	}

	// Calculate time window
	//
	// time.Now() gives current time
	// .Add(-duration) subtracts the duration to get start time
	endTime := time.Now()
	startTime := endTime.Add(-duration)

	// Query metrics from database
	metrics, err := getMetricsForService(hostID, service, startTime, endTime)
	if err != nil {
		log.Printf("[ERROR] Failed to get metrics: %v", err)
		http.Error(w, "Failed to get metrics", http.StatusInternalServerError)
		return
	}

	// Get hostname for the response
	hostname, err := getHostname(hostID)
	if err != nil {
		log.Printf("[ERROR] Failed to get hostname: %v", err)
		hostname = hostID // Fallback to ID if name lookup fails
	}

	// Build JSON response
	response := MetricsResponse{
		HostID:    hostID,
		Hostname:  hostname,
		Service:   service,
		StartTime: startTime,
		EndTime:   endTime,
		Metrics:   metrics,
	}

	// Set response headers for JSON
	//
	// Content-Type tells the client this is JSON data
	// This allows the browser to parse it automatically
	w.Header().Set("Content-Type", "application/json")

	// Encode response as JSON and write to client
	//
	// json.NewEncoder(w) creates an encoder that writes to w
	// .Encode(response) converts response to JSON and writes it
	//
	// This is equivalent to:
	//   jsonBytes, _ := json.Marshal(response)
	//   w.Write(jsonBytes)
	// But more efficient (streams instead of buffering)
	err = json.NewEncoder(w).Encode(response)
	if err != nil {
		log.Printf("[ERROR] Failed to encode JSON: %v", err)
	}
}

// =============================================================================
// DATABASE QUERIES
// =============================================================================

// getMetricsForService queries all metrics for a service in a time range.
//
// Parameters:
//   - hostID: The host identifier
//   - service: The service name
//   - startTime: Start of time range
//   - endTime: End of time range
//
// Returns:
//   - []MetricSeries: Array of metric series (one per metric type)
//   - error: Any database error
func getMetricsForService(hostID, service string, startTime, endTime time.Time) ([]MetricSeries, error) {
	// Query all metrics for this service in the time range
	//
	// ORDER BY metric_type, metric_name, collected_at:
	// - Groups metrics of the same type together
	// - Within each type, groups by metric name
	// - Within each metric, orders by time (oldest first)
	const query = `
		SELECT metric_type, metric_name, value, collected_at
		FROM metrics
		WHERE host_id = ? AND service_name = ?
		  AND collected_at BETWEEN ? AND ?
		ORDER BY metric_type, metric_name, collected_at
	`

	// Execute query with parameters
	rows, err := db.Query(query, hostID, service, startTime, endTime)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	// Map to collect points by metric
	//
	// Key: "metric_type:metric_name" (e.g., "cpu:user")
	// Value: Array of data points for that metric
	//
	// map[string][]MetricPoint creates a map where:
	// - Keys are strings
	// - Values are slices of MetricPoint
	metricsMap := make(map[string][]MetricPoint)

	// Also track the order and type/name for each metric
	// We need this to build the final MetricSeries array
	type metricKey struct {
		metricType string
		metricName string
	}
	metricKeys := make(map[string]metricKey)

	// Read all rows
	for rows.Next() {
		var metricType, metricName string
		var value float64
		var collectedAt time.Time

		err := rows.Scan(&metricType, &metricName, &value, &collectedAt)
		if err != nil {
			return nil, err
		}

		// Create a unique key for this metric
		key := metricType + ":" + metricName

		// Store the key info (for building MetricSeries later)
		if _, exists := metricKeys[key]; !exists {
			metricKeys[key] = metricKey{
				metricType: metricType,
				metricName: metricName,
			}
		}

		// Add this point to the metric's data
		metricsMap[key] = append(metricsMap[key], MetricPoint{
			Timestamp: collectedAt,
			Value:     value,
		})
	}

	if err = rows.Err(); err != nil {
		return nil, err
	}

	// Convert map to array of MetricSeries
	//
	// We need to convert from:
	//   map[string][]MetricPoint
	// To:
	//   []MetricSeries
	var result []MetricSeries

	for key, points := range metricsMap {
		// Get the type and name for this metric
		mk := metricKeys[key]

		// Build arrays of timestamps and values
		//
		// JavaScript charts need parallel arrays:
		// - timestamps: ["2025-11-22T10:00:00Z", "2025-11-22T10:01:00Z", ...]
		// - values: [10.5, 12.3, ...]
		timestamps := make([]string, len(points))
		values := make([]float64, len(points))

		for i, point := range points {
			// Format timestamp as ISO 8601 (JavaScript-friendly)
			// time.RFC3339 is the constant for ISO 8601 format
			timestamps[i] = point.Timestamp.Format(time.RFC3339)
			values[i] = point.Value
		}

		// Create MetricSeries for this metric
		series := MetricSeries{
			Name:       mk.metricName,
			Type:       mk.metricType,
			Timestamps: timestamps,
			Values:     values,
		}

		result = append(result, series)
	}

	return result, nil
}

// getHostname looks up the hostname for a host ID.
//
// Parameters:
//   - hostID: The host identifier
//
// Returns:
//   - string: The hostname
//   - error: Any database error
func getHostname(hostID string) (string, error) {
	const query = `SELECT hostname FROM hosts WHERE id = ?`

	var hostname string
	err := db.QueryRow(query, hostID).Scan(&hostname)
	if err != nil {
		return "", err
	}

	return hostname, nil
}

// =============================================================================
// HELPER FUNCTIONS
// =============================================================================

// parseTimeRange converts a time range string to a duration.
//
// Supported formats:
//   - "1h": 1 hour
//   - "6h": 6 hours
//   - "24h": 24 hours (1 day)
//   - "7d": 7 days (1 week)
//   - "30d": 30 days (1 month)
//
// Parameters:
//   - rangeStr: The range string (e.g., "1h", "7d")
//
// Returns:
//   - time.Duration: The duration
//   - error: If the format is invalid
func parseTimeRange(rangeStr string) (time.Duration, error) {
	// Handle day-based ranges specially
	//
	// time.ParseDuration() doesn't understand "d" for days
	// We need to convert days to hours first
	if len(rangeStr) > 1 && rangeStr[len(rangeStr)-1] == 'd' {
		// Extract the number part
		// rangeStr[:len(rangeStr)-1] removes the last character
		// "7d" -> "7"
		numStr := rangeStr[:len(rangeStr)-1]

		// Convert string to integer
		//
		// strconv.Atoi() converts "7" to 7
		// Returns error if not a valid number
		days, err := strconv.Atoi(numStr)
		if err != nil {
			return 0, err
		}

		// Convert days to hours
		// 1 day = 24 hours
		// time.Hour is a constant representing 1 hour
		return time.Duration(days*24) * time.Hour, nil
	}

	// For hour-based ranges, use standard time.ParseDuration
	//
	// time.ParseDuration() understands:
	// - "1h" = 1 hour
	// - "30m" = 30 minutes
	// - "1h30m" = 1.5 hours
	// etc.
	return time.ParseDuration(rangeStr)
}

// =============================================================================
// ACTION API
// =============================================================================

// ActionRequest represents a request to perform an action on a service.
//
// This is sent by the frontend JavaScript when a user clicks an action button.
//
// Example JSON:
//   {
//     "host_id": "bigone-1763842004",
//     "service": "nginx",
//     "action": "restart"
//   }
type ActionRequest struct {
	HostID  string `json:"host_id"`  // Host identifier
	Service string `json:"service"`  // Service name
	Action  string `json:"action"`   // Action to perform (start, stop, restart, monitor, unmonitor)
}

// ActionResponse represents the response from an action request.
//
// This is returned to the frontend to indicate success or failure.
//
// Example JSON success:
//   {
//     "success": true,
//     "message": "Action 'restart' successfully sent to service 'nginx' on host 'bigone'"
//   }
//
// Example JSON failure:
//   {
//     "success": false,
//     "message": "Failed to execute action: invalid action 'foo'"
//   }
type ActionResponse struct {
	Success bool   `json:"success"` // Whether the action succeeded
	Message string `json:"message"` // Human-readable message
}

// HandleActionAPI handles requests to perform actions on services.
//
// URL format:
//   POST /api/action
//
// Request body (JSON):
//   {
//     "host_id": "bigone-1763842004",
//     "service": "nginx",
//     "action": "restart"
//   }
//
// Response (JSON):
//   {
//     "success": true,
//     "message": "Action successfully sent"
//   }
//
// This endpoint:
// 1. Parses the JSON request
// 2. Looks up the host's Monit credentials in the database
// 3. Creates a MonitClient with those credentials
// 4. Executes the requested action
// 5. Returns success/failure
func HandleActionAPI(w http.ResponseWriter, r *http.Request) {
	// Only allow POST requests
	if r.Method != http.MethodPost {
		respondJSON(w, ActionResponse{
			Success: false,
			Message: "Method not allowed",
		}, http.StatusMethodNotAllowed)
		return
	}

	// Parse JSON request body
	var req ActionRequest
	err := json.NewDecoder(r.Body).Decode(&req)
	if err != nil {
		log.Printf("[ERROR] Failed to parse action request: %v", err)
		respondJSON(w, ActionResponse{
			Success: false,
			Message: "Invalid request body",
		}, http.StatusBadRequest)
		return
	}

	// Validate required fields
	if req.HostID == "" {
		respondJSON(w, ActionResponse{
			Success: false,
			Message: "Missing host_id",
		}, http.StatusBadRequest)
		return
	}
	if req.Service == "" {
		respondJSON(w, ActionResponse{
			Success: false,
			Message: "Missing service",
		}, http.StatusBadRequest)
		return
	}
	if req.Action == "" {
		respondJSON(w, ActionResponse{
			Success: false,
			Message: "Missing action",
		}, http.StatusBadRequest)
		return
	}

	// Query host credentials from database
	hostInfo, err := getHostCredentials(req.HostID)
	if err != nil {
		log.Printf("[ERROR] Failed to get host credentials for %s: %v", req.HostID, err)
		respondJSON(w, ActionResponse{
			Success: false,
			Message: "Host not found or missing credentials",
		}, http.StatusNotFound)
		return
	}

	// Log the action attempt
	log.Printf("[INFO] Executing action '%s' on service '%s' (host: %s)",
		req.Action, req.Service, hostInfo.Hostname)

	// Create Monit client with host's credentials
	client := control.NewMonitClient(
		hostInfo.HTTPAddress,
		hostInfo.HTTPPort,
		hostInfo.HTTPUsername,
		hostInfo.HTTPPassword,
	)

	// Execute the action
	err = client.ExecuteAction(req.Service, req.Action)
	if err != nil {
		log.Printf("[ERROR] Failed to execute action: %v", err)
		respondJSON(w, ActionResponse{
			Success: false,
			Message: "Failed to execute action: " + err.Error(),
		}, http.StatusInternalServerError)
		return
	}

	// Success!
	log.Printf("[INFO] Action '%s' successfully sent to service '%s' on host '%s'",
		req.Action, req.Service, hostInfo.Hostname)

	respondJSON(w, ActionResponse{
		Success: true,
		Message: "Action '" + req.Action + "' successfully sent to service '" + req.Service + "' on host '" + hostInfo.Hostname + "'",
	}, http.StatusOK)
}

// HostCredentials represents the information needed to control a Monit agent.
//
// This is retrieved from the database when executing actions.
type HostCredentials struct {
	Hostname     string // Human-readable hostname
	HTTPAddress  string // Monit HTTP server address
	HTTPPort     int    // Monit HTTP server port
	HTTPSSL      int    // SSL enabled flag
	HTTPUsername string // HTTP authentication username
	HTTPPassword string // HTTP authentication password
}

// getHostCredentials retrieves the Monit connection info for a host.
//
// Parameters:
//   - hostID: The host identifier
//
// Returns:
//   - *HostCredentials: The host's connection information
//   - error: Any database error
func getHostCredentials(hostID string) (*HostCredentials, error) {
	const query = `
		SELECT hostname, http_address, http_port, http_ssl, http_username, http_password
		FROM hosts
		WHERE id = ?
	`

	var creds HostCredentials
	err := db.QueryRow(query, hostID).Scan(
		&creds.Hostname,
		&creds.HTTPAddress,
		&creds.HTTPPort,
		&creds.HTTPSSL,
		&creds.HTTPUsername,
		&creds.HTTPPassword,
	)
	if err != nil {
		return nil, err
	}

	return &creds, nil
}

// respondJSON is a helper function to send JSON responses.
//
// Parameters:
//   - w: HTTP response writer
//   - data: Data to encode as JSON
//   - statusCode: HTTP status code
func respondJSON(w http.ResponseWriter, data interface{}, statusCode int) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)

	err := json.NewEncoder(w).Encode(data)
	if err != nil {
		log.Printf("[ERROR] Failed to encode JSON response: %v", err)
	}
}

// =============================================================================
// REMOTE HOST METRICS API
// =============================================================================

// HandleRemoteHostMetricsAPI serves response time metrics for remote host services.
//
// URL format:
//   /api/remote-metrics?host_id=xxx&service=xxx&range=1h
//
// Query parameters:
//   - host_id (required): Host identifier
//   - service (required): Service name
//   - range (optional): Time range (1h, 6h, 24h, 7d, 30d), default: 24h
//
// Returns JSON with timestamps and response time values (ICMP ping and port checks).
func HandleRemoteHostMetricsAPI(w http.ResponseWriter, r *http.Request) {
	// Only allow GET requests
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Parse query parameters
	query := r.URL.Query()
	hostID := query.Get("host_id")
	service := query.Get("service")
	rangeStr := query.Get("range")

	// Validate required parameters
	if hostID == "" {
		http.Error(w, "Missing host_id parameter", http.StatusBadRequest)
		return
	}
	if service == "" {
		http.Error(w, "Missing service parameter", http.StatusBadRequest)
		return
	}

	// Default to 24 hours if range not specified
	if rangeStr == "" {
		rangeStr = "24h"
	}

	// Parse time range
	duration, err := parseTimeRange(rangeStr)
	if err != nil {
		http.Error(w, "Invalid range parameter", http.StatusBadRequest)
		return
	}

	// Calculate time window
	endTime := time.Now()
	startTime := endTime.Add(-duration)

	// Query remote host metrics from database
	metrics, err := getRemoteHostMetricsForGraph(hostID, service, startTime, endTime)
	if err != nil {
		log.Printf("[ERROR] Failed to get remote host metrics: %v", err)
		http.Error(w, "Failed to get metrics", http.StatusInternalServerError)
		return
	}

	// Get hostname for the response
	hostname, err := getHostname(hostID)
	if err != nil {
		log.Printf("[ERROR] Failed to get hostname: %v", err)
		hostname = hostID
	}

	// Build JSON response
	response := MetricsResponse{
		HostID:    hostID,
		Hostname:  hostname,
		Service:   service,
		StartTime: startTime,
		EndTime:   endTime,
		Metrics:   metrics,
	}

	// Set response headers for JSON
	w.Header().Set("Content-Type", "application/json")

	// Encode response as JSON and write to client
	err = json.NewEncoder(w).Encode(response)
	if err != nil {
		log.Printf("[ERROR] Failed to encode JSON: %v", err)
	}
}

// getRemoteHostMetricsForGraph queries response time metrics for a remote host service over a time range.
// This is used for graphing historical data. Do not confuse with getRemoteHostMetrics in handlers_status.go
// which gets only the latest metrics for display on the service detail page.
//
// Parameters:
//   - hostID: The host identifier
//   - service: The service name
//   - startTime: Start of time range
//   - endTime: End of time range
//
// Returns:
//   - []MetricSeries: Array of metric series (ICMP and Port response times)
//   - error: Any database error
func getRemoteHostMetricsForGraph(hostID, service string, startTime, endTime time.Time) ([]MetricSeries, error) {
	// Query remote host metrics for this service in the time range
	// We'll get ICMP response times and Port response times
	const query = `
		SELECT collected_at, icmp_responsetime, port_responsetime
		FROM remote_host_metrics
		WHERE host_id = ? AND service_name = ?
		  AND collected_at BETWEEN ? AND ?
		ORDER BY collected_at
	`

	rows, err := db.Query(query, hostID, service, startTime, endTime)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	// Collect data points
	var icmpPoints []MetricPoint
	var portPoints []MetricPoint

	for rows.Next() {
		var collectedAt time.Time
		var icmpResponse, portResponse *float64

		err := rows.Scan(&collectedAt, &icmpResponse, &portResponse)
		if err != nil {
			return nil, err
		}

		// Add ICMP response time if available (convert to milliseconds)
		if icmpResponse != nil && *icmpResponse > 0 {
			icmpPoints = append(icmpPoints, MetricPoint{
				Timestamp: collectedAt,
				Value:     *icmpResponse * 1000, // Convert seconds to milliseconds
			})
		}

		// Add Port response time if available (convert to milliseconds)
		if portResponse != nil && *portResponse > 0 {
			portPoints = append(portPoints, MetricPoint{
				Timestamp: collectedAt,
				Value:     *portResponse * 1000, // Convert seconds to milliseconds
			})
		}
	}

	if err = rows.Err(); err != nil {
		return nil, err
	}

	// Build metric series
	var result []MetricSeries

	// Add ICMP series if we have data
	if len(icmpPoints) > 0 {
		timestamps := make([]string, len(icmpPoints))
		values := make([]float64, len(icmpPoints))

		for i, point := range icmpPoints {
			timestamps[i] = point.Timestamp.Format(time.RFC3339)
			values[i] = point.Value
		}

		result = append(result, MetricSeries{
			Name:       "icmp_response_time",
			Type:       "response_time",
			Timestamps: timestamps,
			Values:     values,
		})
	}

	// Add Port series if we have data
	if len(portPoints) > 0 {
		timestamps := make([]string, len(portPoints))
		values := make([]float64, len(portPoints))

		for i, point := range portPoints {
			timestamps[i] = point.Timestamp.Format(time.RFC3339)
			values[i] = point.Value
		}

		result = append(result, MetricSeries{
			Name:       "port_response_time",
			Type:       "response_time",
			Timestamps: timestamps,
			Values:     values,
		})
	}

	return result, nil
}
