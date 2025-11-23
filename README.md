# cmonit - Central Monit Monitor

An open-source M/Monit clone that collects and visualizes monitoring data from Monit agents.

![cmonit Dashboard](docs/screenshot.png)

## Features

- HTTP collector compatible with Monit agents
- SQLite database for metrics storage
- Web dashboard with real-time status
- Time-series graphs (CPU, memory, load)
- Multiple time ranges (1h, 6h, 24h, 7d, 30d)
- Configurable listen addresses (IPv4/IPv6 support)
- HTTP Basic Authentication for Web UI
- TLS/HTTPS support for encrypted connections

## Quick Start

### Build

```bash
go build -o cmonit ./cmd/cmonit
```

### Run

```bash
# Default: collector on :8080, web on localhost:3000
./cmonit

# Web accessible from all interfaces
./cmonit -web 0.0.0.0:3000

# IPv6 support
./cmonit -web [::]:3000

# Custom ports
./cmonit -collector :9000 -web :4000

# Specific IP address
./cmonit -web 192.168.1.10:3000

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

# With HTTP Basic Authentication
./cmonit -web-user admin -web-password secretpass

# With TLS/HTTPS
./cmonit -web-cert /path/to/cert.pem -web-key /path/to/key.pem

# Production: Authentication + TLS
./cmonit -web 0.0.0.0:3000 -web-user admin -web-password secretpass -web-cert /path/to/cert.pem -web-key /path/to/key.pem
```

### Command-Line Options

```
  -collector string
        Collector listen address (default ":8080")
        Examples: :8080, localhost:8080, 0.0.0.0:8080, [::]:8080

  -web string
        Web UI listen address (default "localhost:3000")
        Examples: localhost:3000, 0.0.0.0:3000, [::]:3000, 192.168.1.10:3000

  -db string
        Database file path (default "/var/run/cmonit/cmonit.db")

  -pidfile string
        PID file path (default "/var/run/cmonit/cmonit.pid")

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

Example:
```bash
# If cmonit runs on 192.168.1.100
set mmonit http://monit:monit@192.168.1.100:8080/collector

# Or if running locally
set mmonit http://monit:monit@localhost:8080/collector
```

Then reload Monit:
```bash
monit reload
```

## Architecture

```
Monit Agent → :8080/collector → SQLite → :3000 Dashboard
```

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
  -web 0.0.0.0:3000 \
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

# Configure in /etc/rc.conf
sudo sysrc cmonit_enable="YES"
sudo sysrc cmonit_collector=":8080"
sudo sysrc cmonit_web="0.0.0.0:3000"
sudo sysrc cmonit_db="/var/run/cmonit/cmonit.db"
sudo sysrc cmonit_pidfile="/var/run/cmonit/cmonit.pid"
sudo sysrc cmonit_syslog="daemon"

# Start the service
sudo service cmonit start

# Check status
sudo service cmonit status
```

See `rc.d/cmonit` for all configuration options.

## Project Structure

```
cmonit/
├── cmd/cmonit/main.go          # Entry point
├── internal/
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
├── docs/                       # Documentation
├── go.mod                      # Go dependencies
└── cmonit.db                   # SQLite database (created at runtime)
```

## Tech Stack

- **Backend**: Go 1.x
- **Database**: SQLite with WAL mode
- **Frontend**: HTML, Tailwind CSS, Chart.js
- **Protocol**: HTTP Basic Auth, XML

## License

See [LICENSE](LICENSE) file for details.
