# cmonit - Central Monit Monitoring System
## Project Plan

## Overview

**cmonit** is an open-source clone of the proprietary M/Monit software that provides centralized monitoring and management of all Monit-enabled hosts via a modern, clean, and mobile-friendly web interface.

## Technology Stack

### Backend
- **Language**: Go (Golang) - KISS principle, excellent for HTTP services and concurrency
- **Database**: SQLite - Simple, embedded, serverless, perfect for this use case
- **HTTP Framework**: Standard library `net/http` with `gorilla/mux` for routing (optional)

### Frontend
- **CSS Framework**: Tailwind CSS (loaded via CDN)
- **Charting**: Chart.js (loaded via CDN)
- **Templates**: Go `html/template` for server-side rendering

### Why This Stack?

**Go Advantages**:
- Single binary deployment (no dependencies)
- Excellent HTTP and concurrent request handling
- Built-in templating
- Strong standard library
- Cross-platform compilation
- Low memory footprint

**SQLite Advantages**:
- No separate database server needed
- Zero configuration
- ACID compliant
- Perfect for embedded systems
- File-based (easy backup/migration)

**Tailwind + Chart.js**:
- No build step required (CDN-based)
- Modern, responsive design out of the box
- Chart.js is simple and lightweight
- Mobile-friendly by default

## Project Structure

```
cmonit/
├── cmd/
│   └── cmonit/
│       └── main.go          # Entry point
├── internal/
│   ├── api/
│   │   ├── collector.go     # /collector endpoint (receives from Monit)
│   │   ├── api.go           # M/Monit-compatible HTTP API
│   │   └── handlers.go      # HTTP handlers
│   ├── db/
│   │   ├── schema.go        # Database schema
│   │   ├── models.go        # Data models
│   │   ├── queries.go       # Database queries
│   │   └── migrations.go    # Schema initialization
│   ├── parser/
│   │   └── xml.go           # Monit XML parser
│   ├── web/
│   │   ├── server.go        # Web server setup
│   │   └── handlers.go      # Web UI handlers
│   └── config/
│       └── config.go        # Configuration management
├── web/
│   ├── templates/
│   │   ├── base.html        # Base template
│   │   ├── dashboard.html   # Dashboard view
│   │   ├── host.html        # Single host view
│   │   └── graphs.html      # Time-series graphs
│   └── static/
│       └── (optional custom CSS/JS)
├── docs/
│   ├── monit-collector-protocol.md
│   ├── project-plan.md
│   ├── api-compatibility.md
│   └── testing-plan.md
├── LICENSE
├── README.md
├── go.mod
├── go.sum
├── Makefile
└── .gitignore
```

## Development Phases

### Phase 1: Collector Daemon (Foundation)

**Goal**: Create HTTP server that receives and stores Monit status updates

**Components**:
1. Basic Go HTTP server listening on port 8080
2. `/collector` endpoint accepting POST requests
3. HTTP Basic Auth validation (username: monit, password: monit)
4. XML parser for Monit status format
5. SQLite database with initial schema
6. Data insertion logic

**Database Schema (Current - Version 4)**:

The database schema has evolved through automatic migrations:

- **Schema v1**: Initial schema with hosts, services, metrics, events tables
- **Schema v2**: Added filesystem_metrics table for detailed filesystem monitoring
- **Schema v3**: Added network_metrics table for network interface monitoring
- **Schema v4** (Current): Added file_metrics and program_metrics tables

See `internal/db/schema.go` for the complete current schema including:

**Core Tables**:
- `hosts` - Monitored host information with platform details and Monit daemon info
- `services` - Services monitored on each host (all types)
- `events` - Service state change events and Monit restart detection

**Metrics Tables**:
- `metrics` - Time-series system/process CPU, memory, load metrics
- `filesystem_metrics` - Block/inode usage, filesystem type, I/O operations
- `network_metrics` - Link state, speed, duplex, traffic statistics
- `file_metrics` - File mode, ownership, timestamps, checksums (Type 2 services)
- `program_metrics` - Program execution status and output (Type 7 services)

**Schema Management**:
- `schema_version` - Tracks current schema version with automatic migrations
- All migrations are applied automatically on startup

**Acceptance Tests**:
- [ ] Server starts and listens on port 8080
- [ ] `/collector` endpoint responds to POST requests
- [ ] Rejects requests without valid Basic Auth
- [ ] Accepts requests with monit:monit credentials
- [ ] Parses XML from existing Monit agent
- [ ] Creates database file if it doesn't exist
- [ ] Inserts host record on first contact
- [ ] Updates host last_seen on each contact
- [ ] Inserts/updates service records
- [ ] Stores metrics in time-series table
- [ ] Returns HTTP 200 with `Server: cmonit/0.1` header
- [ ] Handles gzip-compressed requests (if sent by Monit)

**Deliverable**: Working daemon that collects data from the running Monit agent

---

### Phase 2: Web UI - Dashboard

**Goal**: Display current status of all monitored hosts and services

**Components**:
1. Web server on port 3000 (separate from collector)
2. Dashboard page showing all hosts
3. Service status table per host
4. Basic styling with Tailwind CSS
5. Auto-refresh every 30 seconds

**Dashboard Features**:
- List of all monitored hosts
- For each host:
  - Hostname
  - Last seen timestamp
  - Overall health status
  - Number of services
- For each service:
  - Service name
  - Service type
  - Current status (OK, Failed, etc.)
  - Monitor status
  - Key metrics (CPU%, Memory%, etc.)

**Acceptance Tests**:
- [ ] Web server starts on port 3000
- [ ] Dashboard accessible at http://localhost:3000/
- [ ] Shows all monitored hosts
- [ ] Shows all services for each host
- [ ] Displays current status correctly
- [ ] Shows correct timestamps
- [ ] Auto-refreshes every 30 seconds
- [ ] Responsive design works on mobile
- [ ] Color-coded status indicators (green=OK, red=failed, yellow=warning)
- [ ] No JavaScript errors in console

**Deliverable**: Functional web dashboard showing real-time status

---

### Phase 3: Web UI - Time-Series Graphs

**Goal**: Visualize metrics over time using Chart.js

**Components**:
1. Graphs page for each host/service
2. Chart.js integration
3. Query optimization for time-series data
4. Time range selector (1h, 6h, 24h, 7d, 30d)

**Graph Types**:
- **System metrics**:
  - CPU usage over time (user, system, nice, wait)
  - Memory usage over time
  - Swap usage over time
  - Load average over time
  - Disk space usage over time (per filesystem)
  - Network traffic over time (per interface)

- **Process metrics**:
  - CPU% over time
  - Memory usage over time
  - Thread count over time

**Acceptance Tests**:
- [ ] Graphs page accessible for each host
- [ ] CPU usage graph displays correctly
- [ ] Memory usage graph displays correctly
- [ ] Disk space graph displays correctly
- [ ] Load average graph displays correctly
- [ ] Time range selector works (1h, 6h, 24h, 7d, 30d)
- [ ] Graphs update when time range changes
- [ ] Hover tooltips show exact values
- [ ] Legend works correctly
- [ ] Graphs are responsive on mobile
- [ ] Data points are interpolated correctly
- [ ] No performance issues with large datasets

**Deliverable**: Interactive time-series graphs for all metrics

---

### Phase 4: M/Monit API Compatibility

**Goal**: Implement M/Monit HTTP API for compatibility with existing tools

**API Endpoints** (from https://mmonit.com/documentation/http-api/):

Authentication & Session:
- `POST /login` - User login
- `GET /logout` - User logout

Status & Monitoring:
- `GET /status/hosts/:id` - Get host status
- `GET /status/hosts/:id/services` - Get all services for a host
- `GET /status/hosts/:id/services/:name` - Get specific service status
- `GET /uptime/hosts/:id` - Get uptime data
- `GET /reports/uptime/hosts/:id` - Get uptime reports

Events:
- `GET /events/list` - List events
- `GET /events/get/:id` - Get specific event
- `DELETE /events/delete/:id` - Delete event

Administrative:
- `GET /admin/hosts` - List all hosts
- `POST /admin/hosts` - Add new host
- `DELETE /admin/hosts/:id` - Remove host
- `PUT /admin/hosts/:id` - Update host
- `GET /admin/groups` - List groups
- `POST /admin/groups` - Create group

**Acceptance Tests**:
- [ ] All API endpoints respond correctly
- [ ] JSON responses match M/Monit format
- [ ] Authentication works
- [ ] Error responses have correct format
- [ ] API documentation is complete
- [ ] Integration tests pass

**Deliverable**: M/Monit-compatible REST API

---

### Phase 5: Advanced Features

**Additional features to consider**:

1. **Alerting**:
   - Email notifications
   - Webhook support
   - Alert rules

2. **Multi-host support**:
   - Host groups
   - Filtering and search

3. **User management**:
   - Multiple users
   - Role-based access control
   - API tokens

4. **Data retention**:
   - Automatic cleanup of old metrics
   - Configurable retention policies
   - Data aggregation for long-term storage

5. **Export**:
   - CSV export of metrics
   - PDF reports

---

## Testing Strategy

### Unit Tests
- XML parser tests with sample Monit data
- Database query tests
- HTTP handler tests

### Integration Tests
- End-to-end flow from Monit → collector → database → UI
- API compatibility tests

### Manual Tests
- Real Monit agent sending data
- UI testing on different browsers
- Mobile responsiveness testing

### Performance Tests
- Multiple Monit agents (10, 50, 100 hosts)
- Long-running stability test (24h+)
- Database growth over time

---

## Deployment

### Binary Distribution
```bash
# Build for current platform
make build

# Build for all platforms
make build-all

# Output:
# bin/cmonit-linux-amd64
# bin/cmonit-freebsd-amd64
# bin/cmonit-darwin-amd64
```

### Running
```bash
# Start with default settings
./cmonit

# Custom config
./cmonit --config /path/to/config.yaml

# Specify database location
./cmonit --db /path/to/cmonit.db

# Change ports
./cmonit --collector-port 8080 --web-port 3000
```

### Configuration File (cmonit.yaml)
```yaml
collector:
  port: 8080
  auth:
    username: monit
    password: monit
  compression: true

web:
  port: 3000
  refresh_interval: 30

database:
  path: ./cmonit.db
  retention_days: 30

logging:
  level: info
  format: json
```

---

## Development Workflow

### Day 1-2: Phase 1 - Collector Setup
1. Initialize Go module
2. Set up project structure
3. Create database schema
4. Implement XML parser
5. Build collector endpoint
6. Test with real Monit agent

### Day 3-4: Phase 2 - Dashboard
1. Set up web server
2. Create HTML templates
3. Implement dashboard handlers
4. Add Tailwind CSS styling
5. Test UI

### Day 5-6: Phase 3 - Graphs
1. Implement graph data queries
2. Create graph templates
3. Integrate Chart.js
4. Add time range selectors
5. Test graphs

### Day 7-8: Phase 4 - API
1. Design API routes
2. Implement API handlers
3. Add authentication
4. Write API tests
5. Document API

### Day 9-10: Polish & Documentation
1. Add README
2. Write user documentation
3. Create deployment guide
4. Performance testing
5. Bug fixes

---

## Success Criteria

The project will be considered successful when:

1. ✅ cmonit receives and stores data from at least one Monit agent
2. ✅ Web dashboard displays real-time status of all monitored services
3. ✅ Time-series graphs show historical metrics
4. ✅ M/Monit API endpoints return correct data
5. ✅ All acceptance tests pass
6. ✅ System runs stably for 24+ hours
7. ✅ Documentation is complete
8. ✅ Single binary deployment works on FreeBSD, Linux, and macOS

---

## Alternative Considerations

### Why NOT use other languages?

**Python**:
- ❌ Requires Python runtime
- ❌ Dependency management complexity
- ❌ Slower than Go for HTTP services
- ✅ Easier for some developers

**Node.js**:
- ❌ Requires Node.js runtime
- ❌ Callback hell / async complexity
- ❌ npm dependency bloat
- ✅ Good ecosystem for web

**Rust**:
- ❌ Steeper learning curve
- ❌ Longer compilation times
- ❌ More verbose
- ✅ Ultimate performance
- ✅ Memory safety

**Conclusion**: Go provides the best balance of simplicity, performance, and deployment convenience for this project.

### Why NOT use PostgreSQL/MySQL?

- ❌ Requires separate database server
- ❌ Additional configuration complexity
- ❌ Overkill for single-server deployment
- ✅ Better for very large deployments (1000+ hosts)

SQLite is perfect for small-to-medium deployments (<100 hosts). If scaling becomes an issue, migration to PostgreSQL can be done later.

---

## Testing with Multiple Monit Clients

### Option 1: Docker Containers
Create multiple Docker containers running Monit, each configured to send to cmonit:

```bash
# Create 5 test monit instances
for i in {1..5}; do
    docker run -d --name monit$i \
        -e MMONIT_URL=http://monit:monit@host.docker.internal:8080/collector \
        custom-monit-image
done
```

### Option 2: Multiple VMs/Jails
- Use FreeBSD jails or VMs
- Install Monit in each
- Configure each to point to cmonit

### Option 3: Mock Data Generator
Create a Go tool that simulates multiple Monit agents sending data:

```bash
# Simulate 10 hosts
./mock-monit --hosts 10 --interval 30 --url http://localhost:8080/collector
```

**Recommendation**: Start with the single existing Monit agent for Phase 1-2. Add multiple test clients in Phase 3 for load testing.

---

## Risk Mitigation

| Risk | Impact | Mitigation |
|------|--------|------------|
| XML parsing complexity | High | Use encoding/xml package, extensive testing |
| Database performance | Medium | Proper indexing, connection pooling |
| UI responsiveness | Low | Use Tailwind, test on mobile |
| API compatibility | High | Reference M/Monit docs, integration tests |
| Memory leaks | Medium | Profiling, load testing |
| Concurrent writes | Medium | Use SQLite WAL mode, proper locking |

---

## Phase 5: Host Lifecycle Management

**Goal**: Intelligent stale host detection and host deletion functionality for dynamic infrastructure

### Features

**1. Heartbeat-Based Health Status**
- Store poll interval from Monit (typically 30s)
- Three-state health indicator:
  - 🟢 Green (Healthy): `last_seen < poll_interval * 2`
  - 🟡 Yellow (Warning): `poll_interval * 2 <= last_seen < poll_interval * 5`
  - 🔴 Red (Offline): `last_seen >= poll_interval * 5`
- Visual status on dashboard with "last seen" timestamp
- No false alarms during maintenance windows

**2. Host Deletion with Safety**
- Delete button for offline hosts (>1 hour)
- Confirmation dialog requiring hostname entry
- Shows deletion impact (services, metrics, events count)
- Cascade deletion across all related tables
- Safety: Cannot delete recently active hosts

**3. Future Enhancements**
- Soft delete/archive functionality (Phase 2)
- Host lifecycle tags: production, ephemeral, testing, staging (Phase 2)
- Auto-archive policies for ephemeral hosts (Phase 3)
- Batch operations for multiple hosts (Phase 3)
- Data export before deletion (Phase 3)

### Database Schema Changes (Schema v5)

```sql
ALTER TABLE hosts ADD COLUMN poll_interval INTEGER DEFAULT 30;
```

**Future schema additions** (Phase 2+):
```sql
ALTER TABLE hosts ADD COLUMN archived INTEGER DEFAULT 0;
ALTER TABLE hosts ADD COLUMN archived_at DATETIME;
ALTER TABLE hosts ADD COLUMN lifecycle TEXT DEFAULT 'production';
```

### Implementation Phases

**Phase 1 (Essential) - Status Tracking:**
- [x] Store poll_interval in database
- [x] Add schema v7 migration (includes poll_interval field)
- [x] Implement health status calculation helper
- [x] Add health indicator to dashboard (🟢🟡🔴)
- [x] Show "last seen" with human-readable time
- [x] Add DELETE /admin/hosts/:id API endpoint
- [x] Add cascade deletion function
- [x] Add delete confirmation UI with hostname verification

**Known Issues Fixed:**
- ~~Bug: DELETE endpoint returned HTTP 500 due to incorrect DATETIME-to-integer conversion~~ Fixed in `/internal/db/storage.go:1170` using `CAST(strftime('%s', last_seen) AS INTEGER)`

**Phase 2 (Enhanced) - Archive System:**
- [ ] Add archived flag and timestamps
- [ ] Implement archive/restore functionality
- [ ] Show deletion impact statistics
- [ ] Add "Archived Hosts" page
- [ ] Host lifecycle tags

**Phase 3 (Advanced) - Automation:**
- [ ] Auto-archive policies based on lifecycle
- [ ] Batch operations (archive/delete multiple)
- [ ] Data export before deletion
- [ ] Email notifications before auto-archive

### Acceptance Tests

Phase 1:
- [x] Poll interval correctly stored from XML
- [x] Health status calculates correctly (green/yellow/red)
- [x] Dashboard shows visual health indicators
- [x] Cannot delete host active within last hour
- [x] Delete requires correct hostname confirmation
- [x] Deletion cascades to all related tables
- [x] Metrics/events/services deleted with host
- [x] Database integrity maintained after deletion

---

## Future Enhancements

- Prometheus exporter
- Grafana integration
- Clustering support (multiple cmonit instances)
- InfluxDB backend option
- Docker/Kubernetes native monitoring
- Mobile app
- Slack/Discord/Telegram notifications
- Incident management workflow
