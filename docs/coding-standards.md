# cmonit Coding Standards

## Overview

All code in this project must be **extensively commented** to serve as educational material for someone learning Go. Think of every file as a tutorial.

## Comment Philosophy

**Goal:** Someone who has never seen Go code before should be able to understand what's happening by reading the comments.

### Good vs Bad Examples

❌ **Bad (minimal comments):**
```go
func parseXML(data []byte) (*Status, error) {
    var status Status
    err := xml.Unmarshal(data, &status)
    if err != nil {
        return nil, err
    }
    return &status, nil
}
```

✅ **Good (educational comments):**
```go
// parseXML takes raw XML data from a Monit agent and converts it into a Status struct
//
// Parameters:
//   - data: Raw XML bytes received from the HTTP request body
//
// Returns:
//   - *Status: A pointer to the parsed Status struct (pointer allows us to return nil on error)
//   - error: Any error that occurred during parsing, or nil if successful
//
// How it works:
// 1. Create an empty Status struct to hold the parsed data
// 2. Use Go's xml.Unmarshal() to parse the XML into the struct
// 3. Check if parsing failed (err != nil means there was an error)
// 4. Return the result
func parseXML(data []byte) (*Status, error) {
    // Create an empty Status struct
    // var declares a variable, Status is the type
    var status Status

    // xml.Unmarshal() parses XML data into a Go struct
    // It takes two parameters:
    //   1. data ([]byte) - the XML as raw bytes
    //   2. &status - the address of our struct (& means "address of")
    // It returns an error if parsing fails, or nil if successful
    err := xml.Unmarshal(data, &status)

    // Check if there was an error
    // In Go, "if err != nil" is the standard way to check for errors
    // nil means "no error" in Go
    if err != nil {
        // Return nil for the status (nothing parsed) and the error
        // In Go, we can return multiple values from a function
        return nil, err
    }

    // Success! Return the parsed status and nil (no error)
    // &status means "the address of the status variable"
    // We return a pointer (*Status) rather than the value to avoid copying large structs
    return &status, nil
}
```

## Commenting Rules

### 1. File Header Comments

Every file must start with a comment explaining:
- What this file does
- What package it belongs to
- Key responsibilities

```go
// Package api handles all HTTP endpoints for the cmonit collector.
//
// This file (collector.go) specifically implements the /collector endpoint
// that receives POST requests from Monit agents. The collector:
// - Validates HTTP Basic Authentication
// - Parses XML status data from Monit
// - Stores the data in the SQLite database
// - Returns appropriate HTTP responses
//
// The collector endpoint is the core of cmonit - without it, we can't
// receive data from Monit agents.
package api
```

### 2. Function Comments

Every function needs:
- What it does (in plain English)
- What each parameter means
- What it returns
- Step-by-step explanation of the logic

```go
// HandleCollector is the HTTP handler for POST /collector
//
// This function is called every time a Monit agent sends status data.
// It runs for each HTTP request in its own goroutine (lightweight thread).
//
// Parameters:
//   - w: http.ResponseWriter - lets us write the HTTP response back to Monit
//   - r: *http.Request - contains the incoming HTTP request data (headers, body, etc.)
//
// How it works:
// 1. Validate the HTTP method (must be POST)
// 2. Check HTTP Basic Authentication
// 3. Read the request body (XML data)
// 4. Parse the XML into Go structs
// 5. Store the data in the database
// 6. Send HTTP 200 OK response
func HandleCollector(w http.ResponseWriter, r *http.Request) {
    // ... implementation with inline comments
}
```

### 3. Struct Comments

Every struct and its fields need explanation:

```go
// Host represents a single monitored server/machine in our system
// Each host runs a Monit agent that sends us status updates
//
// This struct maps to the "hosts" table in SQLite
type Host struct {
    // ID is the unique identifier from Monit (like a UUID)
    // This comes from the Monit configuration and stays the same even if
    // the hostname changes
    ID string `json:"id" db:"id"`

    // Hostname is the human-readable name of the server (e.g., "web-server-01")
    // This is what we display in the UI
    Hostname string `json:"hostname" db:"hostname"`

    // Incarnation is a timestamp (Unix seconds) of when Monit was started
    // If Monit restarts, this changes - helps us detect restarts
    Incarnation int64 `json:"incarnation" db:"incarnation"`

    // Version is the Monit version string (e.g., "5.35.2")
    Version string `json:"version" db:"version"`

    // LastSeen is when we last received data from this host
    // We use this to detect if a host has gone offline
    // time.Time is Go's built-in type for dates and times
    LastSeen time.Time `json:"last_seen" db:"last_seen"`
}
```

### 4. Inline Comments

Comment complex logic, especially:
- Why something is done (not just what)
- Go-specific idioms that might be confusing
- Error handling patterns
- Concurrency/goroutines

```go
// Read the HTTP request body
// io.ReadAll() reads all bytes until EOF (End Of File)
// It returns ([]byte, error) - the data and any error that occurred
body, err := io.ReadAll(r.Body)
if err != nil {
    // Log the error for debugging
    // log.Printf() writes to standard error with a timestamp
    log.Printf("Error reading request body: %v", err)

    // http.Error() sends an HTTP error response to the client
    // It takes: (ResponseWriter, error message string, HTTP status code)
    http.Error(w, "Failed to read request body", http.StatusBadRequest)

    // return exits the function early
    // Since we encountered an error, there's nothing more to do
    return
}

// Don't forget to close the request body!
// defer means "run this when the function exits"
// This is Go's way of ensuring cleanup happens, like try/finally in other languages
defer r.Body.Close()
```

### 5. Explain Go Idioms

Go has patterns that might be unfamiliar. Always explain them:

#### Error Handling
```go
// Go doesn't have exceptions like try/catch
// Instead, functions return errors as values
// The standard pattern is:
//   1. Call a function that returns (result, error)
//   2. Check if err != nil (nil means no error)
//   3. Handle the error or return it to the caller
result, err := doSomething()
if err != nil {
    // Handle or return the error
    return nil, err
}
```

#### Defer
```go
// defer schedules a function to run when the current function exits
// Think of it like "finally" in try/catch/finally
// This is commonly used for cleanup (closing files, database connections, etc.)
file, err := os.Open("data.txt")
if err != nil {
    return err
}
defer file.Close() // This will run when the function exits, even if there's an error later
```

#### Pointers
```go
// Go has pointers (addresses in memory), marked with *
// & means "address of" and * means "value at this address"

// Create a variable
x := 42

// Get its address (pointer)
ptr := &x  // ptr is a pointer to x

// Get the value from a pointer (dereference)
value := *ptr  // value is now 42

// Why use pointers?
// 1. Avoid copying large structs
// 2. Allow functions to modify the original variable
// 3. Represent "optional" values (nil pointer = no value)
```

#### := vs =
```go
// := declares a new variable and infers its type
// Use this when creating a new variable
name := "Alice"  // Declares 'name' as a string

// = assigns to an existing variable
// Use this when modifying an existing variable
name = "Bob"  // Changes the existing 'name' variable
```

#### Goroutines
```go
// A goroutine is a lightweight thread
// go keyword runs a function concurrently
go doSomethingInBackground()

// The main function continues immediately - doesn't wait
// Goroutines are how Go handles concurrency efficiently
```

### 6. SQL Query Comments

Explain SQL queries in detail:

```go
// SQL query to insert a new host into the database
// INSERT OR REPLACE means:
//   - If the ID already exists, update the row
//   - If the ID doesn't exist, insert a new row
// This is SQLite's version of "UPSERT" (update or insert)
//
// The ? are placeholders - they get replaced with actual values
// Using placeholders prevents SQL injection attacks
const insertHostSQL = `
    INSERT OR REPLACE INTO hosts (
        id,          -- Monit's unique ID
        hostname,    -- Server name
        incarnation, -- When Monit started
        version,     -- Monit version
        last_seen    -- Current timestamp
    ) VALUES (?, ?, ?, ?, ?)
`

// Execute the query with actual values
// The ? placeholders are replaced with these values in order
// db.Exec() returns (Result, error)
result, err := db.Exec(insertHostSQL,
    host.ID,           // Replaces first ?
    host.Hostname,     // Replaces second ?
    host.Incarnation,  // Replaces third ?
    host.Version,      // Replaces fourth ?
    time.Now(),        // Replaces fifth ? with current time
)
```

### 7. HTTP Handler Comments

Explain HTTP concepts:

```go
// Set the response header to indicate we're an M/Monit-compatible server
// HTTP headers are metadata sent before the response body
//
// "Server" header tells the client what software is running
// Monit checks this header to decide if it should compress future requests
//
// w.Header().Set() sets an HTTP header
//   - First parameter: header name
//   - Second parameter: header value
w.Header().Set("Server", "cmonit/0.1")

// Set Content-Type to tell the client we're sending plain text
// Content-Type describes the format of the response body
w.Header().Set("Content-Type", "text/plain; charset=utf-8")

// Send HTTP 200 OK status
// 200 means "success" in HTTP
// Status codes:
//   - 2xx = success
//   - 4xx = client error (like 404 Not Found)
//   - 5xx = server error (like 500 Internal Server Error)
w.WriteHeader(http.StatusOK)

// Write the response body (content sent to the client)
// fmt.Fprintf() writes formatted text
// It's like printf() but writes to an io.Writer (in this case, our HTTP response)
fmt.Fprintf(w, "OK\n")
```

## Code Organization Comments

### Package-Level Comments

```go
// Package db provides all database operations for cmonit.
//
// Database Structure:
// - SQLite database with 4 main tables: hosts, services, metrics, events
// - Uses database/sql package (Go's standard SQL interface)
// - Uses mattn/go-sqlite3 driver (imported with blank identifier)
//
// Key Functions:
// - InitDB(): Creates the database file and tables
// - StoreHost(): Saves host information
// - StoreMetrics(): Saves time-series data
//
// Thread Safety:
// - database/sql package handles connection pooling automatically
// - Safe to call from multiple goroutines (concurrent requests)
package db
```

### Import Comments

Explain unusual imports:

```go
import (
    // Standard library imports (built into Go)
    "database/sql"  // SQL database interface
    "encoding/xml"  // XML parsing
    "fmt"           // Formatted I/O (like printf)
    "log"           // Logging to stderr
    "net/http"      // HTTP client and server
    "time"          // Time and date functions

    // Third-party imports (need 'go get' to install)
    // The _ means "import for side effects only"
    // We don't use sqlite3 directly, but importing it registers the driver
    _ "github.com/mattn/go-sqlite3"  // SQLite driver
)
```

## Example: Fully Commented Function

Here's a complete example of how thorough comments should be:

```go
// StoreHost saves or updates a host record in the database
//
// This function is called every time we receive status data from a Monit agent.
// If the host ID already exists, it updates the last_seen timestamp.
// If it's a new host, it creates a new record.
//
// Parameters:
//   - db: Database connection (pointer to sql.DB)
//   - host: Host struct containing the data to store
//
// Returns:
//   - error: nil if successful, error describing what went wrong if failed
//
// Database behavior:
// - Uses INSERT OR REPLACE (SQLite's UPSERT)
// - Creates the hosts table if it doesn't exist
// - Thread-safe (can be called from multiple goroutines)
func StoreHost(db *sql.DB, host *Host) error {
    // SQL query to insert or update the host record
    //
    // INSERT OR REPLACE is SQLite's way of saying:
    // "If a row with this ID exists, replace it. Otherwise, insert a new row."
    //
    // The ? are placeholders that will be replaced with actual values
    // This prevents SQL injection attacks (a security vulnerability where
    // user input could modify the SQL query)
    const query = `
        INSERT OR REPLACE INTO hosts (
            id,
            hostname,
            incarnation,
            version,
            last_seen,
            created_at
        ) VALUES (?, ?, ?, ?, ?, COALESCE(
            (SELECT created_at FROM hosts WHERE id = ?),
            ?
        ))
    `

    // Get the current time
    // time.Now() returns the current moment as a time.Time value
    now := time.Now()

    // Execute the SQL query
    //
    // db.Exec() runs a query that doesn't return rows (INSERT, UPDATE, DELETE)
    // For queries that return rows (SELECT), we'd use db.Query() instead
    //
    // Parameters after the query replace the ? placeholders in order:
    //   1st ? = host.ID
    //   2nd ? = host.Hostname
    //   etc.
    //
    // db.Exec() returns two values:
    //   - sql.Result: information about the query (rows affected, etc.)
    //   - error: nil if successful, error object if failed
    //
    // We use _ (underscore) to ignore the Result since we don't need it
    // In Go, you must explicitly ignore unused return values with _
    _, err := db.Exec(
        query,
        host.ID,           // 1st ? - the unique host ID
        host.Hostname,     // 2nd ? - the hostname
        host.Incarnation,  // 3rd ? - when Monit started
        host.Version,      // 4th ? - Monit version
        now,               // 5th ? - last_seen timestamp
        host.ID,           // 6th ? - for the subquery (preserve created_at)
        now,               // 7th ? - created_at if new host
    )

    // Check if the query failed
    // This is Go's standard error handling pattern:
    //   1. Call a function that returns an error
    //   2. Check if err != nil (nil means "no error")
    //   3. Handle or return the error
    if err != nil {
        // Log the error for debugging
        // log.Printf() writes to stderr with a timestamp
        // %v is a placeholder that prints the error message
        log.Printf("Failed to store host %s: %v", host.Hostname, err)

        // Return the error to the caller
        // The caller can then decide what to do (retry, log, show user error, etc.)
        return err
    }

    // Success! Return nil (no error)
    return nil
}
```

## Testing Comments

Test functions should also be educational:

```go
// TestStoreHost verifies that we can store and retrieve a host from the database
//
// This is a unit test - it tests one specific function in isolation
// Go's testing package finds all functions named TestXxx and runs them
//
// Test strategy:
// 1. Create an in-memory SQLite database (fast, no file I/O)
// 2. Initialize the schema
// 3. Create a test host
// 4. Store it in the database
// 5. Query it back
// 6. Verify the data matches what we stored
func TestStoreHost(t *testing.T) {
    // t *testing.T is provided by Go's testing framework
    // We use it to report test failures with t.Errorf() or t.Fatalf()

    // Create an in-memory SQLite database for testing
    // ":memory:" is a special SQLite syntax meaning "don't use a file"
    // This makes tests fast and avoids leaving test data on disk
    db, err := sql.Open("sqlite3", ":memory:")
    if err != nil {
        // t.Fatalf() fails the test immediately and stops execution
        // Use this for errors that prevent the rest of the test from running
        t.Fatalf("Failed to open test database: %v", err)
    }
    // defer means "run this when the function exits"
    // Ensures the database connection is closed even if the test fails
    defer db.Close()

    // Create the database schema
    err = InitDB(db)
    if err != nil {
        t.Fatalf("Failed to initialize database: %v", err)
    }

    // Create a test host
    // Use realistic but obviously fake data
    testHost := &Host{
        ID:          "test-id-12345",
        Hostname:    "test-server",
        Incarnation: 1234567890,
        Version:     "5.35.2",
    }

    // Store the host
    err = StoreHost(db, testHost)
    if err != nil {
        t.Fatalf("Failed to store host: %v", err)
    }

    // Query it back
    var retrieved Host
    err = db.QueryRow(
        "SELECT id, hostname, incarnation, version FROM hosts WHERE id = ?",
        testHost.ID,
    ).Scan(&retrieved.ID, &retrieved.Hostname, &retrieved.Incarnation, &retrieved.Version)

    if err != nil {
        t.Fatalf("Failed to retrieve host: %v", err)
    }

    // Verify the data matches
    // Use t.Errorf() for failures that don't prevent other checks
    if retrieved.Hostname != testHost.Hostname {
        t.Errorf("Hostname mismatch: got %s, want %s",
            retrieved.Hostname, testHost.Hostname)
    }

    if retrieved.Version != testHost.Version {
        t.Errorf("Version mismatch: got %s, want %s",
            retrieved.Version, testHost.Version)
    }

    // If we get here without calling t.Errorf() or t.Fatalf(), the test passes!
}
```

## Documentation Comments

Go has a built-in documentation system. Public functions should use this format:

```go
// StoreHost saves or updates a host in the database.
//
// The host is identified by its ID field. If a host with this ID already exists,
// it will be updated. Otherwise, a new host record is created.
//
// Example:
//
//     host := &Host{
//         ID: "abc123",
//         Hostname: "web-server-01",
//         Version: "5.35.2",
//     }
//     err := StoreHost(db, host)
//     if err != nil {
//         log.Fatal(err)
//     }
//
// This function is safe to call from multiple goroutines simultaneously.
func StoreHost(db *sql.DB, host *Host) error {
    // ...
}
```

These comments appear in `go doc` output and IDE tooltips.

## Summary

### Comment Checklist

For every file:
- [ ] File header explaining purpose
- [ ] Package comment
- [ ] Import comments (especially for unusual imports)

For every function:
- [ ] What it does (in plain English)
- [ ] What parameters mean
- [ ] What it returns
- [ ] Step-by-step logic explanation
- [ ] Error handling explanation

For every struct:
- [ ] What it represents
- [ ] What each field means
- [ ] Struct tags explained (json, db, xml)

For complex code:
- [ ] Inline comments explaining why (not just what)
- [ ] Go idioms explained
- [ ] SQL queries documented
- [ ] HTTP concepts explained
- [ ] Concurrency patterns explained

### Golden Rule

**If you had to explain this code to someone who has never seen Go before, what would you say?**

Write that in the comments!

## Benefits

Extensive commenting:
- ✅ Helps you understand your own code later
- ✅ Makes code review easier
- ✅ Serves as documentation
- ✅ Helps others learn Go
- ✅ Catches logic errors (if you can't explain it, it might be wrong)
- ✅ Makes debugging faster

## Remember

**You're not just writing code - you're teaching Go through working examples!**
