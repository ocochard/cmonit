package web

import (
	"database/sql"
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"
)

// HandleStatus serves the main status overview page.
func HandleStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	data, err := getStatusData()
	if err != nil {
		log.Printf("[ERROR] Failed to get status data: %v", err)
		http.Error(w, "Failed to load status data", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")

	err = templates.ExecuteTemplate(w, "status.html", data)
	if err != nil {
		log.Printf("[ERROR] Failed to render template: %v", err)
	}
}

// HandleHostDetail serves the single-host detail page with graphs.
func HandleHostDetail(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Extract host ID from URL path: /host/{host_id}
	path := strings.TrimPrefix(r.URL.Path, "/host/")
	hostID := strings.Split(path, "/")[0]

	if hostID == "" {
		http.Error(w, "Host ID required", http.StatusBadRequest)
		return
	}

	data, err := getHostDetailData(hostID)
	if err != nil {
		log.Printf("[ERROR] Failed to get host detail data for %s: %v", hostID, err)
		http.Error(w, "Failed to load host data", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")

	err = templates.ExecuteTemplate(w, "dashboard.html", data)
	if err != nil {
		log.Printf("[ERROR] Failed to render template: %v", err)
	}
}

// HandleHostEvents serves the events page for a specific host.
func HandleHostEvents(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Extract host ID from URL path: /host/{host_id}/events
	path := strings.TrimPrefix(r.URL.Path, "/host/")
	hostID := strings.Split(path, "/")[0]

	if hostID == "" {
		http.Error(w, "Host ID required", http.StatusBadRequest)
		return
	}

	data, err := getEventsData(hostID)
	if err != nil {
		log.Printf("[ERROR] Failed to get events data for %s: %v", hostID, err)
		http.Error(w, "Failed to load events data", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")

	err = templates.ExecuteTemplate(w, "events.html", data)
	if err != nil {
		log.Printf("[ERROR] Failed to render template: %v", err)
	}
}

// getStatusData queries the database and builds StatusData for the main status page.
func getStatusData() (*StatusData, error) {
	const hostsQuery = `
		SELECT id, hostname, last_seen
		FROM hosts
		ORDER BY last_seen DESC
	`

	rows, err := db.Query(hostsQuery)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var hosts []HostStatus

	for rows.Next() {
		var hostStatus HostStatus

		err := rows.Scan(
			&hostStatus.ID,
			&hostStatus.Hostname,
			&hostStatus.LastSeen,
		)
		if err != nil {
			return nil, err
		}

		// Check if host is stale (not seen in 5+ minutes)
		hostStatus.IsStale = time.Since(hostStatus.LastSeen) > 5*time.Minute

		// Get services for this host to calculate status
		services, err := getServicesForHost(hostStatus.ID)
		if err != nil {
			log.Printf("[ERROR] Failed to get services for host %s: %v", hostStatus.ID, err)
			services = []Service{}
		}

		// Calculate overall host status based on services and staleness
		calculateHostStatus(&hostStatus, services)

		// Get system CPU and memory from the metrics table
		cpuPercent, memPercent := getSystemMetrics(hostStatus.ID, hostStatus.Hostname)
		hostStatus.CPUPercent = cpuPercent
		hostStatus.MemoryPercent = memPercent

		// Get event count for this host
		hostStatus.EventCount, err = getEventCount(hostStatus.ID)
		if err != nil {
			log.Printf("[ERROR] Failed to get event count for host %s: %v", hostStatus.ID, err)
			hostStatus.EventCount = 0
		}

		hosts = append(hosts, hostStatus)
	}

	if err = rows.Err(); err != nil {
		return nil, err
	}

	return &StatusData{
		Hosts:      hosts,
		LastUpdate: time.Now(),
	}, nil
}

// getHostDetailData gets detailed data for a single host (for the detail page).
func getHostDetailData(hostID string) (*DashboardData, error) {
	const hostQuery = `
		SELECT id, hostname, version, os_name, os_release, machine,
		       cpu_count, total_memory, total_swap, system_uptime, boottime, last_seen
		FROM hosts
		WHERE id = ?
	`

	var host HostWithServices

	err := db.QueryRow(hostQuery, hostID).Scan(
		&host.ID,
		&host.Hostname,
		&host.Version,
		&host.OSName,
		&host.OSRelease,
		&host.Machine,
		&host.CPUCount,
		&host.TotalMemory,
		&host.TotalSwap,
		&host.SystemUptime,
		&host.Boottime,
		&host.LastSeen,
	)
	if err != nil {
		return nil, err
	}

	host.IsStale = time.Since(host.LastSeen) > 5*time.Minute

	host.Services, err = getServicesForHost(host.ID)
	if err != nil {
		log.Printf("[ERROR] Failed to get services for host %s: %v", host.ID, err)
		host.Services = []Service{}
	}

	return &DashboardData{
		Hosts:      []HostWithServices{host},
		LastUpdate: time.Now(),
	}, nil
}

// getEventsData gets events for a specific host.
func getEventsData(hostID string) (*EventsData, error) {
	// Get hostname first
	var hostname string
	err := db.QueryRow("SELECT hostname FROM hosts WHERE id = ?", hostID).Scan(&hostname)
	if err != nil {
		return nil, err
	}

	// Query events for this host (most recent first, limit to 100)
	const eventsQuery = `
		SELECT id, service_name, event_type, message, created_at
		FROM events
		WHERE host_id = ?
		ORDER BY created_at DESC
		LIMIT 100
	`

	rows, err := db.Query(eventsQuery, hostID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var events []Event

	for rows.Next() {
		var event Event

		err := rows.Scan(
			&event.ID,
			&event.ServiceName,
			&event.EventType,
			&event.Message,
			&event.CreatedAt,
		)
		if err != nil {
			return nil, err
		}

		event.EventTypeName = getEventTypeName(event.EventType)

		events = append(events, event)
	}

	if err = rows.Err(); err != nil {
		return nil, err
	}

	return &EventsData{
		HostID:     hostID,
		Hostname:   hostname,
		Events:     events,
		LastUpdate: time.Now(),
	}, nil
}

// calculateHostStatus determines the overall status of a host based on its services.
func calculateHostStatus(hostStatus *HostStatus, services []Service) {
	hostStatus.TotalServices = len(services)
	hostStatus.FailedServices = 0

	// Count failed/warning services
	for _, svc := range services {
		if svc.Status != 0 { // Not OK
			hostStatus.FailedServices++
		}
	}

	// Determine status color and description
	if hostStatus.IsStale {
		// Red: Host is stale (no recent report)
		hostStatus.StatusColor = "red"
		hostStatus.StatusName = "Critical"
		hostStatus.StatusDescription = fmt.Sprintf("No report from Monit. Last report was %s",
			hostStatus.LastSeen.Format("02 Jan 2006 15:04:05 MST"))
	} else if hostStatus.FailedServices > 0 {
		// Orange: Some services are down
		hostStatus.StatusColor = "orange"
		hostStatus.StatusName = "Warning"
		availableServices := hostStatus.TotalServices - hostStatus.FailedServices
		hostStatus.StatusDescription = fmt.Sprintf("%d out of %d services are available",
			availableServices, hostStatus.TotalServices)
	} else if hostStatus.TotalServices > 0 {
		// Green: All services are OK
		hostStatus.StatusColor = "green"
		hostStatus.StatusName = "OK"
		if hostStatus.TotalServices == 1 {
			hostStatus.StatusDescription = "Service is available"
		} else {
			hostStatus.StatusDescription = fmt.Sprintf("All %d services are available", hostStatus.TotalServices)
		}
	} else {
		// Gray: No services configured
		hostStatus.StatusColor = "gray"
		hostStatus.StatusName = "Unknown"
		hostStatus.StatusDescription = "No services configured"
	}
}

// getEventCount returns the number of events for a given host.
func getEventCount(hostID string) (int, error) {
	var count int
	err := db.QueryRow("SELECT COUNT(*) FROM events WHERE host_id = ?", hostID).Scan(&count)
	if err != nil {
		return 0, err
	}
	return count, nil
}

// getEventTypeName converts Monit event type number to name.
//
// Monit event types (from monit documentation):
// - 0x01: Checksum
// - 0x02: Resource
// - 0x04: Timeout
// - 0x08: Timestamp
// - 0x10: Size
// - 0x20: Connection
// - 0x40: Permission
// - 0x80: UID
// - 0x100: GID
// - 0x200: Nonexist
// - 0x400: Invalid
// - 0x800: Data
// - 0x1000: Exec
// - 0x2000: Changed
// - 0x4000: Match
// - 0x8000: Action
// - 0x10000: PID
// - 0x20000: PPID
// - 0x40000: Heartbeat
// - 0x80000: Status
// - 0x100000: Icmp
// - 0x200000: Content
// - 0x400000: Instance
// - 0x800000: BytesIn
// - 0x1000000: BytesOut
// - 0x2000000: PacketsIn
// - 0x4000000: PacketsOut
// - 0x8000000: Speed
// - 0x10000000: Saturation
// - 0x20000000: Uptime
func getEventTypeName(eventType int) string {
	switch eventType {
	case 0x01:
		return "Checksum"
	case 0x02:
		return "Resource"
	case 0x04:
		return "Timeout"
	case 0x08:
		return "Timestamp"
	case 0x10:
		return "Size"
	case 0x20:
		return "Connection"
	case 0x40:
		return "Permission"
	case 0x80:
		return "UID"
	case 0x100:
		return "GID"
	case 0x200:
		return "Nonexist"
	case 0x400:
		return "Invalid"
	case 0x800:
		return "Data"
	case 0x1000:
		return "Exec"
	case 0x2000:
		return "Changed"
	case 0x4000:
		return "Match"
	case 0x8000:
		return "Action"
	case 0x10000:
		return "PID"
	case 0x20000:
		return "PPID"
	case 0x40000:
		return "Heartbeat"
	case 0x80000:
		return "Status"
	case 0x100000:
		return "Icmp"
	case 0x200000:
		return "Content"
	case 0x400000:
		return "Instance"
	case 0x800000:
		return "BytesIn"
	case 0x1000000:
		return "BytesOut"
	case 0x2000000:
		return "PacketsIn"
	case 0x4000000:
		return "PacketsOut"
	case 0x8000000:
		return "Speed"
	case 0x10000000:
		return "Saturation"
	case 0x20000000:
		return "Uptime"
	default:
		return fmt.Sprintf("Unknown (0x%X)", eventType)
	}
}

// HandleServiceDetail serves the service detail page showing detailed metrics.
func HandleServiceDetail(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Extract host ID and service name from URL path: /host/{host_id}/service/{service_name}
	path := strings.TrimPrefix(r.URL.Path, "/host/")
	parts := strings.SplitN(path, "/service/", 2)

	if len(parts) != 2 {
		http.Error(w, "Invalid service path", http.StatusBadRequest)
		return
	}

	hostID := parts[0]
	serviceName := parts[1]

	if hostID == "" || serviceName == "" {
		http.Error(w, "Host ID and service name required", http.StatusBadRequest)
		return
	}

	data, err := getServiceDetailData(hostID, serviceName)
	if err != nil {
		log.Printf("[ERROR] Failed to get service detail data for %s/%s: %v", hostID, serviceName, err)
		http.Error(w, "Failed to load service data", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")

	err = templates.ExecuteTemplate(w, "service.html", data)
	if err != nil {
		log.Printf("[ERROR] Failed to render template: %v", err)
	}
}

// getSystemMetrics retrieves the latest CPU and memory metrics for a host.
//
// Returns CPU and memory percentages from the metrics table.
// CPU is calculated as the sum of user + system + nice + wait.
// Memory is retrieved directly from the 'percent' metric.
//
// Returns nil pointers if metrics are not available.
func getSystemMetrics(hostID, hostname string) (*float64, *float64) {
	// Query for the most recent CPU metrics (user, system, nice, wait)
	// We need to sum these to get total CPU usage
	const cpuQuery = `
		SELECT
			SUM(CASE WHEN metric_name = 'user' THEN value ELSE 0 END) +
			SUM(CASE WHEN metric_name = 'system' THEN value ELSE 0 END) +
			SUM(CASE WHEN metric_name = 'nice' THEN value ELSE 0 END) +
			SUM(CASE WHEN metric_name = 'wait' THEN value ELSE 0 END) as total_cpu
		FROM metrics
		WHERE host_id = ? AND service_name = ? AND metric_type = 'cpu'
		AND collected_at = (
			SELECT MAX(collected_at)
			FROM metrics
			WHERE host_id = ? AND metric_type = 'cpu'
		)
	`

	var cpuPercent float64
	err := db.QueryRow(cpuQuery, hostID, hostname, hostID).Scan(&cpuPercent)
	if err != nil {
		// If no CPU data found, return nil
		cpuPercent = 0
	}

	// Query for memory percentage
	const memQuery = `
		SELECT value
		FROM metrics
		WHERE host_id = ? AND service_name = ?
		  AND metric_type = 'memory' AND metric_name = 'percent'
		ORDER BY collected_at DESC
		LIMIT 1
	`

	var memPercent float64
	err = db.QueryRow(memQuery, hostID, hostname).Scan(&memPercent)
	if err != nil {
		// If no memory data found, return nil
		return &cpuPercent, nil
	}

	return &cpuPercent, &memPercent
}

// getServiceDetailData retrieves detailed information about a specific service.
func getServiceDetailData(hostID, serviceName string) (*ServiceDetailData, error) {
	// Query service information
	const serviceQuery = `
		SELECT name, type, status, monitor, pid, cpu_percent, memory_percent, memory_kb, collected_at
		FROM services
		WHERE host_id = ? AND name = ?
		ORDER BY collected_at DESC
		LIMIT 1
	`

	var svc Service
	err := db.QueryRow(serviceQuery, hostID, serviceName).Scan(
		&svc.Name,
		&svc.Type,
		&svc.Status,
		&svc.Monitor,
		&svc.PID,
		&svc.CPUPercent,
		&svc.MemoryPercent,
		&svc.MemoryKB,
		&svc.CollectedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("service not found: %w", err)
	}

	// Convert type and status to human-readable
	svc.TypeName = getServiceTypeName(svc.Type)
	svc.StatusName, svc.StatusColor = getServiceStatusInfo(svc.Status)

	// Get hostname
	var hostname string
	err = db.QueryRow("SELECT hostname FROM hosts WHERE id = ?", hostID).Scan(&hostname)
	if err != nil {
		hostname = hostID // Fallback to host ID if hostname not found
	}

	data := &ServiceDetailData{
		HostID:     hostID,
		Hostname:   hostname,
		Service:    svc,
		LastUpdate: time.Now(),
	}

	// Get filesystem metrics if this is a filesystem service (type 0)
	if svc.Type == 0 {
		data.FilesystemData, err = getFilesystemMetrics(hostID, serviceName)
		if err != nil {
			log.Printf("[WARN] Failed to get filesystem metrics for %s/%s: %v", hostID, serviceName, err)
		}
	}

	// Get process metrics if this is a process service (type 3)
	if svc.Type == 3 {
		data.ProcessData = &ProcessMetrics{
			PID:           *svc.PID,
			CPUPercent:    *svc.CPUPercent,
			MemoryPercent: *svc.MemoryPercent,
			MemoryKB:      *svc.MemoryKB,
		}
	}

	// Get file metrics if this is a file service (type 2)
	if svc.Type == 2 {
		data.FileData, err = getFileMetrics(hostID, serviceName)
		if err != nil {
			log.Printf("[WARN] Failed to get file metrics for %s/%s: %v", hostID, serviceName, err)
		}
	}

	// Get program metrics if this is a program service (type 7)
	if svc.Type == 7 {
		data.ProgramData, err = getProgramMetrics(hostID, serviceName)
		if err != nil {
			log.Printf("[WARN] Failed to get program metrics for %s/%s: %v", hostID, serviceName, err)
		}
	}

	// Get network metrics if this is a network interface service (type 8)
	if svc.Type == 8 {
		data.NetworkData, err = getNetworkMetrics(hostID, serviceName)
		if err != nil {
			log.Printf("[WARN] Failed to get network metrics for %s/%s: %v", hostID, serviceName, err)
		}
	}

	return data, nil
}

// getFilesystemMetrics retrieves the latest filesystem metrics for a service.
func getFilesystemMetrics(hostID, serviceName string) (*FilesystemMetrics, error) {
	const query = `
		SELECT fs_type, fs_flags, mode, uid, gid,
		       block_percent, block_usage_mb, block_total_mb,
		       inode_percent, inode_usage, inode_total,
		       read_bytes_total, read_ops_total,
		       write_bytes_total, write_ops_total
		FROM filesystem_metrics
		WHERE host_id = ? AND service_name = ?
		ORDER BY collected_at DESC
		LIMIT 1
	`

	var fm FilesystemMetrics
	var fsType, fsFlags, mode sql.NullString
	var uid, gid sql.NullInt64

	err := db.QueryRow(query, hostID, serviceName).Scan(
		&fsType,
		&fsFlags,
		&mode,
		&uid,
		&gid,
		&fm.BlockPercent,
		&fm.BlockUsageMB,
		&fm.BlockTotalMB,
		&fm.InodePercent,
		&fm.InodeUsage,
		&fm.InodeTotal,
		&fm.ReadBytesTotal,
		&fm.ReadOpsTotal,
		&fm.WriteBytesTotal,
		&fm.WriteOpsTotal,
	)
	if err != nil {
		return nil, err
	}

	// Convert nullable fields
	if fsType.Valid {
		fm.FSType = fsType.String
	}
	if fsFlags.Valid {
		fm.FSFlags = fsFlags.String
	}
	if mode.Valid {
		fm.Mode = mode.String
	}
	if uid.Valid {
		fm.UID = int(uid.Int64)
	}
	if gid.Valid {
		fm.GID = int(gid.Int64)
	}

	return &fm, nil
}

// getNetworkMetrics retrieves the latest network interface metrics for a service.
func getNetworkMetrics(hostID, serviceName string) (*NetworkMetrics, error) {
	const query = `
		SELECT link_state, link_speed, link_duplex,
		       download_packets_now, download_packets_total,
		       download_bytes_now, download_bytes_total,
		       download_errors_now, download_errors_total,
		       upload_packets_now, upload_packets_total,
		       upload_bytes_now, upload_bytes_total,
		       upload_errors_now, upload_errors_total
		FROM network_metrics
		WHERE host_id = ? AND service_name = ?
		ORDER BY collected_at DESC
		LIMIT 1
	`

	var nm NetworkMetrics
	err := db.QueryRow(query, hostID, serviceName).Scan(
		&nm.LinkState,
		&nm.LinkSpeed,
		&nm.LinkDuplex,
		&nm.DownloadPacketsNow,
		&nm.DownloadPacketsTotal,
		&nm.DownloadBytesNow,
		&nm.DownloadBytesTotal,
		&nm.DownloadErrorsNow,
		&nm.DownloadErrorsTotal,
		&nm.UploadPacketsNow,
		&nm.UploadPacketsTotal,
		&nm.UploadBytesNow,
		&nm.UploadBytesTotal,
		&nm.UploadErrorsNow,
		&nm.UploadErrorsTotal,
	)
	if err != nil {
		return nil, err
	}

	return &nm, nil
}

// getFileMetrics retrieves the latest file metrics for a service.
func getFileMetrics(hostID, serviceName string) (*FileMetrics, error) {
	const query = `
		SELECT mode, uid, gid, size, hardlink,
		       access_time, change_time, modify_time,
		       checksum_type, checksum_value
		FROM file_metrics
		WHERE host_id = ? AND service_name = ?
		ORDER BY collected_at DESC
		LIMIT 1
	`

	var fm FileMetrics
	var mode, checksumType, checksumValue sql.NullString
	var uid, gid, size, hardlink, accessTime, changeTime, modifyTime sql.NullInt64

	err := db.QueryRow(query, hostID, serviceName).Scan(
		&mode,
		&uid,
		&gid,
		&size,
		&hardlink,
		&accessTime,
		&changeTime,
		&modifyTime,
		&checksumType,
		&checksumValue,
	)
	if err != nil {
		return nil, err
	}

	// Convert nullable fields
	if mode.Valid {
		fm.Mode = mode.String
	}
	if uid.Valid {
		fm.UID = int(uid.Int64)
	}
	if gid.Valid {
		fm.GID = int(gid.Int64)
	}
	if size.Valid {
		fm.Size = size.Int64
	}
	if hardlink.Valid {
		fm.Hardlink = int(hardlink.Int64)
	}
	if accessTime.Valid {
		fm.AccessTime = accessTime.Int64
	}
	if changeTime.Valid {
		fm.ChangeTime = changeTime.Int64
	}
	if modifyTime.Valid {
		fm.ModifyTime = modifyTime.Int64
	}
	if checksumType.Valid {
		fm.ChecksumType = checksumType.String
	}
	if checksumValue.Valid {
		fm.ChecksumValue = checksumValue.String
	}

	return &fm, nil
}

// getProgramMetrics retrieves the latest program metrics for a service.
func getProgramMetrics(hostID, serviceName string) (*ProgramMetrics, error) {
	const query = `
		SELECT started, exit_status, output
		FROM program_metrics
		WHERE host_id = ? AND service_name = ?
		ORDER BY collected_at DESC
		LIMIT 1
	`

	var pm ProgramMetrics
	var started, exitStatus sql.NullInt64
	var output sql.NullString

	err := db.QueryRow(query, hostID, serviceName).Scan(
		&started,
		&exitStatus,
		&output,
	)
	if err != nil {
		return nil, err
	}

	// Convert nullable fields
	if started.Valid {
		pm.Started = started.Int64
	}
	if exitStatus.Valid {
		pm.ExitStatus = int(exitStatus.Int64)
	}
	if output.Valid {
		pm.Output = output.String
	}

	return &pm, nil
}
