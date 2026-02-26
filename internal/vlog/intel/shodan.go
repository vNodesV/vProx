package intel

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
)

// ShodanResult represents the host response from Shodan /shodan/host/{ip}.
type ShodanResult struct {
	IP          string   `json:"ip_str"`
	Org         string   `json:"org"`
	ISP         string   `json:"isp"`
	Country     string   `json:"country_name"`
	CountryCode string   `json:"country_code"`
	ASN         string   `json:"asn"`
	Hostnames   []string `json:"hostnames"`
	Ports       []int    `json:"ports"`
	OS          string   `json:"os"`
	Tags        []string `json:"tags"`
}

// shodanHostURL is the Shodan host endpoint.
const shodanHostURL = "https://api.shodan.io/shodan/host/"

// CheckShodan queries Shodan /shodan/host/{ip}.
// Returns (parsed result, raw JSON response body, error).
// Returns (nil, "", nil) if apiKey is empty — no-op, not an error.
func CheckShodan(apiKey, ip string, httpClient *http.Client) (result *ShodanResult, rawJSON string, err error) {
	if apiKey == "" {
		return nil, "", nil
	}
	if httpClient == nil {
		httpClient = http.DefaultClient
	}

	params := url.Values{}
	params.Set("key", apiKey)

	req, err := http.NewRequest(http.MethodGet, shodanHostURL+ip+"?"+params.Encode(), nil)
	if err != nil {
		return nil, "", fmt.Errorf("shodan: build request: %w", err)
	}
	req.Header.Set("Accept", "application/json")

	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, "", fmt.Errorf("shodan: request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, "", fmt.Errorf("shodan: read body: %w", err)
	}
	rawJSON = string(body)

	if resp.StatusCode != http.StatusOK {
		return nil, rawJSON, fmt.Errorf("shodan: HTTP %d: %s", resp.StatusCode, truncate(rawJSON, 200))
	}

	var sr ShodanResult
	if err := json.Unmarshal(body, &sr); err != nil {
		return nil, rawJSON, fmt.Errorf("shodan: parse response: %w", err)
	}

	return &sr, rawJSON, nil
}
