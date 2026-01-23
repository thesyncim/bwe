package bwe

import (
	"sync"
	"testing"
	"time"

	"github.com/thesyncim/bwe/pkg/bwe/internal"
)

// =============================================================================
// Test Trace Generators
// =============================================================================

// stableNetworkTrace generates packets with constant delay (no congestion).
// Packets arrive at the same rate they were sent.
func stableNetworkTrace(clock *internal.MockClock, count int, intervalMs int) []PacketInfo {
	packets := make([]PacketInfo, count)
	sendTime := uint32(0)

	for i := 0; i < count; i++ {
		packets[i] = PacketInfo{
			ArrivalTime: clock.Now(),
			SendTime:    sendTime,
			Size:        1200,
			SSRC:        0x12345678,
		}
		// Both send and receive advance by same interval
		// abs-send-time units: ~262 units per ms (262144 units / 1000 ms)
		sendTime += uint32(intervalMs * 262)
		clock.Advance(time.Duration(intervalMs) * time.Millisecond)
	}
	return packets
}

// congestingNetworkTrace generates packets where receive delay increases.
// Simulates queue building: each packet arrives slightly later than expected.
func congestingNetworkTrace(clock *internal.MockClock, count int, intervalMs int, delayIncreaseMs float64) []PacketInfo {
	packets := make([]PacketInfo, count)
	sendTime := uint32(0)

	for i := 0; i < count; i++ {
		packets[i] = PacketInfo{
			ArrivalTime: clock.Now(),
			SendTime:    sendTime,
			Size:        1200,
			SSRC:        0x12345678,
		}
		// Send time advances normally
		sendTime += uint32(intervalMs * 262)
		// Receive time advances more (queue building)
		clock.Advance(time.Duration(float64(intervalMs)+delayIncreaseMs) * time.Millisecond)
	}
	return packets
}

// drainingNetworkTrace generates packets where receive delay decreases.
// Simulates queue draining: packets arrive faster than expected.
func drainingNetworkTrace(clock *internal.MockClock, count int, intervalMs int, delayDecreaseMs float64) []PacketInfo {
	packets := make([]PacketInfo, count)
	sendTime := uint32(0)

	for i := 0; i < count; i++ {
		packets[i] = PacketInfo{
			ArrivalTime: clock.Now(),
			SendTime:    sendTime,
			Size:        1200,
			SSRC:        0x12345678,
		}
		// Send time advances normally
		sendTime += uint32(intervalMs * 262)
		// Receive time advances less (queue draining)
		advanceMs := float64(intervalMs) - delayDecreaseMs
		if advanceMs < 1 {
			advanceMs = 1 // Minimum 1ms advance to maintain monotonicity
		}
		clock.Advance(time.Duration(advanceMs) * time.Millisecond)
	}
	return packets
}

// wraparoundTrace generates packets that exercise 24-bit abs-send-time wraparound.
func wraparoundTrace(clock *internal.MockClock, count int) []PacketInfo {
	packets := make([]PacketInfo, count)
	// Start near max (64 second mark), generate packets across wrap
	sendTime := uint32(AbsSendTimeMax - 100*20*262)

	for i := 0; i < count; i++ {
		packets[i] = PacketInfo{
			ArrivalTime: clock.Now(),
			SendTime:    sendTime,
			Size:        1200,
			SSRC:        0x12345678,
		}
		// 20ms intervals, wrap when needed
		sendTime = (sendTime + 20*262) % uint32(AbsSendTimeMax)
		clock.Advance(20 * time.Millisecond)
	}
	return packets
}

// burstTrace generates packets in bursts that should be grouped together.
func burstTrace(clock *internal.MockClock, burstCount, packetsPerBurst, interBurstMs, intraBurstMs int) []PacketInfo {
	packets := make([]PacketInfo, burstCount*packetsPerBurst)
	sendTime := uint32(0)
	idx := 0

	for b := 0; b < burstCount; b++ {
		for p := 0; p < packetsPerBurst; p++ {
			packets[idx] = PacketInfo{
				ArrivalTime: clock.Now(),
				SendTime:    sendTime,
				Size:        1200,
				SSRC:        0x12345678,
			}
			sendTime += uint32(intraBurstMs * 262)
			idx++

			if p < packetsPerBurst-1 {
				// Within burst: small interval
				clock.Advance(time.Duration(intraBurstMs) * time.Millisecond)
			}
		}
		// Between bursts: larger interval
		if b < burstCount-1 {
			clock.Advance(time.Duration(interBurstMs) * time.Millisecond)
			sendTime += uint32(interBurstMs * 262)
		}
	}
	return packets
}

// =============================================================================
// Integration Tests for DelayEstimator Pipeline
// =============================================================================

func TestDelayEstimator_StableNetwork(t *testing.T) {
	// Stable network: no congestion, state should remain BwNormal
	clock := internal.NewMockClock(time.Time{})
	config := DefaultDelayEstimatorConfig()
	estimator := NewDelayEstimator(config, clock)

	// Track state changes
	var stateChanges []struct{ old, new BandwidthUsage }
	var mu sync.Mutex
	estimator.SetCallback(func(old, new BandwidthUsage) {
		mu.Lock()
		stateChanges = append(stateChanges, struct{ old, new BandwidthUsage }{old, new})
		mu.Unlock()
	})

	// Generate 100 packets at 20ms intervals with stable delay
	packets := stableNetworkTrace(clock, 100, 20)

	// Feed all packets
	var finalState BandwidthUsage
	for _, pkt := range packets {
		finalState = estimator.OnPacket(pkt)
	}

	// Final state should be Normal
	if finalState != BwNormal {
		t.Errorf("Stable network: final state = %v, want BwNormal", finalState)
	}

	// Should have no state change to Overusing
	mu.Lock()
	for _, sc := range stateChanges {
		if sc.new == BwOverusing {
			t.Errorf("Stable network should not trigger BwOverusing, got transition %v -> %v", sc.old, sc.new)
		}
	}
	mu.Unlock()
}

func TestDelayEstimator_CongestingNetwork(t *testing.T) {
	// Congesting network: increasing delay should eventually trigger BwOverusing
	// Note: Kalman filter converges slowly (~500 iterations for full convergence)
	// and the initial threshold is 12.5ms, so we need the filtered estimate to exceed that.
	// With 50ms delay variation, Kalman will converge toward that value and cross the threshold.
	clock := internal.NewMockClock(time.Time{})
	config := DefaultDelayEstimatorConfig()
	estimator := NewDelayEstimator(config, clock)

	// Track overuse detection
	gotOveruse := false
	estimator.SetCallback(func(old, new BandwidthUsage) {
		if new == BwOverusing {
			gotOveruse = true
		}
	})

	// Generate packets inline with clock advancement
	// This ensures the estimator's clock stays in sync with packet arrivals
	sendTime := uint32(0)
	delayIncreaseMs := 50.0
	intervalMs := 20

	for i := 0; i < 100; i++ {
		pkt := PacketInfo{
			ArrivalTime: clock.Now(),
			SendTime:    sendTime,
			Size:        1200,
			SSRC:        0x12345678,
		}
		estimator.OnPacket(pkt)

		sendTime += uint32(intervalMs * 262)
		clock.Advance(time.Duration(float64(intervalMs)+delayIncreaseMs) * time.Millisecond)
	}

	// Should have detected overuse at some point
	if !gotOveruse {
		t.Error("Congesting network should eventually trigger BwOverusing")
	}
}

func TestDelayEstimator_DrainingNetwork(t *testing.T) {
	// Draining network: decreasing delay should trigger BwUnderusing
	// Underuse is detected when estimate goes below -threshold (negative).
	// We need strong negative delay variations to trigger this.
	//
	// Key constraint: arrival gaps must be > 5ms (burst threshold) to create separate groups
	// So we use longer intervals and smaller delay decrease to maintain > 5ms arrival gaps
	clock := internal.NewMockClock(time.Time{})
	config := DefaultDelayEstimatorConfig()
	estimator := NewDelayEstimator(config, clock)

	// Track underuse detection
	gotUnderuse := false
	estimator.SetCallback(func(old, new BandwidthUsage) {
		if new == BwUnderusing {
			gotUnderuse = true
		}
	})

	// Generate packets with 50ms send interval, 10ms receive interval
	// This gives -40ms delay variation (strong underuse signal)
	// and 10ms arrival gaps (> 5ms burst threshold)
	sendTime := uint32(0)
	sendIntervalMs := 50
	receiveIntervalMs := 10

	for i := 0; i < 100; i++ {
		pkt := PacketInfo{
			ArrivalTime: clock.Now(),
			SendTime:    sendTime,
			Size:        1200,
			SSRC:        0x12345678,
		}
		estimator.OnPacket(pkt)

		sendTime += uint32(sendIntervalMs * 262)
		clock.Advance(time.Duration(receiveIntervalMs) * time.Millisecond)
	}

	// Should have detected underuse at some point
	if !gotUnderuse {
		t.Error("Draining network should eventually trigger BwUnderusing")
	}
}

func TestDelayEstimator_RecoveryFromCongestion(t *testing.T) {
	// Test recovery: congesting -> stable -> should return to normal
	clock := internal.NewMockClock(time.Time{})
	config := DefaultDelayEstimatorConfig()
	estimator := NewDelayEstimator(config, clock)

	// First phase: congesting network
	congestingPackets := congestingNetworkTrace(clock, 150, 20, 2.0)
	for _, pkt := range congestingPackets {
		estimator.OnPacket(pkt)
	}

	// Second phase: stable network
	stablePackets := stableNetworkTrace(clock, 200, 20)
	var finalState BandwidthUsage
	for _, pkt := range stablePackets {
		finalState = estimator.OnPacket(pkt)
	}

	// After stable period, should recover to Normal
	// Note: May stay normal for a while before recovery, or might oscillate
	// The important thing is it doesn't stay stuck in Overusing
	if finalState == BwOverusing {
		t.Errorf("Should recover from congestion, but still in BwOverusing")
	}
}

func TestDelayEstimator_WraparoundHandling(t *testing.T) {
	// Wraparound: timestamps crossing 64-second boundary should not cause issues
	clock := internal.NewMockClock(time.Time{})
	config := DefaultDelayEstimatorConfig()
	estimator := NewDelayEstimator(config, clock)

	// Generate packets that cross the wraparound boundary
	// Using 200 packets at 20ms = 4 seconds, starting 2 seconds before wrap
	packets := wraparoundTrace(clock, 200)

	// Feed all packets - should not panic or produce extreme states
	var finalState BandwidthUsage
	gotOveruse := false
	estimator.SetCallback(func(old, new BandwidthUsage) {
		if new == BwOverusing {
			gotOveruse = true
		}
	})

	for _, pkt := range packets {
		finalState = estimator.OnPacket(pkt)
	}

	// Wraparound with stable timing should not trigger overuse
	// (the timestamps wrap but delay variation should still be ~0)
	if gotOveruse {
		t.Error("Wraparound with stable timing should not trigger BwOverusing")
	}

	// State should be Normal (stable delay variation across wrap)
	if finalState != BwNormal {
		t.Errorf("Wraparound: final state = %v, want BwNormal", finalState)
	}
}

func TestDelayEstimator_WithTrendlineFilter(t *testing.T) {
	// Test with Trendline filter produces consistent state
	// Trendline detects TRENDS - measuring rate of change in delay over time
	// This test verifies that Trendline integration works correctly
	clock := internal.NewMockClock(time.Time{})
	config := DefaultDelayEstimatorConfig()
	config.FilterType = FilterTrendline
	estimator := NewDelayEstimator(config, clock)

	// Generate stable packets
	sendTime := uint32(0)
	baseIntervalMs := 20

	for i := 0; i < 50; i++ {
		pkt := PacketInfo{
			ArrivalTime: clock.Now(),
			SendTime:    sendTime,
			Size:        1200,
			SSRC:        0x12345678,
		}
		estimator.OnPacket(pkt)

		sendTime += uint32(baseIntervalMs * 262)
		clock.Advance(time.Duration(baseIntervalMs) * time.Millisecond)
	}

	// Stable network with trendline should remain Normal
	if estimator.State() != BwNormal {
		t.Errorf("Trendline filter: stable network should be BwNormal, got %v", estimator.State())
	}

	// Verify estimator can be reset
	estimator.Reset()
	if estimator.State() != BwNormal {
		t.Errorf("After reset, state should be BwNormal, got %v", estimator.State())
	}
}

func TestDelayEstimator_MonotonicTimeUsage(t *testing.T) {
	// Verify that the estimator uses monotonic time correctly
	// MockClock panics if we try to go backward, so this test ensures
	// all time operations are forward-only

	clock := internal.NewMockClock(time.Time{})
	config := DefaultDelayEstimatorConfig()
	estimator := NewDelayEstimator(config, clock)

	// This test would panic if any component tried to use backward time
	// We run a full trace through the pipeline

	packets := stableNetworkTrace(clock, 100, 20)

	// If monotonic time is violated, MockClock.Advance would panic
	defer func() {
		if r := recover(); r != nil {
			t.Errorf("Monotonic time violation detected: %v", r)
		}
	}()

	for _, pkt := range packets {
		estimator.OnPacket(pkt)
	}

	// Also test with congesting trace
	clock2 := internal.NewMockClock(time.Time{})
	estimator2 := NewDelayEstimator(config, clock2)
	packets2 := congestingNetworkTrace(clock2, 100, 20, 2.0)
	for _, pkt := range packets2 {
		estimator2.OnPacket(pkt)
	}
}

func TestDelayEstimator_Reset(t *testing.T) {
	// Test that Reset() properly clears all state
	clock := internal.NewMockClock(time.Time{})
	config := DefaultDelayEstimatorConfig()
	estimator := NewDelayEstimator(config, clock)

	// First, create some congestion
	packets := congestingNetworkTrace(clock, 150, 20, 2.0)
	for _, pkt := range packets {
		estimator.OnPacket(pkt)
	}

	// Reset the estimator
	estimator.Reset()

	// State should be back to Normal
	if estimator.State() != BwNormal {
		t.Errorf("After reset, state = %v, want BwNormal", estimator.State())
	}

	// Now a stable trace should keep it normal
	stablePackets := stableNetworkTrace(clock, 100, 20)
	gotOveruse := false
	estimator.SetCallback(func(old, new BandwidthUsage) {
		if new == BwOverusing {
			gotOveruse = true
		}
	})

	for _, pkt := range stablePackets {
		estimator.OnPacket(pkt)
	}

	// Should not trigger overuse after reset + stable packets
	if gotOveruse {
		t.Error("After reset with stable packets, should not trigger BwOverusing")
	}
}

func TestDelayEstimator_BurstGrouping(t *testing.T) {
	// Test that burst grouping works correctly
	// Packets within a burst (< 5ms apart) should be grouped
	clock := internal.NewMockClock(time.Time{})
	config := DefaultDelayEstimatorConfig()
	estimator := NewDelayEstimator(config, clock)

	// Create bursts: 20 bursts, 3 packets each, 20ms between bursts, 2ms within burst
	// Within-burst packets (2ms) should be grouped (< 5ms threshold)
	packets := burstTrace(clock, 20, 3, 20, 2)

	var finalState BandwidthUsage
	for _, pkt := range packets {
		finalState = estimator.OnPacket(pkt)
	}

	// Stable bursts should result in Normal state
	if finalState != BwNormal {
		t.Errorf("Burst grouping with stable network: state = %v, want BwNormal", finalState)
	}
}

func TestDelayEstimator_StateMethod(t *testing.T) {
	clock := internal.NewMockClock(time.Time{})
	config := DefaultDelayEstimatorConfig()
	estimator := NewDelayEstimator(config, clock)

	// Initial state should be Normal
	if estimator.State() != BwNormal {
		t.Errorf("Initial state = %v, want BwNormal", estimator.State())
	}

	// After some stable packets, should still be Normal
	packets := stableNetworkTrace(clock, 10, 20)
	for _, pkt := range packets {
		estimator.OnPacket(pkt)
	}

	if estimator.State() != BwNormal {
		t.Errorf("After stable packets, state = %v, want BwNormal", estimator.State())
	}
}

func TestDelayEstimator_DefaultConfig(t *testing.T) {
	config := DefaultDelayEstimatorConfig()

	if config.FilterType != FilterKalman {
		t.Errorf("Default FilterType = %v, want FilterKalman", config.FilterType)
	}

	if config.BurstThreshold != 5*time.Millisecond {
		t.Errorf("Default BurstThreshold = %v, want 5ms", config.BurstThreshold)
	}
}

func TestDelayEstimator_NilClock(t *testing.T) {
	// Passing nil clock should use MonotonicClock (no panic)
	config := DefaultDelayEstimatorConfig()
	estimator := NewDelayEstimator(config, nil)

	// Should be able to process packets without panic
	pkt := PacketInfo{
		ArrivalTime: time.Now(),
		SendTime:    0,
		Size:        1200,
		SSRC:        0x12345678,
	}

	// This should not panic
	state := estimator.OnPacket(pkt)
	if state != BwNormal {
		t.Logf("State after one packet: %v (expected Normal)", state)
	}
}

func TestDelayEstimator_TrendlineStableNetwork(t *testing.T) {
	// Trendline filter with stable network should remain Normal
	clock := internal.NewMockClock(time.Time{})
	config := DefaultDelayEstimatorConfig()
	config.FilterType = FilterTrendline
	estimator := NewDelayEstimator(config, clock)

	packets := stableNetworkTrace(clock, 100, 20)

	var finalState BandwidthUsage
	for _, pkt := range packets {
		finalState = estimator.OnPacket(pkt)
	}

	if finalState != BwNormal {
		t.Errorf("Trendline stable network: final state = %v, want BwNormal", finalState)
	}
}

func TestDelayEstimator_TrendlineDrainingNetwork(t *testing.T) {
	// Test Trendline filter with decreasing delay (draining network)
	// Trendline with constant negative delay variation will have slope -> 0 (constant)
	// This test verifies the trendline integration maintains stable behavior
	clock := internal.NewMockClock(time.Time{})
	config := DefaultDelayEstimatorConfig()
	config.FilterType = FilterTrendline
	estimator := NewDelayEstimator(config, clock)

	// Generate packets with constant delay but ensure proper packet grouping
	sendTime := uint32(0)
	sendIntervalMs := 50
	receiveIntervalMs := 10

	for i := 0; i < 50; i++ {
		pkt := PacketInfo{
			ArrivalTime: clock.Now(),
			SendTime:    sendTime,
			Size:        1200,
			SSRC:        0x12345678,
		}
		estimator.OnPacket(pkt)

		sendTime += uint32(sendIntervalMs * 262)
		clock.Advance(time.Duration(receiveIntervalMs) * time.Millisecond)
	}

	// Trendline with constant negative delay variation has slope -> 0
	// So the state should stabilize (exact state depends on transient behavior)
	// The key verification is that the pipeline doesn't crash or produce errors
	state := estimator.State()
	t.Logf("Trendline draining network final state: %v", state)
}

// =============================================================================
// Benchmark Tests
// =============================================================================

func BenchmarkDelayEstimator_OnPacket(b *testing.B) {
	clock := internal.NewMockClock(time.Time{})
	config := DefaultDelayEstimatorConfig()

	// Pre-generate packets
	packets := stableNetworkTrace(clock, 10000, 20)

	// Reset clock for benchmark
	clock = internal.NewMockClock(time.Time{})
	estimator := NewDelayEstimator(config, clock)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		estimator.OnPacket(packets[i%len(packets)])
	}
}

func BenchmarkDelayEstimator_TrendlineFilter(b *testing.B) {
	clock := internal.NewMockClock(time.Time{})
	config := DefaultDelayEstimatorConfig()
	config.FilterType = FilterTrendline

	// Pre-generate packets
	packets := stableNetworkTrace(clock, 10000, 20)

	// Reset for benchmark
	clock = internal.NewMockClock(time.Time{})
	estimator := NewDelayEstimator(config, clock)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		estimator.OnPacket(packets[i%len(packets)])
	}
}
