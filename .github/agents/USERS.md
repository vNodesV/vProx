# Agent user assignments

Requested identity mapping:

- `jarvis4.0` → `.github/agents/jarvis4.0.agent.md`
- `jarvisBoss` → `.github/agents/reviewer.agent.md`

Resolved GitHub identities:

- Org: `vNodes-Co`
- Reviewer login: `jarvisBoss`

## GitHub platform constraints

- GitHub user accounts must be created directly on GitHub (they cannot be provisioned from repository files/workflows).
- This repository is owned by a **user** account (`vNodesV`), not an organization, so team-based reviewer routing is unavailable.
- The username format for GitHub accounts does not support dots (`.`), so `jarvis4.0` is a logical ID in this repo policy, not a valid GitHub login.

## Enforcement behavior

- PR approval enforcement is implemented via `.github/workflows/required-reviewer.yml`.
- Current required approver set is `jarvisBoss`.
- Automated JB approval is implemented via `.github/workflows/jb-auto-approve.yml`.

## Required GitHub secret

- Add repository secret: `JB_REVIEW_TOKEN`
- Value: a PAT for user `jarvisBoss` with permissions to read checks and write pull request reviews.

Recommended minimal scopes:
- Fine-grained PAT scoped to repository `vNodesV/vProx`
- Repository permissions:
	- Pull requests: Read and write
	- Checks: Read
	- Contents: Read