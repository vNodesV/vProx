// Package cosmos provides a lightweight client for the cosmos.directory API.
// It auto-fetches chain metadata (pretty name, network type, recommended version,
// explorers) and caches results for 30 minutes. All fetches are bounded by
// io.LimitReader (512 KB) to prevent unbounded reads from external sources.
package cosmos

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"sync"
	"time"
)

const (
	baseURL      = "https://chains.cosmos.directory"
	maxBodyBytes = 512 * 1024 // 512 KB
	cacheTTL     = 30 * time.Minute
	httpTimeout  = 8 * time.Second
)

var httpClient = &http.Client{Timeout: httpTimeout}

type cacheEntry struct {
	data *ChainDirectoryEntry
	exp  time.Time
}

var cache sync.Map // map[string]*cacheEntry

// Explorer is a single block explorer entry from cosmos.directory.
type Explorer struct {
	Kind string `json:"kind"`
	URL  string `json:"url"`
}

// ChainDirectoryEntry holds the enrichment data returned from chains.cosmos.directory.
// Fields that could not be fetched are left as zero values.
type ChainDirectoryEntry struct {
	ChainID            string
	PrettyName         string
	NetworkType        string
	RecommendedVersion string
	Explorers          []Explorer
	FetchedAt          time.Time
}

// raw shapes for JSON unmarshalling
type rawExplorer struct {
	Kind string `json:"kind"`
	URL  string `json:"url"`
}

type rawCodebase struct {
	RecommendedVersion string `json:"recommended_version"`
}

type rawChain struct {
	ChainID     string      `json:"chain_id"`
	PrettyName  string      `json:"pretty_name"`
	NetworkType string      `json:"network_type"`
	Codebase    rawCodebase `json:"codebase"`
	Explorers   []rawExplorer `json:"explorers"`
}

type rawResponse struct {
	Chain rawChain `json:"chain"`
}

// Fetch returns cached or live chain metadata from chains.cosmos.directory/{chainName}.
// On network failure, returns stale cache if available. If no cache exists, returns error.
// Errors are non-fatal — callers should log and proceed with local config values.
func Fetch(chainName string) (*ChainDirectoryEntry, error) {
	if v, ok := cache.Load(chainName); ok {
		e := v.(*cacheEntry)
		if time.Now().Before(e.exp) {
			return e.data, nil
		}
	}

	url := fmt.Sprintf("%s/%s", baseURL, chainName)
	resp, err := httpClient.Get(url)
	if err != nil {
		if v, ok := cache.Load(chainName); ok {
			log.Printf("[cosmos] fetch failed for %q, using stale cache: %v", chainName, err)
			return v.(*cacheEntry).data, nil
		}
		return nil, fmt.Errorf("cosmos.directory fetch %q: %w", chainName, err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxBodyBytes))
	if err != nil {
		return nil, fmt.Errorf("cosmos.directory read %q: %w", chainName, err)
	}

	var raw rawResponse
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, fmt.Errorf("cosmos.directory parse %q: %w", chainName, err)
	}

	r := raw.Chain
	entry := &ChainDirectoryEntry{
		ChainID:            r.ChainID,
		PrettyName:         r.PrettyName,
		NetworkType:        r.NetworkType,
		RecommendedVersion: r.Codebase.RecommendedVersion,
		FetchedAt:          time.Now(),
	}
	for _, e := range r.Explorers {
		entry.Explorers = append(entry.Explorers, Explorer{Kind: e.Kind, URL: e.URL})
	}

	cache.Store(chainName, &cacheEntry{data: entry, exp: time.Now().Add(cacheTTL)})
	return entry, nil
}

// Enrich fills display-only fields from cosmos.directory into the provided pointers.
// Only fields that are currently empty/nil are updated — local config always wins.
// Called in a background goroutine from the push config loader; errors are logged only.
func Enrich(chainName string, dashboardName, networkType, recommendedVersion *string, explorers *[]string) {
	entry, err := Fetch(chainName)
	if err != nil {
		log.Printf("[cosmos] enrich %q: %v", chainName, err)
		return
	}
	if *dashboardName == "" && entry.PrettyName != "" {
		*dashboardName = entry.PrettyName
	}
	if *networkType == "" && entry.NetworkType != "" {
		*networkType = entry.NetworkType
	}
	if *recommendedVersion == "" && entry.RecommendedVersion != "" {
		*recommendedVersion = entry.RecommendedVersion
	}
	if len(*explorers) == 0 {
		for _, e := range entry.Explorers {
			*explorers = append(*explorers, e.URL)
		}
	}
}
