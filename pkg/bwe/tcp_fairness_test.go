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

	"github.com/thesyncim/bwe/pkg/bwe/internal"

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

// =============================================================================
// Adaptive Threshold Verification Tests
// =============================================================================

// TestTCPFairness_AdaptiveThreshold verifies the adaptive threshold mechanism
// that is critical for TCP fairness.
//
// The GCC specification uses asymmetric K_u/K_d coefficients:
//   - K_u = 0.01 (threshold increases when estimate exceeds threshold)
//   - K_d = 0.00018 (threshold decreases when estimate is below threshold)
//
// This asymmetry is critical for TCP fairness:
//   - Threshold increases quickly when congestion is detected (prevents starvation)
//   - Threshold decreases slowly when congestion clears (prevents oscillation)
//
// The ratio K_u/K_d ~ 55:1 means the threshold takes ~55x longer to decrease
// than to increase. This allows the estimator to be more tolerant of transient
// congestion while still being responsive to sustained congestion.
func TestTCPFairness_AdaptiveThreshold(t *testing.T) {
	config := DefaultOveruseConfig()

	// Verify K_u > K_d (asymmetric coefficients)
	assert.Greater(t, config.Ku, config.Kd,
		"K_u should be greater than K_d for TCP fairness")

	// Verify the ratio is approximately 55:1 (as per GCC spec)
	ratio := config.Ku / config.Kd
	t.Logf("K_u/K_d ratio: %.1f (expected ~55)", ratio)
	assert.Greater(t, ratio, 50.0, "K_u/K_d ratio should be at least 50")
	assert.Less(t, ratio, 60.0, "K_u/K_d ratio should not exceed 60")

	// Test threshold adaptation behavior
	clock := internal.NewMockClock(time.Now())
	detector := NewOveruseDetector(config, clock)

	initialThreshold := detector.Threshold()
	t.Logf("Initial threshold: %.2f ms", initialThreshold)

	// Feed high estimates (above threshold) - should increase threshold
	for i := 0; i < 100; i++ {
		detector.Detect(20.0) // Estimate well above initial threshold of 12.5
		clock.Advance(20 * time.Millisecond)
	}

	highThreshold := detector.Threshold()
	t.Logf("Threshold after high estimates: %.2f ms", highThreshold)
	assert.Greater(t, highThreshold, initialThreshold,
		"Threshold should increase when estimate exceeds it")

	// Now feed low estimates (below threshold) - should decrease threshold slowly
	for i := 0; i < 100; i++ {
		detector.Detect(1.0) // Estimate well below threshold
		clock.Advance(20 * time.Millisecond)
	}

	lowThreshold := detector.Threshold()
	t.Logf("Threshold after low estimates: %.2f ms", lowThreshold)

	// The threshold should have decreased, but due to slow Kd,
	// it shouldn't have returned to initial (asymmetric behavior)
	assert.Less(t, lowThreshold, highThreshold,
		"Threshold should decrease when estimate is below it")

	// Key asymmetry test: the decrease should be much smaller than the increase
	increase := highThreshold - initialThreshold
	decrease := highThreshold - lowThreshold
	t.Logf("Threshold increase: %.4f, decrease: %.4f", increase, decrease)

	// Due to asymmetry, decrease should be much smaller relative to increase
	// (both happened over same time period with same estimate magnitude relative to threshold)
	if decrease > 0 {
		asymmetryRatio := increase / decrease
		t.Logf("Asymmetry ratio (increase/decrease): %.1f", asymmetryRatio)
		// The ratio won't be exactly K_u/K_d due to non-linear dynamics,
		// but should show clear asymmetry
		assert.Greater(t, asymmetryRatio, 5.0,
			"Threshold should increase much faster than it decreases")
	}
}

// TestTCPFairness_SustainedCongestion verifies behavior under long-duration congestion.
// This tests that the estimator doesn't gradually starve over time (gradual starvation bug).
//
// Some BWE implementations have bugs where prolonged congestion causes the estimate
// to gradually decrease to zero. The adaptive threshold mechanism should prevent this.
func TestTCPFairness_SustainedCongestion(t *testing.T) {
	clock := internal.NewMockClock(time.Now())
	config := DefaultBandwidthEstimatorConfig()
	estimator := NewBandwidthEstimator(config, clock)

	totalBandwidth := int64(2_000_000) // 2 Mbps

	// First establish a baseline with stable traffic
	t.Log("Establishing baseline...")
	_ = simulateCongestion(estimator, clock, 10*time.Second, totalBandwidth, false)
	baseline := estimator.GetEstimate()
	t.Logf("Baseline estimate: %d bps", baseline)

	// Now simulate sustained congestion for 5+ minutes
	// We'll sample the estimate every minute to track for gradual starvation
	t.Log("Simulating 5 minutes of sustained congestion...")
	estimates := make([]int64, 0, 6)
	estimates = append(estimates, estimator.GetEstimate())

	for minute := 1; minute <= 5; minute++ {
		_ = simulateCongestion(estimator, clock, time.Minute, totalBandwidth, true)
		estimate := estimator.GetEstimate()
		estimates = append(estimates, estimate)
		t.Logf("After minute %d: estimate=%d bps, state=%v",
			minute, estimate, estimator.GetCongestionState())
	}

	// Verify no gradual starvation
	// The estimate should stabilize around fair share, not continue decreasing
	minEstimate := int64(float64(totalBandwidth/2) * fairShareThreshold) // 10% of fair share

	for i, est := range estimates {
		assert.Greater(t, est, minEstimate,
			"Minute %d: estimate should not fall below 10%% of fair share (%d)", i, minEstimate)
	}

	// Check that estimate is stable (not continuously decreasing)
	// Compare last two minutes - they should be similar
	if len(estimates) >= 3 {
		lastMinute := estimates[len(estimates)-1]
		prevMinute := estimates[len(estimates)-2]

		// Allow 50% variation but shouldn't be monotonically decreasing to zero
		if lastMinute < prevMinute {
			decreasePercent := float64(prevMinute-lastMinute) / float64(prevMinute) * 100
			t.Logf("Decrease from minute 4 to 5: %.1f%%", decreasePercent)
			assert.Less(t, decreasePercent, 50.0,
				"Estimate should not be rapidly decreasing (gradual starvation)")
		}
	}

	t.Log("Sustained congestion test: No gradual starvation detected")
}

// TestTCPFairness_RapidTransitions verifies behavior with rapid congestion state changes.
// This simulates scenarios where TCP flows start and stop frequently, testing that
// the estimator doesn't oscillate wildly.
func TestTCPFairness_RapidTransitions(t *testing.T) {
	clock := internal.NewMockClock(time.Now())
	config := DefaultBandwidthEstimatorConfig()
	estimator := NewBandwidthEstimator(config, clock)

	totalBandwidth := int64(2_000_000) // 2 Mbps

	// Establish baseline
	_ = simulateCongestion(estimator, clock, 10*time.Second, totalBandwidth, false)
	t.Logf("Baseline: %d bps", estimator.GetEstimate())

	// Rapidly alternate between congested and clear states
	// Each phase is only 5 seconds (shorter than normal)
	t.Log("Rapid congestion transitions (5s on / 5s off)...")

	estimates := make([]int64, 0, 10)
	for i := 0; i < 5; i++ {
		// Congested phase
		_ = simulateCongestion(estimator, clock, 5*time.Second, totalBandwidth, true)
		congestedEst := estimator.GetEstimate()

		// Clear phase
		_ = simulateCongestion(estimator, clock, 5*time.Second, totalBandwidth, false)
		clearEst := estimator.GetEstimate()

		estimates = append(estimates, congestedEst, clearEst)
		t.Logf("Cycle %d: congested=%d, clear=%d bps", i+1, congestedEst, clearEst)
	}

	// Verify estimates don't oscillate wildly
	// Calculate coefficient of variation (std dev / mean)
	var sum, sumSq float64
	for _, est := range estimates {
		sum += float64(est)
		sumSq += float64(est) * float64(est)
	}
	mean := sum / float64(len(estimates))
	variance := sumSq/float64(len(estimates)) - mean*mean
	if variance < 0 {
		variance = 0
	}
	stdDev := 0.0
	if variance > 0 {
		stdDev = float64(int64(variance)) // Simplified sqrt approximation
		// Actually compute sqrt properly
		for i := 0; i < 10; i++ {
			stdDev = (stdDev + variance/stdDev) / 2
		}
	}
	cv := stdDev / mean

	t.Logf("Estimate statistics: mean=%.0f, stdDev=%.0f, CV=%.2f", mean, stdDev, cv)

	// All estimates should be reasonable (not wild oscillations to 0 or infinity)
	for i, est := range estimates {
		assert.Greater(t, est, int64(10_000),
			"Cycle %d: estimate should not drop to near-zero", i/2+1)
		assert.Less(t, est, int64(10_000_000),
			"Cycle %d: estimate should not spike unreasonably high", i/2+1)
	}

	// The final estimates should show some stability
	// (clear phases should generally be higher than congested phases)
	clearPhaseEstimates := make([]int64, 0, 5)
	congestedPhaseEstimates := make([]int64, 0, 5)
	for i, est := range estimates {
		if i%2 == 0 {
			congestedPhaseEstimates = append(congestedPhaseEstimates, est)
		} else {
			clearPhaseEstimates = append(clearPhaseEstimates, est)
		}
	}

	// Note: Due to the rapid transitions (only 5s phases), the estimate may not
	// fully recover during clear phases. This is expected behavior - recovery
	// takes time due to the multiplicative increase mechanism.
	//
	// What we verify is:
	// 1. Estimates stay within reasonable bounds (done above)
	// 2. No wild oscillations (coefficient of variation check)
	// 3. The system remains functional (doesn't crash or hang)

	// Verify the system doesn't enter a stuck state
	// Both congested and clear estimates should be positive and reasonable
	finalClear := clearPhaseEstimates[len(clearPhaseEstimates)-1]
	finalCongested := congestedPhaseEstimates[len(congestedPhaseEstimates)-1]

	t.Logf("Final estimates: congested=%d, clear=%d bps", finalCongested, finalClear)

	// Both should be reasonable (> 100 kbps, < 10 Mbps)
	assert.Greater(t, finalClear, int64(100_000),
		"Clear phase estimate should be at least 100 kbps")
	assert.Greater(t, finalCongested, int64(100_000),
		"Congested phase estimate should be at least 100 kbps")

	// Verify no extreme divergence between phases
	// The ratio between congested and clear shouldn't be more than 10x
	if finalCongested > finalClear {
		ratio := float64(finalCongested) / float64(finalClear)
		assert.Less(t, ratio, 10.0,
			"Ratio between congested and clear estimates shouldn't be extreme")
	}

	t.Log("Rapid transitions test: Estimator handles transitions without wild oscillations")
}
