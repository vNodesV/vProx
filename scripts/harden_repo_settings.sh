#!/usr/bin/env bash
set -euo pipefail

# Hardens GitHub repository settings for vNodesV/vProx.
# Requires gh CLI authenticated with a token/account that has repo admin rights.

OWNER="vNodesV"
REPO="vProx"
FULL_REPO="${OWNER}/${REPO}"

run_gh() {
  env -u GITHUB_TOKEN -u GH_TOKEN gh "$@"
}

echo "==> Checking GitHub auth (OAuth credential, env token bypassed)..."
run_gh auth status -h github.com

echo "==> Applying repository-level merge/security settings..."
run_gh api \
  -X PATCH "repos/${FULL_REPO}" \
  -f allow_merge_commit=false \
  -f allow_rebase_merge=false \
  -f allow_squash_merge=true \
  -f delete_branch_on_merge=true \
  -f allow_auto_merge=true \
  -f allow_update_branch=true \
  -f has_wiki=false

echo "==> Enabling Dependabot vulnerability alerts..."
run_gh api -X PUT "repos/${FULL_REPO}/vulnerability-alerts"

echo "==> Enabling automated security fixes..."
run_gh api -X PUT "repos/${FULL_REPO}/automated-security-fixes"

echo "==> Enabling Dependabot security updates..."
run_gh api \
  -X PATCH "repos/${FULL_REPO}" \
  -f security_and_analysis[dependabot_security_updates][status]=enabled

echo "==> Applying branch protection on main..."
run_gh api \
  -X PUT "repos/${FULL_REPO}/branches/main/protection" \
  -H "Accept: application/vnd.github+json" \
  --input - <<'JSON'
{
  "required_status_checks": null,
  "enforce_admins": true,
  "required_pull_request_reviews": {
    "dismiss_stale_reviews": true,
    "require_code_owner_reviews": false,
    "required_approving_review_count": 1
  },
  "restrictions": null,
  "required_linear_history": true,
  "allow_force_pushes": false,
  "allow_deletions": false,
  "block_creations": false,
  "required_conversation_resolution": true,
  "lock_branch": false,
  "allow_fork_syncing": true
}
JSON

echo

echo "==> Verification: repository settings"
run_gh api "repos/${FULL_REPO}" --jq '{allow_merge_commit,allow_rebase_merge,allow_squash_merge,delete_branch_on_merge,allow_auto_merge,allow_update_branch,has_wiki,security_and_analysis}'

echo "==> Verification: main branch protection"
run_gh api "repos/${FULL_REPO}/branches/main/protection" --jq '{required_pull_request_reviews,enforce_admins,required_linear_history,allow_force_pushes,allow_deletions,required_conversation_resolution}'

echo "==> Verification: vulnerability alerts"
run_gh api "repos/${FULL_REPO}/vulnerability-alerts" -i | head -n 20

echo "==> Verification: automated security fixes"
run_gh api "repos/${FULL_REPO}/automated-security-fixes" -i | head -n 20

echo "✅ Done: ${FULL_REPO} hardening applied."
