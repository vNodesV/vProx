SHELL := /bin/bash

APP_NAME := vProx
BUILD_SRC := ./cmd/vprox
BUILD_DIR := .build
BUILD_OUT := $(BUILD_DIR)/$(APP_NAME)

VOPS_NAME  := vOps
VOPS_SRC   := ./cmd/vops
VOPS_BUILD := $(BUILD_DIR)/$(VOPS_NAME)

VPROX_HOME := $(HOME)/.vProx
DATA_DIR := $(VPROX_HOME)/data
LOG_DIR := $(DATA_DIR)/logs
CFG_DIR := $(VPROX_HOME)/config
CHAINS_DIR := $(VPROX_HOME)/chains
INTERNAL_DIR := $(VPROX_HOME)/internal
ARCHIVE_DIR := $(LOG_DIR)/archives
SERVICE_DIR := $(VPROX_HOME)/service
SERVICE_PATH := $(SERVICE_DIR)/vProx.service
VOPS_SERVICE := $(SERVICE_DIR)/vOps.service
GEO_DIR := $(DATA_DIR)/geolocation
SAMPLES_DIR := $(VPROX_HOME)/.samples
DIR_LIST := $(DATA_DIR) $(LOG_DIR) $(CFG_DIR) $(CFG_DIR)/chains $(CFG_DIR)/backup \
            $(CFG_DIR)/vops $(CFG_DIR)/infra $(CFG_DIR)/vprox $(CFG_DIR)/fleet \
            $(CFG_DIR)/vprox/nodes $(CFG_DIR)/vops/chains \
            $(INTERNAL_DIR) $(ARCHIVE_DIR) $(SERVICE_DIR) $(GEO_DIR) \
            $(SAMPLES_DIR) $(SAMPLES_DIR)/chains $(SAMPLES_DIR)/backup \
            $(SAMPLES_DIR)/vops $(SAMPLES_DIR)/infra $(SAMPLES_DIR)/fleet \
            $(SAMPLES_DIR)/vprox $(SAMPLES_DIR)/vprox/nodes $(SAMPLES_DIR)/vops/chains

# Sample file revision — format: r{major}_{MMDDYY}_{seq}
# Increment {seq} for multiple revisions on the same day; reset to 0 on new date.
# Injected into the "# rev: {{SAMPLE_REV}}" placeholder in every .sample file at install/refresh time.
SAMPLE_REV := r5_031126_0

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
        validate-go dirs geo config config-vops config-vprox config-modules \
        build build-vops systemd service-vops samples-fleet

all: help

## ─── Public targets ──────────────────────────────────────────────────────────

help:
	@echo ""
	@echo "  vProx / vOps — available targets"
	@echo ""
	@echo "  make install          Full install: vProx + vOps + SSH control plane, config, systemd"
	@echo "  make add-<module>     Reinstall one module  (e.g. make add-vOps)"
	@echo "  make clean            Remove local build artifacts"
	@echo "  make ufw              Passwordless UFW sudoers for vOps block/unblock"
	@echo ""
	@echo "  SSH control plane (fleet module) is installed automatically."
	@echo "  Add VM hosts to: ~/.vProx/config/infra/{datacenter}.toml (e.g. qc.toml, rbx.toml)"
	@echo "  Add chains to:   ~/.vProx/config/chains/{chain}.toml with [management] section"
	@echo ""

install: validate-go dirs geo config config-vops config-vprox config-modules env samples-fleet

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

## Install live config defaults (services.toml → ports.toml fallback, backup.toml) — samples handled by samples-fleet

config: dirs config-modules
	@if [[ ! -f "$(CFG_DIR)/chains/services.toml" && ! -f "$(CFG_DIR)/chains/ports.toml" && ! -f "$(CFG_DIR)/ports.toml" ]]; then \
		echo "Creating default services.toml..."; \
		if [[ -f ".samples/chains/services.sample" ]]; then \
			sed "s/{{SAMPLE_REV}}/$(SAMPLE_REV)/" ".samples/chains/services.sample" > "$(CFG_DIR)/chains/services.toml"; \
			echo "✓ Installed services.toml → $(CFG_DIR)/chains/services.toml"; \
		elif [[ -f ".samples/chains/ports.sample" ]]; then \
			sed "s/{{SAMPLE_REV}}/$(SAMPLE_REV)/" ".samples/chains/ports.sample" > "$(CFG_DIR)/chains/ports.toml"; \
			echo "✓ Installed ports.toml → $(CFG_DIR)/chains/ports.toml (legacy fallback)"; \
		else \
			{ \
				echo "# Default ports for all chains (override per-chain with default_ports = false)"; \
				echo "rpc      = 26657"; \
				echo "rest     = 1317"; \
				echo "grpc     = 9090"; \
				echo "grpc_web = 9091"; \
				echo "api      = 1317"; \
			} > "$(CFG_DIR)/chains/ports.toml"; \
			echo "✓ Created $(CFG_DIR)/chains/ports.toml (minimal fallback)"; \
		fi \
	else \
		echo "✓ Port/service config already exists (services.toml or ports.toml)"; \
	fi
	@if [[ ! -f "$(CFG_DIR)/backup/backup.toml" ]]; then \
		if [[ -f ".samples/backup/backup.sample" ]]; then \
			sed "s/{{SAMPLE_REV}}/$(SAMPLE_REV)/" ".samples/backup/backup.sample" > "$(CFG_DIR)/backup/backup.toml"; \
			echo "✓ Installed backup.toml → $(CFG_DIR)/backup/backup.toml"; \
		else \
			echo "NOTE: .samples/backup/backup.sample not found; skipping backup.toml install"; \
		fi \
	else \
		echo "✓ $(CFG_DIR)/backup/backup.toml already exists"; \
	fi

## Install proxy settings reference (settings.toml) — only sample, never overwrites live

config-vprox: dirs
	@mkdir -p "$(CFG_DIR)/vprox"
	@if [[ -f ".samples/vprox/settings.sample" ]]; then \
		sed "s/{{SAMPLE_REV}}/$(SAMPLE_REV)/" ".samples/vprox/settings.sample" > "$(CFG_DIR)/vprox/settings.sample"; \
		echo "✓ Installed proxy settings sample → $(CFG_DIR)/vprox/settings.sample"; \
		if [[ ! -f "$(CFG_DIR)/vprox/settings.toml" ]]; then \
			echo "  Copy to activate: cp $(CFG_DIR)/vprox/settings.sample $(CFG_DIR)/vprox/settings.toml"; \
		else \
			echo "✓ $(CFG_DIR)/vprox/settings.toml already exists"; \
		fi \
	else \
		echo "NOTE: .samples/vprox/settings.sample not found in repo; skipping"; \
	fi

## Overwrite ALL sample files in SAMPLES_DIR (~/.vProx/.samples/) — safe to run anytime; never touches live config.
## When a sample already exists, it is archived to SAMPLES_DIR/archives/<old_rev>/<subfolder>/
## before the new version is written, so every prior revision is recoverable.
samples-fleet:
	@mkdir -p \
		"$(SAMPLES_DIR)/chains"       "$(SAMPLES_DIR)/backup" \
		"$(SAMPLES_DIR)/vops"         "$(SAMPLES_DIR)/infra" \
		"$(SAMPLES_DIR)/fleet"        "$(SAMPLES_DIR)/vprox" \
		"$(SAMPLES_DIR)/vprox/nodes"  "$(SAMPLES_DIR)/vops/chains"
	@_rev="$(SAMPLE_REV)"; \
	_archive() { \
		local dst="$$1" sub="$$2" old_rev adir; \
		if [[ -f "$$dst" ]]; then \
			old_rev="$$(grep -m1 '^# rev:' "$$dst" 2>/dev/null | sed 's/.*# rev: *//' | tr -d '[:space:]')"; \
			old_rev="$${old_rev:-unknown}"; \
			adir="$(SAMPLES_DIR)/archives/$$old_rev/$$sub"; \
			mkdir -p "$$adir"; \
			mv "$$dst" "$$adir/$$(basename "$$dst")"; \
			echo "  ↳ archived → $$adir/$$(basename "$$dst")  [$$old_rev]"; \
		fi; \
	}; \
	_copy() { sed "s/{{SAMPLE_REV}}/$$_rev/" "$$1" > "$$2" && echo "✓ $$2  [$$_rev]"; }; \
	_archive "$(SAMPLES_DIR)/vops/vops.sample"          "vops";         _copy ".samples/vops/vops.sample"              "$(SAMPLES_DIR)/vops/vops.sample"; \
	_archive "$(SAMPLES_DIR)/chains/chain.sample"       "chains";       _copy ".samples/chains/chain.sample"           "$(SAMPLES_DIR)/chains/chain.sample"; \
	_archive "$(SAMPLES_DIR)/chains/ports.sample"       "chains";       _copy ".samples/chains/ports.sample"           "$(SAMPLES_DIR)/chains/ports.sample"; \
	_archive "$(SAMPLES_DIR)/chains/services.sample"    "chains";       _copy ".samples/chains/services.sample"        "$(SAMPLES_DIR)/chains/services.sample"; \
	_archive "$(SAMPLES_DIR)/backup/backup.sample"      "backup";       _copy ".samples/backup/backup.sample"          "$(SAMPLES_DIR)/backup/backup.sample"; \
	_archive "$(SAMPLES_DIR)/infra/infra.sample"        "infra";        _copy ".samples/infra/infra.sample"            "$(SAMPLES_DIR)/infra/infra.sample"; \
	_archive "$(SAMPLES_DIR)/vprox/settings.sample"     "vprox";        _copy ".samples/vprox/settings.sample"         "$(SAMPLES_DIR)/vprox/settings.sample"; \
	_archive "$(SAMPLES_DIR)/fleet/settings.sample"     "fleet";        _copy ".samples/fleet/settings.sample"         "$(SAMPLES_DIR)/fleet/settings.sample"; \
	_archive "$(SAMPLES_DIR)/vprox/nodes/vprox-node.sample" "vprox/nodes"; _copy ".samples/vprox/nodes/vprox-node.sample" "$(SAMPLES_DIR)/vprox/nodes/vprox-node.sample"; \
	_archive "$(SAMPLES_DIR)/vops/chains/vops-chain.sample" "vops/chains"; _copy ".samples/vops/chains/vops-chain.sample" "$(SAMPLES_DIR)/vops/chains/vops-chain.sample"
	@echo "Done. Samples refreshed — $(SAMPLE_REV). See $(SAMPLES_DIR)/"

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

## Install vProx + vOps to GOPATH/bin and optional /usr/local/bin symlinks

install:
	@echo "Building $(APP_NAME) + $(VOPS_NAME)..."
	GOROOT="$(EFFECTIVE_GOROOT)" go build -o "$(GOPATH_BIN)/$(APP_NAME)" "$(BUILD_SRC)"
	GOROOT="$(EFFECTIVE_GOROOT)" go build -o "$(GOPATH_BIN)/$(VOPS_NAME)" "$(VOPS_SRC)"
	@echo "✓ $(APP_NAME) → $(GOPATH_BIN)/$(APP_NAME)"
	@echo "✓ $(VOPS_NAME) → $(GOPATH_BIN)/$(VOPS_NAME)"
	@echo ""
	@echo "The next step creates symlinks at /usr/local/bin/{$(APP_NAME),$(VOPS_NAME)} and may require sudo."
	@read -p "Create symlinks? (y/n) " -n 1 -r; echo ""; \
	if [[ $$REPLY =~ ^[Yy]$$ ]]; then \
		sudo ln -sf "$(GOPATH_BIN)/$(APP_NAME)" "/usr/local/bin/$(APP_NAME)"; \
		sudo ln -sf "$(GOPATH_BIN)/$(VOPS_NAME)" "/usr/local/bin/$(VOPS_NAME)"; \
		echo "✓ Symlinks created at /usr/local/bin/{$(APP_NAME),$(VOPS_NAME)}"; \
		$(MAKE) systemd; \
		$(MAKE) service-vops; \
	else \
		echo "✓ Skipped symlinks. Run binaries from $(GOPATH_BIN)/"; \
	fi
	@echo ""

## Reinstall a single module — make add-vOps | make add-vProx

add-%: validate-go dirs
	@case "$*" in \
	  vOps|vops|vLog|vlog) \
	    GOROOT="$(EFFECTIVE_GOROOT)" go build -o "$(GOPATH_BIN)/$(VOPS_NAME)" "$(VOPS_SRC)"; \
	    echo "✓ $(VOPS_NAME) → $(GOPATH_BIN)/$(VOPS_NAME)"; \
	    $(MAKE) config-vops; \
	    $(MAKE) samples-fleet; \
	    $(MAKE) service-vops; \
	    ;; \
	  vProx|vprox) \
	    GOROOT="$(EFFECTIVE_GOROOT)" go build -o "$(GOPATH_BIN)/$(APP_NAME)" "$(BUILD_SRC)"; \
	    echo "✓ $(APP_NAME) → $(GOPATH_BIN)/$(APP_NAME)"; \
	    $(MAKE) systemd; \
	    ;; \
	  *) \
	    echo "ERROR: Unknown module '$*'"; \
	    echo "       Available: vProx  vOps"; \
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

## ─── vOps targets ────────────────────────────────────────────────────────────

## Build vOps binary to .build/vOps  (does NOT rebuild vProx)

build-vops:
	@echo "Building $(VOPS_NAME)..."
	mkdir -p "$(BUILD_DIR)"
	GOROOT="$(EFFECTIVE_GOROOT)" go build -o "$(VOPS_BUILD)" "$(VOPS_SRC)"
	@echo "✓ Build complete"
	@echo "  Output: $(VOPS_BUILD)"

## Install .samples/vops/vops.sample → ~/.vProx/config/vops/vops.toml (only if absent)

config-vops: dirs
	@echo "Installing vOps config..."
	@mkdir -p "$(CFG_DIR)/vops"
	@if [[ -f ".samples/vops/vops.sample" ]]; then \
		if [[ ! -f "$(CFG_DIR)/vops/vops.toml" ]]; then \
			cp ".samples/vops/vops.sample" "$(CFG_DIR)/vops/vops.toml"; \
			echo "✓ Copied vops.sample to $(CFG_DIR)/vops/vops.toml"; \
			echo "  Edit $(CFG_DIR)/vops/vops.toml to set your API keys."; \
		else \
			echo "✓ $(CFG_DIR)/vops/vops.toml already exists — checking for missing fields..."; \
			if ! grep -qE "^[[:space:]]*api_key[[:space:]]*=" "$(CFG_DIR)/vops/vops.toml" || grep -qE "^[[:space:]]*#.*api_key" "$(CFG_DIR)/vops/vops.toml"; then \
				echo ""; \
				echo "┌─────────────────────────────────────────────────────────────────┐"; \
				echo "│  ⚠  ACTION REQUIRED — vOps API Key not configured               │"; \
				echo "├─────────────────────────────────────────────────────────────────┤"; \
				echo "│  vOps uses HMAC-SHA256 to authenticate block/unblock requests.  │"; \
				echo "│  These endpoints manipulate UFW firewall rules and MUST be      │"; \
				echo "│  protected with a secret key before use.                        │"; \
				echo "│                                                                 │"; \
				echo "│  1. Generate your key:                                          │"; \
				echo "│       openssl rand -hex 32                                      │"; \
				echo "│                                                                 │"; \
				echo "│  2. Add it to your config:                                      │"; \
				echo "│       $(CFG_DIR)/vops/vops.toml"; \
				echo "│     under [vops]:                                               │"; \
				echo "│       api_key = \"your-generated-key\"                            │"; \
				echo "│                                                                 │"; \
				echo "│  Until this is set, block/unblock endpoints return 503.         │"; \
				echo "└─────────────────────────────────────────────────────────────────┘"; \
				echo ""; \
			fi; \
			if ! grep -qE "^[[:space:]]*base_path[[:space:]]*=" "$(CFG_DIR)/vops/vops.toml"; then \
				echo "  ℹ  base_path not set — if vOps is served at a sub-path (e.g. /vops)"; \
				echo "     add to $(CFG_DIR)/vops/vops.toml under [vops]:"; \
				echo "       base_path = \"/vops\""; \
				echo "     See .vscode/vops.apache2 for the matching Apache config."; \
				echo ""; \
			fi; \
		fi; \
	else \
		echo "WARNING: .samples/vops/vops.sample not found in repo"; \
	fi

## Create and optionally install vOps systemd service

service-vops:
	@echo "Rendering vOps systemd service file..."
	@mkdir -p "$(SERVICE_DIR)"
	@TMP_RENDERED="$$(mktemp)"; \
	sed "s|__HOME__|$(HOME)|g; s|__USER__|$(USER)|g" vops.service.template > "$$TMP_RENDERED"; \
	if [[ -f "$(VOPS_SERVICE)" ]]; then \
		if cmp -s "$$TMP_RENDERED" "$(VOPS_SERVICE)"; then \
			echo "✓ Local vOps.service already up to date"; \
		else \
			echo "⚠ vOps.service differs; applying update..."; \
			cp "$$TMP_RENDERED" "$(VOPS_SERVICE)"; \
			echo "✓ Updated $(VOPS_SERVICE)"; \
		fi; \
	else \
		cp "$$TMP_RENDERED" "$(VOPS_SERVICE)"; \
		echo "✓ Created $(VOPS_SERVICE)"; \
	fi; \
	rm -f "$$TMP_RENDERED"
	@echo ""
	@read -p "Install vOps.service to /etc/systemd/system? (y/n) " -n 1 -r; echo ""; \
	if [[ $$REPLY =~ ^[Yy]$$ ]]; then \
		if sudo cp "$(VOPS_SERVICE)" "/etc/systemd/system/vOps.service" && \
		   sudo systemctl daemon-reload && \
		   sudo systemctl enable vOps.service; \
		then \
			echo "✓ vOps.service installed. Start with: sudo service vOps start"; \
		else \
			echo "✗ Failed. Check: sudo systemctl status vOps.service"; \
		fi; \
	else \
		echo "✓ Skipped. Install manually:"; \
		echo "  sudo cp $(VOPS_SERVICE) /etc/systemd/system/vOps.service"; \
		echo "  sudo systemctl daemon-reload && sudo systemctl enable vOps.service"; \
	fi

## ─── UFW passwordless setup for vOps ─────────────────────────────────────────

## Set up passwordless UFW block/unblock for vOps
ufw:
	@SUDOERS_FILE="/etc/sudoers.d/vops"; \
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
		echo "Setting up passwordless UFW block/unblock for vOps..."; \
		echo "  Allows 'Block IP' and 'Unblock' buttons in vOps UI without password prompt."; \
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

## ─── Compatibility aliases (vLog → vOps) ─────────────────────────────────────

## These targets preserve backward compatibility for scripts and muscle memory
## that reference the old vLog name. They simply delegate to the vOps targets.

.PHONY: build-vlog config-vlog service-vlog

build-vlog: build-vops
config-vlog: config-vops
service-vlog: service-vops
