# vProx

Production-grade reverse proxy for Cosmos SDK node services — RPC, REST, gRPC, WebSocket, and API alias — with per-chain routing, rate limiting, geo enrichment, and automated log management.

## ✨ Features

- **Per-chain routing** — match by Host header; route to path prefixes (`/rpc`, `/rest`, `/grpc`, `/grpc-web`, `/api`) or subdomains (`rpc.<host>`, `api.<host>`).
- **WebSocket proxying** — bidirectional with configurable idle timeout and max lifetime.
- **Rate limiting** — per-IP token bucket with optional auto-quarantine and JSONL audit log.
- **Geo enrichment** — country and ASN logged per request via MMDB lookup (optional).
- **HTML banner injection** — custom banners on RPC index and REST swagger pages.
- **Backup automation** — TOML-configured scheduled backups with multi-file archive support.
- **Service management** — `start -d`, `stop`, `restart` with passwordless sudoers integration.
- **Systemd-ready** — `make install` renders and optionally installs the service unit + sudoers rule.

## 📦 Requirements

- Go **1.25+** (see `go.mod`)
- Linux (for production/systemd); macOS supported for development

## ⚡ Quick Start

```bash
git clone https://github.com/vNodesV/vProx.git
cd vProx
make install
```

Then create a chain config:

```bash
cp $HOME/.vProx/config/chains/chain.sample.toml $HOME/.vProx/config/chains/my-chain.toml
$EDITOR $HOME/.vProx/config/chains/my-chain.toml
```

Start the proxy:

```bash
vProx start           # foreground, listens on :3000 by default
vProx start -d        # start as systemd service (daemon)
vProx stop            # stop the service
vProx restart         # restart the service
```

## 🔍 vLog — Log Archive Analyzer (v1.0.0)

vLog is a companion binary that analyzes vProx log archives and provides a CRM-like security intelligence UI.

```bash
make install-vlog           # build + install vLog binary + config
vlog start                  # foreground server (default: :8889)
vlog start -d               # start as background service
vlog stop                   # stop the service
vlog restart                # restart the service
vlog ingest                 # one-shot archive ingest and exit
vlog status                 # show database stats
```

**Features:**
- Per-IP accounts with request history, rate-limit events, block/unblock status
- Threat scoring (VirusTotal + AbuseIPDB + Shodan) — parallel queries, composite score 0–100
- Full OSINT suite — concurrent DNS, port scan, org/geo, protocol probe, Cosmos RPC (~5s)
- Live investigation UI with SSE progress streams
- **Multi-location endpoint probe** — local + 🇨🇦 Canada + 🌍 worldwide nodes (check-host.net), latency in ms, hover tooltips
- Accounts page: search, sort (URL-persistent, back-nav safe), per-page 25–All, Status column

See [`MODULES.md §11`](./MODULES.md#11-vlog--log-archive-analyzer) for full documentation.

## 📚 Documentation

| Document | Description |
|---|---|
| [`INSTALLATION.md`](./INSTALLATION.md) | Full install guide: build, configure, systemd, troubleshoot |
| [`MODULES.md`](./MODULES.md) | Module-by-module operations and configuration reference |
| [`CLI_FLAGS_GUIDE.md`](./CLI_FLAGS_GUIDE.md) | Complete CLI flag reference with examples |
| [`docs/UPGRADE.md`](./docs/UPGRADE.md) | Upgrade guide (v0.x → v1.x migration notes) |
| [`CHANGELOG.md`](./CHANGELOG.md) | Version history |

## 📄 License

Apache-2.0. See [`LICENSE`](./LICENSE).
