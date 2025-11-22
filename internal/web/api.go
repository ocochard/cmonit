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
