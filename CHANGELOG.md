# Changelog

All notable changes to vProx are documented here.

Format follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/).
Versions follow [Semantic Versioning](https://semver.org/).

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
