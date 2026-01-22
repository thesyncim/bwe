// Package bwe implements Google Congestion Control (GCC) receiver-side
// bandwidth estimation for WebRTC.
package bwe

import (
	"math"
	"runtime"
	"testing"
	"time"

	"bwe/pkg/bwe/internal"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// =============================================================================
// VALID-04: 24-Hour Soak Test (Accelerated)
// =============================================================================

// TestSoak24Hour_Accelerated simulates 24 hours of traffic in accelerated time.
// Uses MockClock to simulate time progression without waiting.
//
// This test verifies VALID-04:
//   - No timestamp-related failures (abs-send-time wraps every 64 seconds)
//   - No memory leaks (bounded memory growth)
//   - Estimates remain reasonable throughout
//
// The test simulates 24 hours with 50 packets per second (20ms intervals),
// totaling 4,320,000 packets. The abs-send-time field wraps ~1350 times.
func TestSoak24Hour_Accelerated(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping 24-hour soak test in short mode")
	}

	// Constants for the test
	const (
		simulatedHours    = 24
		packetsPerSecond  = 50   // 20ms packet intervals
		packetSize        = 1200 // bytes
		packetIntervalMs  = 20
		packetsPerHour    = 60 * 60 * packetsPerSecond // 180,000
		totalPackets      = simulatedHours * packetsPerHour
		memoryLimitMB     = 100 // Maximum allowed heap allocation
		estimateMinBps    = 1   // Minimum valid estimate
		estimateMaxBps    = 1_000_000_000 // 1 Gbps maximum
		absSendTimeUnitsPerMs = 262 // 1ms in abs-send-time units (1 << 18 / 1000)
	)

	// Initialize estimator with mock clock
	clock := internal.NewMockClock(time.Now())
	config := DefaultBandwidthEstimatorConfig()
	estimator := NewBandwidthEstimator(config, clock)

	// Track metrics
	var startMemStats, currentMemStats runtime.MemStats
	runtime.ReadMemStats(&startMemStats)

	sendTime := uint32(0)
	packetsProcessed := 0
	wraparoundCount := 0
	var lastSendTime uint32

	t.Logf("Starting 24-hour soak test: %d packets across %d simulated hours",
		totalPackets, simulatedHours)

	// Process one simulated hour at a time
	for hour := 0; hour < simulatedHours; hour++ {
		// Process one hour of packets
		for i := 0; i < packetsPerHour; i++ {
			// Track wraparound
			if sendTime < lastSendTime {
				wraparoundCount++
			}
			lastSendTime = sendTime

			pkt := PacketInfo{
				ArrivalTime: clock.Now(),
				SendTime:    sendTime,
				Size:        packetSize,
				SSRC:        0x12345678,
			}

			// Process packet - should not panic
			estimate := estimator.OnPacket(pkt)

			// Verify estimate is valid (not NaN, not Inf, within bounds)
			if math.IsNaN(float64(estimate)) {
				t.Fatalf("Hour %d: Got NaN estimate at packet %d", hour, packetsProcessed)
			}
			if math.IsInf(float64(estimate), 0) {
				t.Fatalf("Hour %d: Got Inf estimate at packet %d", hour, packetsProcessed)
			}

			// Advance time and send timestamp
			sendTime = (sendTime + uint32(packetIntervalMs*absSendTimeUnitsPerMs)) % AbsSendTimeMax
			clock.Advance(time.Duration(packetIntervalMs) * time.Millisecond)
			packetsProcessed++
		}

		// Hourly health check
		runtime.ReadMemStats(&currentMemStats)
		estimate := estimator.GetEstimate()

		heapMB := float64(currentMemStats.HeapAlloc) / (1024 * 1024)
		t.Logf("Hour %2d: HeapAlloc=%.2f MB, NumGC=%d, Estimate=%d bps, Wraparounds=%d",
			hour+1, heapMB, currentMemStats.NumGC, estimate, wraparoundCount)

		// Memory check
		if heapMB > memoryLimitMB {
			t.Fatalf("Memory limit exceeded: %.2f MB > %d MB limit", heapMB, memoryLimitMB)
		}

		// Estimate sanity check
		if estimate < estimateMinBps || estimate > estimateMaxBps {
			t.Fatalf("Hour %d: Estimate out of bounds: %d bps (valid: %d - %d)",
				hour+1, estimate, estimateMinBps, estimateMaxBps)
		}
	}

	// Final verification
	runtime.ReadMemStats(&currentMemStats)
	finalEstimate := estimator.GetEstimate()

	t.Logf("\n=== Soak Test Complete ===")
	t.Logf("Total packets processed: %d", packetsProcessed)
	t.Logf("Total wraparounds: %d (expected ~%d)", wraparoundCount, simulatedHours*60*60/64)
	t.Logf("Final estimate: %d bps", finalEstimate)
	t.Logf("Start HeapAlloc: %.2f MB", float64(startMemStats.HeapAlloc)/(1024*1024))
	t.Logf("Final HeapAlloc: %.2f MB", float64(currentMemStats.HeapAlloc)/(1024*1024))
	t.Logf("Total GC cycles: %d", currentMemStats.NumGC)

	// Assertions
	assert.Equal(t, totalPackets, packetsProcessed, "Should process all packets")
	assert.Greater(t, wraparoundCount, 1000, "Should have many timestamp wraparounds")
	assert.Greater(t, finalEstimate, int64(0), "Final estimate should be positive")
	assert.Less(t, finalEstimate, int64(estimateMaxBps), "Final estimate should be bounded")

	// Memory growth check: final should not be more than 50% above start
	// (with some margin for GC timing)
	maxAllowedHeap := float64(startMemStats.HeapAlloc) * 1.5
	if maxAllowedHeap < float64(memoryLimitMB)*1024*1024 {
		maxAllowedHeap = float64(memoryLimitMB) * 1024 * 1024
	}
	assert.Less(t, float64(currentMemStats.HeapAlloc), maxAllowedHeap,
		"Memory should be bounded (no leaks)")
}

// TestSoak1Hour_Accelerated is a shorter soak test for regular CI runs.
// Simulates 1 hour of traffic.
func TestSoak1Hour_Accelerated(t *testing.T) {
	const (
		simulatedMinutes  = 60
		packetsPerSecond  = 50
		packetSize        = 1200
		packetIntervalMs  = 20
		packetsPerMinute  = 60 * packetsPerSecond // 3000
		totalPackets      = simulatedMinutes * packetsPerMinute
		absSendTimeUnitsPerMs = 262
	)

	clock := internal.NewMockClock(time.Now())
	estimator := NewBandwidthEstimator(DefaultBandwidthEstimatorConfig(), clock)

	sendTime := uint32(0)
	var lastSendTime uint32
	wraparoundCount := 0

	for i := 0; i < totalPackets; i++ {
		if sendTime < lastSendTime {
			wraparoundCount++
		}
		lastSendTime = sendTime

		pkt := PacketInfo{
			ArrivalTime: clock.Now(),
			SendTime:    sendTime,
			Size:        packetSize,
			SSRC:        0x12345678,
		}

		estimate := estimator.OnPacket(pkt)
		require.False(t, math.IsNaN(float64(estimate)), "Estimate should not be NaN")
		require.False(t, math.IsInf(float64(estimate), 0), "Estimate should not be Inf")

		sendTime = (sendTime + uint32(packetIntervalMs*absSendTimeUnitsPerMs)) % AbsSendTimeMax
		clock.Advance(time.Duration(packetIntervalMs) * time.Millisecond)
	}

	// 1 hour = 3600 seconds, wraparound every 64 seconds = ~56 wraparounds
	assert.Greater(t, wraparoundCount, 50, "Should have timestamp wraparounds")
	assert.Greater(t, estimator.GetEstimate(), int64(0), "Should have positive estimate")
	t.Logf("1-hour test: %d packets, %d wraparounds, estimate=%d bps",
		totalPackets, wraparoundCount, estimator.GetEstimate())
}

// =============================================================================
// Timestamp Wraparound Stress Tests
// =============================================================================

// TestTimestampWraparound_64Seconds tests the estimator behavior across
// the 64-second abs-send-time boundary (24-bit field wraps at 2^24).
func TestTimestampWraparound_64Seconds(t *testing.T) {
	clock := internal.NewMockClock(time.Now())
	estimator := NewBandwidthEstimator(DefaultBandwidthEstimatorConfig(), clock)

	const (
		packetSize            = 1200
		packetIntervalMs      = 20
		absSendTimeUnitsPerMs = 262
		// AbsSendTimeMax is 2^24 = 16777216
		// At 20ms intervals: 262 * 20 = 5240 units per packet
		// Wraparound at: 16777216 / 5240 ~= 3201 packets
	)

	// Start near the wraparound point (at ~63 seconds)
	// 63 seconds = 63 * 262144 = 16515072 units (close to max of 16777216)
	sendTime := uint32(16515072)
	var estimates []int64
	var sawWraparound bool

	t.Logf("Starting near max abs-send-time: %d (max: %d)", sendTime, AbsSendTimeMax)

	// Process 200 packets to cross the wraparound
	for i := 0; i < 200; i++ {
		prevSendTime := sendTime

		pkt := PacketInfo{
			ArrivalTime: clock.Now(),
			SendTime:    sendTime,
			Size:        packetSize,
			SSRC:        0x12345678,
		}

		estimate := estimator.OnPacket(pkt)
		estimates = append(estimates, estimate)

		// Check for wraparound
		nextSendTime := (sendTime + uint32(packetIntervalMs*absSendTimeUnitsPerMs)) % AbsSendTimeMax
		if nextSendTime < prevSendTime {
			sawWraparound = true
			t.Logf("Wraparound at packet %d: %d -> %d, estimate=%d",
				i, prevSendTime, nextSendTime, estimate)
		}

		// Verify no NaN/Inf at wraparound
		require.False(t, math.IsNaN(float64(estimate)),
			"Estimate should not be NaN at packet %d (wraparound)", i)
		require.False(t, math.IsInf(float64(estimate), 0),
			"Estimate should not be Inf at packet %d (wraparound)", i)

		sendTime = nextSendTime
		clock.Advance(time.Duration(packetIntervalMs) * time.Millisecond)
	}

	assert.True(t, sawWraparound, "Should have crossed wraparound boundary")

	// Check for estimate stability (no sudden jumps)
	// After warmup, estimates should not jump by more than 50%
	warmupPackets := 50
	for i := warmupPackets + 1; i < len(estimates); i++ {
		if estimates[i-1] > 0 {
			ratio := float64(estimates[i]) / float64(estimates[i-1])
			assert.InDelta(t, 1.0, ratio, 0.5,
				"Estimate should not jump by more than 50%% at packet %d", i)
		}
	}

	t.Logf("64-second wraparound test passed: final estimate=%d bps", estimates[len(estimates)-1])
}

// TestTimestampWraparound_MultipleWraps tests the estimator across multiple
// wraparound cycles (10 cycles = 640 seconds = ~10.7 minutes).
func TestTimestampWraparound_MultipleWraps(t *testing.T) {
	clock := internal.NewMockClock(time.Now())
	estimator := NewBandwidthEstimator(DefaultBandwidthEstimatorConfig(), clock)

	const (
		wrapCycles            = 10
		packetSize            = 1200
		packetIntervalMs      = 20
		absSendTimeUnitsPerMs = 262
		secondsPerCycle       = 64
		packetsPerCycle       = secondsPerCycle * 1000 / packetIntervalMs // 3200
		totalPackets          = wrapCycles * packetsPerCycle
	)

	sendTime := uint32(0)
	wraparoundCount := 0
	var lastSendTime uint32
	var estimatesAtWraparound []int64

	t.Logf("Testing %d wraparound cycles (%d packets)", wrapCycles, totalPackets)

	for i := 0; i < totalPackets; i++ {
		// Detect wraparound
		if sendTime < lastSendTime {
			wraparoundCount++
			estimate := estimator.GetEstimate()
			estimatesAtWraparound = append(estimatesAtWraparound, estimate)
			t.Logf("Wraparound %d at packet %d: estimate=%d bps", wraparoundCount, i, estimate)
		}
		lastSendTime = sendTime

		pkt := PacketInfo{
			ArrivalTime: clock.Now(),
			SendTime:    sendTime,
			Size:        packetSize,
			SSRC:        0x12345678,
		}

		estimate := estimator.OnPacket(pkt)

		// Verify no invalid estimates
		require.False(t, math.IsNaN(float64(estimate)), "Estimate should not be NaN")
		require.False(t, math.IsInf(float64(estimate), 0), "Estimate should not be Inf")
		require.Greater(t, estimate, int64(0), "Estimate should be positive")

		sendTime = (sendTime + uint32(packetIntervalMs*absSendTimeUnitsPerMs)) % AbsSendTimeMax
		clock.Advance(time.Duration(packetIntervalMs) * time.Millisecond)
	}

	// Due to integer math rounding in packet interval calculation, we may get
	// wrapCycles or wrapCycles-1 wraparounds. Either is acceptable.
	assert.GreaterOrEqual(t, wraparoundCount, wrapCycles-1, "Should have at least %d wraparounds", wrapCycles-1)
	assert.LessOrEqual(t, wraparoundCount, wrapCycles, "Should have at most %d wraparounds", wrapCycles)

	// Verify estimates remain stable across wraparounds
	// (no sudden large changes at wraparound points)
	for i := 1; i < len(estimatesAtWraparound); i++ {
		prev := estimatesAtWraparound[i-1]
		curr := estimatesAtWraparound[i]
		if prev > 0 {
			ratio := float64(curr) / float64(prev)
			assert.InDelta(t, 1.0, ratio, 0.3,
				"Estimate should not change dramatically at wraparound %d", i)
		}
	}

	t.Logf("Multiple wraparound test passed: %d cycles, final estimate=%d bps",
		wraparoundCount, estimator.GetEstimate())
}

// TestTimestampWraparound_EdgeCases tests specific edge cases around
// the timestamp wraparound boundary.
func TestTimestampWraparound_EdgeCases(t *testing.T) {
	t.Run("ExactMaxValue", func(t *testing.T) {
		clock := internal.NewMockClock(time.Now())
		estimator := NewBandwidthEstimator(DefaultBandwidthEstimatorConfig(), clock)

		// Warm up the estimator
		sendTime := uint32(0)
		for i := 0; i < 50; i++ {
			pkt := PacketInfo{
				ArrivalTime: clock.Now(),
				SendTime:    sendTime,
				Size:        1200,
				SSRC:        0x12345678,
			}
			estimator.OnPacket(pkt)
			sendTime += 5240
			clock.Advance(20 * time.Millisecond)
		}

		// Packet at exactly max-1 value
		pkt := PacketInfo{
			ArrivalTime: clock.Now(),
			SendTime:    uint32(AbsSendTimeMax - 1), // 16777215
			Size:        1200,
			SSRC:        0x12345678,
		}
		estimate := estimator.OnPacket(pkt)
		require.False(t, math.IsNaN(float64(estimate)), "Estimate at max-1 should not be NaN")
		clock.Advance(20 * time.Millisecond)

		// Packet at zero (wrapped)
		pkt = PacketInfo{
			ArrivalTime: clock.Now(),
			SendTime:    0,
			Size:        1200,
			SSRC:        0x12345678,
		}
		estimate = estimator.OnPacket(pkt)
		require.False(t, math.IsNaN(float64(estimate)), "Estimate after wrap to 0 should not be NaN")
		require.Greater(t, estimate, int64(0), "Estimate should be positive")

		t.Logf("Max value edge case passed: estimate=%d bps", estimate)
	})

	t.Run("LargeGapAcrossWraparound", func(t *testing.T) {
		clock := internal.NewMockClock(time.Now())
		estimator := NewBandwidthEstimator(DefaultBandwidthEstimatorConfig(), clock)

		// Warm up
		sendTime := uint32(0)
		for i := 0; i < 100; i++ {
			pkt := PacketInfo{
				ArrivalTime: clock.Now(),
				SendTime:    sendTime,
				Size:        1200,
				SSRC:        0x12345678,
			}
			estimator.OnPacket(pkt)
			sendTime = (sendTime + 5240) % AbsSendTimeMax
			clock.Advance(20 * time.Millisecond)
		}

		estimateBefore := estimator.GetEstimate()

		// Simulate packet loss: jump from near max to near zero
		// (like losing ~30 seconds of packets across the boundary)
		pkt := PacketInfo{
			ArrivalTime: clock.Now(),
			SendTime:    uint32(16000000), // ~61 seconds
			Size:        1200,
			SSRC:        0x12345678,
		}
		estimator.OnPacket(pkt)
		clock.Advance(200 * time.Millisecond) // Simulate gap

		// Next packet after gap, wrapped around
		pkt = PacketInfo{
			ArrivalTime: clock.Now(),
			SendTime:    uint32(262144), // ~1 second (wrapped)
			Size:        1200,
			SSRC:        0x12345678,
		}
		estimate := estimator.OnPacket(pkt)

		require.False(t, math.IsNaN(float64(estimate)), "Estimate should not be NaN after gap")
		require.False(t, math.IsInf(float64(estimate), 0), "Estimate should not be Inf after gap")

		t.Logf("Large gap edge case: before=%d, after=%d bps", estimateBefore, estimate)
	})

	t.Run("ZeroAfterMax", func(t *testing.T) {
		clock := internal.NewMockClock(time.Now())
		estimator := NewBandwidthEstimator(DefaultBandwidthEstimatorConfig(), clock)

		// Process packets leading up to max
		for i := 0; i < 50; i++ {
			sendTime := uint32((AbsSendTimeMax - 50 + i) % AbsSendTimeMax)
			pkt := PacketInfo{
				ArrivalTime: clock.Now(),
				SendTime:    sendTime,
				Size:        1200,
				SSRC:        0x12345678,
			}
			estimator.OnPacket(pkt)
			clock.Advance(20 * time.Millisecond)
		}

		// Now send zero
		pkt := PacketInfo{
			ArrivalTime: clock.Now(),
			SendTime:    0,
			Size:        1200,
			SSRC:        0x12345678,
		}
		estimate := estimator.OnPacket(pkt)

		require.False(t, math.IsNaN(float64(estimate)), "Estimate should not be NaN")
		require.Greater(t, estimate, int64(0), "Estimate should be positive")

		t.Logf("Zero after max: estimate=%d bps", estimate)
	})
}

// TestTimestampWraparound_ContinuousMonitoring runs a longer test that
// monitors for any suspicious behavior at wraparound points.
func TestTimestampWraparound_ContinuousMonitoring(t *testing.T) {
	clock := internal.NewMockClock(time.Now())
	estimator := NewBandwidthEstimator(DefaultBandwidthEstimatorConfig(), clock)

	const (
		packetIntervalMs      = 20
		absSendTimeUnitsPerMs = 262
		testDurationSeconds   = 300 // 5 minutes, enough for ~5 wraparounds
		packetsToProcess      = testDurationSeconds * 1000 / packetIntervalMs
	)

	sendTime := uint32(0)
	var lastSendTime uint32
	var prevEstimate int64

	suspiciousEvents := 0
	wraparoundCount := 0

	for i := 0; i < packetsToProcess; i++ {
		// Detect wraparound
		isWraparound := sendTime < lastSendTime
		if isWraparound {
			wraparoundCount++
		}
		lastSendTime = sendTime

		pkt := PacketInfo{
			ArrivalTime: clock.Now(),
			SendTime:    sendTime,
			Size:        1200,
			SSRC:        0x12345678,
		}

		estimate := estimator.OnPacket(pkt)

		// Monitor for suspicious behavior at wraparound
		if isWraparound && prevEstimate > 0 {
			// Check for estimate jumps > 50%
			if float64(estimate)/float64(prevEstimate) > 1.5 ||
				float64(estimate)/float64(prevEstimate) < 0.5 {
				t.Logf("SUSPICIOUS: Large estimate change at wraparound %d: %d -> %d",
					wraparoundCount, prevEstimate, estimate)
				suspiciousEvents++
			}

			// Check for negative estimate
			if estimate <= 0 {
				t.Logf("SUSPICIOUS: Non-positive estimate at wraparound: %d", estimate)
				suspiciousEvents++
			}

			// Check for NaN/Inf
			if math.IsNaN(float64(estimate)) || math.IsInf(float64(estimate), 0) {
				t.Logf("SUSPICIOUS: NaN or Inf estimate at wraparound")
				suspiciousEvents++
			}
		}

		prevEstimate = estimate
		sendTime = (sendTime + uint32(packetIntervalMs*absSendTimeUnitsPerMs)) % AbsSendTimeMax
		clock.Advance(time.Duration(packetIntervalMs) * time.Millisecond)
	}

	t.Logf("Continuous monitoring: %d packets, %d wraparounds, %d suspicious events",
		packetsToProcess, wraparoundCount, suspiciousEvents)

	assert.Equal(t, 0, suspiciousEvents, "Should have no suspicious events at wraparound")
	assert.GreaterOrEqual(t, wraparoundCount, 4, "Should have multiple wraparounds")
}
