# cmonit Troubleshooting Guide

This document provides troubleshooting tips, diagnostic commands, and solutions to common issues.

## Table of Contents

- [Database Queries](#database-queries)
  - [List All Hosts](#list-all-hosts)
  - [Check Host Status](#check-host-status)
  - [Query Services](#query-services)
  - [Check Availability Data](#check-availability-data)
- [Service Issues](#service-issues)
- [Performance Issues](#performance-issues)
- [Data Issues](#data-issues)

---

## Database Queries

cmonit uses SQLite for data storage. You can query the database directly for troubleshooting.

**Database location**: `/var/run/cmonit/cmonit.db` (default)

### List All Hosts

**Simple list of hostnames:**
```bash
sqlite3 /var/run/cmonit/cmonit.db "SELECT hostname FROM hosts ORDER BY hostname"
```

**With OS information:**
```bash
sqlite3 /var/run/cmonit/cmonit.db "SELECT hostname, os_name, last_seen FROM hosts ORDER BY hostname"
```

**Detailed view with IDs:**
```bash
sqlite3 /var/run/cmonit/cmonit.db "SELECT hostname, os_name, datetime(last_seen), id FROM hosts ORDER BY hostname"
```

**With creation and last seen timestamps:**
```bash
sqlite3 /var/run/cmonit/cmonit.db "SELECT id, hostname, os_name, created_at, last_seen FROM hosts ORDER BY hostname"
```

### Check Host Status

**Check online/offline status:**
```bash
sqlite3 /var/run/cmonit/cmonit.db "SELECT hostname, CAST((strftime('%s', 'now') - strftime('%s', last_seen)) AS INTEGER) as seconds_ago FROM hosts ORDER BY hostname"
```

**With online/offline indicator:**
```bash
sqlite3 /var/run/cmonit/cmonit.db "
SELECT
  hostname,
  CAST((strftime('%s', 'now') - strftime('%s', last_seen)) AS INTEGER) as seconds_ago,
  CASE
    WHEN (strftime('%s', 'now') - strftime('%s', last_seen)) < 300 THEN 'ONLINE'
    ELSE 'OFFLINE'
  END as status
FROM hosts
ORDER BY seconds_ago DESC"
```

**Count hosts by OS:**
```bash
sqlite3 /var/run/cmonit/cmonit.db "
SELECT
  COUNT(*) as total,
  COUNT(CASE WHEN os_name = 'FreeBSD' THEN 1 END) as freebsd,
  COUNT(CASE WHEN os_name = 'Linux' THEN 1 END) as linux,
  COUNT(CASE WHEN os_name = 'Darwin' THEN 1 END) as darwin
FROM hosts"
```

### Query Services

**List all services for a specific host:**
```bash
sqlite3 /var/run/cmonit/cmonit.db "
SELECT name, type, status, monitor, last_seen
FROM services
WHERE host_id = 'your-host-id'
ORDER BY name"
```

**Count services per host:**
```bash
sqlite3 /var/run/cmonit/cmonit.db "
SELECT host_id, COUNT(*) as service_count
FROM services
GROUP BY host_id
ORDER BY service_count DESC"
```

**Find services with issues (status != 0):**
```bash
sqlite3 /var/run/cmonit/cmonit.db "
SELECT h.hostname, s.name, s.type, s.status
FROM services s
JOIN hosts h ON s.host_id = h.id
WHERE s.status != 0
ORDER BY h.hostname, s.name"
```

### Check Availability Data

**Count availability records per host:**
```bash
sqlite3 /var/run/cmonit/cmonit.db "
SELECT host_id, COUNT(*) as record_count
FROM host_availability
GROUP BY host_id
ORDER BY host_id"
```

**Check availability time range for a specific host:**
```bash
sqlite3 /var/run/cmonit/cmonit.db "
SELECT
  MIN(timestamp) as first_ts,
  MAX(timestamp) as last_ts,
  COUNT(*) as records,
  datetime(MIN(timestamp), 'unixepoch') as first_time,
  datetime(MAX(timestamp), 'unixepoch') as last_time
FROM host_availability
WHERE host_id = 'your-host-id'"
```

**Check total database size and record counts:**
```bash
sqlite3 /var/run/cmonit/cmonit.db "
SELECT
  (SELECT COUNT(*) FROM hosts) as hosts,
  (SELECT COUNT(*) FROM services) as services,
  (SELECT COUNT(*) FROM metrics) as metrics,
  (SELECT COUNT(*) FROM host_availability) as availability,
  (SELECT COUNT(*) FROM events) as events"
```

---

## Service Issues

### cmonit Service Won't Start

**Check if the service is running:**
```bash
sudo service cmonit status
```

**Check logs for errors:**
```bash
# FreeBSD (syslog)
sudo tail -100 /var/log/daemon.log | grep cmonit

# Or check all recent cmonit logs
sudo grep cmonit /var/log/daemon.log | tail -50
```

**Common errors:**

1. **Database locked:**
   ```
   [ERROR] Failed to initialize database: database is locked
   ```
   **Solution**: Another cmonit process is running. Stop it first:
   ```bash
   sudo service cmonit stop
   sudo pkill -9 cmonit
   sudo service cmonit start
   ```

2. **Permission denied:**
   ```
   [ERROR] Failed to open database: unable to open database file
   ```
   **Solution**: Check database directory permissions:
   ```bash
   sudo ls -ld /var/run/cmonit/
   sudo mkdir -p /var/run/cmonit/
   sudo chown nobody:wheel /var/run/cmonit/
   ```

3. **Port already in use:**
   ```
   [ERROR] Failed to start collector: address already in use
   ```
   **Solution**: Check what's using the port:
   ```bash
   sudo sockstat -l | grep -E ':(3000|8080)'
   ```

### Monit Agents Not Connecting

**Check if collector is listening:**
```bash
netstat -an | grep -E ':(8080|3000)'
```

**Test collector endpoint:**
```bash
curl -v http://localhost:8080/collector
# Should return 401 Unauthorized (expects POST with auth)
```

**Check Monit agent configuration:**
```bash
# On the Monit agent host
grep -A 5 "set mmonit" /usr/local/etc/monitrc
```

**Check Monit agent logs:**
```bash
# On the Monit agent host
grep -i mmonit /var/log/messages
grep -i collector /var/log/messages
```

### Data Not Appearing in Dashboard

**Check if data is being received:**
```bash
sudo tail -f /var/log/daemon.log | grep "Stored status"
```

**Verify host exists in database:**
```bash
sqlite3 /var/run/cmonit/cmonit.db "SELECT hostname, last_seen FROM hosts"
```

**Check for database errors:**
```bash
sudo grep -E "ERROR|FATAL" /var/log/daemon.log | grep cmonit | tail -20
```

---

## Performance Issues

### Dashboard Loading Slowly

**Check database size:**
```bash
du -h /var/run/cmonit/cmonit.db*
```

**Check metric count:**
```bash
sqlite3 /var/run/cmonit/cmonit.db "SELECT COUNT(*) FROM metrics"
```

**Vacuum database (optimize):**
```bash
sudo service cmonit stop
sudo sqlite3 /var/run/cmonit/cmonit.db "VACUUM;"
sudo service cmonit start
```

### High Memory Usage

**Check cmonit process memory:**
```bash
ps aux | grep cmonit | grep -v grep
```

**Check database connections:**
```bash
sudo lsof | grep cmonit.db
```

---

## Data Issues

### Availability Graph Shows No Data

**Check if availability recording is enabled:**
```bash
sudo grep "availability recording" /var/log/daemon.log | tail -5
```

**Verify availability records exist:**
```bash
sqlite3 /var/run/cmonit/cmonit.db "SELECT COUNT(*) FROM host_availability"
```

**Check for recent availability data:**
```bash
sqlite3 /var/run/cmonit/cmonit.db "
SELECT host_id, COUNT(*) as records,
  datetime(MAX(timestamp), 'unixepoch') as last_record
FROM host_availability
GROUP BY host_id
ORDER BY host_id"
```

### Stale Hosts Not Being Deleted

Hosts can only be deleted if they've been offline for more than 1 hour.

**Check when host was last seen:**
```bash
sqlite3 /var/run/cmonit/cmonit.db "
SELECT hostname,
  datetime(last_seen) as last_seen,
  CAST((strftime('%s', 'now') - strftime('%s', last_seen)) AS INTEGER) as seconds_offline
FROM hosts
WHERE id = 'your-host-id'"
```

**Delete via API (requires >1 hour offline):**
```bash
curl -X DELETE http://localhost:3000/admin/hosts/your-host-id
```

### Schema Version Mismatch

**Check current schema version:**
```bash
sqlite3 /var/run/cmonit/cmonit.db "SELECT version FROM schema_version ORDER BY version DESC LIMIT 1"
```

**Check schema migration history:**
```bash
sqlite3 /var/run/cmonit/cmonit.db "SELECT version, applied_at FROM schema_version ORDER BY version"
```

**Check migration logs:**
```bash
sudo grep -E "Migrating|schema version" /var/log/daemon.log | tail -20
```

---

## Useful Diagnostic Commands

### Check cmonit Binary Version

```bash
/usr/local/bin/cmonit -h 2>&1 | head -1
```

### Monitor Live Data Collection

```bash
sudo tail -f /var/log/daemon.log | grep -E "Stored status|ERROR|WARN"
```

### Check All Configuration

```bash
cat /usr/local/etc/cmonit.conf
```

### Verify File Permissions

```bash
ls -l /usr/local/bin/cmonit
ls -ld /var/run/cmonit/
ls -l /var/run/cmonit/cmonit.db*
```

### Check Listening Ports

```bash
sockstat -l | grep cmonit
# Or
netstat -an | grep LISTEN | grep -E ':(3000|8080)'
```

---

## Getting Help

If you encounter issues not covered in this guide:

1. Check the logs: `sudo grep cmonit /var/log/daemon.log | tail -100`
2. Verify schema version matches cmonit binary version
3. Check GitHub issues: https://github.com/ocochard/cmonit/issues
4. Review the main documentation: [docs/README.md](README.md)

---

## Common SQL Query Patterns

### Find hosts not reporting for X hours

```bash
sqlite3 /var/run/cmonit/cmonit.db "
SELECT hostname,
  datetime(last_seen) as last_seen,
  CAST((strftime('%s', 'now') - strftime('%s', last_seen)) / 3600.0 AS REAL) as hours_offline
FROM hosts
WHERE (strftime('%s', 'now') - strftime('%s', last_seen)) > 3600
ORDER BY hours_offline DESC"
```

### Find hosts with most services

```bash
sqlite3 /var/run/cmonit/cmonit.db "
SELECT h.hostname, COUNT(s.id) as service_count
FROM hosts h
LEFT JOIN services s ON h.id = s.host_id
GROUP BY h.id
ORDER BY service_count DESC"
```

### Check recent events

```bash
sqlite3 /var/run/cmonit/cmonit.db "
SELECT datetime(created_at) as time, h.hostname, e.service_name, e.message
FROM events e
JOIN hosts h ON e.host_id = h.id
ORDER BY e.created_at DESC
LIMIT 20"
```

---

**Last Updated**: 2026-01-09
**Schema Version**: 12
