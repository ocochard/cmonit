# cmonit - Central Monit Monitor

An open-source M/Monit clone that collects and visualizes monitoring data from Monit agents.

![cmonit Dashboard](../screenshot.png)

## Features

- HTTP collector compatible with Monit agents
- SQLite database for metrics storage
- Web dashboard with real-time status
- Time-series graphs (CPU, memory, load)
- Multiple time ranges (1h, 6h, 24h, 7d, 30d)
- Configurable listen addresses (IPv4/IPv6 support)

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
```

### Access

Open your browser to the configured web address:
- Default: **http://localhost:3000/**
- All interfaces: **http://your-server-ip:3000/**

## Configure Monit Agents

Add to your monitrc file:

```
set mmonit http://monit:monit@your-server:8080/collector
```

Replace `your-server` with the hostname or IP where cmonit is running.

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

## Security Notes

- **Default**: Web UI listens on `localhost:3000` (local connections only)
- **Production**: Use a reverse proxy (nginx/apache) with HTTPS
- **Firewall**: Restrict collector port (8080) to trusted Monit agents
- **Authentication**: Collector uses HTTP Basic Auth (monit:monit by default)

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
