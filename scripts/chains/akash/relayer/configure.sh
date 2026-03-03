#!/bin/bash
# Akash relayer configure — Hermes config, keys, channel paths
# Usage: bash configure.sh [--dry-run] [--add-key]
# Channels: akash <-> cosmos hub, akash <-> osmosis

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
CHAIN_DIR="$(cd "$SCRIPT_DIR/.." && pwd)"
source "$SCRIPT_DIR/../../../lib/common.sh"

CFG="$CHAIN_DIR/chain.toml"
load_chain_config "$CFG"

DRY_RUN=false
ADD_KEY=false
for arg in "$@"; do
    [[ "$arg" == "--dry-run" ]] && DRY_RUN=true && warn "DRY RUN — no changes will be made"
    [[ "$arg" == "--add-key" ]] && ADD_KEY=true
done

require_cmd hermes

HERMES_HOME="${HERMES_HOME:-${HOME}/.hermes}"
HERMES_CFG="${HERMES_HOME}/config.toml"

# ── Peer chains (add more as needed) ──────────────────────────────────────────
# Cosmos Hub
GAIA_RPC="${GAIA_RPC:-https://rpc.cosmos.directory/cosmoshub}"
GAIA_GRPC="${GAIA_GRPC:-https://grpc.cosmos.directory:443/cosmoshub}"
GAIA_CHAIN_ID="${GAIA_CHAIN_ID:-cosmoshub-4}"

# Osmosis
OSMOSIS_RPC="${OSMOSIS_RPC:-https://rpc.cosmos.directory/osmosis}"
OSMOSIS_GRPC="${OSMOSIS_GRPC:-https://grpc.cosmos.directory:443/osmosis}"
OSMOSIS_CHAIN_ID="${OSMOSIS_CHAIN_ID:-osmosis-1}"

# Akash local node
AKASH_RPC="http://localhost:${PORT_RPC}"
AKASH_GRPC="localhost:${PORT_GRPC}"

info "=== Hermes Configure ==="
info "  home: ${HERMES_HOME}"
info "  chains: ${CHAIN_ID}, ${GAIA_CHAIN_ID}, ${OSMOSIS_CHAIN_ID}"

# ── 1. Create Hermes home and config ─────────────────────────────────────────
$DRY_RUN || mkdir -p "$HERMES_HOME"

if [[ -f "$HERMES_CFG" ]]; then
    ok "Hermes config already exists (not overwriting)"
else
    info "Writing Hermes config.toml ..."
    $DRY_RUN || cat > "$HERMES_CFG" << TOML
[global]
log_level = "info"

[mode.clients]
enabled = true
refresh = true
misbehaviour = true

[mode.connections]
enabled = false

[mode.channels]
enabled = false

[mode.packets]
enabled = true
clear_interval = 100
clear_on_start = true
tx_confirmation = true
auto_register_counterparty_payee = false

[telemetry]
enabled = true
host = "127.0.0.1"
port = 3001

[[chains]]
id = "${CHAIN_ID}"
type = "CosmosSdk"
rpc_addr = "${AKASH_RPC}"
grpc_addr = "${AKASH_GRPC}"
rpc_timeout = "10s"
trusted_node = false
account_prefix = "akash"
key_name = "akash-relayer"
key_store_type = "Test"
store_prefix = "ibc"
default_gas = 100000
max_gas = 400000
gas_price = { price = 0.025, denom = "uakt" }
gas_multiplier = 1.2
max_msg_num = 30
max_tx_size = 2097152
clock_drift = "5s"
max_block_time = "30s"
trusting_period = "14days"
trust_threshold = { numerator = "1", denominator = "3" }
address_type = { derivation = "cosmos" }

[[chains]]
id = "${GAIA_CHAIN_ID}"
type = "CosmosSdk"
rpc_addr = "${GAIA_RPC}"
grpc_addr = "${GAIA_GRPC}"
rpc_timeout = "10s"
trusted_node = false
account_prefix = "cosmos"
key_name = "cosmos-relayer"
key_store_type = "Test"
store_prefix = "ibc"
default_gas = 100000
max_gas = 400000
gas_price = { price = 0.005, denom = "uatom" }
gas_multiplier = 1.2
max_msg_num = 30
max_tx_size = 2097152
clock_drift = "5s"
max_block_time = "30s"
trusting_period = "14days"
trust_threshold = { numerator = "1", denominator = "3" }
address_type = { derivation = "cosmos" }

[[chains]]
id = "${OSMOSIS_CHAIN_ID}"
type = "CosmosSdk"
rpc_addr = "${OSMOSIS_RPC}"
grpc_addr = "${OSMOSIS_GRPC}"
rpc_timeout = "10s"
trusted_node = false
account_prefix = "osmo"
key_name = "osmosis-relayer"
key_store_type = "Test"
store_prefix = "ibc"
default_gas = 100000
max_gas = 400000
gas_price = { price = 0.0025, denom = "uosmo" }
gas_multiplier = 1.2
max_msg_num = 30
max_tx_size = 2097152
clock_drift = "5s"
max_block_time = "30s"
trusting_period = "14days"
trust_threshold = { numerator = "1", denominator = "3" }
address_type = { derivation = "cosmos" }
TOML
    ok "Hermes config written → ${HERMES_CFG}"
fi

# ── 2. Validate config ────────────────────────────────────────────────────────
$DRY_RUN || {
    hermes --config "$HERMES_CFG" config validate && ok "Config is valid" || die "Config validation failed"
}

# ── 3. Add / restore relayer keys ────────────────────────────────────────────
if $ADD_KEY; then
    warn "You will need to restore mnemonic for each chain's relayer key."
    warn "Fund each address with enough gas tokens before starting the relayer."
    echo ""
    for chain_key in "akash-relayer:${CHAIN_ID}" "cosmos-relayer:${GAIA_CHAIN_ID}" "osmosis-relayer:${OSMOSIS_CHAIN_ID}"; do
        key="${chain_key%%:*}"
        chain="${chain_key##*:}"
        info "Restoring key '${key}' for chain '${chain}' ..."
        $DRY_RUN || hermes --config "$HERMES_CFG" keys add \
            --chain "$chain" \
            --key-name "$key" \
            --mnemonic-file /dev/stdin
        echo ""
    done
else
    info "Keys not added — re-run with --add-key when ready to add relayer mnemonics"
fi

# ── 4. Systemd unit for Hermes ────────────────────────────────────────────────
HERMES_SERVICE="/etc/systemd/system/hermes.service"
require_sudo
if [[ -f "$HERMES_SERVICE" ]]; then
    ok "Hermes systemd service already installed"
else
    info "Installing Hermes systemd service ..."
    HERMES_BIN=$(command -v hermes)
    $DRY_RUN || sudo -n tee "$HERMES_SERVICE" > /dev/null << EOF
[Unit]
Description=Hermes IBC Relayer (Akash)
After=network-online.target akashd.service
Wants=network-online.target

[Service]
User=${NODE_USER}
Group=${NODE_USER}
ExecStart=${HERMES_BIN} --config ${HERMES_CFG} start
Restart=on-failure
RestartSec=10
LimitNOFILE=65535

[Install]
WantedBy=multi-user.target
EOF
    $DRY_RUN || {
        sudo -n systemctl daemon-reload
        sudo -n systemctl enable hermes
        ok "Hermes service installed and enabled (not started — add keys first)"
    }
fi

info "=== Hermes configure complete ==="
info "  Add keys:  bash configure.sh --add-key"
info "  Start:     sudo systemctl start hermes"
info "  Logs:      journalctl -u hermes -f"
info "  Status:    hermes --config ${HERMES_CFG} health-check"
