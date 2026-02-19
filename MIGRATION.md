# vProx Migration Notes - Directory Structure Update

## Summary of Changes

This update restructures the vProx directory layout for better organization and consistency. All data files (logs, geolocation databases) are now consolidated under `data/`.

## New Directory Structure

```
$HOME/.vProx/
├── config/                    # Global configuration
│   └── ports.toml            # Default port settings
├── chains/                    # Per-chain configurations (*.toml)
│   └── chain.sample.toml     # Sample configuration
├── data/                      # All application data
│   ├── geolocation/          # GeoLocation databases
│   │   └── ip2location.mmdb
│   └── logs/                 # Application logs
│       ├── main.log
│       ├── rate-limit.jsonl
│       └── archived/         # Archived log backups
├── internal/                  # Reserved for internal use
└── .env                       # Environment variables
```

## Migration from Old Structure

If you have an existing installation with the old structure:

### Old Structure
```
$HOME/.vProx/
├── config/
├── data/
│   └── ip2location.mmdb      # OLD location
├── logs/                      # OLD location
│   ├── main.log
│   ├── rate-limit.jsonl
│   └── archived/
```

### Migration Steps

1. **Backup existing data** (optional but recommended):
   ```bash
   tar -czf ~/vProx-backup-$(date +%Y%m%d).tar.gz ~/.vProx/
   ```

2. **Move logs to new location**:
   ```bash
   mkdir -p ~/.vProx/data/logs
   if [ -d ~/.vProx/logs ]; then
     mv ~/.vProx/logs/* ~/.vProx/data/logs/ 2>/dev/null || true
     rmdir ~/.vProx/logs 2>/dev/null || true
   fi
   ```

3. **Move geolocation database** (if using old location):
   ```bash
   mkdir -p ~/.vProx/data/geolocation
   if [ -f ~/.vProx/data/ip2location.mmdb ]; then
     mv ~/.vProx/data/ip2location.mmdb ~/.vProx/data/geolocation/
   fi
   ```

4. **Update .env file** (if exists):
   ```bash
   if [ -f ~/.vProx/.env ]; then
     sed -i 's|IP2LOCATION_MMDB=.*|IP2LOCATION_MMDB='$HOME'/.vProx/data/geolocation/ip2location.mmdb|' ~/.vProx/.env
   fi
   ```

5. **Re-run make install** to ensure all directories are created:
   ```bash
   cd /path/to/vProx/repo
   make install
   ```

6. **Restart the service** (if running as systemd service):
   ```bash
   sudo systemctl restart vprox
   ```

## Key Changes

### 1. **Geolocation Database**
- **Old**: Various locations searched, no standard install location
- **New**: `$HOME/.vProx/data/geolocation/ip2location.mmdb`
- **Benefit**: Automatically installed during `make install`

### 2. **Logs Directory**
- **Old**: `$HOME/.vProx/logs/`
- **New**: `$HOME/.vProx/data/logs/`
- **Benefit**: All data consolidated under `data/` directory

### 3. **Chain Configurations**
- **Old**: `$HOME/.vProx/config/*.toml` (mixed with ports.toml)
- **New**: `$HOME/.vProx/chains/*.toml` (dedicated directory)
- **Backward compatibility**: Still loads configs from `config/` if present
- **Benefit**: Cleaner separation of global config vs chain-specific configs

### 4. **GEO Database Installation**
- **Old**: Required `make GEO=true install`
- **New**: Automatically installed if `ip2l/ip2location.mmdb` exists in repo
- **Note**: GeoLite2 database is freely redistributable under Creative Commons
- **Benefit**: One less step during installation

### 5. **Makefile Enhancements**
- ✅ **GOPATH/GOROOT validation**: Verifies Go environment before build
- ✅ **Service file validation**: Checks if `vProx.service` exists and validates `ExecStart`
- ✅ **Improved feedback**: Better status messages during installation
- ✅ **Directory creation**: All directories created automatically

### 6. **Systemd Service**
- Now checks if service file exists with correct ExecStart path
- Only updates if ExecStart differs or file doesn't exist
- Uses current user's HOME and USER automatically
- Improved service description and configuration

## Affected Code Paths

### Go Code Changes
1. **cmd/vprox/main.go**: 
   - Added `chainsDir` variable
   - Updated directory initialization to use `data/logs`
   - Loads chain configs from both `chains/` and `config/` for backward compatibility

2. **internal/geo/geo.go**:
   - Added `$HOME/.vProx/data/geolocation/ip2location.mmdb` as primary search path
   - Maintains backward compatibility with system-wide locations

3. **internal/limit/limiter.go**:
   - Default log path changed to `$HOME/.vProx/data/logs/rate-limit.jsonl`

4. **internal/backup/cfg/config.toml**:
   - Updated paths to use `data/logs/` structure

## Configuration Files

### .env
Now automatically sets IP2LOCATION_MMDB to the new location:
```bash
IP2LOCATION_MMDB=$HOME/.vProx/data/geolocation/ip2location.mmdb
```

### Chain Configurations
Can now be placed in either:
- `$HOME/.vProx/chains/` (preferred)
- `$HOME/.vProx/config/` (backward compatibility)

Sample configuration available at:
- Repo: `chains/chain.sample.toml`
- Installed: `$HOME/.vProx/chains/chain.sample.toml`

## Testing

### Verify Installation
```bash
# Check directory structure
tree -L 3 ~/.vProx/

# Verify geolocation database
ls -lh ~/.vProx/data/geolocation/

# Check if application can find config and geo database
make build
./.build/vProx --help
timeout 2 ./.build/vProx 2>&1 | grep -i geo
```

### Verify Systemd Service
```bash
# Check service file
sudo cat /etc/systemd/system/vprox.service

# Verify it points to correct paths
sudo grep -E "ExecStart|WorkingDirectory|VPROX_HOME" /etc/systemd/system/vprox.service
```

## Rollback

If you need to rollback to the old structure:

1. Restore your backup:
   ```bash
   tar -xzf ~/vProx-backup-*.tar.gz -C ~/
   ```

2. Revert to the previous git commit:
   ```bash
   cd /path/to/vProx/repo
   git checkout <previous-commit>
   make install
   ```

## Questions?

**Q: Will my existing chain configs still work?**
A: Yes! The application still loads configs from `$HOME/.vProx/config/*.toml` for backward compatibility.

**Q: Do I need to update my .env file?**
A: If you run `make install`, it will create a new .env with correct paths. Existing .env files are preserved.

**Q: What if I don't have the geo database?**
A: The application still works without it; geo features will just be disabled.

**Q: Can I use my own systemd service file?**
A: Yes! The Makefile checks if the ExecStart path matches `/usr/local/bin/vProx`. If it does, your custom service file is preserved.

**Q: Is there performance impact?**
A: No, this is purely a reorganization of file locations. There's no performance impact.
