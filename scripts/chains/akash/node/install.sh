#!/bin/bash
# Akash node install — downloads binary + sets up cosmovisor
# Usage: sudo -u <user> bash install.sh [--dry-run]
# Idempotent: safe to re-run; skips steps already complete.

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
CHAIN_DIR="$(cd "$SCRIPT_DIR/.." && pwd)"
source "$SCRIPT_DIR/../../../lib/common.sh"

CFG="$CHAIN_DIR/chain.toml"
load_chain_config "$CFG"

DRY_RUN=false
[[ "${1:-}" == "--dry-run" ]] && DRY_RUN=true && warn "DRY RUN — no changes will be made"

ARCH=$(detect_arch)
INSTALL_DIR="/usr/local/bin"
COSMOVISOR_HOME="${NODE_HOME}/cosmovisor"
GENESIS_BIN_DIR="${COSMOVISOR_HOME}/genesis/bin"

# ── Release asset URL ────────────────────────────────────────────────────────
# akash releases: akash_linux_amd64.zip  (inside: akash binary)
RELEASE_URL="https://github.com/akash-network/node/releases/download/${VERSION}/akash_linux_${ARCH}.zip"
CHECKSUM_URL="https://github.com/akash-network/node/releases/download/${VERSION}/akash_linux_${ARCH}.zip.sha256"

info "=== Akash Node Install ==="
info "  chain:   ${CHAIN_ID}"
info "  version: ${VERSION}"
info "  arch:    ${ARCH}"
info "  home:    ${NODE_HOME}"
info "  user:    ${NODE_USER}"

# ── 1. Check if already installed at correct version ────────────────────────
if binary_version_ok "${INSTALL_DIR}/${BINARY}" "$VERSION"; then
    ok "Binary already at ${VERSION} — skipping download"
else
    info "Installing ${BINARY} ${VERSION} ..."
    $DRY_RUN && { info "[dry-run] would download $RELEASE_URL"; } || {
        TMP=$(mktemp -d)
        trap 'rm -rf "$TMP"' EXIT

        safe_download "$RELEASE_URL" "$TMP/akash.zip"

        # Verify checksum if available
        if curl -fsSL --retry 2 -o "$TMP/akash.zip.sha256" "$CHECKSUM_URL" 2>/dev/null; then
            (cd "$TMP" && sha256sum -c akash.zip.sha256) || die "checksum mismatch — aborting"
            ok "Checksum verified"
        else
            warn "No checksum file available — skipping verification"
        fi

        require_cmd unzip
        unzip -qo "$TMP/akash.zip" -d "$TMP/"
        chmod +x "$TMP/${BINARY}"
        sudo -n install -o root -g root -m 0755 "$TMP/${BINARY}" "${INSTALL_DIR}/${BINARY}"
        ok "${BINARY} installed → ${INSTALL_DIR}/${BINARY}"
    }
fi

# ── 2. Install cosmovisor ────────────────────────────────────────────────────
COSMOVISOR_BIN="${INSTALL_DIR}/cosmovisor"
CV_VERSION="v1.7.0"
CV_URL="https://github.com/cosmos/cosmos-sdk/releases/download/cosmovisor%2F${CV_VERSION}/cosmovisor-${CV_VERSION}-linux-${ARCH}.tar.gz"

if command -v cosmovisor &>/dev/null; then
    ok "cosmovisor already installed ($(cosmovisor version 2>/dev/null || echo 'version unknown'))"
else
    info "Installing cosmovisor ${CV_VERSION} ..."
    $DRY_RUN && { info "[dry-run] would install cosmovisor $CV_VERSION"; } || {
        TMP2=$(mktemp -d)
        trap 'rm -rf "$TMP2"' EXIT
        safe_download "$CV_URL" "$TMP2/cosmovisor.tar.gz"
        tar -xzf "$TMP2/cosmovisor.tar.gz" -C "$TMP2/"
        sudo -n install -o root -g root -m 0755 "$TMP2/cosmovisor" "${COSMOVISOR_BIN}"
        ok "cosmovisor installed → ${COSMOVISOR_BIN}"
    }
fi

# ── 3. Create cosmovisor directory structure ──────────────────────────────────
info "Setting up cosmovisor directory structure ..."
$DRY_RUN && { info "[dry-run] would create $GENESIS_BIN_DIR"; } || {
    sudo -n mkdir -p "${GENESIS_BIN_DIR}"
    sudo -n mkdir -p "${COSMOVISOR_HOME}/upgrades"
    # Link current binary into cosmovisor genesis
    if [[ ! -f "${GENESIS_BIN_DIR}/${BINARY}" ]]; then
        sudo -n cp "${INSTALL_DIR}/${BINARY}" "${GENESIS_BIN_DIR}/${BINARY}"
        ok "Binary linked into cosmovisor genesis"
    else
        ok "cosmovisor genesis binary already present"
    fi
    sudo -n chown -R "${NODE_USER}:${NODE_USER}" "${NODE_HOME}" 2>/dev/null || true
}

# ── 4. Verify ────────────────────────────────────────────────────────────────
$DRY_RUN || {
    installed_ver=$("${INSTALL_DIR}/${BINARY}" version 2>/dev/null | head -1)
    ok "Installed: ${BINARY} ${installed_ver}"
    ok "cosmovisor: $(cosmovisor version 2>/dev/null || echo 'installed')"
}

info "=== Install complete. Next: run configure.sh ==="
