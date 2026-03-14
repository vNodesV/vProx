# vProx v1.4.0 — Master Plan

## Summary

v1.4.0 is a consolidation + polish release:
1. **Config restructure** — three-way TOML split (identity / service-node / infra)
2. **vLog → vOps merge** — single binary, unified CLI, package rename
3. **Themes + Preferences** — CSS variable themes, user settings section
4. **CLI unification** — merged commands, removed dupes, shell auto-completion
5. **Wizard/Settings parity** — full config field coverage audit

---

## 1. Config Restructure (Three-Way Split)

### Target layout

```
config/
  vprox.toml                        # proxy core (ports, rate-limit, logging)
  vops.toml                         # vOps/dashboard (replaces vlog.toml)
  modules.toml                      # installed modules registry (unchanged)

  chains/                           # IDENTITY ONLY (.sample = read-only templates)
    <chain>.sample                  # chain_id, tree_name, dashboard_name, network_type, explorer

  services/
    nodes/                          # per-node proxy + management config
      <chain>-<role>-<dc>.toml      # tree = "<chain_tree_name>" (join key)

  modules/
    infra/                          # physical host registry
      <datacenter>.toml             # [[host]] array-of-tables with [host.ping] subtable
    backup/
      backup.toml                   # unchanged
    fleet/
      settings.toml                 # unchanged

  vhosts/                           # per-vhost (unchanged)
    *.toml
```

### Key decisions
- `.sample` files = identity templates, never written by proxy at runtime
- `services/nodes/` scanner replaces `registered_chains` SQLite table
- `ServiceNode.tree == ChainIdentity.tree_name` → tree-join replaces slug matching
- `[[host]]` TOML array-of-tables in infra replaces flat VM fields

### Migration phases
- P1: Add `.sample` identity files (additive, no breaking change)
- P2: Add `config/services/nodes/` loaders (additive)
- P3: Dashboard tree-join via `tree` field (replaces `deriveChainBase()`)
- P4: Add `config/modules/infra/` (replaces `config/infra/`)
- P5: Deprecate `chains/*.toml` proxy sections (warn on load)
- P6: Remove legacy loaders

---

## 2. vLog → vOps Merge

### Rename map
| Before | After |
|--------|-------|
| Binary: `vlog` | `vprox vops` subcommand |
| Package: `internal/vlog` | `internal/vops` |
| Config: `config/vlog.toml` | `config/vops.toml` |
| Service: `vlog.service` | `vops.service` (compat alias during transition) |
| URL prefix: `/vlog/` | `/vops/` (configurable via `base_path`) |
| DB: `vlog.db` | `vops.db` |

### Binary consolidation
- `vprox start` → starts proxy + vOps together (errgroup coordination)
- `vprox vops start` → vOps standalone
- `vprox proxy start` → proxy standalone (for debug/maintenance)
- Single binary, build tags: `//go:build !novops` to exclude vOps from proxy-only builds

---

## 3. Unified CLI

### Proposed command tree (post-merge)
```
vprox [flags]                        # show help
vprox start [--proxy-only|--vops-only]  # start services
vprox stop                           # stop all
vprox restart                        # restart all
vprox status                         # show service status

vprox proxy start|stop|restart       # proxy only
vprox vops  start|stop|restart       # vOps only

vprox vops archives [list]           # (was: vlog --list-archives)
vprox vops accounts [list]           # (was: vlog --list-accounts)
vprox vops threats  [list]           # (was: vlog --list-threats)
vprox vops enrich   <ip>             # (was: vlog --enrich <ip>)
vprox vops cache    purge [<ip>|all] # (was: vlog --purge-cache)
vprox vops ingest                    # (was: vlog ingest)

vprox fleet [hosts|vms|chains|deploy|update|unregister]
vprox mod   [list|add|update|remove|restart]
vprox chain [status|upgrade]
vprox config [--web|ports|settings|chain|vops|fleet|infra|backup|list|validate]

vprox --validate                     # validate all configs
vprox --info                         # show config summary
vprox --dry-run                      # load + verify, no start
vprox --version
vprox --help
```

### Removed flags (duplicates)
- `vlog start|stop|restart` → absorbed into `vprox vops start|stop|restart`
- `vlog --validate|--info|--dry-run` → already in `vprox`
- `vlog --quiet|--verbose|--version` → already in `vprox`

### Deduplicated flags (unified)
- `--home` → single shared $VPROX_HOME for both proxy + vOps
- `--config` → points to config/ directory
- `--daemon / -d` → starts the combined service

### New: Shell Auto-Completion
- Generate bash/zsh/fish completion scripts via `vprox completion [bash|zsh|fish]`
- Static output (no runtime deps): pipe to `~/.local/share/bash-completion/completions/vprox`
- Covers all levels: commands, subcommands, flag names
- Install via `make install` target

---

## 4. Themes + Preferences

### Architecture
- CSS custom property approach: `<html data-theme="vnodes|dark-blue|light-blue">`
- Theme switch via Settings → Preferences → Theme selector
- Persisted in `[vops.ui] theme = "..."` (config) + localStorage (client override)
- Server-side initial render reads cookie `vprox_theme` for flash-free load

### Three themes

#### Theme 1: vNodes[V] (current, default)
- Dark background, Matrix green (`#00ff00` / `#63ff93`), monospace
- Design tokens: all existing `--vn-*` variables unchanged

#### Theme 2: Dark Blue
- `--vn-bg: #0a0e1a`
- `--vn-text: #e8eaf6`
- `--vn-green: #4fc3f7` (blue accent instead of green)
- `--vn-green-border: rgba(79,195,247,0.25)`
- `--vn-green-glow: rgba(79,195,247,0.08)`
- Keeps glass morphism cards, same layout

#### Theme 3: Light Blue (Classic)
- `--vn-bg: #f0f4f8`
- `--vn-bg-card: rgba(255,255,255,0.92)`
- `--vn-text: #1a2332`
- `--vn-text-muted: #556080`
- `--vn-border: rgba(30,80,160,0.2)`
- `--vn-green: #1565c0` (primary blue)
- No blur/glow effects
- Clean, SaaS/enterprise look

### Preferences section in Settings
Fields:
- Theme (visual swatches: vNodes[V] | Dark Blue | Light Blue)
- Dashboard refresh interval (30s | 65s | 120s | Manual)
- Default accounts page size (25 | 50 | 100 | 200)
- Date format (Relative | Absolute | ISO 8601)
- Monospace field names in editors (on/off)
- Sidebar collapsed by default (on/off)

---

## 5. Wizard / Settings Parity Audit

Config files vs current wizard/settings coverage:
(Full audit to be done during implementation — flag any missing fields)

Priority gaps found so far:
- `[server]` block from `vprox.toml` not in wizard
- `[vops.intel]` API keys in wizard step 4 but not in Settings
- `[validator]` block in service-node TOML not in Chain editor
- `[ws]` block fields (idle_timeout, lifetime) missing from chain editor

---

## 6. Strategic Additions (RICE-scored)

| Feature | Reach | Impact | Confidence | Effort | RICE | Priority |
|---------|-------|--------|------------|--------|------|----------|
| Single binary (vprox+vops) | 10 | 9 | 9 | 5 | 162 | P1 |
| Shell auto-completion | 8 | 7 | 10 | 2 | 280 | P1 |
| Config v1.4.0 restructure | 10 | 8 | 9 | 6 | 120 | P1 |
| vOps rename + merge | 10 | 8 | 10 | 4 | 200 | P1 |
| Themes system | 7 | 6 | 9 | 3 | 126 | P2 |
| Preferences section | 6 | 5 | 9 | 2 | 135 | P2 |
| Real-time SSE dashboard | 5 | 7 | 7 | 5 | 49 | P3 |
| vProxWeb (replace Apache) | 8 | 9 | 6 | 9 | 48 | P3 |
| Upgrade workflow automation | 6 | 9 | 7 | 7 | 54 | P2 |
| Chain health scoring | 5 | 7 | 8 | 4 | 70 | P2 |
| Adaptive rate limiting (ML) | 3 | 8 | 6 | 8 | 18 | P3 |

---

## Implementation Sequence

Phase 1 (foundation):
  - Config P1+P2 loaders (additive, no breaking)
  - vOps package rename + binary merge
  - Unified CLI + auto-completion

Phase 2 (features):
  - Config P3+P4 (tree-join, infra restructure)
  - Themes + Preferences

Phase 3 (polish):
  - Config P5+P6 (deprecate + remove legacy)
  - Wizard/Settings parity audit
  - Upgrade automation

