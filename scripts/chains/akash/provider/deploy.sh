#!/bin/bash
# Akash provider deploy — helm install provider + operators onto k3s
# Usage: bash deploy.sh [--dry-run] [--upgrade]
# Prerequisites: provider/configure.sh completed, k3s running

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
CHAIN_DIR="$(cd "$SCRIPT_DIR/.." && pwd)"
source "$SCRIPT_DIR/../../../lib/common.sh"

CFG="$CHAIN_DIR/chain.toml"
load_chain_config "$CFG"

DRY_RUN=false
UPGRADE=false
for arg in "$@"; do
    [[ "$arg" == "--dry-run" ]] && DRY_RUN=true && warn "DRY RUN — no changes will be made"
    [[ "$arg" == "--upgrade" ]] && UPGRADE=true
done

require_cmd kubectl helm provider-services

export KUBECONFIG="${KUBECONFIG:-/etc/rancher/k3s/k3s.yaml}"

PROVIDER_HOME="${PROVIDER_HOME:-${HOME}/.akash-provider}"
PROVIDER_DOMAIN="${PROVIDER_DOMAIN:-}"
PROVIDER_KEY_NAME="${PROVIDER_KEY_NAME:-provider}"
PROVIDER_KEYRING="${PROVIDER_KEYRING:-os}"

[[ -z "$PROVIDER_DOMAIN" ]] && die "PROVIDER_DOMAIN is required"

PROVIDER_ADDRESS=$(provider-services keys show "$PROVIDER_KEY_NAME" \
    --keyring-backend "$PROVIDER_KEYRING" \
    --home "$PROVIDER_HOME" -a 2>/dev/null) \
    || die "Provider key not found — run configure.sh first"

PRICING_SCRIPT="${PROVIDER_HOME}/pricing_script.sh"
[[ -f "$PRICING_SCRIPT" ]] || die "pricing_script.sh not found — run configure.sh first"

info "=== Akash Provider Deploy ==="
info "  address: ${PROVIDER_ADDRESS}"
info "  domain:  ${PROVIDER_DOMAIN}"
info "  cluster: $(kubectl get nodes --no-headers 2>/dev/null | wc -l) node(s)"

# ── Helm action helper ────────────────────────────────────────────────────────
helm_apply() {
    local action="$1" release="$2" chart="$3"
    shift 3
    if $DRY_RUN; then
        info "[dry-run] helm ${action} ${release} ${chart} $*"
    else
        helm "$action" "$release" "$chart" "$@"
    fi
}

HELM_ACTION="install"
$UPGRADE && HELM_ACTION="upgrade"

# ── 1. akash-node (RPC endpoint for provider) ─────────────────────────────────
# Provider needs a local RPC — we point it at the existing node on localhost.
# No separate node deployment needed.

# ── 2. NGINX ingress controller ───────────────────────────────────────────────
info "Deploying NGINX ingress controller ..."
if kubectl get ns ingress-nginx &>/dev/null; then
    ok "ingress-nginx namespace already exists"
else
    $DRY_RUN || kubectl create namespace ingress-nginx
fi

helm_apply "$HELM_ACTION" ingress-nginx ingress-nginx/ingress-nginx \
    --namespace ingress-nginx \
    --create-namespace \
    --set controller.service.type=NodePort \
    --set controller.service.nodePorts.http=30080 \
    --set controller.service.nodePorts.https=30443 \
    --wait --timeout 5m \
    2>/dev/null || $UPGRADE || warn "ingress-nginx already installed — use --upgrade to upgrade"

ok "NGINX ingress ready"

# ── 3. akash-provider ─────────────────────────────────────────────────────────
info "Deploying akash-provider ..."
helm_apply "$HELM_ACTION" akash-provider akash/akash-provider \
    --namespace akash-services \
    --create-namespace \
    --set "global.moniker=${MONIKER}" \
    --set "global.chainid=${CHAIN_ID}" \
    --set "global.node=http://$(hostname -I | awk '{print $1}'):${PORT_RPC}" \
    --set "provider.address=${PROVIDER_ADDRESS}" \
    --set "provider.domain=${PROVIDER_DOMAIN}" \
    --set "provider.pricingScriptPath=${PRICING_SCRIPT}" \
    --wait --timeout 5m \
    2>/dev/null || $UPGRADE || warn "akash-provider already installed — use --upgrade to upgrade"

ok "akash-provider deployed"

# ── 4. hostname-operator ──────────────────────────────────────────────────────
info "Deploying hostname-operator ..."
helm_apply "$HELM_ACTION" akash-hostname-operator akash/akash-hostname-operator \
    --namespace akash-services \
    --wait --timeout 3m \
    2>/dev/null || $UPGRADE || warn "hostname-operator already installed"

ok "hostname-operator deployed"

# ── 5. inventory-operator ─────────────────────────────────────────────────────
info "Deploying inventory-operator ..."
helm_apply "$HELM_ACTION" akash-inventory-operator akash/akash-inventory-operator \
    --namespace akash-services \
    --wait --timeout 3m \
    2>/dev/null || $UPGRADE || warn "inventory-operator already installed"

ok "inventory-operator deployed"

# ── 6. Status ─────────────────────────────────────────────────────────────────
$DRY_RUN || {
    echo ""
    info "Provider pod status:"
    kubectl get pods -n akash-services --no-headers 2>/dev/null
    echo ""
    ok "=== Provider deploy complete ==="
    info "  Monitor: kubectl logs -n akash-services -l app=akash-provider -f"
    info "  Leases:  provider-services query market lease list --provider ${PROVIDER_ADDRESS} --node tcp://localhost:${PORT_RPC}"
}
