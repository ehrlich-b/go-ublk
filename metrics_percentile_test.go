package ublk

import "testing"

// This file pins calculatePercentile (metrics.go) to EXACT, hand-derived
// values, closing the gap left by TestMetricsHistogram, which only asserts
// wide ranges (metrics_test.go:237-243).
//
// Reference facts:
//   - LatencyBuckets (package var): {1_000, 10_000, 100_000, 1_000_000,
//     10_000_000, 100_000_000, 1_000_000_000, 10_000_000_000}, indices 0..7.
//   - m.LatencyBuckets[i] (struct field) holds CUMULATIVE counts of ops with
//     latency <= LatencyBuckets[i] (confirmed by recordLatency metrics.go:140).
//   - targetCount := uint64(float64(totalOps) * percentile)  -> truncates.
//   - The final interpolated value also goes through uint64(...) -> truncates.
//
// All expected values below are computed BY HAND from the algorithm; none are
// obtained by calling calculatePercentile.

// Item 1: exact NON-interpolated answer — the "bucketCount == prevCount"
// branch (metrics.go:291), which returns the boundary value itself.
//
// Analysis of reachability: the loop returns at the FIRST bucket i whose
// cumulative count satisfies bucketCount >= targetCount, so every earlier
// bucket j<i had count < targetCount, i.e. prevCount < targetCount.  Together
// with bucketCount >= targetCount that forces bucketCount > prevCount for any
// i>0, so this branch is ONLY reachable when the very first bucket matches
// with bucketCount == prevCount == 0, which requires targetCount == 0.
// targetCount == 0 happens whenever float64(totalOps)*percentile < 1.0.
func TestPercentile_NonInterpolatedExact(t *testing.T) {
	m := &Metrics{}
	m.OpCount.Store(1_000)
	m.LatencyBuckets[0].Store(0) // none <= 1us
	m.LatencyBuckets[1].Store(0) // none <= 10us
	m.LatencyBuckets[2].Store(200)
	m.LatencyBuckets[3].Store(400)
	m.LatencyBuckets[7].Store(1_000) // all 1000 ops <= 10s

	// percentile = 0.0005
	//   targetCount = uint64(float64(1000)*0.0005) = uint64(0.5) = 0
	// Loop, i=0: bucketCount = bucket[0] = 0
	//   0 >= targetCount(0) -> true; prevCount = 0 (i==0)
	//   bucketCount(0) == prevCount(0) -> return bucket = LatencyBuckets[0]
	//   => 1_000 ns
	got := m.calculatePercentile(0.0005)
	if got != 1_000 {
		t.Errorf("calculatePercentile(0.0005): bucketCount==prevCount branch expected 1_000 (1us boundary), got %d", got)
	}
}

// Item 2: genuine interpolation — two percentiles (0.50 and 0.99) against the
// SAME histogram, landing in DIFFERENT buckets (index 3 and index 7) and
// exercising different prevBucket/prevCount pairs.  The fractions are
// deliberately NON-0.5 (0.25 and 0.75) so the assertions stay sensitive to a
// "+/-" direction flip in the interpolation (a 0.5 fraction is symmetric
// under that mutation and would hide the bug).
//
// Histogram: OpCount = 10_000, cumulative counts:
//
//	[0]=1_000  [1]=2_000  [2]=4_000  [3]=8_000  [4]=8_500  [5]=9_000
//	[6]=9_600  [7]=10_000
//
// P50, percentile = 0.50:
//
//	targetCount = uint64(float64(10000)*0.50) = uint64(5000.0) = 5_000
//	buckets 0..2 (1_000, 2_000, 4_000) are all < 5_000; prevBucket walks
//	to 100_000.  bucket[3] = 8_000 >= 5_000 -> first match.
//	prevCount = bucket[2] = 4_000
//	fraction = (5_000-4_000)/(8_000-4_000) = 1_000/4_000 = 0.25 (exact float)
//	interpolate: prevBucket(100_000) + uint64(0.25 * (1_000_000-100_000))
//	           = 100_000 + uint64(0.25 * 900_000) = 100_000 + 225_000
//	=> 325_000 ns
//
// P99, percentile = 0.99:
//
//	targetCount = uint64(float64(10000)*0.99) = uint64(9900.0) = 9_900
//	buckets 0..6 (1_000..9_600) are all < 9_900; prevBucket walks to
//	1_000_000_000.  bucket[7] = 10_000 >= 9_900 -> first match.
//	prevCount = bucket[6] = 9_600
//	fraction = (9_900-9_600)/(10_000-9_600) = 300/400 = 0.75 (exact float)
//	interpolate: prevBucket(1_000_000_000) + uint64(0.75 * (10_000_000_000
//	             - 1_000_000_000)) = 1_000_000_000 + uint64(0.75 * 9_000_000_000)
//	           = 1_000_000_000 + 6_750_000_000
//	=> 7_750_000_000 ns
func TestPercentile_Interpolated(t *testing.T) {
	m := &Metrics{}
	m.OpCount.Store(10_000)
	m.LatencyBuckets[0].Store(1_000)
	m.LatencyBuckets[1].Store(2_000)
	m.LatencyBuckets[2].Store(4_000)
	m.LatencyBuckets[3].Store(8_000)
	m.LatencyBuckets[4].Store(8_500)
	m.LatencyBuckets[5].Store(9_000)
	m.LatencyBuckets[6].Store(9_600)
	m.LatencyBuckets[7].Store(10_000)

	if got := m.calculatePercentile(0.50); got != 325_000 {
		t.Errorf("calculatePercentile(0.50): expected 325_000 ns (hand-derived), got %d", got)
	}
	if got := m.calculatePercentile(0.99); got != 7_750_000_000 {
		t.Errorf("calculatePercentile(0.99): expected 7_750_000_000 ns (hand-derived), got %d", got)
	}
}

// Item 2 supplement: a case where float truncation of the interpolated value
// MATTERS — the hand arithmetic must truncate, not round, or it is off by one.
//
// Histogram: OpCount = 10, cumulative counts:
//
//	[0]=0  [1]=0  [2..7]=10   (all 10 ops have latency in (10us, 100us])
//
// percentile = 0.70:
//
//	targetCount = uint64(float64(10)*0.70) = uint64(7.0) = 7
//	buckets 0..1 (0, 0) are < 7; prevBucket walks to 10_000.
//	bucket[2] = 10 >= 7 -> first match.  prevCount = bucket[1] = 0.
//	fraction = (7-0)/(10-0) = 0.7
//	           float64(0.7) = 0.6999999999999999555910790149937383830547332763671875
//	interpolate: prevBucket(10_000) + uint64(0.7 * (100_000-10_000))
//	           = 10_000 + uint64(0.7 * 90_000)
//	Real arithmetic: 0.7 * 90_000 = 63_000 exactly.
//	IEEE float64: 0.6999999999999999556 * 90_000 = 62_999.99999999999 (just
//	under 63_000) -> uint64 truncates toward zero -> 62_999, NOT 63_000.
//	=> 10_000 + 62_999 = 72_999 ns
func TestPercentile_Truncation(t *testing.T) {
	m := &Metrics{}
	m.OpCount.Store(10)
	for i := 2; i < numLatencyBuckets; i++ {
		m.LatencyBuckets[i].Store(10)
	}
	// bucket[0] and bucket[1] left at zero value.

	if got := m.calculatePercentile(0.70); got != 72_999 {
		t.Errorf("calculatePercentile(0.70): expected 72_999 ns (truncation of 0.7*90000 -> 62999), got %d", got)
	}
}

// Item 3: empty histogram — calculatePercentile returns 0 immediately because
// of the totalOps == 0 guard (metrics.go:275), regardless of percentile.
func TestPercentile_EmptyHistogram(t *testing.T) {
	m := &Metrics{} // zero value: OpCount == 0, all buckets 0

	for _, p := range []float64{0.0, 0.50, 0.99, 1.0} {
		if got := m.calculatePercentile(p); got != 0 {
			t.Errorf("calculatePercentile(%v) with OpCount==0: expected 0, got %d", p, got)
		}
	}
}

// Item 4: fallthrough — every bucket's cumulative count is below targetCount,
// so the loop never satisfies bucketCount >= targetCount and the function
// returns LatencyBuckets[numLatencyBuckets-1] (metrics.go:302).
//
// Histogram: OpCount = 10_000, only bucket[7] populated (9_000), all others 0.
//
// percentile = 0.99:
//
//	targetCount = uint64(float64(10000)*0.99) = uint64(9900.0) = 9_900
//	bucket[0..6] = 0 < 9_900; bucket[7] = 9_000 < 9_900 as well
//	(9_000 is deliberately below OpCount*percentile = 9_900).
//	No bucket satisfies >= 9_900 -> fall off the loop.
//	=> LatencyBuckets[numLatencyBuckets-1] = 10_000_000_000 ns (10s)
func TestPercentile_ExceedsEveryBucket(t *testing.T) {
	m := &Metrics{}
	m.OpCount.Store(10_000)
	m.LatencyBuckets[7].Store(9_000)

	if got := m.calculatePercentile(0.99); got != 10_000_000_000 {
		t.Errorf("calculatePercentile(0.99): expected fallthrough 10_000_000_000 ns, got %d", got)
	}
}

// Item 5: percentiles 0.0 and 1.0 (legal inputs; nothing rejects them) on the
// same histogram as TestPercentile_Interpolated.
//
// percentile = 0.0: targetCount = uint64(float64(10000)*0.0) = 0
//
//	i=0: bucket[0] = 1_000 >= 0 -> first match.  prevCount = 0 (i==0).
//	bucketCount(1_000) != prevCount(0) -> interpolate:
//	fraction = (0-0)/(1_000-0) = 0.0
//	return prevBucket(0) + uint64(0.0 * (1_000-0)) = 0
//	=> 0 ns
//
// percentile = 1.0: targetCount = uint64(float64(10000)*1.0) = 10_000
//
//	buckets 0..6 (1_000..9_600) all < 10_000; prevBucket walks to
//	1_000_000_000.  bucket[7] = 10_000 >= 10_000 -> first match.
//	prevCount = bucket[6] = 9_600.
//	fraction = (10_000-9_600)/(10_000-9_600) = 400/400 = 1.0  (exact float)
//	return prevBucket(1_000_000_000) + uint64(1.0 * (10_000_000_000
//	       - 1_000_000_000)) = 1_000_000_000 + 9_000_000_000
//	=> 10_000_000_000 ns
func TestPercentile_ZeroAndOne(t *testing.T) {
	m := &Metrics{}
	m.OpCount.Store(10_000)
	m.LatencyBuckets[0].Store(1_000)
	m.LatencyBuckets[1].Store(2_000)
	m.LatencyBuckets[2].Store(4_000)
	m.LatencyBuckets[3].Store(8_000)
	m.LatencyBuckets[4].Store(8_500)
	m.LatencyBuckets[5].Store(9_000)
	m.LatencyBuckets[6].Store(9_600)
	m.LatencyBuckets[7].Store(10_000)

	if got := m.calculatePercentile(0.0); got != 0 {
		t.Errorf("calculatePercentile(0.0): expected 0 ns, got %d", got)
	}
	if got := m.calculatePercentile(1.0); got != 10_000_000_000 {
		t.Errorf("calculatePercentile(1.0): expected 10_000_000_000 ns, got %d", got)
	}
}

// Item 6: cross-check Snapshot() against direct calculatePercentile calls on
// the SAME histogram, AND against the hand-derived literals from the
// interpolated scenario.  For P999 (not hand-derived elsewhere) we assert the
// two code paths agree and match the hand-derived literal worked out here:
//
// percentile = 0.999:
//
//	targetCount = uint64(float64(10000)*0.999) = uint64(9990.0) = 9_990
//	bucket[6] = 9_600 < 9_990 -> prevBucket = 1_000_000_000
//	bucket[7] = 10_000 >= 9_990 -> prevCount = 9_600
//	fraction = (9_990-9_600)/(10_000-9_600) = 390/400 = 0.975
//	return 1_000_000_000 + uint64(0.975 * 9_000_000_000)
//	     = 1_000_000_000 + 8_775_000_000
//	(float64 0.975*9e9 == 8_775_000_000.0, truncates cleanly)
//	=> 9_775_000_000 ns
func TestPercentile_SnapshotMatch(t *testing.T) {
	m := &Metrics{}
	m.OpCount.Store(10_000)
	m.LatencyBuckets[0].Store(1_000)
	m.LatencyBuckets[1].Store(2_000)
	m.LatencyBuckets[2].Store(4_000)
	m.LatencyBuckets[3].Store(8_000)
	m.LatencyBuckets[4].Store(8_500)
	m.LatencyBuckets[5].Store(9_000)
	m.LatencyBuckets[6].Store(9_600)
	m.LatencyBuckets[7].Store(10_000)

	snap := m.Snapshot()

	// Snapshot wires to the same function our exact tests exercise directly.
	if snap.LatencyP50Ns != m.calculatePercentile(0.50) {
		t.Errorf("Snapshot LatencyP50Ns (%d) != calculatePercentile(0.50) (%d)", snap.LatencyP50Ns, m.calculatePercentile(0.50))
	}
	if snap.LatencyP99Ns != m.calculatePercentile(0.99) {
		t.Errorf("Snapshot LatencyP99Ns (%d) != calculatePercentile(0.99) (%d)", snap.LatencyP99Ns, m.calculatePercentile(0.99))
	}
	if snap.LatencyP999Ns != m.calculatePercentile(0.999) {
		t.Errorf("Snapshot LatencyP999Ns (%d) != calculatePercentile(0.999) (%d)", snap.LatencyP999Ns, m.calculatePercentile(0.999))
	}

	// And Snapshot agrees with the hand-derived exact values.
	if snap.LatencyP50Ns != 325_000 {
		t.Errorf("Snapshot LatencyP50Ns: expected 325_000 ns, got %d", snap.LatencyP50Ns)
	}
	if snap.LatencyP99Ns != 7_750_000_000 {
		t.Errorf("Snapshot LatencyP99Ns: expected 7_750_000_000 ns, got %d", snap.LatencyP99Ns)
	}
	if snap.LatencyP999Ns != 9_775_000_000 {
		t.Errorf("Snapshot LatencyP999Ns: expected 9_775_000_000 ns, got %d", snap.LatencyP999Ns)
	}
}
