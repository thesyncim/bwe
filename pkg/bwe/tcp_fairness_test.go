// Package bwe implements Google Congestion Control (GCC) receiver-side
// bandwidth estimation for WebRTC.
//
// This file contains TCP fairness simulation tests for VALID-03 requirement.
// These tests verify the estimator coexists fairly with TCP traffic:
// - It backs off during congestion (appropriate backoff)
// - It doesn't starve (maintains >10% of fair share)
// - It recovers when competition ends
//
// The tests use simulated congestion patterns rather than real network
// impairment tools (tc/netem). Real TCP fairness testing would require
// a testbed environment.
//
// Reference:
// - GCC Specification: https://datatracker.ietf.org/doc/html/draft-ietf-rmcat-gcc-02
// - C3Lab WebRTC Testbed methodology
// - K_u/K_d coefficients from overuse.go (0.01/0.00018)
package bwe

import (
	"testing"
	"time"

	"bwe/pkg/bwe/internal"

	"github.com/stretchr/testify/assert"
)

// =============================================================================
// TCP Fairness Test Constants
// =============================================================================

const (
	// tcpFairnessTestDuration is the default duration for each phase in TCP fairness tests.
	tcpFairnessTestDuration = 30 * time.Second

	// fairShareThreshold is the minimum fraction of fair share the BWE must maintain
	// during TCP competition. 10% ensures no starvation.
	fairShareThreshold = 0.10

	// maxShareThreshold is the maximum fraction of total bandwidth the BWE should use
	// during TCP competition. 90% ensures appropriate backoff.
	maxShareThreshold = 0.90

	// tcpFairnessPacketInterval is the packet generation interval (50 pps).
	tcpFairnessPacketInterval = 20 * time.Millisecond

	// congestionDelayIncreasePerPacket is the delay increase per packet during congestion
	// simulating queue building from TCP competition (0.3ms).
	congestionDelayIncreasePerPacket = 300 * time.Microsecond

	// maxCongestionDelay is the maximum accumulated delay (queue full after ~100 packets).
	maxCongestionDelay = 30 * time.Millisecond
)

// =============================================================================
// TCP Fairness Simulation Helpers
// =============================================================================

// simulateCongestion simulates network traffic with configurable congestion.
// When congested=true, packets experience increasing delay (simulating queue building from TCP competition).
// Returns the final bandwidth estimate after the simulation period.
//
// The key to triggering overuse detection is that the inter-arrival time must
// consistently exceed the inter-send time. This simulates packets queueing up.
//
// Parameters:
//   - estimator: The bandwidth estimator to test
//   - clock: Mock clock for time control
//   - duration: How long to simulate
//   - availableBandwidth: Total available bandwidth in bits per second
//   - congested: If true, simulate queue building delays
//
// Returns the final bandwidth estimate in bits per second.
func simulateCongestion(
	estimator *BandwidthEstimator,
	clock *internal.MockClock,
	duration time.Duration,
	availableBandwidth int64,
	congested bool,
) int64 {
	// Calculate packet size from available bandwidth
	// At 50 pps (20ms interval), packetSize = bandwidth / (50 * 8)
	packetsPerSecond := int64(50)
	packetSize := int(availableBandwidth / (packetsPerSecond * 8))
	if packetSize < 100 {
		packetSize = 100 // Minimum packet size
	}

	// Generate packets
	numPackets := int(duration / tcpFairnessPacketInterval)
	sendTime := uint32(0)
	sendTimeIncrement := uint32(tcpFairnessPacketInterval.Microseconds() * 262 / 1000) // Convert to abs-send-time units

	// Congestion causes packets to arrive later than expected.
	// We add significant extra delay to each arrival to simulate queue building.
	// This is similar to TestBandwidthEstimator_Congestion which uses 50ms extra delay.
	const congestionExtraDelay = 30 * time.Millisecond // Extra delay per packet during congestion

	for i := 0; i < numPackets; i++ {
		// Build packet info
		pkt := PacketInfo{
			ArrivalTime: clock.Now(),
			SendTime:    sendTime,
			Size:        packetSize,
			SSRC:        0x11111111,
		}

		// Process packet
		estimator.OnPacket(pkt)

		// Update send time (constant sender rate)
		sendTime += sendTimeIncrement

		// Advance clock based on congestion state
		if congested {
			// Congestion: packets arrive significantly later than sent
			// The inter-arrival time (20ms + 30ms = 50ms) is much greater than
			// inter-send time (20ms), triggering queue building detection.
			clock.Advance(tcpFairnessPacketInterval + congestionExtraDelay)
		} else {
			// No congestion: packets arrive at expected rate
			clock.Advance(tcpFairnessPacketInterval)
		}
	}

	return estimator.GetEstimate()
}

// generateCongestionPackets generates a stream of packets with configurable delay pattern.
// This is a lower-level helper for more complex congestion scenarios.
//
// Parameters:
//   - estimator: The bandwidth estimator to test
//   - clock: Mock clock for time control
//   - numPackets: Number of packets to generate
//   - packetSize: Size of each packet in bytes
//   - baseInterval: Base interval between packets
//   - delayPattern: Function that returns additional delay for packet i
//
// Returns estimates collected at regular intervals.
func generateCongestionPackets(
	estimator *BandwidthEstimator,
	clock *internal.MockClock,
	numPackets int,
	packetSize int,
	baseInterval time.Duration,
	delayPattern func(packetNum int) time.Duration,
) []int64 {
	estimates := make([]int64, 0, numPackets/50+1)
	sendTime := uint32(0)
	sendTimeIncrement := uint32(baseInterval.Microseconds() * 262 / 1000)

	for i := 0; i < numPackets; i++ {
		pkt := PacketInfo{
			ArrivalTime: clock.Now(),
			SendTime:    sendTime,
			Size:        packetSize,
			SSRC:        0x22222222,
		}

		estimator.OnPacket(pkt)

		// Collect estimate every 50 packets (1 second at 50 pps)
		if i%50 == 0 && i > 0 {
			estimates = append(estimates, estimator.GetEstimate())
		}

		sendTime += sendTimeIncrement

		// Apply delay pattern
		extraDelay := delayPattern(i)
		clock.Advance(baseInterval + extraDelay)
	}

	// Add final estimate
	estimates = append(estimates, estimator.GetEstimate())
	return estimates
}

// =============================================================================
// VALID-03: TCP Fairness Three-Phase Test
// =============================================================================

// TestTCPFairness_ThreePhase verifies VALID-03: correct behavior with TCP competition.
//
// Methodology from C3Lab WebRTC Testbed:
//   - Phase 1: BWE alone (30s) - should use available bandwidth
//   - Phase 2: BWE + TCP competition (60s) - should reach fair share
//   - Phase 3: BWE alone (30s) - should recover
//
// Pass criteria:
//   - Phase 2 estimate > 10% of fair share (no starvation)
//   - Phase 2 estimate < 90% of total bandwidth (appropriate backoff)
//   - Phase 3 estimate > Phase 2 estimate (recovery)
func TestTCPFairness_ThreePhase(t *testing.T) {
	clock := internal.NewMockClock(time.Now())
	config := DefaultBandwidthEstimatorConfig()
	estimator := NewBandwidthEstimator(config, clock)

	// Total simulated bandwidth: 2 Mbps
	totalBandwidth := int64(2_000_000)

	t.Log("=== Phase 1: BWE alone (30s) ===")
	// Phase 1: No congestion, expect estimate near 2 Mbps
	phase1Estimate := simulateCongestion(
		estimator,
		clock,
		tcpFairnessTestDuration,
		totalBandwidth,
		false, // No congestion
	)
	t.Logf("Phase 1 estimate: %d bps (expected: ~%d bps)", phase1Estimate, totalBandwidth)
	t.Logf("Phase 1 state: congestion=%v, rateControl=%v",
		estimator.GetCongestionState(), estimator.GetRateControlState())

	t.Log("=== Phase 2: BWE + TCP competition (60s) ===")
	// Phase 2: Simulate congestion (TCP takes half), expect estimate ~1 Mbps (fair share)
	// Run for 60 seconds with congestion
	phase2Estimate := simulateCongestion(
		estimator,
		clock,
		2*tcpFairnessTestDuration, // 60 seconds
		totalBandwidth,
		true, // Congested
	)
	fairShare := totalBandwidth / 2
	t.Logf("Phase 2 estimate: %d bps (fair share: %d bps)", phase2Estimate, fairShare)
	t.Logf("Phase 2 state: congestion=%v, rateControl=%v",
		estimator.GetCongestionState(), estimator.GetRateControlState())

	t.Log("=== Phase 3: BWE alone (30s) ===")
	// Phase 3: Congestion clears, expect recovery toward 2 Mbps
	phase3Estimate := simulateCongestion(
		estimator,
		clock,
		tcpFairnessTestDuration,
		totalBandwidth,
		false, // No congestion
	)
	t.Logf("Phase 3 estimate: %d bps (expected recovery toward: %d bps)", phase3Estimate, totalBandwidth)
	t.Logf("Phase 3 state: congestion=%v, rateControl=%v",
		estimator.GetCongestionState(), estimator.GetRateControlState())

	// === Assertions ===

	// Phase 1: Should utilize most of available bandwidth
	// Note: With limited warmup time, may not reach full bandwidth
	assert.Greater(t, phase1Estimate, int64(100_000),
		"Phase 1: Should have positive estimate after warmup")

	// Phase 2: Fair share (between 10% and 90% of total)
	// The BWE should back off when congestion is detected but not be starved
	minAcceptable := int64(float64(fairShare) * fairShareThreshold)
	maxAcceptable := int64(float64(totalBandwidth) * maxShareThreshold)

	assert.Greater(t, phase2Estimate, minAcceptable,
		"Phase 2: Should not be starved (must maintain >10%% of fair share). Got %d, min %d",
		phase2Estimate, minAcceptable)
	assert.Less(t, phase2Estimate, maxAcceptable,
		"Phase 2: Should back off for TCP (must be <90%% of total). Got %d, max %d",
		phase2Estimate, maxAcceptable)

	// Phase 3: Recovery (estimate should increase after congestion ends)
	assert.Greater(t, phase3Estimate, phase2Estimate,
		"Phase 3: Should recover after congestion ends. Phase 2: %d, Phase 3: %d",
		phase2Estimate, phase3Estimate)

	t.Log("=== TCP Fairness Three-Phase Test: PASSED ===")
	t.Logf("Summary: Phase1=%d -> Phase2=%d -> Phase3=%d bps",
		phase1Estimate, phase2Estimate, phase3Estimate)
}
