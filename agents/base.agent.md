# jarvis4.0 base directives (local-only)

This file stores cross-project collaboration rules.
Project-specific memory goes in `agents/projects/<project>.state.md`.

## Start-of-session protocol
1. Read `agents/jarvis4.0_state.md` (router + current project pointer).
2. Read this file (`base.agent.md`).
3. Do not auto-load project state.
4. Load project memory only on explicit `load <project>`.
5. Confirm unresolved work before editing code.

## End-of-session protocol
- If user says `save` / `save state`: append a memory dump to current project file.
- If user says `save new <project>`: create `agents/projects/<project>.state.md` from template and set current project in router.
- If user says `new`: run guided bootstrap flow from router policy (`Create new repo? (y/N)` branch).
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
