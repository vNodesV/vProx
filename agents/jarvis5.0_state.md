# jarvis5.0 router state (local-only)

This file routes multi-project memory handling.
All paths here are local-only (`agents/` is gitignored).

## Active project

- `vprox`

## Agent version

- `jarvis5.0` (supersedes `jarvis4.0`)

## Linked files

| File | Role |
|------|------|
| `.github/agents/jarvis5.0.agent.md` | Agent definition |
| `agents/jarvis5.0_skills.md` | Skill taxonomy |
| `agents/jarvis5.0_resources.md` | Reference links |
| `agents/base.agent.md` | Cross-project rules |
| `agents/projects/vprox.state.md` | vProx project memory |

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
