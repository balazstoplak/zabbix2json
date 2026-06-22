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
// The host ack state mirrors the problem's ack state, so the nagios aggregator's
// standard hostprops filter (e.g. 262154 = HARD|UNACKNOWLEDGED|NO_DOWNTIME) works.
func HostFlags(acknowledged, suppressed bool) int {
	flags := HostChecksEnabled | HostNotificationsEnabled | HostActiveCheck | HostHardState
	if acknowledged {
		flags |= HostStateAcknowledged
	} else {
		flags |= HostStateUnacknowledged
	}
	if suppressed {
		flags |= HostScheduledDowntime
	} else {
		flags |= HostNoScheduledDowntime
	}
	return flags
}

// BuildServices joins problems to their hostnames and produces nagios rows.
func BuildServices(problems []Problem, hostByTrigger map[string]string, now int64) []Service {
	totals := make(map[string]int, len(problems))
	rows := make([]Service, 0, len(problems))
	for _, p := range problems {
		// Not classified (0) and Information (1) severities are always excluded.
		if p.Severity < 2 {
			continue
		}
		host := hostByTrigger[p.TriggerID]
		totals[host]++
		output := p.Opdata
		if output == "" {
			output = p.Name
		}
		rows = append(rows, Service{
			EventID:         p.EventID,
			Hostname:        host,
			Service:         p.Name,
			Status:          SeverityToStatus(p.Severity),
			Flags:           ServiceFlags(p.Acknowledged, p.Suppressed),
			HostFlags:       HostFlags(p.Acknowledged, p.Suppressed),
			LastStateChange: p.Clock,
			DurationSecs:    now - p.Clock,
			PluginOutput:    output,
			HostAlive:       1,
		})
	}
	for i := range rows {
		rows[i].ServicesTotal = totals[rows[i].Hostname]
	}
	return rows
}
