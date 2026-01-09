# cmonit - Central Monit Monitoring System
## Project Plan

## Overview

**cmonit** is an open-source clone of the proprietary M/Monit software that provides centralized monitoring and management of all Monit-enabled hosts via a web interface.

For detailed architecture, technology stack, and implementation details, see **[docs/README.md](README.md)** - the main developer reference.

This document focuses on development phases, planning, and strategy.

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

**Database Schema**: See `internal/db/schema.go` and [docs/README.md](README.md) for complete schema details. Schema is automatically migrated on startup.

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

SQLite works for small-to-medium deployments (<100 hosts). If scaling becomes an issue, migration to PostgreSQL can be done later.

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

## Completed Enhancements (Post Phase 4)

The following features have been successfully implemented beyond the original 4-phase plan:

### Host Lifecycle Management (✅ Completed)

**Goal**: Intelligent stale host detection and deletion for dynamic infrastructure

**Implemented Features**:
- Heartbeat-based health status (green/yellow/red based on poll_interval)
- Host deletion API with safety controls (>1 hour offline required)
- Cascade deletion across all related tables
- Dashboard visual health indicators

See [docs/README.md](README.md) for complete implementation details.

---

### Enhanced System Metrics Display (✅ Completed)

**Goal**: System metrics display in service detail pages

**Implemented Features**:
- Load average (1min, 5min, 15min) with visual indicators
- CPU usage breakdown (platform-aware: FreeBSD vs Linux)
- Memory/Swap usage with color-coded progress bars
- Formatted size display (KB/MB/GB)
- Mobile-responsive design

See [docs/README.md](README.md) for complete implementation details and `templates/service.html` for UI implementation.

---

## Changelog

All notable changes to this project are documented here. The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/), and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

### [Unreleased]

#### Fixed - v1.2.1 (2026-01-09)
- **Availability Data Preservation**: Fixed CASCADE DELETE issue causing availability graph data loss
  - Changed `INSERT OR REPLACE` to `INSERT ... ON CONFLICT DO UPDATE` in StoreHost() and StoreService()
  - Previous implementation deleted and recreated host records on every update, triggering CASCADE DELETE
  - With schema v12's CASCADE DELETE, this wiped all availability history every 30 seconds
  - Now updates rows in-place, preserving all foreign key relationships and historical data
  - Files modified: `internal/db/storage.go`

#### Changed - Schema v12 (2026-01-09)
- **Database Schema Improvements**: Enhanced data integrity and referential integrity
  - All foreign keys now specify ON DELETE CASCADE for automatic cleanup
  - Added CHECK constraints for data validation:
    - Service type constrained to 0-8
    - Monitor status constrained to 0-2
    - Percentages constrained to 0-100 range
    - Port numbers constrained to 1-65535 range
    - Positive value constraints on counters and IDs
  - Description field limited to 8192 characters to prevent excessive storage
  - Improved data consistency for new database installations
  - Existing databases migrate to v12 with constraint definitions (apply to new data)
  - Files modified: `internal/db/schema.go`, `internal/db/storage.go`

#### Added
- **Remote Host Monitoring Support**: Full support for Monit Remote Host service monitoring (type 4)
  - Database schema upgraded to version 8 with new `remote_host_metrics` table
  - Captures ICMP (ping) response times with ping type information
  - Captures TCP/UDP port monitoring metrics including target hostname, port number, protocol, and response times
  - Captures Unix socket monitoring metrics including socket path, protocol, and response times
  - Also supports port and unix socket monitoring for Process services (type 3)
  - Service detail template displays remote host metrics with color-coded response time indicators
    - Green: < 100ms (ICMP/Port) or < 50ms (Unix socket)
    - Yellow: 100-500ms (ICMP/Port) or 50-200ms (Unix socket)
    - Red: > 500ms (ICMP/Port) or > 200ms (Unix socket)
  - Files modified:
    - `internal/db/schema.go`: Added remote_host_metrics table and migration to v8
    - `internal/parser/xml.go`: Added ICMPInfo, PortInfo, UnixSocketInfo structs
    - `internal/db/storage.go`: Added StoreRemoteHostMetrics function
    - `templates/service.html`: Added Remote Host Metrics display section (lines 375-455)

- **System Metrics Display**: Service detail page now displays full system metrics for System type services (type=5)
  - Load Average: 1-minute, 5-minute, and 15-minute load averages displayed in responsive grid layout
  - CPU Usage Breakdown: Platform-aware display of CPU metrics with color-coded progress bars
    - FreeBSD: User, System, Nice, Hard IRQ
    - Linux: User, System, Nice, I/O Wait, Hard IRQ, Soft IRQ, Steal, Guest, Guest Nice
  - Memory Usage: Visual percentage bar with color-coding (green < 80%, yellow 80-90%, red > 90%) and formatted size display (KB/MB/GB)
  - Swap Usage: Visual percentage bar with color-coding and formatted size display
  - File modified: `templates/service.html` (lines 457-656)
  - Conditional rendering gracefully handles platform differences between FreeBSD and Linux Monit agents

#### Enhanced
- **Process Service Metrics**: Fixed CPU and memory metrics for Process type services to include child processes
  - Now displays total CPU/memory usage (process + children) instead of process-only metrics
  - Aligns with Monit's standard behavior of monitoring process families

#### Fixed
- XML parsing for process metrics now correctly captures total CPU and memory (including children)
- Service detail page template now properly displays process family metrics
- **CPU Usage Breakdown Display**: Fixed template conditional logic that prevented CPU metrics from displaying when values were 0.0%
  - Basic CPU metrics (User, System, Nice, I/O Wait) now always display for all hosts, even at 0%
  - Extended Linux-specific metrics (Hard IRQ, Soft IRQ, Steal, Guest, Guest Nice) only display when at least one is non-zero
  - File modified: `templates/service.html` (lines 505-600)
- **Remote Host Metrics XML Parsing**: Fixed XML parser to correctly extract ICMP, Port, and Unix socket monitoring data
  - Added ICMP, Port, Unix fields to ServiceXML proxy struct in `internal/parser/xml.go` (lines 931-934)
  - Updated ToService() function to copy these fields to domain Service struct (lines 954-956)
  - Remote host metrics are now correctly stored in database and displayed on service detail pages
  - Previously, metrics data was present in XML from Monit agents but was not being parsed/stored
- **Remote Host Template Type Mismatch**: Fixed Go template type mismatch error preventing response times from displaying
  - Changed integer literals to float literals in template comparisons (`templates/service.html` lines 392, 421)
  - Template was comparing float64 response time values with integer literals, causing "incompatible types for comparison" error
  - Fixed comparisons: `100` → `100.0`, `500` → `500.0` for both ICMP and Port response time color-coding

### [0.1.0] - Initial Release

#### Added
- Core collector functionality to receive XML status from Monit agents
- SQLite database storage for hosts, services, and metrics
- Web dashboard for status overview
- Service detail pages for individual service monitoring
- Support for multiple service types: Filesystem, Process, File, Network, Program, System

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
