#!/bin/bash
# Akash provider configure — provider wallet, pricing, provider.yaml, TLS
# Usage: bash configure.sh [--dry-run]
# Prerequisites: provider/install.sh completed, provider wallet funded (min 5 AKT)

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
CHAIN_DIR="$(cd "$SCRIPT_DIR/.." && pwd)"
source "$SCRIPT_DIR/../../../lib/common.sh"

CFG="$CHAIN_DIR/chain.toml"
load_chain_config "$CFG"

DRY_RUN=false
[[ "${1:-}" == "--dry-run" ]] && DRY_RUN=true && warn "DRY RUN — no changes will be made"

require_cmd provider-services kubectl helm jq

# ── Required env vars ─────────────────────────────────────────────────────────
PROVIDER_DOMAIN="${PROVIDER_DOMAIN:-}"
PROVIDER_KEY_NAME="${PROVIDER_KEY_NAME:-provider}"
PROVIDER_KEYRING="${PROVIDER_KEYRING:-os}"
PROVIDER_HOME="${PROVIDER_HOME:-${HOME}/.akash-provider}"

[[ -z "$PROVIDER_DOMAIN" ]] && die "PROVIDER_DOMAIN is required (e.g. provider.vnodesv.net)"

PROVIDER_ADDRESS="${PROVIDER_ADDRESS:-}"  # populated below if empty

info "=== Akash Provider Configure ==="
info "  domain: ${PROVIDER_DOMAIN}"
info "  home:   ${PROVIDER_HOME}"

# ── 1. Provider wallet ────────────────────────────────────────────────────────
$DRY_RUN || mkdir -p "$PROVIDER_HOME"

if provider-services keys show "$PROVIDER_KEY_NAME" \
    --keyring-backend "$PROVIDER_KEYRING" \
    --home "$PROVIDER_HOME" &>/dev/null; then
    ok "Provider key '${PROVIDER_KEY_NAME}' already exists"
else
    info "Creating provider wallet ..."
    warn "SAVE THE MNEMONIC — fund this address with at least 5 AKT before deploying"
    $DRY_RUN || provider-services keys add "$PROVIDER_KEY_NAME" \
        --keyring-backend "$PROVIDER_KEYRING" \
        --home "$PROVIDER_HOME"
fi

if ! $DRY_RUN; then
    PROVIDER_ADDRESS=$(provider-services keys show "$PROVIDER_KEY_NAME" \
        --keyring-backend "$PROVIDER_KEYRING" \
        --home "$PROVIDER_HOME" -a 2>/dev/null)
    ok "Provider address: ${PROVIDER_ADDRESS}"

    # Check balance
    BALANCE=$(akash query bank balances "$PROVIDER_ADDRESS" \
        --node "tcp://localhost:${PORT_RPC}" --output json 2>/dev/null \
        | jq -r '.balances[] | select(.denom=="uakt") | .amount' 2>/dev/null || echo "0")
    info "Provider balance: ${BALANCE} uakt"
    [[ "${BALANCE:-0}" -lt 5000000 ]] && warn "Balance < 5 AKT — fund before deploy"
fi

# ── 2. Export key to PEM (required by provider daemon) ────────────────────────
KEY_PEM="${PROVIDER_HOME}/key.pem"
if [[ -f "$KEY_PEM" ]]; then
    ok "key.pem already exists"
else
    info "Exporting provider key to PEM ..."
    $DRY_RUN && { info "[dry-run] would export key.pem"; } || {
        echo "" | provider-services keys export "$PROVIDER_KEY_NAME" \
            --keyring-backend "$PROVIDER_KEYRING" \
            --home "$PROVIDER_HOME" \
            --unarmored-hex --unsafe 2>/dev/null \
            | xxd -r -p | base64 > "$KEY_PEM"
        chmod 600 "$KEY_PEM"
        ok "key.pem exported → ${KEY_PEM}"
    }
fi

# ── 3. Pricing script ─────────────────────────────────────────────────────────
PRICING_SCRIPT="${PROVIDER_HOME}/pricing_script.sh"
if [[ -f "$PRICING_SCRIPT" ]]; then
    ok "pricing_script.sh already exists"
else
    info "Writing default pricing script ..."
    $DRY_RUN || cat > "$PRICING_SCRIPT" << 'PRICING'
#!/bin/bash
# Akash provider pricing script
# Reads resources from stdin (JSON), prints uakt price to stdout
# Tune these rates for your hardware costs

cpu_price=1.60       # uakt per 1000 millicpu/block
memory_price=0.80    # uakt per MB/block
storage_price=0.02   # uakt per MB/block
gpu_price=100.00     # uakt per GPU/block

cpu=$(    jq -r '.cpu    // 0' <<< "$1")
memory=$( jq -r '.memory // 0' <<< "$1")
storage=$(jq -r '.storage // 0' <<< "$1")
gpu=$(    jq -r '.gpu    // 0' <<< "$1")

total=$(echo "scale=0; ($cpu * $cpu_price + $memory * $memory_price + $storage * $storage_price + $gpu * $gpu_price) / 1" | bc)
echo "${total:-1}"
PRICING
    chmod +x "$PRICING_SCRIPT"
    ok "pricing_script.sh written"
fi

# ── 4. provider.yaml ──────────────────────────────────────────────────────────
PROVIDER_YAML="${PROVIDER_HOME}/provider.yaml"
if [[ -f "$PROVIDER_YAML" ]] && ! $DRY_RUN; then
    ok "provider.yaml already exists (not overwriting)"
else
    info "Writing provider.yaml ..."
    $DRY_RUN || cat > "$PROVIDER_YAML" << YAML
---
from: "${PROVIDER_ADDRESS:-FILL_IN_PROVIDER_ADDRESS}"
key: "${KEY_PEM}"
keyringsBackend: "${PROVIDER_KEYRING}"
domain: "${PROVIDER_DOMAIN}"
node: "http://localhost:${PORT_RPC}"
withdrawalperiod: "24h"
attributes:
  - key: region
    value: "qc-ca"
  - key: host
    value: "vNodesV"
  - key: tier
    value: "community"
  - key: organization
    value: "vNodesV"
YAML
    ok "provider.yaml written → ${PROVIDER_YAML}"
fi

# ── 5. On-chain provider registration ────────────────────────────────────────
info "Checking on-chain provider registration ..."
$DRY_RUN || {
    if akash query provider get "$PROVIDER_ADDRESS" \
        --node "tcp://localhost:${PORT_RPC}" &>/dev/null 2>&1; then
        ok "Provider already registered on-chain"
    else
        warn "Provider not registered — registering now ..."
        provider-services tx provider create "${PROVIDER_YAML}" \
            --from "$PROVIDER_KEY_NAME" \
            --keyring-backend "$PROVIDER_KEYRING" \
            --home "$PROVIDER_HOME" \
            --node "tcp://localhost:${PORT_RPC}" \
            --chain-id "$CHAIN_ID" \
            --gas auto --gas-adjustment 1.4 --fees "5000uakt" -y
        ok "Provider registered on-chain"
    fi
}

info "=== Provider configure complete. Next: run deploy.sh ==="
info "  Verify: akash query provider get ${PROVIDER_ADDRESS:-<address>} --node tcp://localhost:${PORT_RPC}"
