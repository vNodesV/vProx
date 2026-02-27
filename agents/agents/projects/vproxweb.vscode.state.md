# project state: vProxWeb (embedded webserver module)

Updated: 2026-02-22

## Project summary

- **Parent project**: vProx (`github.com/vNodesV/vProx`)
- **Feature branch**: `feat/webserver-module`
- **Target version**: v1.0.2
- **Goal**: Embed a full-featured HTTP/HTTPS web server into vProx so Apache2/nginx are no longer required.
- **Architecture shift**: `Internet → Apache2 (:443) → vProx (:3000) → Cosmos nodes`
  becomes `Internet → vProx (:80 redirect + :443 TLS SNI) → Cosmos nodes`

---

## Tech stack (additions to base vProx)

- `net/http` (stdlib) — HTTP/2 auto-enabled on TLS, ServeMux host-pattern routing
- `crypto/tls` — SNI-based multi-cert TLS, TLS 1.2+ min, `GetCertificate` callback
- `compress/gzip` — lazy response compression (compressible types only)
- `net/http/httputil` — `ReverseProxy` for proxy vhosts
- `github.com/pelletier/go-toml/v2` — already in go.mod; used for `vhost.toml`

---

## Module layout

```
internal/webserver/
  config.go          — Config/VHostConfig/TLSConfig/CORSConfig/HeaderConfig/SecurityConfig
                       LoadConfig(path): absent file → empty config (backward compat)
                       applyDefaults(): index=index.html, proxy_timeout_sec=30
                       validate(): no backend+root, TLS pair check, duplicate host check
  webserver.go       — WebServer struct, Mounts() []Mount
                       middleware chain: securityHeaders→cors→headerManip→gzip→(proxy|static)
                       gzipResponseWriter: lazy compression, Flusher impl (no ReadFrom — avoids ∞ recursion)
                       statusRecorder: proxy→static 404 fallback
                       headerManipWriter: strip/inject response headers
  tls.go             — BuildTLSConfig(mounts) → (*tls.Config, *CertStore, error)
                       CertStore: map[host]*tls.Certificate + sync.RWMutex
                       GetCertificate: SNI dispatch, fallback to first cert
                       Reload(): hot cert refresh (future SIGHUP trigger)
  redirect.go        — NewRedirectHandler() → 301 http→https preserving host+path+query
  webserver_test.go  — 14 tests, all passing ✅

config/
  vhost.sample.toml  — Real-world sample based on Apache2 config analysis
                       Covers: elys-srvs, cheqd-srvs, cheqd-ws, cronos-srvs, vnodesv.net static, template
```

## Modified files (relative to `develop` base)

| File | Change |
|------|--------|
| `internal/webserver/config.go` | New |
| `internal/webserver/webserver.go` | New |
| `internal/webserver/tls.go` | New |
| `internal/webserver/redirect.go` | New |
| `internal/webserver/webserver_test.go` | New |
| `config/vhost.sample.toml` | New |
| `cmd/vprox/main.go` | Added ~60-line webserver startup block + `webserver` import |
| `Makefile` | Added vhost.toml install step to `config:` target |

---

## vhost.toml design decisions

- Optional file — absent → empty config, no error (backward compat; `:3000` behavior unchanged)
- `[[vhost]]` TOML array of tables — one block per virtual host
- Proxy-only (`backend` set), static-only (`root` set), or both (proxy first, 404 → static)
- `http_redirect` always `true` when TLS cert+key present (bool zero-value limitation)
- Single `:443` listener with SNI — one port serves all vhosts

## Key implementation notes

- **HTTP/2**: auto-enabled by `net/http` when TLS is active — no extra code
- **gRPC h2c upstream**: deferred to Phase 2 (requires `golang.org/x/net/http2` h2c transport)
- **gzip `ReadFrom` bug**: REMOVED — `io.ReaderFrom` impl caused infinite recursion via `io.Copy`
- **HTTPS mux dispatch**: `http.ServeMux` with `"host/"` patterns (native host routing)
- **Rate limiting**: existing `lim.Middleware` wraps the vhost mux

## Real-world Apache2 architecture (from config analysis)

Active vhosts mapped (relevant to vProxWeb):

| Domain | Mode | Backend |
|--------|------|---------|
| `elys.srvs.vnodesv.net` | Proxy | `http://127.0.0.1:3000` (vProx) |
| `cheqd.srvs.vnodesv.net` | Proxy | `http://127.0.0.1:3000` |
| `cronos.srvs.vnodesv.net` | Proxy | `http://127.0.0.1:3000` |
| `vnodesv.net` / `www.vnodesv.net` | Static | `/var/www/html` |
| `.fr.vnodesv.net` chains | Proxy direct | `10.0.0.11`, `10.0.0.53` (bypass vProx) |

Inactive/deferred: ModSecurity, load balancing, brotli, auth, `status.fr.vnodesv.net`

---

## Project conventions (webserver-specific)

- Logging: `applog.Print("LEVEL", "module", "event", applog.F("key", val))`
- TOML: `github.com/pelletier/go-toml/v2`
- Config absent = graceful degradation, not fatal error
- Tests use `httptest.NewRecorder` + `httptest.NewRequest` (no live listeners)
- Defaults applied only via `LoadConfig` → `applyDefaults()`, not in `New()`

---

## Known follow-ups / open issues

1. **Graceful shutdown** — `:80` and `:443` servers are goroutines not wired to `ctx` shutdown flow. Add to `cleanup()` or server slice in `main.go`.
2. **gRPC h2c upstream (Phase 2)** — `ProxyPass h2c://10.0.0.11:9090/` requires `golang.org/x/net/http2` h2c transport for `.fr.vnodesv.net` chains.
3. **Direct backend vhosts (Phase 2)** — `.fr.vnodesv.net` chains bypass vProx; add direct backend support in vhost.toml.
4. **SIGHUP cert hot-reload** — `CertStore.Reload()` is implemented; just needs signal wiring in `main.go`.
5. **IP-restricted `/status` endpoint** — `status.fr.vnodesv.net` currently Apache mod_status; needs equivalent in vProx.
6. **Integration tests** — HTTPS mux host-based routing needs live TLS test (no certs available locally to test yet).
7. **`applyDefaults` in `New()`** — tests that construct `Config` directly skip defaults; consider calling `applyDefaults` in `New()` too.

---

## Remaining work before PR

- [ ] Fix graceful shutdown wiring in `main.go` (`:80` + `:443` into `ctx`)
- [ ] `MODULES.md` — add `§ webserver` section
- [ ] `INSTALLATION.md` — add `vhost.toml` migration section
- [ ] `CHANGELOG.md` — add v1.0.2 entry
- [ ] Commit and open PR → `develop`

---

## Known files NOT to touch

- `internal/limit/`, `internal/geo/`, `internal/ws/`, `internal/logging/`, `internal/backup/` — existing modules, out of scope
- `chains/*.toml`, `.env`, `config/ports.toml` — existing runtime config, backward compat preserved

---

## Session Memory Dumps

### 2026-02-22 22:03 UTC — vProxWeb bootstrap + full implementation

- **Goal**: Embed a standalone webserver into vProx to replace Apache2. TLS termination, SNI per vhost, HTTP→HTTPS redirect, gzip, CORS, header management, static file serving, proxy mode.
- **Completed**:
  - Full Apache2 config analysis (`/Users/sgau/Downloads/etc/apache2/`) — real architecture reverse-engineered
  - `internal/webserver/config.go` — Config structs + LoadConfig + validate + applyDefaults
  - `internal/webserver/webserver.go` — WebServer, Mounts(), full middleware chain (gzip/CORS/header/security), proxy handler, static handler, proxy+static fallback
  - `internal/webserver/tls.go` — BuildTLSConfig, CertStore, SNI dispatch, Reload
  - `internal/webserver/redirect.go` — HTTP→HTTPS 301 handler
  - `internal/webserver/webserver_test.go` — 14 tests, all passing ✅
  - `config/vhost.sample.toml` — real-world sample based on Apache2 analysis
  - `cmd/vprox/main.go` — webserver import + startup block
  - `Makefile` — vhost.toml install step
  - **Bug fixed**: `gzipResponseWriter.ReadFrom` caused infinite recursion via `io.Copy`; method removed
  - `go build ./...` ✅ | `go test ./internal/webserver/...` 14/14 ✅
- **Files changed**: `internal/webserver/*` (5 files new), `config/vhost.sample.toml` (new), `cmd/vprox/main.go` (modified), `Makefile` (modified)
- **Verification**: `go build ./...` clean; `go test ./internal/webserver/... -v` 14 PASS
- **Open follow-ups**: graceful shutdown wiring, Phase 2 (h2c, direct vhosts, SIGHUP), docs (MODULES.md, INSTALLATION.md, CHANGELOG.md), PR to develop
- **Next first steps**:
  1. Fix graceful shutdown in `main.go`
  2. Write docs (MODULES.md §webserver, INSTALLATION.md, CHANGELOG.md v1.0.2)
  3. `git commit` + `gh pr create --base develop`

### 2026-02-22 22:30 (UTC) — agent self-evaluation + model routing setup
- **Goal**: Self-evaluate jarvis5.0 knowledge gaps; close them in agent MD files; configure model routing policy.
- **Completed**:
  - `agents/base.agent.md`: Header updated from "jarvis4.0" to version-agnostic; session protocol routes to correct state file per agent variant.
  - `.github/agents/jarvis5.0.agent.md` (Copilot): Fixed scope framing (vProx = Go reverse proxy, NOT Cosmos SDK app); added Scientific Rigor + Agility subsections; 8-item Done Criteria checklist; 7-step Execution Workflow + DS extension signals; added **Copilot Runtime Context** (Tool Access, GitHub MCP Tools, Model Routing Policy, Sub-Agent Delegation Protocol); added `model` session command.
  - `.github/agents/jarvis5.0_vscode.agent.md` (VSCode): Fixed Scope framing; added `reviewer.agent.md` to Supporting Files.
  - `.github/agents/jarvis4.0_vscode.agent.md`: Go version `1.23.8+` → `Go 1.25 / toolchain go1.25.7`.
  - `AGENT_DIRECTIVE.md`: Operating mode `jarvis4.0` → `jarvis5.0`.
  - `agents/jarvis5.0_skills.md`: GitHub Actions (2→4 ✅), Release automation (2→3 ✅); added Section 12 (AI Agent Orchestration); updated capability index.
  - `agents/jarvis5.0_resources.md`: Added Section 13 (AI/LLM tools), Section 14 (Release automation + eBPF); updated Quick Domain Lookup.
  - `agents/jarvis5.0_state.md`: Added `model` command with 8-row routing table + sub-agent defaults; rev2 in upgrade history.
  - **Model Routing Policy**: 8-row table + quick 3-rule summary + sub-agent defaults with explicit `model:` parameter encoded in both agent file and state file.
- **Files changed**:
  - `agents/base.agent.md`
  - `agents/jarvis5.0_skills.md`
  - `agents/jarvis5.0_resources.md`
  - `agents/jarvis5.0_state.md`
  - `.github/agents/jarvis5.0.agent.md`
  - `.github/agents/jarvis5.0_vscode.agent.md`
  - `.github/agents/jarvis4.0_vscode.agent.md`
  - `AGENT_DIRECTIVE.md`
- **Verification**: Section headers verified via `grep "^##\|^###"`; scope framing confirmed; model routing consistent across agent file and state file.
- **Open follow-ups**:
  - Mirror `model` command into `jarvis5.0_vscode_state.md`.
  - vProxWeb backlog: graceful shutdown, Phase 2 (h2c, direct vhosts), docs, PR to develop.
- **Next first steps**:
  1. Resume vProxWeb: fix graceful shutdown in `main.go` → `model arch` → `claude-opus-4.6`.
  2. Write docs (MODULES.md §webserver, INSTALLATION.md, CHANGELOG.md v1.0.2).
  3. `git commit` + `gh pr create --base develop`.
