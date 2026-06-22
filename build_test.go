package main

import "testing"

func TestBuildServicesJoinsAndCounts(t *testing.T) {
	problems := []Problem{
		{EventID: "1", TriggerID: "t1", Name: "CPU high", Severity: 4, Clock: 100, Acknowledged: false, Suppressed: false, Opdata: "load 9.1"},
		{EventID: "2", TriggerID: "t2", Name: "Disk low", Severity: 2, Clock: 150, Acknowledged: true, Suppressed: true, Opdata: ""},
	}
	hosts := map[string]string{"t1": "web01", "t2": "web01"}
	rows := BuildServices(problems, hosts, 1000)

	if len(rows) != 2 {
		t.Fatalf("want 2 rows, got %d", len(rows))
	}
	r0 := rows[0]
	if r0.Hostname != "web01" || r0.Service != "CPU high" || r0.Status != StatusCritical {
		t.Errorf("row0 wrong: %+v", r0)
	}
	if r0.PluginOutput != "load 9.1" || r0.DurationSecs != 900 || r0.HostAlive != 1 {
		t.Errorf("row0 derived fields wrong: %+v", r0)
	}
	if r0.ServicesTotal != 2 {
		t.Errorf("row0 ServicesTotal want 2, got %d", r0.ServicesTotal)
	}
	r1 := rows[1]
	if r1.PluginOutput != "Disk low" { // opdata empty -> falls back to name
		t.Errorf("row1 plugin_output fallback wrong: %q", r1.PluginOutput)
	}
	if r1.Flags&ServiceStateAcknowledged == 0 || r1.Flags&ServiceScheduledDowntime == 0 {
		t.Errorf("row1 flags wrong: %d", r1.Flags)
	}
}

func TestBuildServicesDropsNotClassifiedAndInformation(t *testing.T) {
	problems := []Problem{
		{EventID: "1", TriggerID: "t1", Name: "NotClassified", Severity: 0, Clock: 10},
		{EventID: "2", TriggerID: "t2", Name: "Info", Severity: 1, Clock: 10},
		{EventID: "3", TriggerID: "t3", Name: "Warn", Severity: 2, Clock: 10},
		{EventID: "4", TriggerID: "t4", Name: "Disaster", Severity: 5, Clock: 10},
	}
	hosts := map[string]string{"t1": "h", "t2": "h", "t3": "h", "t4": "h"}
	rows := BuildServices(problems, hosts, 100)

	if len(rows) != 2 {
		t.Fatalf("want 2 rows (severity >= 2), got %d", len(rows))
	}
	for _, r := range rows {
		if r.Service == "NotClassified" || r.Service == "Info" {
			t.Errorf("severity 0/1 row leaked through: %q", r.Service)
		}
	}
	// dropped problems must not inflate services_total either
	if rows[0].ServicesTotal != 2 {
		t.Errorf("services_total should exclude dropped problems, got %d", rows[0].ServicesTotal)
	}
}

func TestBuildServicesMissingHost(t *testing.T) {
	rows := BuildServices([]Problem{{EventID: "1", TriggerID: "x", Name: "n", Severity: 5, Clock: 10}}, map[string]string{}, 20)
	if rows[0].Hostname != "" || rows[0].ServicesTotal != 1 {
		t.Errorf("missing-host handling wrong: %+v", rows[0])
	}
}
