# vProx CLI Flags Guide

Authoritative guide for all vProx commands and command-line flags.

## Commands

### `vProx start`
Start the proxy server in the foreground. Logs to stdout and `main.log`.

### `vProx start -d` / `vProx start --daemon`
Start via systemd service (`sudo service vProx start`). Requires sudoers rule (created by `make systemd`).

### `vProx stop`
Stop the running service (`sudo service vProx stop`).

### `vProx restart`
Restart the running service (`sudo service vProx restart`).

---

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

### `--new-backup`
Run one backup cycle and exit (no proxy server start).

Examples:
- `vProx --new-backup`
- `vProx --new-backup --reset_count`

### `--list-backup`
List existing backup archives and exit.

### `--backup-status`
Show backup scheduler status (automation state, next ETA, archive count) and exit.

### `--disable-backup`
Disable automatic backup loop at startup.

Does not affect manual one-shot backups via `--new-backup`.

### `--reset_count`
Reset persisted access counters before backup execution.

### `--reset-count`
Alias for `--reset_count`.

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

### Service management
- `vProx start` — foreground
- `vProx start -d` — daemon (systemd service)
- `vProx stop` — stop service
- `vProx restart` — restart service

### Pre-deploy check
- `vProx --validate`
- `vProx --dry-run --verbose`

### Inspect resolved runtime
- `vProx --info --verbose`

### Temporary hardening
- `vProx --rps 10 --burst 20 --auto-rps 0.5 --auto-burst 1`

### Backup operations
- `vProx --new-backup` — run one backup cycle
- `vProx --new-backup --reset_count` — backup + reset counters
- `vProx --list-backup` — list archives
- `vProx --backup-status` — show scheduler status

---

## vLog CLI Reference

vLog is the companion log-analyzer binary shipped with vProxVL v1.2.0.

### Commands

| Command | Description |
|---|---|
| `vlog start` | Start vLog web server (foreground, default `:8889`) |
| `vlog start -d` | Start as background service (`sudo service vLog start`) |
| `vlog stop` | Stop vLog service (`sudo service vLog stop`) |
| `vlog restart` | Restart vLog service (`sudo service vLog restart`) |
| `vlog ingest` | One-shot: scan archives, ingest new entries, exit |
| `vlog status` | Print database stats and exit |

### Flags (all commands)

| Flag | Default | Description |
|---|---|---|
| `--home PATH` | `$VPROX_HOME` or `~/.vProx` | Override vProx home directory |
| `--port PORT` | from `vlog.toml` | Override web server listen port |
| `--quiet` | false | Suppress non-essential output |
| `--version` | — | Print version and exit |
| `-h`, `--help` | — | Print usage |

### Flags (start only)

| Flag | Default | Description |
|---|---|---|
| `--no-watch` | false | Disable background FS watcher (no auto-ingest) |
| `--no-enrich` | false | Disable auto-enrichment on new IPs |
| `--watch-interval DURATION` | `30s` | FS watcher poll interval |

### Flags (one-shot / diagnostic)

| Flag | Description |
|---|---|
| `--list-archives` | List all ingested archive files |
| `--list-accounts` | Print all IP accounts as JSON |
| `--list-threats` | Print flagged IPs (score ≥ 50) |
| `--enrich IP` | Run threat intelligence on a single IP and print result |
| `--purge-cache IP\|all` | Evict cached intel for one IP or all IPs |
| `--validate` | Validate `vlog.toml` config and exit |
| `--info` | Print resolved config and exit |
| `--dry-run` | Validate + print without starting server |

### Priority order

1. CLI flags
2. `$VPROX_HOME/config/vlog.toml`
3. Built-in defaults

### Practical command sets

#### Service management
```bash
vlog start -d         # daemon
vlog stop             # stop
vlog restart          # restart
vlog status           # show stats
```

#### Manual ingest
```bash
vlog ingest           # process all pending archives
vlog ingest --home /custom/path
```

#### Intelligence
```bash
vlog --enrich 1.2.3.4            # run VT + AbuseIPDB + Shodan on IP
vlog --purge-cache 1.2.3.4       # clear cached score for IP
vlog --list-threats               # print IPs with score ≥ 50
```
