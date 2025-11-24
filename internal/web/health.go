package web

import (
	"fmt"
	"time"
)

// HostHealthStatus represents the health status of a host based on heartbeat
type HostHealthStatus string

const (
	HealthStatusGreen  HostHealthStatus = "green"  // Healthy: last_seen < poll_interval * 2
	HealthStatusYellow HostHealthStatus = "yellow" // Warning: poll_interval * 2 <= last_seen < poll_interval * 5
	HealthStatusRed    HostHealthStatus = "red"    // Offline: last_seen >= poll_interval * 5
)

// CalculateHostHealth determines the health status of a host based on its last_seen
// timestamp and poll_interval.
//
// The health status is calculated as follows:
//   - Green (Healthy): last_seen < poll_interval * 2
//   - Yellow (Warning): poll_interval * 2 <= last_seen < poll_interval * 5
//   - Red (Offline): last_seen >= poll_interval * 5
//
// Parameters:
//   - lastSeen: Unix timestamp of when the host was last seen
//   - pollInterval: Monit's check interval in seconds (typically 30)
//
// Returns:
//   - HostHealthStatus: The calculated health status
//   - int64: Seconds since last seen
func CalculateHostHealth(lastSeen int64, pollInterval int) (HostHealthStatus, int64) {
	now := time.Now().Unix()
	secondsSince := now - lastSeen

	threshold2x := int64(pollInterval * 2)
	threshold5x := int64(pollInterval * 5)

	if secondsSince < threshold2x {
		return HealthStatusGreen, secondsSince
	} else if secondsSince < threshold5x {
		return HealthStatusYellow, secondsSince
	}
	return HealthStatusRed, secondsSince
}

// GetHealthColorClass returns a Tailwind CSS color class for the health status
func GetHealthColorClass(status HostHealthStatus) string {
	switch status {
	case HealthStatusGreen:
		return "text-green-600"
	case HealthStatusYellow:
		return "text-yellow-600"
	case HealthStatusRed:
		return "text-red-600"
	default:
		return "text-gray-600"
	}
}

// GetHealthBackgroundClass returns a Tailwind CSS background color class for the health status
func GetHealthBackgroundClass(status HostHealthStatus) string {
	switch status {
	case HealthStatusGreen:
		return "bg-green-100"
	case HealthStatusYellow:
		return "bg-yellow-100"
	case HealthStatusRed:
		return "bg-red-100"
	default:
		return "bg-gray-100"
	}
}

// GetHealthEmoji returns an emoji indicator for the health status
func GetHealthEmoji(status HostHealthStatus) string {
	switch status {
	case HealthStatusGreen:
		return "🟢"
	case HealthStatusYellow:
		return "🟡"
	case HealthStatusRed:
		return "🔴"
	default:
		return "⚪"
	}
}

// GetHealthLabel returns a human-readable label for the health status
func GetHealthLabel(status HostHealthStatus) string {
	switch status {
	case HealthStatusGreen:
		return "Healthy"
	case HealthStatusYellow:
		return "Warning"
	case HealthStatusRed:
		return "Offline"
	default:
		return "Unknown"
	}
}

// FormatTimeSince returns a human-readable string for the time since the given Unix timestamp
func FormatTimeSince(unixTime int64) string {
	now := time.Now().Unix()
	seconds := now - unixTime

	if seconds < 0 {
		return "just now"
	}

	if seconds < 60 {
		return fmt.Sprintf("%d seconds ago", seconds)
	}

	minutes := seconds / 60
	if minutes < 60 {
		if minutes == 1 {
			return "1 minute ago"
		}
		return fmt.Sprintf("%d minutes ago", minutes)
	}

	hours := minutes / 60
	if hours < 24 {
		if hours == 1 {
			return "1 hour ago"
		}
		return fmt.Sprintf("%d hours ago", hours)
	}

	days := hours / 24
	if days == 1 {
		return "1 day ago"
	}
	return fmt.Sprintf("%d days ago", days)
}

// CanDeleteHost returns true if a host can be safely deleted.
// A host can be deleted if it has been offline for more than 1 hour.
func CanDeleteHost(lastSeen int64) bool {
	now := time.Now().Unix()
	secondsSince := now - lastSeen
	oneHour := int64(3600)
	return secondsSince >= oneHour
}
