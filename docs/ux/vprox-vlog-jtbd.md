# Jobs-to-be-Done: vProx & vLog

> UX Research — 2026-03-06

---

## Audience

**Who operates vProx?**

| Persona | Role | Skill Level | Device |
|---------|------|-------------|--------|
| **Node Operator** | Runs Cosmos SDK validator / RPC infrastructure | Intermediate to expert Linux + networking | Desktop / SSH terminal |
| **Security Analyst** | Reviews threat intel, manages IP blocks, investigates anomalies | Intermediate networking + security | Desktop browser |
| **DevOps Engineer** | Installs, updates, monitors, deploys vProx + vLog | Expert CLI + systemd | SSH terminal + browser |
| **New Team Member** | Joins an existing validator operation | Variable; often unfamiliar with vProx | SSH terminal, docs |

---

## JTBD #1 — Safe Exposure of Cosmos Node Endpoints

### Job Statement
> When I run a Cosmos validator or RPC node that needs to be publicly reachable, I want to put a hardened reverse proxy in front of it, so I can expose RPC/REST/gRPC/WebSocket endpoints without opening my node directly to the internet.

### Context
- Situation: Operator has a Cosmos SDK node (CometBFT RPC `:26657`, REST `:1317`, gRPC `:9090`) that clients need to reach
- Motivation: Prevent direct abuse — rate limiting, geo-blocking, IP banning, cost control
- Outcome: Node is reachable by legitimate clients; bots/spammers/scrapers are throttled

### Current Solution & Pain Points
- **Current:** nginx/Apache with manual rate-limit config, or direct node exposure
- **Pain:** Manual config for each service type; no Cosmos-aware routing; no per-IP counters; no auto-quarantine

### What vProx Must Do Well for This Job
- [x] One config file per chain — clear, not repetitive
- [x] Start/stop/restart in seconds
- [x] Rate limiting "just works" with sensible defaults
- [ ] ❌ Auto-quarantine behavior is invisible at startup — operator doesn't know it's on
- [ ] ❌ Subdomain vs path routing decision is underdocumented — first config is a guessing game
- [ ] ❌ TOML errors don't explain how to fix them — "expected value at line 42" with no hint

---

## JTBD #2 — Real-Time Threat Visibility

### Job Statement
> When my proxy is handling hundreds of requests per minute, I want to know which IPs are abusive or dangerous, so I can block them before they cause node instability or inflate my bandwidth bill.

### Context
- Situation: Proxy has been running for days/weeks; operator notices high rate-limit events or suspicious traffic spikes
- Motivation: Identify bad actors quickly; not wade through raw logs
- Outcome: A ranked list of suspect IPs with enriched context (ASN, threat score, history)

### Current Solution & Pain Points
- **Current:** `grep` through `rate-limit.jsonl`; manual AbuseIPDB lookups; intuition
- **Pain:** No aggregation; no visual ranking; investigates one IP at a time; context switching between terminal and browser

### What vLog Must Do Well for This Job
- [x] Dashboard surfaces flagged IPs with threat scores at a glance
- [x] One-click investigation → TI + OSINT in real time
- [x] Block IP from within the UI (UFW rule applied)
- [ ] ❌ 60-second polling lag — logs may not appear for a minute after backup
- [ ] ❌ No "batch investigate" — can't scan 20 flagged IPs simultaneously
- [ ] ❌ Events on detail page truncated at 20 — can't see full IP history without DB access

---

## JTBD #3 — Reliable Log Archival & Rotation

### Job Statement
> When my proxy log grows large, I want it automatically compressed and archived on a schedule, so I can keep disk usage under control and have a historical record for audits.

### Context
- Situation: Long-running proxy deployment; logs grow continuously
- Motivation: Prevent disk-full crashes; keep archived logs organized and findable
- Outcome: Logs archived on schedule or at size threshold; available in vLog for analysis

### Current Solution & Pain Points
- **Current:** Manual `logrotate` + custom cron jobs; losing history in rotations
- **Pain:** Separate tooling; easy to forget; no vLog integration; no size-trigger

### What vProx Backup Must Do Well for This Job
- [x] Dual trigger: time-based (every N days) OR size-based (> N MB)
- [x] `--new-backup` for on-demand execution
- [ ] ❌ `--disable-backup` mutates backup.toml silently — surprising persistent state change
- [ ] ❌ No UI feedback if backup push to vLog fails — silently ignored
- [ ] ❌ Destination path is implicit — operator discovers it from code or trial

---

## JTBD #4 — New Node Operator Onboarding

### Job Statement
> When I join a team that already runs vProx, I want to get the proxy configured and running for a new chain in under 30 minutes, so I can contribute without blocking the team.

### Context
- Situation: New team member; existing `~/.vProx/` directory structure may already exist
- Motivation: Be productive quickly; not spend time decoding config files
- Outcome: New chain proxied correctly; monitoring active; no senior-engineer handholding

### Current Solution & Pain Points
- **Current:** Senior engineer walks through config manually; README + INSTALLATION.md
- **Pain:** No interactive setup wizard; sample TOML is under-commented; subdomain vs path is unclear; error messages don't guide recovery

### What vProx Must Do Well for This Job
- [x] `make install` is a single command
- [x] Sample TOML exists as a starting point
- [x] `--validate` catches config errors before startup
- [ ] ❌ Error messages lack recovery hints ("no chain configs found" → no suggestion to copy the sample)
- [ ] ❌ No architecture diagram showing request flow
- [ ] ❌ Auto-quarantine is not explained in onboarding docs — new operators don't know it's on

---

## JTBD #5 — Validator Fleet Deployment & Status

### Job Statement
> When I manage multiple validator VMs across chains, I want to deploy upgrades and check their status from a central dashboard, so I don't have to SSH into each server individually.

### Context
- Situation: Operator manages 3–20 validator VMs across 2–6 chains; governance proposals trigger upgrades
- Motivation: Reduce manual SSH ops; know upgrade status without checking each box
- Outcome: Upgrade deployed and verified from the vLog dashboard

### Current Solution & Pain Points
- **Current:** Manual SSH scripts; per-VM status checks; missed upgrade windows
- **Pain:** No central view; no automated deployment; easy to forget a server

### What vLog Push Module Must Do Well for This Job
- [x] Chain Status table shows all VMs + governance + sync status
- [x] Deploy Wizard panel for on-demand script execution
- [ ] ❌ Push module setup (vms.toml + SSH keys) has no wizard or validation step
- [ ] ❌ Chain Status table is dense (16 columns) — overwhelming on first view
- [ ] ❌ No alert if a VM falls behind in block height
