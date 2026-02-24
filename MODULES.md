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
| `[ports]` | table | Per-service port overrides when `default_ports = false` |
| `[expose]` | table | Routing mode: `mode = "path"` or `mode = "vhost"` |
| `[aliases]` | table | Service-specific hostnames for vhost routing |
| `[features]` | table | `banner_injection`, `absolute_links` |
| `[logging]` | table | `file` — per-chain log path (relative to `VPROX_HOME`) |
| `[ws]` | table | `idle_timeout_sec`, `max_lifetime_sec` |

**Routing modes:**

- `mode = "path"` (default): `host/rpc/...` → `ip:26657`, `host/rest/...` → `ip:1317`, etc.
- `mode = "vhost"`: `rpc.<host>` → `ip:26657`, `rest.<host>` → `ip:1317`, etc. (requires DNS/nginx for subdomains)

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

Configure via `$HOME/.vProx/config/backup/backup.toml` (see [`config/backup.sample.toml`](./config/backup.sample.toml)):

```toml
[backup]
automation = true           # Enable automatic backup scheduler
compression = "tar.gz"
interval_days = 7           # Rotate every N days (0 = disable timer)
max_size_mb = 100           # Rotate when main.log exceeds N MB (0 = disable)
check_interval_min = 10     # How often to check conditions

[backup.files]
logs   = ["main.log", "rate-limit.jsonl"]
data   = ["access-counts.json"]
config = ["ports.toml", "chains/your_chain.toml"]
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
vProx --new-backup --reset_count    # or --reset-count
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
file = "logs/my-chain.log"
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
vProx --rps 50                        # Override default RPS (env: VPROX_RPS, default: 25)
vProx --burst 250                     # Override burst capacity (env: VPROX_BURST, default: 100)
vProx --auto-rps 0.5                  # Override quarantine penalty RPS (env: VPROX_AUTO_RPS)
vProx --auto-burst 2                  # Override quarantine penalty burst (env: VPROX_AUTO_BURST)
vProx --disable-auto                  # Disable auto-quarantine for this run
```

For detailed flag descriptions, see [`CLI_FLAGS_GUIDE.md`](./CLI_FLAGS_GUIDE.md).
