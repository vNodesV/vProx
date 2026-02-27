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
GEO_DIR := $(DATA_DIR)/geolocation
LOG_DIR := $(DATA_DIR)/logs
CFG_DIR := $(VPROX_HOME)/config
CHAINS_DIR := $(VPROX_HOME)/chains
INTERNAL_DIR := $(VPROX_HOME)/internal
ARCHIVE_DIR := $(LOG_DIR)/archives
SERVICE_DIR := $(VPROX_HOME)/service
SERVICE_PATH := $(SERVICE_DIR)/vProx.service
VLOG_SERVICE := $(SERVICE_DIR)/vLog.service
DIR_LIST := $(DATA_DIR) $(GEO_DIR) $(LOG_DIR) $(CFG_DIR) $(CFG_DIR)/chains $(CFG_DIR)/backup $(INTERNAL_DIR) $(ARCHIVE_DIR) $(SERVICE_DIR)

# GeoLocation database
GEO_DB_SRC := ip2l/ip2location.mmdb.gz
GEO_DB_DST := $(GEO_DIR)/ip2location.mmdb

ENV_FILE := $(VPROX_HOME)/.env

# Validate Go environment
GOPATH := $(shell go env GOPATH)
GOROOT := $(shell go env GOROOT)
GOPATH_BIN := $(GOPATH)/bin

.PHONY: all validate-go dirs geo config build install clean systemd env \
        build-vlog install-vlog config-vlog service-vlog

all: install
install: validate-go dirs geo config env

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
		gunzip -c "$(GEO_DB_SRC)" > "$(GEO_DB_DST)"; \
		echo "✓ Decompressed GEO DB to $(GEO_DB_DST)"; \
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

config: dirs
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

## Build binary

build:
	@echo "Building $(APP_NAME)..."
	mkdir -p "$(BUILD_DIR)"
	go build -o "$(BUILD_OUT)" "$(BUILD_SRC)"
	@echo "✓ Build complete"
	@echo "  Output: $(BUILD_OUT)"

## Install to GOPATH/bin and symlink to /usr/local/bin
##
## Note: this builds directly to GOPATH/bin and does not write a compiled binary into the repo directory.

install:
	@echo "Installing $(APP_NAME)..."
# 	mkdir -p "$(GOPATH_BIN)"
	go build -o "$(GOPATH_BIN)/$(APP_NAME)" "$(BUILD_SRC)"
	@echo ""
	@echo "The next step will create a symlink to /usr/local/bin/$(APP_NAME) which may require sudo permissions. If you do not have sudo access, you can add $(GOPATH_BIN) to your PATH instead."
	@read -p "Do you want to create the symlink (y/n) " -n 1 -r; echo ""; \
	if [[ $$REPLY =~ ^[Yy]$$ ]]; then \
		sudo ln -sf "$(GOPATH_BIN)/$(APP_NAME)" "/usr/local/bin/$(APP_NAME)"; \
		echo "✓ Symlink created at /usr/local/bin/$(APP_NAME)"; \
		$(MAKE) dirs; \
		$(MAKE) systemd; \
	else \
		echo "✓ Skipped symlink creation. You can run $(APP_NAME) using $(GOPATH_BIN)/$(APP_NAME) or add $(GOPATH_BIN) to your PATH."; \
		$(MAKE) dirs; \
	fi
	@echo ""
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
	go build -o "$(VLOG_BUILD)" "$(VLOG_SRC)"
	@echo "✓ Build complete"
	@echo "  Output: $(VLOG_BUILD)"

## Install vLog to GOPATH/bin + optional /usr/local/bin symlink

install-vlog: validate-go dirs config-vlog
	@echo "Installing $(VLOG_NAME)..."
	go build -o "$(GOPATH_BIN)/$(VLOG_NAME)" "$(VLOG_SRC)"
	@echo ""
	@echo "The next step will create a symlink at /usr/local/bin/$(VLOG_NAME)."
	@read -p "Create symlink? (y/n) " -n 1 -r; echo ""; \
	if [[ $$REPLY =~ ^[Yy]$$ ]]; then \
		sudo ln -sf "$(GOPATH_BIN)/$(VLOG_NAME)" "/usr/local/bin/$(VLOG_NAME)"; \
		echo "✓ Symlink created at /usr/local/bin/$(VLOG_NAME)"; \
		$(MAKE) service-vlog; \
	else \
		echo "✓ Skipped symlink. Run: $(GOPATH_BIN)/$(VLOG_NAME) start"; \
	fi

## Install vlog.sample.toml → ~/.vProx/config/vlog.toml (only if absent)

config-vlog: dirs
	@echo "Installing vLog config..."
	@if [[ -f "config/vlog.sample.toml" ]]; then \
		if [[ ! -f "$(CFG_DIR)/vlog.toml" ]]; then \
			cp "config/vlog.sample.toml" "$(CFG_DIR)/vlog.toml"; \
			echo "✓ Copied vlog.sample.toml to $(CFG_DIR)/vlog.toml"; \
			echo "  Edit $(CFG_DIR)/vlog.toml to set your API keys."; \
		else \
			echo "✓ $(CFG_DIR)/vlog.toml already exists"; \
		fi; \
	else \
		echo "WARNING: config/vlog.sample.toml not found in repo"; \
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
