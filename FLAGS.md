# vProx Command-Line Flags

vProx supports a comprehensive set of command-line flags for configuration, validation, and debugging.

## Quick Reference

```bash
./vProx [flags]
```

Use `-h` or `--help` to see all available flags:

```bash
./vProx -h
```

---

## Configuration & Directory Overrides

### `-home string`
Override the vProx home directory (default: `$VPROX_HOME` env or `~/.vProx`)

**Example:**
```bash
./vProx -home /etc/vprox
```

### `-config string`
Override the config directory (default: `$VPROX_HOME/config`)

Can be absolute or relative to home directory.

**Examples:**
```bash
./vProx -config /etc/vprox/configs
./vProx -config cfg  # ~/vProx/cfg
```

### `-chains string`
Override the chains directory (default: `$VPROX_HOME/chains`)

Can be absolute or relative to home directory.

**Examples:**
```bash
./vProx -chains /var/vprox/chains
./vProx -chains custom-chains  # ~/vProx/custom-chains
```

### `-log-file string`
Override the main log file path (default: `$VPROX_HOME/data/logs/main.log`)

Can be absolute or relative to the logs directory.

**Examples:**
```bash
./vProx -log-file vprox-custom.log
./vProx -log-file /var/log/vprox.log
```

### `-addr string`
Set the listen address (default: `:3000`)

**Examples:**
```bash
./vProx -addr :8080
./vProx -addr 192.168.1.100:3000
./vProx -addr 0.0.0.0:3000
```

---

## Validation & Inspection

### `-validate`
Validate all configuration files and exit.

Loads all chain configs and reports:
- Number of loaded chains
- Chain names and hosts
- Default port configuration
- Any validation errors

**Example:**
```bash
./vProx -validate
# Output:
# CONFIG VALIDATION SUCCESSFUL #############################
# [VALIDATE] Loaded 6 chains from ...
# [VALIDATE] All configs OK
```

### `-info`
Display loaded configuration summary and exit.

Shows:
- Directory paths (home, config, chains, data, logs)
- Loaded chains with names and IPs
- Default port configuration

**Example:**
```bash
./vProx -info
```

### `-version`
Display version information and exit.

**Example:**
```bash
./vProx -version
# Output:
# vProx - Reverse proxy with rate limiting and geolocation
# Version: 1.0.0
```

---

## Runtime Modes

### `-dry-run`
Load all configurations but don't start the server.

Useful for testing configuration changes or validating settings before deployment.

**Example:**
```bash
./vProx -dry-run
# Output:
# DRY-RUN MODE #############################
# Would listen on: :3000
# Loaded chains: 6
# Rate limit: 25.00 RPS, burst 100
# Auto-quarantine: enabled (threshold=120, penalty=1.00 RPS)
# [DRY-RUN] All systems ready (not starting server)
```

### `-backup`
Run a single backup operation and exit.

Executes the log rotation and archival process once.

**Example:**
```bash
./vProx -backup
```

---

## Rate Limiting Overrides

CLI flags override environment variables (`VPROX_*`) and config defaults.

### `-rps float`
Override default requests per second limit (env: `VPROX_RPS`)

Default: 25 RPS

**Example:**
```bash
./vProx -rps 100
./vProx -rps 50.5
```

### `-burst int`
Override default burst capacity (env: `VPROX_BURST`)

Default: 100

**Example:**
```bash
./vProx -burst 500
```

### `-disable-auto`
Disable automatic rate-limit quarantine feature.

Prevents IPs from being auto-quarantined when exceeding thresholds.

**Example:**
```bash
./vProx -disable-auto
```

### `-auto-rps float`
Override auto-quarantine penalty RPS rate (env: `VPROX_AUTO_RPS`)

Default: 1.0 RPS

**Example:**
```bash
./vProx -auto-rps 0.5
```

### `-auto-burst int`
Override auto-quarantine penalty burst capacity (env: `VPROX_AUTO_BURST`)

Default: 1

**Example:**
```bash
./vProx -auto-burst 5
```

---

## Logging & Debugging

### `-verbose`
Enable verbose logging output.

Logs all CLI flag overrides and detailed configuration information.

**Example:**
```bash
./vProx -verbose -dry-run
# Outputs additional [CLI] override messages
```

### `-quiet`
Suppress non-error output.

Only warnings and errors are logged.

**Example:**
```bash
./vProx -quiet
```

---

## Backup Control

### `-disable-backup`
Disable automatic backup loop on startup.

Prevents background log rotation and archival.

**Example:**
```bash
./vProx -disable-backup
```

---

## Common Use Cases

### Validate configuration before deployment
```bash
./vProx -validate
./vProx -info -verbose
```

### Test with custom rate limits
```bash
./vProx -dry-run -rps 100 -burst 500 -verbose
```

### Debug without starting server
```bash
./vProx -dry-run -verbose
./vProx -info
```

### Custom directory structure
```bash
./vProx -home /etc/vprox -config /etc/vprox/conf -chains /var/vprox/chains
```

### Development/testing
```bash
./vProx -dry-run -disable-auto -disable-backup
```

### Strict rate limiting
```bash
./vProx -rps 10 -burst 20
```

### Production with monitoring
```bash
./vProx -addr 0.0.0.0:3000 -verbose
```

---

## Flag Precedence

Flags are applied in this order (highest to lowest priority):

1. **Command-line flags** (highest priority)
2. **Environment variables** (where applicable)
3. **Configuration files** (TOML)
4. **Hardcoded defaults** (lowest priority)

### Example:
```bash
# Set RPS in order of precedence:
VPROX_RPS=30 ./vProx -rps 100  # CLI flag wins: 100 RPS
VPROX_RPS=30 ./vProx           # Env var wins: 30 RPS
```

---

## Exit Codes

- **0**: Success
- **1**: Configuration validation error, fatal error, or flag-triggered exit (`-version`, `-validate`, `-info`, etc.)

---

## Examples

### Check if config is valid
```bash
./vProx -validate && echo "✓ Config OK" || echo "✗ Config ERROR"
```

### Display complete configuration
```bash
./vProx -info -verbose
```

### Test with temporary overrides
```bash
./vProx -dry-run -rps 50 -burst 200 -verbose
```

### Run backup manually
```bash
./vProx -backup
```

### Quiet operation with custom listen address
```bash
./vProx -quiet -addr :8080
```

### Combined flags
```bash
./vProx \
  -home /custom/vprox \
  -chains /custom/chains \
  -addr :4000 \
  -rps 75 \
  -burst 300 \
  -disable-auto \
  -verbose
```
