# jarvis5.0 router state (local-only)

This file routes multi-project memory handling.
All paths here are local-only (`agents/` is gitignored).

## Active project

- `vprox`

## Agent version

- `jarvis5.0` (supersedes `jarvis4.0`)

## Org position

- **Reports to**: `ceo` (`.github/agents/ceo.agent.md`)
- **Role**: VP Engineering — primary executor, dispatcher to all specialist agents

## Linked files

| File | Role |
|------|------|
| `.github/agents/ceo.agent.md` | Direct superior — strategic orchestrator |
| `.github/agents/jarvis5.0.agent.md` | Agent definition |
| `agents/jarvis5.0_skills.md` | Skill taxonomy |
| `agents/jarvis5.0_resources.md` | Reference links |
| `agents/base.agent.md` | Cross-project rules |
| `agents/projects/vprox.state.md` | vProx project memory |
| `agents/ceo_state.md` | CEO strategic state |
| `.vscode/restruct/PLAN.md` | Config restructure design + state (active working folder) |
| `.vscode/restruct/*.sample` | Corrected sample TOML files for v1.4.0 config layout |

## Load order (new session, lazy project memory)

1. `agents/jarvis5.0_state.md` (this file)
2. `agents/base.agent.md` (global rules)
3. `.github/agents/jarvis5.0.agent.md` (full definition)

Project memory loaded only when `load <project-name>` is requested.

---

## Command Protocol

### `load <project-name>`
Switch active project context. Read base.agent.md + project state file.

### `save` / `save state`
Append memory dump to `agents/projects/<active-project>.state.md`.
Fields: timestamp, goal, completed, files changed, verification, follow-ups, next steps.

### `save new <project-name>`
Create new project state file. Set active project.

### `new`
Guided bootstrap for new project/repo. Consult `agents/jarvis5.0_resources.md` for patterns.

### `skills [domain]`
Print skills from `agents/jarvis5.0_skills.md` for domain.

### `resources [domain]`
Print references from `agents/jarvis5.0_resources.md` for domain.

### `model <task-type>`
Print the recommended AI model for the task. Routing table:

| Task class | Model |
|------------|-------|
| Meta-engineering, agent file design, architecture decisions | `claude-opus-4.6` |
| Complex multi-step implementation, multi-file reasoning | `claude-opus-4.6` |
| Security analysis, threat modeling, CVE investigation | `claude-opus-4.6` |
| Standard code changes, PR reviews, CI debugging | `claude-sonnet-4.6` |
| Build / test / lint execution | `claude-sonnet-4.6` |
| Fast codebase exploration, grep/glob synthesis | `claude-haiku-4.5` |
| Heavy code generation, algorithmic implementation | `gpt-5.1-codex` |
| Opus quality needed but latency matters | `claude-opus-4.6-fast` |

Sub-agent defaults:
```
explore     → claude-haiku-4.5
code-review → claude-sonnet-4.6
task        → claude-sonnet-4.6
jarvis5.0   → claude-opus-4.6
reviewer    → claude-sonnet-4.6
```

### `agentupgrade`

Full self-assessment and upgrade of all agent configuration files.

**Protocol:**
1. **INVENTORY** — Read all agent files (`.github/agents/`, `agents/`, `agents/projects/`)
2. **ASSESS** — Evaluate each file for accuracy, completeness, consistency, currency
3. **CONTEXT** — Build complete_state:
   - Recent work: last 2 major PRs/commits/features shipped
   - Current codebase: all modules, architecture, conventions
   - Feature potential: capabilities that could be built next
   - Skill growth: new domains exercised since last upgrade
4. **PATCH** — Apply targeted updates to definitions, skills, resources, state, base, reviewer, project state
5. **VERIFY** — Cross-reference all files for consistency
6. **REPORT** — Changed files, gaps closed, new capabilities, upgrade history entry

**Decision heuristics:**
- New module built → add to Scope, add skill domain, add resources
- New pattern established → add to base or project conventions
- Depth increase → evidence: built production code in that domain
- Stale reference → update or remove; missing cross-reference → add

---

## Naming Rules

- Lowercase slugs: `a-z`, `0-9`, `-`, `_`.

---

## Upgrade History

| Version | Key Additions |
|---------|---------------|
| jarvis4.0 | Cosmos SDK + Go + vProx engineering |
| jarvis5.0 | PhD data science, scientific methodology, skill taxonomy, resource library |
| jarvis5.0 (rev2) | Model routing policy, sub-agent model defaults, `model` session command |
| jarvis5.0 (rev3) | `agentupgrade` command + protocol, vProxWeb module knowledge, per-host config architecture, webserver engineering skills, updated cross-references |
| jarvis5.0 (rev4) | P0-P3 full code review fixes (14 findings), P4 feature roadmap, config architecture naming (vprox.toml/webserver.toml/vhosts/*.toml), concurrency pattern skills (tickers/sweepers/done-channels), IP validation |
| jarvis5.0 (rev5) | Web GUI Engineering domain: architecture decision (Go+html/template+go:embed+htmx), §14 skills, §15 resources, GUI scope in agent definitions |
| jarvis5.0 (rev6) | Config arch marked DONE (webservice.toml + config/vhosts/ shipped); CLI restructuring roadmap captured (restart, --daemon, --new-backup, --list-backups); TOML>env priority rule; splitLogWriter CLI pattern; reviewer + base updated; next backlog: vProx.service hardening + CLI restructure + .env audit |
| jarvis5.0 (rev7) | CLI commands shipped (start/stop/restart/-d/--daemon, all backup flags); service management via `sudo service` + sudoers NOPASSWD; §15 Web Service Architecture & Design skills + §15b resources; systemd depth 3→4; vProxWeb expansion next: replace Apache/nginx with embedded Go webserver |
| jarvis5.0 (rev8) | vLog module planned (log analyzer binary); §16 Log Analysis & IP Intelligence (SQLite, AbuseIPDB/VT/Shodan, threat scoring, CRM data model); vLog scope added to agent definition + reviewer; vprox.state.md updated with vLog session |
| jarvis5.0 (rev10) | Security expanded: §5 defensive depth upgraded (threat modeling 3→4, OWASP 3→4, security headers 4, UFW automation 3); §5b Offensive Security & Penetration Testing added — pentest methodology (PTES/OSSTMM), network recon/OSINT 4/4 (production vLog OSINTStream: concurrent port scan, RDNS, ip-api, Shodan, Cosmos probe), web/API/proxy pentesting 3/4, responsible disclosure/whitehack 3/4; §6b resources (PTES, PortSwigger, nuclei, Burp, Metasploit, CERT/CC CVD, HackerOne, Bugcrowd, CVSS); Identity table updated in all agent definitions; Security capability index split into defensive 4/4 + offensive 3/4 |
| jarvis5.0 (rev11) | vLog v1.0.0 shipped: multi-location probe (check-host.net submit+poll; CA+WW concurrent; fixed field indices row[1]=latency/row[3]=code_str; verified node list from /nodes/hosts); dashboard endpoint table 3 static probe columns (Local/🇨🇦/🌍) with CSS spinner + hover tooltips; Accounts page: All per-page option, URL-based sort persistence (back-nav safe), Status column; intel/OSINT fully parallelized; vLog CLI: stop+restart added; base.agent.md: external probe + static probe columns patterns; both agent definitions updated to v1.0.0 shipped |
| jarvis5.0 (rev12) | Full codebase + security audit (2026-03-01, claude-opus-4.6, 302s code-review + 572s security): 26 findings total — 2 CRITICAL (vLog zero-auth, backup data loss), 6 HIGH (SSRF, WS race, SSE concurrent write, notifyVLog goroutine, rate limiter bypass, API key leak), 7 MEDIUM, 5 LOW; supply chain/SQL injection/command injection CLEAN; base.agent.md: 9 new production security patterns (SSE writer serialization, WS hardening, trusted proxy CIDR, loopback bind, auth middleware, error hygiene, io.LimitReader, backup write order, SSRF IP guard); skills: auth 3→4, UFW 3→4, SSE 3→4, new entries (rate limiter hardening, error hygiene, concurrent handler safety, SSRF guard, WS hardening); resources: §6 Go HTTP Security Hardening subsection (OWASP WS/SSRF/Error/REST, Cloudflare IPs, io.LimitReader, net.IP.IsPrivate); reviewer: audit-driven security checklist (11 items, block on CRITICAL/HIGH) |
| jarvis5.0 (rev14) | Cosmos SDK proxy intelligence (15 hidden gems: /health routing, upgrade halt detection, IBC DoS, WS subscription pooling, ABCI prove= routing, broadcast_tx_commit circuit breaker, dump_consensus_state rate limit, gRPC reflection gate, evidence slashing monitoring, config sanitization); push module scope (internal/push/ SSH dispatcher, VM registry, runner, state, status, api); Phase E CLI plan (vProx mod/push/chain subcommands); scripts/chains/ migration from vApp; §18 Infrastructure Deployment Management NEW (SSH, VM registry, chain upgrade automation); §2 Cosmos SDK 3→4 across CometBFT+IBC; §17 UI/UX Design Systems NEW; §2b Cosmos SDK Hidden Gems resources NEW; §17 Infrastructure+SSH resources NEW; base.agent.md: push/SSH pattern + 8 Cosmos proxy patterns added; reviewer: push/SSH/IBC/upgrade criteria added; vLog updated to v1.2.0 branch status |
| jarvis5.0 (rev15) | vLog dashboard overhaul shipped: Chain Status full-width 16-col table (Chain Info×5 + Governance×4 + Ping×3 + Server×3 + Actions×1); collapsible blocks `<details>/<summary>` + `.v-block` CSS + onclick guard; probe column redesign → 3 independent cols (L/DC/WW); configurable DC probe country per-VM (`[vm.ping]` TOML: `VMPing{Country, Provider}`; `countryNodes` map + `sanitizeProbeNode()` SSRF whitelist; `?country=&provider=` params in `handleAPIProbe`); `ChainStatus.PingCountry`/`.PingProvider` wired through push pipeline; Phase E CLI marked SHIPPED; security audit ALL 24 FINDINGS RESOLVED; 2 new base.agent.md patterns (configurable DC probe, collapsible block); not-applicable catalogue: azure-pricing, mentoring-juniors, noob-mode added |
| jarvis5.0 (rev16) | vLog v1.2.0 dashboard redesign system shipped: drag/drop master-block layout (HTML5 DnD + localStorage + reset button); vcol `∧`/`∨` button per block (toggles `details.open` manually, onclick guard bypass); hcol `›`/`‹` buttons → 75/25% grid split (`3fr 1fr`/`1fr 3fr`), other block collapses to 44px strip pill; toggle listener IIFE reflows grid on vcol; sample file revision schema (`rev{M}.{m}.{p}-{commit7}` header + `make samples-push`); archive buttons renamed (Refresh/Manual Import); full page reload after import; Intel/Flagged IPs renames; independent block collapse (grid reflow IIFE); skills: §14 Dashboard patterns 2→3 + vcol/hcol row NEW; §17 drag/drop row NEW; capability index Web GUI 3→3.5; base.agent.md: 4 new patterns (vcol/hcol, drag/drop, sample revision schema, go:embed cache invalidation) |
| jarvis5.0 (rev17) | **Full capability expansion**: §19 Binary Consolidation NEW (multi-binary→single-binary, go:embed, CLI trees, cobra, module lifecycle, service consolidation); §20 Strategic Product Thinking NEW (RICE/ICE, tech debt accounting, build/buy/borrow, MVP definition, North Star metrics, opportunity cost); Strategic Mode section added to agent definition (activated by roadmap/ship/priority/CEO-mode keywords); MCP Server Ecosystem documented (7 servers: filesystem, SQLite, memory, sequential-thinking, git, Playwright, Brave Search — with install commands + vProx/vLog use cases); §14 Dashboard JS debugging 4 (IIFE scope, window.export, brace balance, CSP) + JSON nil-vs-empty 4; §16 TOML config design 4 + soft migration 4; §18 check-host.net probe 4 + SSH VM metrics 4; resources: §18-§20 NEW (MCP, binary consolidation, strategic thinking — 25+ new references); base.agent.md: 4 new patterns (JSON nil-vs-empty, JS IIFE debugging, soft migration, chain.toml self-contained); reviewer: JSON nil-vs-empty + soft migration + chain.toml checks added; vprox.state.md: v1.3.0 session (chain.toml consolidation arch + impl plan + schema + open bugs); capability index: Web GUI 3.5, Infra Deploy 3.5, Binary Consolidation 3, Strategic Thinking 3 |
| jarvis5.0 (rev18) | **Skill installation**: 6 awesome-copilot skills installed to `.github/skills/` — `polyglot-test-agent` (Go unit test generation, bundled `unit-test-generation.prompt.md`), `conventional-commit` (Conventional Commits spec enforcement), `git-commit` (auto-stage + message from diff, `/commit` trigger), `devops-rollout-plan` (preflight + rollback + comms for v1.x.0 releases + systemd deploys), `refactor` (surgical maintainability improvements), `documentation-writer` (Diataxis framework docs); agent definition updated: Installed Skills section added (auto-load table + 5 auto-invoke rules), Execution Workflow steps 4–7 annotated with skill triggers, agentupgrade 4c/4g/4j/6 marked ✅ INSTALLED; all 6 skills verified via line-count diff against remote (all current) |
| jarvis5.0 (rev19) | **Fleet module rename + config/push removal (v1.3.0)**: `internal/push/` → `internal/fleet/`; CLI `vprox push` → `vprox fleet`; API routes `/api/v1/push/*` → `/api/v1/fleet/*`; `PushConfig` → `FleetConfig`; `config/push/` fully deleted; `config/infra/<datacenter>.toml` canonical VM inventory (all `*.toml` scanned via `LoadFromInfraFiles`); `config/fleet/settings.toml` NEW (SSH defaults + poll interval); `*.sample` naming convention locked (no `.toml` extension, prevents scanner loading); agent definition updated: fleet module section replaces push, config layout updated, Phase E CLI updated, MCP SQLite hint updated (push.db → fleet.db); base.agent.md: 3 new patterns (fleet config separation, fleet rename note, sample naming convention updated); vprox.state.md: 2026-03-09 session appended |
| jarvis5.0 (rev20) | **Restructure design + fleet CLI + chain dedup fixes**: Chain dedup fix (`chainBaseSlug`+`FindVMForChain` in `internal/fleet/config/config.go`, `fe5207e`); HTTP 405 delete → POST workaround (`fe5207e`); fleet CLI `chains`+`unregister` commands added (`e52eaf1`); dashboard remove button removed, `removeChain()` stubbed for Settings page (`e52eaf1`); `.vscode/restruct/` working folder created (10 files: PLAN.md + 9 sample files); two pending bugs documented: `openFleetDB` hardcodes `data/push.db` (HIGH) + `RemoveRegisteredChain` discards `RowsAffected` (MEDIUM); agent definition: v1.4.0 config restructure layout documented (chain/service/infra split + tree-join + `[[host]]` TOML arrays); skills: config architecture + TOML config design rows updated with v1.4.0 restructure knowledge + P1–P6 migration path; resources: TOML array-of-tables spec + `.vscode/restruct/PLAN.md` reference added; base.agent.md: 2 new patterns (`[[array-of-tables]]` subtable scoping + config restructure tree-join) |
