# Agent directive (repo-local): vProx

This file is meant for future sessions (human or agent) to preserve conventions and reduce regressions.

## Operating mode (jarvis4.0)
This repo is often worked on with a "jarvis4.0" agent workflow. The Cosmos-SDK-specific parts of that mode are **not** applicable here, but the execution discipline is.

- Prefer small, testable changes; avoid broad refactors unless required.
- Before editing code, read enough surrounding context to avoid accidental API/behavior changes.
- Use repo-native patterns instead of introducing new ones.
- Validate frequently:
  - `gofmt` on touched files
  - `go build ./...` after meaningful changes
  - `go test ./...` when behavior could be impacted
- Don't "paper over" errors; fix root causes.
- Logging/schema changes are treated as API changes; prefer additive evolution.

### Checkpoints
When completing a discrete chunk of work, add a checkpoint under:
- `docs/checkpoints/YYYY-MM-DD-<topic>.md`

Include:
- what changed and why
- files/symbols involved
- how it was verified (commands + observed behavior)
- known follow-ups / risks

## Project summary
`vProx` is a Go reverse proxy with:
- HTTP proxying
- WebSocket proxying
- Rate limiting
- Optional geo enrichment
- Log archival/backup routines

## Conventions

### Structured logging
- Prefer structured, single-line logs everywhere.
- Use the shared helper in `internal/logging/logging.go`.
- Log lines in `main.log` should be stable and parseable:
  - include `ts`, `level`, `event` as baseline fields
  - avoid multi-line log blocks
  - use quoted strings where values may contain spaces

### Limiter JSONL compatibility
- `~/.vProx/data/logs/rate-limit.jsonl` is treated as an external interface.
- When standardizing fields, preserve existing keys if downstream tooling might depend on them.
  - Example: keep legacy `reason` while also providing standard `event`.
  - Example: keep legacy `ua` while also providing `user_agent`.

### Request correlation (`X-Request-ID`)
- Always ensure a request ID exists for request-scoped operations:
  - Use `logging.EnsureRequestID(r)` early in the request path.
  - Echo it back with `logging.SetResponseRequestID(w, id)`.
- Log it consistently as `request_id`.
- Validation: accept inbound IDs only if safe (length/charset); otherwise generate.
- Note: net/http canonicalizes header keys; clients may display `X-Request-Id`.

### Proxying behavior (future enhancement)
- If/when implementing upstream propagation:
  - Forward `X-Request-ID` to upstream HTTP requests.
  - Include it in WS dial headers.
  - Ensure this does not override an explicit upstream-provided ID unless intended.

## CLI / config
- CLI flags were expanded and documented in `FLAGS.md`.
- Expected operator modes include: validate/info/dry-run.

## Build & test
Typical commands:
- `go build ./...`
- `go test ./...`

When changing logging or middleware behavior:
- run a quick local request sequence
- verify `main.log` and `rate-limit.jsonl` schemas remain intact

## Gotchas
- Avoid breaking file formats under `~/.vProx/data/logs/` without a migration strategy.
- Preserve current fields when "standardizing" output (additive changes are preferred).
