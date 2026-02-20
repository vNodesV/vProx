# vProx Modules & Operations Guide

This document covers **all modules** in vProx, how to configure them, and how to operate them in production.

## 1) Core Proxy (main)

**Purpose**: Loads chain configs, routes requests, rewrites links, injects banners, and applies rate limiting.

### Runtime layout (default)

By default, vProx runs out of:

- `$HOME/.vProx/config` — chain configs and `ports.toml`
- `$HOME/.vProx/data/logs` — `main.log`, `rate-limit.jsonl`, `archives/` backups
- `$HOME/.vProx/data` — backup state, geo DBs

Override base path with:

- `VPROX_HOME=/custom/path`
- CLI: `vProx --home /custom/path`

### Chain configs

Location:

- `$HOME/.vProx/config/*.toml`

Required fields:

- `chain_name`, `host`, `ip`
- `services` (at least one enabled)

Optional:

- `aliases` (rpc/rest/api hostnames)
- `expose` (path or vhost routing)
- `ports` (if `default_ports=false`)
- `features` (banner injection, absolute links)
- `logging.file` for per‑chain logs

### Default ports

Location:

- `$HOME/.vProx/config/ports.toml`

### Run

- `vProx` — start server (default `:3000`)
- `vProx --addr :4000` — override listen address

### Manual backup

- `vProx backup` (shorthand)
- `vProx --backup`

---

## 2) Rate Limiter (`internal/limit`)

**Purpose**: Per‑IP rate limiting with optional auto‑quarantine. Logs JSONL events.

### Logs

- `$HOME/.vProx/logs/rate-limit.jsonl`

### Tuning via `.env`

```ini
VPROX_RPS=25
VPROX_BURST=100
VPROX_AUTO_ENABLED=true
VPROX_AUTO_THRESHOLD=120
VPROX_AUTO_WINDOW_SEC=10
VPROX_AUTO_RPS=1
VPROX_AUTO_BURST=1
VPROX_AUTO_TTL_SEC=900
```

### JSONL fields

`ts`, `ip`, `country`, `asn`, `method`, `path`, `host`, `ua`, `reason`, `rps`, `burst`

---

## 3) WebSockets (`internal/ws`)

**Purpose**: Proxies `/websocket` to backend RPC websocket.

### Behavior

- Honors `ws.idle_timeout_sec` (default 3600)
- Honors `ws.max_lifetime_sec` (0 means unlimited)

---

## 4) Geo (`internal/geo`)

**Purpose**: Country/ASN enrichment for logs using MMDB databases.

### MMDB search order

**IP2Location (preferred):**

- `/usr/local/share/IP2Proxy/ip2location.mmdb`
- `/usr/local/share/IP2Location/ip2location.mmdb`
- `/usr/share/IP2Proxy/ip2location.mmdb`
- `/usr/share/IP2Location/ip2location.mmdb`
- `./ip2location.mmdb`

Or explicitly set:

- `IP2LOCATION_MMDB=/path/to/ip2location.mmdb`

**GeoLite2 fallback:**

- `GEOLITE2_COUNTRY_DB=/path/to/GeoLite2-Country.mmdb`
- `GEOLITE2_ASN_DB=/path/to/GeoLite2-ASN.mmdb`

---

## 5) Backup (`internal/backup`)

**Purpose**: Archive `main.log` with compression and rotation logic.

### Manual backup

```bash
vProx backup
# or
vProx --backup
```

### Automated backups

Enable and tune via `$HOME/.vProx/.env`:

```ini
VPROX_BACKUP_ENABLED=true
VPROX_BACKUP_INTERVAL_DAYS=7
VPROX_BACKUP_MAX_BYTES=52428800
VPROX_BACKUP_CHECK_MINUTES=10
```

Backups are written to:

- `$HOME/.vProx/logs/archived/main.log.<timestamp>.tar.gz`

### Backup behavior

1) Copy `main.log` → `main.log.<timestamp>`
2) Truncate `main.log`
3) Create `main.log.<timestamp>.tar.gz`
4) Delete the copy
5) Move archive to `logs/archived/`

---

## 6) Config & Install (Makefile)

**Install binary** (repo independent):

```bash
make install
```

**Create a template config**:

```bash
make config
```

**Optional GEO DB copy** (expects `ip2l/ip2location.mmdb` in repo):

```bash
make GEO=true install
```

**Systemd service**:

```bash
make systemd
sudo systemctl daemon-reload
sudo systemctl enable vprox
sudo systemctl start vprox
```

---

## 7) Per‑chain logging

If a chain config includes:

```toml
[logging]
file = "logs/chain-name.log"
```

vProx writes the summary log lines to **both** `main.log` and the chain file. Relative paths resolve under `$VPROX_HOME`.

---

## 8) Troubleshooting

- **Unknown host**: check `host` in chain config and ensure the request Host header matches.
- **No configs found**: confirm `$HOME/.vProx/config/*.toml` exists and that `ports.toml` is present.
- **Geo not loading**: verify your MMDB paths and file sizes.
- **Rate limit too strict**: adjust `.env` values under rate limiting.
