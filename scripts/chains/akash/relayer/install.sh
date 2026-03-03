#!/bin/bash
# Akash relayer install — Hermes IBC relayer binary
# Usage: bash install.sh [--dry-run]
# Hermes docs: https://hermes.informal.systems

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
CHAIN_DIR="$(cd "$SCRIPT_DIR/.." && pwd)"
source "$SCRIPT_DIR/../../../lib/common.sh"

CFG="$CHAIN_DIR/chain.toml"
load_chain_config "$CFG"

DRY_RUN=false
[[ "${1:-}" == "--dry-run" ]] && DRY_RUN=true && warn "DRY RUN — no changes will be made"

ARCH=$(detect_arch)
INSTALL_DIR="/usr/local/bin"
HERMES_VERSION="${HERMES_VERSION:-v1.13.3}"

# Hermes asset: hermes-v1.13.3-x86_64-unknown-linux-gnu.tar.gz
case "$ARCH" in
    amd64) HERMES_ARCH="x86_64-unknown-linux-gnu" ;;
    arm64) HERMES_ARCH="aarch64-unknown-linux-gnu" ;;
    *)     die "unsupported arch: $ARCH" ;;
esac

HERMES_URL="https://github.com/informalsystems/hermes/releases/download/${HERMES_VERSION}/hermes-${HERMES_VERSION}-${HERMES_ARCH}.tar.gz"

info "=== Hermes IBC Relayer Install ==="
info "  version: ${HERMES_VERSION}"
info "  arch:    ${HERMES_ARCH}"

# ── Check if already installed ────────────────────────────────────────────────
if command -v hermes &>/dev/null; then
    got=$(hermes version 2>/dev/null | grep -oE 'v[0-9]+\.[0-9]+\.[0-9]+' | head -1)
    if [[ "$got" == "$HERMES_VERSION" ]]; then
        ok "Hermes ${HERMES_VERSION} already installed"
        exit 0
    else
        warn "Hermes ${got} installed — upgrading to ${HERMES_VERSION}"
    fi
fi

$DRY_RUN && { info "[dry-run] would download $HERMES_URL"; exit 0; }

TMP=$(mktemp -d); trap 'rm -rf "$TMP"' EXIT
safe_download "$HERMES_URL" "$TMP/hermes.tar.gz"
tar -xzf "$TMP/hermes.tar.gz" -C "$TMP/"
chmod +x "$TMP/hermes"
sudo -n install -o root -g root -m 0755 "$TMP/hermes" "${INSTALL_DIR}/hermes"
ok "Hermes installed → ${INSTALL_DIR}/hermes"
hermes version 2>/dev/null

info "=== Hermes install complete. Next: run configure.sh ==="
