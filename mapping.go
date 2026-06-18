package main

import "fmt"

// SeverityToStatus maps a Zabbix severity (0-5) to a nagios status bit.
// Valid Zabbix severities are 0-5; anything outside that range maps to UNKNOWN.
func SeverityToStatus(sev int) int {
	switch sev {
	case 4, 5: // High, Disaster
		return StatusCritical
	case 2, 3: // Warning, Average
		return StatusWarning
	default: // 0 Not classified, 1 Information, and out-of-range
		return StatusUnknown
	}
}

// StatusName renders a status bit as its nagios label.
func StatusName(status int) string {
	switch {
	case status&StatusOK != 0:
		return "OK"
	case status&StatusWarning != 0:
		return "WARNING"
	case status&StatusCritical != 0:
		return "CRITICAL"
	case status&StatusUnknown != 0:
		return "UNKNOWN"
	case status&StatusPending != 0:
		return "PENDING"
	default:
		return "-"
	}
}

// FormatDuration replicates nagios format_date, including its '>' boundaries.
func FormatDuration(secs int64) string {
	var day, hour, min int64
	if secs > 86400 {
		day = secs / 86400
		secs %= 86400
	}
	if secs > 3600 {
		hour = secs / 3600
		secs %= 3600
	}
	if secs > 60 {
		min = secs / 60
		secs %= 60
	}
	return fmt.Sprintf("%dd %dh %dm %ds", day, hour, min, secs)
}

func healthyServiceDefaults() int {
	return ServiceChecksEnabled | ServiceNotificationsEnabled |
		ServiceIsNotFlapping | ServiceActiveCheck | ServiceHardState
}

// ServiceFlags builds the nagios service flags bitmask from Zabbix signals.
func ServiceFlags(acknowledged, suppressed bool) int {
	flags := healthyServiceDefaults()
	if acknowledged {
		flags |= ServiceStateAcknowledged
	} else {
		flags |= ServiceStateUnacknowledged
	}
	if suppressed {
		flags |= ServiceScheduledDowntime
	} else {
		flags |= ServiceNoScheduledDowntime
	}
	return flags
}

// HostFlags builds the nagios host flags bitmask from Zabbix signals.
func HostFlags(suppressed bool) int {
	flags := HostChecksEnabled | HostNotificationsEnabled | HostActiveCheck | HostHardState
	if suppressed {
		flags |= HostScheduledDowntime
	} else {
		flags |= HostNoScheduledDowntime
	}
	return flags
}
