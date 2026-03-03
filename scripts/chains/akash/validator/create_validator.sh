#!/bin/bash
# Akash validator — submit MsgCreateValidator transaction
# Usage: bash create_validator.sh [--dry-run]
# Prerequisites:
#   1. Node is synced (catching_up = false)
#   2. Key exists and is funded (min ~10 AKT)
#   3. configure.sh has been run

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
CHAIN_DIR="$(cd "$SCRIPT_DIR/.." && pwd)"
source "$SCRIPT_DIR/../../../lib/common.sh"

CFG="$CHAIN_DIR/chain.toml"
load_chain_config "$CFG"

DRY_RUN=false
[[ "${1:-}" == "--dry-run" ]] && DRY_RUN=true && warn "DRY RUN — no tx will be broadcast"

require_cmd "$BINARY" jq

KEY_NAME="${MONIKER}"
KEYRING="os"

# ── Configurable validator params (override via env) ─────────────────────────
STAKE_AMOUNT="${STAKE_AMOUNT:-10000000uakt}"             # 10 AKT
COMMISSION_RATE="${COMMISSION_RATE:-0.05}"               # 5%
COMMISSION_MAX="${COMMISSION_MAX:-0.20}"                 # 20%
COMMISSION_MAX_CHANGE="${COMMISSION_MAX_CHANGE:-0.01}"   # 1%/day
MIN_SELF_DELEGATION="${MIN_SELF_DELEGATION:-1}"

info "=== Akash Create Validator ==="
info "  moniker:     ${MONIKER}"
info "  key:         ${KEY_NAME}"
info "  stake:       ${STAKE_AMOUNT}"
info "  commission:  ${COMMISSION_RATE} (max ${COMMISSION_MAX})"

# ── 1. Verify node is synced ──────────────────────────────────────────────────
info "Checking sync status ..."
SYNC=$("$BINARY" status --home "$NODE_HOME" 2>/dev/null | jq -r '.SyncInfo.catching_up' 2>/dev/null || echo "unknown")
if [[ "$SYNC" == "true" ]]; then
    die "Node is still syncing. Wait for catching_up = false before creating validator."
elif [[ "$SYNC" == "unknown" ]]; then
    warn "Could not determine sync status — node may not be running"
    read -rp "Continue anyway? [yes/N] " c; [[ "$c" == "yes" ]] || die "Aborted"
else
    ok "Node is synced (catching_up: false)"
fi

# ── 2. Verify key and balance ─────────────────────────────────────────────────
info "Checking key and balance ..."
ADDR=$("$BINARY" keys show "$KEY_NAME" --keyring-backend "$KEYRING" --home "$NODE_HOME" -a 2>/dev/null) \
    || die "Key '${KEY_NAME}' not found — run create_key.sh first"
ok "Address: ${ADDR}"

BALANCE=$("$BINARY" query bank balances "$ADDR" --home "$NODE_HOME" \
    --node "tcp://localhost:${PORT_RPC}" --output json 2>/dev/null \
    | jq -r '.balances[] | select(.denom=="uakt") | .amount' 2>/dev/null || echo "0")
info "Balance: ${BALANCE} uakt"
[[ "${BALANCE:-0}" -lt 10000000 ]] && warn "Balance < 10 AKT — ensure sufficient funds for stake + gas"

# ── 3. Get validator pubkey ───────────────────────────────────────────────────
PUBKEY=$("$BINARY" tendermint show-validator --home "$NODE_HOME" 2>/dev/null) \
    || die "Could not get validator pubkey — is node initialized?"
ok "Validator pubkey obtained"

# ── 4. Create validator.json ──────────────────────────────────────────────────
VAL_JSON="${NODE_HOME}/validator.json"
cat > "$VAL_JSON" << VALJSON
{
  "pubkey": ${PUBKEY},
  "amount": "${STAKE_AMOUNT}",
  "moniker": "${MONIKER}",
  "commission-rate": "${COMMISSION_RATE}",
  "commission-max-rate": "${COMMISSION_MAX}",
  "commission-max-change-rate": "${COMMISSION_MAX_CHANGE}",
  "min-self-delegation": "${MIN_SELF_DELEGATION}"
}
VALJSON
ok "validator.json written: ${VAL_JSON}"
cat "$VAL_JSON"

$DRY_RUN && { info "[dry-run] would broadcast MsgCreateValidator — exiting"; exit 0; }

# ── 5. Broadcast ──────────────────────────────────────────────────────────────
echo ""
warn "About to broadcast MsgCreateValidator on ${CHAIN_ID}"
read -rp "Confirm? [yes/N] " confirm
[[ "$confirm" == "yes" ]] || die "Aborted"

"$BINARY" tx staking create-validator "$VAL_JSON" \
    --from "$KEY_NAME" \
    --keyring-backend "$KEYRING" \
    --home "$NODE_HOME" \
    --node "tcp://localhost:${PORT_RPC}" \
    --chain-id "$CHAIN_ID" \
    --gas auto \
    --gas-adjustment 1.4 \
    --fees "5000uakt" \
    -y

ok "=== Validator creation tx submitted ==="
info "Check: ${BINARY} query staking validator \$(${BINARY} keys show ${KEY_NAME} --bech val -a --keyring-backend ${KEYRING} --home ${NODE_HOME})"
