package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// ---------------------------------------------------------------------------
// IsValidHostname
// ---------------------------------------------------------------------------

func TestIsValidHostname(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		host string
		want bool
	}{
		{"valid simple", "example.com", true},
		{"valid subdomain", "rpc.cosmos.example.com", true},
		{"valid with numbers", "node1.example.com", true},
		{"valid with hyphens", "my-node.example.com", true},
		{"valid mixed case normalized", "Example.COM", true},
		{"empty string", "", false},
		{"single label", "localhost", false},
		{"too long", strings.Repeat("a", 254), false},
		{"starts with hyphen", "-example.com", false},
		{"ends with hyphen", "example-.com", false},
		{"contains underscore", "my_node.example.com", false},
		{"contains space", "my node.example.com", false},
		{"IP address", "192.168.1.1", true}, // regex treats IPs as valid hostnames
		{"trailing dot only", ".", false},
		{"valid short", "a.co", true},
		{"whitespace trimmed valid", "  example.com  ", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := IsValidHostname(tt.host); got != tt.want {
				t.Errorf("IsValidHostname(%q) = %v, want %v", tt.host, got, tt.want)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// ValidatePortsLabel
// ---------------------------------------------------------------------------

func TestValidatePortsLabel(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		label   string
		port    int
		wantErr bool
	}{
		{"valid low", "rpc", 1, false},
		{"valid mid", "rest", 8080, false},
		{"valid high", "grpc", 65535, false},
		{"zero", "rpc", 0, true},
		{"negative", "rest", -1, true},
		{"overflow", "grpc", 65536, true},
		{"large negative", "api", -9999, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			err := ValidatePortsLabel(tt.label, tt.port)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidatePortsLabel(%q, %d) error = %v, wantErr %v", tt.label, tt.port, err, tt.wantErr)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// ValidateAbsoluteLinksMode
// ---------------------------------------------------------------------------

func TestValidateAbsoluteLinksMode(t *testing.T) {
	t.Parallel()
	tests := []struct {
		mode string
		want bool
	}{
		{"", true},
		{"auto", true},
		{"always", true},
		{"never", true},
		{"AUTO", true},
		{"  Always  ", true},
		{"off", false},
		{"path", false},
		{"host", false},
		{"invalid", false},
	}
	for _, tt := range tests {
		t.Run(tt.mode, func(t *testing.T) {
			t.Parallel()
			if got := ValidateAbsoluteLinksMode(tt.mode); got != tt.want {
				t.Errorf("ValidateAbsoluteLinksMode(%q) = %v, want %v", tt.mode, got, tt.want)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// NormalizeVHostPrefixes
// ---------------------------------------------------------------------------

func TestNormalizeVHostPrefixes(t *testing.T) {
	t.Parallel()

	t.Run("fills defaults when empty", func(t *testing.T) {
		t.Parallel()
		e := &Expose{}
		NormalizeVHostPrefixes(e)
		if e.VHostPrefix.RPC != "rpc" {
			t.Errorf("RPC prefix = %q, want rpc", e.VHostPrefix.RPC)
		}
		if e.VHostPrefix.REST != "api" {
			t.Errorf("REST prefix = %q, want api", e.VHostPrefix.REST)
		}
	})

	t.Run("preserves existing values", func(t *testing.T) {
		t.Parallel()
		e := &Expose{
			VHostPrefix: VHostPrefix{RPC: "custom-rpc", REST: "custom-api"},
		}
		NormalizeVHostPrefixes(e)
		if e.VHostPrefix.RPC != "custom-rpc" {
			t.Errorf("RPC prefix = %q, want custom-rpc", e.VHostPrefix.RPC)
		}
		if e.VHostPrefix.REST != "custom-api" {
			t.Errorf("REST prefix = %q, want custom-api", e.VHostPrefix.REST)
		}
	})
}

// ---------------------------------------------------------------------------
// ValidateConfig
// ---------------------------------------------------------------------------

func TestValidateConfig(t *testing.T) {
	t.Parallel()

	validBase := func() *ChainConfig {
		return &ChainConfig{
			ChainName: "cosmos",
			Host:      "rpc.example.com",
			IP:        "10.0.0.1",
			Services:  Services{RPC: true},
			Ports:     Ports{RPC: 26657, REST: 1317},
		}
	}

	t.Run("valid config", func(t *testing.T) {
		t.Parallel()
		c := validBase()
		if err := ValidateConfig(c); err != nil {
			t.Errorf("valid config got error: %v", err)
		}
		if c.SchemaVersion != 1 {
			t.Errorf("SchemaVersion not defaulted: %d", c.SchemaVersion)
		}
	})

	t.Run("invalid host", func(t *testing.T) {
		t.Parallel()
		c := validBase()
		c.Host = ""
		if err := ValidateConfig(c); err == nil {
			t.Error("expected error for empty host")
		}
	})

	t.Run("invalid IP", func(t *testing.T) {
		t.Parallel()
		c := validBase()
		c.IP = "not-an-ip"
		if err := ValidateConfig(c); err == nil {
			t.Error("expected error for invalid IP")
		}
	})

	t.Run("invalid absolute_links mode", func(t *testing.T) {
		t.Parallel()
		c := validBase()
		c.Features.AbsoluteLinks = "bogus"
		if err := ValidateConfig(c); err == nil {
			t.Error("expected error for invalid absolute_links")
		}
	})

	t.Run("invalid port", func(t *testing.T) {
		t.Parallel()
		c := validBase()
		c.Ports.RPC = 0
		if err := ValidateConfig(c); err == nil {
			t.Error("expected error for zero rpc port")
		}
	})

	t.Run("default_ports skips port validation", func(t *testing.T) {
		t.Parallel()
		c := validBase()
		c.DefaultPorts = true
		c.Ports.RPC = 0
		if err := ValidateConfig(c); err != nil {
			t.Errorf("default_ports should skip port checks: %v", err)
		}
	})

	t.Run("no services enabled", func(t *testing.T) {
		t.Parallel()
		c := validBase()
		c.Services = Services{}
		if err := ValidateConfig(c); err == nil {
			t.Error("expected error for no services enabled")
		}
	})

	t.Run("websocket requires rpc", func(t *testing.T) {
		t.Parallel()
		c := validBase()
		c.Services = Services{WebSocket: true}
		if err := ValidateConfig(c); err == nil {
			t.Error("expected error: websocket requires rpc")
		}
	})

	t.Run("invalid rpc_alias", func(t *testing.T) {
		t.Parallel()
		c := validBase()
		c.RPCAliases = []string{"bad hostname!"}
		if err := ValidateConfig(c); err == nil {
			t.Error("expected error for invalid rpc alias")
		}
	})

	t.Run("invalid rest_alias", func(t *testing.T) {
		t.Parallel()
		c := validBase()
		c.RESTAliases = []string{"bad hostname!"}
		if err := ValidateConfig(c); err == nil {
			t.Error("expected error for invalid rest alias")
		}
	})

	t.Run("invalid api_alias", func(t *testing.T) {
		t.Parallel()
		c := validBase()
		c.APIAliases = []string{"bad hostname!"}
		if err := ValidateConfig(c); err == nil {
			t.Error("expected error for invalid api alias")
		}
	})

	t.Run("ws defaults applied", func(t *testing.T) {
		t.Parallel()
		c := validBase()
		c.WS.IdleTimeoutSec = -1
		c.WS.MaxLifetimeSec = -5
		if err := ValidateConfig(c); err != nil {
			t.Fatal(err)
		}
		if c.WS.IdleTimeoutSec != 3600 {
			t.Errorf("IdleTimeoutSec = %d, want 3600", c.WS.IdleTimeoutSec)
		}
		if c.WS.MaxLifetimeSec != 0 {
			t.Errorf("MaxLifetimeSec = %d, want 0", c.WS.MaxLifetimeSec)
		}
	})

	t.Run("grpc port validated when service enabled", func(t *testing.T) {
		t.Parallel()
		c := validBase()
		c.Services.GRPC = true
		c.Ports.GRPC = 0
		if err := ValidateConfig(c); err == nil {
			t.Error("expected error for zero grpc port with grpc enabled")
		}
	})

	t.Run("grpc_web port validated when service enabled", func(t *testing.T) {
		t.Parallel()
		c := validBase()
		c.Services.GRPCWeb = true
		c.Ports.GRPCWeb = 0
		if err := ValidateConfig(c); err == nil {
			t.Error("expected error for zero grpc_web port with grpc_web enabled")
		}
	})

	t.Run("api port validated when service enabled", func(t *testing.T) {
		t.Parallel()
		c := validBase()
		c.Services.APIAlias = true
		c.Ports.API = 0
		if err := ValidateConfig(c); err == nil {
			t.Error("expected error for zero api port with api_alias enabled")
		}
	})
}

// ---------------------------------------------------------------------------
// LoadPorts
// ---------------------------------------------------------------------------

func TestLoadPorts(t *testing.T) {
	t.Parallel()

	t.Run("valid TOML", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		f := filepath.Join(dir, "ports.toml")
		data := `rpc = 26657
rest = 1317
grpc = 9090
`
		if err := os.WriteFile(f, []byte(data), 0o644); err != nil {
			t.Fatal(err)
		}
		p, err := LoadPorts(f)
		if err != nil {
			t.Fatalf("LoadPorts: %v", err)
		}
		if p.RPC != 26657 {
			t.Errorf("RPC = %d, want 26657", p.RPC)
		}
		if p.REST != 1317 {
			t.Errorf("REST = %d, want 1317", p.REST)
		}
		if p.GRPC != 9090 {
			t.Errorf("GRPC = %d, want 9090", p.GRPC)
		}
	})

	t.Run("invalid TOML", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		f := filepath.Join(dir, "ports.toml")
		if err := os.WriteFile(f, []byte("{{{invalid"), 0o644); err != nil {
			t.Fatal(err)
		}
		if _, err := LoadPorts(f); err == nil {
			t.Error("expected error for invalid TOML")
		}
	})

	t.Run("missing file", func(t *testing.T) {
		t.Parallel()
		if _, err := LoadPorts("/nonexistent/ports.toml"); err == nil {
			t.Error("expected error for missing file")
		}
	})

	t.Run("invalid rpc port", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		f := filepath.Join(dir, "ports.toml")
		data := `rpc = 0
rest = 1317
`
		if err := os.WriteFile(f, []byte(data), 0o644); err != nil {
			t.Fatal(err)
		}
		if _, err := LoadPorts(f); err == nil {
			t.Error("expected error for invalid rpc port")
		}
	})

	t.Run("invalid rest port", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		f := filepath.Join(dir, "ports.toml")
		data := `rpc = 26657
rest = -1
`
		if err := os.WriteFile(f, []byte(data), 0o644); err != nil {
			t.Fatal(err)
		}
		if _, err := LoadPorts(f); err == nil {
			t.Error("expected error for invalid rest port")
		}
	})

	t.Run("optional grpc validated if nonzero", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		f := filepath.Join(dir, "ports.toml")
		data := `rpc = 26657
rest = 1317
grpc = 99999
`
		if err := os.WriteFile(f, []byte(data), 0o644); err != nil {
			t.Fatal(err)
		}
		if _, err := LoadPorts(f); err == nil {
			t.Error("expected error for invalid grpc port")
		}
	})

	t.Run("vops_url preserved", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		f := filepath.Join(dir, "ports.toml")
		data := `rpc = 26657
rest = 1317
vops_url = "http://localhost:8090/api/v1/ingest"
`
		if err := os.WriteFile(f, []byte(data), 0o644); err != nil {
			t.Fatal(err)
		}
		p, err := LoadPorts(f)
		if err != nil {
			t.Fatalf("LoadPorts: %v", err)
		}
		if p.VOpsURL != "http://localhost:8090/api/v1/ingest" {
			t.Errorf("VOpsURL = %q", p.VOpsURL)
		}
	})
}

// ---------------------------------------------------------------------------
// ContainsString / InList
// ---------------------------------------------------------------------------

func TestContainsString(t *testing.T) {
	t.Parallel()
	tests := []struct {
		slice []string
		s     string
		want  bool
	}{
		{[]string{"a", "b", "c"}, "b", true},
		{[]string{"a", "b", "c"}, "d", false},
		{nil, "a", false},
		{[]string{}, "", false},
		{[]string{""}, "", true},
	}
	for _, tt := range tests {
		if got := ContainsString(tt.slice, tt.s); got != tt.want {
			t.Errorf("ContainsString(%v, %q) = %v, want %v", tt.slice, tt.s, got, tt.want)
		}
	}
}

func TestInList(t *testing.T) {
	t.Parallel()
	tests := []struct {
		list   []string
		needle string
		want   bool
	}{
		{[]string{"Alpha", "Beta"}, "alpha", true},
		{[]string{"Alpha", "Beta"}, "BETA", true},
		{[]string{"Alpha", "Beta"}, "gamma", false},
		{nil, "a", false},
		{[]string{"  spaced  "}, "spaced", true},
	}
	for _, tt := range tests {
		if got := InList(tt.list, tt.needle); got != tt.want {
			t.Errorf("InList(%v, %q) = %v, want %v", tt.list, tt.needle, got, tt.want)
		}
	}
}

// ---------------------------------------------------------------------------
// HasChainConfigs / IsChainTOML
// ---------------------------------------------------------------------------

func TestIsChainTOML(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		want bool
	}{
		{"cosmos.toml", true},
		{"osmosis.toml", true},
		{"ports.toml", false},
		{"services.toml", false},
		{"backup.toml", false},
		{"cosmos.sample.toml", false},
		{"readme.txt", false},
		{"nottoml", false},
		{"Ports.TOML", false},    // case-insensitive skip
		{"Services.TOML", false}, // case-insensitive skip
		{"Backup.Toml", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := IsChainTOML(tt.name); got != tt.want {
				t.Errorf("IsChainTOML(%q) = %v, want %v", tt.name, got, tt.want)
			}
		})
	}
}

func TestHasChainConfigs(t *testing.T) {
	t.Parallel()

	t.Run("directory with chain TOML", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		if err := os.WriteFile(filepath.Join(dir, "cosmos.toml"), []byte(""), 0o644); err != nil {
			t.Fatal(err)
		}
		if !HasChainConfigs(dir) {
			t.Error("expected true with chain TOML present")
		}
	})

	t.Run("directory without chain TOML", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		if err := os.WriteFile(filepath.Join(dir, "ports.toml"), []byte(""), 0o644); err != nil {
			t.Fatal(err)
		}
		if HasChainConfigs(dir) {
			t.Error("expected false with only ports.toml")
		}
	})

	t.Run("empty directory", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		if HasChainConfigs(dir) {
			t.Error("expected false for empty dir")
		}
	})

	t.Run("nonexistent directory", func(t *testing.T) {
		t.Parallel()
		if HasChainConfigs("/nonexistent/path") {
			t.Error("expected false for nonexistent dir")
		}
	})
}

// ---------------------------------------------------------------------------
// PathPrefix / RouteIDPrefix
// ---------------------------------------------------------------------------

func TestPathPrefix(t *testing.T) {
	t.Parallel()
	tests := []struct {
		dst  string
		want string
	}{
		{"/rpc/status", "RPC"},
		{"/RPC", "RPC"},
		{"/rest/bank/balances", "API"},
		{"/api/v1/node", "API"},
		{"/API/something", "API"},
		{"/websocket", "WSS"},
		{"/ws", "WSS"},
		{"/unknown", "REQ"},
		{"/", "REQ"},
		{"", "REQ"},
	}
	for _, tt := range tests {
		t.Run(tt.dst, func(t *testing.T) {
			t.Parallel()
			if got := PathPrefix(tt.dst); got != tt.want {
				t.Errorf("PathPrefix(%q) = %q, want %q", tt.dst, got, tt.want)
			}
		})
	}
}

func TestRouteIDPrefix(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		prefix string
		route  string
		isRPC  bool
		isREST bool
		want   string
	}{
		{"rpc vhost", "", "", true, false, "RPC"},
		{"rpc prefix", "/rpc", "", false, false, "RPC"},
		{"rpc route", "", "rpc", false, false, "RPC"},
		{"rest vhost", "", "", false, true, "API"},
		{"rest prefix", "/rest", "", false, false, "API"},
		{"api prefix", "/api", "", false, false, "API"},
		{"grpc prefix", "/grpc", "", false, false, "API"},
		{"grpc-web prefix", "/grpc-web", "", false, false, "API"},
		{"rest route", "", "rest", false, false, "API"},
		{"fallback", "/other", "other", false, false, "REQ"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := RouteIDPrefix(tt.prefix, tt.route, tt.isRPC, tt.isREST); got != tt.want {
				t.Errorf("RouteIDPrefix(%q, %q, %v, %v) = %q, want %q",
					tt.prefix, tt.route, tt.isRPC, tt.isREST, got, tt.want)
			}
		})
	}
}
