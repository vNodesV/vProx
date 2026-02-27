// Package intel provides IP threat intelligence enrichment for vLog.
//
// It orchestrates lookups against AbuseIPDB v2, VirusTotal v3, and Shodan,
// computes a composite threat score (0-100), caches raw API responses in
// SQLite via the db package, and updates IPAccount records with intel data.
package intel

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"strings"
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

// EnrichProgress is a single progress event from EnrichStream.
type EnrichProgress struct {
	Step   string `json:"step"`             // identifier: "vt_start", "vt_done", "done", "error", etc.
	Msg    string `json:"msg"`              // human-readable status line
	Pct    int    `json:"pct"`              // 0-100 progress percentage
	IsErr  bool   `json:"err,omitempty"`    // true if this event represents a non-fatal warning
	Score  int64  `json:"score,omitempty"`  // set on the final "done" event
	Status string `json:"status,omitempty"` // set on the final "done" event
}

// EnrichStream synchronously enriches ip, calling emit for each progress step.
//
// If force is true, all sources are queried regardless of cache TTL.
// This is the primary enrichment path; EnrichNow is a thin wrapper.
func (e *Enricher) EnrichStream(ctx context.Context, ip string, force bool, emit func(EnrichProgress)) (*db.IPAccount, error) {
	now := time.Now().UTC()
	nowISO := now.Format(time.RFC3339)

	// ---- VirusTotal ----
	var vtMalicious int64 = -1
	var vtRaw string
	if e.cfg.Keys.VirusTotal != "" {
		if force || !e.cacheValid(ip, sourceVirusTotal, now) {
			emit(EnrichProgress{Step: "vt_start", Msg: "Querying VirusTotal\u2026", Pct: 10})
			_ = e.limiter.Wait(ctx)
			m, raw, err := CheckVirusTotal(e.cfg.Keys.VirusTotal, ip, e.httpClient)
			if err != nil {
				emit(EnrichProgress{Step: "vt_err", Msg: "VirusTotal: " + err.Error(), Pct: 30, IsErr: true})
				log.Printf("[intel] virustotal %s: %v", ip, err)
			} else {
				vtMalicious = m
				vtRaw = raw
				msg := fmt.Sprintf("VirusTotal: %d malicious detection(s)", m)
				if m == 0 {
					msg = "VirusTotal: clean"
				}
				emit(EnrichProgress{Step: "vt_done", Msg: msg, Pct: 30})
				if err2 := e.db.UpsertIntelCache(ip, sourceVirusTotal, nowISO, raw); err2 != nil {
					log.Printf("[intel] cache virustotal %s: %v", ip, err2)
				}
			}
		} else {
			emit(EnrichProgress{Step: "vt_cached", Msg: "VirusTotal: cached (TTL valid)", Pct: 30})
		}
	} else {
		emit(EnrichProgress{Step: "vt_skip", Msg: "VirusTotal: no API key", Pct: 30})
	}

	// ---- AbuseIPDB ----
	var abuseScore int64 = -1
	var abuseRaw string
	if e.cfg.Keys.AbuseIPDB != "" {
		if force || !e.cacheValid(ip, sourceAbuseIPDB, now) {
			emit(EnrichProgress{Step: "abuse_start", Msg: "Querying AbuseIPDB\u2026", Pct: 45})
			_ = e.limiter.Wait(ctx)
			s, raw, err := CheckAbuseIPDB(e.cfg.Keys.AbuseIPDB, ip, e.httpClient)
			if err != nil {
				emit(EnrichProgress{Step: "abuse_err", Msg: "AbuseIPDB: " + err.Error(), Pct: 60, IsErr: true})
				log.Printf("[intel] abuseipdb %s: %v", ip, err)
			} else {
				abuseScore = s
				abuseRaw = raw
				emit(EnrichProgress{Step: "abuse_done", Msg: fmt.Sprintf("AbuseIPDB: confidence score %d", s), Pct: 60})
				if err2 := e.db.UpsertIntelCache(ip, sourceAbuseIPDB, nowISO, raw); err2 != nil {
					log.Printf("[intel] cache abuseipdb %s: %v", ip, err2)
				}
			}
		} else {
			emit(EnrichProgress{Step: "abuse_cached", Msg: "AbuseIPDB: cached (TTL valid)", Pct: 60})
		}
	} else {
		emit(EnrichProgress{Step: "abuse_skip", Msg: "AbuseIPDB: no API key", Pct: 60})
	}

	// ---- Shodan ----
	var shodanResult *ShodanResult
	var shodanRaw string
	if e.cfg.Keys.Shodan != "" {
		if force || !e.cacheValid(ip, sourceShodan, now) {
			emit(EnrichProgress{Step: "shodan_start", Msg: "Querying Shodan\u2026", Pct: 65})
			_ = e.limiter.Wait(ctx)
			sr, raw, err := CheckShodan(e.cfg.Keys.Shodan, ip, e.httpClient)
			if err != nil {
				emit(EnrichProgress{Step: "shodan_err", Msg: "Shodan: " + err.Error(), Pct: 80, IsErr: true})
				log.Printf("[intel] shodan %s: %v", ip, err)
			} else if sr == nil {
				emit(EnrichProgress{Step: "shodan_none", Msg: "Shodan: no data for this IP", Pct: 80})
			} else {
				shodanResult = sr
				shodanRaw = raw
				emit(EnrichProgress{Step: "shodan_done", Msg: fmt.Sprintf("Shodan: %d open port(s)", len(sr.Ports)), Pct: 80})
				if err2 := e.db.UpsertIntelCache(ip, sourceShodan, nowISO, raw); err2 != nil {
					log.Printf("[intel] cache shodan %s: %v", ip, err2)
				}
			}
		} else {
			emit(EnrichProgress{Step: "shodan_cached", Msg: "Shodan: cached (TTL valid)", Pct: 80})
		}
	} else {
		emit(EnrichProgress{Step: "shodan_skip", Msg: "Shodan: no API key", Pct: 80})
	}

	// ---- Build / update IPAccount ----
	emit(EnrichProgress{Step: "score", Msg: "Computing threat score\u2026", Pct: 88})

	acc, err := e.db.GetIPAccount(ip)
	if err != nil {
		acc = &db.IPAccount{
			IP:        ip,
			FirstSeen: nowISO,
			LastSeen:  nowISO,
			Status:    string(ThreatUnknown),
		}
	}

	if abuseScore >= 0 {
		acc.AbuseScore = abuseScore
		acc.AbuseData = abuseRaw
	}
	if vtMalicious >= 0 {
		acc.VTMalicious = vtMalicious
		acc.VTData = vtRaw
	}
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

	emit(EnrichProgress{Step: "save", Msg: "Saving results\u2026", Pct: 94})

	if err := e.db.UpsertIPAccount(acc); err != nil {
		emit(EnrichProgress{Step: "error", Msg: "Save failed: " + err.Error(), Pct: 100, IsErr: true})
		return acc, err
	}

	scoreMsg := fmt.Sprintf("Score: %d/100 \u2022 %s", acc.ThreatScore, strings.ToUpper(acc.Status[:1])+acc.Status[1:])
	emit(EnrichProgress{Step: "done", Msg: scoreMsg, Pct: 100, Score: acc.ThreatScore, Status: acc.Status})
	return acc, nil
}

// EnrichNow synchronously enriches a single IP address (background queue use).
// Uses cache TTL; for force-refresh use EnrichStream directly.
func (e *Enricher) EnrichNow(ip string) (*db.IPAccount, error) {
	return e.EnrichStream(context.Background(), ip, false, func(EnrichProgress) {})
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
