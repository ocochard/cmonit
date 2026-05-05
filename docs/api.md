# cmonit API Reference

## Overview

cmonit exposes two API families on the web port (default 3000):

| Family | Base path | Purpose |
|--------|-----------|---------|
| Native | `/api/` | cmonit-specific endpoints (metrics graphs, actions, groups) |
| M/Monit v2 | `/api/2/` | Spec-compliant M/Monit HTTP API |
| M/Monit legacy | `/status/`, `/events/`, `/admin/` | Older paths, kept for backward compatibility |

**Authentication**: when `-web-user` / `-web-password` are configured, all endpoints require HTTP Basic Auth.

**Content-Type**: all endpoints return `application/json`.

Reference spec: https://mmonit.com/documentation/http-api/static/index.html

---

## Native API

### GET /api/hostgroups

Returns all host groups with their member hostnames.

```bash
curl http://localhost:3000/api/hostgroups
```

```json
{
  "groups": [
    {
      "id": 1,
      "name": "builder",
      "host_count": 2,
      "hostnames": ["host-a", "host-b"]
    }
  ]
}
```

---

### GET /api/metrics

Time-series metrics for a service, used by the dashboard graphs.

**Query parameters**:
- `host_id` (required) — host identifier
- `service` (required) — service name
- `range` — `1h`, `6h`, `24h`, `7d`, `30d` (default `24h`)

```bash
curl "http://localhost:3000/api/metrics?host_id=myhost-0&service=system&range=6h"
```

---

### GET /api/remote-metrics

Response time series for remote host services (ICMP, TCP, Unix socket).

**Query parameters**: `host_id`, `service`, `range` (same as `/api/metrics`)

---

### GET /api/availability

Host availability history (green/yellow/red status over time).

**Query parameters**: `host_id`, `range`

---

### POST /api/action

Execute a Monit action on a service.

```bash
curl -X POST http://localhost:3000/api/action \
  -H "Content-Type: application/json" \
  -d '{"host_id":"myhost-0","service":"nginx","action":"restart"}'
```

Actions: `start`, `stop`, `restart`, `monitor`, `unmonitor`

---

### POST /api/host/description

Update the HTML description for a host (displayed on the host detail page).

```bash
curl -X POST http://localhost:3000/api/host/description \
  -d "host_id=myhost-0&description=<b>Primary build server</b>"
```

---

## M/Monit v2 API (`/api/2/`)

All endpoints accept both `GET` and `POST`. Parameters are passed as query string or form values.

### GET|POST /api/2/status/hosts/list

Returns all monitored hosts.

**Optional parameters**: `hostid`, `hostpattern`, `hostgroupid`, `led`, `sort`, `dir`

```bash
curl http://localhost:3000/api/2/status/hosts/list
```

```json
[
  {
    "id": "myhost-0",
    "hostname": "myhost",
    "status": 0,
    "services": 8,
    "platform": "FreeBSD 16.0-CURRENT (amd64)",
    "lastseen": "2026-05-05T12:00:55Z",
    "monituptime": 3600,
    "cpupercent": 5,
    "mempercent": 42
  }
]
```

**Status values**: 0=OK, 1=Warning, 2=Critical

---

### GET|POST /api/2/status/hosts/get

Returns detailed information for a specific host including all services.

**Required**: `id`

```bash
curl "http://localhost:3000/api/2/status/hosts/get?id=myhost-0"
```

```json
{
  "id": "myhost-0",
  "hostname": "myhost",
  "status": 0,
  "platform": "FreeBSD 16.0-CURRENT (amd64)",
  "platformversion": "16.0-CURRENT",
  "cpucount": 8,
  "memory": 16777216,
  "uptime": 86400,
  "monituptime": 3600,
  "monitversion": "5.35.2",
  "lastseen": "2026-05-05T12:00:55Z",
  "services": [
    {"name": "system", "type": 5, "status": 0, "monitor": 1},
    {"name": "sshd",   "type": 3, "status": 0, "monitor": 1}
  ]
}
```

**Errors**: `400` if `id` missing, `404` if host not found

---

### GET|POST /api/2/status/hosts/summary

Returns host counts by health status.

```bash
curl http://localhost:3000/api/2/status/hosts/summary
```

```json
{
  "summary": [
    {"label": "green",  "data": 3},
    {"label": "orange", "data": 1},
    {"label": "red",    "data": 2}
  ]
}
```

Health thresholds (based on `poll_interval`):
- green: `last_seen < poll_interval × 2`
- orange: `poll_interval × 2 ≤ last_seen < poll_interval × 5`
- red: `last_seen ≥ poll_interval × 5`

---

### GET|POST /api/2/reports/events/list

Returns events with optional filtering and pagination.

**Optional parameters**: `hostid`, `limit` (default 100), `offset` (default 0)

```bash
curl "http://localhost:3000/api/2/reports/events/list?hostid=myhost-0"
```

```json
{
  "records": 24,
  "events": [
    {
      "id": 24,
      "hostid": "myhost-0",
      "hostname": "myhost",
      "service": "myhost",
      "type": 262144,
      "message": "Monit daemon restarted (uptime reset from 2672896 to 0 seconds)",
      "timestamp": "2026-05-05T11:27:16Z"
    }
  ]
}
```

---

### GET|POST /api/2/reports/events/get

Returns a single event by ID.

**Required**: `id`

```bash
curl "http://localhost:3000/api/2/reports/events/get?id=24"
```

**Errors**: `400` if `id` missing, `404` if event not found

---

### GET|POST /api/2/admin/hosts/list

Returns the administrative host list (same data as `/api/2/status/hosts/list`, wrapped with a `records` count).

```bash
curl http://localhost:3000/api/2/admin/hosts/list
```

```json
{
  "records": 4,
  "hosts": [ ... ]
}
```

---

### GET|POST /api/2/admin/hosts/delete

Deletes a host and all associated data (services, metrics, events).

**Required**: `id`

Safety check: the host must have been offline for more than 1 hour. Returns `403` if the host is still active.

```bash
curl "http://localhost:3000/api/2/admin/hosts/delete?id=myhost-0"
```

```json
{"deleted": 1542}
```

**Errors**: `400` if `id` missing, `404` if host not found, `403` if host too recently active

---

## M/Monit legacy paths

These paths are kept for backward compatibility. They behave identically to their `/api/2/` equivalents but use different URL conventions (path parameters instead of query parameters, GET-only).

| Legacy | Equivalent |
|--------|-----------|
| GET /status/hosts | /api/2/status/hosts/list |
| GET /status/hosts/{id} | /api/2/status/hosts/get?id={id} |
| GET /status/hosts/{id}/services | (subset of /api/2/status/hosts/get) |
| GET /events/list | /api/2/reports/events/list |
| GET /events/get/{id} | /api/2/reports/events/get?id={id} |
| GET /admin/hosts | /api/2/admin/hosts/list |
| DELETE /admin/hosts/{id} | /api/2/admin/hosts/delete?id={id} |

---

## Error responses

```json
{"error": "Not Found", "message": "Host not found"}
```

| Code | Meaning |
|------|---------|
| 400 | Missing required parameter |
| 401 | Authentication required |
| 403 | Operation refused (e.g. host still active) |
| 404 | Resource not found |
| 405 | Method not allowed |
| 500 | Internal server error |

---

## Not implemented

The following M/Monit API sections are not implemented (cmonit uses a push model and has no user management):

- `/api/2/reports/uptime/*` — uptime reports
- `/api/2/reports/events/summary` — event summary chart
- `/api/2/reports/events/dismiss` — dismiss events
- `/api/2/admin/hosts/update`, `/test`, `/action` — host management
- `/api/2/admin/groups/*` — group management
- `/api/2/admin/users/*` — user management
- `/api/2/admin/roles/*` — role management
- `/api/2/session/*` — session management
- `/api/2/map/*` — id/name mapping trees
