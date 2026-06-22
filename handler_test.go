package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

type fakeClient struct {
	problems []Problem
	hosts    map[string]string
	probErr  error
	hostErr  error
	ackCalls []ackCall
}

type ackCall struct {
	eventIDs []string
	action   int
	message  string
}

func (f *fakeClient) Problems(ctx context.Context) ([]Problem, error) {
	return f.problems, f.probErr
}
func (f *fakeClient) Hostnames(ctx context.Context, ids []string) (map[string]string, error) {
	return f.hosts, f.hostErr
}
func (f *fakeClient) Acknowledge(ctx context.Context, ids []string, action int, msg string) error {
	f.ackCalls = append(f.ackCalls, ackCall{ids, action, msg})
	return nil
}

func testServer(c Client) *Server {
	s := NewServer(c, &Config{Version: 20110619})
	s.now = func() time.Time { return time.Unix(1000, 0).UTC() }
	return s
}

func TestHandleStatusDefaultFilter(t *testing.T) {
	c := &fakeClient{
		problems: []Problem{
			{EventID: "1", TriggerID: "t1", Name: "Crit", Severity: 5, Clock: 100},
			{EventID: "2", TriggerID: "t2", Name: "Info", Severity: 1, Clock: 100}, // Information: always excluded
		},
		hosts: map[string]string{"t1": "h1", "t2": "h2"},
	}
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	testServer(c).handleStatus(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status %d", rec.Code)
	}
	var env struct {
		Running int `json:"running"`
		Data    []struct {
			Hostname string `json:"hostname"`
			Status   string `json:"status"`
		} `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
		t.Fatalf("bad json: %v\n%s", err, rec.Body.String())
	}
	// Only the severity-5 problem remains; the Information one is filtered out.
	if env.Running != 1 || len(env.Data) != 1 || env.Data[0].Hostname != "h1" {
		t.Fatalf("want running=1, 1 row (h1); got %+v", env)
	}
}

func TestHandleStatusServicestatustypesFilter(t *testing.T) {
	c := &fakeClient{
		problems: []Problem{
			{EventID: "1", TriggerID: "t1", Name: "Crit", Severity: 5, Clock: 100},
			{EventID: "2", TriggerID: "t2", Name: "Warn", Severity: 2, Clock: 100},
		},
		hosts: map[string]string{"t1": "h1", "t2": "h2"},
	}
	req := httptest.NewRequest(http.MethodGet, "/?servicestatustypes=16", nil) // CRITICAL only
	rec := httptest.NewRecorder()
	testServer(c).handleStatus(rec, req)

	if !strings.Contains(rec.Body.String(), `"hostname":"h1"`) || strings.Contains(rec.Body.String(), `"hostname":"h2"`) {
		t.Errorf("filter wrong: %s", rec.Body.String())
	}
}

func TestHandleStatusZabbixDownDegrades(t *testing.T) {
	c := &fakeClient{probErr: context.DeadlineExceeded}
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	testServer(c).handleStatus(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("want 200 on zabbix down, got %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), `"running":0`) || !strings.Contains(rec.Body.String(), `"data":[]`) {
		t.Errorf("want running:0 + empty data: %s", rec.Body.String())
	}
}

func TestHandleStatusHostnamesErrorDegrades(t *testing.T) {
	c := &fakeClient{
		problems: []Problem{{EventID: "1", TriggerID: "t1", Name: "x", Severity: 5, Clock: 100}},
		hostErr:  context.DeadlineExceeded,
	}
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	testServer(c).handleStatus(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), `"running":0`) || !strings.Contains(rec.Body.String(), `"data":[]`) {
		t.Errorf("want running:0 + empty data on hostnames error: %s", rec.Body.String())
	}
}

func TestHandleStatusJSONP(t *testing.T) {
	c := &fakeClient{hosts: map[string]string{}}
	req := httptest.NewRequest(http.MethodGet, "/?callback=cb", nil)
	rec := httptest.NewRecorder()
	testServer(c).handleStatus(rec, req)
	if !strings.HasPrefix(rec.Body.String(), "cb(") {
		t.Errorf("want JSONP wrap: %s", rec.Body.String())
	}
}
