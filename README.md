# vProx

Production-grade reverse proxy for Cosmos SDK node services — RPC, REST, gRPC, WebSocket, and API alias — with per-chain routing, rate limiting, geo enrichment, and automated log management.

## ✨ Features

- **Per-chain routing** — match by Host header; route to path prefixes (`/rpc`, `/rest`, `/grpc`, `/grpc-web`, `/api`) or subdomains (`rpc.<host>`, `api.<host>`).
- **WebSocket proxying** — bidirectional with configurable idle timeout and max lifetime.
- **Rate limiting** — per-IP token bucket with optional auto-quarantine and JSONL audit log.
- **Geo enrichment** — country and ASN logged per request via MMDB lookup (optional).
- **HTML banner injection** — custom banners on RPC index and REST swagger pages.
- **Log management** — structured logs + automated compressed backup rotation.
- **Systemd-ready** — `make install` renders and optionally installs the service unit.

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
cp $HOME/.vProx/chains/chain.sample.toml $HOME/.vProx/chains/my-chain.toml
$EDITOR $HOME/.vProx/chains/my-chain.toml
```

Start the proxy:

```bash
vProx start        # listens on :3000 by default
```

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
