# vProx Project State
<!-- Managed by jarvis5.0 — append entries, never delete history -->

---

## Session: 2026-02-27 (develop branch) — UPDATED 15:38 UTC

### Active Branch
`develop` — HEAD: `54369c8`

### Recent Commits (last 10)
```
54369c8 vlog: refine TI icon thresholds — ⚠ U+26A0 for minor signals
c31b168 vlog: rebuild binary with embedded template fixes
dd945f2 vlog: TI badge icons, sortable tables, dashboard access count, dismiss fix
f47e75c fix(vlog): 4 UI/UX fixes — modal dismiss, score bar, unblock endpoint
a6af638 fix(vlog): always show Shodan row in TI table, 'not indexed' when no data
0563d45 fix(vlog): treat Shodan 404 'no data' as shodan_none not an error
bb3ac19 fix(vlog): fix SSE write-timeout killing enrichment + render Shodan on account page
64b32ab fix(vlog): render Shodan data on account page
d9ee138 chore(agents): save session state 2026-02-27 — block+UFW + Shodan migration
422a208 feat(vlog): block button + UFW integration + Shodan ns3777k migration
```

### Architecture Summary
- **vProx**: Go reverse proxy for Cosmos SDK nodes (RPC/REST/gRPC/WS). Binary: `vprox`.
- **vLog**: Standalone log archive analyzer with CRM-like IP accounts, intel enrichment, OSINT network scan. Binary: `vlog`. Web UI served via Apache reverse proxy.
- **Config**: `$VPROX_HOME/config/vlog/vlog.toml` (vLog), `config/chains/*.toml` (chain configs), `config/ports.toml` (ports + vlog_url hook).
- **DB**: SQLite via `modernc.org/sqlite` (pure Go, WAL mode). Tables: `ip_accounts`, `request_events`, `ratelimit_events`, `ingested_archives`, `intel_cache`.
- **Intel sources**: VirusTotal v3 + AbuseIPDB v2 + Shodan `/shodan/host/{ip}` + ip-api.com (free, no key).
- **OSINT scan**: port scan (7 ports), RDNS, ip-api.com org/country/ASN, protocol detection, Cosmos RPC probe.

### Key Conventions
- `exec.Command` (no shell) + `net.ParseIP()` validation for any OS-level calls
- SSE streaming pattern for long-running ops (EnrichStream, OSINTStream)
- `addColumnIfMissing()` for backward-compatible DB migrations
- `BasePath` prefix for Apache reverse-proxy sub-path serving
- `ip_accounts.status` values: `unknown`, `clean`, `suspicious`, `threat`, `blocked`
- `badge-{status}` CSS class drives color coding

### Completed This Session — 2026-02-27

#### Commits landed on `develop`
| SHA | Description |
|-----|-------------|
| `f84c836` | feat(vlog): ip-api.com org/country/ASN in OSINT scan + account UI redesign |
| `422a208` | feat(vlog): block button + UFW integration + Shodan ns3777k migration |

#### Track A — Block Button + UFW (claude-opus-4.6) ✅
- `internal/vlog/ufw/ufw.go` — NEW package: `Block/Unblock/IsAvailable`; `net.ParseIP` guard → `exec.Command` separate args (no shell); soft-fail if ufw not installed
- `internal/vlog/db/schema.go` — `blocked_ips` table (id, ip, blocked_at, reason, ufw_applied) + index
- `internal/vlog/db/queries.go` — `BlockedIP` struct; `BlockIP/UnblockIP/IsBlocked/ListBlockedIPs`
- `internal/vlog/web/handlers.go` — `handleAPIBlock` (POST) + `handleAPIUnblock` (DELETE)
- `internal/vlog/web/server.go` — `POST/DELETE /api/v1/block/{ip}`
- `account.html` — BLOCK IP / UNBLOCK button in header; `doBlock()` JS with `confirm()`; blocked-article banner
- `vlog.css` — `.btn-block`, `.btn-unblock`, `.badge-blocked`, `.blocked-article`
- `Makefile` — `make ufw-vlog` target → `/etc/sudoers.d/vlog` (interactive, mirrors `systemd` target UX)

Security model: `net.ParseIP()` + `exec.Command` separate args + sudoers exact command restriction.
Sudoers entry: `$(USER) ALL=(ALL) NOPASSWD: /usr/sbin/ufw deny from *, /usr/sbin/ufw delete deny from *`
Production deploy: run `make ufw-vlog` on server before first use.

#### Track B — Shodan ns3777k Migration (claude-sonnet-4.6) ✅
- `go.mod/go.sum` — `github.com/ns3777k/go-shodan/v4 v4.2.0` added
- `internal/vlog/intel/shodan.go` — hand-rolled HTTP replaced with library; `CheckShodan()` signature preserved; `ShodanResult` gains `Vulns []string` + `Services []ShodanService`; `rawJSON` via `json.Marshal(host)` (existing `ExtractShodanRiskFlags` string path unaffected); `CheckShodanSearch()` added for Membership plan search queries
- `internal/vlog/intel/score.go` — `ExtractRiskFlagsFromResult(*ShodanResult)` added — typed path, zero JSON re-parse

Library selection rationale: ns3777k over shadowscatcher — simpler `NewClient(httpClient, token)`, no built-in throttle (our `rate.Limiter` handles it), `HostData` has all needed fields, pure Go, `go-querystring` only dep.

#### OSINT Org/Country/ASN (f84c836)
- `internal/vlog/intel/osint.go` — `checkIPInfo()` via ip-api.com (free, no key, 45 req/min); `ipAPIResponse` struct; org/country/ASN in `OSINTResult` + `OSINTStream`; preserves Shodan values as fallback
- `account.html` — Account Details section first (ORG UPDATE), Threat Intelligence below; two-column `.detail-grid`; Cosmos node inline
- `vlog.css` — `.detail-grid` grid (1fr 1fr) + responsive 700px breakpoint

---

### In-Progress Work (this session)

#### Track A — Block Button + UFW ✅ DONE (422a208)
#### Track B — Shodan ns3777k Migration ✅ DONE (422a208)

### Open Follow-ups
- **Production deploy**: `make ufw-vlog` (installs sudoers entry) + `make vlog && sudo service vLog restart`
- **Shodan search UI**: `CheckShodanSearch()` is wired — future: threat hunting panel in vLog UI (Membership)
- **vProx-level IP deny list**: vLog block list → vProx polls via API or shared SQLite (future P4)
- **ip-api.com rate limit awareness**: 45 req/min free tier — consider backoff if enrichment becomes high-volume

---

---

## Session: 2026-02-27 Afternoon (develop branch)

### Completed This Session — 2026-02-27 (15:38 UTC snapshot)

#### TI UI Refinements — Commits `64b32ab` → `54369c8`

| SHA | Description |
|-----|-------------|
| `64b32ab` | fix(vlog): render Shodan data on account page |
| `bb3ac19` | fix(vlog): fix SSE write-timeout killing enrichment + render Shodan on account page |
| `0563d45` | fix(vlog): treat Shodan 404 'no data' as shodan_none not an error |
| `a6af638` | fix(vlog): always show Shodan row in TI table, 'not indexed' when no data |
| `f47e75c` | fix(vlog): 4 UI/UX fixes — modal dismiss, score bar, unblock endpoint |
| `dd945f2` | vlog: TI badge icons, sortable tables, dashboard access count, dismiss fix |
| `c31b168` | vlog: rebuild binary with embedded template fixes (go clean -cache required) |
| `54369c8` | vlog: refine TI icon thresholds — ⚠ U+26A0 for minor signals |

#### TI Icon Scale (final — 54369c8)
| Icon | Unicode | Source | Threshold |
|------|---------|--------|-----------|
| ✓ green | U+2713 | VT | 0 detections |
| ⚠ yellow | U+26A0 | VT | 1–2 (minor/unconfirmed) |
| ! yellow | literal | VT | 3–9 (suspicious) |
| ! red | literal | VT | ≥10 (high risk) |
| ✓ green | U+2713 | AbuseIPDB | 0–25 |
| ⚠ yellow | U+26A0 | AbuseIPDB | 26–49 (low confidence) |
| ! red | literal | AbuseIPDB | ≥50 |
| ✓/⚠/! | — | Score | 0–19 / 20–49 / ≥50 |
| ◎ grey | U+25CE | Shodan | always shown; neutral recon indicator |

#### Key Bug Fixed: go embed cache invalidation
Root cause: `go build` used stale cache when only `go:embed` files changed.
Fix: `go clean -cache` before build forces full recompile. Added to internal workflow knowledge.

#### UX patterns established
- SSE streaming for long ops (enrichment, OSINT) → `handleEnrichStream`, `handleOSINTStream`
- "Dismiss & Refresh" modal pattern (no auto-close) → page reload shows updated TI card
- Unblock via `POST /api/v1/unblock/{ip}` (not DELETE — Apache strips DELETE bodies)
- Shodan 404 → `shodan_none` status (not error); always renders "not indexed" row in TI table

### Open Follow-ups
- **Production deploy**: `make ufw-vlog` + `make vlog && sudo service vLog restart`
- **Shodan search UI**: `CheckShodanSearch()` wired — future threat hunting panel (Membership plan)
- **vProx-level IP deny list**: vLog block list → vProx polls via API or shared SQLite (future P4)
- **ip-api.com rate limit**: 45 req/min free tier — add backoff if enrichment volume grows
- **Sortable tables**: dashboard table now sortable (dd945f2) — expand to accounts list page

---

## Session: 2026-02-27 evening — vLog v1.1.0 accounts overhaul + prod fixes

### HEAD
`c0bd61a` (vLog/v1.1.0) — SSE keepalive for Apache idle timeout

### Branch
`vLog/v1.1.0` (branched from `develop` @ `4cb7c8c`)

### Work Completed

#### 1. Accounts Page Full Overhaul (8ac3057 on develop)
| Change | Details |
|--------|---------|
| Rename | "IP Accounts" → "Accounts" sitewide |
| Org column | Far-left column; populated from ip-api.com |
| Investigate button | Replaces Status badge; runs TI + OSINT via SSE |
| Search bar | Top-right, server-side LIKE on ip/country/rowid; `q=` param |
| Per-page dropdown | 25/50/100/200; `parsePagination` max raised 100→200 |
| Sort spinner | `initSortableTable(tableId, spinnerId)` with requestAnimationFrame |
| Typography | Inter font, 14px base, professional CSS overhaul |
| Dual-phase SSE | `ti:*` events (0-50%), `osint:*` events (50-100%) |
| Files changed | `db/queries.go`, `handlers.go`, `server.go`, `accounts.html`, `base.html`, `vlog.css` |

#### 2. Apache 403 Root Cause (config-only fix)
- **Cause**: empty `base_path` in vlog.toml → templates emit `/static/vlog.css` → Apache LocationMatch `^/static/` blocks it
- **Fix**: set `base_path = "/vlog"` in `~/.vProx/config/vlog/vlog.toml`
- No code change required

#### 3. Four Bug Fixes (9440612 on develop)
| Bug | Root Cause | Fix |
|-----|-----------|-----|
| Sort broken (ReferenceError) | `initSortableTable` defined at end of `<body>`, called earlier in content block | Moved `<script>` definition to `<head>` |
| Investigate not populating DB | `r.Context()` cancelled by Apache proxy timeout mid-save | Switched to `context.Background()` for both streams |
| Font size | 13px base too small | Bumped to 14px |
| Green button | No visual distinction for already-investigated IPs | `.btn-investigate-done` CSS class; applied when `IntelUpdatedAt != ""` |

#### 4. Prod Server Go Build Fix (4cb7c8c on develop)
- **Problem**: prod GOROOT (`/home/vnodesv/go`) has corrupted stdlib source; dev uses GOTOOLCHAIN auto-download
- **Fix**: Makefile auto-detects clean toolchain via `find $GOPATH/pkg/mod/golang.org -name 'toolchain@*'` and sets `EFFECTIVE_GOROOT`; all `go build` calls prefixed `GOROOT="$(EFFECTIVE_GOROOT)"`
- No `go env -w` / no persistent state change; falls back to GOROOT on clean installs
- `validate-go` prints `↳ using clean toolchain:` when override is active

#### 5. agents/ gitignore (fc895e8 on vLog/v1.1.0)
- Old pattern `agents/*.*` missed subdirectories
- Changed to `agents/` (full subtree)
- Removed 14 previously-tracked agent files from git index
- `.github/agents/` remains tracked

#### 6. SSE Keepalive (c0bd61a on vLog/v1.1.0)
- **Problem**: Apache idle connection timeout fires during silent gap between EnrichStream and OSINTStream phases → `context canceled` in osint log, Network Error on client
- **Fix**: Background goroutine sends SSE comment (`: ping`) every 15s for handler lifetime; goroutine stopped cleanly via done channel + defer close
- `context.Background()` + keepalive = operations complete + connection stays alive through Apache

### Architecture Notes

#### SSE Investigate Handler Pattern
```
handleAPIInvestigate:
  → keepalive goroutine (15s `: ping`, done-channel shutdown)
  → EnrichStream(context.Background(), ip) → emitPhase("ti")  [0-50%]
  → OSINTStream(context.Background(), ip)  → emitPhase("osint")[50-100%]
  Client: ReadableStream (POST) — NOT EventSource (GET-only)
  Phase prefix on Step field; Pct scaled per phase
```

#### Makefile GOROOT Resolution
```makefile
_TOOLCHAIN_GOROOT := $(shell find $(GOPATH)/pkg/mod/golang.org -maxdepth 1 -name 'toolchain@*' | sort -V | tail -1)
EFFECTIVE_GOROOT  := $(if $(_TOOLCHAIN_GOROOT),$(_TOOLCHAIN_GOROOT),$(GOROOT))
# All go build: GOROOT="$(EFFECTIVE_GOROOT)" go build ...
```

#### Button Color Logic
- `IntelUpdatedAt` (string, RFC3339) is selected in `ListIPAccounts` query
- Template: `{{if .IntelUpdatedAt}}btn-investigate-done{{end}}`
- CSS: `.btn-investigate-done` → green (`#198754`) variant

### Open Follow-ups
- **Prod deploy**: `git pull origin vLog/v1.1.0 && make install-vlog && sudo service vLog restart`
- **Investigate populate**: confirm DB rows update after keepalive fix resolves the OSINTStream abort
- **OSINTUpdatedAt in ListIPAccounts**: currently NOT selected (only IntelUpdatedAt is); add if OSINT-only green button needed separately
- **vLog v1.1.0 PR**: merge `vLog/v1.1.0` → `develop` → `main`
- **Shodan search UI**: threat hunting panel (future, requires Shodan membership)
- **vProx IP deny list integration**: vLog block list → vProx polling (future P4)
