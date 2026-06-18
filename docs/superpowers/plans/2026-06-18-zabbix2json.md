# zabbix2json Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** A single Go binary that serves the nagios2json JSON wire format sourced live from the Zabbix 7.0 API, plus a Nagios `cmd.cgi`-compatible acknowledge endpoint mapped to `event.acknowledge`.

**Architecture:** Pure HTTP proxy. Each `GET /` request calls Zabbix `problem.get` + `trigger.get`, joins them into nagios "service" rows, applies the nagios filter bitmasks, and emits the envelope. `cmd.cgi` resolves host+service to an active event id and calls `event.acknowledge`. The Zabbix `Client` is an interface so handlers are unit-tested with a fake (no live Zabbix).

**Tech Stack:** Go 1.22+, standard library `net/http` + `encoding/json`, one dependency `gopkg.in/yaml.v3`. All code in package `main` at the repo root, one responsibility per file.

## Global Constraints

- Go 1.22 or newer.
- Only external dependency: `gopkg.in/yaml.v3`. Everything else stdlib.
- Output must be byte-compatible with nagios2json: compact JSON (no spaces), exact field **names and order**, `status` and `duration` as strings, `running`/`host_alive` as numbers. **JSON must NOT HTML-escape** `<`, `>`, `&` (use `json.Encoder.SetEscapeHTML(false)`), and must have **no trailing newline**.
- Default `servicestatustypes` = `28`. Default `version` echoed in output = `20110619`.
- Severity map: 5/4 → `CRITICAL`, 3/2 → `WARNING`, 1/0 → `UNKNOWN`. `OK`/`PENDING` never produced.
- TDD: every behavioral change is preceded by a failing test. Commit after each task.
- All files live in package `main` at repo root; tests are `package main` in `*_test.go` alongside.

---

### Task 1: Project scaffold + config loader

**Files:**
- Create: `go.mod`
- Create: `config.go`
- Test: `config_test.go`

**Interfaces:**
- Consumes: nothing.
- Produces:
  - `type Config struct { URL, Token, Listen string; Timeout time.Duration; Version uint32 }`
  - `func Load(path string) (*Config, error)` — reads YAML, applies defaults (`Listen=":8080"`, `Timeout=10s`, `Version=20110619`), then env overrides (`ZABBIX_URL`, `ZABBIX_TOKEN`, `LISTEN_ADDR`, `ZABBIX_TIMEOUT`). Empty `path` ⇒ defaults + env only.

- [ ] **Step 1: Initialize the module and dependency**

```bash
cd /home/balazs/PycharmProjects/zabbix2json
go mod init zabbix2json
go get gopkg.in/yaml.v3
```
Expected: `go.mod` and `go.sum` created, `go.mod` shows `go 1.22` (or newer) and the yaml requirement.

- [ ] **Step 2: Write the failing test**

`config_test.go`:
```go
package main

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestLoadAppliesDefaults(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "c.yaml")
	if err := os.WriteFile(path, []byte("url: https://zbx/api_jsonrpc.php\ntoken: abc\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.URL != "https://zbx/api_jsonrpc.php" || cfg.Token != "abc" {
		t.Fatalf("bad url/token: %+v", cfg)
	}
	if cfg.Listen != ":8080" || cfg.Timeout != 10*time.Second || cfg.Version != 20110619 {
		t.Fatalf("defaults not applied: %+v", cfg)
	}
}

func TestLoadParsesTimeoutAndEnvOverride(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "c.yaml")
	body := "url: https://a\ntoken: t\nlisten: \":9000\"\ntimeout: 3s\nversion: 42\n"
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("ZABBIX_URL", "https://override")
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.URL != "https://override" {
		t.Fatalf("env override failed: %q", cfg.URL)
	}
	if cfg.Listen != ":9000" || cfg.Timeout != 3*time.Second || cfg.Version != 42 {
		t.Fatalf("yaml values not parsed: %+v", cfg)
	}
}
```

- [ ] **Step 3: Run test to verify it fails**

Run: `go test ./... -run TestLoad -v`
Expected: FAIL — `undefined: Load` / `undefined: Config`.

- [ ] **Step 4: Write minimal implementation**

`config.go`:
```go
package main

import (
	"fmt"
	"os"
	"time"

	"gopkg.in/yaml.v3"
)

// Config holds runtime settings, resolved from YAML + env.
type Config struct {
	URL     string
	Token   string
	Listen  string
	Timeout time.Duration
	Version uint32
}

type rawConfig struct {
	URL     string `yaml:"url"`
	Token   string `yaml:"token"`
	Listen  string `yaml:"listen"`
	Timeout string `yaml:"timeout"`
	Version uint32 `yaml:"version"`
}

// Load reads YAML config (if path != ""), applies defaults, then env overrides.
func Load(path string) (*Config, error) {
	raw := rawConfig{}
	if path != "" {
		b, err := os.ReadFile(path)
		if err != nil {
			return nil, fmt.Errorf("read config: %w", err)
		}
		if err := yaml.Unmarshal(b, &raw); err != nil {
			return nil, fmt.Errorf("parse config: %w", err)
		}
	}

	cfg := &Config{
		URL:     raw.URL,
		Token:   raw.Token,
		Listen:  raw.Listen,
		Version: raw.Version,
		Timeout: 10 * time.Second,
	}
	if raw.Timeout != "" {
		d, err := time.ParseDuration(raw.Timeout)
		if err != nil {
			return nil, fmt.Errorf("parse timeout %q: %w", raw.Timeout, err)
		}
		cfg.Timeout = d
	}
	if cfg.Listen == "" {
		cfg.Listen = ":8080"
	}
	if cfg.Version == 0 {
		cfg.Version = 20110619
	}

	if v := os.Getenv("ZABBIX_URL"); v != "" {
		cfg.URL = v
	}
	if v := os.Getenv("ZABBIX_TOKEN"); v != "" {
		cfg.Token = v
	}
	if v := os.Getenv("LISTEN_ADDR"); v != "" {
		cfg.Listen = v
	}
	if v := os.Getenv("ZABBIX_TIMEOUT"); v != "" {
		d, err := time.ParseDuration(v)
		if err != nil {
			return nil, fmt.Errorf("parse ZABBIX_TIMEOUT %q: %w", v, err)
		}
		cfg.Timeout = d
	}
	return cfg, nil
}
```

- [ ] **Step 5: Run test to verify it passes**

Run: `go test ./... -run TestLoad -v`
Expected: PASS (both tests).

- [ ] **Step 6: Commit**

```bash
git add go.mod go.sum config.go config_test.go
git commit -m "feat: project scaffold and config loader"
```
(If not a git repo yet, run `git init` first, then this commit.)

---

### Task 2: Model constants + pure mapping functions

**Files:**
- Create: `model.go`
- Create: `mapping.go`
- Test: `mapping_test.go`

**Interfaces:**
- Consumes: nothing.
- Produces:
  - status bit consts: `StatusPending=1, StatusOK=2, StatusWarning=4, StatusUnknown=8, StatusCritical=16`
  - service flag consts: `ServiceScheduledDowntime=1, ServiceNoScheduledDowntime=2, ServiceStateAcknowledged=4, ServiceStateUnacknowledged=8, ServiceChecksEnabled=32, ServiceIsNotFlapping=2048, ServiceNotificationsEnabled=8192, ServiceActiveCheck=131072, ServiceHardState=262144`
  - host flag consts: `HostScheduledDowntime=1, HostNoScheduledDowntime=2, HostChecksEnabled=32, HostNotificationsEnabled=8192, HostActiveCheck=131072, HostHardState=262144`
  - ack action consts: `ZbxActionAcknowledge=2, ZbxActionAddMessage=4, ZbxActionUnack=16`
  - `type Service struct { EventID, Hostname, Service string; Status, Flags, HostFlags int; LastStateChange, DurationSecs int64; PluginOutput string; HostAlive, ServicesTotal, ServicesVisible int }`
  - `func SeverityToStatus(sev int) int`
  - `func StatusName(status int) string`
  - `func FormatDuration(secs int64) string`
  - `func ServiceFlags(acknowledged, suppressed bool) int`
  - `func HostFlags(suppressed bool) int`

- [ ] **Step 1: Write the failing test**

`mapping_test.go`:
```go
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
		0:      "0d 0h 0m 0s",
		59:     "0d 0h 0m 59s",
		61:     "0d 0h 1m 1s",
		3661:   "0d 1h 1m 1s",
		90061:  "1d 1h 1m 1s",
		86400:  "0d 24h 0m 0s", // quirk: 86400 is not > 86400
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
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./... -run 'TestSeverity|TestStatusName|TestFormatDuration|TestServiceFlags|TestHostFlags' -v`
Expected: FAIL — undefined identifiers.

- [ ] **Step 3: Write the constants and Service struct**

`model.go`:
```go
package main

// Nagios service status bits (used by the servicestatustypes filter).
const (
	StatusPending  = 1
	StatusOK       = 2
	StatusWarning  = 4
	StatusUnknown  = 8
	StatusCritical = 16
)

// Nagios service property flag bits (used by the serviceprops filter).
const (
	ServiceScheduledDowntime    = 1
	ServiceNoScheduledDowntime  = 2
	ServiceStateAcknowledged    = 4
	ServiceStateUnacknowledged  = 8
	ServiceChecksEnabled        = 32
	ServiceIsNotFlapping        = 2048
	ServiceNotificationsEnabled = 8192
	ServiceActiveCheck          = 131072
	ServiceHardState            = 262144
)

// Nagios host property flag bits (used by the hostprops filter).
const (
	HostScheduledDowntime   = 1
	HostNoScheduledDowntime  = 2
	HostChecksEnabled        = 32
	HostNotificationsEnabled = 8192
	HostActiveCheck          = 131072
	HostHardState            = 262144
)

// Zabbix event.acknowledge action bits.
const (
	ZbxActionAcknowledge = 2
	ZbxActionAddMessage  = 4
	ZbxActionUnack       = 16
)

// Service is one nagios "host+service" row, built from a Zabbix problem.
type Service struct {
	EventID         string
	Hostname        string
	Service         string
	Status          int
	Flags           int
	HostFlags       int
	LastStateChange int64
	DurationSecs    int64
	PluginOutput    string
	HostAlive       int
	ServicesTotal   int
	ServicesVisible int
}
```

- [ ] **Step 4: Write the mapping functions**

`mapping.go`:
```go
package main

import "fmt"

// SeverityToStatus maps a Zabbix severity (0-5) to a nagios status bit.
func SeverityToStatus(sev int) int {
	switch {
	case sev >= 4:
		return StatusCritical
	case sev >= 2:
		return StatusWarning
	default:
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
```

- [ ] **Step 5: Run test to verify it passes**

Run: `go test ./... -run 'TestSeverity|TestStatusName|TestFormatDuration|TestServiceFlags|TestHostFlags' -v`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add model.go mapping.go mapping_test.go
git commit -m "feat: model constants and pure mapping functions"
```

---

### Task 3: Join problems + hostnames into Service rows

**Files:**
- Create: `zabbix_types.go`
- Modify: `mapping.go` (add `BuildServices`)
- Test: `build_test.go`

**Interfaces:**
- Consumes: `Service`, `ServiceFlags`, `HostFlags`, `SeverityToStatus`.
- Produces:
  - `type Problem struct { EventID, TriggerID, Name string; Severity int; Clock int64; Acknowledged, Suppressed bool; Opdata string }`
  - `func BuildServices(problems []Problem, hostByTrigger map[string]string, now int64) []Service` — joins each problem to its host, sets `PluginOutput` = `Opdata` (or `Name` if empty), `HostAlive`=1, `DurationSecs`=`now-Clock`, and per-host `ServicesTotal` (count of all problems for that host).

- [ ] **Step 1: Write the failing test**

`build_test.go`:
```go
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

func TestBuildServicesMissingHost(t *testing.T) {
	rows := BuildServices([]Problem{{EventID: "1", TriggerID: "x", Name: "n", Severity: 5, Clock: 10}}, map[string]string{}, 20)
	if rows[0].Hostname != "" || rows[0].ServicesTotal != 1 {
		t.Errorf("missing-host handling wrong: %+v", rows[0])
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./... -run TestBuildServices -v`
Expected: FAIL — `undefined: Problem` / `undefined: BuildServices`.

- [ ] **Step 3: Define the Problem type**

`zabbix_types.go`:
```go
package main

// Problem is a normalized Zabbix active problem.
type Problem struct {
	EventID      string
	TriggerID    string
	Name         string
	Severity     int
	Clock        int64
	Acknowledged bool
	Suppressed   bool
	Opdata       string
}
```

- [ ] **Step 4: Add BuildServices to mapping.go**

Append to `mapping.go`:
```go
// BuildServices joins problems to their hostnames and produces nagios rows.
func BuildServices(problems []Problem, hostByTrigger map[string]string, now int64) []Service {
	totals := make(map[string]int, len(problems))
	rows := make([]Service, 0, len(problems))
	for _, p := range problems {
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
			HostFlags:       HostFlags(p.Suppressed),
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
```

- [ ] **Step 5: Run test to verify it passes**

Run: `go test ./... -run TestBuildServices -v`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add zabbix_types.go mapping.go build_test.go
git commit -m "feat: join problems and hostnames into service rows"
```

---

### Task 4: Filter + visible/total counts

**Files:**
- Create: `filter.go`
- Test: `filter_test.go`

**Interfaces:**
- Consumes: `Service`.
- Produces:
  - `func Filter(services []Service, statusTypes, serviceProps, hostProps int) []Service` — drops a row if any active filter excludes it (nagios semantics: `serviceProps`/`hostProps` require **all** bits present; `statusTypes` requires **any** status bit). Sets `ServicesVisible` per host across survivors. A zero filter value disables that filter.

- [ ] **Step 1: Write the failing test**

`filter_test.go`:
```go
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
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./... -run TestFilter -v`
Expected: FAIL — `undefined: Filter`.

- [ ] **Step 3: Write the implementation**

`filter.go`:
```go
package main

// Filter applies the nagios servicestatustypes/serviceprops/hostprops bitmasks
// and populates ServicesVisible (per host) across the surviving rows.
func Filter(services []Service, statusTypes, serviceProps, hostProps int) []Service {
	survivors := make([]Service, 0, len(services))
	for _, s := range services {
		if serviceProps != 0 && s.Flags&serviceProps != serviceProps {
			continue
		}
		if statusTypes != 0 && s.Status&statusTypes == 0 {
			continue
		}
		if hostProps != 0 && s.HostFlags&hostProps != hostProps {
			continue
		}
		survivors = append(survivors, s)
	}
	visible := make(map[string]int, len(survivors))
	for _, s := range survivors {
		visible[s.Hostname]++
	}
	for i := range survivors {
		survivors[i].ServicesVisible = visible[survivors[i].Hostname]
	}
	return survivors
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./... -run TestFilter -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add filter.go filter_test.go
git commit -m "feat: nagios filter bitmasks and visible counts"
```

---

### Task 5: Output envelope (JSON / JSONP / plaintext)

**Files:**
- Create: `output.go`
- Test: `output_test.go`

**Interfaces:**
- Consumes: `Service`, `StatusName`, `FormatDuration`.
- Produces:
  - `type Envelope struct { Version uint32; Running int; ServerTime, Created int64; LocalTime [2]int64; Data []Service }`
  - `func WriteJSON(w io.Writer, env Envelope, callback string) error` — compact, no HTML escaping, no trailing newline, exact field order; wraps in `callback(...)` if `callback != ""`.
  - `func WritePlaintext(w io.Writer, services []Service) error`

- [ ] **Step 1: Write the failing test**

`output_test.go`:
```go
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
	if strings.Contains(out, `<`) || strings.Contains(out, `&`) {
		t.Errorf("HTML escaping must be disabled: %s", out)
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
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./... -run 'TestWriteJSON|TestWritePlaintext' -v`
Expected: FAIL — `undefined: WriteJSON` / `undefined: Envelope`.

- [ ] **Step 3: Write the implementation**

`output.go`:
```go
package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
)

// Envelope is the top-level nagios2json response.
type Envelope struct {
	Version    uint32
	Running    int
	ServerTime int64
	Created    int64
	LocalTime  [2]int64
	Data       []Service
}

// jsonRow mirrors the nagios2json row field order exactly.
type jsonRow struct {
	LastStateChange int64  `json:"last_state_change"`
	PluginOutput    string `json:"plugin_output"`
	Status          string `json:"status"`
	Flags           int    `json:"flags"`
	Hostname        string `json:"hostname"`
	Service         string `json:"service"`
	HostAlive       int    `json:"host_alive"`
	ServicesTotal   int    `json:"services_total"`
	ServicesVisible int    `json:"services_visible"`
	Duration        string `json:"duration"`
}

type jsonEnvelope struct {
	Version    uint32    `json:"version"`
	Running    int       `json:"running"`
	ServerTime int64     `json:"servertime"`
	LocalTime  [2]int64  `json:"localtime"`
	Created    int64     `json:"created"`
	Data       []jsonRow `json:"data"`
}

// WriteJSON emits the envelope as compact JSON (optionally JSONP-wrapped),
// matching nagios2json: no HTML escaping, no trailing newline.
func WriteJSON(w io.Writer, env Envelope, callback string) error {
	rows := make([]jsonRow, 0, len(env.Data))
	for _, s := range env.Data {
		rows = append(rows, jsonRow{
			LastStateChange: s.LastStateChange,
			PluginOutput:    s.PluginOutput,
			Status:          StatusName(s.Status),
			Flags:           s.Flags,
			Hostname:        s.Hostname,
			Service:         s.Service,
			HostAlive:       s.HostAlive,
			ServicesTotal:   s.ServicesTotal,
			ServicesVisible: s.ServicesVisible,
			Duration:        FormatDuration(s.DurationSecs),
		})
	}
	payload := jsonEnvelope{
		Version: env.Version, Running: env.Running, ServerTime: env.ServerTime,
		LocalTime: env.LocalTime, Created: env.Created, Data: rows,
	}

	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(payload); err != nil {
		return err
	}
	body := bytes.TrimRight(buf.Bytes(), "\n")

	if callback != "" {
		if _, err := fmt.Fprintf(w, "%s(", callback); err != nil {
			return err
		}
	}
	if _, err := w.Write(body); err != nil {
		return err
	}
	if callback != "" {
		if _, err := io.WriteString(w, ")"); err != nil {
			return err
		}
	}
	return nil
}

// WritePlaintext renders the nagios console table format.
func WritePlaintext(w io.Writer, services []Service) error {
	for _, s := range services {
		_, err := fmt.Fprintf(w, "%-25s | %-20s | %-8s | %-16s | \"%s\"\n",
			s.Hostname, s.Service, StatusName(s.Status), FormatDuration(s.DurationSecs), s.PluginOutput)
		if err != nil {
			return err
		}
	}
	return nil
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./... -run 'TestWriteJSON|TestWritePlaintext' -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add output.go output_test.go
git commit -m "feat: nagios2json output envelope, JSONP and plaintext"
```

---

### Task 6: Zabbix JSON-RPC client

**Files:**
- Create: `zabbix.go`
- Test: `zabbix_test.go`

**Interfaces:**
- Consumes: `Problem`.
- Produces:
  - `type Client interface { Problems(ctx context.Context) ([]Problem, error); Hostnames(ctx context.Context, triggerIDs []string) (map[string]string, error); Acknowledge(ctx context.Context, eventIDs []string, action int, message string) error }`
  - `type HTTPClient struct { ... }` implementing `Client`
  - `func NewHTTPClient(url, token string, timeout time.Duration) *HTTPClient`

- [ ] **Step 1: Write the failing test**

`zabbix_test.go`:
```go
package main

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

type rpcReq struct {
	Method string                 `json:"method"`
	Params map[string]interface{} `json:"params"`
}

func TestHTTPClientProblems(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer tok" {
			t.Errorf("auth header: %q", got)
		}
		body, _ := io.ReadAll(r.Body)
		var req rpcReq
		json.Unmarshal(body, &req)
		if req.Method != "problem.get" {
			t.Errorf("method: %q", req.Method)
		}
		io.WriteString(w, `{"jsonrpc":"2.0","id":1,"result":[
			{"eventid":"55","objectid":"t9","name":"CPU","severity":"4","clock":"100","acknowledged":"1","suppressed":"0","opdata":"9.1"}
		]}`)
	}))
	defer srv.Close()

	c := NewHTTPClient(srv.URL, "tok", 5*time.Second)
	probs, err := c.Problems(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(probs) != 1 {
		t.Fatalf("want 1 problem, got %d", len(probs))
	}
	p := probs[0]
	if p.EventID != "55" || p.TriggerID != "t9" || p.Severity != 4 || p.Clock != 100 {
		t.Errorf("parsed wrong: %+v", p)
	}
	if !p.Acknowledged || p.Suppressed || p.Opdata != "9.1" {
		t.Errorf("bool/opdata parse wrong: %+v", p)
	}
}

func TestHTTPClientHostnames(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, `{"jsonrpc":"2.0","id":1,"result":[
			{"triggerid":"t9","hosts":[{"name":"web01"}]}
		]}`)
	}))
	defer srv.Close()

	c := NewHTTPClient(srv.URL, "tok", 5*time.Second)
	m, err := c.Hostnames(context.Background(), []string{"t9"})
	if err != nil {
		t.Fatal(err)
	}
	if m["t9"] != "web01" {
		t.Errorf("hostname map wrong: %+v", m)
	}
}

func TestHTTPClientAcknowledgeSendsAction(t *testing.T) {
	var captured rpcReq
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		json.Unmarshal(body, &captured)
		io.WriteString(w, `{"jsonrpc":"2.0","id":1,"result":{"eventids":["55"]}}`)
	}))
	defer srv.Close()

	c := NewHTTPClient(srv.URL, "tok", 5*time.Second)
	if err := c.Acknowledge(context.Background(), []string{"55"}, ZbxActionAcknowledge|ZbxActionAddMessage, "fixing"); err != nil {
		t.Fatal(err)
	}
	if captured.Method != "event.acknowledge" {
		t.Errorf("method: %q", captured.Method)
	}
	if captured.Params["message"] != "fixing" {
		t.Errorf("message param missing: %+v", captured.Params)
	}
}

func TestHTTPClientRPCError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, `{"jsonrpc":"2.0","id":1,"error":{"code":-32602,"message":"Invalid params","data":"boom"}}`)
	}))
	defer srv.Close()

	c := NewHTTPClient(srv.URL, "tok", 5*time.Second)
	_, err := c.Problems(context.Background())
	if err == nil || !strings.Contains(err.Error(), "boom") {
		t.Fatalf("want rpc error containing data, got %v", err)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./... -run TestHTTPClient -v`
Expected: FAIL — `undefined: NewHTTPClient`.

- [ ] **Step 3: Write the implementation**

`zabbix.go`:
```go
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"time"
)

// Client is the Zabbix API surface zabbix2json needs (mockable in tests).
type Client interface {
	Problems(ctx context.Context) ([]Problem, error)
	Hostnames(ctx context.Context, triggerIDs []string) (map[string]string, error)
	Acknowledge(ctx context.Context, eventIDs []string, action int, message string) error
}

// HTTPClient talks to a real Zabbix 7.0 JSON-RPC endpoint with a Bearer token.
type HTTPClient struct {
	url   string
	token string
	hc    *http.Client
}

func NewHTTPClient(url, token string, timeout time.Duration) *HTTPClient {
	return &HTTPClient{url: url, token: token, hc: &http.Client{Timeout: timeout}}
}

type rpcRequest struct {
	JSONRPC string      `json:"jsonrpc"`
	Method  string      `json:"method"`
	Params  interface{} `json:"params"`
	ID      int         `json:"id"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    string `json:"data"`
}

type rpcResponse struct {
	Result json.RawMessage `json:"result"`
	Error  *rpcError       `json:"error"`
}

func (c *HTTPClient) call(ctx context.Context, method string, params interface{}, result interface{}) error {
	reqBody, err := json.Marshal(rpcRequest{JSONRPC: "2.0", Method: method, Params: params, ID: 1})
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.url, bytes.NewReader(reqBody))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.token)

	resp, err := c.hc.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	var rpc rpcResponse
	if err := json.NewDecoder(resp.Body).Decode(&rpc); err != nil {
		return fmt.Errorf("decode %s response: %w", method, err)
	}
	if rpc.Error != nil {
		return fmt.Errorf("zabbix %s error %d: %s (%s)", method, rpc.Error.Code, rpc.Error.Message, rpc.Error.Data)
	}
	if result != nil {
		if err := json.Unmarshal(rpc.Result, result); err != nil {
			return fmt.Errorf("unmarshal %s result: %w", method, err)
		}
	}
	return nil
}

type rawProblem struct {
	EventID      string `json:"eventid"`
	ObjectID     string `json:"objectid"`
	Name         string `json:"name"`
	Severity     string `json:"severity"`
	Clock        string `json:"clock"`
	Acknowledged string `json:"acknowledged"`
	Suppressed   string `json:"suppressed"`
	Opdata       string `json:"opdata"`
}

func atoiSafe(s string) int { n, _ := strconv.Atoi(s); return n }
func atoi64(s string) int64 { n, _ := strconv.ParseInt(s, 10, 64); return n }

func (c *HTTPClient) Problems(ctx context.Context) ([]Problem, error) {
	params := map[string]interface{}{
		"output":    []string{"eventid", "objectid", "name", "severity", "clock", "r_clock", "acknowledged", "suppressed", "opdata"},
		"recent":    false,
		"sortfield": []string{"eventid"},
		"sortorder": "DESC",
	}
	var raw []rawProblem
	if err := c.call(ctx, "problem.get", params, &raw); err != nil {
		return nil, err
	}
	out := make([]Problem, 0, len(raw))
	for _, r := range raw {
		out = append(out, Problem{
			EventID:      r.EventID,
			TriggerID:    r.ObjectID,
			Name:         r.Name,
			Severity:     atoiSafe(r.Severity),
			Clock:        atoi64(r.Clock),
			Acknowledged: r.Acknowledged == "1",
			Suppressed:   r.Suppressed == "1",
			Opdata:       r.Opdata,
		})
	}
	return out, nil
}

type rawTrigger struct {
	TriggerID string `json:"triggerid"`
	Hosts     []struct {
		Name string `json:"name"`
	} `json:"hosts"`
}

func (c *HTTPClient) Hostnames(ctx context.Context, triggerIDs []string) (map[string]string, error) {
	out := make(map[string]string, len(triggerIDs))
	if len(triggerIDs) == 0 {
		return out, nil
	}
	params := map[string]interface{}{
		"output":      []string{"triggerid"},
		"triggerids":  triggerIDs,
		"selectHosts": []string{"name"},
	}
	var raw []rawTrigger
	if err := c.call(ctx, "trigger.get", params, &raw); err != nil {
		return nil, err
	}
	for _, t := range raw {
		if len(t.Hosts) > 0 {
			out[t.TriggerID] = t.Hosts[0].Name
		}
	}
	return out, nil
}

func (c *HTTPClient) Acknowledge(ctx context.Context, eventIDs []string, action int, message string) error {
	params := map[string]interface{}{
		"eventids": eventIDs,
		"action":   action,
	}
	if action&ZbxActionAddMessage != 0 && message != "" {
		params["message"] = message
	}
	return c.call(ctx, "event.acknowledge", params, nil)
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./... -run TestHTTPClient -v`
Expected: PASS (all four).

- [ ] **Step 5: Commit**

```bash
git add zabbix.go zabbix_test.go
git commit -m "feat: Zabbix 7.0 JSON-RPC client"
```

---

### Task 7: Status handler (`GET /`)

**Files:**
- Create: `handler.go`
- Test: `handler_test.go`

**Interfaces:**
- Consumes: `Client`, `Config`, `BuildServices`, `Filter`, `WriteJSON`, `WritePlaintext`, `Envelope`.
- Produces:
  - `type Server struct { client Client; cfg *Config; now func() time.Time }`
  - `func NewServer(client Client, cfg *Config) *Server`
  - `func (s *Server) handleStatus(w http.ResponseWriter, r *http.Request)`
  - `func parseIntParam(values url.Values, key string, def int) int` (helper)

- [ ] **Step 1: Write the failing test**

`handler_test.go`:
```go
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
	return f.hosts, nil
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
			{EventID: "2", TriggerID: "t2", Name: "Info", Severity: 1, Clock: 100}, // UNKNOWN, still in default 28
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
	if env.Running != 1 || len(env.Data) != 2 {
		t.Fatalf("want running=1, 2 rows; got %+v", env)
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

func TestHandleStatusJSONP(t *testing.T) {
	c := &fakeClient{hosts: map[string]string{}}
	req := httptest.NewRequest(http.MethodGet, "/?callback=cb", nil)
	rec := httptest.NewRecorder()
	testServer(c).handleStatus(rec, req)
	if !strings.HasPrefix(rec.Body.String(), "cb(") {
		t.Errorf("want JSONP wrap: %s", rec.Body.String())
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./... -run TestHandleStatus -v`
Expected: FAIL — `undefined: NewServer`.

- [ ] **Step 3: Write the implementation**

`handler.go`:
```go
package main

import (
	"net/http"
	"net/url"
	"strconv"
	"time"
)

// Server holds dependencies for the HTTP handlers.
type Server struct {
	client Client
	cfg    *Config
	now    func() time.Time
}

func NewServer(client Client, cfg *Config) *Server {
	return &Server{client: client, cfg: cfg, now: time.Now}
}

func parseIntParam(values url.Values, key string, def int) int {
	raw := values.Get(key)
	if raw == "" {
		return def
	}
	n, err := strconv.Atoi(raw)
	if err != nil {
		return def
	}
	return n
}

func (s *Server) handleStatus(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	statusTypes := parseIntParam(q, "servicestatustypes", 28)
	serviceProps := parseIntParam(q, "serviceprops", 0)
	hostProps := parseIntParam(q, "hostprops", 0)
	callback := q.Get("callback")
	jsonMode := parseIntParam(q, "json", 1)

	now := s.now()
	_, off := now.Zone()
	isdst := 0
	if now.IsDST() {
		isdst = 1
	}

	env := Envelope{
		Version:    s.cfg.Version,
		Running:    0,
		ServerTime: now.Unix(),
		Created:    now.Unix(),
		LocalTime:  [2]int64{int64(isdst), int64(off)},
		Data:       []Service{},
	}

	problems, err := s.client.Problems(r.Context())
	if err == nil {
		env.Running = 1
		triggerIDs := make([]string, 0, len(problems))
		for _, p := range problems {
			triggerIDs = append(triggerIDs, p.TriggerID)
		}
		hosts, herr := s.client.Hostnames(r.Context(), triggerIDs)
		if herr != nil {
			hosts = map[string]string{}
		}
		rows := BuildServices(problems, hosts, now.Unix())
		env.Data = Filter(rows, statusTypes, serviceProps, hostProps)
	}

	if jsonMode == 0 {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		_ = WritePlaintext(w, env.Data)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = WriteJSON(w, env, callback)
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./... -run TestHandleStatus -v`
Expected: PASS (all four).

- [ ] **Step 5: Commit**

```bash
git add handler.go handler_test.go
git commit -m "feat: GET / status handler"
```

---

### Task 8: Acknowledge handler (`cmd.cgi`)

**Files:**
- Create: `ack.go`
- Modify: `handler.go` (add `handleCmd` method)
- Test: `ack_test.go`

**Interfaces:**
- Consumes: `Server`, `Client`, `BuildServices`.
- Produces:
  - `func ackActionForCmd(cmdTyp int, hasMessage bool) (action int, ok bool)` — `34/33`→`ZbxActionAcknowledge (|AddMessage if hasMessage)`; `52/51`→`ZbxActionUnack`; else `(0,false)`.
  - `func (s *Server) handleCmd(w http.ResponseWriter, r *http.Request)` — resolves host(+service) to active event ids via a snapshot, calls `Acknowledge`, responds JSON `{"success":bool,"error":string}`.

- [ ] **Step 1: Write the failing test**

`ack_test.go`:
```go
package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestAckActionForCmd(t *testing.T) {
	if a, ok := ackActionForCmd(34, true); !ok || a != (ZbxActionAcknowledge|ZbxActionAddMessage) {
		t.Errorf("svc ack w/ message wrong: %d %v", a, ok)
	}
	if a, ok := ackActionForCmd(33, false); !ok || a != ZbxActionAcknowledge {
		t.Errorf("host ack wrong: %d %v", a, ok)
	}
	if a, ok := ackActionForCmd(52, false); !ok || a != ZbxActionUnack {
		t.Errorf("remove ack wrong: %d %v", a, ok)
	}
	if _, ok := ackActionForCmd(99, false); ok {
		t.Error("unknown cmd_typ should not be ok")
	}
}

func TestHandleCmdResolvesAndAcks(t *testing.T) {
	c := &fakeClient{
		problems: []Problem{
			{EventID: "55", TriggerID: "t1", Name: "PING", Severity: 5, Clock: 100},
			{EventID: "56", TriggerID: "t2", Name: "DISK", Severity: 5, Clock: 100},
		},
		hosts: map[string]string{"t1": "web01", "t2": "web01"},
	}
	form := "cmd_typ=34&host=web01&service=PING&com_data=fixing&com_author=me"
	req := httptest.NewRequest(http.MethodPost, "/cmd.cgi", strings.NewReader(form))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()

	testServer(c).handleCmd(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status %d body %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"success":true`) {
		t.Errorf("want success: %s", rec.Body.String())
	}
	if len(c.ackCalls) != 1 {
		t.Fatalf("want 1 ack call, got %d", len(c.ackCalls))
	}
	call := c.ackCalls[0]
	if len(call.eventIDs) != 1 || call.eventIDs[0] != "55" {
		t.Errorf("wrong eventids: %+v", call.eventIDs)
	}
	if call.action != (ZbxActionAcknowledge|ZbxActionAddMessage) || call.message != "fixing" {
		t.Errorf("wrong action/message: %+v", call)
	}
}

func TestHandleCmdNoMatchFails(t *testing.T) {
	c := &fakeClient{problems: []Problem{}, hosts: map[string]string{}}
	form := "cmd_typ=34&host=nope&service=PING&com_data=x"
	req := httptest.NewRequest(http.MethodPost, "/cmd.cgi", strings.NewReader(form))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()

	testServer(c).handleCmd(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Errorf("want 404, got %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), `"success":false`) {
		t.Errorf("want failure body: %s", rec.Body.String())
	}
	if len(c.ackCalls) != 0 {
		t.Errorf("should not have called Acknowledge")
	}
}

func TestHandleCmdBadCmdTyp(t *testing.T) {
	c := &fakeClient{}
	form := "cmd_typ=99&host=h&service=s"
	req := httptest.NewRequest(http.MethodPost, "/cmd.cgi", strings.NewReader(form))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()

	testServer(c).handleCmd(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("want 400, got %d", rec.Code)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./... -run 'TestAckActionForCmd|TestHandleCmd' -v`
Expected: FAIL — `undefined: ackActionForCmd` / `undefined: handleCmd`.

- [ ] **Step 3: Write the implementation**

`ack.go`:
```go
package main

import (
	"encoding/json"
	"net/http"
	"strconv"
)

// ackActionForCmd maps a nagios cmd_typ to a Zabbix event.acknowledge action.
func ackActionForCmd(cmdTyp int, hasMessage bool) (int, bool) {
	switch cmdTyp {
	case 33, 34: // ACKNOWLEDGE_HOST_PROBLEM / ACKNOWLEDGE_SVC_PROBLEM
		action := ZbxActionAcknowledge
		if hasMessage {
			action |= ZbxActionAddMessage
		}
		return action, true
	case 51, 52: // REMOVE_HOST_ACKNOWLEDGEMENT / REMOVE_SVC_ACKNOWLEDGEMENT
		return ZbxActionUnack, true
	default:
		return 0, false
	}
}

type ackResponse struct {
	Success bool   `json:"success"`
	Error   string `json:"error,omitempty"`
}

func writeAck(w http.ResponseWriter, code int, resp ackResponse) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(resp)
}

func (s *Server) handleCmd(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		writeAck(w, http.StatusBadRequest, ackResponse{false, "bad form"})
		return
	}
	cmdTyp, _ := strconv.Atoi(r.Form.Get("cmd_typ"))
	host := r.Form.Get("host")
	service := r.Form.Get("service")
	message := r.Form.Get("com_data")

	action, ok := ackActionForCmd(cmdTyp, message != "")
	if !ok {
		writeAck(w, http.StatusBadRequest, ackResponse{false, "unsupported cmd_typ"})
		return
	}

	problems, err := s.client.Problems(r.Context())
	if err != nil {
		writeAck(w, http.StatusBadGateway, ackResponse{false, "zabbix unavailable"})
		return
	}
	triggerIDs := make([]string, 0, len(problems))
	for _, p := range problems {
		triggerIDs = append(triggerIDs, p.TriggerID)
	}
	hosts, _ := s.client.Hostnames(r.Context(), triggerIDs)
	rows := BuildServices(problems, hosts, 0)

	var eventIDs []string
	for _, row := range rows {
		if row.Hostname != host {
			continue
		}
		// host-level commands (33/51) match any service on the host; svc commands match the name.
		if (cmdTyp == 34 || cmdTyp == 52) && row.Service != service {
			continue
		}
		eventIDs = append(eventIDs, row.EventID)
	}
	if len(eventIDs) == 0 {
		writeAck(w, http.StatusNotFound, ackResponse{false, "no matching active problem"})
		return
	}

	if err := s.client.Acknowledge(r.Context(), eventIDs, action, message); err != nil {
		writeAck(w, http.StatusBadGateway, ackResponse{false, err.Error()})
		return
	}
	writeAck(w, http.StatusOK, ackResponse{Success: true})
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./... -run 'TestAckActionForCmd|TestHandleCmd' -v`
Expected: PASS (all four).

- [ ] **Step 5: Commit**

```bash
git add ack.go ack_test.go
git commit -m "feat: cmd.cgi acknowledge handler"
```

---

### Task 9: main wiring + routes + README

**Files:**
- Create: `main.go`
- Create: `README.md`
- Test: (full build + `go vet` + manual smoke)

**Interfaces:**
- Consumes: `Load`, `NewHTTPClient`, `NewServer`, handler methods.
- Produces: runnable binary; routes `GET /` → `handleStatus`, `GET,POST /cmd.cgi` → `handleCmd`.

- [ ] **Step 1: Write main.go**

`main.go`:
```go
package main

import (
	"flag"
	"log"
	"net/http"
)

func main() {
	configPath := flag.String("config", "/etc/zabbix2json.yaml", "path to YAML config")
	flag.Parse()

	cfg, err := Load(*configPath)
	if err != nil {
		log.Fatalf("config: %v", err)
	}
	if cfg.URL == "" || cfg.Token == "" {
		log.Fatal("config: url and token are required (set in YAML or ZABBIX_URL/ZABBIX_TOKEN)")
	}

	client := NewHTTPClient(cfg.URL, cfg.Token, cfg.Timeout)
	srv := NewServer(client, cfg)

	mux := http.NewServeMux()
	mux.HandleFunc("/cmd.cgi", srv.handleCmd)
	mux.HandleFunc("/", srv.handleStatus)

	log.Printf("zabbix2json listening on %s -> %s", cfg.Listen, cfg.URL)
	if err := http.ListenAndServe(cfg.Listen, mux); err != nil {
		log.Fatalf("server: %v", err)
	}
}
```

- [ ] **Step 2: Build and vet**

Run:
```bash
go build ./...
go vet ./...
go test ./...
```
Expected: build succeeds, vet clean, all tests PASS.

- [ ] **Step 3: Manual smoke test (local, no real Zabbix needed for the 404 path)**

Run:
```bash
ZABBIX_URL=https://example/api_jsonrpc.php ZABBIX_TOKEN=dummy LISTEN_ADDR=:8088 \
  ./zabbix2json -config /dev/null &
sleep 1
curl -s 'http://127.0.0.1:8088/?servicestatustypes=28'
kill %1
```
Expected: a JSON envelope with `"running":0` and `"data":[]` (Zabbix unreachable → graceful degrade), confirming the wire format and server wiring. With a real `ZABBIX_URL`/`ZABBIX_TOKEN` it returns live problems.

- [ ] **Step 4: Write README.md**

`README.md` (document: purpose, build `go build -o zabbix2json`, config file example from the spec, the `GET /` params table, the `cmd.cgi` ack params + cmd_typ table, the severity mapping, and the documented limitations — OK/PENDING rows not produced; `sticky`/`persistent`/`send_notification` ignored; `host_alive` always 1).

- [ ] **Step 5: Commit**

```bash
git add main.go README.md
git commit -m "feat: main wiring, routes, and README"
```

---

## Self-Review

**Spec coverage:**
- §2 decisions → Tasks 1–8 (config T1, severity map T2, problem.get/trigger.get T6, pure proxy T7, cmd.cgi T8, token config T1). ✓
- §5 status output + params + envelope → T5, T7. ✓
- §6 field/severity/flags/duration mapping → T2, T3. ✓
- §7 ack flow + cmd_typ table → T8. ✓
- §8 error handling (running:0 degrade; ack returns error) → T7 (`TestHandleStatusZabbixDownDegrades`), T8 (`TestHandleCmdNoMatchFails`). ✓
- §9 testing → tests in every task. ✓
- §10 YAGNI exclusions → honored (no cache, no include-fields, no host-only rows). ✓

**Placeholder scan:** No TBD/TODO; all code is complete. README in T9 step 4 is described by exact content list (acceptable — it is prose documentation, not code). ✓

**Type consistency:** `Client` interface methods (`Problems`/`Hostnames`/`Acknowledge`) identical across T6, T7, T8. `Service`/`Problem`/`Envelope` fields consistent across tasks. `ackActionForCmd`, `BuildServices`, `Filter`, `WriteJSON` signatures match their consumers. `fakeClient` (T7) implements the same interface used in T8. ✓
