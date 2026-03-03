#!/bin/bash
# Akash provider install — k3s + helm + provider-services binary
# Usage: bash install.sh [--dry-run]
# Prerequisites: node/install.sh completed, validator is active
# NOTE: Requires a dedicated machine / VM — do NOT run on your validator VM.
#       Provider needs K3s (Kubernetes) which has significant resource overhead.

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
CHAIN_DIR="$(cd "$SCRIPT_DIR/.." && pwd)"
source "$SCRIPT_DIR/../../../lib/common.sh"

CFG="$CHAIN_DIR/chain.toml"
load_chain_config "$CFG"

DRY_RUN=false
[[ "${1:-}" == "--dry-run" ]] && DRY_RUN=true && warn "DRY RUN — no changes will be made"

require_sudo

ARCH=$(detect_arch)
INSTALL_DIR="/usr/local/bin"

# ── Versions (override via env) ───────────────────────────────────────────────
PS_VERSION="${PROVIDER_SERVICES_VERSION:-v0.10.5}"
K3S_VERSION="${K3S_VERSION:-v1.35.1+k3s1}"
HELM_VERSION="${HELM_VERSION:-v3.17.1}"

# provider-services release asset: provider-services_linux_amd64.zip
PS_URL="https://github.com/akash-network/provider/releases/download/${PS_VERSION}/provider-services_linux_${ARCH}.zip"

info "=== Akash Provider Install ==="
info "  provider-services: ${PS_VERSION}"
info "  k3s:               ${K3S_VERSION}"
info "  helm:              ${HELM_VERSION}"

# ── 1. provider-services binary ───────────────────────────────────────────────
if command -v provider-services &>/dev/null; then
    got=$(provider-services version 2>/dev/null | grep -oE 'v[0-9]+\.[0-9]+\.[0-9]+' | head -1)
    if [[ "$got" == "$PS_VERSION" ]]; then
        ok "provider-services ${PS_VERSION} already installed"
    else
        warn "provider-services ${got} installed — upgrading to ${PS_VERSION}"
        INSTALL_PS=true
    fi
else
    INSTALL_PS=true
fi

if ${INSTALL_PS:-false}; then
    info "Installing provider-services ${PS_VERSION} ..."
    $DRY_RUN && { info "[dry-run] would download $PS_URL"; } || {
        TMP=$(mktemp -d); trap 'rm -rf "$TMP"' EXIT
        require_cmd unzip
        safe_download "$PS_URL" "$TMP/ps.zip"
        unzip -qo "$TMP/ps.zip" -d "$TMP/"
        chmod +x "$TMP/provider-services"
        sudo -n install -o root -g root -m 0755 "$TMP/provider-services" "${INSTALL_DIR}/provider-services"
        ok "provider-services installed → ${INSTALL_DIR}/provider-services"
    }
fi

# ── 2. k3s ────────────────────────────────────────────────────────────────────
if command -v k3s &>/dev/null; then
    ok "k3s already installed ($(k3s --version | head -1))"
else
    info "Installing k3s ${K3S_VERSION} ..."
    $DRY_RUN && { info "[dry-run] would install k3s ${K3S_VERSION}"; } || {
        # Official k3s install script — idempotent, handles systemd service
        curl -sfL https://get.k3s.io | INSTALL_K3S_VERSION="${K3S_VERSION}" \
            INSTALL_K3S_EXEC="server --disable traefik" \
            K3S_KUBECONFIG_MODE="644" sh -
        ok "k3s installed"
        # Wait for k3s to be ready
        info "Waiting for k3s to be ready ..."
        timeout 60 bash -c 'until sudo -n k3s kubectl get nodes &>/dev/null; do sleep 3; done'
        ok "k3s cluster ready"
    }
fi

# ── 3. helm ───────────────────────────────────────────────────────────────────
if command -v helm &>/dev/null; then
    ok "helm already installed ($(helm version --short 2>/dev/null))"
else
    info "Installing helm ${HELM_VERSION} ..."
    $DRY_RUN && { info "[dry-run] would install helm ${HELM_VERSION}"; } || {
        TMP2=$(mktemp -d); trap 'rm -rf "$TMP2"' EXIT
        HELM_URL="https://get.helm.sh/helm-${HELM_VERSION}-linux-${ARCH}.tar.gz"
        safe_download "$HELM_URL" "$TMP2/helm.tar.gz"
        tar -xzf "$TMP2/helm.tar.gz" -C "$TMP2/"
        sudo -n install -o root -g root -m 0755 "$TMP2/linux-${ARCH}/helm" "${INSTALL_DIR}/helm"
        ok "helm installed → ${INSTALL_DIR}/helm"
    }
fi

# ── 4. Add Akash helm repo ────────────────────────────────────────────────────
$DRY_RUN || {
    if ! helm repo list 2>/dev/null | grep -q akash; then
        info "Adding Akash helm repo ..."
        helm repo add akash https://akash-network.github.io/helm-charts
        helm repo update
        ok "Akash helm repo added"
    else
        ok "Akash helm repo already configured"
    fi
}

# ── 5. kubectl alias (uses k3s kubeconfig) ────────────────────────────────────
$DRY_RUN || {
    if ! command -v kubectl &>/dev/null; then
        sudo -n ln -sf /usr/local/bin/k3s /usr/local/bin/kubectl
        ok "kubectl → k3s symlink created"
    fi
    # Export KUBECONFIG for current user
    KUBECONFIG_PATH="/etc/rancher/k3s/k3s.yaml"
    grep -q "KUBECONFIG" ~/.bashrc 2>/dev/null || \
        echo "export KUBECONFIG=${KUBECONFIG_PATH}" >> ~/.bashrc
}

info "=== Provider install complete. Next: run configure.sh ==="
info "  Verify cluster: kubectl get nodes"
info "  Verify helm:    helm repo list"
