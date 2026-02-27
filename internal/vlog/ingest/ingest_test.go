package ingest_test

import (
	"testing"

	"github.com/vNodesV/vProx/internal/vlog/ingest"
)

func TestParseLogLine_Access(t *testing.T) {
	tests := []struct {
		name     string
		line     string
		wantIP   string
		wantPath string
		wantMeth string
		wantNil  bool
	}{
		{
			// Real vProx LineLifecycle format (current)
			name:     "vProx lifecycle format",
			line:     `10:23AM NEW ID=API1A2B3C4D5E6F7G8H9I0J1K2 status=COMPLETED method=GET from=1.2.3.4 count=5 to=API.EXAMPLE.COM endpoint=/rpc latency=12ms userAgent="curl/7.64.1" country=US module=vProx`,
			wantIP:   "1.2.3.4",
			wantPath: "/rpc",
			wantMeth: "GET",
		},
		{
			// vProxWeb module also accepted
			name:     "vProxWeb module accepted",
			line:     `10:23AM NEW ID=REQ1A2B from=5.6.7.8 method=POST endpoint=/rest country=US module=vProxWeb`,
			wantIP:   "5.6.7.8",
			wantPath: "/rest",
			wantMeth: "POST",
		},
		{
			// Legacy format (module=access) for backward compatibility
			name:     "legacy access line with ua alias",
			line:     `10:23AM INF request ip=5.6.7.8 method=POST path=/rest ua="curl/7.64.1" module=access`,
			wantIP:   "5.6.7.8",
			wantPath: "/rest",
			wantMeth: "POST",
		},
		{
			name:    "non-request module skipped",
			line:    `10:23AM NEW status=STARTED module=backup`,
			wantNil: true,
		},
		{
			name:    "empty line skipped",
			line:    "",
			wantNil: true,
		},
		{
			name:    "comment line skipped",
			line:    "# this is a comment",
			wantNil: true,
		},
		{
			// Legacy module=proxy also accepted
			name:     "legacy proxy module accepted",
			line:     `10:23AM INF request ip=9.10.11.12 method=GET path=/api module=proxy`,
			wantIP:   "9.10.11.12",
			wantPath: "/api",
			wantMeth: "GET",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ev := ingest.ParseLogLine(tt.line, "backup.20250101_120000.tar.gz", "2025-01-01T12:00:00Z")
			if tt.wantNil {
				if ev != nil {
					t.Errorf("expected nil, got event with IP=%q", ev.IP)
				}
				return
			}
			if ev == nil {
				t.Fatal("expected non-nil event, got nil")
			}
			if ev.IP != tt.wantIP {
				t.Errorf("IP: got %q, want %q", ev.IP, tt.wantIP)
			}
			if ev.Path != tt.wantPath {
				t.Errorf("Path: got %q, want %q", ev.Path, tt.wantPath)
			}
			if ev.Method != tt.wantMeth {
				t.Errorf("Method: got %q, want %q", ev.Method, tt.wantMeth)
			}
		})
	}
}

func TestParseRateLimitLine(t *testing.T) {
	tests := []struct {
		name       string
		line       string
		wantIP     string
		wantEvent  string
		wantRPS    float64
		wantNil    bool
	}{
		{
			name:      "valid rate limit event",
			line:      `{"ts":"2025-01-01T12:00:00Z","level":"ERROR","event":"429","reason":"429","ip":"1.2.3.4","method":"GET","path":"/rpc","host":"api.example.com","rps":25.0,"burst":100}`,
			wantIP:    "1.2.3.4",
			wantEvent: "429",
			wantRPS:   25.0,
		},
		{
			name:    "empty line",
			line:    "",
			wantNil: true,
		},
		{
			name:    "invalid json",
			line:    `{"broken":`,
			wantNil: true,
		},
		{
			name:  "ua alias fallback",
			line:  `{"ts":"2025-01-01T12:00:01Z","event":"429","ip":"5.6.7.8","ua":"Mozilla/5.0","method":"GET","path":"/rpc","host":"x","rps":10.0,"burst":50}`,
			wantIP: "5.6.7.8",
			wantEvent: "429",
			wantRPS: 10.0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ev := ingest.ParseRateLimitLine(tt.line, "backup.20250101_120000.tar.gz")
			if tt.wantNil {
				if ev != nil {
					t.Errorf("expected nil, got event IP=%q", ev.IP)
				}
				return
			}
			if ev == nil {
				t.Fatal("expected non-nil event, got nil")
			}
			if ev.IP != tt.wantIP {
				t.Errorf("IP: got %q, want %q", ev.IP, tt.wantIP)
			}
			if ev.Event != tt.wantEvent {
				t.Errorf("Event: got %q, want %q", ev.Event, tt.wantEvent)
			}
			if ev.RPS != tt.wantRPS {
				t.Errorf("RPS: got %f, want %f", ev.RPS, tt.wantRPS)
			}
		})
	}
}
