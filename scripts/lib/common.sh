#!/bin/bash
# vProx scripts — shared library
# Source from chain scripts: source "$(dirname "$0")/../../../lib/common.sh"

set -euo pipefail

# ── colours ──────────────────────────────────────────────────────────────────
RED='\033[0;31m'; GREEN='\033[0;32m'; YELLOW='\033[1;33m'; CYAN='\033[0;36m'; NC='\033[0m'

info()    { echo -e "${CYAN}[INFO]${NC}  $*"; }
ok()      { echo -e "${GREEN}[ OK ]${NC}  $*"; }
warn()    { echo -e "${YELLOW}[WARN]${NC}  $*"; }
die()     { echo -e "${RED}[FAIL]${NC}  $*" >&2; exit 1; }

# ── toml parser (minimal — key = "value" only) ────────────────────────────────
toml_get() {
    local file="$1" key="$2"
    grep -E "^${key}\s*=" "$file" | head -1 | sed 's/.*=\s*"\(.*\)".*/\1/; s/.*=\s*\(.*\)/\1/' | tr -d '"' | xargs
}

toml_section_get() {
    local file="$1" section="$2" key="$3"
    awk "/^\[${section}\]/{found=1; next} /^\[/{found=0} found && /^${key}\s*=/{print; exit}" "$file" \
        | sed 's/.*=\s*"\(.*\)".*/\1/; s/.*=\s*\(.*\)/\1/' | tr -d '"' | xargs
}

# ── load chain config ─────────────────────────────────────────────────────────
# Usage: load_chain_config /path/to/chain.toml
# Sets: CHAIN_ID BINARY VERSION MONIKER GENESIS_URL SEEDS PEERS MIN_GAS
#       NODE_HOME NODE_USER PORT_RPC PORT_P2P PORT_GRPC PORT_API
load_chain_config() {
    local cfg="$1"
    [[ -f "$cfg" ]] || die "chain config not found: $cfg"

    CHAIN_ID=$(toml_get "$cfg" "chain_id")
    BINARY=$(toml_get "$cfg" "binary")
    VERSION="${CHAIN_VERSION:-$(toml_get "$cfg" "version")}"
    MONIKER="${CHAIN_MONIKER:-$(toml_get "$cfg" "moniker")}"

    GENESIS_URL=$(toml_section_get "$cfg" "network" "genesis_url")
    SEEDS=$(toml_section_get "$cfg" "network" "seeds")
    PEERS=$(toml_section_get "$cfg" "network" "peers")
    MIN_GAS=$(toml_section_get "$cfg" "network" "min_gas")

    NODE_HOME="${CHAIN_HOME:-$(toml_section_get "$cfg" "node" "home")}"
    NODE_USER="${CHAIN_USER:-$(toml_section_get "$cfg" "node" "user")}"

    PORT_RPC=$(toml_section_get "$cfg" "ports" "rpc")
    PORT_P2P=$(toml_section_get "$cfg" "ports" "p2p")
    PORT_GRPC=$(toml_section_get "$cfg" "ports" "grpc")
    PORT_API=$(toml_section_get "$cfg" "ports" "api")

    ok "Loaded config: chain=${CHAIN_ID} binary=${BINARY} version=${VERSION} home=${NODE_HOME}"
}

# ── require root-capable commands ─────────────────────────────────────────────
require_sudo() {
    sudo -n true 2>/dev/null || die "passwordless sudo required — run: make sudoers"
}

# ── require commands on PATH ──────────────────────────────────────────────────
require_cmd() {
    for cmd in "$@"; do
        command -v "$cmd" &>/dev/null || die "required command not found: $cmd"
    done
}

# ── arch detection ────────────────────────────────────────────────────────────
detect_arch() {
    local arch
    arch=$(uname -m)
    case "$arch" in
        x86_64)  echo "amd64" ;;
        aarch64) echo "arm64" ;;
        *)       die "unsupported architecture: $arch" ;;
    esac
}

# ── safe download ─────────────────────────────────────────────────────────────
safe_download() {
    local url="$1" dest="$2"
    info "Downloading $(basename "$dest") ..."
    curl -fsSL --retry 3 --retry-delay 5 -o "$dest" "$url" || die "download failed: $url"
}

# ── idempotency guard ─────────────────────────────────────────────────────────
binary_version_ok() {
    local bin="$1" want="$2"
    local got
    got=$("$bin" version 2>/dev/null | head -1 | grep -oE 'v[0-9]+\.[0-9]+\.[0-9]+' | head -1) || return 1
    [[ "$got" == "$want" ]]
}

# ── vProx chain dir helper ────────────────────────────────────────────────────
# Usage: CHAIN_DIR=$(chain_dir akash)
# Returns: $VPROX_HOME/scripts/chains/akash  (or ~/vProx/scripts/chains/akash)
chain_dir() {
    local chain="$1"
    local base="${VPROX_HOME:-${HOME}/vProx}"
    echo "${base}/scripts/chains/${chain}"
}
