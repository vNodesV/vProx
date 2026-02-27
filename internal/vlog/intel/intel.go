// Package intel provides IP threat intelligence enrichment for vLog.
//
// It orchestrates lookups against AbuseIPDB v2, VirusTotal v3, and Shodan,
// computes a composite threat score (0-100), caches raw API responses in
// SQLite via the db package, and updates IPAccount records with intel data.
package intel

import (
	"context"
	"log"
	"net/http"
	"time"

	"golang.org/x/time/rate"

	"github.com/vNodesV/vProx/internal/vlog/config"
	"github.com/vNodesV/vProx/internal/vlog/db"
)

// intel source identifiers for cache keys.
const (
	sourceAbuseIPDB  = "abuseipdb"
	sourceVirusTotal = "virustotal"
	sourceShodan     = "shodan"
)

// Enricher orchestrates IP threat intelligence lookups.
type Enricher struct {
	cfg        config.IntelConfig
	db         *db.DB
	httpClient *http.Client
	limiter    *rate.Limiter
	queue      chan string
	done       chan struct{}
}

// NewEnricher creates a new Enricher with the given config and database.
// It initialises an HTTP client with a 10s timeout, a rate limiter based on
// RateLimitRPM, and a buffered enrichment queue (capacity 100).
func NewEnricher(cfg config.IntelConfig, d *db.DB) *Enricher {
	rpm := cfg.RateLimitRPM
	if rpm <= 0 {
		rpm = 30 // sensible default
	}

	// Convert RPM to rate.Limiter params: events per second, burst of 1.
	rps := float64(rpm) / 60.0

	return &Enricher{
		cfg:        cfg,
		db:         d,
		httpClient: &http.Client{Timeout: 10 * time.Second},
		limiter:    rate.NewLimiter(rate.Limit(rps), 1),
		queue:      make(chan string, 100),
		done:       make(chan struct{}),
	}
}

// Start launches the background enrichment worker goroutine.
// It reads IPs from the queue and enriches each one synchronously.
// A recover guard restarts the worker if an unexpected panic occurs.
func (e *Enricher) Start() {
	go func() {
		defer func() {
			if r := recover(); r != nil {
				log.Printf("[intel] worker panic (recovered): %v — restarting", r)
				e.Start() // restart the goroutine
			}
		}()
		for {
			select {
			case ip := <-e.queue:
				if _, err := e.EnrichNow(ip); err != nil {
					log.Printf("[intel] enrich %s: %v", ip, err)
				}
			case <-e.done:
				// Drain remaining items.
				for {
					select {
					case ip := <-e.queue:
						if _, err := e.EnrichNow(ip); err != nil {
							log.Printf("[intel] enrich %s: %v", ip, err)
						}
					default:
						return
					}
				}
			}
		}
	}()
}

// Stop signals the enrichment worker to drain and exit.
func (e *Enricher) Stop() {
	close(e.done)
}

// Enqueue adds ip to the enrichment queue. Non-blocking; drops if full.
func (e *Enricher) Enqueue(ip string) {
	select {
	case e.queue <- ip:
	default:
		// Queue full — drop silently to avoid blocking callers.
	}
}

// EnrichNow synchronously enriches a single IP address.
//
// Steps:
//  1. Check intel_cache for each source; skip if fetched_at is within CacheTTLHours.
//  2. Call CheckAbuseIPDB, CheckVirusTotal, CheckShodan (skip if key empty).
//  3. Cache raw JSON for each source via UpsertIntelCache.
//  4. Get existing IPAccount (or create a minimal one).
//  5. Update account fields from API results.
//  6. Compute ThreatScore, set Status via Level, build ThreatFlags.
//  7. Upsert IPAccount and return it.
func (e *Enricher) EnrichNow(ip string) (*db.IPAccount, error) {
	now := time.Now().UTC()
	nowISO := now.Format(time.RFC3339)

	// ---- AbuseIPDB ----
	var abuseScore int64 = -1
	var abuseRaw string
	if e.cfg.Keys.AbuseIPDB != "" && !e.cacheValid(ip, sourceAbuseIPDB, now) {
		_ = e.limiter.Wait(context.Background())
		s, raw, err := CheckAbuseIPDB(e.cfg.Keys.AbuseIPDB, ip, e.httpClient)
		if err != nil {
			log.Printf("[intel] abuseipdb %s: %v", ip, err)
		} else {
			abuseScore = s
			abuseRaw = raw
			if err := e.db.UpsertIntelCache(ip, sourceAbuseIPDB, nowISO, raw); err != nil {
				log.Printf("[intel] cache abuseipdb %s: %v", ip, err)
			}
		}
	}

	// ---- VirusTotal ----
	var vtMalicious int64 = -1
	var vtRaw string
	if e.cfg.Keys.VirusTotal != "" && !e.cacheValid(ip, sourceVirusTotal, now) {
		_ = e.limiter.Wait(context.Background())
		m, raw, err := CheckVirusTotal(e.cfg.Keys.VirusTotal, ip, e.httpClient)
		if err != nil {
			log.Printf("[intel] virustotal %s: %v", ip, err)
		} else {
			vtMalicious = m
			vtRaw = raw
			if err := e.db.UpsertIntelCache(ip, sourceVirusTotal, nowISO, raw); err != nil {
				log.Printf("[intel] cache virustotal %s: %v", ip, err)
			}
		}
	}

	// ---- Shodan ----
	var shodanResult *ShodanResult
	var shodanRaw string
	if e.cfg.Keys.Shodan != "" && !e.cacheValid(ip, sourceShodan, now) {
		_ = e.limiter.Wait(context.Background())
		sr, raw, err := CheckShodan(e.cfg.Keys.Shodan, ip, e.httpClient)
		if err != nil {
			log.Printf("[intel] shodan %s: %v", ip, err)
		} else {
			shodanResult = sr
			shodanRaw = raw
			if err := e.db.UpsertIntelCache(ip, sourceShodan, nowISO, raw); err != nil {
				log.Printf("[intel] cache shodan %s: %v", ip, err)
			}
		}
	}

	// ---- Build / update IPAccount ----
	acc, err := e.db.GetIPAccount(ip)
	if err != nil {
		// Not found — create a minimal account.
		acc = &db.IPAccount{
			IP:        ip,
			FirstSeen: nowISO,
			LastSeen:  nowISO,
			Status:    string(ThreatUnknown),
		}
	}

	// Apply AbuseIPDB data.
	if abuseScore >= 0 {
		acc.AbuseScore = abuseScore
		acc.AbuseData = abuseRaw
	}

	// Apply VirusTotal data.
	if vtMalicious >= 0 {
		acc.VTMalicious = vtMalicious
		acc.VTData = vtRaw
	}

	// Apply Shodan data.
	if shodanResult != nil {
		acc.ShodanData = shodanRaw
		if acc.Org == "" {
			acc.Org = shodanResult.Org
		}
		if acc.Country == "" {
			acc.Country = shodanResult.Country
		}
		if acc.ASN == "" {
			acc.ASN = shodanResult.ASN
		}
		acc.Hostnames = hostnamesToJSON(shodanResult.Hostnames)
		acc.OpenPorts = portsToJSON(shodanResult.Ports)
	}

	// Compute composite threat score.
	shodanFlags := ExtractShodanRiskFlags(acc.ShodanData)
	effectiveAbuse := abuseScore
	if effectiveAbuse < 0 && acc.AbuseScore > 0 {
		effectiveAbuse = acc.AbuseScore
	}
	effectiveVT := vtMalicious
	if effectiveVT < 0 && acc.VTMalicious > 0 {
		effectiveVT = acc.VTMalicious
	}

	acc.ThreatScore = ComputeScore(effectiveAbuse, effectiveVT, shodanFlags)
	acc.Status = string(Level(acc.ThreatScore))
	acc.ThreatFlags = BuildThreatFlags(acc)
	acc.IntelUpdatedAt = nowISO

	if err := e.db.UpsertIPAccount(acc); err != nil {
		return acc, err
	}
	return acc, nil
}

// cacheValid returns true if the cached entry for (ip, source) was fetched
// within the configured CacheTTLHours window relative to now.
func (e *Enricher) cacheValid(ip, source string, now time.Time) bool {
	if e.cfg.CacheTTLHours <= 0 {
		return false
	}
	fetchedAt, _, err := e.db.GetIntelCache(ip, source)
	if err != nil || fetchedAt == "" {
		return false
	}
	t, err := time.Parse(time.RFC3339, fetchedAt)
	if err != nil {
		return false
	}
	return now.Sub(t) < time.Duration(e.cfg.CacheTTLHours)*time.Hour
}
