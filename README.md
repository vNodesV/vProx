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

- **Default ports**: `$HOME/.vProx/config/ports.toml`
- **Per-chain config**: `$HOME/.vProx/config/*.toml`

### Geo DBs (optional)

If you want geo lookups in logs, provide one of the following:

- **IP2Location** MMDB (preferred)
  - Default locations tried:
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

- `go run .`

By default, vProx listens on **:3000** and routes based on the **Host** header.

## 🧱 Install (Linux)

The `make install` flow builds and installs the binary and sets up runtime folders so the service can run independent of the repo.

- `make install`

Optional Geo DB copy (if you have `ip2l/ip2location.mmdb` in the repo):

- `make GEO=true install`

To generate the systemd unit file:

- `make systemd`

Then enable and start the service (Linux):

- `sudo systemctl daemon-reload`
- `sudo systemctl enable vprox`
- `sudo systemctl start vprox`

To run a manual backup of `main.log`:

- `vProx backup`
- `vProx --backup`

## 🧪 Build

- `go build -o vprox .`

## 📂 Logs

- Main log: `$HOME/.vProx/logs/main.log`
- Rate limit events: `$HOME/.vProx/logs/rate-limit.jsonl`

## 🛡️ Rate limiting

vProx includes an IP-aware rate limiter with optional auto‑quarantine. It writes JSONL events to `$HOME/.vProx/logs/rate-limit.jsonl`.

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

Automated backups are controlled via `.env` (loaded from `$HOME/.vProx/.env`):

- `VPROX_BACKUP_ENABLED=true`
- `VPROX_BACKUP_INTERVAL_DAYS=7` (optional)
- `VPROX_BACKUP_MAX_BYTES=52428800` (optional)
- `VPROX_BACKUP_CHECK_MINUTES=10`

Backups create `main.log.<timestamp>.tar.gz` in `$HOME/.vProx/logs/archived`.

## 🧰 Notes

- Create your chain configs under `$HOME/.vProx/config` (use `make config` to generate a template).
- If you change chain configs, restart the server.

## 📄 License

Apache-2.0. See `LICENSE`.
