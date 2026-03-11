package status

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// ValidatorOnChain holds bonded/jailed/missed-blocks status for a single validator.
type ValidatorOnChain struct {
	Bonded       bool
	Jailed       bool
	MissedBlocks int64
}

// PollValidatorStatus fetches on-chain validator state from the Cosmos REST API.
//
// Flow:
//  1. GET /cosmos/staking/v1beta1/validators/{valoper} → bonded status + jailed + consensus pubkey
//  2. Derive bech32 consensus address from ed25519 pubkey (SHA256[:20] → bech32 with {chain}valcons prefix)
//  3. GET /cosmos/slashing/v1beta1/signing_infos/{cons_addr} → missed_blocks_counter
//
// Returns zero-value ValidatorOnChain on any network error; callers treat 0 as "unavailable".
func PollValidatorStatus(ctx context.Context, restURL, valoper string) ValidatorOnChain {
	if restURL == "" || valoper == "" {
		return ValidatorOnChain{}
	}

	var vs ValidatorOnChain

	// Step 1: staking info.
	url := restURL + "/cosmos/staking/v1beta1/validators/" + valoper
	req, err := httpNewGET(ctx, url)
	if err != nil {
		return vs
	}
	resp, err := httpClient.Do(req)
	if err != nil || resp.StatusCode != 200 {
		if resp != nil {
			resp.Body.Close()
		}
		return vs
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	resp.Body.Close()
	if err != nil {
		return vs
	}

	var stakingResp struct {
		Validator struct {
			Status  string `json:"status"`
			Jailed  bool   `json:"jailed"`
			ConsPub struct {
				Key string `json:"key"` // base64 ed25519 pubkey bytes
			} `json:"consensus_pubkey"`
		} `json:"validator"`
	}
	if err := json.Unmarshal(body, &stakingResp); err != nil {
		return vs
	}

	vs.Bonded = stakingResp.Validator.Status == "BOND_STATUS_BONDED"
	vs.Jailed = stakingResp.Validator.Jailed

	// Step 2: derive consensus address.
	consAddr, err := deriveConsAddress(valoper, stakingResp.Validator.ConsPub.Key)
	if err != nil {
		return vs
	}

	// Step 3: slashing signing info → missed_blocks_counter.
	url2 := restURL + "/cosmos/slashing/v1beta1/signing_infos/" + consAddr
	req2, err := httpNewGET(ctx, url2)
	if err != nil {
		return vs
	}
	resp2, err := httpClient.Do(req2)
	if err != nil || resp2.StatusCode != 200 {
		if resp2 != nil {
			resp2.Body.Close()
		}
		return vs
	}
	body2, err := io.ReadAll(io.LimitReader(resp2.Body, 1<<20))
	resp2.Body.Close()
	if err != nil {
		return vs
	}

	var slashResp struct {
		ValSigningInfo struct {
			MissedBlocksCounter string `json:"missed_blocks_counter"`
		} `json:"val_signing_info"`
	}
	if err := json.Unmarshal(body2, &slashResp); err == nil {
		fmt.Sscanf(slashResp.ValSigningInfo.MissedBlocksCounter, "%d", &vs.MissedBlocks) //nolint:errcheck
	}

	return vs
}

// deriveConsAddress converts a valoper bech32 address + base64 ed25519 pubkey into the
// corresponding bech32 consensus address.
//
//   - Chain prefix: extracted from valoper by stripping "valoper1..." suffix.
//     e.g. "cheqdvaloper1abc…" → chain hrp = "cheqd" → cons hrp = "cheqdvalcons"
//   - Consensus address bytes = SHA256(pubkeyBytes)[:20]  (Cosmos SDK / CometBFT convention)
func deriveConsAddress(valoper, pubkeyBase64 string) (string, error) {
	if pubkeyBase64 == "" {
		return "", fmt.Errorf("empty pubkey")
	}

	idx := strings.Index(valoper, "valoper1")
	if idx < 0 {
		return "", fmt.Errorf("cannot extract chain prefix from valoper %q", valoper)
	}
	chainHRP := valoper[:idx]
	consHRP := chainHRP + "valcons"

	pubkeyBytes, err := base64.StdEncoding.DecodeString(pubkeyBase64)
	if err != nil {
		return "", fmt.Errorf("decode pubkey: %w", err)
	}

	h := sha256.Sum256(pubkeyBytes)
	return bech32Encode(consHRP, h[:20])
}

// httpNewGET builds a GET *http.Request with context.
func httpNewGET(ctx context.Context, url string) (*http.Request, error) {
	return http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
}
