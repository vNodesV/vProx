package intel_test

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/vNodesV/vProx/internal/vlog/config"
	"github.com/vNodesV/vProx/internal/vlog/db"
	"github.com/vNodesV/vProx/internal/vlog/intel"
)

func TestComputeScore_AllSources(t *testing.T) {
	tests := []struct {
		name            string
		abuseScore      int64
		vtMalicious     int64
		shodanRiskFlags int64
		wantMin         int64
		wantMax         int64
	}{
		{"clean IP all sources", 0, 0, 0, 0, 5},
		{"high abuse only", 100, -1, 0, 60, 70}, // AbuseIPDB only: 100*(0.4/0.6)=67
		{"high VT only", -1, 70, 0, 60, 70},     // VT only (70*5=100 capped): 100*(0.4/0.6)=67
		{"shodan risk only", -1, -1, 3, 55, 65}, // Shodan only: 3*20=60 with full weight → 60
		{"all max", 100, 100, 5, 95, 100},
		{"no data", -1, -1, -1, -1, -1}, // special: nothing fetched
		{"abuse confirmed", 85, 0, 0, 30, 40},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			score := intel.ComputeScore(tt.abuseScore, tt.vtMalicious, tt.shodanRiskFlags)
			if score < tt.wantMin || score > tt.wantMax {
				t.Errorf("ComputeScore(%d, %d, %d) = %d, want %d-%d",
					tt.abuseScore, tt.vtMalicious, tt.shodanRiskFlags, score, tt.wantMin, tt.wantMax)
			}
		})
	}
}

func TestLevel(t *testing.T) {
	tests := []struct {
		score int64
		want  intel.ThreatLevel
	}{
		{-1, intel.ThreatUnknown},
		{0, intel.ThreatClean},
		{19, intel.ThreatClean},
		{20, intel.ThreatSuspicious},
		{49, intel.ThreatSuspicious},
		{50, intel.ThreatMalicious},
		{100, intel.ThreatMalicious},
	}

	for _, tt := range tests {
		got := intel.Level(tt.score)
		if got != tt.want {
			t.Errorf("Level(%d) = %q, want %q", tt.score, got, tt.want)
		}
	}
}

func TestExtractShodanRiskFlags(t *testing.T) {
	tests := []struct {
		name    string
		data    string
		want    int64
		wantMin int64
		exact   bool
	}{
		{
			name:  "no data returns -1",
			data:  "",
			want:  -1,
			exact: true,
		},
		{
			name:    "clean ports",
			data:    `{"ports":[80,443]}`,
			wantMin: 0,
		},
		{
			name:    "risky port telnet",
			data:    `{"ports":[23,80,443]}`,
			wantMin: 1,
		},
		{
			name:    "risky port 4444",
			data:    `{"ports":[4444,80]}`,
			wantMin: 1,
		},
		{
			name:    "multiple risky ports",
			data:    `{"ports":[4444,6666,1080,9050]}`,
			wantMin: 2,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			flags := intel.ExtractShodanRiskFlags(tt.data)
			if tt.exact {
				if flags != tt.want {
					t.Errorf("ExtractShodanRiskFlags(%q) = %d, want %d", tt.data, flags, tt.want)
				}
			} else if flags < tt.wantMin {
				t.Errorf("ExtractShodanRiskFlags(%q) = %d, want >= %d", tt.data, flags, tt.wantMin)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// BuildThreatFlags
// ---------------------------------------------------------------------------

func TestBuildThreatFlags(t *testing.T) {
	tests := []struct {
		name string
		acc  *db.IPAccount
		want []string // expected flags
	}{
		{
			name: "nil account",
			acc:  nil,
			want: nil,
		},
		{
			name: "clean account",
			acc: &db.IPAccount{
				AbuseScore: 0, VTMalicious: 0, RatelimitEvents: 0,
			},
			want: nil,
		},
		{
			name: "abuse confirmed",
			acc: &db.IPAccount{
				AbuseScore: 85, VTMalicious: 0,
			},
			want: []string{"ABUSEIPDB_CONFIRMED"},
		},
		{
			name: "VT malicious",
			acc: &db.IPAccount{
				AbuseScore: 0, VTMalicious: 5,
			},
			want: []string{"VT_MALICIOUS"},
		},
		{
			name: "high ratelimit events",
			acc: &db.IPAccount{
				AbuseScore: 0, VTMalicious: 0, RatelimitEvents: 15,
			},
			want: []string{"HIGH_RATELIMIT_EVENTS"},
		},
		{
			name: "datacenter ASN",
			acc: &db.IPAccount{
				AbuseScore: 0, VTMalicious: 0, Org: "Amazon Cloud Hosting Inc",
			},
			want: []string{"DATACENTER_ASN"},
		},
		{
			name: "multiple flags",
			acc: &db.IPAccount{
				AbuseScore: 90, VTMalicious: 10, RatelimitEvents: 50,
				Org: "Digital Ocean Cloud",
			},
			want: []string{"ABUSEIPDB_CONFIRMED", "VT_MALICIOUS", "HIGH_RATELIMIT_EVENTS", "DATACENTER_ASN"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := intel.BuildThreatFlags(tt.acc)
			if len(tt.want) == 0 {
				if got != "[]" {
					t.Errorf("BuildThreatFlags = %q, want []", got)
				}
				return
			}
			for _, flag := range tt.want {
				if !strings.Contains(got, flag) {
					t.Errorf("BuildThreatFlags missing flag %q in %q", flag, got)
				}
			}
		})
	}
}

// ---------------------------------------------------------------------------
// ExtractRiskFlagsFromResult
// ---------------------------------------------------------------------------

func TestExtractRiskFlagsFromResult(t *testing.T) {
	tests := []struct {
		name string
		sr   *intel.ShodanResult
		want int64
	}{
		{"nil result", nil, -1},
		{"clean ports", &intel.ShodanResult{Ports: []int{80, 443}}, 0},
		{"risky port 4444", &intel.ShodanResult{Ports: []int{4444, 80}}, 1},
		{"telnet + SOCKS", &intel.ShodanResult{Ports: []int{23, 1080}}, 2},
		{"8080 without proxy label", &intel.ShodanResult{
			Ports:    []int{8080},
			Services: []intel.ShodanService{{Port: 8080, Product: "nginx"}},
		}, 0},
		{"8080 with proxy label", &intel.ShodanResult{
			Ports:    []int{8080},
			Services: []intel.ShodanService{{Port: 8080, Product: "Squid proxy"}},
		}, 1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := intel.ExtractRiskFlagsFromResult(tt.sr)
			if got != tt.want {
				t.Errorf("ExtractRiskFlagsFromResult = %d, want %d", got, tt.want)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// ParseShodanJSON
// ---------------------------------------------------------------------------

func TestParseShodanJSON(t *testing.T) {
	t.Run("empty string", func(t *testing.T) {
		if got := intel.ParseShodanJSON(""); got != nil {
			t.Error("expected nil for empty string")
		}
	})

	t.Run("invalid JSON", func(t *testing.T) {
		if got := intel.ParseShodanJSON("{{{"); got != nil {
			t.Error("expected nil for invalid JSON")
		}
	})

	t.Run("valid JSON", func(t *testing.T) {
		raw := `{"ports":[80,443],"ip_str":"1.2.3.4","org":"Example Corp","country_name":"US","asn":"AS1234"}`
		result := intel.ParseShodanJSON(raw)
		if result == nil {
			t.Fatal("expected non-nil result")
		}
		if result.Org != "Example Corp" {
			t.Errorf("Org = %q", result.Org)
		}
	})
}

// ---------------------------------------------------------------------------
// CheckVirusTotal with httptest
// ---------------------------------------------------------------------------

func TestCheckVirusTotal_EmptyKey(t *testing.T) {
	t.Parallel()
	m, raw, err := intel.CheckVirusTotal("", "1.2.3.4", nil)
	if err != nil {
		t.Fatal(err)
	}
	if m != 0 || raw != "" {
		t.Errorf("empty key: got (%d, %q), want (0, \"\")", m, raw)
	}
}

func TestCheckVirusTotal_HappyPath(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("x-apikey"); got != "test-key" {
			t.Errorf("x-apikey = %q", got)
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"data":{"attributes":{"last_analysis_stats":{"malicious":5,"suspicious":1,"harmless":70,"undetected":2}}}}`)
	}))
	defer srv.Close()

	// Override the VT URL by calling the function with a custom client
	// that redirects to our test server
	client := srv.Client()
	// We need to make the request go to our test server
	origTransport := client.Transport
	client.Transport = rewriteTransport{base: origTransport, target: srv.URL}

	m, raw, err := intel.CheckVirusTotal("test-key", "1.2.3.4", client)
	if err != nil {
		t.Fatalf("CheckVirusTotal: %v", err)
	}
	if m != 5 {
		t.Errorf("malicious = %d, want 5", m)
	}
	if raw == "" {
		t.Error("expected non-empty raw JSON")
	}
}

func TestCheckVirusTotal_HTTPError(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		fmt.Fprint(w, `{"error":"forbidden"}`)
	}))
	defer srv.Close()

	client := srv.Client()
	client.Transport = rewriteTransport{base: client.Transport, target: srv.URL}

	_, _, err := intel.CheckVirusTotal("test-key", "1.2.3.4", client)
	if err == nil {
		t.Error("expected error for 403 response")
	}
}

// ---------------------------------------------------------------------------
// CheckAbuseIPDB with httptest
// ---------------------------------------------------------------------------

func TestCheckAbuseIPDB_EmptyKey(t *testing.T) {
	t.Parallel()
	s, raw, err := intel.CheckAbuseIPDB("", "1.2.3.4", nil)
	if err != nil {
		t.Fatal(err)
	}
	if s != 0 || raw != "" {
		t.Errorf("empty key: got (%d, %q)", s, raw)
	}
}

func TestCheckAbuseIPDB_HappyPath(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Key"); got != "test-abuse-key" {
			t.Errorf("Key header = %q", got)
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"data":{"ipAddress":"1.2.3.4","abuseConfidenceScore":75,"countryCode":"US","isp":"Example ISP"}}`)
	}))
	defer srv.Close()

	client := srv.Client()
	client.Transport = rewriteTransport{base: client.Transport, target: srv.URL}

	score, raw, err := intel.CheckAbuseIPDB("test-abuse-key", "1.2.3.4", client)
	if err != nil {
		t.Fatalf("CheckAbuseIPDB: %v", err)
	}
	if score != 75 {
		t.Errorf("score = %d, want 75", score)
	}
	if raw == "" {
		t.Error("expected non-empty raw JSON")
	}
}

func TestCheckAbuseIPDB_HTTPError(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
		fmt.Fprint(w, `{"errors":[{"detail":"rate limit"}]}`)
	}))
	defer srv.Close()

	client := srv.Client()
	client.Transport = rewriteTransport{base: client.Transport, target: srv.URL}

	_, _, err := intel.CheckAbuseIPDB("test-key", "1.2.3.4", client)
	if err == nil {
		t.Error("expected error for 429 response")
	}
}

// ---------------------------------------------------------------------------
// CheckShodan — empty key no-op
// ---------------------------------------------------------------------------

func TestCheckShodan_EmptyKey(t *testing.T) {
	t.Parallel()
	result, raw, err := intel.CheckShodan("", "1.2.3.4", nil)
	if err != nil {
		t.Fatal(err)
	}
	if result != nil || raw != "" {
		t.Errorf("empty key: got non-nil result or raw")
	}
}

// ---------------------------------------------------------------------------
// rewriteTransport — redirect requests to test server
// ---------------------------------------------------------------------------

type rewriteTransport struct {
	base   http.RoundTripper
	target string
}

func (t rewriteTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	req.URL.Scheme = "http"
	parsed, _ := url.Parse(t.target)
	req.URL.Host = parsed.Host
	if t.base != nil {
		return t.base.RoundTrip(req)
	}
	return http.DefaultTransport.RoundTrip(req)
}

// ---------------------------------------------------------------------------
// NewEnricher + Start/Stop lifecycle
// ---------------------------------------------------------------------------

func TestNewEnricher(t *testing.T) {
	tmpDir := t.TempDir()
	d, err := db.Open(tmpDir + "/test.db")
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()

	cfg := config.IntelConfig{
		RateLimitRPM:  30,
		CacheTTLHours: 24,
	}
	e := intel.NewEnricher(cfg, d)
	if e == nil {
		t.Fatal("NewEnricher returned nil")
	}

	e.Start()
	e.Enqueue("1.2.3.4") // Should not block
	e.Stop()
}

// ---------------------------------------------------------------------------
// Enqueue overflow (non-blocking)
// ---------------------------------------------------------------------------

func TestEnqueueOverflow(t *testing.T) {
	tmpDir := t.TempDir()
	d, err := db.Open(tmpDir + "/test.db")
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()

	cfg := config.IntelConfig{RateLimitRPM: 1000}
	e := intel.NewEnricher(cfg, d)
	// Don't start the worker — queue will fill up

	// Enqueue > 100 items without blocking (queue capacity is 100)
	for i := 0; i < 150; i++ {
		e.Enqueue(fmt.Sprintf("10.0.0.%d", i%256))
	}
	// If we got here without blocking, the test passes
	e.Stop()
}

// ---------------------------------------------------------------------------
// portsToJSON / hostnamesToJSON (via score.go)
// ---------------------------------------------------------------------------

func TestComputeScoreEdgeCases(t *testing.T) {
	// All sources -1 → -1
	if got := intel.ComputeScore(-1, -1, -1); got != -1 {
		t.Errorf("all -1: got %d, want -1", got)
	}

	// Only abuse available
	got := intel.ComputeScore(50, -1, -1)
	if got != 50 {
		t.Errorf("abuse only 50: got %d, want 50", got)
	}

	// Only VT available (5 malicious = 25 normalized)
	got = intel.ComputeScore(-1, 5, -1)
	if got != 25 {
		t.Errorf("VT only 5: got %d, want 25", got)
	}

	// Only Shodan (2 flags = 40 normalized)
	got = intel.ComputeScore(-1, -1, 2)
	if got != 40 {
		t.Errorf("shodan only 2: got %d, want 40", got)
	}

	// VT capped at 20 = 100
	got = intel.ComputeScore(-1, 25, -1)
	if got != 100 {
		t.Errorf("VT capped: got %d, want 100", got)
	}

	// Shodan capped at 5 = 100
	got = intel.ComputeScore(-1, -1, 10)
	if got != 100 {
		t.Errorf("shodan capped: got %d, want 100", got)
	}

	// Abuse capped at 100
	got = intel.ComputeScore(150, -1, -1)
	if got != 100 {
		t.Errorf("abuse capped: got %d, want 100", got)
	}
}

// ---------------------------------------------------------------------------
// CheckOSINT — tests the exported OSINT function
// ---------------------------------------------------------------------------

func TestCheckOSINT(t *testing.T) {
	// CheckOSINT does real DNS lookups so we test with localhost/loopback
	// which should complete quickly and not fail
	result, err := intel.CheckOSINT("127.0.0.1")
	if err != nil {
		t.Logf("CheckOSINT(127.0.0.1) error (may be expected in CI): %v", err)
		return
	}
	if result == nil {
		t.Error("expected non-nil result for localhost")
	}
}
