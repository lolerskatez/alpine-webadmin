package telemetry

import (
	"testing"
	"time"
)

func BenchmarkBuildSnapshot(b *testing.B) {
	hub := NewHub()
	c := NewCollector(hub, 2*time.Second)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = c.buildSnapshot()
	}
}

func BenchmarkBuildSnapshotParallel(b *testing.B) {
	hub := NewHub()
	c := NewCollector(hub, 2*time.Second)
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			_ = c.buildSnapshot()
		}
	})
}

func BenchmarkCollectAndEncode(b *testing.B) {
	hub := NewHub()
	c := NewCollector(hub, 2*time.Second)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		c.collect()
	}
}
