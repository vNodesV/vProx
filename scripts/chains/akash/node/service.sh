#!/bin/bash
# Akash node service — install, enable, and start cosmovisor systemd unit
# Usage: bash service.sh [--dry-run] [--restart]
# Requires: passwordless sudo for systemctl + tee + install

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
CHAIN_DIR="$(cd "$SCRIPT_DIR/.." && pwd)"
source "$SCRIPT_DIR/../../../lib/common.sh"

CFG="$CHAIN_DIR/chain.toml"
load_chain_config "$CFG"

DRY_RUN=false
RESTART_ONLY=false
for arg in "$@"; do
    [[ "$arg" == "--dry-run" ]] && DRY_RUN=true  && warn "DRY RUN — no changes will be made"
    [[ "$arg" == "--restart" ]] && RESTART_ONLY=true
done

require_sudo
require_cmd cosmovisor

SERVICE_NAME="akashd"
SERVICE_FILE="/etc/systemd/system/${SERVICE_NAME}.service"
COSMOVISOR_BIN=$(command -v cosmovisor)

info "=== Akash Node Service ==="
info "  service:    ${SERVICE_NAME}"
info "  user:       ${NODE_USER}"
info "  home:       ${NODE_HOME}"
info "  cosmovisor: ${COSMOVISOR_BIN}"

# ── Restart only ──────────────────────────────────────────────────────────────
if $RESTART_ONLY; then
    info "Restarting ${SERVICE_NAME} ..."
    $DRY_RUN || sudo -n systemctl restart "$SERVICE_NAME"
    ok "Restarted"
    sudo -n systemctl status "$SERVICE_NAME" --no-pager -l | head -20
    exit 0
fi

# ── 1. Write systemd unit ─────────────────────────────────────────────────────
info "Writing ${SERVICE_FILE} ..."
$DRY_RUN || sudo -n tee "$SERVICE_FILE" > /dev/null << EOF
[Unit]
Description=Akash Network Node (cosmovisor)
After=network-online.target
Wants=network-online.target

[Service]
User=${NODE_USER}
Group=${NODE_USER}
ExecStart=${COSMOVISOR_BIN} run start --home ${NODE_HOME}
Restart=on-failure
RestartSec=5
LimitNOFILE=65535

# cosmovisor environment
Environment="DAEMON_NAME=${BINARY}"
Environment="DAEMON_HOME=${NODE_HOME}"
Environment="DAEMON_ALLOW_DOWNLOAD_BINARIES=true"
Environment="DAEMON_RESTART_AFTER_UPGRADE=true"
Environment="DAEMON_LOG_BUFFER_SIZE=512"
Environment="UNSAFE_SKIP_BACKUP=true"
Environment="HOME=/home/${NODE_USER}"

[Install]
WantedBy=multi-user.target
EOF
ok "Service file written"

# ── 2. Reload, enable, start ──────────────────────────────────────────────────
$DRY_RUN || {
    sudo -n systemctl daemon-reload
    sudo -n systemctl enable "$SERVICE_NAME"
    ok "Service enabled"

    if sudo -n systemctl is-active --quiet "$SERVICE_NAME"; then
        warn "Service already running — use --restart to restart"
    else
        info "Starting ${SERVICE_NAME} ..."
        sudo -n systemctl start "$SERVICE_NAME"
        sleep 2
        if sudo -n systemctl is-active --quiet "$SERVICE_NAME"; then
            ok "Service is running"
        else
            warn "Service may not have started — check: journalctl -u ${SERVICE_NAME} -n 50"
        fi
    fi
}

# ── 3. Status ──────────────────────────────────────────────────────────────────
$DRY_RUN || sudo -n systemctl status "$SERVICE_NAME" --no-pager -l | head -25

info "=== Service setup complete ==="
info "  Logs:   journalctl -u ${SERVICE_NAME} -f"
info "  Status: systemctl status ${SERVICE_NAME}"
info "  Sync:   ${BINARY} status --home ${NODE_HOME} | jq .SyncInfo"
