# cmonit - Central Monit Monitor

Open Source central monitoring dashboard for Monit: collect status, graph metrics, and control services across multiple hosts.

![cmonit Status](docs/status.png)
![cmonit Host](docs/host.png)

## Features

### Monitoring & Visualization
- **Multi-page dashboard**: Status overview, host details, and events pages
- **Real-time status**: Color-coded status indicators (green/orange/red/gray)
- **System metrics**: CPU, Memory, Load average with time-series graphs
- **Multiple time ranges**: 1h, 6h, 24h for historical data visualization
- **Platform information**: OS, CPU count, memory, uptime display
- **Process monitoring**: PID, CPU%, memory usage for process services

### Events & Alerts
- **Event tracking**: Automatic logging of service state changes
- **Monit restart detection**: Tracks Monit daemon uptime and detects restarts
- **Event history**: View all events per host with timestamps and details
- **Heartbeat-based health status**: Visual indicators (green/yellow/red) based on poll interval
- **Stale host detection**: Automatic detection of offline hosts with configurable thresholds

### Host Lifecycle Management
- **Health indicators**: Color-coded status based on last heartbeat and poll interval
  - Green: Healthy (last_seen < poll_interval × 2)
  - Yellow: Warning (poll_interval × 2 ≤ last_seen < poll_interval × 5)
  - Red: Offline (last_seen ≥ poll_interval × 5)
- **Host deletion**: Remove offline hosts with safety checks (requires >1 hour offline)
- **Cascade deletion**: Automatically removes all associated services, metrics, and events
- **Deletion confirmation**: Requires hostname verification to prevent accidental removal

### Service Control
- **Remote actions**: Start, stop, restart services from the dashboard
- **Monitor control**: Enable/disable monitoring for individual services
- **Real-time feedback**: Action confirmation and status updates

### Security & Deployment
- **HTTP Basic Authentication**: Protect web UI with username/password
- **TLS/HTTPS support**: Encrypted connections with certificate support
- **Configurable addresses**: IPv4/IPv6, custom ports, specific interface binding
- **SQLite database**: Reliable storage with WAL mode for concurrency
- **Syslog integration**: Daemon logging for production environments

## Quick Start

### Build

```bash
go build -o cmonit ./cmd/cmonit
```

### Run

#### Quick Start (CLI only)

```bash
# Default: collector on localhost:8080, web on localhost:3000
./cmonit

# Web accessible from all interfaces (both collector and web UI)
./cmonit -listen 0.0.0.0:3000

# IPv6 support
./cmonit -listen [::]:3000

# Custom ports (collector inherits IP from -listen)
./cmonit -collector 9000 -listen 0.0.0.0:4000

# Specific IP address
./cmonit -listen 192.168.1.10:3000

# Custom database path
./cmonit -db /var/db/cmonit.db

# Custom PID file location
./cmonit -pidfile /tmp/cmonit.pid

# Log to syslog (daemon facility)
./cmonit -syslog daemon

# Log to syslog (local0 facility)
./cmonit -syslog local0

# Development mode (current directory, stderr logging)
./cmonit -db ./cmonit.db -pidfile ./cmonit.pid

# Run as daemon in background
./cmonit -daemon

# Custom collector credentials (Monit agents must match)
./cmonit -collector-user myuser -collector-password mypassword

# With HTTP Basic Authentication
./cmonit -web-user admin -web-password secretpass

# With TLS/HTTPS
./cmonit -web-cert /path/to/cert.pem -web-key /path/to/key.pem

# Production: Authentication + TLS
./cmonit -listen 0.0.0.0:3000 -web-user admin -web-password secretpass -web-cert /path/to/cert.pem -web-key /path/to/key.pem
```

#### Configuration File (Production/Complex Deployments)

For production environments or complex configurations, use a TOML configuration file:

```bash
# Create config file
cp cmonit.conf.sample /etc/cmonit/cmonit.conf
vim /etc/cmonit/cmonit.conf

# Run with config file
./cmonit -config /etc/cmonit/cmonit.conf

# Override specific settings with CLI flags (CLI takes priority)
./cmonit -config /etc/cmonit/cmonit.conf -debug -listen 127.0.0.1:3000
```

**Example configuration file** (TOML format):

```toml
[network]
listen = "0.0.0.0:3000"
collector_port = "8080"

[collector]
user = "monit"
password = "secretpassword"

[web]
user = "admin"
password = "adminpassword"
cert = "/etc/cmonit/ssl/cert.pem"
key = "/etc/cmonit/ssl/key.pem"

[storage]
database = "/var/lib/cmonit/cmonit.db"
pidfile = "/var/run/cmonit/cmonit.pid"

[logging]
syslog = "daemon"
debug = false

[process]
daemon = true
```

**Benefits:**
- Organized, grouped settings
- Version control friendly
- Comments for documentation
- Easier to manage complex configurations
- CLI flags still work (and override config file)

See `cmonit.conf.sample` for a fully documented configuration file.

### Command-Line Options

```
  -config string
        Configuration file path (TOML format, optional)
        Example: /etc/cmonit/cmonit.conf
        Note: CLI flags override config file settings

  -collector string
        Collector port number - inherits IP address from -listen (default "8080")
        Examples: 8080, 9000
        Note: Collector listens on same IP as -listen with this port

  -listen string
        Web UI listen address (default "localhost:3000")
        Examples: localhost:3000, 0.0.0.0:3000, [::]:3000, 192.168.1.10:3000
        Note: Collector inherits the IP address from this flag

  -db string
        Database file path (default "/var/run/cmonit/cmonit.db")

  -pidfile string
        PID file path (default "/var/run/cmonit/cmonit.pid")

  -collector-user string
        Collector HTTP Basic Auth username - Monit agents must use this (default "monit")

  -collector-password string
        Collector HTTP Basic Auth password - Monit agents must use this (default "monit")

  -daemon
        Run in background as a daemon process

  -syslog string
        Syslog facility for daemon logging (daemon, local0-local7)
        Leave empty for stderr logging (default: empty)

  -web-user string
        Web UI HTTP Basic Auth username (empty = no authentication)

  -web-password string
        Web UI HTTP Basic Auth password (empty = no authentication)

  -web-cert string
        Web UI TLS certificate file (empty = HTTP only)

  -web-key string
        Web UI TLS key file (empty = HTTP only)
```

### Access

Open your browser to the configured web address:
- Default: **http://localhost:3000/**
- All interfaces: **http://your-server-ip:3000/**

### Dashboard Pages

cmonit provides a multi-page interface:

1. **Status Overview** (`/`)
   - Table view of all monitored hosts
   - Real-time status indicators (green=OK, orange=warning, red=critical, gray=unknown)
   - CPU and Memory percentages for each host
   - Stale host detection (hosts not seen in 5+ minutes)
   - Event counts per host
   - Click hostname to view details

2. **Host Detail** (`/host/{host_id}`)
   - Detailed platform information (OS, CPU, memory, uptime)
   - Service table with status, monitoring state, and resource usage
   - Action buttons: start, stop, restart, monitor, unmonitor
   - Real-time system metrics graphs (Load, CPU, Memory)
   - Time range selector (1h, 6h, 24h)
   - "View Events" button to see host event history

3. **Events Page** (`/host/{host_id}/events`)
   - Chronological list of events (newest first)
   - Event types: Monit restarts, service state changes
   - Timestamps and detailed messages
   - Auto-refresh every 60 seconds

## Configure Monit Agents

Add to your monitrc file:

```
set mmonit
  http://monit:monit@cmonit-server:8080/collector
set httpd port 2812 and
  allow cmonit-server
  allow user:password
```

Replace `cmonit-server` with the hostname or IP where cmonit is running.

**Note**: The default collector credentials are `monit:monit`. If you change them using `-collector-user` and `-collector-password` flags, update all Monit agents accordingly.

Example:
```bash
# Default credentials
set mmonit http://monit:monit@192.168.1.100:8080/collector

# Custom credentials (if cmonit started with -collector-user myuser -collector-password mypass)
set mmonit http://myuser:mypass@192.168.1.100:8080/collector

# Local connection
set mmonit http://monit:monit@localhost:8080/collector
```

Then reload Monit:
```bash
monit reload
```

## Architecture

```
Monit Agent → localhost:8080/collector → SQLite → localhost:3000 Dashboard
```

Note: Both collector and web UI listen on the same IP address (specified by `-listen`). The collector uses port 8080 (configurable with `-collector`) and web UI uses port 3000 (part of `-listen`).

## Security

### Web UI Authentication

Protect your dashboard with HTTP Basic Authentication:

```bash
./cmonit -web-user admin -web-password your-secure-password
```

When enabled, all web requests will require authentication. Failed attempts are logged for security auditing.

### TLS/HTTPS Support

Enable encrypted connections with TLS certificates:

```bash
# Generate self-signed certificate (testing only)
openssl req -x509 -newkey rsa:4096 -keyout key.pem -out cert.pem -days 365 -nodes

# Run with TLS
./cmonit -web-cert cert.pem -web-key key.pem
```

For production, use certificates from a trusted CA (Let's Encrypt, etc.).

### Production Security

Recommended production configuration:

```bash
./cmonit \
  -listen 0.0.0.0:3000 \
  -web-user admin \
  -web-password "$(cat /etc/cmonit/password)" \
  -web-cert /etc/cmonit/cert.pem \
  -web-key /etc/cmonit/key.pem \
  -syslog daemon
```

Additional recommendations:

- **Default**: Web UI listens on `localhost:3000` (local connections only)
- **Firewall**: Restrict collector port (8080) to trusted Monit agents
- **Authentication**: Both Web UI and collector support HTTP Basic Auth
- **Passwords**: Store credentials securely, never commit to version control
- **Certificates**: Use valid certificates from a trusted CA for production
- **Logging**: Enable syslog for security event monitoring

## Development

```bash
# Run tests
go test ./...

# Clean database
rm -f cmonit.db cmonit.db-*

# Rebuild and run
go build -o cmonit ./cmd/cmonit && ./cmonit

# Show help
./cmonit -h
```

## FreeBSD Installation

For FreeBSD systems, an rc.d startup script is provided:

```bash
# Install the binary
sudo cp cmonit /usr/local/bin/

# Install the rc.d script
sudo cp rc.d/cmonit /usr/local/etc/rc.d/
sudo chmod +x /usr/local/etc/rc.d/cmonit

# Install config file (recommended)
sudo mkdir -p /etc/cmonit
sudo cp cmonit.conf.sample /etc/cmonit/cmonit.conf
sudo vim /etc/cmonit/cmonit.conf

# Configure in /etc/rc.conf (using config file)
sudo sysrc cmonit_enable="YES"
sudo sysrc cmonit_config="/etc/cmonit/cmonit.conf"

# OR configure with individual flags (without config file)
sudo sysrc cmonit_enable="YES"
sudo sysrc cmonit_flags="-listen 0.0.0.0:3000 -collector 8080 -syslog daemon"

# Start the service
sudo service cmonit start

# Check status
sudo service cmonit status
```

**Configuration Methods:**

1. **Config File (Recommended):**
   ```bash
   # /etc/rc.conf
   cmonit_enable="YES"
   cmonit_config="/etc/cmonit/cmonit.conf"
   cmonit_flags="-debug"  # Optional: add extra flags
   ```

2. **CLI Flags Only:**
   ```bash
   # /etc/rc.conf
   cmonit_enable="YES"
   cmonit_flags="-listen 0.0.0.0:3000 -collector 8080 -syslog daemon"
   ```

See `rc.d/cmonit` for all configuration options.

## Project Structure

```
cmonit/
├── cmd/cmonit/main.go          # Entry point
├── internal/
│   ├── config/
│   │   └── config.go           # Configuration file support (TOML)
│   ├── db/
│   │   ├── schema.go           # Database setup
│   │   └── storage.go          # Data storage
│   ├── parser/
│   │   ├── xml.go              # Monit XML parser
│   │   └── xml_test.go         # Parser tests
│   └── web/
│       ├── handler.go          # Dashboard handlers
│       └── api.go              # Metrics API
├── templates/
│   └── dashboard.html          # Web UI template
├── rc.d/
│   └── cmonit                  # FreeBSD rc.d script
├── cmonit.conf.sample         # Example configuration file
├── docs/                       # Documentation
├── go.mod                      # Go dependencies
└── cmonit.db                   # SQLite database (created at runtime)
                                # with .db-wal and .db-shm
```

## Tech Stack

- **Backend**: Go 1.x
- **Database**: SQLite with WAL mode
- **Frontend**: HTML, Tailwind CSS, Chart.js
- **Protocol**: HTTP Basic Auth, XML

## License

Co-Authored-By: 🤖 [Claude Code](https://claude.com/claude-code)

See [LICENSE](LICENSE) file for details.
