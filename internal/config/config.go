// Package config provides configuration file support for cmonit.
//
// cmonit supports both command-line flags and configuration files:
// - CLI flags take highest priority (override config file)
// - Config file provides defaults
// - Built-in defaults are used if neither is specified
//
// Configuration files use TOML format for readability and structure.
package config

import (
	"fmt"
	"os"

	"github.com/BurntSushi/toml"
)

// Config represents the complete cmonit configuration.
//
// This struct maps directly to the TOML file format:
//
//	[network]
//	listen = "0.0.0.0:3000"
//	collector_port = 8080
//
//	[collector]
//	user = "monit"
//	password = "monit"
//
// Fields use TOML tags to map config file keys to struct fields.
// The `toml:"key"` tag specifies the TOML key name.
type Config struct {
	Network   NetworkConfig   `toml:"network"`
	Collector CollectorConfig `toml:"collector"`
	Web       WebConfig       `toml:"web"`
	Storage   StorageConfig   `toml:"storage"`
	Logging   LoggingConfig   `toml:"logging"`
	Process   ProcessConfig   `toml:"process"`
}

// NetworkConfig contains network/listening configuration.
type NetworkConfig struct {
	// Listen is the web UI listen address (host:port)
	// Example: "0.0.0.0:3000", "localhost:3000", "[::]:3000"
	Listen string `toml:"listen"`

	// CollectorPort is the collector port number (inherits IP from Listen)
	// Example: 8080, 9000
	CollectorPort string `toml:"collector_port"`
}

// CollectorConfig contains collector authentication settings.
type CollectorConfig struct {
	// User is the HTTP Basic Auth username for collector endpoint
	// Monit agents must use this username to authenticate
	User string `toml:"user"`

	// Password is the HTTP Basic Auth password for collector endpoint
	// Can be either plain text or bcrypt hash depending on PasswordFormat
	// Monit agents must use this password to authenticate
	Password string `toml:"password"`

	// PasswordFormat specifies the format of the Password field
	// Valid values: "plain" (default) or "bcrypt"
	// When "bcrypt", Password should be a bcrypt hash (e.g., from cmonit -hash-password)
	PasswordFormat string `toml:"password_format"`
}

// WebConfig contains web UI settings.
type WebConfig struct {
	// User is the HTTP Basic Auth username for web UI
	// Empty string disables authentication
	User string `toml:"user"`

	// Password is the HTTP Basic Auth password for web UI
	// Can be either plain text or bcrypt hash depending on PasswordFormat
	// Empty string disables authentication
	Password string `toml:"password"`

	// PasswordFormat specifies the format of the Password field
	// Valid values: "plain" (default) or "bcrypt"
	// When "bcrypt", Password should be a bcrypt hash (e.g., from htpasswd or cmonit -hash-password)
	PasswordFormat string `toml:"password_format"`

	// Cert is the TLS certificate file path for HTTPS (applies to both Web UI and Collector)
	// Empty string disables TLS (uses HTTP)
	Cert string `toml:"cert"`

	// Key is the TLS key file path for HTTPS (applies to both Web UI and Collector)
	// Empty string disables TLS (uses HTTP)
	Key string `toml:"key"`
}

// StorageConfig contains database and file storage settings.
type StorageConfig struct {
	// Database is the SQLite database file path
	Database string `toml:"database"`

	// PidFile is the PID file path
	PidFile string `toml:"pidfile"`
}

// LoggingConfig contains logging settings.
type LoggingConfig struct {
	// Syslog is the syslog facility (daemon, local0-local7)
	// Empty string logs to stderr
	Syslog string `toml:"syslog"`

	// Debug enables verbose debug logging
	Debug bool `toml:"debug"`
}

// ProcessConfig contains process control settings.
type ProcessConfig struct {
	// Daemon runs cmonit as a background daemon
	Daemon bool `toml:"daemon"`
}

// Load reads and parses a TOML configuration file.
//
// The function:
// 1. Checks if the file exists
// 2. Reads the file contents
// 3. Parses the TOML format
// 4. Returns a populated Config struct
//
// Parameters:
//   - path: Path to the TOML configuration file
//
// Returns:
//   - *Config: Parsed configuration
//   - error: Any error that occurred (file not found, parse error, etc.)
//
// Example usage:
//
//	cfg, err := config.Load("/etc/cmonit/cmonit.toml")
//	if err != nil {
//	    log.Fatalf("Failed to load config: %v", err)
//	}
func Load(path string) (*Config, error) {
	// Check if file exists
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return nil, fmt.Errorf("config file does not exist: %s", path)
	}

	// Create empty config struct
	var cfg Config

	// Parse TOML file
	//
	// toml.DecodeFile() reads the file and populates the struct
	// It uses struct tags to map TOML keys to struct fields
	//
	// The decoder is strict - unknown keys will cause an error
	// This helps catch typos in config files
	if _, err := toml.DecodeFile(path, &cfg); err != nil {
		return nil, fmt.Errorf("failed to parse config file: %w", err)
	}

	return &cfg, nil
}

// Merge combines configuration from multiple sources with priority.
//
// Priority order (highest to lowest):
// 1. CLI flags (if provided, i.e., not empty string or default value)
// 2. Config file values
// 3. CLI flag defaults (already set if CLI flag not provided)
//
// This function updates the config struct with CLI flag values when they
// differ from their defaults, effectively giving CLI flags the highest priority.
//
// Parameters:
//   - cfg: Configuration loaded from file
//   - cliValue: Value from CLI flag
//   - defaultValue: Default value for the CLI flag
//
// Returns:
//   - string: The merged value (CLI flag if set, otherwise config file value, otherwise default)
//
// Example:
//
//	// User ran: ./cmonit -config app.toml -debug
//	// Config file has: listen = "0.0.0.0:3000"
//	// CLI flag -listen was not specified (uses default "localhost:3000")
//	//
//	// Result: listen = "0.0.0.0:3000" (from config file)
//	// Result: debug = true (from CLI flag)
func MergeString(cfgValue, cliValue, defaultValue string) string {
	// If CLI flag was explicitly set (differs from default), use it
	if cliValue != defaultValue {
		return cliValue
	}

	// If config file has a value, use it
	if cfgValue != "" {
		return cfgValue
	}

	// Otherwise use the CLI default
	return cliValue
}

// MergeBool merges boolean configuration values with priority.
//
// For booleans, we consider the CLI flag "set" if it's true,
// since false is the default for most boolean flags.
//
// Priority:
// 1. CLI flag (if true)
// 2. Config file value
// 3. CLI default (false)
func MergeBool(cfgValue, cliValue bool) bool {
	// If CLI flag is true, it was explicitly set
	if cliValue {
		return true
	}

	// Otherwise use config file value
	return cfgValue
}
