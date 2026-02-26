package intel

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
)

// AbuseIPDBResult represents the response data from AbuseIPDB v2 /check.
type AbuseIPDBResult struct {
	IPAddress            string `json:"ipAddress"`
	IsPublic             bool   `json:"isPublic"`
	IPVersion            int    `json:"ipVersion"`
	IsWhitelisted        bool   `json:"isWhitelisted"`
	AbuseConfidenceScore int    `json:"abuseConfidenceScore"`
	CountryCode          string `json:"countryCode"`
	ISP                  string `json:"isp"`
	Domain               string `json:"domain"`
	TotalReports         int    `json:"totalReports"`
	NumDistinctUsers     int    `json:"numDistinctUsers"`
	LastReportedAt       string `json:"lastReportedAt"`
}

// abuseIPDBCheckURL is the AbuseIPDB v2 check endpoint.
const abuseIPDBCheckURL = "https://api.abuseipdb.com/api/v2/check"

// CheckAbuseIPDB queries AbuseIPDB v2 /api/v2/check for the given ip.
// Returns (score 0-100, raw JSON response body, error).
// Returns (0, "", nil) if apiKey is empty — no-op, not an error.
// Uses maxAgeInDays=90 for report lookback.
func CheckAbuseIPDB(apiKey, ip string, httpClient *http.Client) (score int64, rawJSON string, err error) {
	if apiKey == "" {
		return 0, "", nil
	}
	if httpClient == nil {
		httpClient = http.DefaultClient
	}

	params := url.Values{}
	params.Set("ipAddress", ip)
	params.Set("maxAgeInDays", "90")

	req, err := http.NewRequest(http.MethodGet, abuseIPDBCheckURL+"?"+params.Encode(), nil)
	if err != nil {
		return 0, "", fmt.Errorf("abuseipdb: build request: %w", err)
	}
	req.Header.Set("Key", apiKey)
	req.Header.Set("Accept", "application/json")

	resp, err := httpClient.Do(req)
	if err != nil {
		return 0, "", fmt.Errorf("abuseipdb: request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return 0, "", fmt.Errorf("abuseipdb: read body: %w", err)
	}
	rawJSON = string(body)

	if resp.StatusCode != http.StatusOK {
		return 0, rawJSON, fmt.Errorf("abuseipdb: HTTP %d: %s", resp.StatusCode, truncate(rawJSON, 200))
	}

	var envelope struct {
		Data AbuseIPDBResult `json:"data"`
	}
	if err := json.Unmarshal(body, &envelope); err != nil {
		return 0, rawJSON, fmt.Errorf("abuseipdb: parse response: %w", err)
	}

	return int64(envelope.Data.AbuseConfidenceScore), rawJSON, nil
}

// truncate shortens s to n characters for error messages.
func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}
