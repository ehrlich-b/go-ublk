package ublk

import (
	"sync/atomic"
	"time"
)

// LatencyBuckets defines the latency histogram buckets in nanoseconds.
// Buckets cover from 1us to 10s with logarithmic spacing.
var LatencyBuckets = []uint64{
	1_000,        // 1us
	10_000,       // 10us
	100_000,      // 100us
	1_000_000,    // 1ms
	10_000_000,   // 10ms
	100_000_000,  // 100ms
	1_000_000_000, // 1s
	10_000_000_000, // 10s
}

const numLatencyBuckets = 8

// Metrics tracks performance and operational statistics for ublk devices
type Metrics struct {
	// I/O operation counters
	ReadOps    atomic.Uint64 // Total read operations
	WriteOps   atomic.Uint64 // Total write operations
	DiscardOps atomic.Uint64 // Total discard operations
	FlushOps   atomic.Uint64 // Total flush operations

	// Byte counters
	ReadBytes    atomic.Uint64 // Total bytes read
	WriteBytes   atomic.Uint64 // Total bytes written
	DiscardBytes atomic.Uint64 // Total bytes discarded

	// Error counters
	ReadErrors    atomic.Uint64 // Read operation errors
	WriteErrors   atomic.Uint64 // Write operation errors
	DiscardErrors atomic.Uint64 // Discard operation errors
	FlushErrors   atomic.Uint64 // Flush operation errors

	// Queue statistics
	QueueDepthTotal atomic.Uint64 // Cumulative queue depth samples
	QueueDepthCount atomic.Uint64 // Number of queue depth measurements
	MaxQueueDepth   atomic.Uint32 // Maximum observed queue depth

	// Performance tracking
	TotalLatencyNs atomic.Uint64 // Cumulative operation latency in nanoseconds
	OpCount        atomic.Uint64 // Total operations (for average latency calculation)

	// Latency histogram buckets (cumulative counts)
	// Each bucket[i] contains the count of operations with latency <= LatencyBuckets[i]
	LatencyBuckets [numLatencyBuckets]atomic.Uint64

	// Device lifecycle
	StartTime atomic.Int64 // Device start timestamp (UnixNano)
	StopTime  atomic.Int64 // Device stop timestamp (UnixNano)
}

// NewMetrics creates a new metrics instance
func NewMetrics() *Metrics {
	m := &Metrics{}
	m.StartTime.Store(time.Now().UnixNano())
	return m
}

// RecordRead records a read operation
func (m *Metrics) RecordRead(bytes uint64, latencyNs uint64, success bool) {
	m.ReadOps.Add(1)
	if success {
		m.ReadBytes.Add(bytes)
	} else {
		m.ReadErrors.Add(1)
	}
	m.recordLatency(latencyNs)
}

// RecordWrite records a write operation
func (m *Metrics) RecordWrite(bytes uint64, latencyNs uint64, success bool) {
	m.WriteOps.Add(1)
	if success {
		m.WriteBytes.Add(bytes)
	} else {
		m.WriteErrors.Add(1)
	}
	m.recordLatency(latencyNs)
}

// RecordDiscard records a discard operation
func (m *Metrics) RecordDiscard(bytes uint64, latencyNs uint64, success bool) {
	m.DiscardOps.Add(1)
	if success {
		m.DiscardBytes.Add(bytes)
	} else {
		m.DiscardErrors.Add(1)
	}
	m.recordLatency(latencyNs)
}

// RecordFlush records a flush operation
func (m *Metrics) RecordFlush(latencyNs uint64, success bool) {
	m.FlushOps.Add(1)
	if !success {
		m.FlushErrors.Add(1)
	}
	m.recordLatency(latencyNs)
}

// RecordQueueDepth records current queue depth for statistics
func (m *Metrics) RecordQueueDepth(depth uint32) {
	m.QueueDepthTotal.Add(uint64(depth))
	m.QueueDepthCount.Add(1)

	// Update max queue depth atomically
	for {
		current := m.MaxQueueDepth.Load()
		if depth <= current {
			break
		}
		if m.MaxQueueDepth.CompareAndSwap(current, depth) {
			break
		}
	}
}

// recordLatency records operation latency and updates histogram
func (m *Metrics) recordLatency(latencyNs uint64) {
	m.TotalLatencyNs.Add(latencyNs)
	m.OpCount.Add(1)

	// Update histogram buckets (cumulative)
	for i, bucket := range LatencyBuckets {
		if latencyNs <= bucket {
			m.LatencyBuckets[i].Add(1)
		}
	}
}

// Stop marks the device as stopped
func (m *Metrics) Stop() {
	m.StopTime.Store(time.Now().UnixNano())
}

// Snapshot returns a point-in-time snapshot of metrics
type MetricsSnapshot struct {
	// I/O operations
	ReadOps    uint64
	WriteOps   uint64
	DiscardOps uint64
	FlushOps   uint64

	// Bytes transferred
	ReadBytes    uint64
	WriteBytes   uint64
	DiscardBytes uint64

	// Error counts
	ReadErrors    uint64
	WriteErrors   uint64
	DiscardErrors uint64
	FlushErrors   uint64

	// Queue statistics
	AvgQueueDepth float64
	MaxQueueDepth uint32

	// Performance
	AvgLatencyNs uint64
	UptimeNs     uint64

	// Latency percentiles (in nanoseconds)
	LatencyP50Ns  uint64 // 50th percentile (median)
	LatencyP99Ns  uint64 // 99th percentile
	LatencyP999Ns uint64 // 99.9th percentile

	// Histogram bucket counts (cumulative)
	LatencyHistogram [numLatencyBuckets]uint64

	// Computed statistics
	ReadIOPS       float64 // Operations per second
	WriteIOPS      float64
	ReadBandwidth  float64 // Bytes per second
	WriteBandwidth float64
	TotalOps       uint64
	TotalBytes     uint64
	ErrorRate      float64 // Percentage of failed operations
}

// Snapshot creates a point-in-time snapshot of metrics
func (m *Metrics) Snapshot() MetricsSnapshot {
	snap := MetricsSnapshot{
		ReadOps:       m.ReadOps.Load(),
		WriteOps:      m.WriteOps.Load(),
		DiscardOps:    m.DiscardOps.Load(),
		FlushOps:      m.FlushOps.Load(),
		ReadBytes:     m.ReadBytes.Load(),
		WriteBytes:    m.WriteBytes.Load(),
		DiscardBytes:  m.DiscardBytes.Load(),
		ReadErrors:    m.ReadErrors.Load(),
		WriteErrors:   m.WriteErrors.Load(),
		DiscardErrors: m.DiscardErrors.Load(),
		FlushErrors:   m.FlushErrors.Load(),
		MaxQueueDepth: m.MaxQueueDepth.Load(),
	}

	// Calculate derived statistics
	snap.TotalOps = snap.ReadOps + snap.WriteOps + snap.DiscardOps + snap.FlushOps
	snap.TotalBytes = snap.ReadBytes + snap.WriteBytes + snap.DiscardBytes

	// Calculate average queue depth
	queueDepthTotal := m.QueueDepthTotal.Load()
	queueDepthCount := m.QueueDepthCount.Load()
	if queueDepthCount > 0 {
		snap.AvgQueueDepth = float64(queueDepthTotal) / float64(queueDepthCount)
	}

	// Calculate average latency
	totalLatencyNs := m.TotalLatencyNs.Load()
	opCount := m.OpCount.Load()
	if opCount > 0 {
		snap.AvgLatencyNs = totalLatencyNs / opCount
	}

	// Calculate uptime
	startTime := m.StartTime.Load()
	stopTime := m.StopTime.Load()
	if stopTime > 0 {
		snap.UptimeNs = uint64(stopTime - startTime)
	} else {
		snap.UptimeNs = uint64(time.Now().UnixNano() - startTime)
	}

	// Calculate rates (operations and bandwidth per second)
	if snap.UptimeNs > 0 {
		uptimeSeconds := float64(snap.UptimeNs) / 1e9
		snap.ReadIOPS = float64(snap.ReadOps) / uptimeSeconds
		snap.WriteIOPS = float64(snap.WriteOps) / uptimeSeconds
		snap.ReadBandwidth = float64(snap.ReadBytes) / uptimeSeconds
		snap.WriteBandwidth = float64(snap.WriteBytes) / uptimeSeconds
	}

	// Calculate error rate
	totalErrors := snap.ReadErrors + snap.WriteErrors + snap.DiscardErrors + snap.FlushErrors
	if snap.TotalOps > 0 {
		snap.ErrorRate = float64(totalErrors) / float64(snap.TotalOps) * 100.0
	}

	// Copy histogram bucket counts
	for i := 0; i < numLatencyBuckets; i++ {
		snap.LatencyHistogram[i] = m.LatencyBuckets[i].Load()
	}

	// Calculate percentiles from histogram
	if opCount > 0 {
		snap.LatencyP50Ns = m.calculatePercentile(0.50)
		snap.LatencyP99Ns = m.calculatePercentile(0.99)
		snap.LatencyP999Ns = m.calculatePercentile(0.999)
	}

	return snap
}

// calculatePercentile estimates the latency at the given percentile (0.0-1.0)
// using linear interpolation between histogram buckets.
func (m *Metrics) calculatePercentile(percentile float64) uint64 {
	totalOps := m.OpCount.Load()
	if totalOps == 0 {
		return 0
	}

	targetCount := uint64(float64(totalOps) * percentile)

	// Find the bucket containing the target percentile
	prevBucket := uint64(0)
	for i, bucket := range LatencyBuckets {
		bucketCount := m.LatencyBuckets[i].Load()
		if bucketCount >= targetCount {
			// Linear interpolation within bucket
			prevCount := uint64(0)
			if i > 0 {
				prevCount = m.LatencyBuckets[i-1].Load()
			}
			if bucketCount == prevCount {
				return bucket
			}
			// Interpolate between prevBucket and bucket
			fraction := float64(targetCount-prevCount) / float64(bucketCount-prevCount)
			return prevBucket + uint64(fraction*float64(bucket-prevBucket))
		}
		prevBucket = bucket
	}

	// If we get here, the latency exceeds all buckets
	return LatencyBuckets[numLatencyBuckets-1]
}

// Reset resets all metrics counters (useful for testing)
func (m *Metrics) Reset() {
	m.ReadOps.Store(0)
	m.WriteOps.Store(0)
	m.DiscardOps.Store(0)
	m.FlushOps.Store(0)
	m.ReadBytes.Store(0)
	m.WriteBytes.Store(0)
	m.DiscardBytes.Store(0)
	m.ReadErrors.Store(0)
	m.WriteErrors.Store(0)
	m.DiscardErrors.Store(0)
	m.FlushErrors.Store(0)
	m.QueueDepthTotal.Store(0)
	m.QueueDepthCount.Store(0)
	m.MaxQueueDepth.Store(0)
	m.TotalLatencyNs.Store(0)
	m.OpCount.Store(0)
	for i := 0; i < numLatencyBuckets; i++ {
		m.LatencyBuckets[i].Store(0)
	}
	m.StartTime.Store(time.Now().UnixNano())
	m.StopTime.Store(0)
}

// Observer interface allows pluggable metrics collection
type Observer interface {
	// ObserveRead is called for each read operation
	ObserveRead(bytes uint64, latencyNs uint64, success bool)

	// ObserveWrite is called for each write operation
	ObserveWrite(bytes uint64, latencyNs uint64, success bool)

	// ObserveDiscard is called for each discard operation
	ObserveDiscard(bytes uint64, latencyNs uint64, success bool)

	// ObserveFlush is called for each flush operation
	ObserveFlush(latencyNs uint64, success bool)

	// ObserveQueueDepth is called periodically with current queue depth
	ObserveQueueDepth(depth uint32)
}

// NoOpObserver is a no-op implementation of Observer
type NoOpObserver struct{}

func (NoOpObserver) ObserveRead(uint64, uint64, bool)     {}
func (NoOpObserver) ObserveWrite(uint64, uint64, bool)    {}
func (NoOpObserver) ObserveDiscard(uint64, uint64, bool)  {}
func (NoOpObserver) ObserveFlush(uint64, bool)            {}
func (NoOpObserver) ObserveQueueDepth(uint32)             {}

// MetricsObserver implements Observer using the built-in Metrics
type MetricsObserver struct {
	metrics *Metrics
}

// NewMetricsObserver creates an observer that records to the given metrics
func NewMetricsObserver(m *Metrics) *MetricsObserver {
	return &MetricsObserver{metrics: m}
}

func (o *MetricsObserver) ObserveRead(bytes uint64, latencyNs uint64, success bool) {
	o.metrics.RecordRead(bytes, latencyNs, success)
}

func (o *MetricsObserver) ObserveWrite(bytes uint64, latencyNs uint64, success bool) {
	o.metrics.RecordWrite(bytes, latencyNs, success)
}

func (o *MetricsObserver) ObserveDiscard(bytes uint64, latencyNs uint64, success bool) {
	o.metrics.RecordDiscard(bytes, latencyNs, success)
}

func (o *MetricsObserver) ObserveFlush(latencyNs uint64, success bool) {
	o.metrics.RecordFlush(latencyNs, success)
}

func (o *MetricsObserver) ObserveQueueDepth(depth uint32) {
	o.metrics.RecordQueueDepth(depth)
}

// Compile-time interface check
var _ Observer = (*MetricsObserver)(nil)
var _ Observer = (*NoOpObserver)(nil)