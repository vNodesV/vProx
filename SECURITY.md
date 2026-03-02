# Security Policy

## Supported Versions

| Version | Supported          |
| ------- | ------------------ |
| 1.2.x   | ✅ Active support  |
| < 1.2   | ❌ No longer supported |

Only the latest minor release receives security patches.

## Reporting a Vulnerability

**Do not open a public issue for security vulnerabilities.**

Please report security vulnerabilities through GitHub's private security
advisory feature:

1. Navigate to the repository's **Security** tab.
2. Click **Report a vulnerability**.
3. Provide a clear description, reproduction steps, and impact assessment.

Alternatively, if the advisory feature is unavailable, contact the
maintainers directly via the email listed in the repository profile.

### Response SLA

| Severity | Acknowledgment | Patch Target |
| -------- | -------------- | ------------ |
| CRITICAL | Within 72 hours | Within 14 days |
| HIGH     | Within 72 hours | Within 30 days |
| MEDIUM   | Within 1 week   | Next release   |
| LOW      | Within 2 weeks  | Best effort    |

## Out of Scope

The following are **not** considered security vulnerabilities for this project:

- **Denial of service via traffic flooding** — mitigated by the built-in rate
  limiter and expected to be handled at the infrastructure/network layer.
- **Public node endpoints** — vProx proxies public Cosmos SDK node endpoints
  (RPC, REST, gRPC, WebSocket) that are intentionally exposed to the internet.
  Information available through these endpoints is public by design.

## Responsible Disclosure Credit

Reporters who follow responsible disclosure will be credited in the
**CHANGELOG** and **release notes** for the patch release that addresses their
finding (unless they prefer to remain anonymous).

## Security Best Practices for Operators

- Run vProx behind a reverse proxy (nginx, Cloudflare) with TLS termination.
- Set `trusted_proxies` in chain config to restrict X-Forwarded-For trust.
- Keep `bind_address` set to `127.0.0.1` (the default) for vLog.
- Rotate API keys (`api_key`, `vt_api_key`, `abuseipdb_api_key`) regularly.
- Monitor rate-limit JSONL logs for anomalous patterns.
