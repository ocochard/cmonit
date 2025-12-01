// Package main is the entry point for the cmonit application.
//
// cmonit (Central Monit) is an open-source M/Monit clone that provides centralized
// monitoring and management of Monit-enabled hosts through a web interface.
//
// This file contains the main() function which:
// - Initializes the database
// - Starts the HTTP collector server (receives data from Monit agents)
// - Starts the web UI server (displays data to users)
//
// The application runs two HTTP servers concurrently:
// - Port 8080: Collector API (receives POST requests from Monit agents)
// - Port 3000: Web UI (serves HTML pages to users)
package main

import (
	// Standard library imports
	// These are packages built into Go - no need to install separately

	"compress/gzip"  // Gzip compression/decompression
	"database/sql"   // SQL database interface
	"flag"           // Command-line flag parsing
	"fmt"            // Formatted I/O - like printf() in C
	"io"             // I/O operations
	"log"            // Logging to stderr with timestamps
	"log/syslog"     // Syslog support for daemon logging
	"net/http"       // HTTP client and server functionality
	"os"             // Operating system functions (exit codes, etc.)
	"os/signal"      // Signal handling for graceful shutdown
	"path/filepath"  // File path manipulation
	"strconv"        // String conversion utilities
	"strings"        // String manipulation
	"syscall"        // System call interface (for signal constants)
	"time"           // Time operations and ticker

	// External packages
	"golang.org/x/crypto/bcrypt" // Bcrypt password hashing

	// Internal packages (our code)
	// These are relative to the module path (github.com/ocochard/cmonit)
	"github.com/ocochard/cmonit/internal/config" // Configuration file support
	"github.com/ocochard/cmonit/internal/db"     // Database operations
	"github.com/ocochard/cmonit/internal/parser" // XML parser
	"github.com/ocochard/cmonit/internal/web"    // Web UI handlers
)

// Global variable to hold the database connection
//
// In Go, variables declared outside functions are "package-level" globals.
// They're accessible to all functions in the package.
//
// Why use a global here?
// - HTTP handlers don't take custom parameters
// - We need the database in handleCollector
// - Alternative: use closures or a struct with methods (more complex)
//
// Later we'll refactor to use dependency injection for better testability.
var globalDB *sql.DB

// debugEnabled controls whether DEBUG log messages are output.
//
// When true, enables verbose DEBUG logging for troubleshooting.
// Controlled by the -debug command-line flag.
var debugEnabled bool

// collectorAuthUsername holds the username for collector HTTP Basic Auth
// Set from the -collector-user command-line flag, defaults to "monit"
var collectorAuthUsername string

// collectorAuthPassword holds the password for collector HTTP Basic Auth
// Set from the -collector-password command-line flag, defaults to "monit"
var collectorAuthPassword string

// collectorAuthPasswordFormat holds the password format for collector HTTP Basic Auth
// Set from the -collector-password-format command-line flag, defaults to "plain"
var collectorAuthPasswordFormat string

// main is the entry point of the program
// Go programs always start execution here
//
// This function:
// 1. Prints a startup message
// 2. Sets up the HTTP routes (URL paths and their handlers)
// 3. Starts the HTTP server
// 4. Waits for termination signals
//
// Note: main() doesn't return a value like in C
// Use os.Exit(code) to return exit codes
func main() {
	// Define command-line flags
	//
	// flag.String() creates a string flag with:
	// - name: the flag name (e.g., "-collector")
	// - default value: used if flag not specified
	// - description: shown in -h help message
	//
	// Returns a *string (pointer to string) that will be set when flag.Parse() runs
	//
	// Address format: "host:port" or ":port"
	// - ":8080" = all interfaces (0.0.0.0 and ::)
	// - "localhost:8080" = only local connections
	// - "192.168.1.10:8080" = specific IPv4
	// - "[::1]:8080" = IPv6 localhost
	// - "[::]:8080" = all IPv6 interfaces
	collectorAddr := flag.String("collector", "8080",
		"Collector port number (e.g., 8080, 9000) - inherits IP from -listen")

	webAddr := flag.String("listen", "localhost:3000",
		"Web UI listen address (e.g., localhost:3000, 0.0.0.0:3000, [::]:3000, 192.168.1.10:3000)")

	webUser := flag.String("web-user", "",
		"Web UI HTTP Basic Auth username (empty = no authentication)")

	webPassword := flag.String("web-password", "",
		"Web UI HTTP Basic Auth password (empty = no authentication)")

	webPasswordFormat := flag.String("web-password-format", "plain",
		"Web UI password format: 'plain' or 'bcrypt' (default: plain)")

	hashPassword := flag.String("hash-password", "",
		"Generate bcrypt hash for given password and exit (utility command)")

	webCert := flag.String("web-cert", "",
		"Web UI TLS certificate file (empty = HTTP only)")

	webKey := flag.String("web-key", "",
		"Web UI TLS key file (empty = HTTP only)")

	dbPath := flag.String("db", "/var/run/cmonit/cmonit.db",
		"Database file path")

	pidFile := flag.String("pidfile", "/var/run/cmonit/cmonit.pid",
		"PID file path")

	syslogFacility := flag.String("syslog", "",
		"Syslog facility (daemon, local0-local7, or empty for stderr logging)")

	debugFlag := flag.Bool("debug", false,
		"Enable verbose DEBUG logging for troubleshooting")

	collectorUser := flag.String("collector-user", "monit",
		"Collector HTTP Basic Auth username (Monit agents must use this)")

	collectorPassword := flag.String("collector-password", "monit",
		"Collector HTTP Basic Auth password (Monit agents must use this)")

	collectorPasswordFormat := flag.String("collector-password-format", "plain",
		"Collector password format: 'plain' or 'bcrypt' (default: plain)")

	daemonMode := flag.Bool("daemon", false,
		"Run in background as a daemon process")

	configFile := flag.String("config", "",
		"Configuration file path (TOML format, optional)")

	// Parse command-line flags
	//
	// flag.Parse() processes os.Args (command-line arguments)
	// After this call, *collectorAddr and *webAddr contain the values
	//
	// Example usage:
	//   ./cmonit                                 # Use defaults
	//   ./cmonit -listen 0.0.0.0:3000           # Web accessible from anywhere
	//   ./cmonit -listen [::]:3000              # IPv6 all interfaces
	//   ./cmonit -collector 9000 -listen :4000  # Custom ports
	flag.Parse()

	// Handle -hash-password utility command
	//
	// This is a convenience command to generate bcrypt hashes for passwords.
	// Usage: ./cmonit -hash-password "mypassword"
	//
	// The generated hash can be used in the configuration file:
	//   [web]
	//   password = "$2a$10$..."
	//   password_format = "bcrypt"
	if *hashPassword != "" {
		// Generate bcrypt hash with cost 10 (default, balanced security/performance)
		//
		// bcrypt.GenerateFromPassword() creates a hash that includes:
		// - Algorithm version ($2a$, $2b$, etc.)
		// - Cost factor (10 = 2^10 iterations)
		// - Salt (random, included in the hash)
		// - Hash of password+salt
		//
		// Example output: $2a$10$N9qo8uLOickgx2ZMRZoMyeIjZAgcfl7p92ldGxad68LJZdL17lhWy
		hash, err := bcrypt.GenerateFromPassword([]byte(*hashPassword), bcrypt.DefaultCost)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error generating bcrypt hash: %v\n", err)
			os.Exit(1)
		}

		// Print the hash to stdout
		fmt.Printf("Bcrypt hash: %s\n\n", string(hash))
		fmt.Println("Add this to your configuration file:")
		fmt.Println("[web]")
		fmt.Println("user = \"admin\"")
		fmt.Printf("password = \"%s\"\n", string(hash))
		fmt.Println("password_format = \"bcrypt\"")
		os.Exit(0)
	}

	// Load configuration file if specified
	//
	// Config file provides defaults, CLI flags override them
	// Priority: CLI flags > Config file > Built-in defaults
	if *configFile != "" {
		cfg, err := config.Load(*configFile)
		if err != nil {
			log.Fatalf("[FATAL] Failed to load config file: %v", err)
		}

		log.Printf("[INFO] Loaded configuration from: %s", *configFile)

		// Merge config file values with CLI flags
		// CLI flags take priority if they differ from defaults
		//
		// We merge each setting, checking if the CLI flag was explicitly set
		// (differs from default) or if we should use the config file value
		*collectorAddr = config.MergeString(cfg.Network.CollectorPort, *collectorAddr, "8080")
		*webAddr = config.MergeString(cfg.Network.Listen, *webAddr, "localhost:3000")
		*collectorUser = config.MergeString(cfg.Collector.User, *collectorUser, "monit")
		*collectorPassword = config.MergeString(cfg.Collector.Password, *collectorPassword, "monit")
		*collectorPasswordFormat = config.MergeString(cfg.Collector.PasswordFormat, *collectorPasswordFormat, "plain")
		*webUser = config.MergeString(cfg.Web.User, *webUser, "")
		*webPassword = config.MergeString(cfg.Web.Password, *webPassword, "")
		*webPasswordFormat = config.MergeString(cfg.Web.PasswordFormat, *webPasswordFormat, "plain")
		*webCert = config.MergeString(cfg.Web.Cert, *webCert, "")
		*webKey = config.MergeString(cfg.Web.Key, *webKey, "")
		*dbPath = config.MergeString(cfg.Storage.Database, *dbPath, "/var/run/cmonit/cmonit.db")
		*pidFile = config.MergeString(cfg.Storage.PidFile, *pidFile, "/var/run/cmonit/cmonit.pid")
		*syslogFacility = config.MergeString(cfg.Logging.Syslog, *syslogFacility, "")
		*debugFlag = config.MergeBool(cfg.Logging.Debug, *debugFlag)
		*daemonMode = config.MergeBool(cfg.Process.Daemon, *daemonMode)
	}

	// Process collector address to inherit IP from -listen
	//
	// If -collector is just a port number (e.g., "8080" or ":8080"),
	// combine it with the host from -listen for consistency.
	//
	// This ensures both servers listen on the same interface by default.
	// Users can still override by specifying a full address for -collector.
	*collectorAddr = buildAddress(*webAddr, *collectorAddr)

	// Handle daemon mode
	//
	// If -daemon flag is set, we detach from the controlling terminal
	// and run in the background. This is done by re-executing ourselves
	// with syscall attributes that create a new session.
	if *daemonMode {
		// Check if already daemonized by looking for our environment variable
		if os.Getenv("CMONIT_DAEMONIZED") != "1" {
			// Not yet daemonized - re-exec ourselves
			args := make([]string, 0, len(os.Args))
			for _, arg := range os.Args {
				// Skip the -daemon flag for the child process
				if arg != "-daemon" {
					args = append(args, arg)
				}
			}

			// Prepare syscall attributes for daemonization
			execSpec := &syscall.ProcAttr{
				Env: append(os.Environ(), "CMONIT_DAEMONIZED=1"),
				Files: []uintptr{
					// stdin, stdout, stderr -> /dev/null
					uintptr(syscall.Stdin),
					uintptr(syscall.Stdout),
					uintptr(syscall.Stderr),
				},
				Sys: &syscall.SysProcAttr{
					Setsid: true, // Create new session (detach from terminal)
				},
			}

			// Get absolute path to current executable
			execPath, err := os.Executable()
			if err != nil {
				log.Fatalf("[FATAL] Failed to get executable path for daemonization: %v", err)
			}

			// Fork and exec the child process
			pid, err := syscall.ForkExec(execPath, args, execSpec)
			if err != nil {
				log.Fatalf("[FATAL] Failed to daemonize: %v", err)
			}

			// Parent process: print the child PID and exit
			fmt.Printf("cmonit daemonized with PID %d\n", pid)
			os.Exit(0)
		}
		// Already daemonized, continue normally
	}

	// Set global debug mode from flag
	debugEnabled = *debugFlag
	db.SetDebugMode(debugEnabled)

	// Validate password formats
	if *collectorPasswordFormat != "plain" && *collectorPasswordFormat != "bcrypt" {
		log.Fatalf("[FATAL] Invalid -collector-password-format: %s (must be 'plain' or 'bcrypt')", *collectorPasswordFormat)
	}
	if *webPasswordFormat != "plain" && *webPasswordFormat != "bcrypt" {
		log.Fatalf("[FATAL] Invalid -web-password-format: %s (must be 'plain' or 'bcrypt')", *webPasswordFormat)
	}

	// Set collector authentication credentials from flags
	collectorAuthUsername = *collectorUser
	collectorAuthPassword = *collectorPassword
	collectorAuthPasswordFormat = *collectorPasswordFormat

	// Setup syslog if requested
	//
	// If -syslog flag is provided, redirect log output to syslog
	// Otherwise, continue logging to stderr (default)
	if *syslogFacility != "" {
		priority, err := parseSyslogFacility(*syslogFacility)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Invalid syslog facility: %v\n", err)
			os.Exit(1)
		}

		// Connect to syslog
		syslogWriter, err := syslog.New(priority, "cmonit")
		if err != nil {
			fmt.Fprintf(os.Stderr, "Failed to connect to syslog: %v\n", err)
			os.Exit(1)
		}

		// Redirect log output to syslog
		log.SetOutput(syslogWriter)
		log.SetFlags(0) // Syslog adds its own timestamp
	}

	// Print startup banner
	// fmt.Println() prints to standard output with a newline
	fmt.Println("=================================")
	fmt.Println("cmonit - Central Monit Monitor")
	fmt.Println("=================================")

	// log.Printf() writes to standard error with a timestamp (or syslog if configured)
	// Example output: "2025/11/22 21:30:00 [INFO] cmonit starting..."
	log.Printf("[INFO] cmonit starting...")
	log.Printf("[INFO] Collector will listen on: %s", *collectorAddr)
	log.Printf("[INFO] Collector authentication: user=%s", collectorAuthUsername)
	log.Printf("[INFO] Web UI will listen on: %s", *webAddr)
	log.Printf("[INFO] Database path: %s", *dbPath)
	log.Printf("[INFO] PID file: %s", *pidFile)

	// Create database directory if it doesn't exist
	//
	// filepath.Dir() extracts the directory path from the full file path
	// Example: "/var/run/cmonit/cmonit.db" -> "/var/run/cmonit"
	dbDir := filepath.Dir(*dbPath)
	if err := os.MkdirAll(dbDir, 0755); err != nil {
		log.Fatalf("[FATAL] Failed to create database directory %s: %v", dbDir, err)
	}

	// Create PID file directory if needed
	pidDir := filepath.Dir(*pidFile)
	if err := os.MkdirAll(pidDir, 0755); err != nil {
		log.Fatalf("[FATAL] Failed to create PID file directory %s: %v", pidDir, err)
	}

	// Write PID file
	//
	// os.Getpid() returns the process ID
	// We write this to a file so other tools (like rc.d scripts) can:
	// - Check if the process is running
	// - Send signals to the process (SIGTERM, SIGHUP, etc.)
	pid := os.Getpid()
	pidContent := []byte(strconv.Itoa(pid) + "\n")
	if err := os.WriteFile(*pidFile, pidContent, 0644); err != nil {
		log.Fatalf("[FATAL] Failed to write PID file: %v", err)
	}
	log.Printf("[INFO] PID %d written to %s", pid, *pidFile)

	// Schedule PID file removal on exit
	defer func() {
		if err := os.Remove(*pidFile); err != nil {
			log.Printf("[WARN] Failed to remove PID file: %v", err)
		}
	}()

	// Initialize the database
	//
	// db.InitDB() does several things:
	// 1. Opens/creates the cmonit.db file
	// 2. Creates all tables if they don't exist
	// 3. Creates indexes for fast queries
	// 4. Enables foreign key constraints
	// 5. Enables WAL mode for better concurrency
	//
	// Returns:
	//   - database: A connection pool we can use for queries
	//   - err: Any error that occurred, or nil if successful
	//
	// The connection pool (*sql.DB):
	// - Manages multiple connections automatically
	// - Reuses connections (performance)
	// - Thread-safe (can be used from multiple goroutines)
	// - Should be created once and reused throughout the application
	//
	// *dbPath dereferences the pointer to get the actual string value
	database, err := db.InitDB(*dbPath)
	if err != nil {
		// Failed to initialize database - can't continue
		// log.Fatalf() prints the error and exits with code 1
		log.Fatalf("[FATAL] Failed to initialize database: %v", err)
	}

	// defer schedules a function to run when main() exits
	// This ensures the database connection is closed cleanly
	//
	// Why defer?
	// - We might exit main() from multiple places (errors, signals, etc.)
	// - defer guarantees cleanup happens no matter how we exit
	// - Like "finally" in try/catch/finally, but cleaner
	//
	// database.Close():
	// - Closes all connections in the pool
	// - Waits for in-flight queries to finish
	// - Releases file locks on cmonit.db
	defer database.Close()

	// Store the database connection in the global variable
	// This makes it accessible to HTTP handlers
	globalDB = database

	// Initialize HTML templates for the web UI
	//
	// web.InitTemplates() does:
	// 1. Loads all .html files from the templates/ directory
	// 2. Parses them using Go's html/template package
	// 3. Caches them for fast rendering
	//
	// This must happen before starting the web server
	err = web.InitTemplates()
	if err != nil {
		log.Fatalf("[FATAL] Failed to load templates: %v", err)
	}

	// Give the web package access to the database
	//
	// The web handlers need to query the database to show host/service status
	// web.SetDB() stores the database connection for use by web handlers
	web.SetDB(database)

	// Set up HTTP routes (URL patterns and their handler functions)
	//
	// http.HandleFunc() registers a handler function for a specific URL pattern
	// When a request comes in matching the pattern, Go calls the handler function
	//
	// Parameters:
	//   - pattern: URL path (e.g., "/collector")
	//   - handler: function to call (must have signature: func(http.ResponseWriter, *http.Request))
	//
	// The handler function receives:
	//   - w: ResponseWriter - used to write the HTTP response back to the client
	//   - r: *Request - contains the incoming request data (method, headers, body, etc.)

	// Register collector endpoint (for Monit agents)
	http.HandleFunc("/collector", handleCollector)

	// Register web UI routes (for human users)
	//
	// We use http.DefaultServeMux for collector routes (port 8080)
	// We'll create a separate ServeMux for web UI routes (port 3000)
	//
	// Why separate muxes?
	// - Collector needs authentication, web UI might not
	// - Different ports for different purposes (security, firewall rules)
	// - Easier to add features to one without affecting the other
	webMux := http.NewServeMux()

	// Main status overview page (shows all hosts in a table)
	webMux.HandleFunc("/", web.HandleStatus)

	// Host detail pages (with graphs) and service detail pages
	// Must be registered before "/" to match more specific paths
	webMux.HandleFunc("/host/", func(w http.ResponseWriter, r *http.Request) {
		// Check if this is a service detail page request
		if strings.Contains(r.URL.Path, "/service/") {
			web.HandleServiceDetail(w, r)
		} else if strings.HasSuffix(r.URL.Path, "/events") {
			// Check if this is an events page request
			web.HandleHostEvents(w, r)
		} else {
			web.HandleHostDetail(w, r)
		}
	})

	// API endpoints for JavaScript to fetch data
	//
	// /api/metrics returns JSON with time-series data
	// Used by Chart.js to draw graphs
	webMux.HandleFunc("/api/metrics", web.HandleMetricsAPI)

	// /api/action performs actions on services (start, stop, restart, etc.)
	// Used by action buttons on the dashboard
	webMux.HandleFunc("/api/action", web.HandleActionAPI)

	// /api/remote-metrics returns JSON with response time data for remote host services
	// Used by Chart.js to draw response time graphs on remote host service detail pages
	webMux.HandleFunc("/api/remote-metrics", web.HandleRemoteHostMetricsAPI)

	// /api/availability returns JSON with host availability time-series data
	// Used by Chart.js to draw availability status graphs showing green/yellow/red status
	webMux.HandleFunc("/api/availability", web.HandleAvailabilityAPI)

	// /api/host/description updates the description field for a host
	// Allows users to add custom HTML notes for each host
	webMux.HandleFunc("/api/host/description", web.HandleUpdateDescription)

	// M/Monit-compatible API endpoints
	//
	// These endpoints provide M/Monit HTTP API compatibility
	// for integration with existing tools and scripts
	//
	// Status API - query host and service status
	webMux.HandleFunc("/status/hosts", web.HandleMMStatusHosts)
	webMux.HandleFunc("/status/hosts/", func(w http.ResponseWriter, r *http.Request) {
		// Route to appropriate handler based on URL path
		if strings.HasSuffix(r.URL.Path, "/services") {
			web.HandleMMStatusServices(w, r)
		} else {
			web.HandleMMStatusHost(w, r)
		}
	})

	// Events API - query events
	webMux.HandleFunc("/events/list", web.HandleMMEventsList)
	webMux.HandleFunc("/events/get/", web.HandleMMEventsGet)

	// Admin API - host administration
	webMux.HandleFunc("/admin/hosts", web.HandleMMAdminHosts)
	webMux.HandleFunc("/admin/hosts/", web.HandleMMAdminHosts)

	// Start the collector HTTP server in a goroutine (lightweight thread)
	//
	// The "go" keyword runs a function concurrently
	// This allows the main() function to continue while the server runs
	// Without "go", ListenAndServe would block forever
	//
	// Why use a goroutine?
	// - We need to run multiple things concurrently (collector + web UI)
	// - We need main() to continue so we can handle shutdown signals
	go func() {
		log.Printf("[INFO] Collector listening on %s", *collectorAddr)

		// http.ListenAndServe() starts an HTTP server
		//
		// Parameters:
		//   - addr: address to listen on
		//     - ":8080" = all interfaces (0.0.0.0 and ::), port 8080
		//     - "localhost:8080" = only local connections
		//     - "192.168.1.10:8080" = specific IPv4 address
		//     - "[::1]:8080" = IPv6 localhost
		//     - "[::]:8080" = all IPv6 interfaces
		//   - handler: if nil, uses the default ServeMux (what we registered with HandleFunc)
		//
		// Returns:
		//   - error: only returns if the server fails to start or crashes
		//            normally this function blocks forever
		//
		// Note: This is a blocking call - it runs forever until an error occurs
		//
		// *collectorAddr dereferences the pointer to get the string value from the flag
		err := http.ListenAndServe(*collectorAddr, nil)

		// If we reach here, the server crashed or failed to start
		// log.Fatalf() prints the error and exits the program with code 1
		// %v is a verb that prints the error message
		if err != nil {
			log.Fatalf("[FATAL] Collector server failed: %v", err)
		}
	}()

	// Start web UI server in a goroutine
	//
	// The web server provides:
	// - Dashboard showing all monitored hosts
	// - Service status tables
	// - Time-series graphs
	// - Metrics API for Chart.js
	//
	// This runs concurrently with the collector server
	go func() {
		// Prepare the handler with optional authentication
		var handler http.Handler = webMux

		// Add HTTP Basic Auth if credentials are provided
		if *webUser != "" && *webPassword != "" {
			log.Printf("[INFO] Web UI authentication enabled for user: %s (format: %s)", *webUser, *webPasswordFormat)
			handler = basicAuth(webMux, *webUser, *webPassword, *webPasswordFormat)
		} else {
			log.Printf("[WARNING] Web UI authentication disabled - use -web-user and -web-password for production")
		}

		// Validate TLS configuration
		tlsEnabled := *webCert != "" || *webKey != ""
		if tlsEnabled {
			if *webCert == "" || *webKey == "" {
				log.Fatalf("[FATAL] Both -web-cert and -web-key must be provided for TLS")
			}
		}

		// Start the appropriate server (HTTP or HTTPS)
		if tlsEnabled {
			log.Printf("[INFO] Web UI listening on %s (HTTPS)", *webAddr)
			err := http.ListenAndServeTLS(*webAddr, *webCert, *webKey, handler)
			if err != nil {
				log.Fatalf("[FATAL] Web server failed: %v", err)
			}
		} else {
			log.Printf("[INFO] Web UI listening on %s (HTTP)", *webAddr)
			log.Printf("[WARNING] TLS disabled - use -web-cert and -web-key for encrypted connections")
			err := http.ListenAndServe(*webAddr, handler)
			if err != nil {
				log.Fatalf("[FATAL] Web server failed: %v", err)
			}
		}
	}()

	// Start availability recording background job
	//
	// This goroutine runs continuously, recording availability status
	// for all hosts at regular intervals (every 60 seconds by default).
	//
	// Why is this needed?
	// - RecordHostAvailability is called when we RECEIVE data from Monit
	// - But what about hosts that go offline? They stop sending data
	// - This background job ensures we continue recording their "offline" status
	// - Creates a complete time-series even when hosts are down
	//
	// The job:
	// 1. Sleeps for 60 seconds
	// 2. Queries all hosts from the database
	// 3. For each host, records their current availability status
	// 4. Repeats forever
	go func() {
		log.Printf("[INFO] Starting availability recording background job")

		// Use a ticker to run every 60 seconds
		// time.Ticker sends a value on its channel at regular intervals
		ticker := time.NewTicker(60 * time.Second)
		defer ticker.Stop()

		for {
			// Wait for the next tick
			<-ticker.C

			// Record availability for all hosts
			err := db.RecordAvailabilityForAllHosts(globalDB)
			if err != nil {
				log.Printf("[WARN] Failed to record availability for all hosts: %v", err)
			}
		}
	}()

	// Wait for interrupt signal to gracefully shut down
	//
	// This keeps the program running until the user presses Ctrl+C
	// or the system sends a termination signal
	//
	// Why do we need this?
	// - Our HTTP servers are running in goroutines
	// - If main() exits, the program terminates immediately
	// - We need to keep main() alive so the servers can handle requests

	// Create a channel to receive OS signals
	// A channel is Go's way of communicating between goroutines
	// Think of it like a pipe - one end sends, the other receives
	//
	// make(chan os.Signal, 1) creates a channel that can hold 1 signal
	// The buffer size of 1 prevents the signal from being lost if we're not ready
	quit := make(chan os.Signal, 1)

	// signal.Notify() tells Go to send signals to our channel
	//
	// Parameters:
	//   - quit: the channel to receive signals
	//   - os.Interrupt: Ctrl+C signal (SIGINT)
	//   - syscall.SIGTERM: termination signal (sent by "kill" command)
	//
	// When the user presses Ctrl+C or runs "kill <pid>", Go will send
	// the signal to our quit channel
	signal.Notify(quit, os.Interrupt, syscall.SIGTERM)

	// Wait for a signal
	// The <- operator receives a value from a channel
	// This line blocks (waits) until a signal is received
	//
	// When a signal arrives:
	//   - It's received from the channel
	//   - We continue to the next line
	//   - The program can shut down gracefully
	<-quit

	// We received a shutdown signal
	log.Printf("[INFO] Shutdown signal received, exiting...")

	// Clean up PID file before exit
	// We do this explicitly here because os.Exit() bypasses deferred functions
	if err := os.Remove(*pidFile); err != nil {
		log.Printf("[WARN] Failed to remove PID file: %v", err)
	} else {
		log.Printf("[INFO] Removed PID file: %s", *pidFile)
	}

	// TODO: Clean up resources
	// - Close database connections (handled by defer in main)
	// - Finish pending HTTP requests
	// - Flush logs

	log.Printf("[INFO] cmonit stopped")

	// Exit with code 0 (success)
	// In Unix, 0 means success, non-zero means error
	os.Exit(0)
}

// handleCollector handles HTTP requests to the /collector endpoint
//
// This is the endpoint where Monit agents POST their status data.
// Each Monit agent sends XML data every 30 seconds (configurable).
//
// Parameters:
//   - w: http.ResponseWriter - used to write the HTTP response back to the client
//   - r: *http.Request - contains the incoming request (method, headers, body, etc.)
//
// How HTTP handlers work in Go:
// 1. Client makes a request to /collector
// 2. Go's HTTP server receives the request
// 3. Go looks up which handler is registered for /collector
// 4. Go calls this function, passing the ResponseWriter and Request
// 5. This function processes the request and writes a response
// 6. Go sends the response back to the client
//
// This function currently implements basic functionality for T1.1 and T1.2.
// We'll add more features in subsequent tests:
// - T1.3-T1.5: HTTP Basic Authentication
// - T1.7: XML parsing
// - T1.8-T1.13: Database storage
func handleCollector(w http.ResponseWriter, r *http.Request) {
	// Log the incoming request for debugging
	// This helps us see which hosts are sending data
	//
	// r.Method is the HTTP method (GET, POST, PUT, DELETE, etc.)
	// r.RemoteAddr is the client's IP address and port
	if debugEnabled {
		log.Printf("[DEBUG] %s /collector from %s", r.Method, r.RemoteAddr)
	}

	// Check if request method is POST
	// The collector endpoint should only accept POST requests from Monit agents
	//
	// r.Method contains the HTTP method as a string
	// We compare it to "POST" to ensure it's the correct method
	if r.Method != http.MethodPost {
		// http.Error() is a helper function that:
		// 1. Sets the Content-Type to text/plain
		// 2. Writes the status code
		// 3. Writes the error message
		//
		// http.StatusMethodNotAllowed is the constant for 405
		// 405 means "I understand the request, but this HTTP method isn't allowed"
		log.Printf("[WARN] Method not allowed: %s", r.Method)
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Validate HTTP Basic Authentication
	//
	// HTTP Basic Auth sends credentials in the Authorization header:
	// Authorization: Basic base64(username:password)
	//
	// r.BasicAuth() is a helper function that:
	// 1. Reads the Authorization header
	// 2. Checks if it starts with "Basic "
	// 3. Decodes the base64-encoded credentials
	// 4. Splits them into username and password
	//
	// Returns:
	//   - username: the username string
	//   - password: the password string
	//   - ok: bool - true if Basic Auth was provided, false otherwise
	username, password, ok := r.BasicAuth()

	// Check if authentication header was provided
	if !ok {
		w.Header().Set("WWW-Authenticate", `Basic realm="cmonit"`)
		log.Printf("[WARN] Authentication missing from %s", r.RemoteAddr)
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	// Check username (always plain text comparison)
	if username != collectorAuthUsername {
		w.Header().Set("WWW-Authenticate", `Basic realm="cmonit"`)
		log.Printf("[WARN] Authentication failed for user '%s' from %s", username, r.RemoteAddr)
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	// Check password based on format
	var passwordMatch bool

	if collectorAuthPasswordFormat == "bcrypt" {
		// Bcrypt comparison
		//
		// bcrypt.CompareHashAndPassword() verifies that the password
		// matches the stored bcrypt hash.
		//
		// This is secure because:
		// - Each password has a unique salt (prevents rainbow table attacks)
		// - Bcrypt is intentionally slow (prevents brute force)
		// - Cost factor can be increased as hardware improves
		err := bcrypt.CompareHashAndPassword([]byte(collectorAuthPassword), []byte(password))
		passwordMatch = (err == nil)
	} else {
		// Plain text comparison (default)
		//
		// Direct string comparison
		// Less secure but simpler and backward compatible
		passwordMatch = (password == collectorAuthPassword)
	}

	if !passwordMatch {
		// Authentication failed
		w.Header().Set("WWW-Authenticate", `Basic realm="cmonit"`)
		log.Printf("[WARN] Authentication failed for user '%s' from %s", username, r.RemoteAddr)
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	// If we reach here, authentication succeeded!
	if debugEnabled {
		log.Printf("[DEBUG] Authenticated as '%s' (format: %s)", username, collectorAuthPasswordFormat)
	}

	// Check if the request body is gzip-compressed
	//
	// Monit can send compressed data to reduce bandwidth.
	// When compression is enabled, it sends:
	//   Content-Encoding: gzip
	//
	// r.Header.Get() retrieves a header value (case-insensitive)
	// If the header doesn't exist, it returns "" (empty string)
	//
	// strings.Contains() checks if a string contains a substring
	// We use it to handle variations like "gzip" or "gzip, deflate"
	contentEncoding := r.Header.Get("Content-Encoding")
	isGzipped := strings.Contains(strings.ToLower(contentEncoding), "gzip")

	// Create a reader for the request body
	// We'll either read it directly or decompress it first
	//
	// io.Reader is an interface - many types implement it:
	// - r.Body (HTTP request body)
	// - gzip.Reader (decompresses while reading)
	// - bytes.Buffer, files, etc.
	//
	// This is Go's way of abstraction - we can swap implementations easily
	var bodyReader io.Reader = r.Body

	if isGzipped {
		// Request is gzip-compressed, create a decompression reader
		//
		// gzip.NewReader() wraps the original reader and decompresses data
		// as we read from it. This is called "streaming decompression" -
		// we don't need to decompress everything into memory at once.
		//
		// How it works:
		// 1. We read from gzipReader
		// 2. gzipReader reads compressed data from r.Body
		// 3. gzipReader decompresses it on-the-fly
		// 4. We get decompressed data
		//
		// Returns:
		//   - *gzip.Reader: a reader that decompresses
		//   - error: nil if gzip header is valid, error if corrupted
		gzipReader, err := gzip.NewReader(r.Body)
		if err != nil {
			log.Printf("[ERROR] Failed to create gzip reader: %v", err)
			http.Error(w, "Failed to decompress request", http.StatusBadRequest)
			return
		}

		// Schedule the gzip reader to be closed when function exits
		// This is important to release internal buffers
		defer gzipReader.Close()

		// Use the gzip reader instead of the raw body
		bodyReader = gzipReader

		if debugEnabled {
			log.Printf("[DEBUG] Request is gzip-compressed, decompressing...")
		}
	}

	// Read the request body (XML data from Monit)
	//
	// io.ReadAll() reads all bytes from a Reader until EOF
	// bodyReader is either:
	//   - r.Body directly (uncompressed)
	//   - gzipReader (which decompresses from r.Body)
	//
	// Returns:
	//   - []byte: all the bytes read (decompressed if gzipped)
	//   - error: nil if successful, error if reading failed
	//
	// Why read all at once?
	// - Monit's XML is small (usually <100KB, even smaller when compressed)
	// - Simpler than streaming parse
	// - We need all data to parse XML anyway
	body, err := io.ReadAll(bodyReader)
	if err != nil {
		log.Printf("[ERROR] Failed to read request body: %v", err)
		http.Error(w, "Failed to read request body", http.StatusBadRequest)
		return
	}

	// Always close the request body when done
	//
	// defer means "run this when the function exits"
	// Closing the body:
	// - Releases resources (network socket, buffers)
	// - Allows connection reuse (HTTP keep-alive)
	// - Prevents resource leaks
	//
	// Why defer instead of immediate close?
	// - Function might return early (errors, etc.)
	// - defer ensures cleanup happens no matter how we exit
	// - Like "finally" in try/catch/finally
	defer r.Body.Close()

	// Log the size for debugging
	// Helps identify unusually large or small requests
	if debugEnabled {
		log.Printf("[DEBUG] Received %d bytes from %s", len(body), r.RemoteAddr)
	}

	// Parse the XML into our data structures
	//
	// parser.ParseMonitXML() does:
	// 1. Fix encoding declaration (ISO-8859-1 -> UTF-8)
	// 2. Parse XML into structs using encoding/xml
	// 3. Return populated MonitStatus struct
	//
	// Returns:
	//   - *MonitStatus: parsed data
	//   - error: nil if successful, error describing problem if failed
	status, err := parser.ParseMonitXML(body)
	if err != nil {
		// XML parsing failed
		// This could mean:
		// - Malformed XML
		// - Unexpected structure
		// - Encoding issues
		log.Printf("[ERROR] Failed to parse XML: %v", err)
		http.Error(w, "Failed to parse XML", http.StatusBadRequest)
		return
	}

	// Log what we received for debugging
	log.Printf("[INFO] Parsed status from %s: %d services",
		status.Server.LocalHostname, len(status.Services))

	// In debug mode, save the raw XML to /var/log for debugging
	//
	// This helps debug what data Monit is actually sending
	// Useful for:
	// - Verifying XML structure
	// - Checking if expected fields are present
	// - Troubleshooting parser issues
	if debugEnabled {
		// Create a safe filename from the hostname
		// Replace any characters that might cause filesystem issues
		safeHostname := status.Server.LocalHostname
		// In production, you might want to add more sanitization

		xmlFilePath := fmt.Sprintf("/tmp/cmonit.%s.xml", safeHostname)
		err := os.WriteFile(xmlFilePath, body, 0644)
		if err != nil {
			log.Printf("[WARN] Failed to write debug XML to %s: %v", xmlFilePath, err)
		} else {
			log.Printf("[DEBUG] Saved XML to %s", xmlFilePath)
		}
	}

	// Store everything in the database
	//
	// db.StoreMonitStatus() does:
	// 1. Store host information (hosts table)
	// 2. Store all services (services table)
	// 3. Extract and store metrics (metrics table)
	//
	// This is where all the data persistence happens!
	err = db.StoreMonitStatus(globalDB, status)
	if err != nil {
		// Database storage failed
		// Log the error but still return success to Monit
		// We don't want Monit to think we're down and stop sending data
		log.Printf("[ERROR] Failed to store status: %v", err)
		// Still return 200 OK (see comment below)
	}

	// Set response headers
	//
	// HTTP headers are metadata sent before the response body
	// They tell the client about the response (content type, server info, etc.)
	//
	// w.Header() returns a Header map (like a dictionary)
	// .Set(key, value) sets a header value

	// Tell the client what software we're running
	// Monit checks this to determine if it should use compression
	w.Header().Set("Server", "cmonit/0.1")

	// Tell the client we're sending plain text
	// Content-Type describes the format of the response body
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")

	// Send HTTP 200 OK status code
	//
	// w.WriteHeader() sends the status code and headers to the client
	// After calling this, you can't modify headers anymore
	//
	// HTTP status codes:
	//   - 200: OK (success)
	//   - 400: Bad Request (client error)
	//   - 401: Unauthorized (authentication required)
	//   - 500: Internal Server Error (server error)
	//
	// http.StatusOK is a constant equal to 200
	w.WriteHeader(http.StatusOK)

	// Write the response body
	//
	// fmt.Fprintf() writes formatted text to an io.Writer
	// w (ResponseWriter) implements io.Writer, so we can write to it
	//
	// This sends "OK\n" to the client
	// The \n adds a newline at the end
	fmt.Fprintf(w, "OK\n")

	// The function returns here
	// Go automatically sends the response to the client
	// No need to explicitly "send" like in some other languages
}

// buildAddress constructs a full listen address by combining host from listenAddr
// with port from collectorPort.
//
// This allows the -collector flag to specify just a port number, inheriting
// the IP address from the -listen flag for consistency.
//
// Parameters:
//   - listenAddr: The -listen flag value (e.g., "0.0.0.0:3000", "localhost:3000", "[::]:3000")
//   - collectorPort: The -collector flag value (e.g., "8080", ":8080", or "192.168.1.10:8080")
//
// Returns:
//   - A full address string for the collector (e.g., "0.0.0.0:8080")
//
// Behavior:
//   - If collectorPort is just a number (e.g., "8080"), prepend host from listenAddr
//   - If collectorPort starts with ":" (e.g., ":8080"), prepend host from listenAddr
//   - If collectorPort already contains a host, return it as-is (backwards compatibility)
//
// Examples:
//   - buildAddress("0.0.0.0:3000", "8080") -> "0.0.0.0:8080"
//   - buildAddress("localhost:3000", ":8080") -> "localhost:8080"
//   - buildAddress("[::]:3000", "8080") -> "[::]:8080"
//   - buildAddress("0.0.0.0:3000", "192.168.1.10:8080") -> "192.168.1.10:8080" (no change)
func buildAddress(listenAddr, collectorPort string) string {
	// Check if collectorPort already looks like a full address (contains ":")
	// but is not just ":port" format
	if strings.Contains(collectorPort, ":") && !strings.HasPrefix(collectorPort, ":") {
		// Already a full address like "192.168.1.10:8080", use as-is
		return collectorPort
	}

	// Extract host from listenAddr using strings.Cut
	//
	// strings.Cut splits on the first occurrence of ":"
	// Examples:
	// - IPv4: "0.0.0.0:3000" -> host="0.0.0.0", after="3000", found=true
	// - Hostname: "localhost:3000" -> host="localhost", after="3000", found=true
	// - Port only: ":3000" -> host="", after="3000", found=true
	// - IPv6: "[::]:3000" -> host="[::", after="]:3000", found=true (needs special handling)
	host, _, found := strings.Cut(listenAddr, ":")
	if !found || listenAddr == "" {
		// No ":" found or empty address, fall back to collectorPort as-is
		// This shouldn't happen with valid flag values
		if strings.HasPrefix(collectorPort, ":") {
			return collectorPort
		}
		return ":" + collectorPort
	}

	// Special handling for IPv6 addresses
	// If listenAddr is "[::]:3000", host will be "[::", we need to extract "::"
	if strings.HasPrefix(host, "[") {
		// IPv6 address, need to handle brackets
		// listenAddr is something like "[::]:3000"
		// We want to extract just the IPv6 address part
		endBracket := strings.Index(listenAddr, "]")
		if endBracket > 0 {
			host = listenAddr[1:endBracket] // Extract "::";
		}
	}

	// Handle empty host (when listenAddr starts with ":")
	if host == "" {
		// listenAddr was ":3000", which means all interfaces
		// Use "" which will result in ":port" format
		if strings.HasPrefix(collectorPort, ":") {
			return collectorPort
		}
		return ":" + collectorPort
	}

	// Normalize collectorPort to remove leading ":" if present
	port := strings.TrimPrefix(collectorPort, ":")

	// Combine host and port
	// For IPv6, wrap in brackets
	if strings.Contains(host, ":") {
		// IPv6 address, needs brackets
		return "[" + host + "]:" + port
	}

	// IPv4 or hostname
	return host + ":" + port
}

// parseSyslogFacility converts a facility string to syslog.Priority
//
// Supported facilities:
//   - daemon: LOG_DAEMON facility
//   - local0-local7: LOG_LOCAL0 through LOG_LOCAL7
//
// Returns:
//   - syslog.Priority: The priority combining facility and severity
//   - error: If the facility string is invalid
func parseSyslogFacility(facility string) (syslog.Priority, error) {
	// Map facility names to syslog constants
	// The priority combines facility (where to log) and severity (log level)
	// We use LOG_INFO as the default severity
	facilities := map[string]syslog.Priority{
		"daemon": syslog.LOG_DAEMON | syslog.LOG_INFO,
		"local0": syslog.LOG_LOCAL0 | syslog.LOG_INFO,
		"local1": syslog.LOG_LOCAL1 | syslog.LOG_INFO,
		"local2": syslog.LOG_LOCAL2 | syslog.LOG_INFO,
		"local3": syslog.LOG_LOCAL3 | syslog.LOG_INFO,
		"local4": syslog.LOG_LOCAL4 | syslog.LOG_INFO,
		"local5": syslog.LOG_LOCAL5 | syslog.LOG_INFO,
		"local6": syslog.LOG_LOCAL6 | syslog.LOG_INFO,
		"local7": syslog.LOG_LOCAL7 | syslog.LOG_INFO,
	}

	priority, ok := facilities[strings.ToLower(facility)]
	if !ok {
		return 0, fmt.Errorf("unknown facility '%s', supported: daemon, local0-local7", facility)
	}

	return priority, nil
}

// basicAuth wraps an HTTP handler with HTTP Basic Authentication.
//
// HTTP Basic Auth is a simple authentication scheme built into HTTP.
// The client must send username:password in the Authorization header.
//
// Supports two password formats:
// - "plain": Direct string comparison (less secure, default for backward compatibility)
// - "bcrypt": Secure bcrypt hash comparison (recommended for production)
//
// Parameters:
//   - next: The handler to wrap (will only be called if auth succeeds)
//   - username: Required username
//   - password: Required password (plain text or bcrypt hash depending on format)
//   - format: Password format ("plain" or "bcrypt")
//
// Returns:
//   - http.Handler: Wrapped handler that checks credentials first
//
// How it works:
// 1. Extract credentials from Authorization header
// 2. Compare username (always plain text comparison)
// 3. Compare password based on format:
//    - plain: Direct string comparison
//    - bcrypt: Hash the incoming password and compare with stored hash
// 4. If match: call next handler
// 5. If no match: return 401 Unauthorized
//
// Security notes:
// - ALWAYS use HTTPS to encrypt credentials in transit
// - Use bcrypt format for production deployments
// - Use strong passwords (12+ characters, mixed case, numbers, symbols)
// - Consider using environment variables instead of config files for credentials
func basicAuth(next http.Handler, username, password, format string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Get credentials from Authorization header
		//
		// r.BasicAuth() extracts username/password from the header
		// Returns:
		//   - user: the username
		//   - pass: the password
		//   - ok: true if Authorization header was present and valid format
		user, pass, ok := r.BasicAuth()

		// Check if authentication header was provided
		if !ok {
			w.Header().Set("WWW-Authenticate", `Basic realm="cmonit"`)
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			log.Printf("[WARNING] Authentication missing from %s", r.RemoteAddr)
			return
		}

		// Check username (always plain text comparison)
		if user != username {
			w.Header().Set("WWW-Authenticate", `Basic realm="cmonit"`)
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			log.Printf("[WARNING] Failed authentication attempt from %s (user: %s)", r.RemoteAddr, user)
			return
		}

		// Check password based on format
		var passwordMatch bool

		if format == "bcrypt" {
			// Bcrypt comparison
			//
			// bcrypt.CompareHashAndPassword() does:
			// 1. Extracts the salt from the stored hash
			// 2. Hashes the incoming password with that salt
			// 3. Compares the resulting hash with the stored hash
			//
			// Returns nil if match, error if mismatch
			//
			// This is secure because:
			// - Each password has a unique salt (prevents rainbow table attacks)
			// - Bcrypt is intentionally slow (prevents brute force)
			// - Cost factor can be increased as hardware improves
			err := bcrypt.CompareHashAndPassword([]byte(password), []byte(pass))
			passwordMatch = (err == nil)
		} else {
			// Plain text comparison (default)
			//
			// Direct string comparison
			// Less secure but simpler for development/testing
			passwordMatch = (pass == password)
		}

		if !passwordMatch {
			// Authentication failed - return 401 Unauthorized
			//
			// WWW-Authenticate header tells the browser to show login dialog
			// Basic realm="..." is the authentication realm (domain/area)
			w.Header().Set("WWW-Authenticate", `Basic realm="cmonit"`)

			// 401 = Unauthorized (though "Unauthenticated" would be more accurate)
			// This tells the client that authentication is required
			http.Error(w, "Unauthorized", http.StatusUnauthorized)

			// Log the failed attempt (security audit trail)
			// Don't log the password for security
			log.Printf("[WARNING] Failed authentication attempt from %s (user: %s)", r.RemoteAddr, user)
			return
		}

		// Authentication succeeded - call the next handler
		//
		// next.ServeHTTP() passes the request to the wrapped handler
		// The request continues normally from here
		next.ServeHTTP(w, r)
	})
}
