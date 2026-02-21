# project state: vprox

## Project summary
`vProx` is a Go reverse proxy with:
- HTTP proxying
- WebSocket proxying
- rate limiting and auto-quarantine
- optional geo enrichment
- backup/archive routines

## Project conventions

### Logging
- Prefer structured single-line logs via `internal/logging/logging.go`.
- Main log format follows CosmosSDK/journalctl-cat style:
  - `<time> <LVL> <message> key=value ... module=<source>`
- Include `request_id` on request-scoped events.

### Limiter JSONL compatibility
- `~/.vProx/data/logs/rate-limit.jsonl` is compatibility-sensitive.
- Keep legacy fields where possible:
  - keep `reason` alongside `event`
  - keep `ua` alongside `user_agent`

### Request correlation
- Ensure request ID early: `logging.EnsureRequestID(r)`
- Echo on response: `logging.SetResponseRequestID(w, id)`
- Log as `request_id`

## Known follow-ups
- Optional naming polish for event message text.

---

## Session Memory Dumps

### 2026-02-19 10:28 (UTC) — flags + logging standardization
- Goal:
  - Verify `--flag` behavior, normalize docs/help to `--`, and migrate `main.log` rendering to CosmosSDK-like output with `module=`.
- Completed:
  - Verified runtime accepts both `--flag` and `-flag`; mismatch was help formatting.
  - Added custom CLI usage output so `--help` prints long flags in `--` style.
  - Updated `FLAGS.md` and `CLI_FLAGS_GUIDE.md` to use `--...` consistently.
  - Switched shared logger output format to:
    - `<time> <LVL> <message> key=value ... module=<source>`
    - with short levels (`INF`, `WRN`, `ERR`, `DBG`).
- Files changed:
  - `main.go`
  - `FLAGS.md`
  - `CLI_FLAGS_GUIDE.md`
  - `internal/logging/logging.go`
- Verification:
  - `gofmt -w internal/logging/logging.go main.go internal/limit/limiter.go internal/ws/ws.go internal/backup/backup.go`
  - `go build ./...`
  - `./vProx --help` shows `--` long flags.
  - `go run . --home /tmp/vprox-logcheck --validate` produced log line like `10:27AM INF status ... module=geo`.
- Open follow-ups:
  - Optional event-text polish.
  - Upstream propagation of `X-Request-ID`.
- Next first steps:
  - Run short live traffic test and inspect `main.log` + `rate-limit.jsonl` together.

### 2026-02-19 10:41 (UTC) — multi-project jarvis memory router + local-only enforcement
- Goal:
  - Avoid cross-project context bleed by introducing project-scoped state files.
  - Ensure all agent collaboration artifacts stay local (ignored + not tracked), so they are not committed/pushed/cloned.
- Completed:
  - Converted `agents/jarvis4.0_state.md` into a project router with command protocol:
    - `load <project>`
    - `save` / `save state`
    - `save new <project>`
  - Added `agents/base.agent.md` for cross-project operating rules.
  - Added project-scoped state:
    - `agents/projects/vprox.state.md`
  - Added template for new projects:
    - `agents/projects/_template.state.md`
  - Verified `agents/` is ignored and untracked (won’t be committed/pushed/cloned).
- Files changed:
  - `agents/jarvis4.0_state.md`
  - `agents/base.agent.md`
  - `agents/projects/vprox.state.md`
  - `agents/projects/_template.state.md`
  - `.gitignore` already contained `agents/` ignore rule and was verified.
- Verification:
  - `git check-ignore -v agents/...` confirmed ignore rules.
  - `git ls-files agents .github/agents` returned empty (not tracked).
- Open follow-ups:
  - Run `go test ./...` as a sanity check if/when touching more runtime logic.
  - Optional: upstream propagation of `X-Request-ID` to backend HTTP + WS dial headers.
- Next first steps:
  - If switching repos/projects: `load <project-name>` or `save new <project-name>`.
  - For vProx polish: do a short live traffic run and check `main.log` ergonomics (message text + key naming).

### 2026-02-19 11:35 (UTC) — standalone repo alignment + start command + local service workflow
- Goal:
  - Finalize vProx as a standalone repo/module, add Cosmos-style foreground run command (`vProx start`), and make systemd handling non-invasive/local-first.
- Completed:
  - Migrated module/import paths from `github.com/vNodesV/vApp/modules/vProx/...` to `github.com/vNodesV/vProx/...`.
  - Moved entrypoint to conventional Go layout `cmd/vprox/main.go` and removed root `main.go`; added root `doc.go`.
  - Added `start` command behavior:
    - `vProx start` runs foreground and logs to stdout for journald/journalctl-cat workflows.
    - `backup` command shorthand preserved (`vProx backup` → `--backup`).
  - Updated service unit template to use `ExecStart=/usr/local/bin/vProx start` and journald-friendly settings.
  - Changed Makefile systemd flow to local-first:
    - generates/updates `~/.vProx/service/vProx.service`
    - compares rendered unit with existing local one and updates only on diffs.
  - Added explicit operator warning when local unit changes:
    - “This version generated a new service file. Review it and replace the current system service if needed.”
  - Updated docs to reflect manual unit install path:
    - copy local unit to `/etc/systemd/system/vProx.service`, then `daemon-reload`, `enable`, `start`, and `journalctl -u vProx.service -f --output=cat`.
- Files changed:
  - `go.mod`
  - `cmd/vprox/main.go`
  - `doc.go`
  - `internal/limit/limiter.go`
  - `internal/ws/ws.go`
  - `internal/backup/backup.go`
  - `vprox.service.template`
  - `Makefile`
  - `README.md`
  - `MIGRATION.md`
- Verification:
  - `gofmt -w cmd/vprox/main.go doc.go internal/ws/ws.go internal/limit/limiter.go internal/logging/logging.go internal/backup/backup.go`
  - `go test ./...` passed after each major batch.
  - `go build -o ./.build/vProx ./cmd/vprox` passed.
  - `./.build/vProx --help` confirms `start` command appears.
  - `make systemd` confirms local unit render/update at `~/.vProx/service/vProx.service`.
- Open follow-ups:
  - Optional: split `cmd/vprox/main.go` into smaller files (`config.go`, `router.go`, `server.go`) for maintainability.
  - Optional: event message wording polish while preserving field/schema compatibility.
- Next first steps:
  - On host machine (with systemd):
    1) `sudo cp ~/.vProx/service/vProx.service /etc/systemd/system/vProx.service`
    2) `sudo systemctl daemon-reload`
    3) `sudo systemctl enable vProx.service`
    4) `sudo systemctl start vProx.service`
    5) `journalctl -u vProx.service -f --output=cat`

### 2026-02-19 14:24 (UTC) — journald + main.log dual sink and Cosmos-style colorized output
- Goal:
  - Ensure `vProx` service logs are visible in both `journalctl` and `tail -f ~/.vProx/data/logs/main.log`, while matching CosmosSDK-style colorful terminal output.
- Completed:
  - Updated startup logging path in `cmd/vprox/main.go` so `start` mode writes to both stdout and `main.log`.
  - Added a split log writer that keeps `main.log` plain text while applying ANSI color rendering only to stdout output.
  - Implemented level/key/value color highlighting to improve readability in `journalctl -f --output=cat` (Cosmos-like style).
  - Preserved `--quiet` behavior so `start --quiet` keeps file-only output (no stdout mirroring).
  - Verified current service template already uses `ExecStart=/usr/local/bin/vProx start` and journald output settings.
- Files changed:
  - `cmd/vprox/main.go`
- Verification:
  - `gofmt -w cmd/vprox/main.go && go build ./... && go test ./...` passed.
  - `make systemd` confirmed local unit is up to date.
- Open follow-ups:
  - Optional: add `VPROX_COLOR_LOGS` env toggle for operators who prefer non-colored journald output.
  - Optional: expose additional key-specific coloring rules if needed for chain-specific fields.
- Next first steps:
  - On host machine, ensure installed unit is the generated one from `~/.vProx/service/vProx.service` (not older `ExecStart=vProx` variant).
  - Restart service and validate both streams:
    - `journalctl -u vProx.service -f --output=cat`
    - `tail -f ~/.vProx/data/logs/main.log`

### 2026-02-19 14:42 (UTC) — jarvis agent v1.3 lazy policy + portable `new` repo workflow
- Goal:
  - Move agent startup behavior to lazy project-memory loading and define a portable, automation-ready `new` workflow for creating projects/repositories.
- Completed:
  - Upgraded `.github/agents/jarvis4.0.agent.md` to `v1.3`.
  - Switched bootstrap policy to lazy loading:
    - startup loads router + base only,
    - project memory loads only on explicit `load <project-name>`.
  - Added formal `new` guided flow with branch:
    - `Create new repo? (y/N)`
    - `No`: stay in current repo and scaffold locally
    - `Yes`: collect idea, analyze/fetch best practices, create repo under `vNodesV/<new-repo>`, create/start Codespace, then bootstrap implementation.
  - Added inheritance requirements for new repositories:
    - carry over `.gitignore` baseline policy,
    - copy agent config bundle (`.github/agents/jarvis4.0.agent.md`, `agents/jarvis4.0_state.md`, `agents/base.agent.md`, `agents/projects/_template.state.md`).
  - Synced command semantics in `agents/jarvis4.0_state.md` and `agents/base.agent.md` to match v1.3 behavior.
- Files changed:
  - `.github/agents/jarvis4.0.agent.md`
  - `agents/jarvis4.0_state.md`
  - `agents/base.agent.md`
  - `agents/projects/vprox.state.md`
- Verification:
  - Reviewed updated file contents for consistency across entrypoint/router/base directives.
  - Checked GitHub automation prerequisites at runtime:
    - `gh` installed (`2.83.1`)
    - authenticated as `vNodesV`
    - Codespaces listing works (`gh codespace list`).
- Open follow-ups:
  - Add optional “preflight gate” in agent spec to block `new` until repo-create and codespace prerequisites validate.
  - Rotate exposed GitHub token used in terminal output and avoid verbose token printing in future checks.
- Next first steps:
  - If desired, implement strict preflight checklist enforcement in `jarvis4.0.agent.md` for `new`.
  - If creating a new project, run `new` and follow the new guided branch flow.

### 2026-02-20 10:25 (UTC) — backup pipeline hardening + log schema/style refinement
- Goal:
  - Fix backup archive destination ambiguity and enforce operator-friendly backup status logging style in `main.log` after rotation.
- Completed:
  - Reworked backup flow in `internal/backup/backup.go`:
    - copy `main.log` to temp snapshot,
    - truncate `main.log`,
    - compress snapshot to `tar.gz`,
    - write first line in new `main.log` with structured backup status metadata.
  - Added explicit failure-path status logging with `failed=<reason>`.
  - Changed default archive destination from `.../logs/archived` to `.../logs/archives` in runtime, Makefile, and docs.
  - Removed post-backup extra line (`Backup completed`) so first line remains the structured backup entry.
  - Added human-readable size fields and then refined schema per operator requests:
    - removed underscore-heavy keys,
    - switched status/result-style values to uppercase text,
    - added explicit `module=BACKUP` field,
    - removed `filebytes` and `archivebytes` from output.
- Files changed:
  - `internal/backup/backup.go`
  - `cmd/vprox/main.go`
  - `Makefile`
  - `README.md`
  - `MIGRATION.md`
  - `MODULES.md`
  - `internal/backup/cfg/config.toml`
  - `internal/backup/cfg/config.json`
  - `agents/projects/vprox.state.md`
- Verification:
  - `gofmt -w internal/backup/backup.go cmd/vprox/main.go`
  - `go build ./...`
  - `go test ./...`
  - Multiple smoke runs with temp `VPROX_HOME` verified:
    - first line of `main.log` is backup status entry,
    - archive created under `~/.vProx/data/logs/archives`,
    - output style reflects uppercase values and no underscore-heavy field names.
- Open follow-ups:
  - Optional: add dedicated tests for `buildBackupStatusLine` and `RunOnce` filesystem behavior.
  - Optional: de-duplicate or preserve legacy field aliases if backward compatibility for downstream parsers is needed.
- Next first steps:
  - If schema is final, add regression tests for backup log line formatting and archive placement.
  - Run one production-host validation of `vProx backup` and inspect `journalctl` + `tail -f ~/.vProx/data/logs/main.log`.

### 2026-02-20 22:55 (UTC) — limiter/ws/logging + counter persistence + docs/agent hardening
- Goal:
  - Finalize runtime/operator experience: consistent limiter/access logging, access-counter persistence across restart/backup with explicit reset control, Apache/WS compatibility validation, and complete CLI/docs alignment.
- Completed:
  - Implemented access counter persistence in `cmd/vprox/main.go`:
    - counters now load from and save to `~/.vProx/data/access-counts.json`,
    - counters survive restart and backup by default,
    - added explicit reset path via `vProx backup --reset_count` (alias `--reset-count`).
  - Updated CLI/help surface for reset flags and backup semantics.
  - Hardened limiter logging in `internal/limit/limiter.go`:
    - blocked/canceled limiter paths emit explicit `status=limited` access lines,
    - limiter mirror events include normalized reason/status fields,
    - event text humanized while preserving machine-readable fields.
  - Improved stdout/journal readability by bolding key names in colorized rendering.
  - Reworked backup status outputs in `internal/backup/backup.go` to use structured lifecycle events:
    - `BACKUP STARTED`, `BACKUP COMPLETE`, `BACKUP FAILED`,
    - `request_id` and consistent field naming.
  - Validated Apache vhost compatibility with current `ws.go` routing (`/websocket`) and provided corrected templates for main/api/rpc deployments.
  - Synced documentation with deployed behavior:
    - `README.md`, `FLAGS.md`, `CLI_FLAGS_GUIDE.md`, `MODULES.md`, `MIGRATION.md`.
  - Reviewed and hardened agent config files:
    - de-duplicated `.github/agents/jarvis4.0.agent.md` header/content while preserving policy intent,
    - confirmed router/base consistency with lazy-load workflow.
  - Binary cleanup request handling:
    - removed local built binary artifact `.build/vProx`,
    - preserved tracked `ip2l/ip2location.mmdb` per operator requirement.
- Files changed:
  - `cmd/vprox/main.go`
  - `internal/limit/limiter.go`
  - `internal/backup/backup.go`
  - `README.md`
  - `FLAGS.md`
  - `CLI_FLAGS_GUIDE.md`
  - `MODULES.md`
  - `MIGRATION.md`
  - `.github/agents/jarvis4.0.agent.md`
  - `agents/projects/vprox.state.md`
- Verification:
  - Repeated `gofmt -w` on touched Go files.
  - Repeated `go build ./...` and `go test ./...` after major change batches.
  - Verified help output from source path: `go run ./cmd/vprox --help` includes `--reset_count` and `--reset-count`.
  - Smoke-tested limiter behavior with low RPS/burst and observed `status=limited` access + limiter entries.
  - Confirmed MMDB tracked/present and local build binary absent.
- Open follow-ups:
  - Add targeted tests for access-count persistence/load/reset paths and backup lifecycle log formatting.
  - Optional: simplify WS hard-timeout flow (`internal/ws/ws.go`) to remove duplicated timeout signaling path.
  - Optional: add preflight checklist section for `new` workflow in agent spec.
- Next first steps:
  - Add focused unit/integration tests for new persistence and backup-reset behaviors.
  - Run one host-level validation of service restart + backup reset workflow and inspect `journalctl`/`main.log`.

### 2026-02-21 10:48 (UTC) — v1.0.0 release polish + GitHub governance/CI hardening
- Goal:
  - Finalize professional repo posture for `v1.0.0`: release-ready documentation layout, hardened GitHub settings, CI/security workflow baseline, and enforced PR-only change flow.
- Completed:
  - Updated `README.md` to include a bottom “Additional Documentation” index listing non-README markdown docs.
  - Prepared high-level `v1.0.0` release description focused on highlights/features.
  - Audited live GitHub repo settings via `gh api` and identified missing protections/security automation.
  - Applied repository hardening settings (via local admin script run):
    - merge strategy tightened to squash-only,
    - delete branch on merge enabled,
    - auto-merge/update-branch enabled,
    - wiki disabled,
    - Dependabot vulnerability alerts enabled,
    - automated security fixes enabled,
    - Dependabot security updates enabled.
  - Enabled branch protection on `main` and then updated to require specific checks:
    - `Go build/test/lint`
    - `Dependency Review`
    - `Analyze (Go)`
    - with strict status checks and PR review requirements.
  - Added professional workflow stack:
    - `.github/workflows/ci.yml`
    - `.github/workflows/codeql.yml`
    - `.github/workflows/dependency-review.yml`
    - `.github/dependabot.yml`
  - Diagnosed push failure root cause (`GH006` protected branch): direct push to `main` blocked by PR-only policy and required checks.
  - Recovered push flow by branching and opening PR:
    - branch `chore/repo-hardening`
    - PR `#1` created against `main`.
  - Kept admin helper scripts local-only and ignored:
    - `scripts/harden_repo_settings.sh`
    - `scripts/set_required_checks.sh`
- Files changed:
  - `README.md`
  - `.gitignore`
  - `.github/workflows/ci.yml`
  - `.github/workflows/codeql.yml`
  - `.github/workflows/dependency-review.yml`
  - `.github/dependabot.yml`
  - `agents/projects/vprox.state.md`
  - local-only scripts (ignored): `scripts/harden_repo_settings.sh`, `scripts/set_required_checks.sh`
- Verification:
  - Confirmed branch protection JSON from GitHub API reflects PR-required policy and required checks.
  - Confirmed vulnerability alerts now return HTTP 204 and automated security fixes are enabled.
  - Validated workflow/dependabot YAML files with editor diagnostics (no errors).
  - Verified protected branch enforcement from actual push attempt and successful PR-based workaround.
  - Verified script ignore behavior with `git check-ignore -v`.
- Open follow-ups:
  - Merge PR `#1` after all required checks pass and approval is granted.
  - Optionally add `SECURITY.md`, `CODEOWNERS`, and issue/PR templates for additional professionalism.
  - Optionally add repo topics for discoverability (`go`, `reverse-proxy`, `cosmos-sdk`, `cometbft`, etc.).
- Next first steps:
  - Monitor PR `#1` checks and merge when green.
  - Add policy docs (`SECURITY.md`, `CODEOWNERS`) in a follow-up PR.
