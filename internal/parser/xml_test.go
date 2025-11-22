// Package parser_test contains tests for the XML parser.
//
// Test files in Go are named with _test.go suffix.
// Go's testing framework automatically finds and runs these tests.
//
// To run tests: go test ./internal/parser
package parser

import (
	"os"      // File operations
	"testing" // Testing framework
)

// TestParseMonitXML tests parsing of real Monit XML data.
//
// Test functions must:
// - Start with "Test"
// - Take a single parameter: t *testing.T
// - Use t.Error() or t.Fatal() to report failures
//
// This test:
// 1. Reads a sample XML file
// 2. Parses it
// 3. Verifies the parsed data is correct
func TestParseMonitXML(t *testing.T) {
	// Read the sample XML file
	// os.ReadFile() reads the entire file into a []byte
	//
	// This file should be created by running:
	//   curl -u admin:monit http://localhost:2812/_status?format=xml > test-status.xml
	data, err := os.ReadFile("../../test-status.xml")
	if err != nil {
		// File doesn't exist or can't be read
		// t.Fatalf() fails the test and stops execution
		// Use Fatal for errors that prevent the rest of the test from running
		t.Fatalf("Failed to read test file: %v", err)
	}

	// Parse the XML
	status, err := ParseMonitXML(data)
	if err != nil {
		t.Fatalf("Failed to parse XML: %v", err)
	}

	// Verify the parsed data
	// These checks ensure our struct tags are correct

	// Check server information
	if status.Server.LocalHostname == "" {
		t.Errorf("Server hostname is empty")
	}

	if status.Server.Version == "" {
		t.Errorf("Server version is empty")
	}

	// Check that we parsed some services
	if len(status.Services) == 0 {
		t.Errorf("No services were parsed")
	}

	// Print summary for manual verification
	// t.Logf() prints informational messages (only shown with -v flag)
	t.Logf("Parsed successfully:")
	t.Logf("  Hostname: %s", status.Server.LocalHostname)
	t.Logf("  Version: %s", status.Server.Version)
	t.Logf("  Services: %d", len(status.Services))

	// Check each service
	for i, service := range status.Services {
		t.Logf("  Service %d: %s (type %d)", i+1, service.Name, service.Type)

		// Verify service has a name
		if service.Name == "" {
			t.Errorf("Service %d has empty name", i)
		}

		// Check type-specific fields
		switch service.Type {
		case 5: // System service
			if service.System == nil {
				t.Errorf("System service '%s' missing System metrics", service.Name)
			} else {
				t.Logf("    Load: %.2f, %.2f, %.2f",
					service.System.Load.Avg01,
					service.System.Load.Avg05,
					service.System.Load.Avg15)
				t.Logf("    CPU: user=%.1f%% system=%.1f%%",
					service.System.CPU.User,
					service.System.CPU.System)
				t.Logf("    Memory: %.1f%%", service.System.Memory.Percent)
			}

		case 3: // Process service
			if service.PID == nil {
				t.Errorf("Process service '%s' missing PID", service.Name)
			} else {
				memPct := 0.0
				cpuPct := 0.0
				if service.Memory != nil {
					memPct = service.Memory.Percent
				}
				if service.CPU != nil {
					cpuPct = service.CPU.Percent
				}
				t.Logf("    PID: %d, Memory: %.1f%%, CPU: %.1f%%",
					*service.PID, memPct, cpuPct)
			}

		case 7: // Program service
			if service.Program == nil {
				t.Errorf("Program service '%s' missing Program info", service.Name)
			} else {
				t.Logf("    Status: %d, Output: %s",
					service.Program.Status,
					service.Program.Output)
			}
		}
	}
}
