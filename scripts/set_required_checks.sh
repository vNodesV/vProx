#!/usr/bin/env bash
set -euo pipefail

OWNER="vNodesV"
REPO="vProx"
FULL_REPO="${OWNER}/${REPO}"
BRANCH="main"

# Update this list if workflow/job names change.
REQUIRED_CONTEXTS=(
  "Go build/test/lint"
  "Dependency Review"
  "Analyze (Go)"
  "Required reviewer approved"
)

run_gh() {
  env -u GITHUB_TOKEN -u GH_TOKEN gh "$@"
}

echo "==> Using GitHub OAuth credential (env token bypassed)..."
run_gh auth status -h github.com >/dev/null

echo "==> Applying branch protection with required status checks on ${FULL_REPO}:${BRANCH}"

# Build JSON contexts array
contexts_json=$(printf '%s\n' "${REQUIRED_CONTEXTS[@]}" | jq -R . | jq -s .)

jq -n \
  --argjson contexts "${contexts_json}" \
  '{
    required_status_checks: {
      strict: true,
      contexts: $contexts
    },
    enforce_admins: true,
    required_pull_request_reviews: {
      dismiss_stale_reviews: true,
      require_code_owner_reviews: true,
      required_approving_review_count: 1
    },
    restrictions: null,
    required_linear_history: true,
    allow_force_pushes: false,
    allow_deletions: false,
    block_creations: false,
    required_conversation_resolution: true,
    lock_branch: false,
    allow_fork_syncing: false
  }' \
| run_gh api \
    -X PUT "repos/${FULL_REPO}/branches/${BRANCH}/protection" \
    -H "Accept: application/vnd.github+json" \
    --input -

echo "==> Verification: required status checks"
run_gh api "repos/${FULL_REPO}/branches/${BRANCH}/protection" --jq '.required_status_checks'

echo "✅ Done: required checks configured."
