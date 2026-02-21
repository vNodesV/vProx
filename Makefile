SHELL := /bin/bash

APP_NAME := vProx
BUILD_SRC := ./cmd/vprox
BUILD_DIR := .build
BUILD_OUT := $(BUILD_DIR)/$(APP_NAME)

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
DIR_LIST := $(DATA_DIR) $(GEO_DIR) $(LOG_DIR) $(CFG_DIR) $(CHAINS_DIR) $(INTERNAL_DIR) $(ARCHIVE_DIR) $(SERVICE_DIR)

# GeoLocation database
GEO_DB_SRC := ip2l/ip2location.mmdb
GEO_DB_DST := $(GEO_DIR)/ip2location.mmdb

ENV_FILE := $(VPROX_HOME)/.env

# Validate Go environment
GOPATH := $(shell go env GOPATH)
GOROOT := $(shell go env GOROOT)
GOPATH_BIN := $(GOPATH)/bin

.PHONY: all validate-go dirs geo config build install clean systemd env

all: validate-go dirs geo config env install
install: validate-go dirs geo config env install

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
	

## Install GEO DB automatically (GeoLite2 is free to redistribute)

geo:
	@echo "Installing GeoLocation database..."
	@if [[ ! -f "$(GEO_DB_SRC)" ]]; then \
		echo "WARNING: GEO DB not found at $(GEO_DB_SRC)"; \
		echo "Geolocation features will be disabled until a database is provided."; \
	else \
		cp "$(GEO_DB_SRC)" "$(GEO_DB_DST)"; \
		echo "✓ Copied GEO DB to $(GEO_DB_DST)"; \
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
		echo "✓ Created $(ENV_FILE)"; \
	else \
		echo "✓ $(ENV_FILE) already exists"; \
	fi

## Copy chain sample config to user's chains directory

config: dirs
	@echo "Installing sample chain configuration..."
	@if [[ -f "chains/chain.sample.toml" ]]; then \
		cp "chains/chain.sample.toml" "$(CHAINS_DIR)/chain.sample.toml"; \
		echo "✓ Copied chain.sample.toml to $(CHAINS_DIR)/"; \
	else \
		echo "WARNING: chains/chain.sample.toml not found in repo"; \
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
	else \
		echo "✓ Skipped symlink creation. You can run $(APP_NAME) using $(GOPATH_BIN)/$(APP_NAME) or add $(GOPATH_BIN) to your PATH."; \
	fi
	@echo ""
	@$(MAKE) dirs
	@$(MAKE) systemd

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
	@echo "The next step allows you to easily install the generated service file on a systemd host and sudo permission is required.  If you choose not to do this now, you can manually copy $(SERVICE_PATH) to /etc/systemd/system/ and enable/start the service later."
	@read -p "Do you want to install the systemd service now? (y/n) " -n 1 -r; echo ""; \
	if [[ $$REPLY =~ ^[Yy]$$ ]]; then \
		if sudo cp "$(SERVICE_PATH)" "/etc/systemd/system/vProx.service" && \
		   sudo systemctl daemon-reload && \
		   sudo systemctl enable vProx.service && \
		   sudo systemctl start vProx.service; then \
			echo "✓ vProx.service installed and started"; \
		else \
			echo "✗ Failed to install or start vProx.service. Please check 'sudo systemctl status vProx.service' for details."; \
		fi; \
	else \
		echo "✓ Skipped systemd service installation. You can manually copy $(SERVICE_PATH) to /etc/systemd/system/ and enable/start the service later using the commands below."; \
		echo "  sudo cp $(SERVICE_PATH) /etc/systemd/system/vProx.service"; \
		echo "  sudo systemctl daemon-reload"; \
		echo "  sudo systemctl enable vProx.service"; \
		echo "  sudo systemctl start vProx.service"; \
	fi;\

