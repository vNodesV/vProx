# vProx Modules & Operations Guide

This document covers **all modules** in vProx, their architecture, configuration, and production operations.

For build and install instructions, see [`INSTALLATION.md`](./INSTALLATION.md).
For CLI flags, see [`CLI_FLAGS_GUIDE.md`](./CLI_FLAGS_GUIDE.md).

## 1) Core Proxy (main)

**Purpose**: Loads chain configs, routes requests, rewrites links, injects banners, and applies rate limiting.

### Runtime layout (default)

By default, vProx runs out of:

- `$HOME/.vProx/config` — chain configs and `ports.toml`
- `$HOME/.vProx/data/logs` — `main.log`, `rate-limit.jsonl`, `archives/` backups
- `$HOME/.vProx/data` — backup state, geo DBs, `access-counts.json`

Override base path with:

- `VPROX_HOME=/custom/path`
- CLI: `vProx --home /custom/path`

### Chain configs

Chain configs live in `$HOME/.vProx/config/chains/*.toml`. For backward compatibility, configs in `$HOME/.vProx/chains/*.toml` and `$HOME/.vProx/config/*.toml` are also scanned.

**v1.4.0 config layout additions:**
- `config/chains/*.sample` — identity-only sample files (`chain_id`, `network_type`, `tree_name`, `dashboard_name`); no proxy/service fields; used as chain identity templates
- `config/services/nodes/*.toml` — per-node proxy + management config (planned); uses `tree` field as join key to chain identity

**Required fields:**

| Field | Type | Description |
|---|---|---|
| `chain_name` | string | Unique chain identifier (used in logs) |
| `host` | string | Host header vProx matches to route to this chain |
| `ip` | string | Backend node IP address |
| `services.*` | bool | At least one of `rpc`, `rest`, `websocket`, `grpc`, `grpc_web` must be `true` |

**Optional fields:**

| Field | Type | Description |
|---|---|---|
| `default_ports` | bool | `true` (default) — inherit ports from `config/ports.toml` |
| `msg_rpc` | bool | Show `[message].rpc_msg` banner on RPC index page |
| `msg_api` | bool | Show `[message].api_msg` banner on REST/swagger pages |
| `rpc_aliases` | `[]string` | Extra RPC hostnames for vhost routing (e.g. `["rpc-alt.example.com"]`) |
| `rest_aliases` | `[]string` | Extra REST hostnames for vhost routing |
| `api_aliases` | `[]string` | Extra API hostnames for vhost routing |
| `[ports]` | table | Per-service port overrides when `default_ports = false` |
| `[expose]` | table | Routing booleans: `path = true` (prefix routing) and/or `vhost = true` (subdomain routing) |
| `[features]` | table | `rpc_address_masking`, `mask_rpc`, `swagger_masking`, `absolute_links` |
| `[message]` | table | `rpc_msg` and `api_msg` banner strings (shown only when `msg_rpc`/`msg_api` are `true`) |
| `[logging]` | table | `file` — per-chain log filename (relative to `data/logs/`) |
| `[ws]` | table | `idle_timeout_sec`, `max_lifetime_sec` |

**Routing modes** (`[expose]` block — both can be true simultaneously):

- `path = true`: `host/rpc/...` → `ip:26657`, `host/rest/...` → `ip:1317`, etc.
- `vhost = true`: `rpc.<host>` → `ip:26657`, `api.<host>` → `ip:1317`, etc. Requires DNS or a reverse proxy for each subdomain.

A fully annotated example is at [`config/chains/chain.sample.toml`](./config/chains/chain.sample.toml).

> Changes to chain configs require a server restart: `sudo systemctl restart vProx.service`

### Default ports

`$HOME/.vProx/config/ports.toml` defines the default port for each service. Created by `make install`:

```toml
rpc      = 26657
rest     = 1317
grpc     = 9090
grpc_web = 9091
api      = 1317
```

### Run

- `vProx start` — start server foreground (default `:3000`)
- `vProx start -d` — start as daemon (systemd service)
- `vProx stop` — stop the service
- `vProx restart` — restart the service
- `vProx --addr :4000` — override listen address

### Manual backup

- `vProx --new-backup`
- `vProx --new-backup --reset_count` (also accepts `--reset-count`)
- `vProx --list-backup` — list existing archives
- `vProx --backup-status` — show scheduler status

---

## 2) Rate Limiter (`internal/limit`)

**Purpose**: Per‑IP token-bucket rate limiting with optional auto‑quarantine. Emits structured JSONL events to an audit log.

### Algorithm

Each IP gets an independent token bucket: replenished at `VPROX_RPS` tokens/second, with a burst capacity of `VPROX_BURST`. When the bucket is empty, the request is rejected with HTTP 429. Auto-quarantine watches for IPs exceeding `VPROX_AUTO_THRESHOLD` requests in `VPROX_AUTO_WINDOW_SEC` seconds and temporarily applies a stricter limiter.

### Tuning via `.env`

```ini
# Standard rate limiting
VPROX_RPS=25                  # Tokens replenished per second
VPROX_BURST=100               # Max burst size

# Auto-quarantine
VPROX_AUTO_ENABLED=true       # Enable auto-quarantine
VPROX_AUTO_THRESHOLD=120      # Requests in window that triggers quarantine
VPROX_AUTO_WINDOW_SEC=10      # Sliding window size (seconds)
VPROX_AUTO_RPS=1              # Rate during quarantine
VPROX_AUTO_BURST=1            # Burst during quarantine
VPROX_AUTO_TTL_SEC=900        # Quarantine duration (seconds, 0 = permanent)
```

### Log format

JSONL events are written to `$HOME/.vProx/data/logs/rate-limit.jsonl`. Only significant events are logged (429 responses, auto-quarantine add/expire, canceled waits).

**Fields:**

| Field | Description |
|---|---|
| `ts` | ISO 8601 timestamp |
| `ip` | Source IP |
| `country` | ISO country code (geo enrichment) |
| `asn` | Autonomous system number |
| `method` | HTTP method |
| `path` | Request path |
| `host` | Host header |
| `ua` / `user_agent` | User-Agent (both aliases present for compatibility) |
| `reason` / `event` | Event type (both aliases present for compatibility) |
| `rps` | Active rate limit |
| `burst` | Active burst limit |

> **Compatibility note**: `reason`/`event` and `ua`/`user_agent` are both emitted as aliases for backward compatibility with existing log consumers.

---

## 3) WebSockets (`internal/ws`)

**Purpose**: Bidirectional WebSocket proxy — forwards `/websocket` connections to the backend RPC WebSocket endpoint, pumping frames in both directions with configurable lifetime controls.

### Behavior

- Upgrade is initiated by the client; vProx opens a new WS connection to the backend.
- Two goroutines run in parallel: client→backend and backend→client pump.
- Both directions share the same timeout context.

### Configuration (per-chain `[ws]` block)

```toml
[ws]
idle_timeout_sec  = 3600   # Close connection if idle for this long (0 = disabled)
max_lifetime_sec  = 0      # Force-close after this duration (0 = unlimited)
```

### Logging

WebSocket lifecycle events (open, close, timeout, error) are logged to `main.log` with:
- `event=ws_open`, `event=ws_close`, `event=ws_timeout`, `event=ws_error`
- `request_id`, `chain`, `backend`, `duration_ms`

---

## 4) Geo (`internal/geo`)

**Purpose**: Country and ASN enrichment for request log lines using MaxMind-compatible MMDB databases. Lookup results are cached for 10 minutes.

### Database search order

vProx probes the following paths in order and uses the first valid database found:

**IP2Location (preferred):**

1. `$IP2LOCATION_MMDB` (explicit env var)
2. `$HOME/.vProx/data/geolocation/ip2location.mmdb` (installed by `make install`)
3. `/usr/local/share/IP2Proxy/ip2location.mmdb`
4. `/usr/local/share/IP2Location/ip2location.mmdb`
5. `/usr/share/IP2Proxy/ip2location.mmdb`
6. `/usr/share/IP2Location/ip2location.mmdb`
7. `./ip2location.mmdb`

**GeoLite2 fallback:**

```ini
GEOLITE2_COUNTRY_DB=/path/to/GeoLite2-Country.mmdb
GEOLITE2_ASN_DB=/path/to/GeoLite2-ASN.mmdb
```

If no database is found, geo enrichment is silently disabled. All proxy functionality continues normally.

### Log fields

- `country` — ISO 3166-1 alpha-2 code (e.g., `US`, `DE`)
- `asn` — Autonomous System Number (e.g., `AS15169`)

---

## 5) Backup (`internal/backup`)

**Purpose**: Multi-file archive and rotation with atomic copy-truncate semantics and gzip compression.

### Manual backup

```bash
vProx --new-backup                # Run one backup cycle
vProx --new-backup --reset_count  # Backup + reset access counters
vProx --list-backup               # List existing archives
vProx --backup-status             # Show scheduler status
```

### Automated backups

Configure via `$HOME/.vProx/config/backup/backup.toml` (see [`config/backup/backup.sample.toml`](./config/backup/backup.sample.toml)):

```toml
[backup]
automation = false          # Enable automatic backup scheduler (default: false — opt-in)
compression = "tar.gz"
interval_days = 7           # Rotate every N days (0 = disable timer)
max_size_mb = 100           # Rotate when main.log exceeds N MB (0 = disable)
check_interval_min = 10     # How often to check conditions

[backup.files]
# All *.log files in data/logs/ are auto-discovered (chain logs included automatically).
# List only non-.log files you want archived (e.g. .jsonl, .json).
logs   = ["rate-limit.jsonl"]
data   = ["access-counts.json"]
config = ["ports.toml"]
```

Trigger logic: backup fires when **either** `interval_days` or `max_size_mb` threshold is met (whichever comes first).

Backups are written to:

```
$HOME/.vProx/data/logs/archives/backup.YYYYMMDD_HHMMSS.tar.gz
```

### Backup lifecycle

1. Copy `main.log` → `main.log.<timestamp>.copy`
2. Truncate `main.log` (zero-length; new writes start immediately)
3. Emit `BACKUP STARTED` to stdout/main.log
4. Compress copy → `main.log.<timestamp>.tar.gz` in `archives/`
5. Delete `.copy` file
6. Emit `BACKUP COMPLETE` or `BACKUP FAILED`

**Structured log fields:**

| Field | Description |
|---|---|
| `request_id` | Correlation ID for the backup operation |
| `status` | `started`, `complete`, `failed` |
| `filesize` | Original log size (bytes) |
| `compression` | Compression ratio |
| `location` | Archive path |
| `filename` | Archive filename |
| `archivesize` | Compressed size (bytes) |
| `failed` | Error reason (on failure only) |

### Access counter persistence

Source access counters (`src_count`) are persisted at `$HOME/.vProx/data/access-counts.json`. They survive restarts and backup cycles. Reset only when explicitly requested:

```bash
vProx backup --reset_count    # or --reset-count
```

---

## 6) Logging (`internal/logging`)

**Purpose**: Structured single-line log output to both stdout (ANSI-colored) and `main.log` (plain text). Dual-sink is activated on `start`; dev mode (`go run`) outputs to stdout only.

### Format

```
<timestamp> <LEVEL> <message> key=value key=value ... module=<source>
```

**Levels:** `INF`, `WRN`, `ERR`, `DBG`

### Request correlation

Every incoming request gets a `request_id` (UUID) assigned in middleware:

- `logging.EnsureRequestID(r)` — assigns ID early in the handler chain
- `logging.SetResponseRequestID(w, id)` — echoes ID in the response header

All log lines for a request carry the same `request_id` for trace correlation.

### Per-chain logging

If a chain config includes a `[logging]` block:

```toml
[logging]
file = "my-chain.log"   # filename only; resolves to data/logs/my-chain.log
```

vProx writes summary lines to **both** `main.log` and the chain-specific file. Relative paths resolve under `$VPROX_HOME`.

---

## 7) Build & Install (Makefile)

See [`INSTALLATION.md`](./INSTALLATION.md) for the full install guide.

**Quick reference:**

```bash
make install    # Full install (validate, dirs, geo, config, env, binary, systemd)
make build      # Build binary only → .build/vProx
make dirs       # Create runtime directories
make geo        # Decompress MMDB (ip2l/ip2location.mmdb.gz → $HOME/.vProx/data/geolocation/)
make config     # Install sample configs
make systemd    # Render (and optionally install) systemd unit file + sudoers rule
make clean      # Remove .build/
```

---

## 8) Troubleshooting

- **Unknown host / 404 on all requests**: the `host` field in chain config must match the incoming `Host` header exactly. Test with `curl -H "Host: <chain-host>" http://localhost:3000/rpc/status`.
- **No configs found**: confirm `$HOME/.vProx/config/chains/*.toml` exists and `ports.toml` is present.
- **Geo not loading**: verify `IP2LOCATION_MMDB` path and run `make geo` to re-decompress.
- **Rate limit too strict**: increase `VPROX_RPS` and `VPROX_BURST` in `.env`, restart.
- **WebSocket drops immediately**: check `[ws] idle_timeout_sec` in chain config; default is 3600.
- **Binary not found after install**: add `$(go env GOPATH)/bin` to `PATH`.

---

## 9) CLI Flags (Quick Reference)

For the full flag reference with examples, see [`CLI_FLAGS_GUIDE.md`](./CLI_FLAGS_GUIDE.md).

**Most common flags:**

```bash
vProx --help                          # Built-in help
vProx --version                       # Print version
vProx start                           # Start server foreground (default :3000)
vProx start -d                        # Start as daemon (systemd service)
vProx stop                            # Stop the service
vProx restart                         # Restart the service
vProx --validate                      # Validate config and exit
vProx --info --verbose                # Print resolved runtime/config summary
vProx --dry-run                       # Load everything, don't start server
vProx --addr :4000                    # Override listen address (default :3000)
vProx --home /custom/path             # Override runtime home (default $HOME/.vProx)
vProx --config /path/to/config        # Override config dir
vProx --chains /path/to/chains        # Override chains dir
vProx --log-file /path/to/main.log    # Override log file path
vProx --quiet                         # Suppress non-error output
vProx --new-backup                    # Run one backup cycle
vProx --new-backup --reset_count      # Backup + reset access counters
vProx --list-backup                   # List backup archives
vProx --backup-status                 # Show scheduler status
```

**Rate limit overrides (CLI, override .env):**

```bash
vProx --rps 50              # Requests per second
vProx --burst 200           # Burst capacity
vProx --disable-auto        # Disable auto-quarantine
vProx --auto-rps 0.5        # Penalty RPS during quarantine
vProx --auto-burst 1        # Penalty burst during quarantine
```

---

## 10) Chain Config Reference

See [`config/chains/chain.sample.toml`](./config/chains/chain.sample.toml) for a fully annotated template.

**Key top-level fields:**

```toml
chain_name    = "my-chain"             # Unique identifier (logs)
host          = "my-chain.example.com" # Matched Host header
ip            = "10.0.0.1"            # Backend node IP
default_ports = true                   # Use ports.toml defaults

msg_rpc       = false    # Show rpc_msg banner on RPC index
msg_api       = false    # Show api_msg banner on REST swagger

rpc_aliases   = []       # Extra RPC hostnames (vhost routing only)
rest_aliases  = []       # Extra REST hostnames (vhost routing only)
api_aliases   = []       # Extra API alias hostnames (vhost routing only)
```

**Features block:**

```toml
[features]
rpc_address_masking = true   # Mask local IP (10.0.0.x/) links on RPC index HTML
mask_rpc            = ""     # Replacement label (empty = remove the link entirely)
swagger_masking     = false  # Rewrite Swagger Try-It base URL to public host (future)
absolute_links      = "auto" # auto | always | never
```

---

## 11) vOps — Log Archive Analyzer

**Version**: v1.4.0 (renamed from vLog; previously shipped as vLog with vProxVL v1.2.0)

**Purpose**: Standalone binary that analyzes vProx log archives. Maintains a SQLite database of per-IP accounts, request events, and rate-limit events. Provides a CRM-like web UI and REST API for security intelligence, traffic analysis, and multi-location endpoint health monitoring.

**Location:**
- `cmd/vops/` — binary entry point
- `internal/vops/` — packages (config, db, ingest, intel, web)

**Binary**: `vops` (integrated via `vprox vops`) — standalone, mirrors vProx architecture (single binary, embedded HTTP server, Apache-proxied).

**Database**: SQLite at `$VPROX_HOME/data/vops.db` via `modernc.org/sqlite` (pure Go, no CGO required).

**Config**: `$VPROX_HOME/config/vops/vops.toml` — sample at `config/vops/vops.sample.toml`.

### CLI Commands

| Command | Action |
|---|---|
| `vprox vops start` | Start vOps server (foreground) |
| `vprox vops start -d` | Start as background daemon (`sudo service vOps start`) |
| `vprox vops stop` | Stop vOps service (`sudo service vOps stop`) |
| `vprox vops restart` | Restart vOps service (`sudo service vOps restart`) |
| `vprox vops ingest` | One-shot archive ingest and exit |
| `vprox vops status` | Show database stats and exit |
| `vprox vops accounts` | List IP accounts as JSON |
| `vprox vops threats` | List flagged IPs (score ≥ 50) |
| `vprox vops cache` | Manage intel cache |

**Runtime flags (start):** `--home`, `--port`, `--quiet`, `--no-watch`, `--no-enrich`, `--watch-interval`
**One-shot flags:** `--list-archives`, `--list-accounts`, `--list-threats`, `--enrich <ip>`, `--purge-cache <ip|all>`, `--validate`, `--info`, `--dry-run`

### Dashboard

The dashboard (`GET /`) provides:

- **Stats cards**: total IPs, total requests, rate-limit events, flagged IPs
- **Dual-line Chart.js charts**: requests over time (left block) and IPs/rate-limits (right block) with chart-type dropdown
- **Endpoint status panel**: table of proxied hosts with request counts, unique IPs, last seen, and 3 live probe columns:

| Column | Source | Description |
|---|---|---|
| **Live** | — | Probe trigger button |
| **Local** | vOps server | Direct HTTP probe from the vOps host; shows latency in green or error in red |
| **🇨🇦** | check-host.net — Vancouver | External probe from Canada node |
| **🌍** | check-host.net — random WW node | External probe from Europe/Asia/Americas |

During probing, each cell shows a CSS spinner ring. On completion, cells show `NNms` (green) or error text (red). Hovering shows the probe node label and probed URL.

### Accounts Page

`GET /accounts` — paginated, searchable, sortable IP account list:

- **Search**: by IP, country code, or row ID
- **Per-page**: 25 / 50 / 100 / 200 / All
- **Sort**: any column; sort state persisted in URL (back-nav and direct URL sharing work correctly)
- **Columns**: Org (ip-api.com) · IP · Country · Requests · Rate Limits · Threat Score · Last Seen · Actions · Status
- **Status badge**: ALLOWED (green) / BLOCKED (red)
- **Investigate button**: turns green (`.btn-investigate-done`) when threat intel exists for that IP

### Web UI Routes

| Route | Description |
|---|---|
| `GET /` | Dashboard: stats, charts, endpoint probe panel |
| `GET /accounts` | Paginated IP account list with search and sort |
| `GET /accounts/:ip` | CRM-like IP account detail |
| `POST /api/v1/ingest` | Trigger archive ingest |
| `GET /api/v1/accounts` | JSON account list |
| `GET /api/v1/accounts/:ip` | JSON account detail |
| `GET /api/v1/probe?host=HOST` | Multi-location HTTP probe (local + CA + WW) |
| `GET /api/v1/chart?type=TYPE` | Chart data (requests, ips, endpoint_summary, …) |
| `POST /api/v1/enrich/:ip` | SSE: run threat intelligence (VirusTotal + AbuseIPDB + Shodan) |
| `POST /api/v1/osint/:ip` | SSE: run OSINT scan |
| `POST /api/v1/investigate/:ip` | SSE: full investigation (TI + OSINT, two-phase) |
| `POST /api/v1/block/:ip` | Flag IP as blocked |
| `POST /api/v1/unblock/:ip` | Remove block flag |
| `GET /api/v1/stats` | JSON dashboard stats |

### Internal Packages

| Package | Description |
|---|---|
| `internal/vops/config/` | TOML config loader (`vops.toml`) |
| `internal/vops/db/` | SQLite schema, connection pool, query methods (5 tables + 6 indexes) |
| `internal/vops/ingest/` | Archive scanner, log parser (`main.log` + `rate-limit.jsonl`), FS watcher |
| `internal/vops/intel/` | AbuseIPDB v2, VirusTotal v3, Shodan API clients; parallel queries (3 goroutines); composite threat scoring (0–100); ~10s vs former ~30s |
| `internal/vops/web/` | Embedded HTTP server, `html/template` + `go:embed` + htmx UI, SSE handlers, probe handler |

### OSINT Engine

5 operations run concurrently via `sync.WaitGroup` + `sync.Mutex`:

| Operation | Source | Detail |
|---|---|---|
| Reverse DNS | stdlib | PTR lookup |
| Port scan | stdlib | TCP dial on common ports (22, 80, 443, 26657, 1317, 9090, 9091) |
| Org / geo | ip-api.com | Country, city, ISP, org, ASN |
| Protocol probe | net/http | Cosmos RPC `/status`, REST `/cosmos/base/tendermint/v1beta1/node_info` |
| Cosmos RPC | CometBFT | Node info if RPC port open |

Typical completion: ~5s (concurrent) vs ~23s (sequential).

### vProx Integration

After `--new-backup`, vProx optionally POSTs to `vops_url/api/v1/ingest` to trigger automatic ingest:

```toml
# $VPROX_HOME/config/ports.toml
vops_url = "http://localhost:8889"
```

The POST is non-fatal — if vOps is unavailable, vProx logs a warning and continues normally.

### Security Assessment

vOps builds a composite threat score (0–100) for each IP using three external intelligence sources (queries run in parallel):

| Source | Weight | API Version |
|---|---|---|
| AbuseIPDB | 40% | v2 |
| VirusTotal | 40% | v3 |
| Shodan | 20% | current |

**Threat levels:**

| Score Range | Level |
|---|---|
| 0–19 | clean |
| 20–49 | suspicious |
| 50–100 | malicious |

**Threat flags:** `ABUSEIPDB_CONFIRMED`, `VT_MALICIOUS`, `SHODAN_OPEN_RISKY_PORT`, `HIGH_RATELIMIT_EVENTS`, `DATACENTER_ASN`

### Dependencies

- `modernc.org/sqlite v1.46.1` — pure Go SQLite driver, no CGO required

---

## 12) Fleet Module (`internal/fleet/`)

**Version**: v1.3.0  
**Purpose**: Centralized SSH control plane. vProx SSHes into validator VMs to execute chain maintenance scripts, monitor upgrade proposals, poll chain status (height, governance, sync), and dispatch deployments.

**Location:**
- `internal/fleet/` — packages: `config/`, `ssh/`, `runner/`, `state/`, `status/`, `api/`
- `cmd/vprox/fleet.go` — CLI command wiring
- `scripts/chains/{chain}/{component}/{script}.sh` — scripts executed remotely on VMs

### Config Files

| File | Purpose |
|---|---|
| `config/fleet/settings.toml` | Global SSH defaults + poll interval. Copy from `settings.sample`. |
| `config/infra/<datacenter>.toml` | VM registry (one file per datacenter). All `*.toml` files in `config/infra/` are scanned. |
| `config/chains/*.toml` `[management]` | Per-chain SSH target, role, and datacenter. `managed_host=true` registers the chain node. |

### SSH Key Setup

Generate a dedicated key for fleet → VM connections (run once on the vProx machine):

```bash
ssh-keygen -t ed25519 -C "vprox-fleet" -f ~/.ssh/vprox_fleet
```

Authorize it on each validator VM:

```bash
ssh-copy-id -i ~/.ssh/vprox_fleet.pub <user>@<vm-ip>
```

Allow passwordless sudo for fleet scripts on each VM (edit `/etc/sudoers` or drop a file in `/etc/sudoers.d/`):

```
# /etc/sudoers.d/vprox-fleet
<user> ALL=(ALL) NOPASSWD: /usr/bin/apt-get, /usr/bin/systemctl
```

Set the key path in `config/fleet/settings.toml`:

```toml
[ssh]
user     = "ubuntu"
key_path = "~/.ssh/vprox_fleet"
port     = 22
```

### Credential Precedence

SSH credentials are resolved per-VM with the following priority (highest → lowest):

```
[[vm]].user / [[vm]].key_path         ← VM-specific override (infra.toml)
[host].user / [host].ssh_key_path     ← Physical host default (same infra.toml file)
[vprox].ssh_key_path                  ← Fleet-wide default (infra.toml [vprox] section)
config/fleet/settings.toml [ssh]      ← Global fallback
```

### Chain Registration

To register a chain node for SSH management, set `managed_host = true` in the chain's `[management]` block:

```toml
# config/chains/mychain.toml
[management]
managed_host     = true
lan_ip           = ""        # empty = use top-level chain.ip
user             = ""        # empty = fleet settings.toml default
key_path         = ""        # empty = fleet settings.toml default
port             = 0         # 0 = port 22
type             = ["validator"]
valoper          = "cosmosvaloper1..."
datacenter       = "QC"
exposed_services = true      # true = probe via chain.host; false = probe via lan_ip directly
```

`exposed_services` controls probe routing only — it is independent of `managed_host`.

| `managed_host` | `exposed_services` | Behaviour |
|---|---|---|
| false | false | vOps probes via `lan_ip`; no SSH management |
| false | true | vOps probes via `chain.host` (through vProx); no SSH management |
| true | false | SSH management enabled; vOps probes via `lan_ip` (same-LAN case) |
| true | true | SSH management enabled; vOps probes via `chain.host` (public domain) |

### CLI Commands

```bash
vprox fleet hosts          # List physical hosts from config/infra/
vprox fleet vms            # List all registered VMs
vprox fleet chains         # List chains registered in fleet SQLite state
vprox fleet unregister <chain>   # Remove chain from fleet state (by chain_name)
vprox fleet deploy --chain <name> --script <path>   # Dispatch script to VM via SSH
vprox fleet update [--host <name>]   # Run apt upgrade on VM(s) via SSH
```

### API Routes

| Method | Route | Description |
|---|---|---|
| `GET` | `/api/v1/fleet/vms` | JSON list of all VMs with status |
| `GET` | `/api/v1/fleet/chains` | JSON list of registered chains |
| `POST` | `/api/v1/fleet/deploy` | Dispatch a script to a VM |
| `GET` | `/api/v1/fleet/deployments` | Deployment history |
| `POST` | `/api/v1/fleet/chains/registered` | Register a chain |
| `POST` | `/api/v1/fleet/chains/registered/{chain}/unregister` | Unregister a chain |

### Internal Packages

| Package | Description |
|---|---|
| `internal/fleet/config/` | Infra loader (`LoadFromInfraFiles`, `LoadFromChainConfigs`); credential precedence resolution |
| `internal/fleet/ssh/` | SSH client dispatcher using `golang.org/x/crypto/ssh` |
| `internal/fleet/runner/` | Remote bash execution over SSH; captures stdout/stderr |
| `internal/fleet/state/` | SQLite persistence for deployments + registered chains |
| `internal/fleet/status/` | Cosmos RPC poller: height, gov proposals, upgrade plan, sync status |
| `internal/fleet/api/` | HTTP handlers wired into vProx router |

### Infra File Structure

```toml
# config/infra/qc.toml — one file per datacenter

[host]
name         = "homelab-qc"
lan_ip       = "10.0.0.1"
datacenter   = "QC"
user         = "ubuntu"           # default user for all [[vm]] in this file
ssh_key_path = "~/.ssh/vprox_fleet"  # default key for all [[vm]] in this file

[vprox]
name     = "vProx"
lan_ip   = "10.0.0.65"
key      = "~/.vprox/secret/id.agent"

[[vm]]
name       = "mychain-val"
host       = "10.0.0.66"        # SSH target
lan_ip     = "10.0.0.66"        # shown in dashboard
port       = 22
datacenter = "QC"
type       = "validator"
chain_name = "mychain"          # links to config/chains/mychain.toml

[vm.ping]
country  = "CA"                 # check-host.net probe country
provider = ""                   # empty = random node in country
```

Multiple VMs under the same physical host inherit `[host].user` and `[host].ssh_key_path` when their own `user`/`key_path` fields are empty.


