SHELL := /bin/bash

APP_NAME := vProx
BUILD_SRC := main.go
BUILD_OUT := $(APP_NAME)

VPROX_HOME := $(HOME)/.vProx
DATA_DIR := $(VPROX_HOME)/data
LOG_DIR := $(VPROX_HOME)/logs
CFG_DIR := $(VPROX_HOME)/config
ARCHIVE_DIR := $(LOG_DIR)/archived

GEO ?= false
GEO_DB_SRC ?= ip2l/ip2location.mmdb
GEO_DB_DST := $(DATA_DIR)/ip2location.mmdb

ENV_FILE := $(VPROX_HOME)/.env

GOPATH_BIN := $(shell go env GOPATH)/bin

SYSTEMD_PATH := /etc/systemd/system/vprox.service

.PHONY: all dirs geo config build install systemd env

all: dirs geo config env build install systemd

## Create required folders under $HOME/.vProx

dirs:
	mkdir -p "$(DATA_DIR)" "$(LOG_DIR)" "$(CFG_DIR)" "$(ARCHIVE_DIR)"

## Optionally install GEO DB when GEO=true

geo:
	@if [[ "$(GEO)" == "true" || "$(GEO)" == "TRUE" ]]; then \
		if [[ ! -f "$(GEO_DB_SRC)" ]]; then \
			echo "GEO=true but GEO DB not found at $(GEO_DB_SRC)"; \
			exit 1; \
		fi; \
		cp "$(GEO_DB_SRC)" "$(GEO_DB_DST)"; \
		echo "Copied GEO DB to $(GEO_DB_DST)"; \
		echo "IP2LOCATION_MMDB=$(GEO_DB_DST)" > "$(ENV_FILE)"; \
		echo "Wrote $(ENV_FILE) with IP2LOCATION_MMDB"; \
	else \
		echo "GEO=false; skipping GEO DB install"; \
	fi

## Create .env if missing (non-geo defaults)

env:
	@if [[ ! -f "$(ENV_FILE)" ]]; then \
		echo "# Optional geo DB paths" > "$(ENV_FILE)"; \
		echo "IP2LOCATION_MMDB=" >> "$(ENV_FILE)"; \
		echo "GEOLITE2_COUNTRY_DB=" >> "$(ENV_FILE)"; \
		echo "GEOLITE2_ASN_DB=" >> "$(ENV_FILE)"; \
		echo "" >> "$(ENV_FILE)"; \
		echo "# Backup automation" >> "$(ENV_FILE)"; \
		echo "VPROX_BACKUP_ENABLED=false" >> "$(ENV_FILE)"; \
		echo "VPROX_BACKUP_INTERVAL_DAYS=0" >> "$(ENV_FILE)"; \
		echo "VPROX_BACKUP_MAX_BYTES=0" >> "$(ENV_FILE)"; \
		echo "VPROX_BACKUP_CHECK_MINUTES=10" >> "$(ENV_FILE)"; \
		echo "" >> "$(ENV_FILE)"; \
		echo "# Rate limiting" >> "$(ENV_FILE)"; \
		echo "VPROX_RPS=25" >> "$(ENV_FILE)"; \
		echo "VPROX_BURST=100" >> "$(ENV_FILE)"; \
		echo "VPROX_AUTO_ENABLED=true" >> "$(ENV_FILE)"; \
		echo "VPROX_AUTO_THRESHOLD=120" >> "$(ENV_FILE)"; \
		echo "VPROX_AUTO_WINDOW_SEC=10" >> "$(ENV_FILE)"; \
		echo "VPROX_AUTO_RPS=1" >> "$(ENV_FILE)"; \
		echo "VPROX_AUTO_BURST=1" >> "$(ENV_FILE)"; \
		echo "VPROX_AUTO_TTL_SEC=900" >> "$(ENV_FILE)"; \
		echo "" >> "$(ENV_FILE)"; \
		echo "# Server" >> "$(ENV_FILE)"; \
		echo "VPROX_ADDR=:3000" >> "$(ENV_FILE)"; \
		echo "Created $(ENV_FILE)"; \
	fi

## Create chain template config

config: dirs
	cat > "$(CFG_DIR)/chain.toml.template" <<'EOF'

chain_name     = "your_chain"
host           = "api-link.example"
ip             = "0.0.0.0"
default_ports  = false
msg            = true

[message]
    api_msg = "https://api_link"
    rpc_msg = "https://rpc_link"

[expose]
    path  = true        # /rpc, /rest, /websocket on base host
    vhost = true        # enable api.<host>, rpc.<host>

[expose.vhost_prefix]
    rpc  = "rpc"
    rest = "api"

[services]
    rpc        = true
    rest       = true
    websocket  = true
    grpc       = true
    grpc_web   = true
    api_alias  = true   # /api -> REST (1317)

[ports]              # used only when default_ports = false
    rpc      = 26657
    rest     = 1317
    grpc     = 9090
    grpc_web = 9091
    api      = 1317

[features]
    inject_rpc_index    = true      # inject on RPC index HTML
    inject_rest_swagger = false     # inject on /rest/swagger/
    absolute_links      = "auto"    # auto | always | never

[logging]
    file   = "logs/your_chain.log"  # per-chain log (fallback to main.log if empty)
    format = "summary"              # (future) summary | json | raw

EOF

## Build binary

build:
	go build -o "$(BUILD_OUT)" "$(BUILD_SRC)"

## Install to GOPATH/bin and symlink to /usr/local/bin

install: build
	mkdir -p "$(GOPATH_BIN)"
	cp "$(BUILD_OUT)" "$(GOPATH_BIN)/$(APP_NAME)"
    sudo ln -sf "$(GOPATH_BIN)/$(APP_NAME)" "/usr/local/bin/$(APP_NAME)"
    @mkdir -p "$(CFG_DIR)"
    @cp -n chains/*.toml "$(CFG_DIR)/" 2>/dev/null || true
    @cp -n chains/ports/ports.toml "$(CFG_DIR)/ports.toml" 2>/dev/null || true

## Create systemd service file

systemd:
	@sudo tee "$(SYSTEMD_PATH)" > /dev/null <<'EOF'
[Unit]
Description=Custom Go RPC Rewriter Proxy for Tendermint
After=network.target
Wants=network-online.target

[Service]
Environment=VPROX_HOME=/home/vnodesv/.vProx
EnvironmentFile=/home/vnodesv/.vProx/.env
Environment=IP2LOCATION_MMDB=/usr/local/share/IP2Proxy/ip2location.mmdb
Environment=IP2PROXY_DISABLE=1
ExecStart=/usr/local/bin/vProx
Restart=no
User=vnodesv
WorkingDirectory=/home/vnodesv/.vProx
Environment=GOTRACEBACK=all

[Install]
WantedBy=multi-user.target
EOF
