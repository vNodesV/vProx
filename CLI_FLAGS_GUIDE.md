# vProx CLI Flags Guide

This is the authoritative guide for all `vProx` command-line flags.

> Binary usage:
>
> `vProx [flags]`
>
> You can also run:
>
> `vProx -h`

---

## How vProx resolves configuration

At startup, vProx resolves values in this practical order:

1. **CLI flags** (highest priority)
2. **Environment variables** (for supported settings)
3. **Built-in defaults**
4. **TOML files** (for chain/port routing config; loaded from resolved directories)

Important: directory flags (`-home`, `-config`, `-chains`) affect where TOML files are loaded from.

---

## Flags by category

## Path and runtime location flags

### `-home string`
Override `VPROX_HOME` runtime home.

- Default resolution:
  - `$VPROX_HOME` if set
  - otherwise `~/.vProx`
- Side effects:
  - affects derived defaults for `config/`, `chains/`, `data/`, `logs/`, archive paths

Use this when:
- running multiple isolated vProx instances
- testing in a temporary workspace
- using a custom deployment layout

Examples:
- `vProx -home /srv/vprox-a`
- `vProx -home /tmp/vprox-dev`

---

### `-config string`
Override config directory (where `ports.toml` and backward-compatible chain TOMLs may be read).

- Default: `<home>/config`
- If relative: resolved under `<home>`
- If absolute: used as-is

Use this when:
- your `ports.toml` is not under default home
- you are migrating old layouts

Examples:
- `vProx -config /etc/vprox/config`
- `vProx -config config-alt`

---

### `-chains string`
Override primary chains directory (`*.toml` chain definitions).

- Default: `<home>/chains`
- If relative: resolved under `<home>`
- If absolute: used as-is

Use this when:
- chain definitions are centrally managed elsewhere
- running environment-specific chain sets

Examples:
- `vProx -chains /etc/vprox/chains`
- `vProx -chains chains-staging`

---

### `-log-file string`
Override main log file path.

- Default: `<home>/data/logs/main.log`
- If relative: resolved under `<home>/data/logs`
- If absolute: used as-is

Use this when:
- integrating with host logging/rotation conventions
- separating test and production logs

Examples:
- `vProx -log-file main-dev.log`
- `vProx -log-file /var/log/vprox/main.log`

---

### `-addr string`
Set listen address for HTTP server.

- Default order:
  - `VPROX_ADDR` env if set
  - otherwise `:3000`
- CLI always overrides env.

Use this when:
- binding a different port
- binding to explicit interfaces

Examples:
- `vProx -addr :8080`
- `vProx -addr 0.0.0.0:3000`
- `vProx -addr 127.0.0.1:3000`

---

## Operational mode flags

### `-version`
Print version text and exit immediately.

Current output includes product name and static version string.

Use this when:
- validating installed binary version in scripts

---

### `-validate`
Load/validate config and exit.

What it does:
- requires `ports.toml` to exist in resolved config dir
- loads chain configs from resolved `chains` directory and also `config` for compatibility
- validates schema constraints (hostnames, IPs, service compatibility, port ranges, etc.)
- prints validation summary

Exit behavior:
- success: exits 0
- validation/load failure: exits non-zero

Use this when:
- pre-flight checks before deploy/restart
- CI sanity check for config changes

---

### `-info`
Load config, print runtime/config summary, and exit.

What it shows:
- resolved directories and main log path
- loaded chains
- default ports
- extra chain detail if `-verbose` is also set

Use this when:
- auditing active configuration resolution
- troubleshooting missing/duplicate chain hosts

---

### `-dry-run`
Load everything and print runtime readiness summary, but **do not start server**.

Includes:
- resolved listen address
- number of loaded chains
- effective limiter settings
- auto-quarantine enabled/disabled
- backup enabled/disabled

Use this when:
- testing startup behavior safely
- checking CLI/env override outcomes

---

### `-backup`
Run one backup operation and exit.

Behavior:
- invokes backup module once using main log and archive paths
- does **not** start the proxy server

Use this when:
- manual archive/rotation action
- ad-hoc operational maintenance

Also supported shorthand command:
- `vProx backup` (mapped internally to `--backup`)

---

## Rate-limiter override flags

These flags override limiter environment values for this process execution.

### `-rps float`
Override default requests-per-second rate.

- env fallback: `VPROX_RPS`
- built-in default: `25`
- applied only when value `> 0`

Use this when:
- temporarily tightening/loosening traffic profile

Example:
- `vProx -rps 50`

---

### `-burst int`
Override default burst capacity.

- env fallback: `VPROX_BURST`
- built-in default: `100`
- applied only when value `> 0`

Use this when:
- accommodating bursty but legitimate clients

Example:
- `vProx -burst 250`

---

### `-disable-auto`
Disable auto-quarantine logic.

- equivalent outcome to setting `VPROX_AUTO_ENABLED=false`
- explicit CLI override takes precedence

Use this when:
- running controlled tests
- diagnosing limiter behavior without auto penalties

---

### `-auto-rps float`
Override auto-quarantine penalty RPS.

- env fallback: `VPROX_AUTO_RPS`
- built-in default: `1`
- applied only when value `> 0`

Use this when:
- you want soft or strict penalty profiles for quarantined clients

Example:
- `vProx -auto-rps 0.5`

---

### `-auto-burst int`
Override auto-quarantine penalty burst.

- env fallback: `VPROX_AUTO_BURST`
- built-in default: `1`
- applied only when value `> 0`

Use this when:
- quarantined clients still need brief burst allowance

Example:
- `vProx -auto-burst 2`

---

## Backup control flag

### `-disable-backup`
Disable automatic backup loop at startup.

What it affects:
- only the background auto-backup scheduler
- does not disable manual `-backup` run mode

Use this when:
- running ephemeral environments
- external log rotation handles archival

---

## Logging/verbosity flags

### `-verbose`
Enable extra startup detail logs.

Adds messages for:
- CLI overrides
- per-chain details in `-info`
- selected mode diagnostics

Use this when:
- debugging startup/flag interactions

---

### `-quiet`
Flag exists, but current implementation still writes logs normally to the configured log file.

Practical note:
- do not rely on `-quiet` for strict silence behavior today
- treat it as reserved/minimal-effect in current build

Use this when:
- future compatibility (safe no-op-ish behavior today)

---

## Precedence and interaction examples

### CLI beats env

If both are set, CLI wins:

- `VPROX_RPS=30 vProx -rps 100` → effective default limiter RPS is `100`

### Dry-run with overrides

`vProx -dry-run -rps 60 -burst 300 -disable-auto -disable-backup -verbose`

Useful for validating effective startup profile without serving traffic.

### Custom dirs + info

`vProx -home /srv/vprox -chains /srv/vprox/chains-prod -info -verbose`

Useful to confirm exactly which directories are being used.

---

## Common workflows

### 1) Pre-deploy validation
- `vProx -validate`
- `vProx -dry-run -verbose`

### 2) Investigate config resolution
- `vProx -info -verbose`

### 3) Temporary traffic hardening
- `vProx -rps 10 -burst 20 -auto-rps 0.5 -auto-burst 1`

### 4) Backup-only operation
- `vProx -backup`

---

## Exit expectations

Typical outcomes:
- `-version`, `-validate` success, `-info`, `-dry-run`, `-backup` success → exit `0`
- fatal startup/config/build path issues → non-zero

---

## Related files

- `main.go` (flag definitions and runtime behavior)
- `FLAGS.md` (short companion reference)
- `README.md` (project usage overview)
