# vProx

[![CI](https://github.com/vNodesV/vProx/actions/workflows/ci.yml/badge.svg)](https://github.com/vNodesV/vProx/actions/workflows/ci.yml)
[![CodeQL](https://github.com/vNodesV/vProx/actions/workflows/codeql.yml/badge.svg)](https://github.com/vNodesV/vProx/actions/workflows/codeql.yml)
![Go Version](https://img.shields.io/github/go-mod/go-version/vNodesV/vProx)
[![License](https://img.shields.io/badge/License-Apache_2.0-blue.svg)](./LICENSE)

Production-grade reverse proxy for Cosmos SDK blockchain nodes. vProx routes RPC, REST, gRPC, gRPC-Web, and WebSocket traffic to backend nodes with per-chain configuration, IP-based rate limiting, geo enrichment, Prometheus metrics, and structured logging. Includes **vOps**, a standalone log analysis dashboard with threat intelligence and OSINT capabilities (formerly vLog, renamed in v1.4.0).

## Features

**Proxy & Routing**

- Per-chain TOML configuration with host-header matching
- Path-based routing (`/rpc`, `/rest`, `/grpc`, `/grpc-web`, `/api`) and subdomain routing (`rpc.<host>`, `api.<host>`)
- WebSocket proxying with configurable idle timeout and max lifetime
- HTML banner injection and RPC address masking

**Security & Rate Limiting**

- Per-IP token bucket rate limiting with auto-quarantine
- Trusted proxy CIDR configuration (XFF header trust scoping)
- WebSocket origin allowlist (same-origin by default)
- JSONL rate-limit audit log

**Observability**

- Prometheus metrics endpoint (`/metrics`) — 8 metrics covering requests, connections, latency, errors, rate limits, geo cache, and backups
- Health check endpoint (`/healthz`) — JSON status with uptime; returns 503 on subsystem failure
- pprof debug server on separate port (`VPROX_DEBUG=1` only)
- Structured dual-sink logging (stdout + file) with typed request IDs (`RPC{hex}`, `API{hex}`)

**Geo Enrichment**

- IP2Location MMDB lookup for country and ASN per request
- Bundled database (`assets/geo/ip2location.mmdb.gz`) — no external download required
- In-memory cache with periodic refresh

**Backup & Operations**

- Automated log backup with TOML-configured scheduling and multi-file archive support
- Service management: `start -d`, `stop`, `restart` with systemd integration
- Passwordless sudoers rule for daemon control

**CI/CD**

- golangci-lint with 14 linters enforced on every PR
- Test coverage gate (≥60%)
- Automated release workflow: cross-compilation for linux/darwin × amd64/arm64

## Quick Start

### Prerequisites

| Tool | Version | Notes |
|------|---------|-------|
| Go | 1.25+ | See `go.mod` |
| make | GNU make | Build automation |
| git | Any | Clone the repo |
| apache2-utils | Any | `htpasswd` for vOps auth (optional) |

### Install

```bash
git clone https://github.com/vNodesV/vProx.git
cd vProx
make install
```

`make install` validates Go, creates `~/.vProx/` directories, decompresses the GeoIP database, installs sample configs, creates `.env`, and builds the binary to `$GOPATH/bin/vProx`.

### GeoIP database

The geo database is installed automatically by `make install`. To install or update it separately:

```bash
make geo
```

This extracts `assets/geo/ip2location.mmdb.gz` to `~/.vProx/data/geolocation/ip2location.mmdb`. No sudo required.

### Configure a chain

```bash
cp ~/.vProx/config/chains/chain.sample.toml ~/.vProx/config/chains/my-chain.toml
$EDITOR ~/.vProx/config/chains/my-chain.toml
```

### Run

```bash
vProx start             # foreground, listens on :3000 by default
vProx start -d          # start as systemd service (daemon)
vProx start --with-vops # start proxy + vOps in integrated mode
vProx stop              # stop the service
vProx restart           # restart the service
vProx --validate        # validate config and exit
vProx completion bash   # generate bash shell completion script
vProx completion zsh    # generate zsh shell completion script
vProx completion fish   # generate fish shell completion script
```

## Architecture

vProx follows a modular internal architecture with clearly separated concerns:

| Package | Responsibility |
|---------|---------------|
| `cmd/vprox` | CLI entrypoint, flag parsing, server lifecycle |
| `internal/config` | Chain and port TOML loading, validation |
| `internal/counter` | Per-IP access counter with disk persistence |
| `internal/logging` | Structured logging, typed request IDs (`RPC{hex}`, `API{hex}`) |
| `internal/metrics` | Prometheus metric registration and recording helpers |
| `internal/backup` | Scheduled log archival with tar.gz compression |
| `internal/geo` | MMDB geo lookup with in-memory cache |
| `internal/limit` | Token bucket rate limiter with auto-quarantine |
| `internal/ws` | WebSocket proxy with idle/lifetime timers |

**Data flow**: Incoming request → host-header match → chain config lookup → rate limiter → geo enrichment → reverse proxy → structured log + metrics.

For the full module-by-module reference, see [`MODULES.md`](./MODULES.md).

## vOps

vOps is a standalone companion binary for analyzing vProx log archives (renamed from vLog in v1.4.0). It provides a web-based dashboard with:

- Per-IP account profiles with request history and block/unblock controls
- Threat intelligence scoring (AbuseIPDB + VirusTotal + Shodan) — composite score 0–100
- OSINT engine: concurrent DNS, port scan, org/geo, protocol probe, Cosmos RPC (~5s)
- Multi-location endpoint probing (local + 🇨🇦 Canada + 🌍 worldwide)
- Dashboard authentication with bcrypt password hashing
- Machine-to-machine ingest API with API key auth

### Install and run

```bash
make install-vops        # build + install vOps binary + config
vprox vops start         # foreground server (default: :8889)
vprox vops start -d      # start as background service
vprox vops stop          # stop vOps service
vprox vops restart       # restart vOps service
vprox vops status        # show status and database stats
vprox vops ingest        # one-shot archive ingest
vprox vops accounts      # list IP accounts as JSON
vprox vops threats       # list flagged IPs (score ≥ 50)
vprox vops cache         # manage intel cache
```

For full setup including authentication, API key configuration, and block/unblock, see the [Installing vOps](./INSTALLATION.md#installing-vops) section in `INSTALLATION.md`.

## Configuration

vProx uses TOML configuration files stored under `~/.vProx/`:

| File | Purpose |
|------|---------|
| `config/ports.toml` | Default service ports for all chains; `vops_url` for integrated mode |
| `config/chains/*.toml` | Per-chain routing, services, and feature flags |
| `config/chains/*.sample` | Identity-only chain samples (`chain_id`, `network_type`, `tree_name`) |
| `config/backup/backup.toml` | Backup automation schedule and settings |
| `config/vops/vops.toml` | vOps server settings, auth, intel API keys |
| `.env` | Environment variables (rate limits, geo paths, server address) |

Override the config base path:

```bash
export VPROX_HOME=/opt/vprox
# or
vProx --home /opt/vprox
```

For the complete CLI flag reference, see [`CLI_FLAGS_GUIDE.md`](./CLI_FLAGS_GUIDE.md).

## Security

- Run vProx behind a TLS-terminating reverse proxy (nginx, Cloudflare).
- Set `trusted_proxies` in chain config to restrict X-Forwarded-For trust to known CIDR ranges.
- Keep vOps's `bind_address` set to `127.0.0.1` (the default).
- Rotate API keys regularly.

To report a vulnerability, see [`SECURITY.md`](./SECURITY.md).

## Documentation

| Document | Description |
|----------|-------------|
| [`INSTALLATION.md`](./INSTALLATION.md) | Full install guide: build, configure, systemd, auth, observability |
| [`MODULES.md`](./MODULES.md) | Module-by-module operations and configuration reference |
| [`CLI_FLAGS_GUIDE.md`](./CLI_FLAGS_GUIDE.md) | Complete CLI flag reference with examples |
| [`docs/UPGRADE.md`](./docs/UPGRADE.md) | Upgrade guide (v0.x → v1.x migration notes) |
| [`CHANGELOG.md`](./CHANGELOG.md) | Version history |
| [`SECURITY.md`](./SECURITY.md) | Security policy and vulnerability reporting |

## Contributing

Contributions are welcome. Please:

1. Open an issue describing the change before submitting a PR.
2. Ensure `make build` and `go test ./...` pass.
3. Maintain test coverage above the 60% gate.
4. Follow existing code style; `golangci-lint` runs on every PR.

## License

Apache-2.0. See [`LICENSE`](./LICENSE).
