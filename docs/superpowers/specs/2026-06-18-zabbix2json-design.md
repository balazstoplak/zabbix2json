# zabbix2json — Design Spec

**Date:** 2026-06-18
**Status:** Approved (design phase)

## 1. Goal

Provide an HTTP API that emits output **byte-compatible with the
[nagios2json](../../../../nagios2json) wire format**, but sourced live from the
**Zabbix 7.0 JSON-RPC API** instead of a Nagios `status.dat` file.

Two capabilities:

1. **Status output** — a `GET /` endpoint returning the nagios2json JSON
   envelope, with the same query-parameter filters.
2. **Acknowledge** — a Nagios `cmd.cgi`-compatible endpoint that translates
   acknowledge/unacknowledge commands into Zabbix `event.acknowledge` calls.

Design priorities, in order: **drop-in output compatibility**, **simplicity**,
**speed**.

## 2. Key decisions (locked)

| Decision | Choice | Rationale |
|----------|--------|-----------|
| Row mapping | One row per **active Zabbix problem** (`problem.get`) | Matches the aggregator/dashboard use case |
| List scope | **Active problems only** | OK/PENDING rows are never producible; accepted trade-off |
| Severity map | Disaster/High→CRITICAL, Average/Warning→WARNING, Info/NotClassified→UNKNOWN | Matches ops intuition |
| Language | **Go** | Single static binary, fast stdlib HTTP+JSON, easy deploy |
| Serving model | **Pure proxy** — every request queries Zabbix live, no cache | Simplest; always live |
| Ack interface | **Nagios `cmd.cgi`-compatible** | Frontend needs zero changes |
| Auth/config | **Zabbix API token (Bearer), from a YAML config file** | Native to 7.0, no login/logout round-trips |

## 3. Architecture

```
                         ┌────────────────────────────┐
 frontend  ── GET / ───▶ │  zabbix2json (Go daemon)   │ ── problem.get ──▶  Zabbix
 (aggregator)            │  • parse nagios filters    │ ── trigger.get ─▶   7.0 API
           ◀─ JSON ───── │  • map → nagios model      │                    (Bearer
                         │  • emit nagios2json JSON   │ ◀── results ──────  token)
 frontend ─ POST cmd.cgi▶│  • ack → event.acknowledge │ ── event.ack ───▶
                         └────────────────────────────┘
```

Single Go binary. Pure proxy: no background poller, no cache.

### Config file (`-config /etc/zabbix2json.yaml`)

```yaml
url:     https://zbx.example/api_jsonrpc.php
token:   <api-token>          # Zabbix 7.0 Bearer token
listen:  ":8080"
timeout: 10s                  # Zabbix HTTP client timeout
version: 20110619             # value echoed in the "version" output field
```

Env vars override file values; flags override env. (`ZABBIX_URL`,
`ZABBIX_TOKEN`, `LISTEN_ADDR`, `ZABBIX_TIMEOUT`.)

## 4. Components (Go files)

| File | Responsibility |
|------|----------------|
| `main.go` | flag/config bootstrap, wire dependencies, start `http.Server`, register routes |
| `config.go` | YAML config struct, loader, env/flag overrides |
| `zabbix.go` | JSON-RPC client. `Client` **interface** with `Problems()`, `ResolveTriggers()`, `Acknowledge()` + concrete HTTP impl (mockable for tests) |
| `model.go` | nagios constants (status + flags bitmasks), `Service` struct |
| `mapping.go` | severity→status, problem→flags, `duration` formatting, snapshot assembly |
| `filter.go` | apply `servicestatustypes`/`serviceprops`/`hostprops` bitmasks; per-host `services_total`/`services_visible` counts |
| `output.go` | JSON envelope, JSONP `callback` wrap, plaintext (`json=0`) |
| `handler.go` | `GET /` status handler, `cmd.cgi` ack handler, query parsing |

## 5. Status output — `GET /`

### 5.1 Request parameters (compatible with nagios2json)

| Param | Default | Meaning |
|-------|---------|---------|
| `servicestatustypes` | `28` | bitmask: PENDING=1, OK=2, WARNING=4, UNKNOWN=8, CRITICAL=16 |
| `serviceprops` | `0` | service flags bitmask (see §6.3); row must contain **all** bits |
| `hostprops` | `0` | host flags bitmask; row's host must contain all bits |
| `callback` | — | JSONP function name (wraps output) |
| `json` | `1` | `1` = JSON, `0` = plaintext console table |

### 5.2 Data flow

1. Parse query params.
2. `problem.get` → active problems. Output fields:
   `eventid, objectid (triggerid), name, severity, clock, r_clock,
   acknowledged, suppressed, opdata`. (`recent=false`, only unresolved.)
3. `trigger.get` with the batched `triggerids` and `selectHosts` →
   host `name` + `maintenance_status` for each problem.
4. Build one `data[]` row per problem (§6).
5. Compute per-host `services_total` (problems for that host in the snapshot)
   and `services_visible` (those passing the filter), matching nagios semantics.
6. Apply filter bitmasks (§6.3), then emit the envelope (§5.3).

Two Zabbix API calls per request (problem.get + one batched trigger.get).

### 5.3 Output envelope

```json
{
  "version": 20110619,
  "running": 1,
  "servertime": 1409075862,
  "localtime": [<tm_isdst>, <tm_gmtoff_seconds>],
  "created": 1409075855,
  "data": [ { <row> }, ... ]
}
```

- `running`: `1` if the Zabbix API responded, else `0`.
- `servertime`: current unix time.
- `created`: snapshot time (= now, since pure proxy).
- `localtime`: `[is_dst, gmt_offset_seconds]` of the server's local time.
- `version`: from config (default `20110619` for drop-in compat).
- JSONP: if `callback` set, output is wrapped `FN(<envelope>)`.

## 6. Field & severity mapping

### 6.1 Severity → status

| Zabbix severity | Nagios status | bit |
|-----------------|---------------|-----|
| 5 Disaster, 4 High | `CRITICAL` | 16 |
| 3 Average, 2 Warning | `WARNING` | 4 |
| 1 Information, 0 Not classified | `UNKNOWN` | 8 |

(`OK`=2 and `PENDING`=1 are never produced — see §2 list scope.)

### 6.2 Row fields

| Field | Source |
|-------|--------|
| `last_state_change` | problem `clock` (unix) |
| `plugin_output` | problem `opdata` if non-empty, else problem `name` |
| `status` | severity map (§6.1) |
| `flags` | bitmask (§6.3) |
| `hostname` | trigger's host `name` |
| `service` | problem `name` (resolved trigger name) |
| `host_alive` | `1` always (Zabbix 7.0 has no single host ping-state; simplification) |
| `services_total` | count of problems for this host in snapshot |
| `services_visible` | count of this host's problems passing the filter |
| `duration` | `now − clock`, formatted `"%dd %dh %dm %ds"` |

### 6.3 Flags bitmask (service)

Derived where Zabbix provides the info; remaining bits defaulted to
"enabled/healthy" so they never accidentally exclude a row under
`serviceprops` filtering.

| Condition | Flag (bit) |
|-----------|------------|
| `acknowledged == 1` | `SERVICE_STATE_ACKNOWLEDGED` (4) |
| `acknowledged == 0` | `SERVICE_STATE_UNACKNOWLEDGED` (8) |
| `suppressed == 1` (host in maintenance) | `SERVICE_SCHEDULED_DOWNTIME` (1) |
| `suppressed == 0` | `SERVICE_NO_SCHEDULED_DOWNTIME` (2) |
| always | `SERVICE_HARD_STATE` (262144) |
| always | `SERVICE_CHECKS_ENABLED` (32) |
| always | `SERVICE_NOTIFICATIONS_ENABLED` (8192) |
| always | `SERVICE_IS_NOT_FLAPPING` (2048) |
| always | `SERVICE_ACTIVE_CHECK` (131072) |

This makes the practically-useful filters work: ACK (`serviceprops=4`),
UNACK (`8`), in-downtime (`1`).

`hostprops` filtering uses a host-flags value built from the same
maintenance signal (`HOST_SCHEDULED_DOWNTIME`/`HOST_NO_SCHEDULED_DOWNTIME`)
plus always-on healthy defaults.

### 6.4 Duration format

Replicate nagios `format_date`: break the second-diff into days/hours/minutes/
seconds and render `"%dd %dh %dm %ds"` (note nagios uses `>` not `>=` at each
boundary — replicate exactly).

## 7. Acknowledge — `cmd.cgi`

Accept `GET` or `POST` with Nagios params:
`cmd_typ`, `host`, `service`, `com_author`, `com_data`, `sticky`,
`persistent`, `send_notification`.

| `cmd_typ` | Meaning | Zabbix action |
|-----------|---------|---------------|
| 34 | ACKNOWLEDGE_SVC_PROBLEM | resolve eventid; `event.acknowledge(action = 2 \| (4 if com_data), message=com_data)` |
| 33 | ACKNOWLEDGE_HOST_PROBLEM | same, host-level problem resolution |
| 52 | REMOVE_SVC_ACKNOWLEDGEMENT | `event.acknowledge(action = 16)` (unacknowledge) |
| 51 | REMOVE_HOST_ACKNOWLEDGEMENT | same |

**Event resolution:** `problem.get` filtered by host + trigger name (`service`)
to find the active problem's `eventid`, then `event.acknowledge(eventids=[id], ...)`.

`sticky`, `persistent`, `send_notification` have no clean Zabbix equivalent and
are **ignored** (documented behavior).

**Response:** JSON `{"success":true}` on success, `{"success":false,"error":"..."}`
with HTTP 4xx/5xx on failure.

## 8. Error handling

- Zabbix JSON-RPC / transport errors are logged.
- **Status path** degrades gracefully: on Zabbix failure, emit envelope with
  `running:0` and empty `data`, HTTP 200 (frontend treats it like a down
  Nagios instance).
- **Ack path** returns the error to the caller (no silent success).

## 9. Testing (TDD)

Table-driven unit tests:

- severity → status mapping
- flags bitmask assembly (ack/unack, suppressed/not)
- `duration` formatting (boundary cases matching nagios `format_date`)
- filter logic for `servicestatustypes` / `serviceprops` / `hostprops`
- `services_total` / `services_visible` counting
- `cmd.cgi` param → `event.acknowledge` translation (action bitmask per `cmd_typ`)

The Zabbix `Client` is an **interface**; handlers are tested with a fake client
and `net/http/httptest` — no live Zabbix required.

## 10. Out of scope (YAGNI)

- Background poller / in-memory cache (pure proxy chosen).
- `include-fields` extra-field support (may add later).
- Standalone host-only rows (nagios has none; rows are services).
- OK/PENDING rows (not producible from `problem.get`).
- `sticky`/`persistent`/`send_notification` semantics.
