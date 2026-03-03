// Package status polls Cosmos SDK node endpoints for chain health data.
// It queries CometBFT RPC /status, Cosmos REST /cosmos/gov/v1beta1/proposals,
// and /cosmos/upgrade/v1beta1/current_plan with a shared HTTP client.
package status

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

const pollTimeout = 8 * time.Second

var httpClient = &http.Client{Timeout: pollTimeout}

// ChainStatus holds all polled data for one chain.
type ChainStatus struct {
	Chain   string `json:"chain"`
	RPCURL  string `json:"rpc_url"`
	RESTURL string `json:"rest_url,omitempty"`

	// CometBFT /status
	Moniker         string    `json:"moniker"`
	Height          int64     `json:"height"`
	EarliestHeight  int64     `json:"earliest_height"`
	CatchingUp      bool      `json:"catching_up"`
	LatestBlockTime time.Time `json:"latest_block_time"`
	AvgBlockSec     float64   `json:"avg_block_sec,omitempty"`
	NodeStatus      string    `json:"node_status"` // synced|syncing|down

	// Governance
	ActiveProposals int `json:"active_proposals"`

	// Upgrade plan
	UpgradePending bool   `json:"upgrade_pending"`
	UpgradeName    string `json:"upgrade_name,omitempty"`
	UpgradeHeight  int64  `json:"upgrade_height,omitempty"`
	UpgradeEstUTC  string `json:"upgrade_est_utc,omitempty"`

	// Metadata
	UpdatedAt time.Time `json:"updated_at"`
	Error     string    `json:"error,omitempty"`
}

// Poll fetches full chain status from rpcURL and restURL.
// restURL may be empty — governance and upgrade data will be skipped.
func Poll(ctx context.Context, chain, rpcURL, restURL string) *ChainStatus {
	s := &ChainStatus{
		Chain:     chain,
		RPCURL:    rpcURL,
		RESTURL:   restURL,
		UpdatedAt: time.Now().UTC(),
	}

	if err := pollRPC(ctx, s); err != nil {
		s.NodeStatus = "down"
		s.Error = err.Error()
		return s
	}

	if restURL != "" {
		pollGov(ctx, s)
		pollUpgrade(ctx, s)
	}

	return s
}

// ---- CometBFT RPC /status ----

type rpcStatusResponse struct {
	Result struct {
		NodeInfo struct {
			Moniker string `json:"moniker"`
		} `json:"node_info"`
		SyncInfo struct {
			LatestBlockHeight   string    `json:"latest_block_height"`
			EarliestBlockHeight string    `json:"earliest_block_height"`
			LatestBlockTime     time.Time `json:"latest_block_time"`
			CatchingUp          bool      `json:"catching_up"`
		} `json:"sync_info"`
	} `json:"result"`
}

func pollRPC(ctx context.Context, s *ChainStatus) error {
	if s.RPCURL == "" {
		return fmt.Errorf("rpc_url is empty")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, s.RPCURL+"/status", nil)
	if err != nil {
		return fmt.Errorf("rpc /status request: %w", err)
	}
	resp, err := httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("rpc /status: %w", err)
	}
	defer resp.Body.Close()

	var r rpcStatusResponse
	if err := json.NewDecoder(resp.Body).Decode(&r); err != nil {
		return fmt.Errorf("rpc /status decode: %w", err)
	}

	si := r.Result.SyncInfo
	var h, eh int64
	fmt.Sscanf(si.LatestBlockHeight, "%d", &h)
	fmt.Sscanf(si.EarliestBlockHeight, "%d", &h)
	fmt.Sscanf(si.EarliestBlockHeight, "%d", &eh)

	s.Moniker = r.Result.NodeInfo.Moniker
	s.Height = h
	s.EarliestHeight = eh
	s.CatchingUp = si.CatchingUp
	s.LatestBlockTime = si.LatestBlockTime

	if si.CatchingUp {
		s.NodeStatus = "syncing"
	} else {
		s.NodeStatus = "synced"
	}

	// Estimate avg block time from earliest→latest span.
	blockSpan := h - eh
	if blockSpan > 0 && !si.LatestBlockTime.IsZero() {
		age := time.Since(si.LatestBlockTime).Seconds()
		if age > 0 {
			s.AvgBlockSec = (age + float64(blockSpan)*6) / float64(blockSpan+1)
		}
	}

	return nil
}

// ---- Cosmos REST: governance ----

type govProposalsResponse struct {
	Proposals []struct {
		ID string `json:"proposal_id"`
	} `json:"proposals"`
}

func pollGov(ctx context.Context, s *ChainStatus) {
	url := s.RESTURL + "/cosmos/gov/v1beta1/proposals?proposal_status=2"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return
	}
	resp, err := httpClient.Do(req)
	if err != nil {
		return
	}
	defer resp.Body.Close()

	var r govProposalsResponse
	if err := json.NewDecoder(resp.Body).Decode(&r); err != nil {
		return
	}
	s.ActiveProposals = len(r.Proposals)
}

// ---- Cosmos REST: upgrade plan ----

type upgradeResponse struct {
	Plan *struct {
		Name   string `json:"name"`
		Height string `json:"height"`
		Info   string `json:"info"`
	} `json:"plan"`
}

func pollUpgrade(ctx context.Context, s *ChainStatus) {
	url := s.RESTURL + "/cosmos/upgrade/v1beta1/current_plan"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return
	}
	resp, err := httpClient.Do(req)
	if err != nil {
		return
	}
	defer resp.Body.Close()

	var r upgradeResponse
	if err := json.NewDecoder(resp.Body).Decode(&r); err != nil || r.Plan == nil {
		return
	}

	var upgradeHeight int64
	fmt.Sscanf(r.Plan.Height, "%d", &upgradeHeight)
	if upgradeHeight == 0 {
		return
	}

	s.UpgradePending = true
	s.UpgradeName = r.Plan.Name
	s.UpgradeHeight = upgradeHeight

	// Estimate upgrade time from current height + avg block time.
	if s.AvgBlockSec > 0 && s.Height > 0 {
		blocksLeft := upgradeHeight - s.Height
		if blocksLeft > 0 {
			estDur := time.Duration(float64(blocksLeft)*s.AvgBlockSec) * time.Second
			s.UpgradeEstUTC = time.Now().UTC().Add(estDur).Format(time.RFC3339)
		}
	}
}
