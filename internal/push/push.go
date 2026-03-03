// Package push is the vProx validator deployment control plane.
//
// It provides:
//   - SSH-based deployment of chain scripts to remote VMs
//   - Cosmos node status polling (height, governance, upgrade plan)
//   - SQLite-backed deployment history and registered-chain registry
//   - HTTP API handlers wired into the vLog web server
package push

import (
	"context"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/vNodesV/vProx/internal/push/config"
	"github.com/vNodesV/vProx/internal/push/runner"
	"github.com/vNodesV/vProx/internal/push/state"
	"github.com/vNodesV/vProx/internal/push/status"
)

// Service is the top-level push control plane.
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
		return nil, fmt.Errorf("push: load config: %w", err)
	}

	db, err := state.Open(dbPath)
	if err != nil {
		return nil, fmt.Errorf("push: open state db: %w", err)
	}

	return &Service{
		cfg:      cfg,
		db:       db,
		runner:   runner.New(),
		statuses: make(map[string]*status.ChainStatus),
	}, nil
}

// Close releases the underlying database connection.
func (s *Service) Close() error { return s.db.Close() }

// StartPolling launches background goroutines that refresh chain status
// every interval. Call in a goroutine; stops when ctx is cancelled.
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

// pollAll refreshes status for all chains across all VMs.
func (s *Service) pollAll(ctx context.Context) {
	for _, vm := range s.cfg.VMs {
		for _, ch := range vm.Chains {
			st := status.Poll(ctx, ch.Name, ch.RPCURL, ch.RESTURL)
			s.mu.Lock()
			s.statuses[ch.Name] = st
			s.mu.Unlock()
		}
	}

	// Also poll registered (external) chains.
	registered, err := s.db.ListRegisteredChains()
	if err != nil {
		log.Printf("[push] list registered chains: %v", err)
		return
	}
	for _, rc := range registered {
		st := status.Poll(ctx, rc.Chain, rc.RPCURL, rc.RESTURL)
		s.mu.Lock()
		s.statuses[rc.Chain] = st
		s.mu.Unlock()
	}
}

// Status returns the last polled status for chain, or nil if never polled.
func (s *Service) Status(chain string) *status.ChainStatus {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.statuses[chain]
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
func (s *Service) VMs() []config.VM { return s.cfg.VMs }

// DB exposes the state database for use by API handlers.
func (s *Service) DB() *state.DB { return s.db }

// Runner exposes the remote runner for use by API handlers.
func (s *Service) Runner() *runner.Runner { return s.runner }

// FindVM looks up a VM by name.
func (s *Service) FindVM(name string) *config.VM { return s.cfg.FindVM(name) }
