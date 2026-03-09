# jarvis5.0_vscode Router State (local-only)

This file routes multi-project memory handling for the local VSCode agent variant.
All paths here are local-only (`agents/` is gitignored).

## Active project

- `vprox`

## Agent version

- `jarvis5.0_vscode` (supersedes `jarvis4.0_vscode`)

## Linked files

| File | Role |
|------|------|
| `.github/agents/jarvis5.0_vscode.agent.md` | Agent definition (behaviors, scope, workflow) |
| `agents/jarvis5.0_skills.md` | Full skill taxonomy with depth levels |
| `agents/jarvis5.0_resources.md` | Curated reference links by domain |
| `agents/base.agent.md` | Cross-project engineering discipline rules |
| `agents/projects/vprox.state.md` | vProx project memory (also see `vprox.vscode.state.md` for VSCode sessions) |

## Load order (new session, lazy project memory)

1. `agents/jarvis5.0_vscode_state.md` (this file)
2. `agents/base.agent.md` (global rules)
3. `.github/agents/jarvis5.0_vscode.agent.md` (full agent definition)

Project memory is loaded only when `load <project-name>` is requested.

---

## Command Protocol

### `load <project-name>`

Switch active project context.

Actions:
- Set **Active project** in this file.
- Read:
  - `agents/base.agent.md`
  - `agents/projects/<project-name>.vscode.state.md` (or fallback to `agents/projects/<project-name>.state.md`)
- If file does not exist, prompt to run `save new <project-name>`.

Examples:
- `load vprox`
- `load chain-ops`
- `load cosmo-relayer`

### `save` (or `save state`)

Append a memory dump to the current active project file:
- `agents/projects/<active-project>.vscode.state.md`

Required fields:
- timestamp (UTC)
- goal
- completed
- files changed
- verification performed
- open follow-ups
- next first steps

### `save new <project-name>`

Create a new project state file and switch active project.

Actions:
- Create `agents/projects/<project-name>.vscode.state.md` from template.
- Set **Active project** to `<project-name>` in this file.
- Add initial seed section for the new project.

Examples:
- `save new cosmo-relayer`
- `save new sdk-migration-lab`
- `save new data-analysis`

### `new`

Guided bootstrap command for project initialization (local workflow).

Flow:
1. Ask: `Create new repo? (y/N)`
2. If **No**:
   - Stay in current repo.
   - Create missing local project state files from template as needed.
   - Set active project and continue.
3. If **Yes**:
   - Ask for project idea.
   - Search reference resources (`agents/jarvis5.0_resources.md`) for relevant patterns and best practices.
   - Analyze requirements, identify stack, propose architecture plan.
   - Create repository scaffold locally.
   - Apply same `.gitignore` policy baseline as the current repo, then adjust for stack-specific needs.
   - Copy agent bundle (`.github/agents/jarvis5.0_vscode.agent.md`, `agents/` structure).
   - Begin implementation and write initial handoff notes.

### `skills`

Print summary from `agents/jarvis5.0_skills.md` (capability index + relevant domains for current task).

### `skills [domain]`

Print skills for specific domain. Examples:
- `skills go` → Go engineering skills
- `skills ml` → Machine learning skills
- `skills stats` → Statistics skills
- `skills security` → Security skills

### `resources [domain]`

Print reference links from `agents/jarvis5.0_resources.md` for a domain. Examples:
- `resources go` → Go references
- `resources cosmos` → Cosmos SDK references
- `resources ml` → ML references
- `resources vprox` → vProx-specific references

### `profile`

Run profiling analysis for the current active project:

> **Prerequisite**: vProx does not ship with `net/http/pprof` imported. To enable, add
> `import _ "net/http/pprof"` to `cmd/vprox/main.go` and expose a debug HTTP server.
> See `resources go` → "pprof Guide" for setup pattern.

1. Start pprof server (`go tool pprof`)
2. Collect CPU/heap/goroutine profiles
3. Analyze hotspots and report findings

### `bench [package]`

Run benchmarks with statistical comparison:
1. Run `go test -bench=. -benchmem -count=10 [package]`
2. Compare with `benchstat` if prior results exist
3. Report findings with significance assessment

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

**Decision heuristics for ASSESS:**
- New module built → add to Scope, add skill domain, add resources
- New pattern established → add to base.agent.md or project conventions
- Depth increase → evidence: built production code in that domain
- Stale reference → update or remove
- Missing cross-reference → add link between files

---

## Data Science Session Extensions

When a task requires data science methodology, extend the standard workflow:

```
STANDARD: Understand → Investigate → Patch → Verify → Document

EXTENDED for DS tasks:
  → Define metric (what are we measuring and why?)
  → Design experiment (control/treatment/sample size)
  → Collect data (sufficient sample, avoid bias)
  → Analyze (apply appropriate statistical method)
  → Conclude (state uncertainty, confidence intervals)
  → Recommend (decision with trade-offs stated)
```

Activate extended mode by recognizing these task signals:
- Performance analysis requests
- Traffic pattern investigation
- Rate limiting threshold tuning
- Anomaly investigation in logs
- Capacity planning questions
- A/B testing or feature comparison

---

## Naming Rules

- Use lowercase slug names for project files.
- Allowed chars: `a-z`, `0-9`, `-`, `_`.

---

## Local Development Context

This agent prioritizes:
- Fast iteration with local builds (`make build`, `go run ./...`)
- Direct terminal access for debugging and profiling
- File system watching for live reload (`make watch` if available)
- Local language server features (gopls for Go, rust-analyzer for Rust)
- In-editor diagnostics (staticcheck, vet, golangci-lint via LSP)
- Local pprof server for profiling sessions

---

## Upgrade History

| Version | Date | Key Additions |
|---------|------|---------------|
| jarvis4.0_vscode | 2026-02-21 | Cosmos SDK + Go + vProx-specific engineering |
| jarvis5.0_vscode | 2026-02-22 | PhD data science, scientific methodology, extended skill taxonomy, curated resource library, new commands (skills, resources, profile, bench) |
| jarvis5.0_vscode (rev2) | 2026-02-23 | Model routing policy, `model` + `agentupgrade` commands, vProxWeb module knowledge, per-host config architecture |
| jarvis5.0_vscode (rev3) | 2026-02-23 | P0-P3 code review fixes (14 findings), P4 feature roadmap, config naming (vprox.toml/webserver.toml/vhosts/*.toml), concurrency pattern skills |
| jarvis5.0_vscode (rev4) | 2026-02-23 | Web GUI Engineering domain: Go+html/template+go:embed+htmx stack, §14 skills, §15 resources |
| jarvis5.0_vscode (rev6–13) | 2026-02-24–2026-03-02 | vLog v1.0.0→v1.1.0 shipped (multi-region probe, auth, Matrix theme, accounts page); security audit P0 fixed; base.agent.md: 20+ production patterns (SSRF, SSE concurrency, session auth, CSS tokens, glass morphism, viewport bg); skills §14→4/4 Web GUI, §16→4/4 Log Analysis, §17 UI/UX NEW |
| jarvis5.0_vscode (rev14) | 2026-03-02 | awesome-copilot skill registry (201 skills) integrated into agentupgrade protocol — Step 0 SKILL SYNC added; all skills mapped to steps 0–6 (4a–4l); vscode agentupgrade section synced to match jarvis5.0 protocol |

---

*This router state is local-only. Do not commit to the repository.*
