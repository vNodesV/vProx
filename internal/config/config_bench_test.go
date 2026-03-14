package config

import (
	"testing"
)

// validChain returns a fully populated, valid ChainConfig for benchmarks.
func validChain() *ChainConfig {
	return &ChainConfig{
		SchemaVersion: 1,
		ChainName:     "cosmoshub",
		Host:          "rpc.cosmos.example.com",
		IP:            "10.0.0.1",
		RPCAliases:    []string{"rpc-alt.cosmos.example.com"},
		RESTAliases:   []string{"api-alt.cosmos.example.com"},
		APIAliases:    []string{"lcd.cosmos.example.com"},
		Expose: Expose{
			Path:  true,
			VHost: true,
			VHostPrefix: VHostPrefix{
				RPC:  "rpc",
				REST: "api",
			},
		},
		Services: Services{
			RPC:       true,
			REST:      true,
			WebSocket: true,
			GRPC:      true,
			GRPCWeb:   true,
			APIAlias:  true,
		},
		Ports: Ports{
			RPC:     26657,
			REST:    1317,
			GRPC:    9090,
			GRPCWeb: 9091,
			API:     1318,
		},
		WS: WSConfig{
			IdleTimeoutSec: 3600,
			MaxLifetimeSec: 0,
		},
		Features: Features{
			RPCAddressMasking: true,
			MaskRPC:           "rpc.cosmos.example.com",
			SwaggerMasking:    true,
			AbsoluteLinks:     "auto",
		},
	}
}

// BenchmarkValidateChainConfig measures full chain config validation
// including hostname checks, port validation, alias validation, and
// normalization.
func BenchmarkValidateChainConfig(b *testing.B) {
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		c := validChain()
		if err := ValidateConfig(c); err != nil {
			b.Fatalf("unexpected error: %v", err)
		}
	}
}

// BenchmarkIsValidHostname measures the regex-based hostname validation
// which is the hot-path parsing function called for every alias.
func BenchmarkIsValidHostname(b *testing.B) {
	b.ReportAllocs()

	hosts := []string{
		"rpc.cosmos.example.com",
		"api.example.com",
		"node1.stargaze.example.network",
		"a.co",
		"my-long-subdomain.with-many.parts.example.org",
	}

	b.ResetTimer()
	for i := range b.N {
		IsValidHostname(hosts[i%len(hosts)])
	}
}

// BenchmarkValidatePortsLabel measures port range validation.
func BenchmarkValidatePortsLabel(b *testing.B) {
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		_ = ValidatePortsLabel("rpc", 26657)
	}
}

// BenchmarkValidateAbsoluteLinksMode measures feature mode validation.
func BenchmarkValidateAbsoluteLinksMode(b *testing.B) {
	b.ReportAllocs()

	modes := []string{"auto", "always", "never", ""}

	b.ResetTimer()
	for i := range b.N {
		ValidateAbsoluteLinksMode(modes[i%len(modes)])
	}
}
