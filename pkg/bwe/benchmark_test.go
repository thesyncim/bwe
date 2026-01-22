// Package bwe benchmarks for allocation verification.
//
// Allocation Benchmarks for PERF-01 Verification
// ===============================================
//
// These benchmarks verify that steady-state packet processing allocates
// less than 1 object per packet, as required by PERF-01.
//
// How to run:
//
//	go test -bench=ZeroAlloc -benchmem ./pkg/bwe/...
//
// Expected output: 0 allocs/op in steady state
//
// How to debug allocation failures:
//
//	go build -gcflags="-m" ./pkg/bwe 2>&1 | grep -E "(escapes|moved to heap)"
//
// Escape Analysis Findings
// ========================
// This section documents heap escapes identified in the hot path.
// (Updated after running escape analysis in Task 3)
//
// Acceptable escapes:
// - sync.Pool.Get returns interface{} (required for pool pattern)
// - time.Time values are small structs passed by value
//
// No escapes expected in:
// - BandwidthEstimator.OnPacket (hot path)
// - DelayEstimator.OnPacket (hot path)
// - InterArrivalCalculator.AddPacket (hot path)
// - KalmanFilter.Update (hot path)
// - TrendlineEstimator.Update (hot path)
// - OveruseDetector.Detect (hot path)
package bwe

import (
	"testing"
	"time"

	"bwe/pkg/bwe/internal"
)

// benchResult is a package-level variable to prevent compiler optimizations
// from eliminating benchmark loops that produce unused results.
var benchResult int64

// benchUsage is a package-level variable for BandwidthUsage results.
var benchUsage BandwidthUsage

// BenchmarkBandwidthEstimator_OnPacket_ZeroAlloc benchmarks the main OnPacket
// method of BandwidthEstimator for zero allocations.
//
// This is the most critical benchmark for PERF-01. In steady state (after
// initial warmup), OnPacket should allocate zero objects per operation.
//
// Target: 0 allocs/op
func BenchmarkBandwidthEstimator_OnPacket_ZeroAlloc(b *testing.B) {
	b.ReportAllocs()

	config := DefaultBandwidthEstimatorConfig()
	clock := internal.NewMockClock(time.Now())
	estimator := NewBandwidthEstimator(config, clock)

	// Warmup: process enough packets to initialize internal state
	// This ensures the sliding window buffers are preallocated
	sendTime := uint32(0)
	for i := 0; i < 1000; i++ {
		pkt := PacketInfo{
			ArrivalTime: clock.Now(),
			SendTime:    sendTime,
			Size:        1200,
			SSRC:        0x12345678,
		}
		estimator.OnPacket(pkt)
		sendTime += 262 // ~1ms in abs-send-time units
		clock.Advance(time.Millisecond)
	}

	// Reset timer after warmup
	b.ResetTimer()

	// Benchmark loop: measure steady-state allocations
	for i := 0; i < b.N; i++ {
		pkt := PacketInfo{
			ArrivalTime: clock.Now(),
			SendTime:    sendTime,
			Size:        1200,
			SSRC:        0x12345678,
		}
		benchResult = estimator.OnPacket(pkt)
		sendTime += 262
		clock.Advance(time.Millisecond)
	}
}

// BenchmarkDelayEstimator_OnPacket_ZeroAlloc benchmarks the delay estimator
// component for zero allocations.
//
// This verifies no allocations in the filter/detector chain:
// - InterArrivalCalculator (burst grouping)
// - KalmanFilter or TrendlineEstimator (noise reduction)
// - OveruseDetector (congestion detection)
//
// Target: 0 allocs/op
func BenchmarkDelayEstimator_OnPacket_ZeroAlloc(b *testing.B) {
	b.ReportAllocs()

	config := DefaultDelayEstimatorConfig()
	clock := internal.NewMockClock(time.Now())
	estimator := NewDelayEstimator(config, clock)

	// Warmup
	sendTime := uint32(0)
	for i := 0; i < 1000; i++ {
		pkt := PacketInfo{
			ArrivalTime: clock.Now(),
			SendTime:    sendTime,
			Size:        1200,
			SSRC:        0x12345678,
		}
		estimator.OnPacket(pkt)
		sendTime += 262
		clock.Advance(time.Millisecond)
	}

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		pkt := PacketInfo{
			ArrivalTime: clock.Now(),
			SendTime:    sendTime,
			Size:        1200,
			SSRC:        0x12345678,
		}
		benchUsage = estimator.OnPacket(pkt)
		sendTime += 262
		clock.Advance(time.Millisecond)
	}
}

// BenchmarkDelayEstimator_Kalman_ZeroAlloc specifically benchmarks the
// Kalman filter variant of the delay estimator.
//
// Target: 0 allocs/op
func BenchmarkDelayEstimator_Kalman_ZeroAlloc(b *testing.B) {
	b.ReportAllocs()

	config := DefaultDelayEstimatorConfig()
	config.FilterType = FilterKalman
	clock := internal.NewMockClock(time.Now())
	estimator := NewDelayEstimator(config, clock)

	// Warmup
	sendTime := uint32(0)
	for i := 0; i < 1000; i++ {
		pkt := PacketInfo{
			ArrivalTime: clock.Now(),
			SendTime:    sendTime,
			Size:        1200,
			SSRC:        0x12345678,
		}
		estimator.OnPacket(pkt)
		sendTime += 262
		clock.Advance(time.Millisecond)
	}

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		pkt := PacketInfo{
			ArrivalTime: clock.Now(),
			SendTime:    sendTime,
			Size:        1200,
			SSRC:        0x12345678,
		}
		benchUsage = estimator.OnPacket(pkt)
		sendTime += 262
		clock.Advance(time.Millisecond)
	}
}

// BenchmarkDelayEstimator_Trendline_ZeroAlloc specifically benchmarks the
// Trendline filter variant of the delay estimator.
//
// Target: 0 allocs/op
func BenchmarkDelayEstimator_Trendline_ZeroAlloc(b *testing.B) {
	b.ReportAllocs()

	config := DefaultDelayEstimatorConfig()
	config.FilterType = FilterTrendline
	clock := internal.NewMockClock(time.Now())
	estimator := NewDelayEstimator(config, clock)

	// Warmup
	sendTime := uint32(0)
	for i := 0; i < 1000; i++ {
		pkt := PacketInfo{
			ArrivalTime: clock.Now(),
			SendTime:    sendTime,
			Size:        1200,
			SSRC:        0x12345678,
		}
		estimator.OnPacket(pkt)
		sendTime += 262
		clock.Advance(time.Millisecond)
	}

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		pkt := PacketInfo{
			ArrivalTime: clock.Now(),
			SendTime:    sendTime,
			Size:        1200,
			SSRC:        0x12345678,
		}
		benchUsage = estimator.OnPacket(pkt)
		sendTime += 262
		clock.Advance(time.Millisecond)
	}
}

// BenchmarkRateStats_Update_ZeroAlloc benchmarks the rate statistics
// sliding window update for zero allocations.
//
// Target: 0 allocs/op
func BenchmarkRateStats_Update_ZeroAlloc(b *testing.B) {
	b.ReportAllocs()

	config := DefaultRateStatsConfig()
	stats := NewRateStats(config)

	// Warmup to fill sliding window
	now := time.Now()
	for i := 0; i < 1000; i++ {
		stats.Update(1200, now)
		now = now.Add(time.Millisecond)
	}

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		stats.Update(1200, now)
		now = now.Add(time.Millisecond)
	}
}

// BenchmarkRateController_Update_ZeroAlloc benchmarks the AIMD rate
// controller update for zero allocations.
//
// Target: 0 allocs/op
func BenchmarkRateController_Update_ZeroAlloc(b *testing.B) {
	b.ReportAllocs()

	config := DefaultRateControllerConfig()
	controller := NewRateController(config)

	now := time.Now()

	// Warmup
	for i := 0; i < 100; i++ {
		controller.Update(BwNormal, 1_000_000, now)
		now = now.Add(100 * time.Millisecond)
	}

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		// Alternate between signals to exercise all code paths
		signal := BandwidthUsage(i % 3)
		benchResult = controller.Update(signal, 1_000_000, now)
		now = now.Add(100 * time.Millisecond)
	}
}

// BenchmarkKalmanFilter_Update_ZeroAlloc benchmarks the Kalman filter
// update for zero allocations.
//
// Target: 0 allocs/op
func BenchmarkKalmanFilter_Update_ZeroAlloc(b *testing.B) {
	b.ReportAllocs()

	config := DefaultKalmanConfig()
	filter := NewKalmanFilter(config)

	// Warmup
	for i := 0; i < 1000; i++ {
		filter.Update(float64(i%10) * 0.1)
	}

	b.ResetTimer()

	var result float64
	for i := 0; i < b.N; i++ {
		result = filter.Update(float64(i%10) * 0.1)
	}
	_ = result
}

// BenchmarkTrendlineEstimator_Update_ZeroAlloc benchmarks the Trendline
// estimator update for zero allocations.
//
// Target: 0 allocs/op
func BenchmarkTrendlineEstimator_Update_ZeroAlloc(b *testing.B) {
	b.ReportAllocs()

	config := DefaultTrendlineConfig()
	estimator := NewTrendlineEstimator(config)

	now := time.Now()

	// Warmup
	for i := 0; i < 1000; i++ {
		estimator.Update(now, float64(i%10)*0.1)
		now = now.Add(time.Millisecond)
	}

	b.ResetTimer()

	var result float64
	for i := 0; i < b.N; i++ {
		result = estimator.Update(now, float64(i%10)*0.1)
		now = now.Add(time.Millisecond)
	}
	_ = result
}

// BenchmarkOveruseDetector_Detect_ZeroAlloc benchmarks the overuse detector
// for zero allocations.
//
// Target: 0 allocs/op
func BenchmarkOveruseDetector_Detect_ZeroAlloc(b *testing.B) {
	b.ReportAllocs()

	config := DefaultOveruseConfig()
	clock := internal.NewMockClock(time.Now())
	detector := NewOveruseDetector(config, clock)

	// Warmup
	for i := 0; i < 1000; i++ {
		detector.Detect(float64(i%10) * 0.1)
		clock.Advance(time.Millisecond)
	}

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		benchUsage = detector.Detect(float64(i%10) * 0.1)
		clock.Advance(time.Millisecond)
	}
}

// BenchmarkInterArrivalCalculator_AddPacket_ZeroAlloc benchmarks the
// inter-arrival calculator for zero allocations.
//
// Target: 0 allocs/op
func BenchmarkInterArrivalCalculator_AddPacket_ZeroAlloc(b *testing.B) {
	b.ReportAllocs()

	calc := NewInterArrivalCalculator(5 * time.Millisecond)

	now := time.Now()
	sendTime := uint32(0)

	// Warmup
	for i := 0; i < 1000; i++ {
		pkt := PacketInfo{
			ArrivalTime: now,
			SendTime:    sendTime,
			Size:        1200,
			SSRC:        0x12345678,
		}
		calc.AddPacket(pkt)
		sendTime += 262
		now = now.Add(time.Millisecond)
	}

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		pkt := PacketInfo{
			ArrivalTime: now,
			SendTime:    sendTime,
			Size:        1200,
			SSRC:        0x12345678,
		}
		_, _ = calc.AddPacket(pkt)
		sendTime += 262
		now = now.Add(time.Millisecond)
	}
}
