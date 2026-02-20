# vProx flags (quick reference)

For the complete and up-to-date detailed documentation of every flag, see:

- [`CLI_FLAGS_GUIDE.md`](./CLI_FLAGS_GUIDE.md)

This file intentionally stays short.

## Quick usage

- `vProx --help` — show built-in help
- `vProx --validate` — validate config and exit
- `vProx --info --verbose` — print resolved runtime/config summary
- `vProx --dry-run` — load everything without starting server
- `vProx --backup` — run one backup cycle and exit
- `vProx backup --reset_count` — backup and reset persisted access counters

## Most common overrides

- `--home`, `--config`, `--chains`, `--log-file`, `--addr`
- `--rps`, `--burst`, `--disable-auto`, `--auto-rps`, `--auto-burst`
- `--disable-backup`, `--reset_count` (`--reset-count`), `--verbose`, `--quiet`, `--version`
