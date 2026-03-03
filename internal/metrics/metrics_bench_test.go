package metrics

import (
	"sync/atomic"
	"testing"
	"time"
)

// BenchmarkRecordRequest measures the hot path: counter increment +
// histogram observe for a single proxied request.
func BenchmarkRecordRequest(b *testing.B) {
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		RecordRequest("GET", "/rpc", 200, 15*time.Millisecond)
	}
}

// BenchmarkRecordRequest_Parallel measures concurrent RecordRequest
// throughput from GOMAXPROCS goroutines.
func BenchmarkRecordRequest_Parallel(b *testing.B) {
	b.ReportAllocs()
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			RecordRequest("GET", "/rpc", 200, 15*time.Millisecond)
		}
	})
}

// BenchmarkRecordProxyError measures the error counter increment path.
func BenchmarkRecordProxyError(b *testing.B) {
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		RecordProxyError("/rpc", "backend_error")
	}
}

// BenchmarkIncDecConnections measures gauge increment/decrement
// (paired operations as they appear in production).
func BenchmarkIncDecConnections(b *testing.B) {
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		IncActiveConnections()
		DecActiveConnections()
	}
}

// BenchmarkRecordRequest_VaryLabels measures RecordRequest with
// varying label cardinality to check label-lookup overhead.
func BenchmarkRecordRequest_VaryLabels(b *testing.B) {
	b.ReportAllocs()

	routes := []string{"/rpc", "/api", "/rest", "/grpc", "/ws"}
	methods := []string{"GET", "POST", "PUT", "DELETE"}
	codes := []int{200, 201, 400, 404, 429, 500, 502, 503}

	var counter atomic.Int64

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		i := int(counter.Add(1))
		for pb.Next() {
			route := routes[i%len(routes)]
			method := methods[i%len(methods)]
			code := codes[i%len(codes)]
			RecordRequest(method, route, code, 10*time.Millisecond)
			i++
		}
	})
}
