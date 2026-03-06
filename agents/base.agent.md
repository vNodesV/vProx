# vProx agent base directives (local-only)
**Compatible with**: jarvis4.0, jarvis5.0, jarvis5.0_vscode

This file stores cross-project collaboration rules.
Project-specific memory goes in `agents/projects/<project>.state.md`.

## Start-of-session protocol
1. Read the active agent's router state file:
   - jarvis5.0 (Copilot): `agents/jarvis5.0_state.md`
   - jarvis5.0_vscode: `agents/jarvis5.0_vscode_state.md`
   - jarvis4.0: `agents/jarvis4.0_state.md`
2. Read this file (`base.agent.md`).
3. Do not auto-load project state.
4. Load project memory only on explicit `load <project>`.
5. Confirm unresolved work before editing code.

## End-of-session protocol
- If user says `save` / `save state`: append a memory dump to current project file.
- If user says `save new <project>`: create `agents/projects/<project>.state.md` from template and set current project in router.
- If user says `new`: run guided bootstrap flow from router policy (`Create new repo? (y/N)` branch).
- If user says `agentupgrade`: run full self-assessment and upgrade protocol (inventory → assess → context → patch → verify → report).
- Keep entries concise and action-oriented.

## Engineering discipline
- Small, testable changes.
- Read enough context before patching.
- Reuse project-native patterns.
- Follow decision priority stack:
  1) state safety/backward compatibility
  2) security correctness
  3) build/test reliability
  4) performance/operability
  5) developer experience
- Validate frequently:
  - `gofmt` touched files
  - `go build ./...`
  - `go test ./...` when behavior changes
- Fix root causes.
- Treat log schema changes as compatibility-sensitive.

## Established patterns (prefer these over inventing new ones)
- **Goroutine shutdown**: Use done-channel (`close(done)`) not direct Close() from other goroutines.
- **TOML tri-state**: Use `*bool` when need to distinguish "not set" from "false" in TOML config.
- **Background batching**: dirty-flag + ticker pattern for periodic I/O (not per-request writes).
- **sync.Map lifecycle**: Always pair sync.Map with a sweeper goroutine to prevent unbounded growth.
- **IP header validation**: Always `net.ParseIP()` untrusted header values before logging/using.
- **Regex caching**: Cache compiled regexes keyed by input params; protect with `sync.RWMutex`.
- **Embedded web GUI**: Use `html/template` + `go:embed` + htmx for admin dashboards; no JS framework, single-binary deployment.
- **CLI dual-output**: Use `splitLogWriter{stdout, file}` for any CLI command that must appear in both the terminal and journalctl (e.g. `--backup`, `start`); file-only output is invisible to systemd journal.
- **TOML > .env priority**: TOML config files are the source of truth for all runtime settings; `.env` is for deployment secrets and per-environment overrides only. When both exist, TOML wins.
- **SSE keepalive**: All SSE handlers use a background goroutine sending `: ping\n\n` every 15s (done-channel shutdown via `defer close(kaDone)`); always pass `context.Background()` (not `r.Context()`) to streaming operations — Apache `ProxyTimeout` cancels `r.Context()` before long operations finish. Keepalive interval (15s) must be less than `ProxyTimeout` (60s). Applied in vLog: `handleAPIInvestigate`, `handleAPIEnrich`, `handleAPIosint`.
- **Service management**: Use `runServiceCommand(action)` → `sudo service vProx <action>` for daemon start/stop/restart. Never call `systemctl` directly from Go code. Sudoers NOPASSWD setup via `make systemd` grants passwordless access to `/usr/sbin/service vProx start|stop|restart`.
- **External HTTP probe (check-host.net)**: Submit GET `https://check-host.net/check-http?host=URL&node=NODE` (Accept: application/json) → receive `request_id`; poll `/check-result/{id}` every 2s up to 12s. Result shape per node: `[[status_int, latency_secs_float, msg_str, code_str|null, ip_str|null]]`. `status==1` → success; `row[1]` → latency; `row[3]` (string) → HTTP code. Verified live nodes at `/nodes/hosts` endpoint. Run CA+WW concurrently with `sync.WaitGroup`. Applied in vLog `handleAPIProbe`.
- **Static probe columns**: In HTML tables, use 3 separate `<td class="probe-local|probe-ca|probe-ww">` columns per row (not inline `<span>`). Insert `<span class="probe-spinner">` (CSS `@keyframes` ring) during loading. Set `cell.title` for hover tooltip. Find cells via `btn.closest('tr').querySelector('.probe-*')`.
- **SSE writer serialization**: `http.ResponseWriter` is NOT safe for concurrent use. SSE handlers that have a keepalive goroutine AND an emit() path MUST serialize all writes with a `sync.Mutex` (or a single-writer goroutine pattern). Failure to do so produces corrupt SSE framing. This applies to every handler in `internal/vlog/web/handlers.go` that uses the 15s keepalive pattern.
- **WebSocket hardening**: After every `upgrader.Upgrade()` call, immediately set `conn.SetReadLimit(n)` on both the client and backend connections to prevent OOM from unbounded frames (e.g., `1<<20` for client, `4<<20` for backend). Track active connection count with `sync/atomic`; reject upgrades when over a configurable max. Validate `Origin` header against allowed hosts instead of `CheckOrigin: always true`.
- **Trusted proxy CIDR**: Rate limiter and IP detection logic MUST only honor proxy headers (`CF-Connecting-IP`, `X-Forwarded-For`, `Forwarded`) when `r.RemoteAddr` matches a configured trusted proxy CIDR allowlist (e.g., Cloudflare IP ranges, `127.0.0.1`). Without this, any client can spoof IP and bypass per-IP rate limiting.
- **Admin server loopback bind**: Any HTTP server that serves admin/management endpoints (vLog, future GUI) MUST bind to `127.0.0.1:<port>`, NOT `0.0.0.0`. Apache/nginx proxies the public path. Binding to all interfaces bypasses the Apache IP-restriction layer.
- **API auth middleware**: Every HTTP endpoint that mutates state (block/unblock, ingest trigger, config changes) MUST be wrapped with an auth middleware (API key header check: `X-VLog-Key`) regardless of the assumed network boundary. Apply with: `mux.HandleFunc("POST /api/v1/block/{ip}", s.requireAPIKey(s.handleAPIBlock))`.
- **Error response sanitization**: NEVER return `err.Error()` in HTTP JSON responses. Database errors, file path errors, and SQLite diagnostics MUST stay server-side in logs. Return `{"error":"internal error"}` (or a fixed human-readable message) to the client. Pattern: `log.Printf("block %s: %v", ip, err); writeJSON(w, 500, errResp("internal error"))`.
- **io.LimitReader on external responses**: Every `io.ReadAll(resp.Body)` on an outbound HTTP response (VirusTotal, AbuseIPDB, Shodan, check-host.net) MUST be wrapped: `io.ReadAll(io.LimitReader(resp.Body, 1<<20))`. Unbounded reads are DoS-ready if the external API is compromised or misconfigured. Exception: already-bounded cases like `io.ReadFull`.
- **Backup write order (data safety)**: In `internal/backup/backup.go`, NEVER truncate or delete source files until AFTER `writeTarGz` returns nil AND the archive file has been `os.Stat`-verified. Truncating before the write is a data loss footgun — if the write fails (disk full, permission), the only copies of the data are gone. Pattern: `if err := writeTarGz(...); err != nil { return err }; truncateSources(...)`.
- **SSRF private-IP guard**: Any handler that accepts an IP from user input and makes outbound network connections (TCP probe, HTTP GET, DNS lookup) MUST validate: `net.ParseIP(ip) != nil && !parsed.IsPrivate() && !parsed.IsLoopback() && !parsed.IsLinkLocalUnicast() && !parsed.IsUnspecified()`. Applies to: `handleAPIosint`, `handleAPIEnrich`, any future probe endpoint.
- **Configurable DC probe (check-host.net country routing)**: `countryNodes map[string][]string` maps ISO 3166-1 alpha-2 codes (CA/US/FR/DE/NL/GB/UK/FI/JP/SG/BR/IN) to specific check-host.net node hostnames. `sanitizeProbeNode(s string) string` whitelists `provider` query param values against the same map (SSRF guard — only known nodes allowed). `handleAPIProbe` priority: provider (whitelist-checked) → country→node → fallback CA node. Frontend passes `?country=c.ping_country&provider=c.ping_provider` per-chain. Applied in `internal/vlog/web/handlers.go`.
- **Collapsible dashboard blocks**: Wrap each block `<article class="v-block"><details open><summary onclick="if(event.target.closest('button')){event.preventDefault()}">...</summary><div class="v-block-body">...</div></details></article>`. The `onclick` guard prevents action buttons inside `<summary>` from toggling the block. CSS: `.v-block-title` for header text, `.v-block-actions` for button group in summary, `summary::-webkit-details-marker {display:none}` + `::before` custom chevron ▶/▼. Pattern applied to all 4 vLog dashboard blocks.
- **vcol/hcol block toggle (dashboard)**: vcol = vertical collapse via dedicated `∧`/`∨` button in `<summary>`; button manually sets `d.open = !d.open` (onclick guard blocks natural toggle); icon flips `∧` ↔ `∨`. hcol = horizontal expand via `›`/`‹` buttons; toggles `.row-expand-left`/`.row-expand-right` class on `.v-blocks-row` (CSS: `3fr 1fr`/`1fr 3fr`); collapsed block gets `.is-strip` class — hides `<details>`, shows vertical pill label; `.v-block.is-strip` must set `width:44px; justify-self:center` to stay narrow in the wider column. Toggle listener IIFE reflows grid on `<details>` toggle events (`auto 1fr` or `1fr auto`) for vcol. Pill click calls `collapseBlockRow()` → restores `1fr 1fr`.
- **Dashboard drag/drop layout**: HTML5 `dragstart`/`dragover`/`drop` on outer master blocks; drag triggered only from `.v-drag-handle` (⠿ icon in summary); `dragHandle` guard in `dragstart`. `localStorage.setItem('vlog-block-order', JSON.stringify([...ids]))` persists order. Reset Layout button restores default order + clears `localStorage`. Inner drag (per-subblock) removed — only master-block movement.
- **Sample file revision schema**: Each sample TOML gets a header comment `# revision: rev{M}.{m}.{p}-{commit7}` where `{commit7}` is the first 7 chars of the current git commit SHA. `make samples-push` rewrites all 4 sample files (`vlog.sample.toml`, `vms.sample.toml`, `backup.sample.toml`, `chain.sample.toml`) via a shell recipe that reads `$(shell git rev-parse --short=7 HEAD)` and stamps the revision header.
- **go:embed cache invalidation**: `go build` uses the stale compile cache when only `go:embed` files change (templates, CSS, static assets). Always run `go clean -cache` before building when only embedded files were modified; alternatively use `go build -a` to force full recompile. Applied to vLog binary rebuilds.
- **Cosmos `/health` vs `/status` routing**: Use `/health` (returns 200 OK, zero cost, no state) for liveness probes; reserve `/status` for sync detection (`sync_info.catching_up` boolean). Polling `/status` per-request is wasteful — cache with 10-30s TTL.
- **Cosmos upgrade pre-failover**: Cache `/cosmos/upgrade/v1beta1/current_plan` with 60s TTL. When `latest_block_height >= Plan.Height` → pre-failover validator to standby node before halt. Poll `/module_versions` post-upgrade to detect version mismatches.
- **IBC /channels DoS protection**: `/ibc/core/channel/v1/channels` has no built-in pagination on all chain versions → can return unbounded responses. Always enforce `?pagination.limit=N` at proxy level for any IBC channel/packet query. Route to dedicated query nodes, never to validators.
- **broadcast_tx_commit circuit breaker**: `broadcast_tx_commit` blocks on event subscription until `timeout_broadcast_tx_commit` (default 10s). When node reports `MaxSubscriptionClients` exceeded, proxy MUST fall back to `broadcast_tx_sync` rather than queuing indefinitely. Track per-node subscription saturation.
- **Cosmos WS subscription pooling**: CometBFT defaults: `max_subscription_clients=100`, `max_subscriptions_per_client=5`. Proxy should pool WS connections and share subscriptions across client connections. Return clear error to clients when limits would be exceeded. WS ping period ~27s — proxy keepalive must flush within this window.
- **ABCI prove= routing**: `abci_query?prove=true` generates merkle proofs (CPU/IO expensive). `prove=false` is cheap key-value lookup. Route `prove=true` requests exclusively to query-only replica nodes; never to validators or primary serving nodes.
- **dump_consensus_state rate limit**: `/dump_consensus_state` marshals all peer consensus states — most expensive RPC in CometBFT. Rate-limit to 1 req/min per IP at proxy level and never cache (contains live peer state).
- **Go/JSON nil-vs-empty slice**: `var s []T` marshals to `null`; `s := make([]T, 0)` marshals to `[]`. API responses MUST use `make([]T, 0)` (or literal `[]T{}`) for empty collections to avoid breaking frontend `Array.isArray()` / `.length` checks. Applied in vLog: `7b149e0` fixed push/chains and push/vms endpoints.
- **Dashboard JS IIFE debugging**: Each dashboard block has its own IIFE. Cross-IIFE communication uses `window.fnName = fnName` exports. When deleting a JS function, ALWAYS delete its `window.*` export — stale exports cause `ReferenceError` at parse time, crashing the entire IIFE (no partial execution). Debug via browser DevTools Console; check CSP `connect-src` if API calls fail silently. Pattern learned from `76ed378` (stale `window.addChain`).
- **Soft migration (dual-source config loading)**: When consolidating config files (e.g., vms.toml → chain.toml `[management]`), load BOTH sources. New source wins when present; old source provides fallback. Log deprecation warning once per old-source load. Never hard-cut — always provide at least one release cycle where both work. Pattern: `LoadFromChainConfigs()` → `MergeConfigs(chainVMs, legacyVMs)`. Applied in v1.3.0 vms.toml→chain.toml consolidation.
- **Chain.toml self-contained config**: Each chain file should be self-contained: chain identity, endpoints, management (VM host/SSH), ping (probe country/provider), and explorer — no cross-file references. Global defaults (SSH user, key_path) go in the parent config (e.g., `[vlog.push.defaults]`). Per-chain overrides in `[management]` section. Pattern locks in v1.3.0.

## Quality gates (minimum)
- Expected behavior and constraints are explicit before patching.
- Root cause is identified (not only symptoms).
- All touched files are formatted.
- Build succeeds after changes.
- Tests run at appropriate scope for the change.
- Behavior/config docs updated when needed.

## Uncertainty protocol
- If multiple viable outcomes exist, present options with risks and recommendation.
- Ask for confirmation when path choice changes behavior or compatibility.
- Prefer smallest safe change that preserves existing functionality.

## Session completion standard
- End with: changed files, verification performed, open follow-ups, and next first steps.

## Memory dump format
Each save entry should include:
- timestamp (UTC)
- goal
- completed work
- files changed
- verification
- open follow-ups
- next first steps
