#!/bin/bash
# Akash validator — create or import validator key
# Usage: bash create_key.sh [--import] [--dry-run]
#   default: generates new key named $MONIKER
#   --import: restores from mnemonic (interactive)

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
CHAIN_DIR="$(cd "$SCRIPT_DIR/.." && pwd)"
source "$SCRIPT_DIR/../../../lib/common.sh"

CFG="$CHAIN_DIR/chain.toml"
load_chain_config "$CFG"

DRY_RUN=false
IMPORT=false
for arg in "$@"; do
    [[ "$arg" == "--dry-run" ]] && DRY_RUN=true && warn "DRY RUN — no changes will be made"
    [[ "$arg" == "--import"  ]] && IMPORT=true
done

require_cmd "$BINARY" jq

KEY_NAME="${MONIKER}"
KEYRING="os"

info "=== Akash Validator Key ==="
info "  key name: ${KEY_NAME}"
info "  keyring:  ${KEYRING}"
info "  home:     ${NODE_HOME}"

# ── Check if key already exists ───────────────────────────────────────────────
KEY_EXISTS=false
if "$BINARY" keys show "$KEY_NAME" --keyring-backend "$KEYRING" --home "$NODE_HOME" &>/dev/null; then
    KEY_EXISTS=true
    ok "Key '${KEY_NAME}' already exists"
    "$BINARY" keys show "$KEY_NAME" --keyring-backend "$KEYRING" --home "$NODE_HOME" --output json \
        | jq '{name: .name, address: .address, pubkey: .pubkey}'
fi

if $KEY_EXISTS && ! $IMPORT; then
    warn "Key exists. Use --import to overwrite with a mnemonic restore."
    exit 0
fi

$DRY_RUN && { info "[dry-run] would create/import key '${KEY_NAME}'"; exit 0; }

# ── Import from mnemonic ──────────────────────────────────────────────────────
if $IMPORT; then
    warn "You are about to restore a key from mnemonic."
    warn "This is IRREVERSIBLE if a key with this name exists — it will be overwritten."
    read -rp "Confirm restore for key '${KEY_NAME}'? [yes/N] " confirm
    [[ "$confirm" == "yes" ]] || die "Aborted"
    "$BINARY" keys add "$KEY_NAME" \
        --recover \
        --keyring-backend "$KEYRING" \
        --home "$NODE_HOME"

# ── Generate new key ──────────────────────────────────────────────────────────
else
    info "Generating new key '${KEY_NAME}' ..."
    echo ""
    echo -e "${RED}╔════════════════════════════════════════════════════════════╗${NC}"
    echo -e "${RED}║  SAVE YOUR MNEMONIC — IT WILL NOT BE SHOWN AGAIN          ║${NC}"
    echo -e "${RED}╚════════════════════════════════════════════════════════════╝${NC}"
    echo ""
    "$BINARY" keys add "$KEY_NAME" \
        --keyring-backend "$KEYRING" \
        --home "$NODE_HOME"
fi

echo ""
info "Key details:"
"$BINARY" keys show "$KEY_NAME" \
    --keyring-backend "$KEYRING" \
    --home "$NODE_HOME" \
    --output json | jq '{name: .name, address: .address}'

info "=== Fund this address with AKT before running create_validator.sh ==="
