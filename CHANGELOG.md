# Changelog

All notable changes to vProx are documented here.

Format follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/).
Versions follow [Semantic Versioning](https://semver.org/).

---

## [v1.2.0] — 2026-03-01

This release ships **vProx v1.2.0** and **vLog v1.0.0** together as **vProxVL v1.2.0**.

### Added — vLog v1.0.0

- **vLog module**: standalone log archive analyzer binary (`vlog`)
  - SQLite database (`$VPROX_HOME/data/vlog.db`) for IP accounts, request events, and rate-limit events
  - Ingests vProx log archives (`*.tar.gz`) from `$VPROX_HOME/data/logs/archives` — oldest-first, with deduplication via `ingested_archives` table
  - Background FS watcher for automatic ingestion of new archives
  - **IP Security Assessment**: AbuseIPDB v2 + VirusTotal v3 + Shodan — composite threat score (0–100); parallelized (3 concurrent goroutines); ~10s vs former ~30s
  - **OSINT engine**: 5 concurrent ops (DNS, port scan, ip-api.com, protocol probe, Cosmos RPC) via `sync.WaitGroup`; ~5s vs former ~23s
  - CRM-like IP account profiles with threat flags, notes, enrichment history, block/unblock status
  - **Accounts page**: server-side search (IP/country/row ID), per-page selector (25/50/100/200/All), sortable columns with URL-based sort persistence (back-nav safe), Status column (ALLOWED/BLOCKED), Org lookup via ip-api.com
  - **Dashboard**: dual-line Chart.js request charts; standalone endpoint status panel with 3 probe columns (Local | 🇨🇦 | 🌍), CSS spinner, node hover tooltips
  - **Multi-location endpoint probe** (`GET /api/v1/probe`): local SSRF-guarded probe discovers reachable URL; concurrent CA (Vancouver) + worldwide probes via check-host.net HTTP-check API (submit + poll); response: `{host, url, local, ca, ww}` per-location result with `{ok, code, latency_ms, error, node}`
  - REST API: `/api/v1/ingest`, `/api/v1/accounts`, `/api/v1/probe`, `/api/v1/enrich/:ip`, `/api/v1/osint/:ip`, `/api/v1/investigate/:ip`, `/api/v1/stats`, `/api/v1/block/:ip`, `/api/v1/unblock/:ip`, `/api/v1/chart`
  - CLI: `vlog start [-d]`, `vlog stop`, `vlog restart`, `vlog ingest`, `vlog status`
  - vProx integration: optional POST to vLog after `--new-backup` via `vlog_url` in `config/ports.toml`
  - Config: `$VPROX_HOME/config/vlog.toml` (sample: `config/vlog/vlog.sample.toml`)
- **`modernc.org/sqlite v1.46.1`** — pure-Go SQLite driver (no CGO required)

### Added — vProx v1.2.0

- Chain log auto-discovery: `--new-backup` auto-includes all `*.log` files from `data/logs/` (except `main.log`); per-chain logs included without manual declaration
- vLog push hook: vProx POSTs to `vlog_url` after `--new-backup` (non-fatal if vLog unreachable)
- Typed request IDs: `RPC{24HEX}`, `API{24HEX}`, `REQ{24HEX}` stamped on every proxied request (vhost + alias routes included)

### Changed — vProx v1.2.0

- **Chain config format refactored** (`config/chains/*.toml`):
  - `msg = bool` split into `msg_rpc = bool` and `msg_api = bool` (independent per-service banner control)
  - `[aliases]` sub-table removed; replaced by flat top-level `rpc_aliases`, `rest_aliases`, `api_aliases` string arrays
  - `features.inject_rpc_index` renamed to `features.rpc_address_masking`
  - `features.inject_rest_swagger` removed
  - `features.mask_rpc string` added (replacement label for masked local-IP links; empty = remove)
  - `features.swagger_masking bool` added (reserved; not yet implemented)
  - `[ports]` section now explicitly noted as optional when `default_ports = true`
  - `config/backup.sample.toml` default changed to `automation = false` (safe opt-in default)
- Banner injection bug fixed: `msg_rpc`/`msg_api` flags now correctly gate banner content; address masking (`rpc_address_masking`) operates independently of banner flags

### Fixed — vProx v1.2.0

- Request ID missing on vhost-mode and alias routes (api.*, grpc) — now always assigned before log
- REST probe path stripped `/api/` prefix incorrectly — now probes `/cosmos/base/tendermint/v1beta1/node_info` directly
- Banner (`rpc_msg`) injected even when `msg = false` — root cause: injection gated on `InjectRPCIndex` only, ignoring `Msg` flag; now fully decoupled

---

## [v1.0.2] — included in v1.2.0

### Added
- `internal/logging`: `NewTypedID(prefix)` — generates `{PREFIX}{24HEX_UPPER}` correlation IDs (API, RPC, WSS, BUP, etc.)
- `internal/logging`: `LineLifecycle()` / `PrintLifecycle()` — `NEW`/`UPD` structured lifecycle log format (no event token; fields-first)
- `internal/backup/config.go` — `BackupConfig` structs, `DefaultConfig()`, `LoadConfig()` for `backup.toml`
- `config/backup/backup.sample.toml` — annotated backup config; installed by `make config`
- CLI commands: `start`, `stop`, `restart` with `runServiceCommand()` → `sudo service vProx start|stop|restart`
- CLI flag: `-d` / `--daemon` — start as systemd service
- CLI flags: `--new-backup`, `--list-backup`, `--backup-status`
- Makefile `systemd:` target creates `/etc/sudoers.d/vprox` for passwordless service management
- Unified structured log format across all modules:
  - **API/RPC requests**: `NEW ID=API{hex} status=COMPLETED method=GET from=IP count=N to=HOST endpoint=/PATH latency=Xms userAgent=... country=XX module=vProx`
  - **WebSocket connect**: `NEW ID=WSS{hex} status=CONNECTED ... module=vProx` (emitted at handshake completion)
  - **WebSocket close**: `UPD ID=WSS{hex} status=CLOSED reason=IDLE duration=Xs upload=XMiB download=XMiB averageRate=XMiB/s module=ws`
  - **Backup start**: `NEW ID=BUP{hex} status=STARTED method=AUTO|MANUAL timestamp=... compression=TAR.GZ source=... list=loaded|default to=... size=... module=backup`
  - **Backup done**: `UPD ID=BUP{hex} status=COMPLETED location=... compressedSize=... module=backup`

### Changed
- `logRequestSummary`: migrated from `Line("INFO","access","request",...)` to `LineLifecycle("NEW","vProx",...)` with renamed fields (`from`, `count`, `to`, `endpoint`, `latency`, `userAgent`) and uppercase values; `pathPrefix()` helper derives ID prefix from URL path
- `ws.HandleWS`: WSS ID (`WSS{hex}`) generated at connection entry and set via `X-Request-ID` header; `LogRequestSummary` moved to post-handshake (emits CONNECTED); session-end `applog.Print` replaced by `PrintLifecycle("UPD",...)`
- `internal/backup/backup.go`: `newBupID()`, multi-file `writeTarGz`, rewritten `RunOnce`, extended `Options` (Method/ExtraFiles/ListSource), `StartAuto` sets `Method=AUTO`
- `cmd/vprox/main.go`: loads `backup.toml`, `resolveBackupExtraFiles` helper, wires config into both `RunOnce` and `StartAuto`; env vars still override TOML values
- Backup automation driven solely by `backup.toml` `automation` bool (removed `VPROX_BACKUP_ENABLED` env var)
- Chain sample moved from `chains/chain.sample.toml` → `config/chains/chain.sample.toml`
- Makefile `config` target installs chain and backup samples to `config/chains/` and `config/backup/`
- Makefile no longer creates legacy `$HOME/.vProx/chains/` directory (legacy dir still scanned if present)

### Removed
- `VPROX_BACKUP_ENABLED` env var — backup automation now controlled solely by `backup.toml`
- `internal/backup/cfg/config.json` and `config.toml` — dead legacy config files

### Fixed
- **P0** `gzipResponseWriter.WriteHeader()` committed response headers before `Content-Encoding: gzip` was set; status code is now buffered and forwarded after headers are finalized
- **P0** Per-request disk I/O: `saveAccessCountsLocked()` did JSON marshal + atomic write on every request while holding mutex. Moved to 1-second background ticker with dirty flag
- **P1** `intToBytes` produced empty output for negative integers (`for i > 0` loop); replaced with `strconv.Itoa`
- **P1** `Forwarded` header parser split on `;` before `,`; failed for multi-hop proxy chains. Now splits by comma (hops) first, then semicolon (params) per RFC 7239
- **P1** Rate limiter `sync.Map` entries (`pool`, `autoState`, `lastAllowLog`) never evicted; ~270 bytes/IP unbounded growth. Added 5-minute sweeper goroutine
- **P1** `io.ReadAll` on upstream HTML response with no size limit; OOM risk. Wrapped with `io.LimitReader(reader, 10<<20)`
- **P2** `rewriteLinks` compiled regexes per request on hot path; now cached per (IP, host) pair
- **P2** `geo.Close()` did not reset `sync.Once`; geo permanently disabled after close. Now resets init guard for hot-reload
- **P2** WebSocket `hardTimer` called `cConn.Close()`/`bConn.Close()` from timer goroutine while pump goroutines still running (gorilla/websocket not concurrent-safe). Replaced with done-channel coordination
- **P3** `clientIP()` returned raw header values without validation; log injection risk. Added `net.ParseIP` validation
- **P3** `ip2lPaths` evaluated `os.Getenv("HOME")` at package init; missed later `VPROX_HOME` override. Moved to `initDB()` resolution
- **P3** Geo cache entries only evicted on re-access; slow unbounded growth. Added periodic 5-minute sweep

### Planned (P4 — feature improvements)
- Move `access-counts.json` to `data/logs/` + include in backup tar.gz
- Webserver CLI subcommands: `vProx webserver new|list|validate|remove`
- Makefile: "Install vProx WebServer? {y/N}" prompt + `make install webserver`
- `.env` `[WebServer]` section with `AUTO_START` boolean
- Config architecture: `vprox.toml` (proxy), `webserver.toml` (webserver module), per-host `~/.vProx/vhosts/*.toml`
- Analyze separate systemd service for webserver module
- Explore web GUI for vProx/vProxWeb management

---

## [v1.0.1-beta] — 2026-02-22

### Added
- `approval-gate.yml` — unified PR approval workflow; `/approve` comment from `@vNodesV` triggers approval after all CI checks pass
- `INSTALLATION.md` — comprehensive install guide (build, configure, systemd, troubleshoot)
- `docs/UPGRADE.md` — upgrade guide for v0.x → v1.x migrations (replaces MIGRATION.md)
- `CHANGELOG.md` — this file

### Changed
- `ip2l/ip2location.mmdb` → `ip2l/ip2location.mmdb.gz` — MMDB compressed (17 MB → 6.8 MB; 60% clone size reduction)
- `Makefile` `geo` target — now decompresses `.gz` instead of copying uncompressed file
- `README.md` — rewritten as concise project overview (~50 lines); links to INSTALLATION.md and MODULES.md
- `MODULES.md` — expanded to full operations reference (490+ lines); integrates CLI flags quick reference; fixes `make GEO=true install` documentation error
- `.gitignore` — added `ip2l/ip2location.mmdb` rule; added `!docs/UPGRADE.md` exception

### Removed
- `required-reviewer.yml` — replaced by `approval-gate.yml`
- `jb-auto-approve.yml` — replaced by `approval-gate.yml`
- `FLAGS.md` — content integrated into `MODULES.md §9`
- `MIGRATION.md` — moved to `docs/UPGRADE.md`

### Security
- Approval workflow now requires all CI checks (build/test/lint, CodeQL, Dependency Review) to pass before any review can be submitted; unauthorized approval attempts are silently rejected

---

## [v1.0.0] — 2026-02-20

### Added
- Initial public release
- Per-chain TOML config (path and vhost routing modes)
- HTTP/WebSocket reverse proxy (`gorilla/websocket`)
- IP-based rate limiting with auto-quarantine (`golang.org/x/time/rate`)
- Geo enrichment via IP2Location / GeoLite2 MMDB (`oschwald/geoip2-golang`)
- Structured dual-sink logging (stdout + `main.log`)
- JSONL rate-limit audit log with backward-compatible field aliases
- Automated log backup with copy-truncate semantics
- Access counter persistence across restarts (`access-counts.json`)
- `make install` — full install: binary, directories, geo DB, .env, systemd unit
- `vprox.service.template` — systemd unit template
- `.env.example` — environment variable reference
- `chains/chain.sample.toml` — annotated chain configuration template
