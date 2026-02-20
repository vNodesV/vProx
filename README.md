# vProx

A Go-based reverse proxy for blockchain node services (RPC/REST/GRPC/GRPC-Web/API alias) with per-chain routing, optional virtual-host exposure, websocket support, HTML banner injection, and built-in rate limiting.

## ✨ Key features

- **Per-chain routing** by Host header and path prefixes (`/rpc`, `/rest`, `/grpc`, `/grpc-web`, `/api`).
- **Optional vhost exposure** (e.g., `rpc.<chain-host>`, `api.<chain-host>`).
- **WebSocket proxying** with idle timeout and max lifetime controls.
- **HTML banner injection** for RPC index and REST swagger pages.
- **Rate limiting** with auto-quarantine and JSONL logging.
- **Geo enrichment** (country/ASN) if MMDB databases are available.

## 📦 Requirements

- Go **1.25** (see `go.mod`)
- Optional MMDB databases for geo lookup (see **Geo DBs** below)

## ⚙️ Configuration

Configuration is **TOML-only**. By default, vProx uses `$HOME/.vProx` as its runtime home (override with `VPROX_HOME`).

**Directory Structure:**
```
$HOME/.vProx/
├── config/          # Global configuration
│   └── ports.toml   # Default port settings
├── chains/          # Per-chain configurations (*.toml)
├── data/
│   ├── geolocation/ # GeoLocation databases
│   ├── access-counts.json # persisted source access counters
│   └── logs/        # Application logs
│       ├── main.log
│       ├── rate-limit.jsonl
│       └── archives/
└── .env             # Environment variables
```

- **Default ports**: `$HOME/.vProx/config/ports.toml`
- **Per-chain config**: `$HOME/.vProx/chains/*.toml` (or `config/*.toml` for backward compatibility)

### Geo DBs (optional)

If you want geo lookups in logs, provide one of the following:

- **IP2Location / GeoLite2** MMDB (preferred)
  - Default locations tried:
    - `$HOME/.vProx/data/geolocation/ip2location.mmdb` (installed by `make install`)
    - `/usr/local/share/IP2Proxy/ip2location.mmdb`
    - `/usr/local/share/IP2Location/ip2location.mmdb`
    - `/usr/share/IP2Proxy/ip2location.mmdb`
    - `/usr/share/IP2Location/ip2location.mmdb`
    - `./ip2location.mmdb`
  - Or set `IP2LOCATION_MMDB` to an explicit path

- **GeoLite2** fallback MMDBs
  - `GEOLITE2_COUNTRY_DB`
  - `GEOLITE2_ASN_DB`

See `.env.example` for optional environment variables (geo + backup automation).

## ▶️ Run

From the repo root (dev):

- `go run ./cmd/vprox`
- `go run ./cmd/vprox start` (foreground, logs to stdout)

By default, vProx listens on **:3000** and routes based on the **Host** header.

## 🧱 Install (Linux)

The `make install` flow builds and installs the binary and sets up runtime folders so the service can run independent of the repo.

- `make install`

The geolocation database (if present in `ip2l/ip2location.mmdb`) will be automatically installed to `$HOME/.vProx/data/geolocation/`.

To generate the systemd unit file:

- `make systemd`

`make install` also renders a local unit file at:

- `$HOME/.vProx/service/vProx.service`

To use that unit on a systemd host, copy it and enable the service:

- `sudo cp $HOME/.vProx/service/vProx.service /etc/systemd/system/vProx.service`
- `sudo systemctl daemon-reload`
- `sudo systemctl enable vProx.service`
- `sudo systemctl start vProx.service`

To follow live service logs in CosmosSDK-style line output:

- `journalctl -u vProx.service -f --output=cat`

To run a manual backup of `main.log`:

- `vProx backup`
- `vProx --backup`
- `vProx backup --reset_count` (also accepts `--reset-count`)

## 🧪 Build

- `make build`

Or, if you prefer raw Go tooling (keeps artifacts out of the repo root):

- `go build -o ./.build/vProx ./cmd/vprox`

## 📂 Logs

- Main log: `$HOME/.vProx/data/logs/main.log`
- Rate limit events: `$HOME/.vProx/data/logs/rate-limit.jsonl`
- Access counter state: `$HOME/.vProx/data/access-counts.json`

## 🛡️ Rate limiting

vProx includes an IP-aware rate limiter with optional auto‑quarantine. It writes JSONL events to `$HOME/.vProx/data/logs/rate-limit.jsonl`.

Key behaviors:

- **Defaults** allow bursts and enforce 429 on overflow.
- **Auto‑quarantine** can temporarily override abusive IPs.
- **Log filtering** keeps only important events (429 / auto‑add / auto‑expire / canceled waits).

JSONL fields include: `ts`, `ip`, `country`, `asn`, `method`, `path`, `host`, `ua`, `reason`, `rps`, `burst`.

To change the log path, set `VPROX_HOME` or pass a custom path to `limit.WithLogPath` in code.

Rate limit tuning (optional, via `$HOME/.vProx/.env`):

- `VPROX_RPS` / `VPROX_BURST`
- `VPROX_AUTO_ENABLED`
- `VPROX_AUTO_THRESHOLD`, `VPROX_AUTO_WINDOW_SEC`
- `VPROX_AUTO_RPS`, `VPROX_AUTO_BURST`, `VPROX_AUTO_TTL_SEC`

## 🧩 Backups

Manual backup of `main.log`:

- `vProx backup`
- `vProx backup --reset_count` (resets persisted access counters before backup)

Automated backups are controlled via `.env` (loaded from `$HOME/.vProx/.env`):

- `VPROX_BACKUP_ENABLED=true`
- `VPROX_BACKUP_INTERVAL_DAYS=7` (optional)
- `VPROX_BACKUP_MAX_BYTES=52428800` (optional)
- `VPROX_BACKUP_CHECK_MINUTES=10`

Backups create `main.log.<timestamp>.tar.gz` in `$HOME/.vProx/data/logs/archives`.

Backup lifecycle emits structured lines to `main.log`/stdout (when running with `start`) using:
- message: `BACKUP STARTED`, `BACKUP COMPLETE`, `BACKUP FAILED`
- `request_id`, `status`, `filesize`, `compression`, `location`, `filename`, `archivesize`
- `failed=<reason>` on failures

Access source counters are persisted across restarts/backups and are only reset when explicitly requested with:
- `vProx backup --reset_count`
- `vProx backup --reset-count`

## 🧰 Notes

- Create your chain configs under `$HOME/.vProx/chains/` (a sample is provided at `chains/chain.sample.toml`).
- For backward compatibility, chain configs in `$HOME/.vProx/config/` are also loaded.
- If you change chain configs, restart the server.

## 🤝 Local collaboration artifacts

The following paths are intentionally local-only and ignored by git:

- `docs/`
- any `agents/` directory (for example `.github/agents/`)

They are used for session notes/agent coordination and are not part of distributable runtime source.

## 📚 Additional Documentation

- [`CLI_FLAGS_GUIDE.md`](./CLI_FLAGS_GUIDE.md) — complete command-line flag reference and examples.
- [`FLAGS.md`](./FLAGS.md) — quick flag reference.
- [`MODULES.md`](./MODULES.md) — module-by-module operations guide.
- [`MIGRATION.md`](./MIGRATION.md) — migration notes and compatibility guidance.

## 📄 License

Apache-2.0. See `LICENSE`.
