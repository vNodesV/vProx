# Checkpoint: 2026-02-19 — Structured logs + request correlation

## Snapshot
This checkpoint captures the repo state after:
- CLI `--flags` expansion and `FLAGS.md` documentation.
- Standardized structured logging across access, limiter, websocket, and backup.
- End-to-end request correlation using `X-Request-ID` (`request_id` in logs).

Repo: `vNodesV/vProx` (branch: `main`)

## What changed (high level)
### Logging
- `main.log` is now consistent, single-line, structured key/value output.
- `rate-limit.jsonl` is now a more standard JSONL schema **while preserving legacy fields** to avoid breaking downstream parsers.

### Request correlation
- `X-Request-ID` is accepted from clients if it passes basic safety validation; otherwise a new ID is generated.
- The request ID is echoed back on responses.
- The request ID is included as `request_id` in:
  - access logs (`main.log`)
  - limiter mirror logs (`main.log`)
  - limiter JSONL records (`rate-limit.jsonl`)
  - websocket session close logs (`main.log`)

## Files involved
- `internal/logging/logging.go`
  - Shared structured logging helpers.
  - Request ID helpers:
    - `EnsureRequestID(r *http.Request) string`
    - `RequestIDFrom(r *http.Request) string`
    - `SetResponseRequestID(w http.ResponseWriter, id string)`
    - `NewRequestID() string`
- `main.go`
  - Access logging standardized + `request_id` added.
  - Expanded CLI flags (see `FLAGS.md`).
- `internal/limit/limiter.go`
  - JSONL schema standardized.
  - Legacy compatibility preserved (`reason`, `ua`) while adding `event`, `user_agent`.
  - Request ID ensured, returned, and logged.
- `internal/ws/ws.go`
  - WS session logs standardized + `request_id` added.
- `internal/backup/backup.go`
  - Backup logs standardized.

## Log schema notes
### `main.log` (structured line)
- Format: key/value fields, stable quoting.
- Always include `ts`, `level`, `event`.
- Where applicable include `request_id`.

### `rate-limit.jsonl`
- New-ish fields: `level`, `event`, `user_agent`, `request_id`.
- Legacy fields kept: `reason`, `ua`.

## Runtime verification performed
Using strict limiter settings (example: `-rps 1 -burst 1 -disable-auto`):
- Request without inbound `X-Request-ID`:
  - response includes generated request id header (HTTP canonicalization may display as `X-Request-Id`).
- Request with inbound `X-Request-ID: external-req-123`:
  - response echoes that id.
  - limiter event includes `request_id` in both `main.log` and `rate-limit.jsonl`.

## Known follow-ups (not done yet)
1. **Propagate `X-Request-ID` upstream** when proxying (HTTP + WS dial). Today it’s used for local correlation (response + local logs), but not necessarily forwarded to the backend.
2. Consider whether to always emit the header for non-proxied early errors (it already does for limiter; verify all early-return paths).
3. Confirm there are no remaining ad-hoc log emitters bypassing `internal/logging`.

## How to continue from here
- Preferred pattern: any new logging should use `internal/logging`.
- Any request-scoped event should include `request_id`.
- If you change limiter JSONL fields, keep compatibility keys unless there’s an explicit breaking change plan.
