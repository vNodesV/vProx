package geo

import (
	"fmt"
	"testing"
	"time"
)

// BenchmarkGeoLookup_CacheHit measures a warm cache lookup.
// Pre-seeds the package-level cache so no MMDB file is needed.
func BenchmarkGeoLookup_CacheHit(b *testing.B) {
	b.ReportAllocs()

	const ip = "198.51.100.1"
	// Seed cache directly (package-level sync.Map).
	cache.Store(ip, cacheEntry{
		cc:  "US",
		asn: "AS13335",
		exp: time.Now().Add(1 * time.Hour),
	})
	b.Cleanup(func() { cache.Delete(ip) })

	b.ResetTimer()
	for b.Loop() {
		cc, asn, ok := cacheGet(ip)
		if !ok || cc == "" || asn == "" {
			b.Fatal("expected cache hit")
		}
	}
}

// BenchmarkGeoLookup_CacheMiss measures the cold lookup path when
// the cache has no entry. Without MMDB files this exercises the
// cache-miss + ParseIP path and returns empty strings.
func BenchmarkGeoLookup_CacheMiss(b *testing.B) {
	b.ReportAllocs()

	// Ensure none of these IPs are cached.
	b.Cleanup(func() {
		for i := range b.N {
			cache.Delete(fmt.Sprintf("192.0.2.%d", i%256))
		}
	})

	// Force sync.Once to fire (no-op when no MMDB present).
	once.Do(func() {})

	b.ResetTimer()
	for i := range b.N {
		ip := fmt.Sprintf("192.0.2.%d", i%256)
		cache.Delete(ip) // ensure miss
		cacheGet(ip)
	}
}

// BenchmarkGeoInfo measures the Info() function which builds a
// status string. Uses sync.Once guard so it's safe without MMDB.
func BenchmarkGeoInfo(b *testing.B) {
	b.ReportAllocs()

	// Make sure once has fired so Info() doesn't try to load DBs each time.
	once.Do(func() {})

	b.ResetTimer()
	for b.Loop() {
		s := Info()
		_ = s
	}
}

// BenchmarkGeoCacheSet measures the cost of writing a cache entry.
func BenchmarkGeoCacheSet(b *testing.B) {
	b.ReportAllocs()

	b.Cleanup(func() {
		// Clean up all seeded entries.
		cache.Range(func(key, _ any) bool {
			cache.Delete(key)
			return true
		})
	})

	b.ResetTimer()
	for i := range b.N {
		ip := fmt.Sprintf("10.0.%d.%d", (i/256)%256, i%256)
		cacheSet(ip, "CA", "AS577")
	}
}

// BenchmarkGeoCacheGet_Parallel measures concurrent cache reads.
func BenchmarkGeoCacheGet_Parallel(b *testing.B) {
	b.ReportAllocs()

	// Seed 256 IPs
	for i := range 256 {
		ip := fmt.Sprintf("198.51.100.%d", i)
		cache.Store(ip, cacheEntry{
			cc:  "US",
			asn: "AS13335",
			exp: time.Now().Add(1 * time.Hour),
		})
	}
	b.Cleanup(func() {
		for i := range 256 {
			cache.Delete(fmt.Sprintf("198.51.100.%d", i))
		}
	})

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		i := 0
		for pb.Next() {
			ip := fmt.Sprintf("198.51.100.%d", i%256)
			cacheGet(ip)
			i++
		}
	})
}
