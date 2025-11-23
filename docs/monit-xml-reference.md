# Monit XML Protocol Reference

This document provides a complete reference of all XML fields that Monit sends to cmonit, including which fields are currently used and which are available but not yet implemented.

## Document Status

- **Last Updated**: 2025-11-23
- **Monit Version**: 5.35.2
- **cmonit Schema Version**: 2

## Overview

Monit sends status updates via HTTP POST to the `/collector` endpoint in XML format. This document catalogs all available fields in the Monit XML protocol.

## Legend

- ✅ **USED**: Field is currently parsed and stored by cmonit
- ⚠️ **PARSED**: Field is parsed but not stored in database
- ❌ **UNUSED**: Field is available but not currently used
- 📊 **METRICS**: Field is stored as time-series metrics
- 🎛️ **CONTROL**: Field is used for remote control functionality

---

## XML Structure

```xml
<monit>
  <server>...</server>
  <platform>...</platform>
  <service>...</service>  <!-- Multiple services -->
</monit>
```

---

## 1. Server Section (`<server>`)

Information about the Monit agent/daemon itself.

| Field | Status | Type | Description | Storage Location |
|-------|--------|------|-------------|------------------|
| `id` | ✅ USED | string | Unique identifier for this Monit instance (only if idfile configured) | `hosts.id` |
| `incarnation` | ✅ USED | int64 | Unix timestamp when Monit started | `hosts.incarnation` |
| `version` | ✅ USED | string | Monit version (e.g., "5.35.2") | `hosts.version` |
| `uptime` | ⚠️ PARSED | int64 | How long Monit has been running (seconds) | Not stored |
| `poll` | ⚠️ PARSED | int | Check interval in seconds | Not stored |
| `startdelay` | ⚠️ PARSED | int | Delay before first check (seconds) | Not stored |
| `localhostname` | ✅ USED | string | Hostname of the monitored server | `hosts.hostname` |
| `controlfile` | ⚠️ PARSED | string | Path to monitrc configuration file | Not stored |

### HTTP Server Configuration (`<httpd>`)

Information about Monit's built-in HTTP server (for remote control).

| Field | Status | Type | Description | Storage Location |
|-------|--------|------|-------------|------------------|
| `address` | 🎛️ CONTROL | string | IP address Monit listens on | `hosts.http_address` |
| `port` | 🎛️ CONTROL | int | TCP port number (usually 2812) | `hosts.http_port` |
| `ssl` | 🎛️ CONTROL | int | SSL enabled (0=no, 1=yes) | `hosts.http_ssl` |

### Credentials (`<credentials>`)

HTTP Basic Authentication credentials for controlling Monit remotely.

| Field | Status | Type | Description | Storage Location |
|-------|--------|------|-------------|------------------|
| `username` | 🎛️ CONTROL | string | HTTP auth username | `hosts.http_username` |
| `password` | 🎛️ CONTROL | string | HTTP auth password | `hosts.http_password` |

### Unused Server Fields

The following fields are available in the Monit XML but not currently used by cmonit:

- `uptime`: Could be displayed in the UI to show how long Monit has been running
- `poll`: Could be used to show check frequency or estimate next update time
- `startdelay`: Could be used to show initialization status
- `controlfile`: Could be displayed for debugging purposes

---

## 2. Platform Section (`<platform>`)

Operating system and hardware information.

| Field | Status | Type | Description | Storage Location |
|-------|--------|------|-------------|------------------|
| `name` | ✅ USED | string | OS name (FreeBSD, Linux, Darwin, etc.) | `hosts.os_name` |
| `release` | ✅ USED | string | OS version/release | `hosts.os_release` |
| `version` | ✅ USED | string | Full OS version string with kernel info | `hosts.os_version` |
| `machine` | ✅ USED | string | CPU architecture (amd64, arm64, i386) | `hosts.machine` |
| `cpu` | ✅ USED | int | Number of CPU cores | `hosts.cpu_count` |
| `memory` | ✅ USED | int64 | Total RAM in **kilobytes** | `hosts.total_memory` |
| `swap` | ✅ USED | int64 | Total swap space in **kilobytes** | `hosts.total_swap` |

### Platform Information Usage

All platform fields are now stored and displayed in the dashboard:

- **Dashboard display**: OS info, architecture, and hardware specs shown in host header
- **Capacity planning**: Total memory/CPU displayed for each monitored host
- **Architecture awareness**: CPU architecture visible (amd64, arm64, i386)
- **Memory display**: Converted from KB to GB for user-friendly display (e.g., "31.3 GB" RAM, "64.0 GB" swap)

**Note**: Monit sends memory and swap values in **kilobytes**, not bytes. The dashboard converts these to GB by dividing by 1048576 (1024²).

---

## 3. Service Section (`<service>`)

Information about monitored services. Each `<service>` element represents one monitored item.

### Common Service Fields

These fields appear in all service types:

| Field | Status | Type | Description | Storage Location |
|-------|--------|------|-------------|------------------|
| `type` | ✅ USED | int | Service type (see table below) | `services.type` |
| `name` | ✅ USED | string | Service name from monitrc | `services.name` |
| `collected_sec` | ✅ USED | int64 | Unix timestamp when collected | `services.collected_at` |
| `collected_usec` | ⚠️ PARSED | int64 | Microseconds part of timestamp | Not stored |
| `status` | ✅ USED | int | Current status (0=OK, 1=failed, etc.) | `services.status` |
| `status_hint` | ⚠️ PARSED | int | Additional status information | Not stored |
| `monitor` | ✅ USED | int | Monitoring state (0=off, 1=on, 2=init) | `services.monitor` |
| `monitormode` | ⚠️ PARSED | int | Monitoring mode (0=active, 1=passive, 2=manual) | Not stored |
| `onreboot` | ⚠️ PARSED | int | Behavior on reboot (0=start, 1=nostart, 2=laststate) | Not stored |
| `pendingaction` | ⚠️ PARSED | int | Action pending execution | Not stored |

### Service Types

| Type | Name | Description |
|------|------|-------------|
| 0 | Filesystem | Mounted filesystem monitoring |
| 1 | Directory | Directory existence/permissions |
| 2 | File | File monitoring (size, checksum, permissions) |
| 3 | Process | Process/daemon monitoring |
| 4 | Remote Host | Network host ping/connection checks |
| 5 | System | System-wide metrics (CPU, memory, load) |
| 6 | FIFO | Named pipe monitoring |
| 7 | Program | Script/command execution checks |
| 8 | Network | Network interface monitoring |

---

## 4. System Service Fields (Type 5)

System-wide performance metrics.

### System-Level Metrics

| Field | Status | Type | Description | Storage |
|-------|--------|------|-------------|---------|
| `uptime` | ✅ USED | int64 | System uptime in seconds | `hosts.system_uptime` |
| `boottime` | ✅ USED | int64 | Unix timestamp of last boot | `hosts.boottime` |

### File Descriptors (System Level)

| Field | Status | Type | Description | Storage |
|-------|--------|------|-------------|---------|
| `filedescriptors.allocated` | ❌ UNUSED | int | File descriptors currently allocated | - |
| `filedescriptors.unused` | ❌ UNUSED | int | Unused file descriptors | - |
| `filedescriptors.maximum` | ❌ UNUSED | int | Maximum file descriptors allowed | - |

### Load Average (`<load>`)

| Field | Status | Type | Description | Storage |
|-------|--------|------|-------------|---------|
| `avg01` | 📊 METRICS | float64 | 1-minute load average | `metrics` (type=load, name=avg01) |
| `avg05` | 📊 METRICS | float64 | 5-minute load average | `metrics` (type=load, name=avg05) |
| `avg15` | 📊 METRICS | float64 | 15-minute load average | `metrics` (type=load, name=avg15) |

### CPU Usage (`<cpu>`)

| Field | Status | Type | Description | Storage |
|-------|--------|------|-------------|---------|
| `user` | 📊 METRICS | float64 | % time in user mode | `metrics` (type=cpu, name=user) |
| `system` | 📊 METRICS | float64 | % time in kernel mode | `metrics` (type=cpu, name=system) |
| `nice` | ⚠️ PARSED | float64 | % time in low-priority processes | Not stored |
| `hardirq` | ⚠️ PARSED | float64 | % time handling hardware interrupts | Not stored |
| `wait` | ⚠️ PARSED | float64 | % time waiting for I/O | Not stored |

### Memory Usage (`<memory>`)

| Field | Status | Type | Description | Storage |
|-------|--------|------|-------------|---------|
| `percent` | 📊 METRICS | float64 | % of RAM used | `metrics` (type=memory, name=percent) |
| `kilobyte` | ⚠️ PARSED | int64 | RAM used in KB | Not stored |

### Swap Usage (`<swap>`)

| Field | Status | Type | Description | Storage |
|-------|--------|------|-------------|---------|
| `percent` | 📊 METRICS | float64 | % of swap used | `metrics` (type=swap, name=percent) |
| `kilobyte` | 📊 METRICS | int64 | Swap used in KB | `metrics` (type=swap, name=kilobyte) |

### System Metrics Usage

System uptime and boottime are now displayed in the dashboard:

- **System uptime**: Displayed in host header, formatted as "Xd Xh Xm" (e.g., "21d 4h 23m")
- **Boot time**: Shown as formatted timestamp (e.g., "Jan 2, 15:04")
- **Swap monitoring**: Swap percentage and KB metrics stored for alerting on RAM pressure

### Unused System Metrics

Potential use cases for remaining unused fields:

- **File descriptor tracking**: Monitor system-wide FD exhaustion
- **Additional CPU metrics**: Graph nice, wait, hardirq for detailed performance analysis

---

## 5. Process Service Fields (Type 3)

Fields specific to process/daemon monitoring.

### Process Identification

| Field | Status | Type | Description | Storage |
|-------|--------|------|-------------|---------|
| `pid` | ✅ USED | int | Process ID | `services.pid` |
| `ppid` | ⚠️ PARSED | int | Parent process ID | Not stored |
| `uid` | ⚠️ PARSED | int | User ID | Not stored |
| `euid` | ⚠️ PARSED | int | Effective user ID | Not stored |
| `gid` | ⚠️ PARSED | int | Group ID | Not stored |
| `uptime` | ⚠️ PARSED | int64 | Process uptime in seconds | Not stored |
| `threads` | ⚠️ PARSED | int | Number of threads | Not stored |
| `children` | ⚠️ PARSED | int | Number of child processes | Not stored |

### Process Memory (`<memory>`)

| Field | Status | Type | Description | Storage |
|-------|--------|------|-------------|---------|
| `percent` | ✅ USED | float64 | % of system RAM used by process | `services.memory_percent` |
| `percenttotal` | ⚠️ PARSED | float64 | % of RAM used by process + children | Not stored |
| `kilobyte` | ✅ USED | int64 | Memory used in KB | `services.memory_kb` |
| `kilobytetotal` | ⚠️ PARSED | int64 | Memory used by process + children in KB | Not stored |

### Process CPU (`<cpu>`)

| Field | Status | Type | Description | Storage |
|-------|--------|------|-------------|---------|
| `percent` | ✅ USED | float64 | % of CPU used by process | `services.cpu_percent` |
| `percenttotal` | ⚠️ PARSED | float64 | % of CPU used by process + children | Not stored |

### File Descriptors (Process Level)

| Field | Status | Type | Description | Storage |
|-------|--------|------|-------------|---------|
| `filedescriptors.open` | ❌ UNUSED | int | FDs open by this process | - |
| `filedescriptors.opentotal` | ❌ UNUSED | int | FDs open by process + children | - |
| `filedescriptors.limit.soft` | ❌ UNUSED | int | Soft FD limit for process | - |
| `filedescriptors.limit.hard` | ❌ UNUSED | int | Hard FD limit for process | - |

### I/O Operations

#### Read Operations (`<read><operations>`)

| Field | Status | Type | Description | Storage |
|-------|--------|------|-------------|---------|
| `count` | ❌ UNUSED | int64 | Read operations since last check | - |
| `total` | ❌ UNUSED | int64 | Total read operations since process start | - |

#### Write Operations (`<write><operations>`)

| Field | Status | Type | Description | Storage |
|-------|--------|------|-------------|---------|
| `count` | ❌ UNUSED | int64 | Write operations since last check | - |
| `total` | ❌ UNUSED | int64 | Total write operations since process start | - |

### Process Metrics Usage

Process resource monitoring is now displayed in the dashboard:

- **PID display**: Process ID shown in services table for debugging
- **CPU usage**: Displayed as percentage with color coding (red >50%, yellow >20%, gray ≤20%)
- **Memory usage**: Displayed as percentage with same color coding, plus absolute value in MB
- **Dashboard integration**: All process services (Type 3) show resource metrics in real-time

**Example display**: Process "smbd" shows PID 53720, 0.0% CPU, 0.8% memory (262 MB)

### Unused Process Fields

Potential use cases for remaining fields:

- **Process tree**: Display parent/child relationships using ppid and children
- **Thread count**: Monitor applications that spawn many threads
- **File descriptor monitoring**: Alert before FD limits are reached
- **I/O statistics**: Graph read/write operations to identify I/O-heavy processes
- **User identification**: Show which user owns each process (uid/euid)
- **Process uptime**: Track how long processes have been running

---

## 6. File Service Fields (Type 2)

Fields specific to file monitoring.

### File Metadata

| Field | Status | Type | Description | Storage |
|-------|--------|------|-------------|---------|
| `mode` | ⚠️ PARSED | string | Unix permissions (octal, e.g., "644") | Not stored |
| `uid` | ⚠️ PARSED | int | File owner user ID | Not stored |
| `gid` | ⚠️ PARSED | int | File owner group ID | Not stored |
| `size` | ⚠️ PARSED | int64 | File size in bytes | Not stored |
| `hardlink` | ❌ UNUSED | int | Number of hard links to file | - |

### File Timestamps (`<timestamps>`)

| Field | Status | Type | Description | Storage |
|-------|--------|------|-------------|---------|
| `access` | ❌ UNUSED | int64 | Last access time (Unix timestamp) | - |
| `change` | ❌ UNUSED | int64 | Last status change time | - |
| `modify` | ❌ UNUSED | int64 | Last modification time | - |

### File Checksum (`<checksum>`)

| Field | Status | Type | Description | Storage |
|-------|--------|------|-------------|---------|
| `checksum` | ⚠️ PARSED | string | File hash value | Not stored |
| `checksum type` (attr) | ⚠️ PARSED | string | Hash algorithm (MD5, SHA1, SHA256) | Not stored |

### Unused File Fields

Potential use cases:

- **File integrity monitoring**: Store checksums and alert on changes
- **Timestamp tracking**: Monitor when files were last accessed or modified
- **Permission monitoring**: Alert on permission changes
- **Size tracking**: Graph file growth over time
- **Hard link detection**: Identify files with multiple hard links

---

## 7. Program Service Fields (Type 7)

Fields for program/script execution checks.

| Field | Status | Type | Description | Storage |
|-------|--------|------|-------------|---------|
| `program.started` | ⚠️ PARSED | int64 | When program was last executed | Not stored |
| `program.status` | ⚠️ PARSED | int | Exit code (0=success) | Not stored |
| `program.output` | ⚠️ PARSED | string | Program output (stdout) | Not stored |

### Unused Program Fields

Potential use cases:

- **Exit code tracking**: Store exit codes for trend analysis
- **Output logging**: Keep history of script outputs
- **Execution frequency**: Show when checks were last run
- **Alerting**: Trigger alerts based on specific exit codes or output patterns

---

## Summary: Storage Strategy

### Currently Stored

1. **Host Information**: ID, hostname, version, incarnation, HTTP credentials
2. **Platform Information** (NEW): OS name/release/version, CPU architecture, CPU count, total memory, total swap
3. **System Metrics** (NEW): System uptime, boot time
4. **Service Status**: Name, type, status, monitor state, collected timestamp
5. **Process Metrics** (NEW): PID, CPU percentage, memory percentage, memory KB
6. **Time-Series Metrics**: Load averages, CPU usage (user/system), memory usage, swap usage

### Not Stored (But Parsed)

Fields that are parsed into Go structs but not persisted:

- Server operational data (poll interval, startdelay, controlfile path)
- Process parent/child relationships (ppid, children count)
- Process identity (uid, euid, gid)
- Process details (threads, uptime)
- File metadata (permissions, checksums, sizes)
- Program output and exit codes

### Not Used At All

Fields available in XML but not parsed:

- System/process file descriptor statistics
- Process I/O operation counts
- File timestamps (access, change, modify)
- File hard link counts
- Additional CPU metrics (nice, hardirq, wait)

---

## Future Enhancement Opportunities

### Recently Implemented (Schema v2)

1. ✅ **Platform Information Display**: Shows OS, architecture, and hardware specs in dashboard
2. ✅ **Process Resource Monitoring**: Displays per-process CPU/memory usage with color coding
3. ✅ **Swap Monitoring**: Swap metrics stored for alerting on RAM pressure
4. ✅ **System Uptime Display**: Shows system uptime and boot time in dashboard

### High Priority (Next)

5. **File Descriptor Tracking**: Monitor FD usage to prevent exhaustion (system and per-process)
6. **Process Resource Graphing**: Time-series graphs of per-process CPU/memory usage
7. **Swap Usage Alerting**: Active alerts when swap usage exceeds thresholds

### Medium Priority

8. **File Integrity Monitoring**: Track file checksums and alert on changes
9. **Additional CPU Metrics**: Graph wait, nice, hardirq for detailed analysis
10. **Program Output History**: Store script outputs for debugging
11. **Process Tree Display**: Show parent/child relationships and thread counts

### Low Priority

12. **I/O Statistics**: Track read/write operations per process
13. **File Timestamp Monitoring**: Alert on unexpected file access/modification
14. **Process Uptime Tracking**: Monitor how long processes have been running
15. **Hard Link Detection**: Identify files with multiple hard links
16. **User/Group Display**: Show which user/group owns each process

---

## Schema Version History

### Version 1 (Initial Release)

- Initial schema with basic host and service tracking
- Time-series metrics for system load, CPU, and memory
- Remote control credentials storage
- Schema version tracking system

### Version 2 (Current - 2025-11-23)

Added platform information and process metrics:

**Hosts table additions:**
- `os_name TEXT` - Operating system name (FreeBSD, Linux, etc.)
- `os_release TEXT` - OS version/release
- `os_version TEXT` - Full OS version string with kernel info
- `machine TEXT` - CPU architecture (amd64, arm64, i386)
- `cpu_count INTEGER` - Number of CPU cores
- `total_memory INTEGER` - Total RAM in kilobytes
- `total_swap INTEGER` - Total swap space in kilobytes
- `system_uptime INTEGER` - System uptime in seconds
- `boottime INTEGER` - Unix timestamp of last boot

**Services table additions:**
- `pid INTEGER` - Process ID (for Type 3 services)
- `cpu_percent REAL` - CPU usage percentage (for Type 3 services)
- `memory_percent REAL` - Memory usage percentage (for Type 3 services)
- `memory_kb INTEGER` - Memory usage in kilobytes (for Type 3 services)

**Dashboard enhancements:**
- Platform information displayed in host header (OS, CPU, memory, swap, uptime)
- Per-process resource metrics shown in services table
- Color-coded CPU and memory usage (red >50%, yellow >20%, gray ≤20%)
- Memory displayed in both percentage and absolute MB values

### Future Versions

When adding new fields from this reference, increment the schema version and add a migration in `internal/db/schema.go`.

---

## References

- [Monit Manual](https://mmonit.com/monit/documentation/monit.html)
- [Monit XML Format Documentation](https://mmonit.com/monit/documentation/monit.html#XML)
- [cmonit Parser Code](../internal/parser/xml.go)
- [cmonit Database Schema](../internal/db/schema.go)

---

## Contributing

When adding support for additional XML fields:

1. Update the parser structs in `internal/parser/xml.go`
2. Increment `currentSchemaVersion` in `internal/db/schema.go`
3. Add a migration case in `migrateSchema()` function
4. Update database schema constants
5. Update this document to reflect the changes
6. Test migration with existing databases

---

**Document Version**: 1.0
**Author**: cmonit development team
**License**: Same as cmonit project
