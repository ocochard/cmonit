# cmonit Testing Plan

## Overview

This document outlines all acceptance tests that must pass for each phase of development. Following the **Plan → Act → Validate** loop, we will execute one test at a time and ensure it passes before moving to the next.

---

## Phase 1: Collector Daemon Tests

### Test Environment
- FreeBSD system with Monit running
- Monit configured to send to `http://monit:monit@127.0.0.1:8080/collector`
- Current monitored services: system, sshd, nginx, file checks, temperature check

### T1.1: Server Startup
**Description**: Verify the cmonit daemon starts successfully

**Steps**:
1. Run `./cmonit`
2. Check that process starts without errors
3. Verify it listens on port 8080

**Expected**:
```
$ ./cmonit
[INFO] cmonit starting...
[INFO] Collector listening on :8080
[INFO] Database initialized: cmonit.db
```

**Validation**:
```bash
netstat -an | grep 8080
# Should show: *.8080  *.*  LISTEN
```

**Status**: ⬜ Not started

---

### T1.2: Endpoint Availability
**Description**: Verify /collector endpoint responds

**Steps**:
1. Send GET request to /collector
2. Verify it responds (even if with error)

**Command**:
```bash
curl -v http://localhost:8080/collector
```

**Expected**:
- HTTP response (405 Method Not Allowed or similar)
- Server should not crash

**Status**: ⬜ Not started

---

### T1.3: Authentication - Rejected Without Credentials
**Description**: Verify requests without auth are rejected

**Steps**:
1. Send POST request without Authorization header
2. Verify 401 Unauthorized response

**Command**:
```bash
curl -X POST http://localhost:8080/collector \
  -H "Content-Type: text/xml" \
  -d "<test/>"
```

**Expected**:
```
HTTP/1.1 401 Unauthorized
WWW-Authenticate: Basic realm="cmonit"
```

**Status**: ⬜ Not started

---

### T1.4: Authentication - Rejected With Wrong Credentials
**Description**: Verify requests with wrong credentials are rejected

**Steps**:
1. Send POST request with wrong username/password
2. Verify 401 Unauthorized response

**Command**:
```bash
curl -X POST http://localhost:8080/collector \
  -u "wrong:password" \
  -H "Content-Type: text/xml" \
  -d "<test/>"
```

**Expected**:
```
HTTP/1.1 401 Unauthorized
```

**Status**: ⬜ Not started

---

### T1.5: Authentication - Accepted With Correct Credentials
**Description**: Verify requests with correct credentials are accepted

**Steps**:
1. Send POST request with monit:monit credentials
2. Verify 200 OK response

**Command**:
```bash
curl -X POST http://localhost:8080/collector \
  -u "monit:monit" \
  -H "Content-Type: text/xml" \
  -d "<test/>"
```

**Expected**:
```
HTTP/1.1 200 OK
Server: cmonit/0.1
```

**Status**: ⬜ Not started

---

### T1.6: Database Creation
**Description**: Verify database file is created on first run

**Steps**:
1. Delete cmonit.db if exists
2. Start cmonit
3. Check that cmonit.db file exists
4. Verify schema is created

**Commands**:
```bash
rm -f cmonit.db
./cmonit &
sleep 1
ls -l cmonit.db
sqlite3 cmonit.db ".schema"
```

**Expected**:
- cmonit.db file exists
- Tables created: hosts, services, metrics, events
- Indexes created

**Status**: ⬜ Not started

---

### T1.7: XML Parsing - System Service
**Description**: Verify XML from Monit can be parsed

**Steps**:
1. Capture XML from real Monit agent
2. Send it to collector
3. Verify it parses without errors

**Command**:
```bash
# Capture XML from monit
curl -u admin:monit http://localhost:2812/_status?format=xml > test-status.xml

# Send to cmonit
curl -X POST http://localhost:8080/collector \
  -u "monit:monit" \
  -H "Content-Type: text/xml" \
  --data-binary @test-status.xml
```

**Expected**:
```
HTTP/1.1 200 OK
Server: cmonit/0.1
```

**Validation**:
- No errors in cmonit logs
- Log shows "Parsed X services"

**Status**: ⬜ Not started

---

### T1.8: Database - Host Record Created
**Description**: Verify host record is created on first contact

**Steps**:
1. Send status from Monit
2. Query database for host record

**Command**:
```bash
sqlite3 cmonit.db "SELECT * FROM hosts;"
```

**Expected**:
```
<host-id>|<hostname>|<incarnation>|<version>|<timestamp>|<timestamp>
```

**Status**: ⬜ Not started

---

### T1.9: Database - Host Record Updated
**Description**: Verify host last_seen is updated on each contact

**Steps**:
1. Send status from Monit
2. Note last_seen timestamp
3. Wait 30 seconds for Monit to send again
4. Query database again
5. Verify last_seen has been updated

**Command**:
```bash
sqlite3 cmonit.db "SELECT hostname, last_seen FROM hosts;"
# Wait for next Monit update
sleep 35
sqlite3 cmonit.db "SELECT hostname, last_seen FROM hosts;"
```

**Expected**:
- last_seen timestamp increases

**Status**: ⬜ Not started

---

### T1.10: Database - Service Records Created
**Description**: Verify service records are created

**Steps**:
1. Send status from Monit
2. Query database for service records

**Command**:
```bash
sqlite3 cmonit.db "SELECT host_id, name, type, status FROM services;"
```

**Expected**:
- Multiple service records
- Should see: system service, sshd, nginx, etc.

**Status**: ⬜ Not started

---

### T1.11: Database - Service Records Updated
**Description**: Verify service records are updated on each status update

**Steps**:
1. Query service last_seen
2. Wait for next Monit update
3. Query again
4. Verify last_seen updated

**Command**:
```bash
sqlite3 cmonit.db "SELECT name, last_seen FROM services LIMIT 5;"
sleep 35
sqlite3 cmonit.db "SELECT name, last_seen FROM services LIMIT 5;"
```

**Expected**:
- last_seen timestamps increase

**Status**: ⬜ Not started

---

### T1.12: Database - Metrics Stored
**Description**: Verify time-series metrics are stored

**Steps**:
1. Send status from Monit
2. Query metrics table

**Command**:
```bash
sqlite3 cmonit.db "SELECT service_name, metric_type, metric_name, value, collected_at FROM metrics ORDER BY collected_at DESC LIMIT 20;"
```

**Expected**:
```
system|cpu|user|5.2|2025-11-22 21:30:00
system|cpu|system|2.1|2025-11-22 21:30:00
system|memory|percent|45.6|2025-11-22 21:30:00
system|load|avg01|1.23|2025-11-22 21:30:00
...
```

**Status**: ⬜ Not started

---

### T1.13: Database - Metrics Accumulate Over Time
**Description**: Verify metrics accumulate with each status update

**Steps**:
1. Check metrics count
2. Wait for 2 more Monit updates (60 seconds)
3. Check metrics count again

**Command**:
```bash
sqlite3 cmonit.db "SELECT COUNT(*) FROM metrics;"
sleep 65
sqlite3 cmonit.db "SELECT COUNT(*) FROM metrics;"
```

**Expected**:
- Count increases by ~50-100 per update (depending on services)

**Status**: ⬜ Not started

---

### T1.14: Response Headers
**Description**: Verify collector returns correct headers

**Steps**:
1. Send request
2. Check response headers

**Command**:
```bash
curl -X POST http://localhost:8080/collector \
  -u "monit:monit" \
  -H "Content-Type: text/xml" \
  -d "<test/>" \
  -v 2>&1 | grep "Server:"
```

**Expected**:
```
< Server: cmonit/0.1
```

**Status**: ⬜ Not started

---

### T1.15: Gzip Compression Support
**Description**: Verify collector can handle gzip-compressed requests

**Steps**:
1. Create gzipped XML
2. Send with Content-Encoding: gzip
3. Verify it's accepted

**Command**:
```bash
echo "<test/>" | gzip > test.xml.gz
curl -X POST http://localhost:8080/collector \
  -u "monit:monit" \
  -H "Content-Type: text/xml" \
  -H "Content-Encoding: gzip" \
  --data-binary @test.xml.gz
```

**Expected**:
```
HTTP/1.1 200 OK
```

**Status**: ⬜ Not started

---

### T1.16: Continuous Operation
**Description**: Verify collector runs continuously without crashes

**Steps**:
1. Start collector
2. Let it run for 5 minutes (10 Monit updates)
3. Verify no crashes or memory leaks

**Commands**:
```bash
./cmonit &
CMONIT_PID=$!
sleep 300
ps -p $CMONIT_PID  # Should still be running
kill $CMONIT_PID
```

**Expected**:
- Process still running after 5 minutes
- No error messages
- Memory usage stable

**Status**: ⬜ Not started

---

## Phase 2: Web Dashboard Tests

### Test Environment
- cmonit collector running with data
- At least 5 minutes of collected metrics

### T2.1: Web Server Startup
**Description**: Verify web server starts on port 3000

**Steps**:
1. Start cmonit
2. Verify web server listening

**Command**:
```bash
./cmonit
# In another terminal:
netstat -an | grep 3000
```

**Expected**:
```
[INFO] Web server listening on :3000
*.3000  *.*  LISTEN
```

**Status**: ⬜ Not started

---

### T2.2: Dashboard Accessible
**Description**: Verify dashboard page loads

**Steps**:
1. Open browser to http://localhost:3000/
2. Verify page loads without errors

**Expected**:
- HTTP 200 OK
- HTML page rendered
- No JavaScript errors in console

**Status**: ⬜ Not started

---

### T2.3: Dashboard Shows Hosts
**Description**: Verify all monitored hosts are displayed

**Steps**:
1. Open dashboard
2. Verify host list is displayed

**Expected**:
- At least 1 host displayed
- Hostname shown correctly
- Last seen timestamp shown

**Status**: ⬜ Not started

---

### T2.4: Dashboard Shows Services
**Description**: Verify all services for each host are displayed

**Steps**:
1. Open dashboard
2. Check service list

**Expected**:
- System service shown
- sshd process shown
- nginx process shown
- Other monitored services shown

**Status**: ⬜ Not started

---

### T2.5: Service Status Colors
**Description**: Verify status indicators are color-coded

**Steps**:
1. View dashboard
2. Check service status colors

**Expected**:
- Green for OK/running services
- Red for failed services
- Yellow for warning/degraded
- Grey for not monitored

**Status**: ⬜ Not started

---

### T2.6: Service Metrics Display
**Description**: Verify key metrics are displayed for each service

**Steps**:
1. View dashboard
2. Check metrics shown

**Expected**:
- System: CPU%, Memory%, Swap%, Load average
- Processes: Status, PID, Memory%, CPU%
- Filesystem: Usage%, Space used/total

**Status**: ⬜ Not started

---

### T2.7: Timestamps Display
**Description**: Verify timestamps are shown and formatted correctly

**Steps**:
1. View dashboard
2. Check timestamp format

**Expected**:
- Last seen: "2 minutes ago" or similar
- Collected at: proper datetime format

**Status**: ⬜ Not started

---

### T2.8: Auto-Refresh
**Description**: Verify page auto-refreshes every 30 seconds

**Steps**:
1. Open dashboard
2. Note a metric value
3. Wait 30 seconds
4. Verify page refreshed and value updated

**Expected**:
- Page refreshes automatically
- No full page reload (AJAX or meta refresh)
- Updated data shown

**Status**: ⬜ Not started

---

### T2.9: Mobile Responsiveness
**Description**: Verify dashboard works on mobile screen sizes

**Steps**:
1. Open dashboard
2. Resize browser to mobile width (375px)
3. Verify layout adapts

**Expected**:
- No horizontal scrolling
- Content stacks vertically
- Text remains readable
- Tables/cards adapt to small screen

**Status**: ⬜ Not started

---

### T2.10: No JavaScript Errors
**Description**: Verify no JavaScript errors occur

**Steps**:
1. Open dashboard with browser console open
2. Check for errors

**Expected**:
- Console shows no errors
- All JavaScript loads successfully
- Tailwind CSS loads from CDN

**Status**: ⬜ Not started

---

## Phase 3: Time-Series Graphs Tests

### Test Environment
- cmonit running with at least 1 hour of collected data
- Multiple metric types available

### T3.1: Graphs Page Accessible
**Description**: Verify graphs page loads for a host

**Steps**:
1. Navigate to graphs page for a host
2. Verify page loads

**Expected**:
- HTTP 200 OK
- Graphs page rendered
- Chart.js loads from CDN

**Status**: ⬜ Not started

---

### T3.2: CPU Usage Graph
**Description**: Verify CPU usage graph displays correctly

**Steps**:
1. Open graphs page
2. Check CPU graph

**Expected**:
- Line chart displayed
- Multiple lines: user, system, nice, wait
- X-axis shows time
- Y-axis shows percentage (0-100%)
- Legend shows line colors

**Status**: ⬜ Not started

---

### T3.3: Memory Usage Graph
**Description**: Verify memory usage graph displays correctly

**Steps**:
1. Open graphs page
2. Check memory graph

**Expected**:
- Line chart displayed
- Memory usage shown in percentage or GB
- Swap usage also shown (if available)

**Status**: ⬜ Not started

---

### T3.4: Load Average Graph
**Description**: Verify load average graph displays correctly

**Steps**:
1. Open graphs page
2. Check load average graph

**Expected**:
- Line chart with 3 lines: 1min, 5min, 15min
- X-axis shows time
- Y-axis shows load value

**Status**: ⬜ Not started

---

### T3.5: Disk Space Graph
**Description**: Verify disk space usage graph displays correctly

**Steps**:
1. Open graphs page
2. Check disk graph

**Expected**:
- Graph for each monitored filesystem
- Shows used space over time
- Percentage or absolute values

**Status**: ⬜ Not started

---

### T3.6: Time Range Selector - 1 Hour
**Description**: Verify 1-hour time range works

**Steps**:
1. Select "1h" time range
2. Verify graph updates

**Expected**:
- Graph shows last 1 hour of data
- X-axis spans 1 hour
- Data points updated

**Status**: ⬜ Not started

---

### T3.7: Time Range Selector - 6 Hours
**Description**: Verify 6-hour time range works

**Steps**:
1. Select "6h" time range
2. Verify graph updates

**Expected**:
- Graph shows last 6 hours of data
- More data points than 1h view

**Status**: ⬜ Not started

---

### T3.8: Time Range Selector - 24 Hours
**Description**: Verify 24-hour time range works

**Steps**:
1. Select "24h" time range
2. Verify graph updates

**Expected**:
- Graph shows last 24 hours of data
- Full day visible on X-axis

**Status**: ⬜ Not started

---

### T3.9: Time Range Selector - 7 Days
**Description**: Verify 7-day time range works

**Steps**:
1. Select "7d" time range
2. Verify graph updates

**Expected**:
- Graph shows last 7 days of data
- Data points possibly aggregated

**Status**: ⬜ Not started

---

### T3.10: Time Range Selector - 30 Days
**Description**: Verify 30-day time range works

**Steps**:
1. Select "30d" time range
2. Verify graph updates

**Expected**:
- Graph shows last 30 days of data
- Data points aggregated for performance

**Status**: ⬜ Not started

---

### T3.11: Hover Tooltips
**Description**: Verify tooltips show exact values on hover

**Steps**:
1. Hover over data point on graph
2. Check tooltip

**Expected**:
- Tooltip appears
- Shows exact timestamp
- Shows exact value
- Shows metric name

**Status**: ⬜ Not started

---

### T3.12: Legend Functionality
**Description**: Verify legend can toggle series visibility

**Steps**:
1. Click on legend item
2. Verify series toggles

**Expected**:
- Clicking legend item hides/shows that series
- Other series remain visible
- Graph rescales appropriately

**Status**: ⬜ Not started

---

### T3.13: Mobile Responsiveness - Graphs
**Description**: Verify graphs work on mobile

**Steps**:
1. Open graphs page on mobile width
2. Check graphs render

**Expected**:
- Graphs scale to screen width
- Touch interactions work
- Tooltips work on touch
- Time range selector usable

**Status**: ⬜ Not started

---

### T3.14: Performance - Large Dataset
**Description**: Verify graphs perform well with lots of data

**Steps**:
1. Let collector run for several hours
2. Open 24h graph view
3. Check performance

**Expected**:
- Graph loads in < 2 seconds
- No browser lag
- Smooth interactions
- Memory usage reasonable

**Status**: ⬜ Not started

---

### T3.15: Data Interpolation
**Description**: Verify missing data points are handled correctly

**Steps**:
1. Stop monit agent briefly
2. Restart it
3. View graph spanning the gap

**Expected**:
- Gap in data is visible (line breaks) OR
- Interpolation is clearly indicated
- No misleading data

**Status**: ⬜ Not started

---

## Phase 4: API Compatibility Tests

(To be detailed when starting Phase 4)

---

## Running Tests

### Test Execution Order

Tests must be run in order within each phase:

```bash
# Phase 1
./run-test.sh T1.1  # Server startup
./run-test.sh T1.2  # Endpoint availability
./run-test.sh T1.3  # Auth rejection
... (continue sequentially)

# Phase 2
./run-test.sh T2.1  # Web server startup
... (continue sequentially)

# Phase 3
./run-test.sh T3.1  # Graphs page
... (continue sequentially)
```

### Test Result Tracking

Update this document after each test:
- ⬜ Not started
- ⏳ In progress
- ✅ Passed
- ❌ Failed (add notes)
- ⚠️ Partially passed (add notes)

### Test Failure Protocol

If a test fails:
1. Document the failure details
2. Fix the issue
3. Re-run the failed test
4. Re-run any dependent tests
5. Continue only when test passes

---

## Continuous Testing

### Regression Testing
After each phase, run ALL previous tests to ensure no regressions:

```bash
# After completing Phase 2
./run-all-tests.sh phase1
./run-all-tests.sh phase2

# After completing Phase 3
./run-all-tests.sh phase1
./run-all-tests.sh phase2
./run-all-tests.sh phase3
```

### Performance Baseline
Track performance metrics over time:
- API response times
- Database query times
- Memory usage
- CPU usage
- Database file size

---

## Test Data Cleanup

Between test runs, you may need to clean up:

```bash
# Stop cmonit
pkill cmonit

# Backup database (if needed)
cp cmonit.db cmonit.db.backup

# Clean database
rm cmonit.db

# Restart for fresh test
./cmonit
```

---

## Automated Testing

Future improvement: Create automated test suite

```bash
# Run all automated tests
make test

# Run specific phase tests
make test-phase1
make test-phase2
make test-phase3

# Run with coverage
make test-coverage
```
