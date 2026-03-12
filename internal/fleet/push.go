// Package fleet is the vProx validator deployment control plane.
//
// It provides:
//   - SSH-based deployment of chain scripts to remote VMs
//   - Cosmos node status polling (height, governance, upgrade plan)
//   - SQLite-backed deployment history and registered-chain registry
//   - HTTP API handlers wired into the vLog web server
package fleet

import (
	"context"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/vNodesV/vProx/internal/fleet/config"
	"github.com/vNodesV/vProx/internal/fleet/runner"
	"github.com/vNodesV/vProx/internal/fleet/state"
	"github.com/vNodesV/vProx/internal/fleet/status"
)

// Service is the top-level fleet control plane.
// Inject into the vLog web server for direct-call API handlers.
type Service struct {
	cfg    *config.Config
	db     *state.DB
	runner *runner.Runner

	mu       sync.RWMutex
	statuses map[string]*status.ChainStatus // chain name → latest polled status
}

// New creates a Service from vmsCfgPath and opens the SQLite database at dbPath.
func New(vmsCfgPath, dbPath string) (*Service, error) {
	cfg, err := config.Load(vmsCfgPath)
	if err != nil {
		return nil, fmt.Errorf("fleet: load config: %w", err)
	}

	db, err := state.Open(dbPath)
	if err != nil {
		return nil, fmt.Errorf("fleet: open state db: %w", err)
	}

	return &Service{
		cfg:      cfg,
		db:       db,
		runner:   runner.New(),
		statuses: make(map[string]*status.ChainStatus),
	}, nil
}

// NewEmpty creates a Service with an empty VM registry and opens the SQLite database.
// Use when no vms.toml exists but chain management sections will be loaded via
// AddChainConfigs. The fleet module starts managing VMs after AddChainConfigs is called.
func NewEmpty(dbPath string) (*Service, error) {
	db, err := state.Open(dbPath)
	if err != nil {
		return nil, fmt.Errorf("fleet: open state db: %w", err)
	}
	return &Service{
		cfg:      &config.Config{},
		db:       db,
		runner:   runner.New(),
		statuses: make(map[string]*status.ChainStatus),
	}, nil
}

// SetConfig replaces the runtime fleet configuration and clears cached statuses.
// The next Poll() repopulates chain status using the updated VM registry.
func (s *Service) SetConfig(cfg *config.Config) {
	if cfg == nil {
		cfg = &config.Config{}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.cfg = cfg
	s.statuses = make(map[string]*status.ChainStatus)
}

// AddInfraConfigs loads per-datacenter host files from dir (config/infra/*.toml)
// and merges their hosts and VMs into the service config.
// Each infra file defines one physical host ([host]) and its child VMs ([[vm]]).
func (s *Service) AddInfraConfigs(dir string) error {
	infraCfg, err := config.LoadFromInfraFiles(dir)
	if err != nil {
		return fmt.Errorf("fleet: load infra configs from %s: %w", dir, err)
	}
	if len(infraCfg.Hosts) == 0 && len(infraCfg.VMs) == 0 {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.cfg = config.MergeInfraConfig(s.cfg, infraCfg)
	return nil
}

// AddChainConfigs merges VM entries derived from chain TOML [management] sections
// into the service config. Chain-derived entries take precedence over vms.toml.
// Call after New() (or NewEmpty()) when chains_dir is configured in vlog.toml.
func (s *Service) AddChainConfigs(chainsDir string, defaults config.FleetDefaults) error {
	chainCfg, err := config.LoadFromChainConfigs(chainsDir, defaults)
	if err != nil {
		return fmt.Errorf("fleet: load chain configs from %s: %w", chainsDir, err)
	}
	if len(chainCfg.VMs) == 0 {
		return nil
	}

	s.mu.Lock()
	s.cfg = config.MergeConfigs(s.cfg, chainCfg)
	s.mu.Unlock()
	return nil
}

// Close releases the underlying database connection.
func (s *Service) Close() error { return s.db.Close() }

// StartPolling launches background goroutines that refresh chain status
// every interval. Call in a goroutine; stops when ctx is canceled.
func (s *Service) StartPolling(ctx context.Context, interval time.Duration) {
	if interval <= 0 {
		interval = 60 * time.Second
	}

	// Initial poll.
	s.pollAll(ctx)

	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.pollAll(ctx)
		}
	}
}

// pollAll refreshes status for all chains concurrently.
func (s *Service) pollAll(ctx context.Context) {
	s.mu.RLock()
	cfg := s.cfg
	if cfg == nil {
		cfg = &config.Config{}
	}
	vms := append([]config.VM(nil), cfg.VMs...)
	s.mu.RUnlock()

	active := make(map[string]struct{}, len(vms)+8)
	var wg sync.WaitGroup
	for _, vm := range vms {
		vm := vm
		if vm.Name != "" {
			active[vm.Name] = struct{}{}
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			st := status.Poll(ctx, vm.Name, vm.RPC(), vm.REST())
			st.Type = vm.Type
			st.Datacenter = vm.Datacenter
			st.DashboardName = vm.DashboardName
			st.ChainName = vm.ChainName
			st.NetworkType = vm.NetworkType
			st.PingCountry = vm.Ping.Country
			st.PingProvider = vm.Ping.Provider
			// v1.3.0: chain identity + LAN ping + governance participation
			st.ChainID = vm.ChainID
			st.ExplorerBase = vm.Explorer
			st.InternalIP = vm.DisplayLanIP()
			if st.InternalIP != "" {
				st.LanPingMs = status.PingLanIP(ctx, st.InternalIP)
			}
			if vm.Valoper != "" {
				st.HasValidator = true
				st.ValParticipation = status.PollValParticipation(ctx, vm.REST(), vm.Valoper)
				vs := status.PollValidatorStatus(ctx, vm.REST(), vm.Valoper)
				st.ValBonded = vs.Bonded
				st.ValJailed = vs.Jailed
				st.ValMissedBlocks = vs.MissedBlocks
			}
			s.mu.Lock()
			s.statuses[vm.Name] = st
			s.mu.Unlock()
		}()
	}

	// Also poll registered (external) chains.
	registered, err := s.db.ListRegisteredChains()
	if err != nil {
		log.Printf("[fleet] list registered chains: %v", err)
	} else {
		for _, rc := range registered {
			// Skip if a VM already covers this chain (exact name or base-slug match).
			// e.g. registered "cheqd-testnet" is skipped when VM "cheqd" (ChainName="cheqd") exists.
			if cfg.FindVMForChain(rc.Chain) != nil {
				continue
			}
			rc := rc
			if rc.Chain != "" {
				active[rc.Chain] = struct{}{}
			}
			wg.Add(1)
			go func() {
				defer wg.Done()
				st := status.Poll(ctx, rc.Chain, rc.RPCURL, rc.RESTURL)
				st.Type = "external"
				s.mu.Lock()
				s.statuses[rc.Chain] = st
				s.mu.Unlock()
			}()
		}
	}
	wg.Wait()

	// Prune stale entries so removed/unregistered chains disappear after apply/poll.
	s.mu.Lock()
	for key := range s.statuses {
		if _, ok := active[key]; !ok {
			delete(s.statuses, key)
		}
	}
	s.mu.Unlock()
}

// Poll triggers an immediate concurrent poll of all chains and blocks until complete.
func (s *Service) Poll(ctx context.Context) { s.pollAll(ctx) }

// Status returns the last polled status for chain, or nil if never polled.
func (s *Service) Status(chain string) *status.ChainStatus {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.statuses[chain]
}

// RemoveStatus removes a chain from the in-memory status map.
// Call after removing a registered chain from the DB so it disappears immediately.
func (s *Service) RemoveStatus(chain string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.statuses, chain)
}

// AllStatuses returns a snapshot of all polled chain statuses.
func (s *Service) AllStatuses() []*status.ChainStatus {
	s.mu.RLock()
	defer s.mu.RUnlock()

	out := make([]*status.ChainStatus, 0, len(s.statuses))
	for _, st := range s.statuses {
		out = append(out, st)
	}
	return out
}

// VMs returns the loaded VM registry.
func (s *Service) VMs() []config.VM {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return append([]config.VM(nil), s.cfg.VMs...)
}

// Hosts returns all registered physical hosts from vms.toml and config/infra/*.toml.
func (s *Service) Hosts() []config.Host {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return append([]config.Host(nil), s.cfg.Hosts...)
}

// Config exposes the full fleet configuration.
func (s *Service) Config() *config.Config {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := &config.Config{
		Hosts: append([]config.Host(nil), s.cfg.Hosts...),
		VMs:   append([]config.VM(nil), s.cfg.VMs...),
	}
	return out
}

// DB exposes the state database for use by API handlers.
func (s *Service) DB() *state.DB { return s.db }

// Runner exposes the remote runner for use by API handlers.
func (s *Service) Runner() *runner.Runner { return s.runner }

// FindVM looks up a VM by name.
func (s *Service) FindVM(name string) *config.VM {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for i := range s.cfg.VMs {
		if s.cfg.VMs[i].Name == name {
			vm := s.cfg.VMs[i]
			return &vm
		}
	}
	return nil
}

// BestVM returns the healthiest VM registered for chain.
// Selection criteria (in priority order):
//  1. Status == "synced" (not catching up)
//  2. Highest block height (most up-to-date)
//  3. First match (fallback)
func (s *Service) BestVM(chain string) *config.VM {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var best config.VM
	hasBest := false
	var bestHeight int64

	for i := range s.cfg.VMs {
		vm := s.cfg.VMs[i]
		if vm.Name != chain {
			continue
		}
		st := s.statuses[chain]
		if !hasBest {
			best = vm
			hasBest = true
			if st != nil {
				bestHeight = st.Height
			}
			continue
		}
		if st != nil && st.NodeStatus == "synced" && st.Height > bestHeight {
			best = vm
			bestHeight = st.Height
		}
	}
	if !hasBest {
		return nil
	}
	out := best
	return &out
}

// RegisterVM adds or updates a VM entry in the in-memory config.
// Callers are responsible for persisting vms.toml if desired.
func (s *Service) RegisterVM(vm config.VM) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i, existing := range s.cfg.VMs {
		if existing.Name == vm.Name {
			s.cfg.VMs[i] = vm
			return
		}
	}
	s.cfg.VMs = append(s.cfg.VMs, vm)
}
