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

### Alerting: /api/alerts/

The web UI for this API is the page `/alerts`.

JSON in and out. `POST`/`PUT` require `Content-Type: application/json`. Errors: `400` invalid field, `404` unknown id, `409` duplicate name.

| Method | Path | Purpose |
|--------|------|---------|
| GET, POST | `/api/alerts/channels` | List / create channels |
| PUT, DELETE | `/api/alerts/channels/{id}` | Update / delete a channel |
| POST | `/api/alerts/channels/{id}/test` | Send a test notification (no body) |
| GET, POST | `/api/alerts/rules` | List / create rules |
| PUT, DELETE | `/api/alerts/rules/{id}` | Update (omitted fields keep their value) / delete a rule |
| GET, POST | `/api/alerts/silences` | List active / create silences |
| DELETE | `/api/alerts/silences/{id}` | Remove a silence |
| GET | `/api/alerts/log?limit=N` | Newest send attempts (default 100, max 500) |

Channel: `{"name", "type", "config", "enabled"}`. Types and config keys:
- `email`: `host`, `port` (default 25), `username`, `password`, `from`, `to` (list). STARTTLS if the server offers it; implicit TLS (465) is not supported.
- `telegram`: `bot_token`, `chat_id`
- `slack`: `webhook_url` (incoming webhook)
- `discord`: `webhook_url`, optional `username`. Messages are cut at Discord's 2000-character limit and sent with mentions disabled.
- `ntfy`: `server` (default `https://ntfy.sh`), `topic`, optional `token` (Bearer) or `username` + `password` (Basic). Failures use priority `high`, recoveries `default`.
- `webhook`: `url`, optional `headers`. POSTs cmonit's own JSON `{kind, subject, time, items:[{host_id, hostname, service, type, message, since}]}`. Targets that expect another format (Teams, Twilio) need a relay.

`password`, `token`, `bot_token`, `webhook_url`, `url` and header values are returned as `***`. Sending `***` back on `PUT` keeps the stored value.

Rule: `{"name", "enabled", "trigger", "host_pattern", "service_pattern", "service_types", "hold_seconds", "cooldown_seconds", "notify_recovery", "channel_ids"}`.
- `trigger`: `service_failure` (default) or `host_offline` (no data for 5 poll intervals).
- `host_pattern`, `service_pattern`: glob (`*`, `?`) on hostname / service name; default `*`.
- `service_types`: Monit service type numbers 0-8; empty means all.
- `hold_seconds` (default 30): a failure is sent only after the oldest matching failure is this old. Everything failing on any host inside that window goes out as one notification; services that recover during it are never reported.
- `cooldown_seconds`: minimum gap before the same service is notified again after a flap.
- `notify_recovery` (default true): send one grouped message when notified failures recover.

Silence: `{"host_pattern", "service_pattern", "duration_seconds", "reason"}` in; `{"id", "host_pattern", "service_pattern", "expires_at", "reason"}` out (`expires_at` is unix seconds, 0 means until removed). `duration_seconds` 0 means until removed. Patterns default to `*`. While a silence matches, failures are marked as notified without sending, and pending recovery messages are dropped, so failures already present when it expires stay quiet and new ones alert normally. A host-offline incident is only silenced when `service_pattern` is `*`. Expired silences are deleted by the engine.

What counts as a failure: services with a failed test. Monit's legacy `status` also carries "changed" events (timestamp, size, content match, link speed and duplex), which last one cycle; they are not failures, and `status_hint` marks those bits. Agents that send `<errors>` (Monit 6.1 and later) give each test's exact state, so messages name the failed test ("Memory usage failed"). Older agents (6.0) send only the legacy bits: a failure is `status` minus the `status_hint` bits, and its message names the legacy class (for example "resource limit matched", without saying CPU or memory).

Changed events are sent as `event` notifications: immediately at the next flush (no hold, no recovery message), once per occurrence (a changed state that persists into the next poll is not repeated), to every `service_failure` rule whose filters match, unless silenced. Agents without `<errors>` produce class-level messages ("timestamp changed", "content match"). Events are kept in memory, so a restart can drop those from the last few seconds.

Failure times are Monit's own check timestamps. Notifications list items oldest first with offsets (`+4s`) and name the earliest in the subject (`first: postgres@db01`). This is a hint, not a root-cause diagnosis: resolution is one second at best and in practice the agents' poll interval, and agent clocks may differ.

Failed sends are logged in the alert log and not retried. Services of a host that is offline are not reported while it is offline; after it returns, new failures wait an extra 2 poll intervals.

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
