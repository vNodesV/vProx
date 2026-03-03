# vProx Installation Guide

This guide covers building, installing, and configuring vProx from source on a Linux host.

## Table of Contents

- [Prerequisites](#prerequisites)
- [Quick Install](#quick-install)
- [Build from Source](#build-from-source)
- [Full Install with make](#full-install-with-make)
- [Runtime Directory Layout](#runtime-directory-layout)
- [Configuration](#configuration)
  - [Environment Variables (.env)](#environment-variables-env)
  - [Default Ports (ports.toml)](#default-ports-portstoml)
  - [Per-Chain Config](#per-chain-config)
- [Geo Database](#geo-database)
- [Systemd Service](#systemd-service)
- [Running vProx](#running-vprox)
- [Observability](#observability)
- [Upgrading](#upgrading)
- [Installing vLog](#installing-vlog)
- [Troubleshooting](#troubleshooting)

---

## Prerequisites

| Requirement | Version | Notes |
|---|---|---|
| Go | 1.25+ | See `go.mod` for exact version |
| git | Any | Clone the repo |
| make | GNU make | Build automation |
| gzip / gunzip | Standard | MMDB decompression (part of coreutils) |
| Linux | systemd host | For service installation |
| macOS | Any | Dev/build only; systemd not applicable |

Install Go: <https://go.dev/doc/install>

Verify:

```bash
go version   # go version go1.25.x linux/amd64
make --version
```

---

## Quick Install

```bash
git clone https://github.com/vNodesV/vProx.git
cd vProx
make install
```

`make install` does everything: validates Go, creates runtime directories, decompresses the geo database, sets up the default `.env`, and installs the binary to `$GOPATH/bin/vProx` with an optional symlink to `/usr/local/bin/vProx`.

---

## Build from Source

To build the binary only (no install):

```bash
make build
# Output: .build/vProx
```

Or with raw Go tooling (keeps build artifacts outside the repo root):

```bash
go build -o .build/vProx ./cmd/vprox
```

To build and run directly without installing:

```bash
go run ./cmd/vprox
```

---

## Full Install with make

```bash
make install
```

This runs the following steps in order:

1. **validate-go** — Confirms `GOROOT` and `GOPATH` are set and prints the Go version.
2. **dirs** — Creates the runtime directory tree under `$HOME/.vProx/` (idempotent).
3. **geo** — Decompresses `assets/geo/ip2location.mmdb.gz` → `$HOME/.vProx/data/geolocation/ip2location.mmdb`.
4. **config** — Copies `config/chains/chain.sample.toml` and creates `config/ports.toml` if missing; installs `backup.sample.toml`.
5. **env** — Creates `$HOME/.vProx/.env` with default values if missing.
6. **binary** — Builds and installs to `$GOPATH/bin/vProx`. Prompts (y/n) to create a symlink at `/usr/local/bin/vProx`.
7. **systemd** — Renders `vProx.service` from `vprox.service.template` to `$HOME/.vProx/service/vProx.service`. Optionally installs to `/etc/systemd/system/` and creates `/etc/sudoers.d/vprox` for passwordless service management.

> **Note**: If you do not want a symlink, answer `n` at the prompt. You can then run `vProx` via `$GOPATH/bin/vProx` or add `$GOPATH/bin` to `PATH`.

Other targets:

```bash
make build      # Build binary only → .build/vProx
make dirs       # Create runtime directories only
make geo        # Decompress MMDB only
make config     # Install sample config files only
make systemd    # Render (and optionally install) the systemd unit file
make clean      # Remove .build/ directory
```

---

## Runtime Directory Layout

After `make install`, vProx uses the following layout under `$HOME/.vProx/`:

```
$HOME/.vProx/
├── .env                         # Environment variables (rate limits, geo paths)
├── config/
│   ├── ports.toml               # Default service ports for all chains
│   ├── chains/
│   │   ├── chain.sample.toml    # Sample chain configuration (reference only)
│   │   └── *.toml               # Your chain configs (create one per chain)
│   └── backup/
│       └── backup.toml          # Backup automation config
├── data/
│   ├── geolocation/
│   │   └── ip2location.mmdb     # IP geo database (decompressed by make geo)
│   ├── access-counts.json       # Persisted source access counters
│   └── logs/
│       ├── main.log             # Structured proxy log
│       ├── rate-limit.jsonl     # JSONL rate limit events
│       └── archives/            # Compressed log backups (*.tar.gz)
├── internal/                    # Reserved for internal runtime state
└── service/
    └── vProx.service            # Rendered systemd unit file
```

Override the base path:

```bash
# Environment variable:
export VPROX_HOME=/opt/vprox

# CLI flag (overrides env var):
vProx --home /opt/vprox
```

---

## Configuration

### Environment Variables (.env)

The `.env` file lives at `$VPROX_HOME/.env`. `make install` creates a default one if absent. A reference with all available variables is at [`.env.example`](./.env.example).

Key variables:

```ini
# Geo database paths (auto-set by make install)
IP2LOCATION_MMDB=$HOME/.vProx/data/geolocation/ip2location.mmdb
GEOLITE2_COUNTRY_DB=
GEOLITE2_ASN_DB=

# Server
VPROX_ADDR=:3000

# Rate limiting
VPROX_RPS=25
VPROX_BURST=100
VPROX_AUTO_ENABLED=true
VPROX_AUTO_THRESHOLD=120
VPROX_AUTO_WINDOW_SEC=10
VPROX_AUTO_RPS=1
VPROX_AUTO_BURST=1
VPROX_AUTO_TTL_SEC=900
```

> **Note**: Backup automation is configured via `config/backup/backup.toml`, not `.env`.

### Default Ports (ports.toml)

`$HOME/.vProx/config/ports.toml` defines default service ports applied to all chains unless overridden per-chain:

```toml
rpc      = 26657
rest     = 1317
grpc     = 9090
grpc_web = 9091
api      = 1317
```

### Per-Chain Config

Create one `.toml` file per chain in `$HOME/.vProx/config/chains/`. A fully commented template is at [`config/chains/chain.sample.toml`](./config/chains/chain.sample.toml).

Minimal example (`$HOME/.vProx/config/chains/my-chain.toml`):

```toml
chain_name    = "my-chain"
host          = "my-chain.example.com"   # Host header vProx matches on
ip            = "127.0.0.1"              # Backend node IP
default_ports = true                     # Use ports from config/ports.toml

[services]
rpc       = true
rest      = true
websocket = true
grpc      = false
grpc_web  = false

[expose]
path  = true    # Enable /rpc, /rest, /websocket on the base host
vhost = false   # Enable rpc.<host>, api.<host> subdomains
```

**Path routing** (`path = true`): requests to `my-chain.example.com/rpc/...` are forwarded to `127.0.0.1:26657`.

**Vhost routing** (`vhost = true`): requests to `rpc.my-chain.example.com` are forwarded to `127.0.0.1:26657`. Requires DNS or a reverse proxy for each subdomain. Both `path` and `vhost` can be enabled simultaneously.

> After changing chain configs, restart vProx: `sudo systemctl restart vProx.service`

---

## Geo Database

The IP2Location MMDB provides country and ASN enrichment for log lines. It is bundled in the repo as a compressed archive (`assets/geo/ip2location.mmdb.gz`, 6.8 MB) and decompressed during `make install` to:

```
$HOME/.vProx/data/geolocation/ip2location.mmdb
```

To install or update the database separately (no sudo required):

```bash
make geo
```

If the database is missing at runtime, geo enrichment is silently disabled — all other proxy functionality continues normally.

To use an alternative or updated database, set the path in `.env`:

```bash
# Override with a custom IP2Location database:
IP2LOCATION_MMDB=/path/to/your/ip2location.mmdb

# Or use GeoLite2 as a fallback:
GEOLITE2_COUNTRY_DB=/path/to/GeoLite2-Country.mmdb
GEOLITE2_ASN_DB=/path/to/GeoLite2-ASN.mmdb
```

The lookup cache refreshes every 10 minutes.

---

## Systemd Service

`make install` (or `make systemd` standalone) renders a systemd unit file from `vprox.service.template`:

```
$HOME/.vProx/service/vProx.service
```

To install it on a systemd host:

```bash
sudo cp $HOME/.vProx/service/vProx.service /etc/systemd/system/vProx.service
sudo systemctl daemon-reload
sudo systemctl enable vProx.service
sudo systemctl start vProx.service
```

Or let `make systemd` prompt you to do it automatically.

Check service status:

```bash
sudo systemctl status vProx.service
```

Follow live logs in CosmosSDK-style line format:

```bash
journalctl -u vProx.service -f --output=cat
```

Start / stop / restart (via vProx CLI — passwordless with sudoers rule):

```bash
vProx start -d     # start as daemon
vProx stop         # stop the service
vProx restart      # restart the service
```

Or directly with systemctl:

```bash
sudo systemctl stop vProx.service
sudo systemctl restart vProx.service
```

---

## Running vProx

**Development (no install):**

```bash
go run ./cmd/vprox start       # Start proxy (foreground, logs to stdout)
go run ./cmd/vprox --validate  # Validate config and exit
go run ./cmd/vprox --info      # Print resolved config summary
go run ./cmd/vprox --dry-run   # Load config without starting server
```

**After install:**

```bash
vProx start                    # Start server foreground (default :3000)
vProx start -d                 # Start as daemon (systemd service)
vProx stop                     # Stop the service
vProx restart                  # Restart the service
vProx --addr :4000             # Override listen address
vProx --validate               # Validate config files
vProx --info --verbose         # Full runtime/config summary
vProx --new-backup             # Run one log backup cycle
vProx --new-backup --reset_count  # Backup + reset access counters
vProx --list-backup            # List backup archives
vProx --backup-status          # Show scheduler status
```

For the complete flag reference, see [`CLI_FLAGS_GUIDE.md`](./CLI_FLAGS_GUIDE.md).

---

## Observability

### Prometheus metrics (`/metrics`)

vProx exposes a Prometheus-compatible metrics endpoint at `/metrics` on the main listen port. The following 8 metrics are exported:

| Metric | Type | Labels | Description |
|--------|------|--------|-------------|
| `vprox_requests_total` | Counter | `method`, `route`, `status_code` | Total proxied HTTP requests |
| `vprox_active_connections` | Gauge | — | Currently active proxy connections |
| `vprox_request_duration_seconds` | Histogram | `method`, `route` | Proxy request latency distribution |
| `vprox_proxy_errors_total` | Counter | `route`, `error_type` | Proxy errors (`backend_error`, `request_build_error`, `unknown_host`) |
| `vprox_rate_limit_hits_total` | Counter | — | Requests that received a 429 response |
| `vprox_geo_cache_hits_total` | Counter | — | Geo lookup cache hits |
| `vprox_geo_cache_misses_total` | Counter | — | Geo lookup cache misses |
| `vprox_backup_events_total` | Counter | `status` | Backup lifecycle events (`started`, `completed`, `failed`) |

Example Prometheus scrape configuration:

```yaml
scrape_configs:
  - job_name: "vprox"
    scrape_interval: 15s
    static_configs:
      - targets: ["localhost:3000"]
```

### Health check (`/healthz`)

The `/healthz` endpoint returns a JSON object with server status and uptime:

```json
{
  "status": "ok",
  "uptime": "2h15m30s"
}
```

Returns HTTP 200 when healthy, HTTP 503 when a subsystem has failed. Use this endpoint for load balancer health checks and uptime monitoring.

### pprof debug server

When the `VPROX_DEBUG=1` environment variable is set, vProx starts a separate pprof HTTP server on port 6060 (default). This exposes Go runtime profiling data at the standard `/debug/pprof/` paths.

```bash
VPROX_DEBUG=1 vProx start
# Then in another terminal:
go tool pprof http://localhost:6060/debug/pprof/heap
```

> **Warning**: The pprof server exposes internal runtime state. Never expose port 6060 publicly. It runs on a separate port specifically to prevent accidental exposure through the main proxy port.

---

## Upgrading

1. Pull the latest code:

   ```bash
   cd vProx
   git pull origin main
   ```

2. Re-run the install:

   ```bash
   make install
   ```

   `make install` is idempotent — it skips steps that are already complete (existing `.env`, existing dirs, etc.) and updates what has changed.

3. Restart the service if running:

   ```bash
   sudo systemctl restart vProx.service
   ```

For migration guidance when upgrading between major versions, see [`docs/UPGRADE.md`](./docs/UPGRADE.md).

---

## Installing vLog

vLog is a companion binary for analyzing vProx log archives. It is built and installed separately from vProx.

### Build and install

```bash
make install-vlog
```

This will:
1. Build `vLog` binary to `.build/vLog` and install to `$GOPATH/bin/vLog`
2. Copy `config/vlog/vlog.sample.toml` → `$VPROX_HOME/config/vlog/vlog.toml` (only if absent)
3. Optionally install the systemd service unit

### Configure

Edit `$VPROX_HOME/config/vlog/vlog.toml`:

```toml
[vlog]
port     = 8889
db_path  = "~/.vProx/data/vlog.db"
archives_dir = "~/.vProx/data/logs/archives"

[intel]
abuseipdb_key  = "your-key"
virustotal_key = "your-key"
shodan_key     = "your-key"
auto_enrich    = true
```

### Dashboard authentication (optional)

By default the vLog dashboard is open with no login required. To enable password protection, generate a bcrypt password hash and configure it in `vlog.toml`.

**Why bcrypt?** Bcrypt is a one-way hash — the plaintext password is never stored. The cost factor (12) means each hash verification takes ~250ms. This is intentional: it rate-limits brute-force attacks even if the hash file is compromised. Bcrypt is the OWASP-recommended algorithm for password storage, and Cost=12 is their minimum recommended work factor.

**Generating the hash with `htpasswd`:**

The `htpasswd` utility ships with the `apache2-utils` package (Debian/Ubuntu) or `httpd-tools` (RHEL/Fedora). We use only its bcrypt output format (`-B` flag) — Apache itself is not required. It is the most widely available CLI tool for generating bcrypt hashes on Linux and macOS.

```bash
# Install htpasswd (Debian/Ubuntu)
sudo apt install apache2-utils

# Generate bcrypt hash
htpasswd -nbBC 12 admin yourpassword | cut -d: -f2
```

Flag breakdown:

| Flag | Purpose |
|------|---------|
| `-n` | Print output to stdout (don't write to a file) |
| `-b` | Read the password from the command line argument |
| `-B` | Use bcrypt algorithm (not MD5 or SHA) |
| `-C 12` | Bcrypt cost factor 12 (~250ms per hash; OWASP minimum recommendation) |

The `cut -d: -f2` strips the `admin:` username prefix, leaving only the `$2y$12$...` hash string.

**Alternative if `htpasswd` is unavailable:**

```bash
python3 -c "import bcrypt; print(bcrypt.hashpw(b'yourpassword', bcrypt.gensalt(rounds=12)).decode())"
```

Requires the `bcrypt` Python package (`pip3 install bcrypt`).

**Configure the hash in `vlog.toml`:**

Paste the hash into the `[vlog.auth]` block:

```toml
[vlog.auth]
username      = "admin"
password_hash = "$2y$12$..."   # paste hash here
```

Restart vLog for the change to take effect.

> **Security note**: If `password_hash` is empty or the `[vlog.auth]` block is absent, authentication is bypassed entirely. This is a convenience for development — always set a password hash in production.

### vLog API key

The `/api/v1/ingest` endpoint is a machine-to-machine (M2M) call — vProx pushes log data to vLog after each backup, not a browser user. API keys are the correct auth pattern for M2M communication: stateless, no session cookie overhead, and easy to rotate independently of user credentials.

**Generate a secure key:**

```bash
openssl rand -hex 32
```

**Set the key in `vlog.toml`:**

```toml
[vlog]
api_key = "your-generated-key-here"
```

**How vProx uses it:** When `vlog_url` is configured in `config/ports.toml`, vProx sends the API key in the `X-API-Key` header when POSTing to `/api/v1/ingest` after `--new-backup`. See [vProx integration](#vprox-integration) below.

> **Note**: The API key protects only the ingest endpoint. Block/unblock actions and other dashboard operations from the vLog web UI use session authentication (login), not the API key.

### Block and unblock

The accounts page in the vLog dashboard provides block/unblock controls for individual IP addresses. These actions require only an active login session — no API key is needed from the browser UI. If dashboard authentication is disabled (no `password_hash` set), block/unblock is available without login.

### Run

```bash
vlog start            # foreground server on :8889
vlog start -d         # background daemon (sudo service vLog start)
vlog stop             # stop the service
vlog restart          # restart the service
vlog status           # show database stats
vlog ingest           # one-shot: scan archives and ingest new entries
```

### Apache reverse proxy

Proxy vLog behind Apache with IP restriction (admin-only). See `.vscode/vlog.apache2` in the repo for a validated configuration template.

### vProx integration

To enable automatic ingest after each vProx backup, add to `$HOME/.vProx/config/ports.toml`:

```toml
vlog_url = "http://localhost:8889"
```

When `vlog_url` is set, vProx POSTs to `/api/v1/ingest` with the `X-API-Key` header after each `--new-backup`. Ensure the key matches the `api_key` value in `vlog.toml` (see [vLog API key](#vlog-api-key)). The call is non-fatal — if vLog is unavailable, vProx continues normally.

---

## Troubleshooting

### vProx won't start: "No configs found"

Ensure at least one chain config exists:

```bash
ls $HOME/.vProx/config/chains/*.toml
```

Ports config must also be present:

```bash
cat $HOME/.vProx/config/ports.toml
```

### Unknown host / 404 on all requests

The `host` field in your chain config must exactly match the `Host` header of incoming requests:

```toml
# In chains/my-chain.toml:
host = "my-chain.example.com"
```

Test with: `curl -H "Host: my-chain.example.com" http://localhost:3000/rpc/status`

### Geo not loading

Check the MMDB path in `.env` and confirm the file exists:

```bash
ls -lh $HOME/.vProx/data/geolocation/ip2location.mmdb
echo $IP2LOCATION_MMDB
```

Re-run `make geo` to decompress from the bundled archive.

### Rate limit too aggressive

Adjust `.env` values and restart:

```ini
VPROX_RPS=50
VPROX_BURST=200
VPROX_AUTO_THRESHOLD=300
```

### WebSocket connections dropping immediately

Check chain config timeouts:

```toml
[ws]
idle_timeout_sec = 3600    # 1 hour idle timeout
max_lifetime_sec = 0       # 0 = unlimited
```

### Binary not found after install

Ensure `$GOPATH/bin` is in your PATH:

```bash
export PATH="$PATH:$(go env GOPATH)/bin"
```

