package intel

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
)

// VTResult represents the last_analysis_stats from VirusTotal v3.
type VTResult struct {
	Malicious  int `json:"malicious"`
	Suspicious int `json:"suspicious"`
	Harmless   int `json:"harmless"`
	Undetected int `json:"undetected"`
}

// vtIPURL is the VirusTotal v3 IP address endpoint.
const vtIPURL = "https://www.virustotal.com/api/v3/ip_addresses/"

// CheckVirusTotal queries VirusTotal v3 /api/v3/ip_addresses/{ip}.
// Returns (malicious detections count, raw JSON response body, error).
// Returns (0, "", nil) if apiKey is empty — no-op, not an error.
func CheckVirusTotal(apiKey, ip string, httpClient *http.Client) (malicious int64, rawJSON string, err error) {
	if apiKey == "" {
		return 0, "", nil
	}
	if httpClient == nil {
		httpClient = http.DefaultClient
	}

	req, err := http.NewRequest(http.MethodGet, vtIPURL+ip, nil)
	if err != nil {
		return 0, "", fmt.Errorf("virustotal: build request: %w", err)
	}
	req.Header.Set("x-apikey", apiKey)
	req.Header.Set("Accept", "application/json")

	resp, err := httpClient.Do(req)
	if err != nil {
		return 0, "", fmt.Errorf("virustotal: request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return 0, "", fmt.Errorf("virustotal: read body: %w", err)
	}
	rawJSON = string(body)

	if resp.StatusCode != http.StatusOK {
		return 0, rawJSON, fmt.Errorf("virustotal: HTTP %d: %s", resp.StatusCode, truncate(rawJSON, 200))
	}

	var envelope struct {
		Data struct {
			Attributes struct {
				LastAnalysisStats VTResult `json:"last_analysis_stats"`
			} `json:"attributes"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &envelope); err != nil {
		return 0, rawJSON, fmt.Errorf("virustotal: parse response: %w", err)
	}

	return int64(envelope.Data.Attributes.LastAnalysisStats.Malicious), rawJSON, nil
}
