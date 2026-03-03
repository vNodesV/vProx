#!/bin/bash
# Akash node configure — init node, apply config, download genesis
# Usage: bash configure.sh [--dry-run] [--reset]
# Idempotent: safe to re-run unless --reset is passed.

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
CHAIN_DIR="$(cd "$SCRIPT_DIR/.." && pwd)"
source "$SCRIPT_DIR/../../../lib/common.sh"

CFG="$CHAIN_DIR/chain.toml"
load_chain_config "$CFG"

DRY_RUN=false
RESET=false
for arg in "$@"; do
    [[ "$arg" == "--dry-run" ]] && DRY_RUN=true && warn "DRY RUN — no changes will be made"
    [[ "$arg" == "--reset"   ]] && RESET=true  && warn "RESET — will wipe existing node data"
done

require_cmd "$BINARY" jq curl

COMETBFT_CFG="${NODE_HOME}/config/config.toml"
APP_CFG="${NODE_HOME}/config/app.toml"
CLIENT_CFG="${NODE_HOME}/config/client.toml"
GENESIS="${NODE_HOME}/config/genesis.json"

info "=== Akash Node Configure ==="
info "  home:    ${NODE_HOME}"
info "  moniker: ${MONIKER}"
info "  chain:   ${CHAIN_ID}"

# ── 1. Init node ─────────────────────────────────────────────────────────────
if [[ -f "$COMETBFT_CFG" ]] && ! $RESET; then
    ok "Node already initialised — skipping init (use --reset to force)"
else
    info "Initialising node ..."
    $DRY_RUN || {
        $RESET && rm -rf "${NODE_HOME}/config" "${NODE_HOME}/data"
        "$BINARY" init "$MONIKER" --chain-id "$CHAIN_ID" --home "$NODE_HOME" 2>/dev/null
        ok "Node initialised"
    }
fi

# ── 2. Download genesis ───────────────────────────────────────────────────────
if [[ -f "$GENESIS" ]] && ! $RESET; then
    GENESIS_CHAIN=$(jq -r '.chain_id' "$GENESIS" 2>/dev/null)
    if [[ "$GENESIS_CHAIN" == "$CHAIN_ID" ]]; then
        ok "Genesis already present (chain_id: ${GENESIS_CHAIN})"
    else
        warn "Genesis chain_id mismatch (got: $GENESIS_CHAIN) — re-downloading"
        $DRY_RUN || safe_download "$GENESIS_URL" "$GENESIS"
    fi
else
    info "Downloading genesis ..."
    $DRY_RUN || {
        safe_download "$GENESIS_URL" "$GENESIS"
        ok "Genesis downloaded (chain_id: $(jq -r '.chain_id' "$GENESIS"))"
    }
fi

# ── 3. config.toml (CometBFT) ────────────────────────────────────────────────
info "Applying config.toml settings ..."
$DRY_RUN || {
    # Seeds
    sed -i "s|^seeds = .*|seeds = \"${SEEDS}\"|" "$COMETBFT_CFG"
    # Persistent peers
    [[ -n "$PEERS" ]] && sed -i "s|^persistent_peers = .*|persistent_peers = \"${PEERS}\"|" "$COMETBFT_CFG"
    # Ports
    sed -i "s|^laddr = \"tcp://127.0.0.1:26657\"|laddr = \"tcp://127.0.0.1:${PORT_RPC}\"|" "$COMETBFT_CFG"
    sed -i "s|^laddr = \"tcp://0.0.0.0:26656\"|laddr = \"tcp://0.0.0.0:${PORT_P2P}\"|"     "$COMETBFT_CFG"
    # Prometheus
    sed -i 's|^prometheus = false|prometheus = true|' "$COMETBFT_CFG"
    ok "config.toml updated"
}

# ── 4. app.toml ───────────────────────────────────────────────────────────────
info "Applying app.toml settings ..."
$DRY_RUN || {
    sed -i "s|^minimum-gas-prices = .*|minimum-gas-prices = \"${MIN_GAS}\"|" "$APP_CFG"
    # API server
    sed -i '/^\[api\]/,/^\[/ s|^enable = false|enable = true|'  "$APP_CFG"
    sed -i "s|^address = \"tcp://0.0.0.0:1317\"|address = \"tcp://0.0.0.0:${PORT_API}\"|"   "$APP_CFG"
    # gRPC
    sed -i '/^\[grpc\]/,/^\[/ s|^enable = false|enable = true|' "$APP_CFG"
    sed -i "s|^address = \"0.0.0.0:9090\"|address = \"0.0.0.0:${PORT_GRPC}\"|"              "$APP_CFG"
    # Pruning — default for validator (keep recent)
    sed -i 's|^pruning = .*|pruning = "default"|' "$APP_CFG"
    ok "app.toml updated"
}

# ── 5. client.toml ───────────────────────────────────────────────────────────
info "Applying client.toml settings ..."
$DRY_RUN || {
    sed -i "s|^chain-id = .*|chain-id = \"${CHAIN_ID}\"|"                       "$CLIENT_CFG"
    sed -i "s|^node = .*|node = \"tcp://localhost:${PORT_RPC}\"|"                "$CLIENT_CFG"
    sed -i 's|^keyring-backend = .*|keyring-backend = "os"|'                    "$CLIENT_CFG"
    ok "client.toml updated"
}

info "=== Configure complete. Next: run service.sh to install systemd unit ==="
