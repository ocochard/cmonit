// Package control provides functionality to control Monit services remotely.
//
// This package implements a client for Monit's HTTP control API, allowing
// cmonit to perform actions (start, stop, restart, monitor, unmonitor) on
// services running on remote Monit agents.
//
// Monit's Control API:
// - Endpoint: POST to /servicename (e.g., /nginx, /sshd)
// - Actions: start, stop, restart, monitor, unmonitor
// - Security: Requires CSRF token (double-submit cookie pattern)
// - Auth: HTTP Basic Authentication
package control

import (
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
)

// MonitClient represents a connection to a Monit agent.
//
// Each Monit agent has its own HTTP server (usually on port 2812) that
// provides both monitoring status and control capabilities.
type MonitClient struct {
	// Host is the Monit agent hostname or IP address
	Host string

	// Port is the Monit HTTP server port (usually 2812)
	Port int

	// Username for HTTP Basic Authentication
	Username string

	// Password for HTTP Basic Authentication
	Password string

	// BaseURL is the complete URL to the Monit HTTP server
	// Example: http://192.168.1.10:2812
	BaseURL string

	// HTTP client with custom settings (timeouts, etc.)
	httpClient *http.Client
}

// NewMonitClient creates a new Monit client.
//
// Parameters:
//   - host: Monit agent hostname or IP
//   - port: Monit HTTP port (usually 2812)
//   - username: HTTP Basic Auth username (usually "admin")
//   - password: HTTP Basic Auth password
//
// Returns a configured MonitClient ready to use.
func NewMonitClient(host string, port int, username, password string) *MonitClient {
	return &MonitClient{
		Host:     host,
		Port:     port,
		Username: username,
		Password: password,
		BaseURL:  fmt.Sprintf("http://%s:%d", host, port),
		httpClient: &http.Client{
			// 10 second timeout for requests
			// Monit actions are usually fast, but we allow some buffer
			Timeout: 10 * 1000000000, // 10 seconds in nanoseconds
		},
	}
}

// getCSRFToken fetches the CSRF security token from a service page.
//
// Monit uses CSRF protection (double-submit cookie) for all POST requests.
// The token is embedded in the HTML forms as a hidden field.
//
// How it works:
// 1. GET /servicename to retrieve the service's HTML page
// 2. Parse the HTML to extract the security token from hidden fields
// 3. Return the token for use in subsequent POST requests
//
// The token format in HTML:
//   <input type=hidden name='securitytoken' value='TOKEN_HERE'>
//
// Returns:
//   - token: The CSRF security token string
//   - error: nil on success, error description if failed
func (mc *MonitClient) getCSRFToken(serviceName string) (string, error) {
	// Build the service URL (e.g., http://host:2812/nginx)
	serviceURL := fmt.Sprintf("%s/%s", mc.BaseURL, url.PathEscape(serviceName))

	// Create HTTP GET request
	req, err := http.NewRequest("GET", serviceURL, nil)
	if err != nil {
		return "", fmt.Errorf("failed to create request: %w", err)
	}

	// Add HTTP Basic Authentication credentials
	// Monit requires authentication for all pages
	req.SetBasicAuth(mc.Username, mc.Password)

	// Execute the request
	resp, err := mc.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("failed to fetch service page: %w", err)
	}
	defer resp.Body.Close()

	// Check HTTP status code
	// 200 OK: Success
	// 401 Unauthorized: Bad credentials
	// 404 Not Found: Service doesn't exist
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("unexpected status code: %d", resp.StatusCode)
	}

	// Read the HTML response body
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("failed to read response body: %w", err)
	}

	// Extract the CSRF token using regex
	// Pattern matches: <input type=hidden name='securitytoken' value='TOKEN'>
	// We use a regex to handle variations in quotes and spacing
	tokenPattern := regexp.MustCompile(`name=['"]securitytoken['"].*?value=['"]([^'"]+)['"]`)
	matches := tokenPattern.FindStringSubmatch(string(body))

	if len(matches) < 2 {
		return "", fmt.Errorf("CSRF token not found in response")
	}

	// matches[0] is the full match, matches[1] is the captured token
	return matches[1], nil
}

// ExecuteAction performs an action on a Monit service.
//
// This is the main function for controlling Monit services remotely.
// It handles the complete workflow including CSRF token retrieval.
//
// Parameters:
//   - serviceName: Name of the service (as configured in monitrc)
//   - action: Action to perform (start, stop, restart, monitor, unmonitor)
//
// Workflow:
// 1. GET the service page to obtain CSRF token
// 2. POST to the service URL with action and token
// 3. Monit schedules the action for next monitoring cycle
// 4. Return success/failure
//
// Returns:
//   - error: nil if successful, error description if failed
//
// Example usage:
//   client := NewMonitClient("192.168.1.10", 2812, "admin", "monit")
//   err := client.ExecuteAction("nginx", "restart")
//   if err != nil {
//       log.Printf("Failed to restart nginx: %v", err)
//   }
func (mc *MonitClient) ExecuteAction(serviceName, action string) error {
	// Validate action parameter
	// Only these actions are supported by Monit
	validActions := map[string]bool{
		"start":     true,
		"stop":      true,
		"restart":   true,
		"monitor":   true,
		"unmonitor": true,
	}

	if !validActions[action] {
		return fmt.Errorf("invalid action '%s', must be one of: start, stop, restart, monitor, unmonitor", action)
	}

	// Step 1: Get CSRF token from the service page
	token, err := mc.getCSRFToken(serviceName)
	if err != nil {
		return fmt.Errorf("failed to get CSRF token: %w", err)
	}

	// Step 2: Prepare the POST request
	serviceURL := fmt.Sprintf("%s/%s", mc.BaseURL, url.PathEscape(serviceName))

	// Build POST body: action=ACTIONNAME&securitytoken=TOKEN
	// We use url.Values to properly encode the form data
	formData := url.Values{}
	formData.Set("action", action)
	formData.Set("securitytoken", token)

	// Create HTTP POST request
	req, err := http.NewRequest("POST", serviceURL, strings.NewReader(formData.Encode()))
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	// Set required headers
	req.SetBasicAuth(mc.Username, mc.Password)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	// IMPORTANT: Set the CSRF token cookie
	// Monit's double-submit cookie pattern requires the token in both:
	// 1. POST body (done above)
	// 2. Cookie header (done here)
	req.Header.Set("Cookie", fmt.Sprintf("securitytoken=%s", token))

	// Step 3: Execute the action request
	resp, err := mc.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("failed to execute action: %w", err)
	}
	defer resp.Body.Close()

	// Check response status
	// 200 OK: Action accepted
	// 403 Forbidden: CSRF token validation failed
	// 400 Bad Request: Invalid action or service
	if resp.StatusCode != http.StatusOK {
		// Read error message from response body
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("action failed with status %d: %s", resp.StatusCode, string(body))
	}

	// Success! The action has been scheduled by Monit
	// Note: The action happens asynchronously in Monit's next monitoring cycle
	return nil
}

// GetServiceStatus retrieves the status of a service from Monit.
//
// This can be used to verify that an action completed successfully.
//
// Parameters:
//   - serviceName: Name of the service
//
// Returns:
//   - status: HTML snippet with service status
//   - error: nil on success, error if failed
//
// Note: This is a simple implementation that returns raw HTML.
// For structured data, use the XML status API instead.
func (mc *MonitClient) GetServiceStatus(serviceName string) (string, error) {
	serviceURL := fmt.Sprintf("%s/%s", mc.BaseURL, url.PathEscape(serviceName))

	req, err := http.NewRequest("GET", serviceURL, nil)
	if err != nil {
		return "", fmt.Errorf("failed to create request: %w", err)
	}

	req.SetBasicAuth(mc.Username, mc.Password)

	resp, err := mc.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("failed to fetch status: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("unexpected status code: %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("failed to read response: %w", err)
	}

	return string(body), nil
}
