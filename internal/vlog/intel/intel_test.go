package intel_test

import (
	"testing"

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
		{"high abuse only", 100, -1, 0, 60, 70},       // AbuseIPDB only: 100*(0.4/0.6)=67
		{"high VT only", -1, 70, 0, 60, 70},            // VT only (70*5=100 capped): 100*(0.4/0.6)=67
		{"shodan risk only", -1, -1, 3, 55, 65},        // Shodan only: 3*20=60 with full weight → 60
		{"all max", 100, 100, 5, 95, 100},
		{"no data", -1, -1, -1, -1, -1},                // special: nothing fetched
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
		name     string
		data     string
		wantMin  int64
	}{
		{
			name:    "no data",
			data:    "",
			wantMin: 0,
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
			if flags < tt.wantMin {
				t.Errorf("ExtractShodanRiskFlags(%q) = %d, want >= %d", tt.data, flags, tt.wantMin)
			}
		})
	}
}
