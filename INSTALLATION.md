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
- [Upgrading](#upgrading)
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
3. **geo** — Decompresses `ip2l/ip2location.mmdb.gz` → `$HOME/.vProx/data/geolocation/ip2location.mmdb`.
4. **config** — Copies `chains/chain.sample.toml` and creates `config/ports.toml` if missing.
5. **env** — Creates `$HOME/.vProx/.env` with default values if missing.
6. **binary** — Builds and installs to `$GOPATH/bin/vProx`. Prompts (y/n) to create a symlink at `/usr/local/bin/vProx`.
7. **systemd** — Renders `vProx.service` from `vprox.service.template` to `$HOME/.vProx/service/vProx.service`.

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
├── .env                         # Environment variables (rate limits, backup, geo paths)
├── config/
│   └── ports.toml               # Default service ports for all chains
├── chains/
│   ├── chain.sample.toml        # Sample chain configuration (reference only)
│   └── *.toml                   # Your chain configs (create one per chain)
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

# Backup automation
VPROX_BACKUP_ENABLED=false
VPROX_BACKUP_INTERVAL_DAYS=0
VPROX_BACKUP_MAX_BYTES=0
VPROX_BACKUP_CHECK_MINUTES=10
```

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

Create one `.toml` file per chain in `$HOME/.vProx/chains/`. A fully commented template is at [`chains/chain.sample.toml`](./chains/chain.sample.toml).

Minimal example (`$HOME/.vProx/chains/my-chain.toml`):

```toml
chain_name = "my-chain"
host       = "my-chain.example.com"   # Host header vProx matches on
ip         = "127.0.0.1"              # Backend node IP

[services]
rpc       = true
rest      = true
websocket = true
grpc      = false
grpc_web  = false

[expose]
mode = "path"   # "path" (prefix routing) or "vhost" (subdomain routing)
```

**Path routing** (`mode = "path"`): requests to `my-chain.example.com/rpc/...` are forwarded to `127.0.0.1:26657`.

**Vhost routing** (`mode = "vhost"`): requests to `rpc.my-chain.example.com` are forwarded to `127.0.0.1:26657`. Requires DNS or nginx upstream for each subdomain.

> After changing chain configs, restart vProx: `sudo systemctl restart vProx.service`

---

## Geo Database

The IP2Location MMDB provides country and ASN enrichment for log lines. It is bundled in the repo as a compressed archive (`ip2l/ip2location.mmdb.gz`, 6.8 MB) and decompressed during `make install` to:

```
$HOME/.vProx/data/geolocation/ip2location.mmdb
```

If the database is missing at runtime, geo enrichment is silently disabled — all other proxy functionality continues normally.

To provide an alternative or updated database:

```bash
# Set explicit path in .env:
IP2LOCATION_MMDB=/path/to/your/ip2location.mmdb

# Or use GeoLite2 fallback:
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

Stop / restart:

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
vProx start                    # Start server (default :3000)
vProx --addr :4000             # Override listen address
vProx --validate               # Validate config files
vProx --info --verbose         # Full runtime/config summary
vProx backup                   # Run one log backup cycle
vProx backup --reset_count     # Backup + reset access counters
```

For the complete flag reference, see [`CLI_FLAGS_GUIDE.md`](./CLI_FLAGS_GUIDE.md).

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

## Troubleshooting

### vProx won't start: "No configs found"

Ensure at least one chain config exists:

```bash
ls $HOME/.vProx/chains/*.toml
ls $HOME/.vProx/config/*.toml
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

Or use the symlink if you accepted it during `make install`:

```bash
which vProx     # Should return /usr/local/bin/vProx
```
