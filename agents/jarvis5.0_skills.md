# jarvis5.0 Skill Taxonomy
**Version**: 5.0  
**Format**: Domain → Skill → Depth (1=aware, 2=proficient, 3=expert, 4=PhD/research)

---

## 1. Systems Engineering

### 1.1 Go
| Skill | Depth | Notes |
|-------|-------|-------|
| Standard library | 4 | net/http, sync, context, io, encoding, os, testing, runtime |
| Goroutines & channels | 4 | Lifecycle, deadlock analysis, select patterns, channel direction |
| Go memory model | 4 | Happens-before, atomic, sync.Mutex/RWMutex |
| Concurrency patterns | 4 | Background tickers, sync.Map sweepers, done-channel coordination, dirty-flag batching |
| Interfaces & composition | 4 | Embedding, duck typing, interface pollution detection |
| Error handling | 4 | errors.Is/As, wrapping, sentinel errors, custom types |
| Generics (1.18+) | 3 | Type constraints, type sets, instantiation |
| Testing | 4 | Table-driven, subtests, TestMain, testify, fuzzing (go-fuzz) |
| Benchmarking | 4 | -bench, -benchmem, -cpuprofile, pprof flame graphs |
| CGo | 2 | Awareness; avoid unless required |
| go:generate / embed | 3 | Code generation patterns, fs.FS |
| Build tags | 3 | //go:build, GOOS/GOARCH constraints |
| Module system | 4 | go.mod, replace, retract, workspace mode |

### 1.2 Rust (CosmWasm)
| Skill | Depth | Notes |
|-------|-------|-------|
| Ownership & borrowing | 4 | Lifetimes, borrow checker, NLL |
| Traits | 4 | Orphan rule, blanket impls, dyn vs impl Trait |
| Error handling | 3 | thiserror, anyhow, Result combinators |
| Async (tokio) | 3 | Future, Pin, waker, runtime |
| Unsafe | 3 | Justification discipline, invariants, UB catalog |
| CosmWasm patterns | 4 | Contract lifecycle, messages, state, CW-multi-test |
| Cargo tooling | 3 | workspace, features, profiles, audit |

### 1.3 Shell / Ops
| Skill | Depth | Notes |
|-------|-------|-------|
| Bash scripting | 3 | set -euo pipefail, trap, heredocs, parameter expansion |
| Make | 3 | Pattern rules, .PHONY, automatic variables |
| jq / yq | 3 | JSON/YAML transforms, streaming |
| systemd | 4 | Unit files, ExecStart, RestartPolicy, SyslogIdentifier, journalctl capture, service command delegation, sudoers NOPASSWD setup for service management |
| git | 4 | Rebase, reflog, bisect, worktrees, hooks |
| Docker / OCI | 3 | Multi-stage builds, layer caching, non-root user |

---

## 2. Cosmos SDK / Blockchain

| Skill | Depth | Notes |
|-------|-------|-------|
| Cosmos SDK v0.50.x | 4 | Module system, keeper, ante handlers, ABCI |
| CometBFT v0.38 | 4 | Consensus, mempool, RPC, P2P; WS subscription limits (100/5), ping period ~27s |
| IBC-go v8.x | 4 | Channels, packets, light clients; `/channels` DoS risk (no pagination), proxy enforcement |
| CosmWasm v2.x | 4 | Contract patterns, migrations, gas optimization |
| Protobuf / gRPC | 3 | .proto design, buf tooling, gRPC-gateway; reflection endpoint leaks schema |
| Tendermint RPC | 4 | Endpoints, WS subscription/JSONRPC; `/health` vs `/status` routing; expensive: `/dump_consensus_state` |
| Cosmos REST/API | 4 | REST routes, pagination, protobuf JSON; gov/evidence/upgrade module patterns |
| Chain upgrade flow | 4 | Software upgrade proposals; `/current_plan` proxy caching + halt-height failover pattern |
| State compatibility | 4 | Backward-compatible store migrations |
| Cosmos proxy intelligence | 4 | ABCI `prove=true` routing, `broadcast_tx_commit` circuit breaker, mempool health via RPC, WS pool mgmt |

---

## 3. Data Science (PhD Level)

### 3.1 Statistics
| Skill | Depth | Notes |
|-------|-------|-------|
| Probability theory | 4 | Distributions, PDFs, CDFs, moment generating functions |
| Frequentist inference | 4 | Hypothesis testing, p-values, Type I/II errors, power |
| Bayesian inference | 4 | Prior/posterior, MCMC, conjugate distributions |
| Regression analysis | 4 | OLS, logistic, ridge/lasso, GLM, mixed models |
| Non-parametric methods | 3 | Mann-Whitney, Kruskal-Wallis, bootstrap |
| Time series | 4 | ARIMA, SARIMA, exponential smoothing, Granger causality |
| Survival analysis | 3 | Kaplan-Meier, Cox proportional hazards |
| Causal inference | 4 | DAGs, do-calculus, instrumental variables, DID |
| Experimental design | 4 | RCT, A/B testing, factorial design, power analysis |

### 3.2 Machine Learning
| Skill | Depth | Notes |
|-------|-------|-------|
| Supervised learning | 4 | Linear/tree/SVM/neural classifiers & regressors |
| Unsupervised learning | 3 | K-means, DBSCAN, GMM, PCA, ICA |
| Ensemble methods | 4 | Random forest, XGBoost, LightGBM, stacking |
| Neural networks | 3 | MLP, CNN, RNN/LSTM, attention, transformers (applied) |
| Model evaluation | 4 | CV, AUC/ROC, calibration, bias-variance trade-off |
| Feature engineering | 4 | Encoding, scaling, interaction features, selection |
| Hyperparameter tuning | 3 | Grid, random, Bayesian (Optuna) |
| Anomaly detection | 4 | Z-score, IQR, isolation forest, OCSVM, COPOD |
| Recommender systems | 2 | Collaborative filtering, matrix factorization |

### 3.3 Data Engineering
| Skill | Depth | Notes |
|-------|-------|-------|
| Data pipeline design | 4 | ETL vs ELT, idempotency, schema evolution |
| Streaming patterns | 3 | At-least-once, exactly-once, watermarks, windowing |
| Data modeling | 4 | 3NF, star schema, data vault, wide tables |
| SQL | 4 | Window functions, CTEs, query planning, index design |
| Go data tools | 3 | database/sql, sqlx, GORM (avoid magic), custom scanners |
| Schema validation | 3 | JSON Schema, Protobuf, Avro, schema registry |

### 3.4 Applied DS for vProx / Infrastructure
| Skill | Depth | Notes |
|-------|-------|-------|
| Rate limiting analysis | 4 | Token bucket math, traffic distribution modeling |
| Anomaly detection in logs | 4 | JSONL event stream analysis, burst detection |
| Geo-distribution analysis | 3 | IP-to-country, ASN-based traffic segmentation |
| Access pattern modeling | 4 | Time-series decomposition of node access counts |
| Performance benchmarking | 4 | Statistical significance, load test design |
| Capacity planning | 3 | Resource modeling, headroom, forecast |

---

## 4. Observability & Performance

| Skill | Depth | Notes |
|-------|-------|-------|
| Structured logging | 4 | JSON, JSONL, log levels, correlation IDs, sampling |
| Metrics (Prometheus) | 3 | Counter/gauge/histogram, alerting, Grafana |
| Distributed tracing | 3 | OpenTelemetry, span propagation, trace context |
| Go profiling | 4 | pprof (CPU, heap, goroutine, mutex, block), trace |
| Flame graphs | 3 | Interpretation, hotspot identification |
| Load testing | 3 | vegeta, k6, wrk; statistical result analysis |
| Benchmarking | 4 | go test -bench, -benchmem, microbenchmark pitfalls |
| SLI/SLO design | 3 | Error budget, availability calculation |

---

## 5. Security Engineering (Defensive)

| Skill | Depth | Notes |
|-------|-------|-------|
| Threat modeling | 4 | STRIDE, data flow diagrams, attack surface mapping, trust boundary identification |
| OWASP Top 10 | 4 | Injection, broken auth, SSRF, XSS, IDOR — and reverse-proxy-specific variants |
| Input validation | 4 | Allowlist over blocklist, context-aware encoding, IP header sanitization (net.ParseIP); private/loopback/link-local SSRF guard pattern; `io.LimitReader` DoS hardening on external response bodies |
| Authentication | 4 | JWT, API keys, mTLS, OAuth 2.0 flows; admin API key middleware design + loopback-only binding — production-designed for vLog |
| Secrets management | 4 | Env vars, vault, never-hardcode policy |
| Cryptography | 3 | TLS 1.3, AES-GCM, Ed25519, ECDSA, bcrypt — when to use each |
| Supply chain security | 3 | Dependency review, SBOM, VEX, SLSA levels |
| Container security | 2 | Distroless, non-root, capability dropping |
| Security headers | 4 | HSTS, CSP, X-Frame-Options, Referrer-Policy — production-shipped in vProxWeb |
| UFW / firewall automation | 4 | `exec.Command` safe invocation, `net.ParseIP` guard, sudoers NOPASSWD scoping — production-shipped in vLog; defense-in-depth: auth middleware + loopback bind required before any ufw endpoint |
| Rate limiter hardening | 4 | Trusted proxy CIDR allowlist pattern; `r.RemoteAddr` CIDR check before honoring XFF/CF-Connecting-IP; Cloudflare IP range integration; prevents IP spoofing bypass — audited in vProx |
| Error response hygiene | 4 | Never `err.Error()` in HTTP responses; return generic message + server-side log; prevents path/schema/db diagnostic leakage (CWE-200) — audited across all vLog handlers |
| Concurrent HTTP handler safety | 4 | `http.ResponseWriter` not concurrent-safe; SSE keepalive+emit must be serialized via `sync.Mutex` or single-writer goroutine; WebSocket pump vs WriteControl race prevention — audited in vProx/vLog |

---

## 5b. Offensive Security & Penetration Testing

| Skill | Depth | Notes |
|-------|-------|-------|
| Pentest methodology | 3 | PTES, OSSTMM; recon → scan → exploit → post-exploit → report lifecycle |
| Network reconnaissance | 4 | nmap (OS/service fingerprinting, NSE scripts), masscan, SYN scan, banner grabbing — conceptual + applied in vLog port scan |
| OSINT (network layer) | 4 | Shodan, Censys, ip-api.com, RDNS, BGP/ASN routing, certificate transparency; production-shipped in vLog `OSINTStream` (concurrent: DNS, port scan, protocol probe, Cosmos RPC probe, ip-api.com) |
| Web application pentesting | 3 | OWASP Testing Guide; SQLi, XSS, SSRF, IDOR, broken auth, XXE, LFI/RFI; PortSwigger Academy labs |
| API security testing | 3 | REST/gRPC: JWT forgery, rate-limit bypass, parameter pollution, verbose error disclosure, mass assignment |
| Proxy security (vProx context) | 4 | Header injection via X-Forwarded-For/X-Real-IP, SSRF through reverse proxy, path traversal, upstream confusion, IP spoofing — directly applicable to vProx threat model |
| Vulnerability assessment | 3 | CVSS v3.1 scoring, exploit chaining, attack surface mapping, severity triage |
| Responsible disclosure / whitehack | 3 | Coordinated disclosure (CERT/CC model), CVE assignment, bug bounty programs (HackerOne, Bugcrowd), security.txt RFC 9116, ethical scope definition |
| Post-exploitation (defensive modeling) | 2 | Privilege escalation, lateral movement — conceptual; applied only for threat modeling and detection logic design |
| CTF / lab environments | 3 | HackTheBox, TryHackMe patterns; applied to sharpen anomaly detection and scoring logic in vLog |

---

## 6. Software Architecture

| Skill | Depth | Notes |
|-------|-------|-------|
| Distributed systems | 4 | CAP theorem, consensus, eventual consistency |
| API design | 4 | REST, gRPC, versioning, error contracts, pagination |
| Event-driven design | 3 | CQRS, event sourcing, outbox pattern |
| Microservices | 3 | Service mesh, circuit breaker, bulkhead |
| Monolith-first | 4 | Modular monolith, seam identification, strangler fig |
| DDD | 3 | Bounded contexts, aggregates, domain events |
| Concurrency patterns | 4 | Worker pools, fan-out/fan-in, backpressure |
| Proxy patterns | 4 | Reverse proxy, load balancing, health checks, circuit breaking |

---

## 7. Testing Methodology

| Skill | Depth | Notes |
|-------|-------|-------|
| Unit testing | 4 | Table-driven, mocking, dependency injection |
| Integration testing | 4 | In-process servers, testcontainers |
| Property-based testing | 3 | go-fuzz, rapid, quickcheck; invariant design |
| Benchmark testing | 4 | go test -bench, b.ReportMetric, statistical comparison |
| Smoke testing | 3 | Minimal happy-path coverage |
| Chaos engineering | 2 | Fault injection principles, controlled failure |
| Test pyramid | 4 | Right ratio of unit/integration/e2e |

---

## 8. Scientific & Research Methodology

| Skill | Depth | Notes |
|-------|-------|-------|
| Root cause analysis | 4 | 5 Whys, fishbone (Ishikawa), fault tree analysis |
| Experiment design | 4 | Control/treatment, randomization, blinding, ITT |
| Literature review | 3 | Systematic review, citation networks, grey literature |
| Technical writing | 4 | Precision language, reproducible methods sections |
| Decision analysis | 4 | Multi-criteria decision making, MCDA, regret minimization |
| Trade-off analysis | 4 | Pareto front, cost-benefit, opportunity cost |
| Estimation | 4 | Reference class forecasting, planning poker, Monte Carlo |

---

## 9. Development Tools & Environment

### VSCode / Local
| Tool | Depth |
|------|-------|
| gopls (LSP) | 4 |
| rust-analyzer | 3 |
| delve (Go debugger) | 3 |
| staticcheck / golangci-lint | 3 |
| TOML/YAML validators | 3 |
| Make task runner | 3 |
| git + git-worktrees | 4 |

### Profiling & Analysis
| Tool | Depth |
|------|-------|
| pprof (CPU, heap, trace) | 4 |
| go tool trace | 3 |
| perf (Linux) | 2 |
| flamegraph.pl | 3 |
| benchstat | 4 |

### Data Science Tools (applied, via Go or external)
| Tool | Depth |
|------|-------|
| Python (NumPy, pandas, scipy) | 3 |
| statsmodels / scikit-learn | 3 |
| Jupyter (for analysis notebooks) | 2 |
| R (for statistical validation) | 2 |
| SQL (any dialect) | 4 |
| jq (JSON stream analysis) | 4 |

---

## 10. Repository & Release Engineering

| Skill | Depth | Notes |
|---|---|---|
| Git internals | 4 | reflog, bisect, worktrees, plumbing commands |
| Branch strategy | 4 | trunk-based, GitFlow, hybrid models; protection rules |
| gitignore policy | 4 | Public/private surface analysis, glob patterns, untracking |
| Clone size optimization | 3 | LFS, .gitattributes, compressed assets, MMDB patterns |
| GitHub Actions authoring | 4 | Workflows, jobs, matrix, reusable workflows, OIDC, custom actions, approval gates |
| CI/CD pipeline design | 4 | Check gates, approval workflows, dependency review, required reviewers |
| Release tagging | 3 | Semantic versioning, annotated tags, tarball generation |
| Pre-launch audits | 4 | Clone surface review, privacy analysis, dependency review |
| Dependabot management | 3 | Config, auto-merge policy, branch hygiene |
| Release automation | 3 | Goreleaser, artifact signing, SBOM generation |

---

## 11. Technical Documentation

| Skill | Depth | Notes |
|---|---|---|
| README design | 4 | User-first, scannable, links to deeper docs |
| Installation guides | 4 | Prerequisites, step-by-step, troubleshooting |
| Changelog / release notes | 4 | Keep-a-changelog format, semantic versioning alignment |
| API / config reference | 4 | Flag tables, env var tables, schema references |
| Upgrade / migration guides | 4 | Backward-compat framing, step-by-step migration |
| Docs architecture | 3 | README → INSTALLATION → MODULES → reference hierarchy |
| Docs-as-code | 3 | Tracked in VCS, PR review, link validation |
| Inline code comments | 4 | Explain why not what; document invariants and edge cases |

---

## 12. AI Agent Orchestration & LLM Tooling

| Skill | Depth | Notes |
|-------|-------|-------|
| GitHub Copilot CLI | 4 | Agent directives, custom agents, task delegation, session state, model routing |
| LLM prompt engineering | 3 | System prompts, few-shot, chain-of-thought, structured output |
| AI agent file design | 4 | Agent MD files, skill taxonomies, resource libraries, state routing, upgrade protocols |
| LLM fine-tuning (applied) | 1 | Future: AI-augmented rate limiting and traffic classification |
| AI-assisted code review | 3 | Copilot review agent, automated PR gating, reviewer agents |
| Agentic workflow design | 4 | Task decomposition, sub-agent delegation, parallel execution, model routing policy |

---

## 13. Web Server Engineering

| Skill | Depth | Notes |
|-------|-------|-------|
| HTTP/HTTPS server design | 4 | `net/http` ServeMux, host-pattern routing, middleware chains |
| TLS / SNI | 4 | `crypto/tls`, multi-cert SNI dispatch, `GetCertificate`, cert hot-reload |
| Reverse proxy | 4 | `net/http/httputil.ReverseProxy`, header manipulation, WebSocket upgrade |
| Response compression | 4 | `compress/gzip`, lazy compression, content-type detection, Flusher compat |
| CORS | 4 | Origin reflection, preflight handling, multi-origin security |
| ResponseWriter wrapping | 4 | Unwrap protocol (Go 1.20+), Hijacker/Flusher chain, statusRecorder |
| Static file serving | 3 | `http.FileServer`, `fs.FS`, index fallback, cache headers |
| HTTP/2 | 3 | Auto-enabled on TLS, h2c for gRPC upstream (planned) |
| Per-host config | 4 | Per-entity TOML files, directory scanning, hot reload, *bool tri-state for TOML |
| Config architecture | 4 | vprox.toml (proxy), webserver.toml (module), vhosts/*.toml (per-host), config naming conventions |
| Apache config import | 3 | VirtualHost parsing, directive mapping, TOML generation |
| Apache reverse proxy config | 4 | ProxyTimeout, RequestReadTimeout (handshake/header/body=0), ProxyPass timeout=, SSE-safe settings, double-compress guard (`SetEnvIfNoCase Content-Encoding`), `X-Real-IP`; multi-vhost validation |
| CLI subcommand design | 3 | `flag` package, subcommand dispatch, help formatting |

---

## 14. Web GUI Engineering (Go)

| Skill | Depth | Notes |
|-------|-------|-------|
| Embedded web GUI architecture | 3 | `go:embed` + `html/template` + htmx for single-binary admin dashboards |
| html/template | 3 | Template composition, partials, FuncMap, context-aware escaping |
| go:embed (fs.FS) | 3 | Static asset embedding, production vs dev mode switching |
| htmx | 3 | Partial fragment swaps, SSE/WS integration, hx-get/post/target/swap |
| Server-Sent Events (SSE) | 4 | Keepalive goroutine (15s `: ping`), `context.Background()` isolation from `r.Context()`, Apache `ProxyTimeout` interaction, done-channel shutdown; `http.ResponseWriter` concurrency safety (keepalive + emit MUST be mutex-serialized); production-shipped in vLog (`handleAPIInvestigate`, `handleAPIEnrich`, `handleAPIosint`) |
| Dashboard patterns | 3 | Status panels, metric widgets, collapsible blocks (`<details>`/`<summary>`), drag/drop layout (HTML5 DnD + localStorage persistence), reset layout button; vcol/hcol block expansion system — production in vLog v1.2.0 |
| Dashboard vcol/hcol | 3 | vcol: `∧`/`∨` button toggles `details.open` manually (onclick guard bypass); hcol: `›`/`‹` toggles `.row-expand-left/right` (75/25% grid split); strip pill: `.is-strip` hides details, shows rotated label, `width:44px; justify-self:center`; toggle IIFE reflows grid on vcol; `collapseBlockRow()` restores 50/50 |
| Dashboard JS debugging | 4 | IIFE scope isolation (per-block JS IIFEs, `window.*` exports for cross-IIFE calls); brace balance analysis; CSP `connect-src` debugging; `ReferenceError` from stale `window.fn = fn` after fn deletion — production-learned in vLog `76ed378` |
| Go/JSON nil-vs-empty slice | 4 | `var []T` → `null` vs `make([]T,0)` → `[]` in `encoding/json`; production gotcha in API responses — fixed in `7b149e0` |
| CSS frameworks (light) | 2 | Pico CSS, classless frameworks for admin UIs without build step |

---

## 15. Web Service Architecture & Design

| Skill | Depth | Notes |
|-------|-------|-------|
| Embedded HTTP server | 4 | `net/http.Server`, graceful shutdown, `context.Context` propagation, ReadTimeout/WriteTimeout tuning |
| TLS certificate management | 4 | SNI dispatch, `tls.Config.GetCertificate`, ACME/Let's Encrypt, `certmagic`, cert hot-reload |
| Reverse proxy implementation | 4 | `httputil.ReverseProxy`, Director/Transport, header forwarding (X-Forwarded-For/Proto/Host), WebSocket upgrade passthrough |
| Static file serving | 3 | `http.FileServer`, `fs.FS` from `go:embed`, cache-control headers, directory listing suppression |
| Middleware chains | 4 | CORS, gzip, request ID injection, auth, rate limiting, logging; composable `http.Handler` wrapping |
| Virtual host routing | 4 | Host-based multiplexing, per-vhost config, wildcard matching, default fallback |
| Response compression | 4 | `compress/gzip`, content-type detection, minimum-size threshold, `Flusher` compatibility |
| CORS policy engine | 4 | Origin matching, preflight caching, credential forwarding, per-vhost policy |
| WebSocket proxying | 4 | Upgrade handshake forwarding, bidirectional pump, idle/hard timeouts, connection draining; `SetReadLimit()` on both ends to prevent OOM; `sync/atomic` connection counter; Origin validation; WriteControl vs pump race prevention — production-audited in vProx |
| Health checks & readiness | 3 | `/healthz`, `/readyz`, upstream backend health probing, circuit breaker integration |
| Load balancing | 3 | Round-robin, weighted, least-connections, health-aware backend selection |
| HTTP/2 & h2c | 3 | Auto H2 on TLS, h2c for gRPC upstream, `golang.org/x/net/http2` |
| Config-driven architecture | 4 | `webservice.toml` + `config/vhosts/*.toml`, hot-reload, `*bool` tri-state, duplicate host detection |
| Apache/nginx migration | 3 | VirtualHost directive mapping, mod_proxy → Go reverse proxy, SSL cert migration |
| Graceful reload | 3 | Config reload without downtime, listener socket inheritance, zero-downtime restart |
| Access logging | 4 | Structured request logs, latency tracking, correlation IDs, country enrichment, module tagging |
| Security headers | 4 | HSTS, CSP, X-Frame-Options, X-Content-Type-Options, Referrer-Policy, per-vhost overrides |
| Rate limiting integration | 4 | Per-IP, per-route, per-vhost token bucket; auto-quarantine; bypass rules |

---

## 16. Log Analysis & IP Intelligence

| Skill | Depth | Notes |
|-------|-------|-------|
| Structured log parsing | 4 | key=value (`main.log`), JSON Lines (`rate-limit.jsonl`), lifecycle events, field aliases |
| Archive ingestion pipeline | 3 | `archive/tar`, `compress/gzip`, deduplication via registry table, oldest-first processing |
| SQLite (Go) | 3 | `modernc.org/sqlite` (pure Go, no CGO), WAL mode, prepared statements, migrations, FTS5 |
| CRM data modeling | 3 | Per-entity profiles (ip_accounts), event tables, intel cache, composite enrichment |
| AbuseIPDB API (v2) | 3 | IP check endpoint, confidence score, report count, categories, rate-limit awareness |
| VirusTotal API (v3) | 3 | IP address report, malicious/suspicious counts, per-engine breakdown, quota management |
| Shodan API | 3 | `/shodan/host/{ip}`, open ports, services/banners, hostnames, org/ISP/country enrichment |
| Composite threat scoring | 3 | Weighted multi-source 0-100 score, flag taxonomy, per-source breakdown display |
| IP enrichment orchestration | 3 | Async enrichment queue, cache TTL, API rate limiting, graceful degradation |
| FS watcher patterns | 3 | Poll-based archive watcher, dedup via processed-file registry, trigger-on-new logic |
| Log analyzer web UI | 3 | Dashboard + CRM account view (search/sort/per-page/Investigate btn) + query builder + threat panel; htmx partial SSE updates; production-shipped in vLog v1.1.0 |
| TOML config design patterns | 4 | Struct backfill defaults, tri-state `*bool`, soft migration (dual-source loading with precedence), vms-to-chain consolidation pattern, deprecation warnings, backward compat strategy — applied in v1.3.0 chain.toml redesign |
| Soft migration patterns | 4 | Dual-source loading (old config still works, new config wins when present), deprecation log warnings, backward compat via fallback struct, phased rollout — v1.3.0 vms.toml→chain.toml `[management]` |

---

## 17. UI/UX Design Systems

| Skill | Depth | Notes |
|-------|-------|-------|
| CSS design tokens | 4 | `--var-*` system, dark/light theme switching, token inheritance — production in vLog Matrix [V] |
| Glass morphism UI | 4 | `backdrop-filter:blur()`, translucent bg, glow borders — production in vLog cards |
| Viewport-fill backgrounds | 4 | `background-size:100% 100% fixed`, `body::before` overlay pattern — production in vLog |
| Sticky footer (flex) | 4 | `flex-direction:column` + `main{flex:1}` — production in vLog base.html |
| Session auth UX | 4 | Login page (standalone, no base.html dep), `HttpOnly`/`SameSite` cookie, bcrypt, HMAC session — production vLog auth |
| CSS animations | 3 | `@keyframes`, spinner rings (`probe-spinner`), transition timing |
| Responsive layout | 3 | CSS Grid, flexbox, media queries (700px breakpoint), per-page controls |
| Dashboard drag/drop | 3 | HTML5 DnD: `dragstart`/`dragover`/`drop` on master blocks; drag handle guard (`closest('.v-drag-handle')`); `localStorage` order persistence; reset layout button — production in vLog v1.2.0 |

---

## 18. Infrastructure Deployment Management

| Skill | Depth | Notes |
|-------|-------|-------|
| SSH dispatcher (Go) | 4 | `golang.org/x/crypto/ssh` client; dedicated key per service; `exec.Command` alternative for remote ops; production in `internal/push/ssh/` |
| Remote script execution | 4 | SSH channel exec, stdout/stderr capture, exit code handling, timeout via context; `~/vProx/scripts/chains/{chain}/{component}/{script}.sh` pattern |
| VM registry management | 3 | TOML-based VM inventory (vms.toml); per-VM: host, port, user, key_path, datacenter, chain list |
| Validator chain operations | 4 | Node/validator/provider/relayer component types; chain script layout (`scripts/chains/{chain}/{component}/{script}.sh`); akash patterns |
| Chain status polling | 3 | Cosmos RPC height + gov + upgrade plan polling; stale-node detection |
| check-host.net HTTP probe API | 4 | Submit+poll pattern, `countryNodes` map, `sanitizeProbeNode()` SSRF whitelist, 12s deadline 2s interval, concurrent CA+WW with `sync.WaitGroup`; `?country=&provider=` params; production in vLog `handleAPIProbe` |
| SSH VM metrics collection | 4 | Compound bash command pattern (`df + free + uptime` in single SSH session), metrics struct parsing, concurrent SSH polling with `sync.WaitGroup` + `sync.Mutex` on results; production in vLog Chain Status Server columns |
| Deployment tracking (SQLite) | 3 | `deployments` + `registered_chains` tables; deployment history; upgrade state machine |
| Module management | 3 | `internal/modules/` pattern; git fetch + `go build` + binary install + service restart automation |
| Chain upgrade automation | 4 | `/current_plan` polling → halt-height detection → binary pre-download → swap at halt → snapshot → service restart |

---

| Area | Current | Target | Notes |
|------|---------|--------|-------|
## 19. Binary Consolidation & Single-Binary Distribution

| Skill | Depth | Notes |
|-------|-------|-------|
| Multi-binary → single-binary consolidation | 3 | `cmd/` layout: `cmd/vprox/`, `cmd/vlog/` → `cmd/vprox/` with subcommands; shared `internal/` packages; goreleaser multi-binary config |
| Embedded HTTP server multiplexing | 4 | Multiple HTTP servers in one Go binary (separate goroutines, separate listen ports, shared config); `errgroup`/`os.Signal` graceful shutdown coordination |
| go:embed asset management | 3 | `go:embed` for templates/CSS/static files; build tags for feature inclusion/exclusion; `fs.FS` routing; cache invalidation (`go clean -cache`) |
| CLI subcommand trees | 3 | cobra/flag: `vprox vlog start|stop|status` pattern; shared flag inheritance; nested FlagSets; help formatting |
| UPX / binary compression | 2 | `upx --best` awareness; trade-off: startup time vs disk size; not needed for server daemons |
| Module lifecycle management | 3 | `internal/modules/`: git fetch + `go build` + binary install + systemd service; `config/modules.toml` state |
| Service consolidation (systemd) | 3 | `vlog.service` → `vprox.service` with `vlog` subcommand; ExecStart args; user migration |

---

## 20. Strategic Product Thinking (CEO/Venture Mode)

| Skill | Depth | Notes |
|-------|-------|-------|
| RICE/ICE prioritization | 3 | `(Reach × Impact × Confidence) / Effort` scoring; backlog triage; feature ranking |
| Technical debt accounting | 4 | Velocity impact, compound interest metaphor, decision framework; when to pay vs when to carry |
| Build vs buy vs borrow | 3 | Dependency risk matrix, maintenance burden, community health signals (stars/commits/issues) |
| MVP definition | 3 | Minimum that ships value; scope boxing; user-first framing |
| North Star metrics | 3 | vProx: proxy uptime × chains managed; vLog: threats detected × response time; metric trees |
| Opportunity cost reasoning | 3 | "What are we NOT building while doing this?" framing; time-value of features |
| Milestone planning | 3 | Phase-gated delivery; launch criteria definition; acceptance vs scope trade-offs |

---

## Growth Target

| Area | Current | Target | Notes |
|------|---------|--------|-------|
| GPU computing (CUDA) | 1 | 2 | Not required for vProx now |
| LLM fine-tuning | 1 | 2 | Future: AI-augmented rate limiting |
| eBPF / kernel tracing | 1 | 2 | Future: kernel-level profiling |
| Formal verification | 1 | 2 | Future: invariant proving for consensus |
| GitHub Actions advanced | 4 | 4 | ✅ Achieved: custom actions, OIDC, approval gates |
| Release automation | 3 | 3 | ✅ Achieved: Goreleaser, SBOM generation |
| AI agent orchestration | 4 | 4 | ✅ Achieved: model routing, agentupgrade protocol, multi-agent delegation |
| Web server engineering | 4 | 4 | ✅ Achieved: full vProxWeb module (TLS SNI, gzip, CORS, proxy, static) |
| Web service arch/design | 4 | 4 | ✅ Achieved: embedded HTTP server, vhost routing, middleware chains, config-driven |
| Web GUI engineering | 3 | 4 | In progress: Go+htmx embedded dashboard for vProx/vProxWeb management |
| Log analysis & IP intel | 3 | 4 | Active: vLog v1.1.0 — accounts UI + SSE intel handlers in production, archive ingestion, Apache-proxied |

---

## Capability Index (Quick Reference)

```
Go Systems:           ████████████████████ 4/4
Cosmos SDK (context): ████████████████████ 4/4  (proxy intelligence: /health routing, upgrade detection, IBC DoS, WS pooling)
Rust/CosmWasm:        ████████████████     3.5/4
Statistics:           ████████████████████ 4/4
Machine Learning:     ████████████████     3.5/4
Data Engineering:     ████████████████████ 4/4
Security (defensive): ████████████████████ 4/4  (auth middleware, error hygiene, SSRF IP guard, RW concurrency safety — production-audited)
Offensive Security:   ████████████         3/4    (OSINT: production vLog OSINTStream; pentest: methodology + web/API/proxy security)
Architecture:         ████████████████████ 4/4
Testing:              ████████████████████ 4/4
Observability:        ████████████████     3.5/4
Research Method:      ████████████████████ 4/4
Repo & Release Eng:   ████████████████████ 4/4
Technical Docs:       ████████████████████ 4/4
AI Agent Design:      ████████████████████ 4/4
Web Server Eng:       ████████████████████ 4/4
Web Service Arch:     ████████████████████ 4/4
Web GUI Eng:          ████████████████     3.5/4  (JS debugging, JSON nil-vs-empty, CSP — production patterns)
Log Analysis & Intel: ████████████████████ 4/4  (production: auth, Matrix theme, SSE intel, accounts, ingestion, vLog v1.2.0)
UI/UX Design Systems: ████████████████████ 4/4  (CSS tokens, glass morphism, viewport-fill, sticky footer, session auth UX — production)
Infrastructure Deploy:████████████████     3.5/4  (SSH dispatcher, VM registry, chain scripts, check-host.net probe, SSH metrics — active)
Binary Consolidation: ████████████         3/4    (multi-binary awareness, cmd/ layout, go:embed, module lifecycle — planned for v1.4.0)
Strategic Thinking:   ████████████         3/4    (RICE/ICE, tech debt, build/buy, MVP, North Star metrics)
```

---

*Skills are living documentation. Update this file when capabilities change or new domains are acquired.*
*Last updated: 2026-03-07 (rev17: §19 Binary Consolidation NEW (multi-binary, go:embed, CLI trees, module lifecycle); §20 Strategic Product Thinking NEW (RICE/ICE, tech debt, build/buy, MVP, North Star); §14 Dashboard JS debugging 3→4 + JSON nil-vs-empty 4 added; §16 TOML config design 4 + soft migration 4 added; §18 check-host.net probe 4 + SSH VM metrics 4 added; capability index updated: Web GUI 3→3.5, Infra Deploy 3→3.5, Binary Consolidation 3 NEW, Strategic Thinking 3 NEW)*
