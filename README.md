zabbix2json
===========

[![build](https://github.com/balazstoplak/zabbix2json/actions/workflows/build.yml/badge.svg)](https://github.com/balazstoplak/zabbix2json/actions/workflows/build.yml)

Serves the [nagios2json](https://github.com/macskas/nagios2json) JSON wire
format, but sourced live from the **Zabbix 7.0 API** instead of a Nagios
`status.dat` file. Drop it behind your existing Nagios aggregator front-end and
point that front-end at Zabbix without changing the front-end.

It is a single static Go binary acting as a **pure proxy**: every request queries
the Zabbix API directly (no cache, no background poller).

Build
-----
```sh
go build -o zabbix2json
```
Single binary, one dependency (`gopkg.in/yaml.v3`).

### Prebuilt binaries

Each version tag (`v*`) publishes prebuilt Linux binaries to
[Releases](https://github.com/balazstoplak/zabbix2json/releases)
(`zabbix2json-linux-amd64`, `zabbix2json-linux-arm64`, plus `sha256sums.txt`):

```sh
curl -fsSLO https://github.com/balazstoplak/zabbix2json/releases/latest/download/zabbix2json-linux-amd64
chmod +x zabbix2json-linux-amd64
```

Configuration
-------------
Pass `-config /path/to/zabbix2json.yaml` (default `/etc/zabbix2json.yaml`):

```yaml
url:     https://zbx.example/api_jsonrpc.php   # Zabbix 7.0 JSON-RPC endpoint
token:   <api-token>                           # Zabbix API token (Bearer auth)
listen:  ":8080"                               # HTTP listen address
timeout: 10s                                   # Zabbix HTTP client timeout
version: 20110619                              # value echoed in the "version" field
```

Environment variables override the file: `ZABBIX_URL`, `ZABBIX_TOKEN`,
`LISTEN_ADDR`, `ZABBIX_TIMEOUT`. `url` and `token` are required.

Zabbix 7.0 setup (user, role, permissions, token)
--------------------------------------------------
zabbix2json authenticates with a single **API token** bound to a dedicated
Zabbix user. That user needs three things: **read** access to the host groups
you want to surface, an API-enabled **role**, and — for the acknowledge feature —
the role's acknowledge/comment actions. Everything below is done as a *Super
admin*.

The methods used are `problem.get`, `trigger.get`, and `event.acknowledge`. None
of them require *write* access: zabbix2json only acknowledges, adds a comment, and
unacknowledges (it never closes problems or changes severity), so **read**
permission on the host groups is sufficient.

### 1. Create a role (`Users → Roles → Create role`)

- **User type:** `User` is enough.
- **Access to API:** check **Enabled**. Optionally set **API methods** to an
  *allow list* and restrict it to exactly:
  `problem.get`, `trigger.get`, `event.acknowledge`.
- **Access to actions** (only needed for the `cmd.cgi` ack endpoint): enable
  **Acknowledge problems** and **Add problem comments**. Leave *Close problems*,
  *Change severity*, and *Suppress problems* off — they are not used.

Name it e.g. `zabbix2json role`.

### 2. Create a user group with read permission (`Users → User groups → Create user group`)

Host-group permissions in Zabbix are granted through **user groups**, not roles.

- Name it e.g. `zabbix2json`.
- On the **Host permissions** tab, add the host group(s) you want exposed with
  permission level **Read** (use a parent group with *Include subgroups* to cover
  everything).

### 3. Create the user (`Users → Users → Create user`)

- **Username:** e.g. `zabbix2json`.
- **Groups:** the `zabbix2json` user group from step 2.
- **Role** (Permissions tab): the `zabbix2json role` from step 1.
- A password is required by the form but is unused — the service authenticates
  only with the token.

### 4. Create the API token (`Users → API tokens → Create API token`)

- **Name:** e.g. `zabbix2json`.
- **User:** select the `zabbix2json` user.
- **Set expiration date and time:** uncheck for a non-expiring token (or set one
  and rotate it).
- **Enabled:** checked.
- Press **Add**, then **copy the _Auth token_ value immediately** — Zabbix shows
  it only once and it cannot be retrieved later.

Put that value in `token:` (or `ZABBIX_TOKEN`) and set `url:` to your
`…/api_jsonrpc.php` endpoint.

### Verify

```sh
curl -s -H 'Content-Type: application/json' \
  -d '{"jsonrpc":"2.0","method":"problem.get","params":{"limit":1},"id":1}' \
  -H "Authorization: Bearer <token>" \
  https://zbx.example/api_jsonrpc.php
```
A JSON-RPC `result` array confirms the token and permissions work; an `error`
about access usually means the user group lacks read permission on the host
group, or the role has API disabled.

Status endpoint — `GET /`
-------------------------
Returns the nagios2json envelope. Each row in `data[]` is **one active Zabbix
problem** mapped to a nagios host+service.

| Param | Default | Meaning |
|-------|---------|---------|
| `servicestatustypes` | `28` | bitmask: PENDING=1, OK=2, WARNING=4, UNKNOWN=8, CRITICAL=16 (default 28 = WARNING\|UNKNOWN\|CRITICAL) |
| `serviceprops` | `0` | service flags bitmask; a row must contain **all** set bits (e.g. `4`=acknowledged, `8`=unacknowledged, `1`=in downtime) |
| `hostprops` | `0` | host flags bitmask; the row's host must contain all set bits |
| `callback` | — | JSONP function name (wraps the output) |
| `json` | `1` | `1` = JSON, `0` = plaintext console table |

Example output:
```json
{"version":20110619,"running":1,"servertime":1781786663,"localtime":[1,7200],"created":1781786663,
 "data":[{"last_state_change":1781780000,"plugin_output":"load 9.1","status":"CRITICAL","flags":270376,
 "hostname":"web01","service":"CPU high","host_alive":1,"services_total":2,"services_visible":1,
 "duration":"0d 1h 54m 23s"}]}
```

`running` is `1` when the Zabbix API responded, `0` when it was unreachable (in
which case `data` is empty and the response is still HTTP 200, so the front-end
degrades gracefully like it would with a down Nagios instance).

Severity mapping
----------------
| Zabbix severity | nagios status |
|-----------------|---------------|
| 5 Disaster, 4 High | `CRITICAL` |
| 3 Average, 2 Warning | `WARNING` |
| 1 Information, 0 Not classified | `UNKNOWN` |

Acknowledge endpoint — `cmd.cgi`
--------------------------------
Accepts the Nagios `cmd.cgi` parameters and translates them into Zabbix
`event.acknowledge`. `GET` or `POST` (form-encoded):

| Param | Meaning |
|-------|---------|
| `cmd_typ` | `34` ack service / `33` ack host / `52` remove service ack / `51` remove host ack |
| `host` | host (visible) name |
| `service` | trigger name (required for `34`/`52`; ignored for host-level `33`/`51`) |
| `com_data` | acknowledge comment → added as a Zabbix message |
| `com_author` | author (informational; Zabbix attributes to the token's user) |

The handler resolves `host`+`service` to the active problem's event id, then
calls `event.acknowledge` (action `2`=acknowledge, `+4`=add message when
`com_data` is set, `16`=unacknowledge for the remove commands). Responds with
`{"success":true}` or `{"success":false,"error":"..."}`.

```sh
curl -d 'cmd_typ=34&host=web01&service=CPU high&com_data=investigating' \
  http://127.0.0.1:8080/cmd.cgi
```

Limitations (by design)
-----------------------
- **Active problems only.** Rows come from Zabbix `problem.get`, so `OK` and
  `PENDING` rows are never produced — requesting `servicestatustypes=2` returns
  nothing.
- **`host_alive` is always `1`.** Zabbix 7.0 has no single host ping-state
  (availability is per-interface).
- The Nagios `sticky`, `persistent`, and `send_notification` ack parameters have
  no Zabbix equivalent and are ignored.
