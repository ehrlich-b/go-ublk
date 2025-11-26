package ublk

import (
	"testing"
	"time"
)

func TestMetrics(t *testing.T) {
	m := NewMetrics()

	// Test initial state
	snap := m.Snapshot()
	if snap.TotalOps != 0 {
		t.Errorf("Expected 0 initial ops, got %d", snap.TotalOps)
	}

	// Record some operations
	m.RecordRead(1024, 1000000, true)  // 1KB read, 1ms latency, success
	m.RecordWrite(2048, 2000000, true) // 2KB write, 2ms latency, success
	m.RecordRead(512, 500000, false)   // 512B read, 0.5ms latency, error

	snap = m.Snapshot()

	// Check operation counts
	if snap.ReadOps != 2 {
		t.Errorf("Expected 2 read ops, got %d", snap.ReadOps)
	}
	if snap.WriteOps != 1 {
		t.Errorf("Expected 1 write op, got %d", snap.WriteOps)
	}

	// Check byte counts (only successful operations)
	if snap.ReadBytes != 1024 {
		t.Errorf("Expected 1024 read bytes, got %d", snap.ReadBytes)
	}
	if snap.WriteBytes != 2048 {
		t.Errorf("Expected 2048 write bytes, got %d", snap.WriteBytes)
	}

	// Check error counts
	if snap.ReadErrors != 1 {
		t.Errorf("Expected 1 read error, got %d", snap.ReadErrors)
	}
	if snap.WriteErrors != 0 {
		t.Errorf("Expected 0 write errors, got %d", snap.WriteErrors)
	}

	// Check error rate
	expectedErrorRate := float64(1) / float64(3) * 100.0 // 1 error out of 3 ops
	if snap.ErrorRate < expectedErrorRate-0.1 || snap.ErrorRate > expectedErrorRate+0.1 {
		t.Errorf("Expected error rate ~%.1f%%, got %.1f%%", expectedErrorRate, snap.ErrorRate)
	}
}

func TestMetricsQueueDepth(t *testing.T) {
	m := NewMetrics()

	// Record queue depths
	m.RecordQueueDepth(10)
	m.RecordQueueDepth(20)
	m.RecordQueueDepth(15)

	snap := m.Snapshot()

	// Check max queue depth
	if snap.MaxQueueDepth != 20 {
		t.Errorf("Expected max queue depth 20, got %d", snap.MaxQueueDepth)
	}

	// Check average queue depth
	expectedAvg := float64(10+20+15) / 3.0
	if snap.AvgQueueDepth < expectedAvg-0.1 || snap.AvgQueueDepth > expectedAvg+0.1 {
		t.Errorf("Expected avg queue depth %.1f, got %.1f", expectedAvg, snap.AvgQueueDepth)
	}
}

func TestMetricsLatency(t *testing.T) {
	m := NewMetrics()

	// Record operations with known latencies
	m.RecordRead(1024, 1000000, true)  // 1ms
	m.RecordWrite(1024, 2000000, true) // 2ms

	snap := m.Snapshot()

	// Check average latency
	expectedAvgNs := uint64(1500000) // 1.5ms in nanoseconds
	if snap.AvgLatencyNs != expectedAvgNs {
		t.Errorf("Expected avg latency %d ns, got %d ns", expectedAvgNs, snap.AvgLatencyNs)
	}
}

func TestMetricsUptime(t *testing.T) {
	m := NewMetrics()

	// Sleep briefly to generate uptime
	time.Sleep(10 * time.Millisecond)

	snap := m.Snapshot()

	// Check that uptime is reasonable (should be at least 10ms)
	if snap.UptimeNs < 10*1000000 {
		t.Errorf("Expected uptime >= 10ms, got %d ns", snap.UptimeNs)
	}

	// Stop metrics and check stopped uptime
	m.Stop()
	time.Sleep(5 * time.Millisecond)

	snap2 := m.Snapshot()

	// Uptime should not have increased significantly after stop
	if snap2.UptimeNs > snap.UptimeNs+2*1000000 { // Allow 2ms tolerance
		t.Errorf("Uptime increased too much after stop: %d -> %d", snap.UptimeNs, snap2.UptimeNs)
	}
}

func TestMetricsReset(t *testing.T) {
	m := NewMetrics()

	// Record some operations
	m.RecordRead(1024, 1000000, true)
	m.RecordWrite(2048, 2000000, true)
	m.RecordQueueDepth(10)

	// Verify operations were recorded
	snap := m.Snapshot()
	if snap.TotalOps == 0 {
		t.Error("Expected some operations before reset")
	}

	// Reset metrics
	m.Reset()

	// Verify reset worked
	snap = m.Snapshot()
	if snap.TotalOps != 0 {
		t.Errorf("Expected 0 ops after reset, got %d", snap.TotalOps)
	}
	if snap.TotalBytes != 0 {
		t.Errorf("Expected 0 bytes after reset, got %d", snap.TotalBytes)
	}
	if snap.MaxQueueDepth != 0 {
		t.Errorf("Expected 0 max queue depth after reset, got %d", snap.MaxQueueDepth)
	}
}

func TestObserver(t *testing.T) {
	// Test NoOpObserver doesn't panic
	observer := &NoOpObserver{}
	observer.ObserveRead(1024, 1000000, true)
	observer.ObserveWrite(1024, 1000000, true)
	observer.ObserveDiscard(1024, 1000000, true)
	observer.ObserveFlush(1000000, true)
	observer.ObserveQueueDepth(10)

	// Test MetricsObserver forwards to metrics
	m := NewMetrics()
	metricsObserver := NewMetricsObserver(m)

	metricsObserver.ObserveRead(1024, 1000000, true)
	metricsObserver.ObserveWrite(2048, 2000000, true)

	snap := m.Snapshot()
	if snap.ReadOps != 1 {
		t.Errorf("Expected 1 read op from observer, got %d", snap.ReadOps)
	}
	if snap.WriteOps != 1 {
		t.Errorf("Expected 1 write op from observer, got %d", snap.WriteOps)
	}
	if snap.ReadBytes != 1024 {
		t.Errorf("Expected 1024 read bytes from observer, got %d", snap.ReadBytes)
	}
	if snap.WriteBytes != 2048 {
		t.Errorf("Expected 2048 write bytes from observer, got %d", snap.WriteBytes)
	}
}

func TestMetricsRates(t *testing.T) {
	m := NewMetrics()

	// Simulate a known time period
	startTime := time.Now()
	m.StartTime.Store(startTime.UnixNano())

	// Record operations
	m.RecordRead(1024, 1000000, true)
	m.RecordWrite(2048, 2000000, true)

	// Simulate 1 second has passed
	stopTime := startTime.Add(1 * time.Second)
	m.StopTime.Store(stopTime.UnixNano())

	snap := m.Snapshot()

	// Check IOPS rates (should be 1 read/sec, 1 write/sec)
	if snap.ReadIOPS < 0.9 || snap.ReadIOPS > 1.1 {
		t.Errorf("Expected ReadIOPS ~1.0, got %.2f", snap.ReadIOPS)
	}
	if snap.WriteIOPS < 0.9 || snap.WriteIOPS > 1.1 {
		t.Errorf("Expected WriteIOPS ~1.0, got %.2f", snap.WriteIOPS)
	}

	// Check bandwidth rates (should be 1024 B/s read, 2048 B/s write)
	if snap.ReadBandwidth < 1000 || snap.ReadBandwidth > 1050 {
		t.Errorf("Expected ReadBandwidth ~1024, got %.2f", snap.ReadBandwidth)
	}
	if snap.WriteBandwidth < 2000 || snap.WriteBandwidth > 2100 {
		t.Errorf("Expected WriteBandwidth ~2048, got %.2f", snap.WriteBandwidth)
	}
}

func TestMetricsHistogram(t *testing.T) {
	m := NewMetrics()

	// Record operations with various latencies
	// 50 ops at 500us (50th percentile should be around 500us)
	// 49 ops at 5ms
	// 1 op at 50ms (99th percentile)
	for i := 0; i < 50; i++ {
		m.RecordRead(1024, 500_000, true) // 500us
	}
	for i := 0; i < 49; i++ {
		m.RecordWrite(1024, 5_000_000, true) // 5ms
	}
	m.RecordWrite(1024, 50_000_000, true) // 50ms (this is the P99)

	snap := m.Snapshot()

	// Total should be 100 ops
	if snap.TotalOps != 100 {
		t.Errorf("Expected 100 total ops, got %d", snap.TotalOps)
	}

	// P50 should be around 500us-1ms range (the 50th percentile)
	// With cumulative buckets, 50 ops at 500us means bucket[2] (100us) has 50
	if snap.LatencyP50Ns < 100_000 || snap.LatencyP50Ns > 1_000_000 {
		t.Errorf("Expected P50 in 100us-1ms range, got %d ns", snap.LatencyP50Ns)
	}

	// P99 should be in the 10ms-100ms range (99th percentile)
	if snap.LatencyP99Ns < 5_000_000 || snap.LatencyP99Ns > 100_000_000 {
		t.Errorf("Expected P99 in 5ms-100ms range, got %d ns", snap.LatencyP99Ns)
	}

	// Verify histogram buckets are populated
	totalInBuckets := uint64(0)
	for i := 0; i < len(snap.LatencyHistogram); i++ {
		totalInBuckets += snap.LatencyHistogram[i]
	}
	// Due to cumulative nature, total should be >= TotalOps
	if totalInBuckets == 0 {
		t.Error("Expected histogram buckets to be populated")
	}
}
