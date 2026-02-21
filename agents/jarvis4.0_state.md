# jarvis4.0 router state (local-only)

This file routes multi-project memory handling.
All paths here are local-only (`agents/` is gitignored).

## Active project
- `vprox`

## Load order (new session, lazy project memory)
1. `agents/jarvis4.0_state.md` (this file)
2. `agents/base.agent.md` (global rules)

Project memory is loaded only when `load <project-name>` is requested.

## Command protocol

### `load <project-name>`
Switch active project context.

Actions:
- Set **Active project** in this file.
- Read:
  - `agents/base.agent.md`
  - `agents/projects/<project-name>.state.md`
- If file does not exist, prompt to run `save new <project-name>`.

Examples:
- `load vprox`
- `load chain-ops`

### `save` (or `save state`)
Append a memory dump to the current active project file:
- `agents/projects/<active-project>.state.md`

Required fields:
- timestamp (UTC)
- goal
- completed
- files changed
- verification
- open follow-ups
- next first steps

### `save new <project-name>`
Create a new project state file and switch active project.

Actions:
- Create `agents/projects/<project-name>.state.md` from template.
- Set **Active project** to `<project-name>` in this file.
- Add initial seed section for the new project.

Examples:
- `save new cosmo-relayer`
- `save new sdk-migration-lab`

### `new`
Guided bootstrap command for project initialization.

Flow:
1. Ask: `Create new repo? (y/N)`
2. If **No**:
  - Stay in current repo.
  - Create missing local project state files from template as needed.
  - Set active project and continue.
3. If **Yes**:
  - Ask for project idea.
  - Analyze requirements and gather missing best-practice knowledge from the web.
  - Propose architecture/stack and initialization plan.
  - Create repository under `vNodesV/<new-repo>`.
  - Create/start Codespace for that repository.
  - Apply same `.gitignore` policy baseline as the current repo, then adjust only for stack-specific needs.
  - Copy agent config bundle (`.github/agents/jarvis4.0.agent.md`, `agents/jarvis4.0_state.md`, `agents/base.agent.md`, `agents/projects/_template.state.md`).
  - Begin implementation and write initial handoff notes.

## Naming rules
- Use lowercase slug names for project files.
- Allowed chars: `a-z`, `0-9`, `-`, `_`.
