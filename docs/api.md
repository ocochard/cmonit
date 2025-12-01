# cmonit API Documentation

This document describes the M/Monit-compatible HTTP API provided by cmonit.

## Overview

cmonit provides a subset of the M/Monit HTTP API for querying host status, services, and events. This allows integration with existing M/Monit-compatible tools and scripts.

**Base URL**: `http://your-server:3000`

**Authentication**: All API endpoints respect the web UI authentication settings (`-web-user` and `-web-password`). If authentication is enabled, use HTTP Basic Auth.

**Content-Type**: All endpoints return `application/json`

---

## Status API

Query host and service status information.

### GET /status/hosts

List all monitored hosts with summary information.

**Request**:
```bash
curl http://localhost:3000/status/hosts
```

**Response**: `200 OK`
```json
[
  {
    "id": "bigone-0",
    "hostname": "bigone",
    "status": 0,
    "services": 13,
    "platform": "FreeBSD 16.0-CURRENT (amd64)",
    "lastseen": "2025-11-23T14:30:15Z",
    "monituptime": 3600,
    "cpupercent": 5,
    "mempercent": 42
  }
]
```

**Fields**:
- `id` (string) - Unique host identifier (hostname-incarnation)
- `hostname` (string) - Host name
- `status` (int) - Overall host status (0=OK, 1=Warning, 2=Critical)
- `services` (int) - Number of monitored services
- `platform` (string) - Operating system and architecture
- `lastseen` (string) - ISO 8601 timestamp of last update
- `monituptime` (int) - Monit daemon uptime in seconds
- `cpupercent` (int) - System CPU usage percentage
- `mempercent` (int) - System memory usage percentage

---

### GET /status/hosts/{id}

Get detailed information for a specific host.

**Request**:
```bash
curl http://localhost:3000/status/hosts/bigone-0
```

**Response**: `200 OK`
```json
{
  "id": "bigone-0",
  "hostname": "bigone",
  "status": 0,
  "platform": "FreeBSD 16.0-CURRENT (amd64)",
  "osname": "FreeBSD",
  "osrelease": "16.0-CURRENT",
  "machine": "amd64",
  "cpucount": 8,
  "memory": 16384,
  "swap": 8192,
  "lastseen": "2025-11-23T14:30:15Z",
  "monitversion": "5.35.2",
  "monituptime": 3600,
  "incarnation": "0",
  "cpupercent": 5,
  "mempercent": 42
}
```

**Additional Fields**:
- `osname` (string) - Operating system name
- `osrelease` (string) - OS release/version
- `machine` (string) - CPU architecture
- `cpucount` (int) - Number of CPU cores
- `memory` (int) - Total RAM in MB
- `swap` (int) - Total swap in MB
- `monitversion` (string) - Monit daemon version
- `incarnation` (string) - Monit restart counter

**Error Response**: `404 Not Found` if host doesn't exist

---

### GET /status/hosts/{id}/services

List all services monitored on a specific host.

**Request**:
```bash
curl http://localhost:3000/status/hosts/bigone-0/services
```

**Response**: `200 OK`
```json
[
  {
    "name": "system",
    "type": 5,
    "status": 0,
    "monitor": 1,
    "monitormode": 0,
    "pendingaction": 0
  },
  {
    "name": "sshd",
    "type": 3,
    "status": 0,
    "monitor": 1,
    "monitormode": 0,
    "pendingaction": 0,
    "pid": 1234,
    "ppid": 1,
    "memory": 12345,
    "cpu": 1
  },
  {
    "name": "rootfs",
    "type": 0,
    "status": 0,
    "monitor": 1,
    "monitormode": 0,
    "pendingaction": 0,
    "blocks": 5000000,
    "blockstotal": 10000000,
    "percent": 50
  }
]
```

**Service Fields**:

**Common to all services**:
- `name` (string) - Service name
- `type` (int) - Service type (0=filesystem, 3=process, 5=system, etc.)
- `status` (int) - Service status (0=OK, 1=Warning, 2=Critical)
- `monitor` (int) - Monitoring enabled (0=no, 1=yes, 2=initializing)
- `monitormode` (int) - Monitoring mode (0=active, 1=passive)
- `pendingaction` (int) - Pending action code

**Process services** (type 3):
- `pid` (int) - Process ID
- `ppid` (int) - Parent process ID
- `memory` (int) - Memory usage in KB
- `cpu` (int) - CPU usage percentage

**Filesystem services** (type 0):
- `blocks` (int) - Used blocks
- `blockstotal` (int) - Total blocks
- `percent` (int) - Usage percentage

**Service Types**:
- 0 = Filesystem
- 1 = Directory
- 2 = File
- 3 = Process
- 4 = Remote host
- 5 = System
- 6 = Fifo
- 7 = Program
- 8 = Network

**Status Codes**:
- 0 = Running/OK
- 1 = Warning/Degraded
- 2 = Critical/Failed
- -1 = Unknown/Not monitored

---

## Events API

Query event history and state changes.

### GET /events/list

List all events with optional pagination.

**Request**:
```bash
# All events
curl http://localhost:3000/events/list

# With pagination
curl "http://localhost:3000/events/list?limit=10&offset=0"
```

**Query Parameters**:
- `limit` (int, optional) - Maximum number of events to return (default: 100)
- `offset` (int, optional) - Number of events to skip (default: 0)

**Response**: `200 OK`
```json
{
  "records": [
    {
      "id": 42,
      "hostname": "bigone",
      "service": "nginx",
      "event_type": "service_status_change",
      "message": "Service status changed from running to failed",
      "timestamp": "2025-11-23T14:25:00Z"
    },
    {
      "id": 41,
      "hostname": "bigone",
      "service": "monit",
      "event_type": "monit_restart",
      "message": "Monit daemon restarted (uptime decreased)",
      "timestamp": "2025-11-23T14:20:00Z"
    }
  ],
  "recordsReturned": 2,
  "totalRecords": 2
}
```

**Response Fields**:
- `records` (array) - Array of event objects
- `recordsReturned` (int) - Number of events in this response
- `totalRecords` (int) - Total number of events in database

**Event Fields**:
- `id` (int) - Unique event ID
- `hostname` (string) - Host where event occurred
- `service` (string) - Service name
- `event_type` (string) - Type of event
- `message` (string) - Human-readable event description
- `timestamp` (string) - ISO 8601 timestamp

**Event Types**:
- `service_status_change` - Service status changed (running → failed, etc.)
- `service_monitor_change` - Monitoring state changed
- `monit_restart` - Monit daemon restarted
- `service_action` - Action executed on service

---

### GET /events/get/{id}

Get details for a specific event.

**Request**:
```bash
curl http://localhost:3000/events/get/42
```

**Response**: `200 OK`
```json
{
  "id": 42,
  "hostname": "bigone",
  "service": "nginx",
  "event_type": "service_status_change",
  "message": "Service status changed from running to failed",
  "timestamp": "2025-11-23T14:25:00Z"
}
```

**Error Response**: `404 Not Found` if event doesn't exist

---

## Admin API

Administrative endpoints for host management.

### GET /admin/hosts

Get administrative view of all hosts.

**Request**:
```bash
curl http://localhost:3000/admin/hosts
```

**Response**: `200 OK`
```json
{
  "records": [
    {
      "id": "bigone-0",
      "hostname": "bigone",
      "status": 0,
      "services": 13,
      "platform": "FreeBSD 16.0-CURRENT (amd64)",
      "lastseen": "2025-11-23T14:30:15Z",
      "monituptime": 3600,
      "cpupercent": 5,
      "mempercent": 42
    }
  ],
  "recordsReturned": 1,
  "totalRecords": 1
}
```

**Response Fields**:
- `records` (array) - Array of host summary objects (same format as `/status/hosts`)
- `recordsReturned` (int) - Number of hosts in this response
- `totalRecords` (int) - Total number of hosts

---

## Authentication

When cmonit is started with `-web-user` and `-web-password`, all API endpoints require HTTP Basic Authentication.

**Example with authentication**:
```bash
curl -u admin:password http://localhost:3000/status/hosts
```

**Unauthorized Response**: `401 Unauthorized`
```
HTTP/1.1 401 Unauthorized
WWW-Authenticate: Basic realm="cmonit"
```

---

## Error Handling

### HTTP Status Codes

- `200 OK` - Request succeeded
- `401 Unauthorized` - Authentication required or failed
- `404 Not Found` - Resource doesn't exist (host, event, etc.)
- `500 Internal Server Error` - Server error (database issue, etc.)

### Error Response Format

Most errors return only an HTTP status code. For some endpoints, a JSON error message may be included:

```json
{
  "error": "Host not found"
}
```

---

## Rate Limiting

Currently, cmonit does not implement rate limiting. Consider using a reverse proxy (nginx, HAProxy) for rate limiting in production.

---

## CORS

CORS headers are not currently set. If you need to access the API from a web application on a different domain, use a reverse proxy to add CORS headers.

---

## Usage Examples

### Shell Script: Check Host Status

```bash
#!/bin/sh
# check-host-status.sh - Monitor host status via API

API_URL="http://localhost:3000"
HOST_ID="bigone-0"

# Get host details
response=$(curl -s "${API_URL}/status/hosts/${HOST_ID}")
cpupercent=$(echo "$response" | jq -r '.cpupercent')
mempercent=$(echo "$response" | jq -r '.mempercent')

echo "Host: ${HOST_ID}"
echo "CPU: ${cpupercent}%"
echo "Memory: ${mempercent}%"

# Alert if high usage
if [ "$cpupercent" -gt 80 ] || [ "$mempercent" -gt 90 ]; then
    echo "WARNING: High resource usage detected!"
    exit 1
fi
```

### Python: List All Events

```python
#!/usr/bin/env python3
# list-events.py - Fetch and display recent events

import requests
import json
from datetime import datetime

API_URL = "http://localhost:3000"

# Fetch recent events
response = requests.get(f"{API_URL}/events/list?limit=10")
data = response.json()

print(f"Recent Events (showing {data['recordsReturned']} of {data['totalRecords']}):\n")

for event in data['records']:
    timestamp = datetime.fromisoformat(event['timestamp'].replace('Z', '+00:00'))
    print(f"[{timestamp.strftime('%Y-%m-%d %H:%M:%S')}] {event['hostname']}/{event['service']}")
    print(f"  Type: {event['event_type']}")
    print(f"  Message: {event['message']}\n")
```

### JavaScript: Dashboard Integration

```javascript
// dashboard.js - Fetch host status for web dashboard

async function fetchHostStatus() {
    const response = await fetch('http://localhost:3000/status/hosts');
    const hosts = await response.json();

    hosts.forEach(host => {
        console.log(`${host.hostname}: ${host.services} services, CPU ${host.cpupercent}%, Memory ${host.mempercent}%`);

        // Update UI
        updateHostCard(host);
    });
}

function updateHostCard(host) {
    const statusColor = host.status === 0 ? 'green' :
                       host.status === 1 ? 'yellow' : 'red';

    // ... update DOM elements
}

// Poll every 30 seconds
setInterval(fetchHostStatus, 30000);
fetchHostStatus(); // Initial load
```

### Nagios Check Plugin

```bash
#!/bin/sh
# check_cmonit_host.sh - Nagios/Icinga plugin for cmonit

API_URL="http://localhost:3000"
HOST_ID="$1"

if [ -z "$HOST_ID" ]; then
    echo "UNKNOWN - Host ID required"
    exit 3
fi

response=$(curl -s -w "%{http_code}" "${API_URL}/status/hosts/${HOST_ID}")
http_code="${response: -3}"
json_data="${response:0:-3}"

if [ "$http_code" != "200" ]; then
    echo "CRITICAL - API returned HTTP $http_code"
    exit 2
fi

status=$(echo "$json_data" | jq -r '.status')
services=$(echo "$json_data" | jq -r '.services')
cpupercent=$(echo "$json_data" | jq -r '.cpupercent')
mempercent=$(echo "$json_data" | jq -r '.mempercent')

case "$status" in
    0)
        echo "OK - Host ${HOST_ID}: ${services} services, CPU ${cpupercent}%, Memory ${mempercent}%"
        exit 0
        ;;
    1)
        echo "WARNING - Host ${HOST_ID}: Some services degraded"
        exit 1
        ;;
    2|*)
        echo "CRITICAL - Host ${HOST_ID}: Service failures detected"
        exit 2
        ;;
esac
```

---

## Differences from M/Monit API

cmonit implements a **subset** of the M/Monit HTTP API. Key differences:

### Implemented Features ✅

- `GET /status/hosts` - List all hosts
- `GET /status/hosts/{id}` - Get host details
- `GET /status/hosts/{id}/services` - List services
- `GET /events/list` - List events with pagination
- `GET /events/get/{id}` - Get specific event
- `GET /admin/hosts` - Administrative host list

### Not Implemented ❌

- `POST /admin/hosts` - Add/remove hosts (cmonit uses push model)
- `POST /admin/hosts/{id}/action` - Execute actions (use cmonit's `/api/action` instead)
- `GET /reports/*` - Reporting endpoints
- `GET /uptime/*` - Uptime statistics
- WebSocket endpoints for real-time updates
- User management endpoints

### Alternative Endpoints

For service actions (start, stop, restart), use cmonit's native action API:

```bash
# cmonit action API (not M/Monit compatible)
curl -X POST http://localhost:3000/api/action \
  -H "Content-Type: application/json" \
  -d '{
    "host_id": "bigone-0",
    "service": "nginx",
    "action": "restart"
  }'
```

---

## Performance Considerations

- API queries read directly from SQLite database
- No caching is currently implemented
- Response times typically < 100ms for status queries
- Event queries may be slower with large datasets (use pagination)
- Concurrent requests are supported (SQLite WAL mode)
- Consider caching results if polling frequently (< 10 seconds)

---

## Security Best Practices

1. **Enable Authentication**: Always use `-web-user` and `-web-password` in production
2. **Use HTTPS**: Enable TLS with `-web-cert` and `-web-key` for sensitive environments
3. **Firewall Rules**: Restrict API access to trusted networks
4. **Reverse Proxy**: Use nginx/HAProxy for additional security (rate limiting, IP filtering)
5. **Monitor Access**: Check logs for unauthorized access attempts

---

## Troubleshooting

### Empty Results

**Issue**: API returns empty arrays `[]` or `null` values

**Solution**:
- Verify Monit agents are configured to send data to cmonit collector
- Check that collector is receiving data: `sqlite3 cmonit.db "SELECT COUNT(*) FROM hosts;"`
- Ensure Monit agents have updated at least once (wait 30-60 seconds)

### Slow Queries

**Issue**: API responses take several seconds

**Solution**:
- Check database size: `ls -lh cmonit.db`
- Rebuild database if large: backup, delete old data, restart
- Ensure SQLite WAL mode is enabled (automatic in cmonit)
- Use pagination for event queries

### Authentication Not Working

**Issue**: API accepts requests without credentials

**Solution**:
- Verify cmonit was started with both `-web-user` and `-web-password` flags
- Check logs for authentication configuration messages
- Test with curl: `curl -i http://localhost:3000/status/hosts` (should return 401)

---

## API Versioning

Currently, cmonit does not version its API. The API is stable for the implemented M/Monit-compatible endpoints. Future versions will maintain backward compatibility where possible.

---

## Support

For issues, feature requests, or questions:
- GitHub Issues: https://github.com/your-org/cmonit/issues
- Documentation: See `docs/` directory
- M/Monit API Reference: https://mmonit.com/documentation/http-api/

---

## License

Same as cmonit - See [LICENSE](../LICENSE) file for details.
