SHELL := /bin/bash

APP_NAME := vProx
BUILD_SRC := ./cmd/vprox
BUILD_DIR := .build
BUILD_OUT := $(BUILD_DIR)/$(APP_NAME)

VLOG_NAME  := vLog
VLOG_SRC   := ./cmd/vlog
VLOG_BUILD := $(BUILD_DIR)/$(VLOG_NAME)

VPROX_HOME := $(HOME)/.vProx
DATA_DIR := $(VPROX_HOME)/data
LOG_DIR := $(DATA_DIR)/logs
CFG_DIR := $(VPROX_HOME)/config
CHAINS_DIR := $(VPROX_HOME)/chains
INTERNAL_DIR := $(VPROX_HOME)/internal
ARCHIVE_DIR := $(LOG_DIR)/archives
SERVICE_DIR := $(VPROX_HOME)/service
SERVICE_PATH := $(SERVICE_DIR)/vProx.service
VLOG_SERVICE := $(SERVICE_DIR)/vLog.service
GEO_DIR := $(DATA_DIR)/geolocation
DIR_LIST := $(DATA_DIR) $(LOG_DIR) $(CFG_DIR) $(CFG_DIR)/chains $(CFG_DIR)/backup \
            $(CFG_DIR)/push $(CFG_DIR)/vlog $(INTERNAL_DIR) $(ARCHIVE_DIR) $(SERVICE_DIR) $(GEO_DIR)

# GeoLocation database — bundled in assets/geo/, extracted to user data dir
GEO_DB_SRC := assets/geo/ip2location.mmdb.gz
GEO_DB_DST := $(GEO_DIR)/ip2location.mmdb

ENV_FILE := $(VPROX_HOME)/.env

# Validate Go environment
GOPATH := $(shell go env GOPATH)
GOROOT := $(shell go env GOROOT)
GOPATH_BIN := $(GOPATH)/bin

# On servers where GOROOT points to a manually installed (potentially broken)
# Go tree, the module-cache toolchain has a clean stdlib. Prefer it when present.
# Falls back to the current GOROOT transparently (no persistent state is changed).
_TOOLCHAIN_GOROOT := $(shell find $(GOPATH)/pkg/mod/golang.org -maxdepth 1 -name 'toolchain@*' 2>/dev/null | sort -V | tail -1)
EFFECTIVE_GOROOT  := $(if $(_TOOLCHAIN_GOROOT),$(_TOOLCHAIN_GOROOT),$(GOROOT))

.PHONY: all install clean ufw help \
        validate-go dirs geo config config-push config-vlog config-modules \
        build build-vlog systemd service-vlog

all: help

## ─── Public targets ──────────────────────────────────────────────────────────

help:
	@echo ""
	@echo "  vProx / vLog — available targets"
	@echo ""
	@echo "  make install          Full install: vProx + vLog, config, systemd"
	@echo "  make add-<module>     Reinstall one module  (e.g. make add-vLog)"
	@echo "  make clean            Remove local build artifacts"
	@echo "  make ufw              Passwordless UFW sudoers for vLog block/unblock"
	@echo ""

install: validate-go dirs geo config config-vlog config-push config-modules env

## Validate Go environment

validate-go:
	@echo "Validating Go environment..."
	@if [[ -z "$(GOROOT)" ]]; then \
		echo "ERROR: GOROOT is not set. Please ensure Go is properly installed."; \
		exit 1; \
	fi
	@if [[ -z "$(GOPATH)" ]]; then \
		echo "ERROR: GOPATH is not set. Please ensure Go is properly configured."; \
		exit 1; \
	fi
	@echo "✓ GOROOT: $(GOROOT)"
	@if [[ "$(EFFECTIVE_GOROOT)" != "$(GOROOT)" ]]; then \
		echo "  ↳ using clean toolchain: $(EFFECTIVE_GOROOT)"; \
	fi
	@echo "✓ GOPATH: $(GOPATH)"
	@echo "✓ Go version: $$(go version)"

## Create required folders under $HOME/.vProx

dirs:
	@echo "Inspecting directory structure..."
	@for dir in $(DIR_LIST); do \
		if [[ ! -d "$$dir" ]]; then \
			mkdir -p "$$dir"; \
			echo "✓ $$dir created..."; \
		else \
			echo "✓ $$dir already exists"; \
		fi; \
	done
	

## Install GEO DB — decompress from bundled .gz

geo:
	@echo "Installing GeoLocation database..."
	@if [[ ! -f "$(GEO_DB_SRC)" ]]; then \
		echo "WARNING: GEO DB not found at $(GEO_DB_SRC)"; \
		echo "Geolocation features will be disabled until a database is provided."; \
	else \
		mkdir -p "$(GEO_DIR)"; \
		gunzip -c "$(GEO_DB_SRC)" > "$(GEO_DB_DST)"; \
		echo "✓ Installed GEO DB to $(GEO_DB_DST)"; \
	fi

## Create .env if missing

env:
	@echo "Setting up environment configuration..."
	@if [[ ! -f "$(ENV_FILE)" ]]; then \
		echo "# Geolocation database paths" > "$(ENV_FILE)"; \
		echo "IP2LOCATION_MMDB=$(GEO_DB_DST)" >> "$(ENV_FILE)"; \
		echo "GEOLITE2_COUNTRY_DB=" >> "$(ENV_FILE)"; \
		echo "GEOLITE2_ASN_DB=" >> "$(ENV_FILE)"; \
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
		echo "✓ Created $(ENV_FILE)"; \
	else \
		echo "✓ $(ENV_FILE) already exists"; \
	fi

## Copy chain sample config to user's chains directory

config: dirs config-push config-modules
	@echo "Installing sample chain configuration..."
	@if [[ -f "config/chains/chain.sample.toml" ]]; then \
		cp "config/chains/chain.sample.toml" "$(CFG_DIR)/chains/chain.sample.toml"; \
		echo "✓ Copied chain.sample.toml to $(CFG_DIR)/chains/"; \
	else \
		echo "WARNING: config/chains/chain.sample.toml not found in repo"; \
	fi
	@if [[ ! -f "$(CFG_DIR)/ports.toml" ]]; then \
		echo "Creating default ports.toml..."; \
		{ \
			echo "# Default ports for all chains (override per-chain with default_ports = false)"; \
			echo "rpc      = 26657"; \
			echo "rest     = 1317"; \
			echo "grpc     = 9090"; \
			echo "grpc_web = 9091"; \
			echo "api      = 1317"; \
		} > "$(CFG_DIR)/ports.toml"; \
		echo "✓ Created $(CFG_DIR)/ports.toml"; \
	else \
		echo "✓ $(CFG_DIR)/ports.toml already exists"; \
	fi
	@if [[ ! -f "$(CFG_DIR)/backup/backup.toml" ]]; then \
		if [[ -f "config/backup.sample.toml" ]]; then \
			cp "config/backup.sample.toml" "$(CFG_DIR)/backup/backup.toml"; \
			echo "✓ Copied backup.sample.toml to $(CFG_DIR)/backup/backup.toml"; \
		else \
			echo "NOTE: config/backup.sample.toml not found; skipping backup.toml install"; \
		fi \
	else \
		echo "✓ $(CFG_DIR)/backup/backup.toml already exists"; \
	fi

## Install push VM registry stub (activates push module in vLog)

config-push:
	@mkdir -p "$(CFG_DIR)/push"
	@if [[ ! -f "$(CFG_DIR)/push/vms.toml" ]]; then \
		if [[ -f "config/push/vms.sample.toml" ]]; then \
			cp "config/push/vms.sample.toml" "$(CFG_DIR)/push/vms.toml"; \
			echo "✓ Installed push config → $(CFG_DIR)/push/vms.toml"; \
		else \
			printf '# vms.toml — push VM registry\n# Add VMs with: vprox push add\n# Push module activates automatically when this file exists.\n' \
				> "$(CFG_DIR)/push/vms.toml"; \
			echo "✓ Created empty push config → $(CFG_DIR)/push/vms.toml"; \
		fi \
	else \
		echo "✓ $(CFG_DIR)/push/vms.toml already exists"; \
	fi

## Install modules registry stub

config-modules:
	@if [[ ! -f "$(CFG_DIR)/modules.toml" ]]; then \
		printf '# modules.toml — managed module registry\n# Use: vprox mod add <chain> <component>\n' \
			> "$(CFG_DIR)/modules.toml"; \
		echo "✓ Created modules registry → $(CFG_DIR)/modules.toml"; \
	else \
		echo "✓ $(CFG_DIR)/modules.toml already exists"; \
	fi

## Build binary

build:
	@echo "Building $(APP_NAME)..."
	mkdir -p "$(BUILD_DIR)"
	GOROOT="$(EFFECTIVE_GOROOT)" go build -o "$(BUILD_OUT)" "$(BUILD_SRC)"
	@echo "✓ Build complete"
	@echo "  Output: $(BUILD_OUT)"

## Install vProx + vLog to GOPATH/bin and optional /usr/local/bin symlinks

install:
	@echo "Building $(APP_NAME) + $(VLOG_NAME)..."
	GOROOT="$(EFFECTIVE_GOROOT)" go build -o "$(GOPATH_BIN)/$(APP_NAME)" "$(BUILD_SRC)"
	GOROOT="$(EFFECTIVE_GOROOT)" go build -o "$(GOPATH_BIN)/$(VLOG_NAME)" "$(VLOG_SRC)"
	@echo "✓ $(APP_NAME) → $(GOPATH_BIN)/$(APP_NAME)"
	@echo "✓ $(VLOG_NAME) → $(GOPATH_BIN)/$(VLOG_NAME)"
	@echo ""
	@echo "The next step creates symlinks at /usr/local/bin/{$(APP_NAME),$(VLOG_NAME)} and may require sudo."
	@read -p "Create symlinks? (y/n) " -n 1 -r; echo ""; \
	if [[ $$REPLY =~ ^[Yy]$$ ]]; then \
		sudo ln -sf "$(GOPATH_BIN)/$(APP_NAME)" "/usr/local/bin/$(APP_NAME)"; \
		sudo ln -sf "$(GOPATH_BIN)/$(VLOG_NAME)" "/usr/local/bin/$(VLOG_NAME)"; \
		echo "✓ Symlinks created at /usr/local/bin/{$(APP_NAME),$(VLOG_NAME)}"; \
		$(MAKE) systemd; \
		$(MAKE) service-vlog; \
	else \
		echo "✓ Skipped symlinks. Run binaries from $(GOPATH_BIN)/"; \
	fi
	@echo ""

## Reinstall a single module — make add-vLog | make add-vProx

add-%: validate-go dirs
	@case "$*" in \
	  vLog|vlog) \
	    GOROOT="$(EFFECTIVE_GOROOT)" go build -o "$(GOPATH_BIN)/$(VLOG_NAME)" "$(VLOG_SRC)"; \
	    echo "✓ $(VLOG_NAME) → $(GOPATH_BIN)/$(VLOG_NAME)"; \
	    $(MAKE) config-vlog; \
	    $(MAKE) service-vlog; \
	    ;; \
	  vProx|vprox) \
	    GOROOT="$(EFFECTIVE_GOROOT)" go build -o "$(GOPATH_BIN)/$(APP_NAME)" "$(BUILD_SRC)"; \
	    echo "✓ $(APP_NAME) → $(GOPATH_BIN)/$(APP_NAME)"; \
	    $(MAKE) systemd; \
	    ;; \
	  *) \
	    echo "ERROR: Unknown module '$*'"; \
	    echo "       Available: vProx  vLog"; \
	    exit 1; \
	    ;; \
	esac

## Clean local build artifacts (never touches installed binary)

clean:
	@echo "Cleaning build artifacts..."
	rm -rf "$(BUILD_DIR)" "./$(APP_NAME)"
	@echo "✓ Clean"

## Create or update systemd service file

systemd:
	@echo "Rendering local systemd service file..."
	@mkdir -p "$(SERVICE_DIR)"
	@TMP_RENDERED="$$(mktemp)"; \
	sed "s|__HOME__|$(HOME)|g; s|__USER__|$(USER)|g" vprox.service.template > "$$TMP_RENDERED"; \
	if [[ -f "$(SERVICE_PATH)" ]]; then \
		if cmp -s "$$TMP_RENDERED" "$(SERVICE_PATH)"; then \
			echo "✓ Local vProx.service already up to date"; \
		else \
			echo "⚠ Local vProx.service differs from template; applying update..."; \
			cp "$$TMP_RENDERED" "$(SERVICE_PATH)"; \
			echo "✓ Updated $(SERVICE_PATH)"; \
			echo "⚠ This version generated a new service file. Review it and replace the current system service if needed."; \
		fi; \
	else \
		echo "Creating new local systemd service file..."; \
		cp "$$TMP_RENDERED" "$(SERVICE_PATH)"; \
		echo "✓ Created $(SERVICE_PATH)"; \
	fi; \
	rm -f "$$TMP_RENDERED"
	@echo ""
	@echo "The next step installs the generated service in /etc/systemd/system and requires sudo."
	@read -p "Do you want to install the systemd service now? (y/n) " -n 1 -r; echo ""; \
	if [[ $$REPLY =~ ^[Yy]$$ ]]; then \
		if sudo cp "$(SERVICE_PATH)" "/etc/systemd/system/vProx.service" && \
		   sudo systemctl daemon-reload && \
		   sudo systemctl enable vProx.service; \
		then \
			echo "✓ vProx.service installed."; \
			echo "  Start with: vProx start -d  (or: sudo service vProx start)"; \
		else \
			echo "✗ Failed to install vProx.service. Check 'sudo systemctl status vProx.service'."; \
		fi; \
	else \
		echo "✓ Skipped systemd service installation. You can install manually with:"; \
		echo "  sudo cp $(SERVICE_PATH) /etc/systemd/system/vProx.service"; \
		echo "  sudo systemctl daemon-reload"; \
		echo "  sudo systemctl enable vProx.service"; \
	fi;
	@echo ""
	@SUDOERS_FILE="/etc/sudoers.d/vprox"; \
	SUDOERS_LINE="$(USER) ALL=(ALL) NOPASSWD: /usr/sbin/service vProx start, /usr/sbin/service vProx stop, /usr/sbin/service vProx restart"; \
	if [[ -f "$$SUDOERS_FILE" ]]; then \
		if grep -qF "$$SUDOERS_LINE" "$$SUDOERS_FILE"; then \
			echo "✓ Sudoers rule already configured ($$SUDOERS_FILE)"; \
		else \
			echo "⚠ $$SUDOERS_FILE exists but differs. Current content:"; \
			sudo cat "$$SUDOERS_FILE"; \
			echo ""; \
			read -p "Overwrite with updated rule? (y/n) " -n 1 -r; echo ""; \
			if [[ $$REPLY =~ ^[Yy]$$ ]]; then \
				echo "$$SUDOERS_LINE" | sudo tee "$$SUDOERS_FILE" > /dev/null; \
				sudo chmod 0440 "$$SUDOERS_FILE"; \
				echo "✓ Updated $$SUDOERS_FILE"; \
			else \
				echo "✓ Skipped sudoers update"; \
			fi; \
		fi; \
	else \
		echo "Setting up passwordless service management for $(USER)..."; \
		echo "  This allows 'vProx start -d', 'vProx stop', and 'vProx restart' without a password prompt."; \
		read -p "Create sudoers rule? (y/n) " -n 1 -r; echo ""; \
		if [[ $$REPLY =~ ^[Yy]$$ ]]; then \
			echo "$$SUDOERS_LINE" | sudo tee "$$SUDOERS_FILE" > /dev/null; \
			sudo chmod 0440 "$$SUDOERS_FILE"; \
			echo "✓ Created $$SUDOERS_FILE"; \
		else \
			echo "✓ Skipped. You can create it manually:"; \
			echo "  echo '$$SUDOERS_LINE' | sudo tee $$SUDOERS_FILE"; \
			echo "  sudo chmod 0440 $$SUDOERS_FILE"; \
		fi; \
	fi

## ─── vLog targets ────────────────────────────────────────────────────────────

## Build vLog binary to .build/vLog  (does NOT rebuild vProx)

build-vlog:
	@echo "Building $(VLOG_NAME)..."
	mkdir -p "$(BUILD_DIR)"
	GOROOT="$(EFFECTIVE_GOROOT)" go build -o "$(VLOG_BUILD)" "$(VLOG_SRC)"
	@echo "✓ Build complete"
	@echo "  Output: $(VLOG_BUILD)"

## Install config/vlog/vlog.sample.toml → ~/.vProx/config/vlog/vlog.toml (only if absent)

config-vlog: dirs
	@echo "Installing vLog config..."
	@mkdir -p "$(CFG_DIR)/vlog"
	@if [[ -f "config/vlog/vlog.sample.toml" ]]; then \
		if [[ ! -f "$(CFG_DIR)/vlog/vlog.toml" ]]; then \
			cp "config/vlog/vlog.sample.toml" "$(CFG_DIR)/vlog/vlog.toml"; \
			echo "✓ Copied vlog.sample.toml to $(CFG_DIR)/vlog/vlog.toml"; \
			echo "  Edit $(CFG_DIR)/vlog/vlog.toml to set your API keys."; \
		else \
			echo "✓ $(CFG_DIR)/vlog/vlog.toml already exists — checking for missing fields..."; \
			if ! grep -qE "^[[:space:]]*api_key[[:space:]]*=" "$(CFG_DIR)/vlog/vlog.toml" || grep -qE "^[[:space:]]*#.*api_key" "$(CFG_DIR)/vlog/vlog.toml"; then \
				echo ""; \
				echo "┌─────────────────────────────────────────────────────────────────┐"; \
				echo "│  ⚠  ACTION REQUIRED — vLog API Key not configured               │"; \
				echo "├─────────────────────────────────────────────────────────────────┤"; \
				echo "│  vLog uses HMAC-SHA256 to authenticate block/unblock requests.  │"; \
				echo "│  These endpoints manipulate UFW firewall rules and MUST be      │"; \
				echo "│  protected with a secret key before use.                        │"; \
				echo "│                                                                 │"; \
				echo "│  1. Generate your key:                                          │"; \
				echo "│       openssl rand -hex 32                                      │"; \
				echo "│                                                                 │"; \
				echo "│  2. Add it to your config:                                      │"; \
				echo "│       $(CFG_DIR)/vlog/vlog.toml"; \
				echo "│     under [vlog]:                                               │"; \
				echo "│       api_key = \"your-generated-key\"                            │"; \
				echo "│                                                                 │"; \
				echo "│  Until this is set, block/unblock endpoints return 503.         │"; \
				echo "└─────────────────────────────────────────────────────────────────┘"; \
				echo ""; \
			fi; \
			if ! grep -qE "^[[:space:]]*base_path[[:space:]]*=" "$(CFG_DIR)/vlog/vlog.toml"; then \
				echo "  ℹ  base_path not set — if vLog is served at a sub-path (e.g. /vlog)"; \
				echo "     add to $(CFG_DIR)/vlog/vlog.toml under [vlog]:"; \
				echo "       base_path = \"/vlog\""; \
				echo "     See .vscode/vlog.apache2 for the matching Apache config."; \
				echo ""; \
			fi; \
		fi; \
	else \
		echo "WARNING: config/vlog/vlog.sample.toml not found in repo"; \
	fi

## Create and optionally install vLog systemd service

service-vlog:
	@echo "Rendering vLog systemd service file..."
	@mkdir -p "$(SERVICE_DIR)"
	@TMP_RENDERED="$$(mktemp)"; \
	sed "s|__HOME__|$(HOME)|g; s|__USER__|$(USER)|g" vlog.service.template > "$$TMP_RENDERED"; \
	if [[ -f "$(VLOG_SERVICE)" ]]; then \
		if cmp -s "$$TMP_RENDERED" "$(VLOG_SERVICE)"; then \
			echo "✓ Local vLog.service already up to date"; \
		else \
			echo "⚠ vLog.service differs; applying update..."; \
			cp "$$TMP_RENDERED" "$(VLOG_SERVICE)"; \
			echo "✓ Updated $(VLOG_SERVICE)"; \
		fi; \
	else \
		cp "$$TMP_RENDERED" "$(VLOG_SERVICE)"; \
		echo "✓ Created $(VLOG_SERVICE)"; \
	fi; \
	rm -f "$$TMP_RENDERED"
	@echo ""
	@read -p "Install vLog.service to /etc/systemd/system? (y/n) " -n 1 -r; echo ""; \
	if [[ $$REPLY =~ ^[Yy]$$ ]]; then \
		if sudo cp "$(VLOG_SERVICE)" "/etc/systemd/system/vLog.service" && \
		   sudo systemctl daemon-reload && \
		   sudo systemctl enable vLog.service; \
		then \
			echo "✓ vLog.service installed. Start with: sudo service vLog start"; \
		else \
			echo "✗ Failed. Check: sudo systemctl status vLog.service"; \
		fi; \
	else \
		echo "✓ Skipped. Install manually:"; \
		echo "  sudo cp $(VLOG_SERVICE) /etc/systemd/system/vLog.service"; \
		echo "  sudo systemctl daemon-reload && sudo systemctl enable vLog.service"; \
	fi

## ─── UFW passwordless setup for vLog ─────────────────────────────────────────

## Set up passwordless UFW block/unblock for vLog
ufw:
	@SUDOERS_FILE="/etc/sudoers.d/vlog"; \
	SUDOERS_LINE="$(USER) ALL=(ALL) NOPASSWD: /usr/sbin/ufw deny from *, /usr/sbin/ufw delete deny from *"; \
	if [[ -f "$$SUDOERS_FILE" ]]; then \
		if grep -qF "$$SUDOERS_LINE" "$$SUDOERS_FILE"; then \
			echo "✓ Sudoers rule already configured ($$SUDOERS_FILE)"; \
		else \
			echo "⚠ $$SUDOERS_FILE exists but differs. Current content:"; \
			sudo cat "$$SUDOERS_FILE"; \
			echo ""; \
			read -p "Overwrite with updated rule? (y/n) " -n 1 -r; echo ""; \
			if [[ $$REPLY =~ ^[Yy]$$ ]]; then \
				echo "$$SUDOERS_LINE" | sudo tee "$$SUDOERS_FILE" > /dev/null; \
				sudo chmod 0440 "$$SUDOERS_FILE"; \
				echo "✓ Updated $$SUDOERS_FILE"; \
			else \
				echo "✓ Skipped sudoers update"; \
			fi; \
		fi; \
	else \
		echo "Setting up passwordless UFW block/unblock for vLog..."; \
		echo "  Allows 'Block IP' and 'Unblock' buttons in vLog UI without password prompt."; \
		read -p "Create sudoers rule? (y/n) " -n 1 -r; echo ""; \
		if [[ $$REPLY =~ ^[Yy]$$ ]]; then \
			echo "$$SUDOERS_LINE" | sudo tee "$$SUDOERS_FILE" > /dev/null; \
			sudo chmod 0440 "$$SUDOERS_FILE"; \
			echo "✓ Created $$SUDOERS_FILE"; \
		else \
			echo "✓ Skipped. You can create it manually:"; \
			echo "  echo '$$SUDOERS_LINE' | sudo tee $$SUDOERS_FILE"; \
			echo "  sudo chmod 0440 $$SUDOERS_FILE"; \
		fi; \
	fi
