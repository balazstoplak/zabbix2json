package main

import "testing"

func TestSeverityToStatus(t *testing.T) {
	cases := map[int]int{
		5: StatusCritical, 4: StatusCritical,
		3: StatusWarning, 2: StatusWarning,
		1: StatusUnknown, 0: StatusUnknown,
		99: StatusUnknown,
	}
	for sev, want := range cases {
		if got := SeverityToStatus(sev); got != want {
			t.Errorf("sev %d: got %d want %d", sev, got, want)
		}
	}
}

func TestStatusName(t *testing.T) {
	cases := map[int]string{
		StatusOK: "OK", StatusWarning: "WARNING", StatusCritical: "CRITICAL",
		StatusUnknown: "UNKNOWN", StatusPending: "PENDING", 0: "-",
	}
	for st, want := range cases {
		if got := StatusName(st); got != want {
			t.Errorf("status %d: got %q want %q", st, got, want)
		}
	}
}

func TestFormatDuration(t *testing.T) {
	// Replicates nagios format_date: '>' (not '>=') boundaries.
	cases := map[int64]string{
		0:     "0d 0h 0m 0s",
		59:    "0d 0h 0m 59s",
		61:    "0d 0h 1m 1s",
		3661:  "0d 1h 1m 1s",
		90061: "1d 1h 1m 1s",
		86400: "0d 24h 0m 0s", // quirk: 86400 is not > 86400
	}
	for secs, want := range cases {
		if got := FormatDuration(secs); got != want {
			t.Errorf("secs %d: got %q want %q", secs, got, want)
		}
	}
}

func TestServiceFlags(t *testing.T) {
	base := ServiceChecksEnabled | ServiceNotificationsEnabled | ServiceIsNotFlapping | ServiceActiveCheck | ServiceHardState
	ackDown := ServiceFlags(true, true)
	if ackDown&ServiceStateAcknowledged == 0 || ackDown&ServiceScheduledDowntime == 0 {
		t.Errorf("ack+downtime bits missing: %d", ackDown)
	}
	if ackDown&base != base {
		t.Errorf("healthy defaults missing: %d", ackDown)
	}
	unackUp := ServiceFlags(false, false)
	if unackUp&ServiceStateUnacknowledged == 0 || unackUp&ServiceNoScheduledDowntime == 0 {
		t.Errorf("unack+no-downtime bits missing: %d", unackUp)
	}
	if unackUp&ServiceStateAcknowledged != 0 || unackUp&ServiceScheduledDowntime != 0 {
		t.Errorf("unexpected ack/downtime bits: %d", unackUp)
	}
}

func TestHostFlags(t *testing.T) {
	if HostFlags(true)&HostScheduledDowntime == 0 {
		t.Error("expected HostScheduledDowntime when suppressed")
	}
	if HostFlags(false)&HostNoScheduledDowntime == 0 {
		t.Error("expected HostNoScheduledDowntime when not suppressed")
	}
}
