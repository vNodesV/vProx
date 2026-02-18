package geo

import (
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/oschwald/geoip2-golang"
	maxminddb "github.com/oschwald/maxminddb-golang"
)

var (
	once sync.Once

	// Primary: IP2Location MMDB
	ip2lDB       *maxminddb.Reader
	ip2lPathUsed string

	// Fallbacks: GeoLite2
	geoCountry *geoip2.Reader
	geoASN     *geoip2.Reader

	lastIP2LErr string
)

// Preferred MMDB location(s)
var ip2lPaths = []string{
	"/usr/local/share/IP2Proxy/ip2location.mmdb",    // primary (your requested path)
	"/usr/local/share/IP2Location/ip2location.mmdb", // common alt
	"/usr/share/IP2Proxy/ip2location.mmdb",
	"/usr/share/IP2Location/ip2location.mmdb",
	"./ip2location.mmdb",
}

// Fallback GeoLite2 paths (optional)
var geoCountryPaths = []string{
	"/usr/share/GeoIP/GeoLite2-Country.mmdb",
	"/usr/local/share/GeoIP/GeoLite2-Country.mmdb",
}
var geoASNPaths = []string{
	"/usr/share/GeoIP/GeoLite2-ASN.mmdb",
	"/usr/local/share/GeoIP/GeoLite2-ASN.mmdb",
}

// ------------------------
// Safe open + init
// ------------------------

func safeOpenMMDB(path string) (db *maxminddb.Reader, err error) {
	fi, statErr := os.Stat(path)
	if statErr != nil {
		return nil, statErr
	}
	// Very small files are suspicious for mmdb
	if fi.Size() < 1<<20 { // 1 MiB
		return nil, fmt.Errorf("mmdb too small: %s (%d bytes)", path, fi.Size())
	}
	defer func() {
		if r := recover(); r != nil {
			db = nil
			err = fmt.Errorf("mmdb open panic for %s: %v", path, r)
		}
	}()
	return maxminddb.Open(filepath.Clean(path))
}

// lazy init — tries IP2Location MMDB first, then GeoLite2 fallbacks
func initDB() {
	// 1) IP2Location MMDB
	if ip2lDB == nil {
		if p := strings.TrimSpace(os.Getenv("IP2LOCATION_MMDB")); p != "" {
			if db, err := safeOpenMMDB(p); err == nil {
				ip2lDB = db
				ip2lPathUsed = p
				logIP2LMeta(ip2lDB)
			} else {
				lastIP2LErr = err.Error()
				fmt.Fprintf(os.Stderr, "[geo] ip2location-mmdb: %v\n", err)
			}
		}
	}
	if ip2lDB == nil {
		for _, p := range ip2lPaths {
			if db, err := safeOpenMMDB(p); err == nil {
				ip2lDB = db
				ip2lPathUsed = p
				logIP2LMeta(ip2lDB)
				break
			} else {
				lastIP2LErr = err.Error()
				fmt.Fprintf(os.Stderr, "[geo] ip2location-mmdb: %v\n", err)
			}
		}
	}

	// 2) GeoLite2 fallbacks
	if geoCountry == nil {
		if p := os.Getenv("GEOLITE2_COUNTRY_DB"); p != "" {
			if db, err := geoip2.Open(filepath.Clean(p)); err == nil {
				geoCountry = db
			}
		}
	}
	if geoCountry == nil {
		for _, p := range geoCountryPaths {
			if db, err := geoip2.Open(filepath.Clean(p)); err == nil {
				geoCountry = db
				break
			}
		}
	}
	if geoASN == nil {
		if p := os.Getenv("GEOLITE2_ASN_DB"); p != "" {
			if db, err := geoip2.Open(filepath.Clean(p)); err == nil {
				geoASN = db
			}
		}
	}
	if geoASN == nil {
		for _, p := range geoASNPaths {
			if db, err := geoip2.Open(filepath.Clean(p)); err == nil {
				geoASN = db
				break
			}
		}
	}
}

func logIP2LMeta(db *maxminddb.Reader) {
	if db == nil {
		return
	}
	meta := db.Metadata
	build := time.Unix(int64(meta.BuildEpoch), 0).UTC().Format("2006-01-02")
	desc := ""
	if meta.Description != nil {
		if v, ok := meta.Description["en"]; ok {
			desc = v
		}
	}
	fmt.Fprintf(os.Stderr, "[geo] ip2location-mmdb loaded: type=%s build=%s recs=%d desc=%q path=%s\n",
		meta.DatabaseType, build, meta.NodeCount, desc, ip2lPathUsed)
}

// ------------------------
// Micro-cache
// ------------------------

type cacheEntry struct {
	cc, asn string
	exp     time.Time
}

var cache sync.Map // ip string -> cacheEntry

const cacheTTL = 10 * time.Minute

func cacheGet(ip string) (cc, asn string, ok bool) {
	if v, ok := cache.Load(ip); ok {
		e := v.(cacheEntry)
		if time.Now().Before(e.exp) {
			return e.cc, e.asn, true
		}
		cache.Delete(ip)
	}
	return "", "", false
}
func cacheSet(ip, cc, asn string) {
	cache.Store(ip, cacheEntry{cc: cc, asn: asn, exp: time.Now().Add(cacheTTL)})
}

// ------------------------
// Public API
// ------------------------

// Lookup returns (countryISO2, "AS####") with caching and fallbacks.
func Lookup(ipStr string) (string, string) {
	once.Do(initDB)
	if ipStr == "" {
		return "", ""
	}
	if cc, asn, ok := cacheGet(ipStr); ok {
		return cc, asn
	}

	ip := net.ParseIP(ipStr)
	if ip == nil {
		return "", ""
	}

	// 1) Try IP2Location (single lookup, extract both)
	if ip2lDB != nil {
		var raw map[string]interface{}
		if err := ip2lDB.Lookup(ip, &raw); err == nil && raw != nil {
			cc := normalizeCountry(mmdbGetStringFromRaw(raw, "country_code"),
				mmdbGetStringFromRaw(raw, "country_short"),
				mmdbGetStringFromRaw(raw, "country.iso_code"),
			)

			asn := normalizeASN(
				mmdbGetStringFromRaw(raw, "asn"),
				mmdbGetStringFromRaw(raw, "autonomous_system_number"),
			)
			if asn == "" {
				if num, ok := mmdbGetUintFromRaw(raw, "autonomous_system_number"); ok {
					asn = "AS" + strconv.FormatUint(num, 10)
				}
			}

			if cc != "" || asn != "" {
				cacheSet(ipStr, cc, asn)
				return cc, asn
			}
		}
	}

	// 2) GeoLite2 fallbacks
	cc := ""
	asn := ""
	if geoCountry != nil {
		if rec, err := geoCountry.Country(ip); err == nil && rec != nil {
			cc = strings.ToUpper(strings.TrimSpace(rec.Country.IsoCode))
		}
	}
	if geoASN != nil {
		if rec, err := geoASN.ASN(ip); err == nil && rec != nil && rec.AutonomousSystemNumber != 0 {
			asn = "AS" + strconv.FormatUint(uint64(rec.AutonomousSystemNumber), 10)
		}
	}
	if cc != "" || asn != "" {
		cacheSet(ipStr, cc, asn)
	}
	return cc, asn
}

// Country returns ISO-3166 alpha-2 (e.g., "US").
func Country(ipStr string) string {
	cc, _ := Lookup(ipStr)
	return cc
}

// ASN returns like "AS13335".
func ASN(ipStr string) string {
	_, asn := Lookup(ipStr)
	return asn
}

// LookupProxy is a no-op here (IP2Location MMDB does not provide proxy flags in standard DBs).
type ProxyMeta struct {
	IsProxy     bool
	Type        string
	Threat      string
	Provider    string
	Residential string
}

func LookupProxy(_ string) (ProxyMeta, bool) { return ProxyMeta{}, false }

// Info returns a one-line status string for logging.
func Info() string {
	once.Do(initDB)
	var parts []string

	if ip2lDB != nil {
		meta := ip2lDB.Metadata
		build := time.Unix(int64(meta.BuildEpoch), 0).UTC().Format("2006-01-02")
		parts = append(parts, fmt.Sprintf("ip2location-mmdb type=%s build=%s path=%s", meta.DatabaseType, build, ip2lPathUsed))
	} else {
		msg := "ip2location-mmdb not loaded"
		if v := strings.TrimSpace(os.Getenv("IP2LOCATION_MMDB")); v != "" {
			msg += " (open_failed)"
		}
		if lastIP2LErr != "" {
			msg += " reason=" + lastIP2LErr
		}
		parts = append(parts, msg)
	}
	if geoCountry != nil {
		parts = append(parts, "geolite2-country=ok")
	}
	if geoASN != nil {
		parts = append(parts, "geolite2-asn=ok")
	}
	return "[geo] " + strings.Join(parts, " | ")
}

// Close releases DB resources (useful on shutdown / hot-reload).
func Close() {
	if ip2lDB != nil {
		_ = ip2lDB.Close()
	}
	if geoCountry != nil {
		_ = geoCountry.Close()
	}
	if geoASN != nil {
		_ = geoASN.Close()
	}
}

// ------------------------
// MMDB helpers
// ------------------------

// mmdbGetStringFromRaw supports dot paths like "country.iso_code"
func mmdbGetStringFromRaw(raw map[string]interface{}, path string) string {
	if raw == nil || path == "" {
		return ""
	}
	val, ok := dig(raw, path)
	if !ok {
		return ""
	}
	switch v := val.(type) {
	case string:
		return strings.TrimSpace(v)
	case float64:
		return strconv.FormatInt(int64(v), 10)
	case int64:
		return strconv.FormatInt(v, 10)
	case uint64:
		return strconv.FormatUint(v, 10)
	default:
		return ""
	}
}

func mmdbGetUintFromRaw(raw map[string]interface{}, path string) (uint64, bool) {
	if raw == nil || path == "" {
		return 0, false
	}
	val, ok := dig(raw, path)
	if !ok {
		return 0, false
	}
	switch v := val.(type) {
	case float64:
		if v < 0 {
			return 0, false
		}
		return uint64(v), true
	case int64:
		if v < 0 {
			return 0, false
		}
		return uint64(v), true
	case uint64:
		return v, true
	case string:
		if v == "" {
			return 0, false
		}
		if u, err := strconv.ParseUint(v, 10, 64); err == nil {
			return u, true
		}
		return 0, false
	default:
		return 0, false
	}
}

// dig walks a dotted path (e.g., "country.iso_code") in a generic MMDB record.
func dig(m map[string]interface{}, path string) (interface{}, bool) {
	cur := interface{}(m)
	for _, key := range strings.Split(path, ".") {
		asMap, ok := cur.(map[string]interface{})
		if !ok {
			return nil, false
		}
		next, ok := asMap[key]
		if !ok {
			return nil, false
		}
		cur = next
	}
	return cur, true
}

// ------------------------
// Normalizers
// ------------------------

func normalizeCountry(vals ...string) string {
	for _, s := range vals {
		s = strings.TrimSpace(s)
		if s == "" {
			continue
		}
		if s == "-" || strings.EqualFold(s, "NA") || strings.EqualFold(s, "N/A") || strings.EqualFold(s, "NOT SUPPORTED") {
			continue
		}
		if len(s) == 2 {
			return strings.ToUpper(s)
		}
		// return upper for non-2-letter codes as-is
		return strings.ToUpper(s)
	}
	return ""
}

func normalizeASN(vals ...string) string {
	for _, s := range vals {
		s = strings.TrimSpace(s)
		if s == "" || s == "-" {
			continue
		}
		u := strings.ToUpper(s)
		if strings.HasPrefix(u, "AS") {
			return u
		}
		// numeric string
		if _, err := strconv.ParseUint(u, 10, 64); err == nil {
			return "AS" + u
		}
	}
	return ""
}
