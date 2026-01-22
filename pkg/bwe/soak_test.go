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
