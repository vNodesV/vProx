// Package upgrade queries Cosmos SDK governance for software upgrade proposals.
// It supports both gov v1 (cosmos-sdk ≥ v0.46) and gov v1beta1 endpoints,
// trying v1 first and falling back transparently.
package upgrade

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"
)

const queryTimeout = 10 * time.Second

var httpClient = &http.Client{Timeout: queryTimeout}

// Proposal holds the fields relevant to a software upgrade proposal.
type Proposal struct {
	ID          uint64    `json:"id"`
	Title       string    `json:"title"`
	Status      string    `json:"status"`
	UpgradeName string    `json:"upgrade_name,omitempty"`
	UpgradePlan string    `json:"upgrade_plan,omitempty"` // raw JSON of plan
	Height      int64     `json:"height,omitempty"`
	BinaryURL   string    `json:"binary_url,omitempty"`
	VotingEnd   time.Time `json:"voting_end,omitempty"`
}

// FetchProposal retrieves a single proposal by ID from a Cosmos REST endpoint.
// restURL should be the base URL (e.g. "https://rest.akash.network").
func FetchProposal(ctx context.Context, restURL string, propID uint64) (*Proposal, error) {
	ctx, cancel := context.WithTimeout(ctx, queryTimeout)
	defer cancel()

	// Try gov v1 first (cosmos-sdk ≥ v0.46)
	p, err := fetchV1(ctx, restURL, propID)
	if err == nil {
		return p, nil
	}
	// Fallback to v1beta1
	return fetchV1beta1(ctx, restURL, propID)
}

// FetchActiveProposals returns all proposals currently in VOTING_PERIOD.
func FetchActiveProposals(ctx context.Context, restURL string) ([]Proposal, error) {
	ctx, cancel := context.WithTimeout(ctx, queryTimeout)
	defer cancel()

	ps, err := fetchActiveV1(ctx, restURL)
	if err == nil {
		return ps, nil
	}
	return fetchActiveV1beta1(ctx, restURL)
}

// ── gov v1 ──────────────────────────────────────────────────────────────────

func fetchV1(ctx context.Context, base string, id uint64) (*Proposal, error) {
	url := fmt.Sprintf("%s/cosmos/gov/v1/proposals/%d", strings.TrimRight(base, "/"), id)
	var wrapper struct {
		Proposal struct {
			ID       string `json:"id"`
			Title    string `json:"title"`
			Status   string `json:"status"`
			Messages []struct {
				Type    string `json:"@type"`
				Content struct {
					Type string `json:"@type"`
					Plan struct {
						Name   string `json:"name"`
						Height string `json:"height"`
						Info   string `json:"info"`
					} `json:"plan"`
				} `json:"content"`
				// gov v1 embeds plan directly in message
				Plan *struct {
					Name   string `json:"name"`
					Height string `json:"height"`
					Info   string `json:"info"`
				} `json:"plan"`
			} `json:"messages"`
			VotingEndTime string `json:"voting_end_time"`
		} `json:"proposal"`
	}
	if err := getJSON(ctx, url, &wrapper); err != nil {
		return nil, err
	}
	p := &Proposal{
		Title:  wrapper.Proposal.Title,
		Status: wrapper.Proposal.Status,
	}
	if n, err := strconv.ParseUint(wrapper.Proposal.ID, 10, 64); err == nil {
		p.ID = n
	}
	if t, err := time.Parse(time.RFC3339Nano, wrapper.Proposal.VotingEndTime); err == nil {
		p.VotingEnd = t
	}
	for _, msg := range wrapper.Proposal.Messages {
		plan := msg.Plan
		if plan == nil {
			plan = &msg.Content.Plan
		}
		if plan != nil && plan.Name != "" {
			p.UpgradeName = plan.Name
			if h, err := strconv.ParseInt(plan.Height, 10, 64); err == nil {
				p.Height = h
			}
			p.BinaryURL = extractBinaryURL(plan.Info)
			break
		}
	}
	return p, nil
}

func fetchActiveV1(ctx context.Context, base string) ([]Proposal, error) {
	url := fmt.Sprintf("%s/cosmos/gov/v1/proposals?proposal_status=PROPOSAL_STATUS_VOTING_PERIOD&pagination.limit=50",
		strings.TrimRight(base, "/"))
	var wrapper struct {
		Proposals []json.RawMessage `json:"proposals"`
	}
	if err := getJSON(ctx, url, &wrapper); err != nil {
		return nil, err
	}
	var out []Proposal
	for _, raw := range wrapper.Proposals {
		var v struct {
			ID    string `json:"id"`
			Title string `json:"title"`
		}
		if err := json.Unmarshal(raw, &v); err != nil {
			continue
		}
		id, _ := strconv.ParseUint(v.ID, 10, 64)
		p, err := fetchV1(ctx, base, id)
		if err != nil {
			p = &Proposal{ID: id, Title: v.Title, Status: "PROPOSAL_STATUS_VOTING_PERIOD"}
		}
		out = append(out, *p)
	}
	return out, nil
}

// ── gov v1beta1 ──────────────────────────────────────────────────────────────

func fetchV1beta1(ctx context.Context, base string, id uint64) (*Proposal, error) {
	url := fmt.Sprintf("%s/cosmos/gov/v1beta1/proposals/%d", strings.TrimRight(base, "/"), id)
	var wrapper struct {
		Proposal struct {
			ProposalID string `json:"proposal_id"`
			Status     string `json:"status"`
			Content    struct {
				Type  string `json:"@type"`
				Title string `json:"title"`
				Plan  struct {
					Name   string `json:"name"`
					Height string `json:"height"`
					Info   string `json:"info"`
				} `json:"plan"`
			} `json:"content"`
			VotingEndTime string `json:"voting_end_time"`
		} `json:"proposal"`
	}
	if err := getJSON(ctx, url, &wrapper); err != nil {
		return nil, err
	}
	p := &Proposal{
		Title:  wrapper.Proposal.Content.Title,
		Status: wrapper.Proposal.Status,
	}
	if n, err := strconv.ParseUint(wrapper.Proposal.ProposalID, 10, 64); err == nil {
		p.ID = n
	}
	if t, err := time.Parse(time.RFC3339Nano, wrapper.Proposal.VotingEndTime); err == nil {
		p.VotingEnd = t
	}
	plan := wrapper.Proposal.Content.Plan
	if plan.Name != "" {
		p.UpgradeName = plan.Name
		if h, err := strconv.ParseInt(plan.Height, 10, 64); err == nil {
			p.Height = h
		}
		p.BinaryURL = extractBinaryURL(plan.Info)
	}
	return p, nil
}

func fetchActiveV1beta1(ctx context.Context, base string) ([]Proposal, error) {
	url := fmt.Sprintf("%s/cosmos/gov/v1beta1/proposals?proposal_status=PROPOSAL_STATUS_VOTING_PERIOD&pagination.limit=50",
		strings.TrimRight(base, "/"))
	var wrapper struct {
		Proposals []struct {
			ProposalID string `json:"proposal_id"`
			Status     string `json:"status"`
			Content    struct {
				Title string `json:"title"`
			} `json:"content"`
		} `json:"proposals"`
	}
	if err := getJSON(ctx, url, &wrapper); err != nil {
		return nil, err
	}
	var out []Proposal
	for _, v := range wrapper.Proposals {
		id, _ := strconv.ParseUint(v.ProposalID, 10, 64)
		p, err := fetchV1beta1(ctx, base, id)
		if err != nil {
			p = &Proposal{ID: id, Title: v.Content.Title, Status: v.Status}
		}
		out = append(out, *p)
	}
	return out, nil
}

// ── helpers ──────────────────────────────────────────────────────────────────

func getJSON(ctx context.Context, url string, dst any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	resp, err := httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("HTTP %d for %s", resp.StatusCode, url)
	}
	return json.NewDecoder(resp.Body).Decode(dst)
}

// extractBinaryURL tries to parse cosmos upgrade-info JSON embedded in plan.Info.
// Format: {"binaries":{"linux/amd64":"https://...","linux/arm64":"..."}}
// Returns the amd64 URL if present, otherwise arm64, otherwise empty.
func extractBinaryURL(info string) string {
	if info == "" {
		return ""
	}
	var v struct {
		Binaries map[string]string `json:"binaries"`
	}
	if err := json.Unmarshal([]byte(info), &v); err != nil {
		return ""
	}
	if u, ok := v.Binaries["linux/amd64"]; ok {
		return u
	}
	if u, ok := v.Binaries["linux/arm64"]; ok {
		return u
	}
	return ""
}
