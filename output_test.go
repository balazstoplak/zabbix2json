package main

import (
	"bytes"
	"strings"
	"testing"
)

func TestWriteJSONExactFormat(t *testing.T) {
	env := Envelope{
		Version: 20110619, Running: 1, ServerTime: 1409075862,
		Created: 1409075855, LocalTime: [2]int64{1, 7200},
		Data: []Service{{
			LastStateChange: 1409075855, PluginOutput: `bad <tag> & "q"`,
			Status: StatusCritical, Flags: 1485146, Hostname: "examplehost",
			Service: "PING", HostAlive: 1, ServicesTotal: 10, ServicesVisible: 1,
			DurationSecs: 0,
		}},
	}
	var buf bytes.Buffer
	if err := WriteJSON(&buf, env, ""); err != nil {
		t.Fatal(err)
	}
	out := buf.String()

	if strings.HasSuffix(out, "\n") {
		t.Error("output must not end with newline")
	}
	// HTML escaping must be disabled: < > & must appear literally (asserted via
	// the plugin_output substring below), and never as their \u00xx escaped forms.
	for _, esc := range []string{"\\u003c", "\\u003e", "\\u0026"} {
		if strings.Contains(out, esc) {
			t.Errorf("HTML escaping must be disabled, found escaped %s in: %s", esc, out)
		}
	}
	wantSubstrs := []string{
		`"version":20110619`, `"running":1`, `"servertime":1409075862`,
		`"localtime":[1,7200]`, `"created":1409075855`, `"data":[`,
		`"last_state_change":1409075855`, `"plugin_output":"bad <tag> & \"q\""`,
		`"status":"CRITICAL"`, `"flags":1485146`, `"hostname":"examplehost"`,
		`"service":"PING"`, `"host_alive":1`, `"services_total":10`,
		`"services_visible":1`, `"duration":"0d 0h 0m 0s"`,
	}
	for _, s := range wantSubstrs {
		if !strings.Contains(out, s) {
			t.Errorf("missing %q in:\n%s", s, out)
		}
	}
	// field order check: last_state_change before plugin_output before status...
	if idx(out, "last_state_change") > idx(out, "plugin_output") ||
		idx(out, "plugin_output") > idx(out, "status") ||
		idx(out, "host_alive") > idx(out, "services_total") {
		t.Errorf("field order wrong:\n%s", out)
	}
}

func idx(s, sub string) int { return strings.Index(s, sub) }

func TestWriteJSONCallbackWrap(t *testing.T) {
	var buf bytes.Buffer
	if err := WriteJSON(&buf, Envelope{Version: 1, Data: []Service{}}, "cb"); err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	if !strings.HasPrefix(out, "cb(") || !strings.HasSuffix(out, ")") {
		t.Errorf("JSONP wrap wrong: %s", out)
	}
}

func TestWriteJSONEmptyDataIsArray(t *testing.T) {
	var buf bytes.Buffer
	if err := WriteJSON(&buf, Envelope{Version: 1, Data: nil}, ""); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(buf.String(), `"data":[]`) {
		t.Errorf("empty data must serialize as []: %s", buf.String())
	}
}

func TestWritePlaintext(t *testing.T) {
	var buf bytes.Buffer
	rows := []Service{{Hostname: "h", Service: "PING", Status: StatusCritical, DurationSecs: 61, PluginOutput: "down"}}
	if err := WritePlaintext(&buf, rows); err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	for _, s := range []string{"h", "PING", "CRITICAL", "0d 0h 1m 1s", "down"} {
		if !strings.Contains(out, s) {
			t.Errorf("plaintext missing %q: %s", s, out)
		}
	}
}
