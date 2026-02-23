# Changelog

All notable changes to vProx are documented here.

Format follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/).
Versions follow [Semantic Versioning](https://semver.org/).

---

## [v1.0.2] — unreleased

### Added
- `internal/webserver` — opt-in embedded virtual-host HTTPS server
  - SNI-based TLS (single `:443` listener, `GetCertificate`)
  - HTTP/2 auto-enabled via `net/http`
  - Middleware chain: `securityHeaders → cors → headerManip → gzip → (proxy | static)`
  - Gzip middleware with buffered status-code to prevent premature header commit (P0 fix)
  - CORS origin reflection — exact-match list; `Vary: Origin` added for non-wildcard configs (P0 fix)
  - Proxy+static fallback with proxy-header clear before static response (P1 fix)
  - `redirectSrv` (`:80`) and `httpsSrv` (`:443`) wired into graceful shutdown (P1 fix)
- `config/vhost.sample.toml` — annotated reference configuration
- `MODULES.md §10` — webserver module reference
- `INSTALLATION.md §10` — vhost.toml migration guide and troubleshooting table
- `webserver_test.go` — 3 regression tests: gzip WriteHeader ordering, CORS multi-origin reflection, proxy→static header leak

### Fixed
- **P0** `gzipResponseWriter.WriteHeader()` committed response headers before `Content-Encoding: gzip` was set; status code is now buffered and forwarded after headers are finalized
- **P0** `corsMiddleware` emitted comma-joined `Access-Control-Allow-Origin` value for multi-origin configs; browsers reject non-single-value ACAO; now reflects matching request origin
- **P0** Proxy handler double `WriteHeader`: `w.WriteHeader(resp.StatusCode)` committed before gzip reader setup; error path wrote second header. Status now deferred until after gzip init
- **P0** Per-request disk I/O: `saveAccessCountsLocked()` did JSON marshal + atomic write on every request while holding mutex. Moved to 1-second background ticker with dirty flag
- **P1** `proxyWithStaticFallback` leaked upstream proxy headers (e.g. `Set-Cookie`, `X-Upstream-*`) into static fallback responses; cleared before handoff
- **P1** Webserver listeners excluded from graceful shutdown; fixed via `var wsServers []*http.Server` collected before goroutine launch
- **P1** `intToBytes` produced empty output for negative integers (`for i > 0` loop); replaced with `strconv.Itoa`
- **P1** `Forwarded` header parser split on `;` before `,`; failed for multi-hop proxy chains. Now splits by comma (hops) first, then semicolon (params) per RFC 7239
- **P1** Rate limiter `sync.Map` entries (`pool`, `autoState`, `lastAllowLog`) never evicted; ~270 bytes/IP unbounded growth. Added 5-minute sweeper goroutine
- **P1** `io.ReadAll` on upstream HTML response with no size limit; OOM risk. Wrapped with `io.LimitReader(reader, 10<<20)`
- **P1** `VHostConfig.HTTPRedirect` forced `true` when TLS present, overriding user's explicit `false`. Changed to `*bool` to distinguish omission from explicit false
- **P2** `rewriteLinks` compiled regexes per request on hot path; now cached per (IP, host) pair
- **P2** `geo.Close()` did not reset `sync.Once`; geo permanently disabled after close. Now resets init guard for hot-reload
- **P2** WebSocket `hardTimer` called `cConn.Close()`/`bConn.Close()` from timer goroutine while pump goroutines still running (gorilla/websocket not concurrent-safe). Replaced with done-channel coordination
- **P2** Per-vhost `http.Transport` never closed on shutdown; idle connections leaked. Transports now tracked and cleaned up in `WebServer.Shutdown()`
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
