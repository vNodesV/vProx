# vProx CLI Flags Guide

Authoritative guide for all vProx command-line flags.

## Invocation style

Use long flags with a double dash:

- `vProx --help`
- `vProx --validate`
- `vProx --dry-run --verbose`

> Compatibility note: Go's flag parser still accepts single-dash form (`-flag`), but project docs standardize on `--flag`.

---

## Configuration paths

### `--home string`
Override runtime home (`VPROX_HOME`), defaulting to `~/.vProx` if unset.

Use when:
- running isolated instances
- testing with disposable environments

Examples:
- `vProx --home /srv/vprox`
- `vProx --home /tmp/vprox-dev`

### `--config string`
Override config directory (contains `ports.toml`; may also include legacy chain TOMLs).

- relative paths resolve under `--home`
- absolute paths are used as-is

Examples:
- `vProx --config /etc/vprox/config`
- `vProx --config config-alt`

### `--chains string`
Override chains directory (primary location for chain `*.toml` files).

- relative paths resolve under `--home`
- absolute paths are used as-is

Examples:
- `vProx --chains /etc/vprox/chains`
- `vProx --chains chains-staging`

### `--log-file string`
Override main log file path.

- default: `<home>/data/logs/main.log`
- relative paths resolve under `<home>/data/logs`

Examples:
- `vProx --log-file main-dev.log`
- `vProx --log-file /var/log/vprox/main.log`

### `--addr string`
HTTP listen address.

- default: `:3000`
- env fallback: `VPROX_ADDR`
- CLI flag has priority over env

Examples:
- `vProx --addr :8080`
- `vProx --addr 0.0.0.0:3000`

---

## Startup / run modes

### `--help`
Show usage and available flags.

### `--version`
Print version and exit.

### `--validate`
Load and validate configuration, then exit.

Checks include:
- required `ports.toml`
- chain host/IP validity
- service/port compatibility
- routing-related config constraints

Use when:
- preflight checks in CI/CD
- verifying config before restart

### `--info`
Load configuration, print runtime summary, and exit.

Includes:
- resolved directories
- loaded chains
- default ports
- extra details when combined with `--verbose`

### `--dry-run`
Load everything but do not start server.

Prints effective runtime summary, including:
- listen address
- chain count
- effective limiter settings
- auto-quarantine status
- backup enabled/disabled status

Use when:
- validating startup behavior safely

### `--backup`
Run one backup cycle and exit (no proxy server start).

Also supported:
- `vProx backup` (mapped internally to `--backup`)

Access source counters are persisted across restarts and backups by default.

---

## Rate limiting overrides

These CLI values override env values for this run.

### `--rps float`
Override default requests-per-second.

- env: `VPROX_RPS`
- built-in default: `25`
- applied when value is `> 0`

Example:
- `vProx --rps 50`

### `--burst int`
Override default burst capacity.

- env: `VPROX_BURST`
- built-in default: `100`
- applied when value is `> 0`

Example:
- `vProx --burst 250`

### `--disable-auto`
Disable auto-quarantine behavior.

Equivalent intent to `VPROX_AUTO_ENABLED=false`, with CLI taking precedence.

### `--auto-rps float`
Override auto-quarantine penalty RPS.

- env: `VPROX_AUTO_RPS`
- built-in default: `1`
- applied when value is `> 0`

Example:
- `vProx --auto-rps 0.5`

### `--auto-burst int`
Override auto-quarantine penalty burst.

- env: `VPROX_AUTO_BURST`
- built-in default: `1`
- applied when value is `> 0`

Example:
- `vProx --auto-burst 2`

---

## Backup controls

### `--reset_count`
Reset persisted access counters before backup execution.

Scope:
- intended for backup mode (`vProx backup --reset_count` or `vProx --backup --reset_count`)

### `--reset-count`
Alias for `--reset_count`.

### `--disable-backup`
Disable automatic backup loop at startup.

Does not affect manual one-shot backups via `--backup`.

---

## Verbosity / diagnostics

### `--verbose`
Enable extra startup diagnostics and override logs.

Great with:
- `--info`
- `--dry-run`

### `--quiet`
Flag is present, but current implementation still logs to configured log file.

Treat as reserved/minimal-effect in the current build.

---

## Precedence rules

For overlapping settings:

1. CLI flags (`--...`)
2. Environment variables
3. Built-in defaults / config-derived values

Example:
- `VPROX_RPS=30 vProx --rps 100` → effective default RPS is `100`

---

## Practical command sets

### Pre-deploy check
- `vProx --validate`
- `vProx --dry-run --verbose`

### Inspect resolved runtime
- `vProx --info --verbose`

### Temporary hardening
- `vProx --rps 10 --burst 20 --auto-rps 0.5 --auto-burst 1`

### Backup-only run
- `vProx --backup`

### Backup + reset access counters
- `vProx backup --reset_count`
