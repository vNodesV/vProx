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
- **Service management**: Use `runServiceCommand(action)` → `sudo service vProx <action>` for daemon start/stop/restart. Never call `systemctl` directly from Go code. Sudoers NOPASSWD setup via `make systemd` grants passwordless access to `/usr/sbin/service vProx start|stop|restart`.

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
