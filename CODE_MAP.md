# CODE_MAP.md

Central reference for the cmonit codebase — a centralized dashboard for [Monit](https://mmonit.com/monit/) agents.

---

## Directory Layout

```
cmd/cmonit/main.go          Entry point, two HTTP servers, daemon mode, signal handling
internal/
  config/config.go          TOML config loader with CLI override priority
  db/
    schema.go               SQLite schema definition + incremental migrations (v1→v12)
    storage.go              All persistence logic (insert/update/query helpers)
  parser/
    xml.go                  Monit XML → Go structs, gzip + charset handling
    xml_test.go             Parser unit tests
  control/
    actions.go              Remote Monit actions (start/stop/restart/monitor)
  web/
    handler.go              Dashboard page handlers (status, host detail, service detail)
    handlers_status.go      Status color computation, service aggregation
    api.go                  REST JSON endpoints (metrics, actions, availability, groups)
    mmonit_api.go           M/Monit-compatible HTTP API (legacy paths + /api/2/ routes)
    health.go               Internal health helper functions (no HTTP endpoint)
    templates/              Embedded Go HTML templates (dashboard, status, service, events)
    static/                 Embedded static assets (favicon, logo)
tests/
  api_test.go               API regression tests; requires -url flag (no default)
rc.d/cmonit                 FreeBSD rc.d startup script
cmonit.conf.sample          TOML configuration reference
```

---

## Two HTTP Servers

| Server     | Default port | Purpose                                    |
|------------|--------------|--------------------------------------------|
| Collector  | 8080         | Receives XML POSTs from Monit agents        |
| Web UI     | 3000         | Serves dashboard and JSON API to browsers   |

Both support TLS and HTTP Basic Auth independently. Credentials are configured separately.

---

## Data Ingest Flow

```
Monit agent  →  POST /collector (XML or gzip-XML)
  handleCollector()          main.go
    parser.ParseMonitXML()   internal/parser/xml.go
    db.StoreMonitStatus()    internal/db/storage.go
      StoreHost()
      StoreService()
      Store*Metrics()        (system, process, filesystem, network, file, program, remote_host)
      StoreEvent()
      StoreHostGroups()
      CleanStaleServices()
```

Background goroutine every 60 s: `RecordAvailabilityForAllHosts()`.

---

## Database Tables (schema v12)

| Table                 | Purpose                                           |
|-----------------------|---------------------------------------------------|
| schema_version        | Migration tracking                                |
| hosts                 | One row per Monit agent (hostname UNIQUE)         |
| services              | One row per (host, service) pair                  |
| metrics               | Time-series generic metrics (load, CPU, mem, …)   |
| events                | State-change history                              |
| filesystem_metrics    | Disk space, inode, I/O per mount                  |
| network_metrics       | Link state, speed, traffic per interface          |
| file_metrics          | Permissions, size, checksum per watched file      |
| program_metrics       | Exit status + output per program check            |
| remote_host_metrics   | Response times for ICMP / TCP / UDP / Unix checks |
| host_availability     | Periodic green/yellow/red snapshots               |
| hostgroups            | Named groups                                      |
| host_hostgroups       | Many-to-many hosts ↔ groups                       |

Migrations are additive SQL blocks in `schema.go:MigrateSchema()`. Bump `currentSchemaVersion` and append a new `case`.

---

## Service Types (from Monit XML)

| int | Meaning          |
|-----|------------------|
| 0   | Filesystem       |
| 1   | Directory        |
| 2   | File             |
| 3   | Process          |
| 4   | Remote host      |
| 5   | System           |
| 6   | FIFO             |
| 7   | Program          |
| 8   | Network iface    |

---

## REST API Endpoints

| Method | Path                              | Handler                      |
|--------|-----------------------------------|------------------------------|
| GET    | /api/metrics                      | HandleMetricsAPI             |
| POST   | /api/action                       | HandleActionAPI              |
| GET    | /api/remote-metrics               | HandleRemoteHostMetricsAPI   |
| GET    | /api/availability                 | HandleAvailabilityAPI        |
| POST   | /api/host/description             | HandleUpdateDescription      |
| GET    | /api/hostgroups                   | HandleHostGroupsAPI          |

`health.go` contains only internal helper functions (`CalculateHostHealth`, `FormatTimeSince`, etc.) — no HTTP endpoint.

### M/Monit-compatible API (`mmonit_api.go`)

Legacy paths (no prefix) and spec-compliant `/api/2/` paths are both registered:

| Legacy path              | `/api/2/` path                        | Notes                              |
|--------------------------|---------------------------------------|------------------------------------|
| GET /status/hosts        | GET\|POST /api/2/status/hosts/list    | All hosts summary                  |
| GET /status/hosts/{id}   | GET\|POST /api/2/status/hosts/get?id= | Host detail                        |
| —                        | GET\|POST /api/2/status/hosts/summary | Count by health status             |
| GET /events/list         | GET\|POST /api/2/reports/events/list  | Event list                         |
| GET /events/get/{id}     | GET\|POST /api/2/reports/events/get?id= | Single event                     |
| GET /admin/hosts         | GET\|POST /api/2/admin/hosts/list     | Admin host list                    |
| DELETE /admin/hosts/{id} | GET\|POST /api/2/admin/hosts/delete?id= | Delete host (>1h offline required) |

---

## Key Structs

```
parser.MonitStatus          Root of parsed XML
parser.Server               Monit daemon metadata (version, incarnation, uptime, poll)
parser.Platform             OS info (name, CPU count, memory, swap)
parser.Service              Per-service data; type determines which sub-struct is populated
config.Config               All runtime settings; loaded from TOML then overlaid by CLI flags
```

---

## Configuration Priority

1. CLI flags (highest)
2. Values from `cmonit.conf` (TOML)
3. Built-in defaults

Relevant fields: listen addresses, collector/web auth credentials, TLS cert/key paths, database path, PID file, syslog facility, daemon mode, debug logging.

---

## External Dependencies

```
github.com/BurntSushi/toml      TOML config parsing
github.com/gomarkdown/markdown  Markdown → HTML for host descriptions
modernc.org/sqlite              SQLite driver (pure Go, no CGO — driver name: "sqlite")
golang.org/x/crypto/bcrypt      Bcrypt password verification
```

---

## Notable Behaviour / Gotchas

- **Monit restart detection**: `StoreHost()` compares `monit_uptime` with elapsed time; a drop triggers a restart event and clears stale services.
- **Stale service cleanup**: Services not seen in the current XML batch are deleted from the `services` table after each ingest.
- **Availability recording**: Happens both on each XML ingest (via `StoreHost`) and every 60 s for hosts that have gone silent.
- **Charset fix**: `parser/xml.go` rewrites `ISO-8859-1` XML declarations to `UTF-8` before parsing.
- **Templates and static assets** are embedded in the binary via `go:embed`; no runtime file dependencies.
- **SQLite WAL mode** is enabled at startup for read/write concurrency between the two servers.
- **Host deletion** is guarded: a host must have been offline for more than 1 hour before `DeleteHost()` proceeds.
- **Description field** accepts raw HTML (stored as-is, rendered in dashboard).
