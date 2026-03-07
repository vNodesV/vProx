// Package status polls Cosmos SDK node endpoints for chain health data.
// It queries CometBFT RPC /status, Cosmos REST /cosmos/gov/v1beta1/proposals,
// and /cosmos/upgrade/v1beta1/current_plan with a shared HTTP client.
package status

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

const pollTimeout = 8 * time.Second

var httpClient = &http.Client{Timeout: pollTimeout}

// ChainStatus holds all polled data for one chain.
type ChainStatus struct {
	Chain   string `json:"chain"`
	Type    string `json:"type"`              // validator | sp | relayer | external
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
	ActiveProposals   int      `json:"active_proposals"`
	ActiveProposalIDs []string `json:"active_proposal_ids,omitempty"`
	VotingEndTime     string   `json:"voting_end_time,omitempty"` // earliest UTC among active proposals

	// Upgrade plan
	UpgradePending    bool   `json:"upgrade_pending"`
	UpgradeName       string `json:"upgrade_name,omitempty"`
	UpgradeHeight     int64  `json:"upgrade_height,omitempty"`
	UpgradeEstUTC     string `json:"upgrade_est_utc,omitempty"`
	UpgradeProposalID string `json:"upgrade_proposal_id,omitempty"`

	// Chain identity
	ChainID     string `json:"chain_id,omitempty"`     // official chain-id from config
	ExplorerBase string `json:"explorer_url,omitempty"` // block explorer base URL for dashboard links

	// LAN ping (vProx → node direct)
	LanPingMs int64 `json:"lan_ping_ms"` // round-trip ms; -1=unreachable, 0=not configured

	// Validator governance participation
	ValParticipation string `json:"val_participation,omitempty"` // "12/15"; empty if not a validator or no valoper

	// Metadata
	Datacenter   string    `json:"datacenter,omitempty"`
	PingCountry  string    `json:"ping_country,omitempty"`
	PingProvider string    `json:"ping_provider,omitempty"`
	UpdatedAt    time.Time `json:"updated_at"`
	Error        string    `json:"error,omitempty"`
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
		if s.UpgradePending {
			pollGovPassedUpgrade(ctx, s)
		}
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
	fmt.Sscanf(si.LatestBlockHeight, "%d", &h)   //nolint:errcheck
	fmt.Sscanf(si.EarliestBlockHeight, "%d", &eh) //nolint:errcheck

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


// pollGov fetches active (voting period) governance proposals.
// Tries the v1 API first (Cosmos SDK 0.47+/0.50+ with CometBFT), falls back to v1beta1.
// Uses proposal_status=2 (numeric) for broadest gateway compatibility.
func pollGov(ctx context.Context, s *ChainStatus) {
	endpoints := []struct {
		url string
		v1  bool
	}{
		{s.RESTURL + "/cosmos/gov/v1/proposals?proposal_status=2", true},
		{s.RESTURL + "/cosmos/gov/v1beta1/proposals?proposal_status=2", false},
	}
	for _, ep := range endpoints {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, ep.url, nil)
		if err != nil {
			continue
		}
		resp, err := httpClient.Do(req)
		if err != nil {
			continue
		}
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
		resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			continue
		}
		var r passedPropResponse
		if err := json.Unmarshal(body, &r); err != nil || len(r.Proposals) == 0 && ep.v1 {
			// v1 returned 200 with 0 proposals → could be empty chain, accept it.
			if err != nil {
				continue
			}
		}
		s.ActiveProposals = len(r.Proposals)
		ids := make([]string, 0, len(r.Proposals))
		var earliest time.Time
		for _, p := range r.Proposals {
			id := p.ID
			if id == "" {
				id = p.ProposalID
			}
			if id != "" {
				ids = append(ids, id)
			}
			if p.VotingEndTime != "" {
				t, err2 := time.Parse(time.RFC3339Nano, p.VotingEndTime)
				if err2 != nil {
					t, err2 = time.Parse(time.RFC3339, p.VotingEndTime)
				}
				if err2 == nil && (earliest.IsZero() || t.Before(earliest)) {
					earliest = t
				}
			}
		}
		s.ActiveProposalIDs = ids
		if !earliest.IsZero() {
			s.VotingEndTime = earliest.UTC().Format(time.RFC3339)
		}
		return // success
	}
}

// passedPropResponse handles both v1beta1 and v1 governance proposal shapes.
type passedPropResponse struct {
	Proposals []struct {
		ProposalID    string `json:"proposal_id"` // v1beta1
		ID            string `json:"id"`          // v1
		VotingEndTime string `json:"voting_end_time"`
		Content       struct {
			Type string `json:"@type"`
			Plan struct {
				Name string `json:"name"`
			} `json:"plan"`
		} `json:"content"` // v1beta1
		Messages []struct {
			Type string `json:"@type"`
			Plan struct {
				Name string `json:"name"`
			} `json:"plan"`
		} `json:"messages"` // v1
	} `json:"proposals"`
}

// pollGovPassedUpgrade tries to find the governance proposal ID for the
// current upgrade plan by searching passed proposals. Best-effort; silently
// skips on any error or no match.
func pollGovPassedUpgrade(ctx context.Context, s *ChainStatus) {
	for _, endpoint := range []string{
		s.RESTURL + "/cosmos/gov/v1beta1/proposals?proposal_status=3",
		s.RESTURL + "/cosmos/gov/v1/proposals?proposal_status=3",
	} {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
		if err != nil {
			continue
		}
		resp, err := httpClient.Do(req)
		if err != nil || resp.StatusCode >= 400 {
			if resp != nil {
				resp.Body.Close()
			}
			continue
		}
		var r passedPropResponse
		if err := json.NewDecoder(resp.Body).Decode(&r); err != nil {
			resp.Body.Close()
			continue
		}
		resp.Body.Close()
		for _, p := range r.Proposals {
			// v1beta1 shape
			if p.Content.Plan.Name == s.UpgradeName &&
				(p.Content.Type == "/cosmos.upgrade.v1beta1.SoftwareUpgradeProposal" ||
					p.Content.Type == "cosmos.upgrade.v1beta1.SoftwareUpgradeProposal") {
				id := p.ProposalID
				if id == "" {
					id = p.ID
				}
				if id != "" {
					s.UpgradeProposalID = id
					return
				}
			}
			// v1 shape
			for _, msg := range p.Messages {
				if msg.Plan.Name == s.UpgradeName &&
					(msg.Type == "/cosmos.upgrade.v1beta1.MsgSoftwareUpgrade" ||
						msg.Type == "cosmos.upgrade.v1beta1.MsgSoftwareUpgrade") {
					id := p.ID
					if id == "" {
						id = p.ProposalID
					}
					if id != "" {
						s.UpgradeProposalID = id
						return
					}
				}
			}
		}
	}
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
	fmt.Sscanf(r.Plan.Height, "%d", &upgradeHeight) //nolint:errcheck
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

// ---- LAN ping ----

// PingLanIP measures the round-trip time (ms) of a GET /health request to the
// node's CometBFT RPC port on the LAN (zero-cost endpoint). Returns -1 when
// unreachable, 0 when lanIP is empty (not configured).
func PingLanIP(ctx context.Context, lanIP string) int64 {
	if lanIP == "" {
		return 0
	}
	url := "http://" + lanIP + ":26657/health"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return -1
	}
	start := time.Now()
	resp, err := httpClient.Do(req)
	if err != nil {
		return -1
	}
	_, _ = io.Copy(io.Discard, resp.Body)
	resp.Body.Close()
	if ms := time.Since(start).Milliseconds(); ms > 0 {
		return ms
	}
	return 1 // sub-ms: report 1 ms rather than 0 (0 = unconfigured)
}

// ---- Governance participation ----

// paginationTotal is a minimal struct for pagination responses that carry a
// count_total value.
type paginationTotal struct {
	Pagination struct {
		Total string `json:"total"`
	} `json:"pagination"`
}

// countGovProposals fetches the total proposal count from the Cosmos REST
// governance endpoint. When voter is non-empty, the query is filtered to
// proposals the address has voted on.  Returns -1 on any error.
func countGovProposals(ctx context.Context, restURL, voter string) int64 {
	endpoint := restURL + "/cosmos/gov/v1/proposals?pagination.count_total=true&pagination.limit=1"
	if voter != "" {
		endpoint += "&voter=" + voter
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return -1
	}
	resp, err := httpClient.Do(req)
	if err != nil {
		return -1
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return -1
	}
	var r paginationTotal
	if err := json.NewDecoder(io.LimitReader(resp.Body, 64*1024)).Decode(&r); err != nil {
		return -1
	}
	var n int64
	fmt.Sscanf(r.Pagination.Total, "%d", &n) //nolint:errcheck
	return n
}

// PollValParticipation returns a "voted/total" string indicating how many
// governance proposals the given valoper address has voted on out of the total.
// Returns empty string when not configured or on error.
func PollValParticipation(ctx context.Context, restURL, valoper string) string {
	if restURL == "" || valoper == "" {
		return ""
	}
	total := countGovProposals(ctx, restURL, "")
	if total < 0 {
		return ""
	}
	voted := countGovProposals(ctx, restURL, valoper)
	if voted < 0 {
		voted = 0
	}
	return fmt.Sprintf("%d/%d", voted, total)
}
