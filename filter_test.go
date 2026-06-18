package main

import "testing"

func sampleRows() []Service {
	return []Service{
		{Hostname: "h1", Service: "a", Status: StatusCritical, Flags: ServiceStateUnacknowledged, HostFlags: HostNoScheduledDowntime, ServicesTotal: 2},
		{Hostname: "h1", Service: "b", Status: StatusWarning, Flags: ServiceStateAcknowledged, HostFlags: HostNoScheduledDowntime, ServicesTotal: 2},
		{Hostname: "h2", Service: "c", Status: StatusUnknown, Flags: ServiceStateUnacknowledged, HostFlags: HostScheduledDowntime, ServicesTotal: 1},
	}
}

func TestFilterStatusTypes(t *testing.T) {
	got := Filter(sampleRows(), StatusCritical|StatusWarning, 0, 0)
	if len(got) != 2 {
		t.Fatalf("want 2 rows, got %d", len(got))
	}
	for _, r := range got {
		if r.Hostname != "h1" {
			t.Errorf("unexpected host %q", r.Hostname)
		}
		if r.ServicesVisible != 2 {
			t.Errorf("ServicesVisible want 2, got %d", r.ServicesVisible)
		}
	}
}

func TestFilterServicePropsRequiresAllBits(t *testing.T) {
	got := Filter(sampleRows(), 0, ServiceStateAcknowledged, 0)
	if len(got) != 1 || got[0].Service != "b" {
		t.Fatalf("want only acknowledged row b, got %+v", got)
	}
}

func TestFilterHostProps(t *testing.T) {
	got := Filter(sampleRows(), 0, 0, HostScheduledDowntime)
	if len(got) != 1 || got[0].Hostname != "h2" {
		t.Fatalf("want only h2, got %+v", got)
	}
	if got[0].ServicesVisible != 1 {
		t.Errorf("ServicesVisible want 1, got %d", got[0].ServicesVisible)
	}
}

func TestFilterZeroIsNoOp(t *testing.T) {
	if got := Filter(sampleRows(), 0, 0, 0); len(got) != 3 {
		t.Fatalf("want 3 rows, got %d", len(got))
	}
}
