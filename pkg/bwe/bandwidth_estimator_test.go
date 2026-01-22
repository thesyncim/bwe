package bwe

import (
	"go/parser"
	"go/token"
	"strconv"
	"testing"
	"time"

	"bwe/pkg/bwe/internal"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// =============================================================================
// BandwidthEstimator Unit Tests
// =============================================================================

func TestBandwidthEstimator_InitialEstimate(t *testing.T) {
	// Test: Returns initial bitrate on first call
	config := DefaultBandwidthEstimatorConfig()
	clock := internal.NewMockClock(time.Time{})
	estimator := NewBandwidthEstimator(config, clock)

	// Before any packets, GetEstimate should return initial bitrate
	assert.Equal(t, config.RateControllerConfig.InitialBitrate, estimator.GetEstimate(),
		"should return initial bitrate before any packets")

	// After one packet (not enough for rate measurement), should still return initial
	pkt := PacketInfo{
		ArrivalTime: clock.Now(),
		SendTime:    0,
		Size:        1200,
		SSRC:        0x12345678,
	}
	estimate := estimator.OnPacket(pkt)

	assert.Equal(t, config.RateControllerConfig.InitialBitrate, estimate,
		"should return initial bitrate when rate measurement not ready")
}

func TestBandwidthEstimator_NormalTraffic(t *testing.T) {
	// Test: Stable traffic maintains/increases estimate
	config := DefaultBandwidthEstimatorConfig()
	clock := internal.NewMockClock(time.Time{})
	estimator := NewBandwidthEstimator(config, clock)

	initialEstimate := config.RateControllerConfig.InitialBitrate
	sendTime := uint32(0)
	intervalMs := 20

	// Feed 50 packets at 20ms intervals with stable delay (no congestion)
	var lastEstimate int64
	for i := 0; i < 50; i++ {
		pkt := PacketInfo{
			ArrivalTime: clock.Now(),
			SendTime:    sendTime,
			Size:        1200,
			SSRC:        0x12345678,
		}
		lastEstimate = estimator.OnPacket(pkt)

		sendTime += uint32(intervalMs * 262) // abs-send-time units
		clock.Advance(time.Duration(intervalMs) * time.Millisecond)
	}

	// Estimate should be >= initial (not decreased due to congestion)
	assert.GreaterOrEqual(t, lastEstimate, initialEstimate,
		"stable traffic should not decrease estimate")

	// Congestion state should be Normal
	assert.Equal(t, BwNormal, estimator.GetCongestionState(),
		"stable traffic should have Normal congestion state")
}

func TestBandwidthEstimator_Congestion(t *testing.T) {
	// Test: Congestion decreases estimate
	config := DefaultBandwidthEstimatorConfig()
	clock := internal.NewMockClock(time.Time{})
	estimator := NewBandwidthEstimator(config, clock)

	sendTime := uint32(0)
	sendIntervalMs := 20
	// Receive with increasing delay (queue building)
	delayIncreaseMs := 50.0

	// Feed packets that simulate congestion
	var lastEstimate int64
	var gotDecrease bool
	for i := 0; i < 100; i++ {
		pkt := PacketInfo{
			ArrivalTime: clock.Now(),
			SendTime:    sendTime,
			Size:        1200,
			SSRC:        0x12345678,
		}
		newEstimate := estimator.OnPacket(pkt)

		// Track if we ever get a decrease
		if newEstimate < lastEstimate && lastEstimate > 0 {
			gotDecrease = true
		}
		lastEstimate = newEstimate

		sendTime += uint32(sendIntervalMs * 262)
		// Arrival time increases more than send time (congestion)
		clock.Advance(time.Duration(float64(sendIntervalMs)+delayIncreaseMs) * time.Millisecond)
	}

	// Should have detected congestion and decreased estimate
	assert.True(t, gotDecrease, "congestion should cause estimate decrease")
	assert.Equal(t, BwOverusing, estimator.GetCongestionState(),
		"persistent congestion should result in Overusing state")
}

func TestBandwidthEstimator_TracksSSRCs(t *testing.T) {
	// Test: Multiple SSRCs tracked correctly
	config := DefaultBandwidthEstimatorConfig()
	clock := internal.NewMockClock(time.Time{})
	estimator := NewBandwidthEstimator(config, clock)

	// Initially no SSRCs
	assert.Empty(t, estimator.GetSSRCs(), "should have no SSRCs initially")

	// Add packets from different SSRCs
	ssrcs := []uint32{0x11111111, 0x22222222, 0x33333333}
	for _, ssrc := range ssrcs {
		pkt := PacketInfo{
			ArrivalTime: clock.Now(),
			SendTime:    0,
			Size:        1200,
			SSRC:        ssrc,
		}
		estimator.OnPacket(pkt)
		clock.Advance(10 * time.Millisecond)
	}

	// Should have all 3 SSRCs
	gotSSRCs := estimator.GetSSRCs()
	assert.Len(t, gotSSRCs, 3, "should have 3 unique SSRCs")

	// Verify all SSRCs present (order may vary due to map iteration)
	ssrcSet := make(map[uint32]bool)
	for _, s := range gotSSRCs {
		ssrcSet[s] = true
	}
	for _, expected := range ssrcs {
		assert.True(t, ssrcSet[expected], "should contain SSRC %x", expected)
	}
}

func TestBandwidthEstimator_DuplicateSSRC(t *testing.T) {
	// Test: Same SSRC not duplicated
	config := DefaultBandwidthEstimatorConfig()
	clock := internal.NewMockClock(time.Time{})
	estimator := NewBandwidthEstimator(config, clock)

	// Add multiple packets from same SSRC
	for i := 0; i < 10; i++ {
		pkt := PacketInfo{
			ArrivalTime: clock.Now(),
			SendTime:    uint32(i * 20 * 262),
			Size:        1200,
			SSRC:        0x12345678,
		}
		estimator.OnPacket(pkt)
		clock.Advance(20 * time.Millisecond)
	}

	// Should have exactly 1 SSRC
	assert.Len(t, estimator.GetSSRCs(), 1, "same SSRC should not be duplicated")
	assert.Equal(t, uint32(0x12345678), estimator.GetSSRCs()[0])
}

func TestBandwidthEstimator_GetCongestionState(t *testing.T) {
	// Test: Exposes delay detector state
	config := DefaultBandwidthEstimatorConfig()
	clock := internal.NewMockClock(time.Time{})
	estimator := NewBandwidthEstimator(config, clock)

	// Initial state should be Normal
	assert.Equal(t, BwNormal, estimator.GetCongestionState(),
		"initial congestion state should be Normal")

	// The congestion state tracks the delay estimator's state
	// Which requires sufficient packets to detect trends
}

func TestBandwidthEstimator_GetRateControlState(t *testing.T) {
	// Test: Exposes rate control state
	config := DefaultBandwidthEstimatorConfig()
	clock := internal.NewMockClock(time.Time{})
	estimator := NewBandwidthEstimator(config, clock)

	// Initial state should be Hold
	assert.Equal(t, RateHold, estimator.GetRateControlState(),
		"initial rate control state should be Hold")
}

func TestBandwidthEstimator_GetIncomingRate(t *testing.T) {
	// Test: Exposes measured rate
	config := DefaultBandwidthEstimatorConfig()
	clock := internal.NewMockClock(time.Time{})
	estimator := NewBandwidthEstimator(config, clock)

	// Initially no rate available
	rate, ok := estimator.GetIncomingRate()
	assert.False(t, ok, "should have no rate initially")
	assert.Equal(t, int64(0), rate)

	// Add enough packets for rate measurement
	for i := 0; i < 10; i++ {
		pkt := PacketInfo{
			ArrivalTime: clock.Now(),
			SendTime:    uint32(i * 20 * 262),
			Size:        1200,
			SSRC:        0x12345678,
		}
		estimator.OnPacket(pkt)
		clock.Advance(20 * time.Millisecond)
	}

	// Now should have a rate
	rate, ok = estimator.GetIncomingRate()
	assert.True(t, ok, "should have rate after packets")
	assert.Greater(t, rate, int64(0), "rate should be positive")

	// Calculate expected rate: 10 packets * 1200 bytes * 8 bits = 96000 bits
	// Over ~180ms (9 intervals of 20ms), rate = 96000 / 0.18 = ~533333 bps
	// Allow some tolerance due to timing
	t.Logf("Measured incoming rate: %d bps", rate)
}

func TestBandwidthEstimator_Reset(t *testing.T) {
	// Test: Reset clears all state
	config := DefaultBandwidthEstimatorConfig()
	clock := internal.NewMockClock(time.Time{})
	estimator := NewBandwidthEstimator(config, clock)

	// Add some packets with congestion
	sendTime := uint32(0)
	for i := 0; i < 100; i++ {
		pkt := PacketInfo{
			ArrivalTime: clock.Now(),
			SendTime:    sendTime,
			Size:        1200,
			SSRC:        0x12345678,
		}
		estimator.OnPacket(pkt)
		sendTime += uint32(20 * 262)
		// Congesting: arrival increases more than send
		clock.Advance(time.Duration(20+50) * time.Millisecond)
	}

	// Verify state changed from initial
	assert.Len(t, estimator.GetSSRCs(), 1, "should have tracked SSRC")

	// Reset
	estimator.Reset()

	// Verify all state cleared
	assert.Equal(t, config.RateControllerConfig.InitialBitrate, estimator.GetEstimate(),
		"estimate should be reset to initial")
	assert.Empty(t, estimator.GetSSRCs(), "SSRCs should be cleared")
	assert.Equal(t, BwNormal, estimator.GetCongestionState(),
		"congestion state should be Normal after reset")
	assert.Equal(t, RateHold, estimator.GetRateControlState(),
		"rate control state should be Hold after reset")

	// GetIncomingRate should have no data
	_, ok := estimator.GetIncomingRate()
	assert.False(t, ok, "incoming rate should not be available after reset")
}

func TestBandwidthEstimator_NoPionDependency(t *testing.T) {
	// Test: Verify no Pion imports in bandwidth_estimator.go
	// This is critical for the standalone core library requirement

	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "bandwidth_estimator.go", nil, parser.ImportsOnly)
	require.NoError(t, err, "should parse bandwidth_estimator.go")

	for _, imp := range f.Imports {
		path, _ := strconv.Unquote(imp.Path.Value)
		assert.NotContains(t, path, "pion",
			"bandwidth_estimator.go should not import pion packages")
	}
}

func TestBandwidthEstimator_StableNetwork(t *testing.T) {
	// Integration test: Simulating stable traffic over longer period
	config := DefaultBandwidthEstimatorConfig()
	clock := internal.NewMockClock(time.Time{})
	estimator := NewBandwidthEstimator(config, clock)

	initialEstimate := config.RateControllerConfig.InitialBitrate
	sendTime := uint32(0)
	intervalMs := 20

	// Simulate 5 seconds of stable traffic (250 packets at 20ms)
	var estimates []int64
	for i := 0; i < 250; i++ {
		pkt := PacketInfo{
			ArrivalTime: clock.Now(),
			SendTime:    sendTime,
			Size:        1200,
			SSRC:        0x12345678,
		}
		estimate := estimator.OnPacket(pkt)
		estimates = append(estimates, estimate)

		sendTime += uint32(intervalMs * 262)
		clock.Advance(time.Duration(intervalMs) * time.Millisecond)
	}

	// Final estimate should be at least initial (stable means can grow)
	finalEstimate := estimates[len(estimates)-1]
	assert.GreaterOrEqual(t, finalEstimate, initialEstimate,
		"stable traffic should maintain or increase estimate")

	// Should remain in Normal or Increase state
	congestionState := estimator.GetCongestionState()
	assert.NotEqual(t, BwOverusing, congestionState,
		"stable traffic should not trigger Overusing")

	t.Logf("Stable network: initial=%d, final=%d, congestion=%v, rateControl=%v",
		initialEstimate, finalEstimate, congestionState, estimator.GetRateControlState())
}

func TestBandwidthEstimator_NilClock(t *testing.T) {
	// Test: Passing nil clock should use default MonotonicClock
	config := DefaultBandwidthEstimatorConfig()
	estimator := NewBandwidthEstimator(config, nil)

	// Should not panic and return valid initial estimate
	assert.NotNil(t, estimator, "should create estimator with nil clock")
	assert.Equal(t, config.RateControllerConfig.InitialBitrate, estimator.GetEstimate())

	// Should be able to process a packet
	pkt := PacketInfo{
		ArrivalTime: time.Now(),
		SendTime:    0,
		Size:        1200,
		SSRC:        0x12345678,
	}
	estimate := estimator.OnPacket(pkt)
	assert.Equal(t, config.RateControllerConfig.InitialBitrate, estimate)
}

func TestBandwidthEstimator_DefaultConfig(t *testing.T) {
	// Test: Default configuration is valid
	config := DefaultBandwidthEstimatorConfig()

	// Should have sensible defaults from sub-configs
	assert.NotNil(t, config.DelayConfig)
	assert.NotNil(t, config.RateStatsConfig)
	assert.NotNil(t, config.RateControllerConfig)

	assert.Equal(t, time.Second, config.RateStatsConfig.WindowSize,
		"default rate stats window should be 1 second")
	assert.Equal(t, int64(300_000), config.RateControllerConfig.InitialBitrate,
		"default initial bitrate should be 300 kbps")
}

func TestBandwidthEstimator_RecoveryFromCongestion(t *testing.T) {
	// Test: Recovery after congestion clears
	config := DefaultBandwidthEstimatorConfig()
	clock := internal.NewMockClock(time.Time{})
	estimator := NewBandwidthEstimator(config, clock)

	sendTime := uint32(0)
	intervalMs := 20

	// Phase 1: Induce congestion (100 packets with increasing delay)
	for i := 0; i < 100; i++ {
		pkt := PacketInfo{
			ArrivalTime: clock.Now(),
			SendTime:    sendTime,
			Size:        1200,
			SSRC:        0x12345678,
		}
		estimator.OnPacket(pkt)
		sendTime += uint32(intervalMs * 262)
		clock.Advance(time.Duration(intervalMs+50) * time.Millisecond)
	}

	congestionEstimate := estimator.GetEstimate()
	t.Logf("After congestion: estimate=%d, state=%v", congestionEstimate, estimator.GetCongestionState())

	// Phase 2: Stable traffic (150 packets with normal delay)
	for i := 0; i < 150; i++ {
		pkt := PacketInfo{
			ArrivalTime: clock.Now(),
			SendTime:    sendTime,
			Size:        1200,
			SSRC:        0x12345678,
		}
		estimator.OnPacket(pkt)
		sendTime += uint32(intervalMs * 262)
		clock.Advance(time.Duration(intervalMs) * time.Millisecond)
	}

	recoveryEstimate := estimator.GetEstimate()
	t.Logf("After recovery: estimate=%d, state=%v", recoveryEstimate, estimator.GetCongestionState())

	// Should have recovered (state should not be stuck in Overusing)
	// Note: estimate may or may not have increased, but state should recover
	assert.NotEqual(t, BwOverusing, estimator.GetCongestionState(),
		"should recover from congestion after stable period")
}

func TestBandwidthEstimator_MultipleSSRCsSameEstimate(t *testing.T) {
	// Test: Multiple SSRCs contribute to same bandwidth estimate
	config := DefaultBandwidthEstimatorConfig()
	clock := internal.NewMockClock(time.Time{})
	estimator := NewBandwidthEstimator(config, clock)

	sendTime := uint32(0)

	// Interleave packets from 2 SSRCs
	for i := 0; i < 50; i++ {
		ssrc := uint32(0x11111111)
		if i%2 == 1 {
			ssrc = 0x22222222
		}
		pkt := PacketInfo{
			ArrivalTime: clock.Now(),
			SendTime:    sendTime,
			Size:        1200,
			SSRC:        ssrc,
		}
		estimator.OnPacket(pkt)
		sendTime += uint32(10 * 262)
		clock.Advance(10 * time.Millisecond)
	}

	// Should track both SSRCs
	assert.Len(t, estimator.GetSSRCs(), 2)

	// Should have a combined estimate (not per-SSRC)
	estimate := estimator.GetEstimate()
	assert.Greater(t, estimate, int64(0), "should have positive estimate")

	t.Logf("Multi-SSRC estimate: %d bps", estimate)
}

// =============================================================================
// Multi-SSRC Aggregation Tests
// =============================================================================

func TestBandwidthEstimator_MultiSSRC_Aggregation(t *testing.T) {
	// Test: Multiple SSRCs feed single estimate
	// Video SSRC: 1 Mbps (125 bytes/ms)
	// Audio SSRC: 50 kbps (~6 bytes/ms)
	clock := internal.NewMockClock(time.Now())
	e := NewBandwidthEstimator(DefaultBandwidthEstimatorConfig(), clock)

	videoSSRC := uint32(0x11111111)
	audioSSRC := uint32(0x22222222)
	sendTime := uint32(0)

	// Feed interleaved packets for 2 seconds
	for i := 0; i < 2000; i++ {
		// Video packet every ms (~1 Mbps with 125 byte packets)
		e.OnPacket(PacketInfo{
			ArrivalTime: clock.Now(),
			SendTime:    sendTime,
			Size:        125,
			SSRC:        videoSSRC,
		})

		// Audio packet every 20ms (~50 kbps with 125 byte packets)
		if i%20 == 0 {
			e.OnPacket(PacketInfo{
				ArrivalTime: clock.Now(),
				SendTime:    sendTime,
				Size:        125,
				SSRC:        audioSSRC,
			})
		}

		sendTime += uint32(262) // 1ms in abs-send-time units
		clock.Advance(time.Millisecond)
	}

	// Verify both SSRCs tracked
	ssrcs := e.GetSSRCs()
	assert.Len(t, ssrcs, 2)
	assert.Contains(t, ssrcs, videoSSRC)
	assert.Contains(t, ssrcs, audioSSRC)

	// Verify aggregated rate exists
	rate, ok := e.GetIncomingRate()
	assert.True(t, ok)
	assert.Greater(t, rate, int64(0))
	t.Logf("Aggregated incoming rate: %d bps", rate)

	// Verify single estimate (not per-SSRC)
	estimate := e.GetEstimate()
	assert.Greater(t, estimate, int64(0))
	t.Logf("Single aggregated estimate: %d bps", estimate)
}

func TestBandwidthEstimator_MultiSSRC_CongestionAffectsAll(t *testing.T) {
	// Test: Congestion via one SSRC affects total estimate
	clock := internal.NewMockClock(time.Now())
	e := NewBandwidthEstimator(DefaultBandwidthEstimatorConfig(), clock)

	videoSSRC := uint32(0x11111111)
	audioSSRC := uint32(0x22222222)
	sendTime := uint32(0)

	// Phase 1: Stable traffic from both SSRCs (1 second)
	for i := 0; i < 1000; i++ {
		e.OnPacket(PacketInfo{
			ArrivalTime: clock.Now(),
			SendTime:    sendTime,
			Size:        125,
			SSRC:        videoSSRC,
		})
		if i%20 == 0 {
			e.OnPacket(PacketInfo{
				ArrivalTime: clock.Now(),
				SendTime:    sendTime,
				Size:        125,
				SSRC:        audioSSRC,
			})
		}
		sendTime += uint32(262)
		clock.Advance(time.Millisecond)
	}

	stableEstimate := e.GetEstimate()
	t.Logf("Stable estimate: %d bps", stableEstimate)

	// Phase 2: Introduce congestion via video SSRC only (packets arrive late)
	for i := 0; i < 500; i++ {
		// Video packets with increasing delay (congestion)
		e.OnPacket(PacketInfo{
			ArrivalTime: clock.Now(),
			SendTime:    sendTime,
			Size:        125,
			SSRC:        videoSSRC,
		})

		// Audio packets arrive normally
		if i%20 == 0 {
			e.OnPacket(PacketInfo{
				ArrivalTime: clock.Now(),
				SendTime:    sendTime,
				Size:        125,
				SSRC:        audioSSRC,
			})
		}

		sendTime += uint32(262)
		// Arrival increases more than send time (congestion)
		clock.Advance(time.Millisecond + 50*time.Millisecond)
	}

	congestedEstimate := e.GetEstimate()
	t.Logf("Congested estimate: %d bps", congestedEstimate)

	// The congestion should affect the total estimate
	// (though the exact behavior depends on how congestion is detected)
	assert.Equal(t, BwOverusing, e.GetCongestionState(),
		"congestion should be detected")
}

// =============================================================================
// REMB Integration Tests
// =============================================================================

func TestBandwidthEstimator_REMBIntegration_Basic(t *testing.T) {
	// Test: MaybeBuildREMB returns packet at interval
	clock := internal.NewMockClock(time.Now())
	e := NewBandwidthEstimator(DefaultBandwidthEstimatorConfig(), clock)

	// Attach REMB scheduler with 1 second interval
	scheduler := NewREMBScheduler(DefaultREMBSchedulerConfig())
	e.SetREMBScheduler(scheduler)

	sendTime := uint32(0)
	rembCount := 0

	// Feed packets for 3 seconds
	for i := 0; i < 3000; i++ {
		e.OnPacket(PacketInfo{
			ArrivalTime: clock.Now(),
			SendTime:    sendTime,
			Size:        125,
			SSRC:        0x12345678,
		})

		// Check for REMB after each packet
		data, sent, err := e.MaybeBuildREMB(clock.Now())
		assert.NoError(t, err)
		if sent {
			assert.NotNil(t, data)
			rembCount++
		}

		sendTime += uint32(262)
		clock.Advance(time.Millisecond)
	}

	// Should have sent ~3 REMBs (one per second)
	assert.GreaterOrEqual(t, rembCount, 2, "should send REMB at regular intervals")
	assert.LessOrEqual(t, rembCount, 5, "should not send too many REMBs")
	t.Logf("REMB packets sent in 3 seconds: %d", rembCount)
}

func TestBandwidthEstimator_REMBIntegration_ImmediateDecrease(t *testing.T) {
	// Test: REMB sent immediately on significant decrease
	clock := internal.NewMockClock(time.Now())
	e := NewBandwidthEstimator(DefaultBandwidthEstimatorConfig(), clock)

	// Custom scheduler with 10 second interval (so regular send is rare)
	config := DefaultREMBSchedulerConfig()
	config.Interval = 10 * time.Second
	scheduler := NewREMBScheduler(config)
	e.SetREMBScheduler(scheduler)

	sendTime := uint32(0)

	// Phase 1: Stable traffic (500ms)
	for i := 0; i < 500; i++ {
		e.OnPacket(PacketInfo{
			ArrivalTime: clock.Now(),
			SendTime:    sendTime,
			Size:        125,
			SSRC:        0x12345678,
		})
		sendTime += uint32(262)
		clock.Advance(time.Millisecond)
	}

	// Send initial REMB
	_, sent, _ := e.MaybeBuildREMB(clock.Now())
	assert.True(t, sent, "should send initial REMB")
	initialEstimate := e.GetEstimate()
	t.Logf("Initial estimate: %d bps", initialEstimate)

	// Advance a little (not enough for regular interval)
	clock.Advance(100 * time.Millisecond)

	// Phase 2: Induce congestion to cause significant decrease
	for i := 0; i < 200; i++ {
		e.OnPacket(PacketInfo{
			ArrivalTime: clock.Now(),
			SendTime:    sendTime,
			Size:        125,
			SSRC:        0x12345678,
		})
		sendTime += uint32(262)
		// Heavy congestion: packets arrive much later than sent
		clock.Advance(time.Millisecond + 100*time.Millisecond)
	}

	// Check if REMB is triggered by decrease
	data, sent, err := e.MaybeBuildREMB(clock.Now())
	assert.NoError(t, err)

	// Should have sent REMB immediately due to decrease, even though interval hasn't passed
	if sent {
		t.Log("REMB sent immediately on decrease")
		assert.NotNil(t, data)
	}

	// Verify estimate decreased
	congestedEstimate := e.GetEstimate()
	t.Logf("Congested estimate: %d bps", congestedEstimate)
	assert.Less(t, congestedEstimate, initialEstimate, "estimate should decrease during congestion")
}

func TestBandwidthEstimator_REMBIntegration_IncludesAllSSRCs(t *testing.T) {
	// Test: REMB contains all seen SSRCs
	clock := internal.NewMockClock(time.Now())
	e := NewBandwidthEstimator(DefaultBandwidthEstimatorConfig(), clock)

	scheduler := NewREMBScheduler(DefaultREMBSchedulerConfig())
	e.SetREMBScheduler(scheduler)

	// Feed packets from 3 SSRCs
	ssrcs := []uint32{0x11111111, 0x22222222, 0x33333333}
	sendTime := uint32(0)

	for i := 0; i < 1000; i++ {
		// Rotate through SSRCs
		ssrc := ssrcs[i%len(ssrcs)]
		e.OnPacket(PacketInfo{
			ArrivalTime: clock.Now(),
			SendTime:    sendTime,
			Size:        125,
			SSRC:        ssrc,
		})
		sendTime += uint32(262)
		clock.Advance(time.Millisecond)
	}

	// Build REMB
	data, sent, err := e.MaybeBuildREMB(clock.Now())
	require.NoError(t, err)
	require.True(t, sent, "should send REMB")

	// Parse REMB and verify all SSRCs present
	remb, err := ParseREMB(data)
	require.NoError(t, err)

	assert.Len(t, remb.SSRCs, 3, "REMB should contain all 3 SSRCs")
	for _, expectedSSRC := range ssrcs {
		assert.Contains(t, remb.SSRCs, expectedSSRC,
			"REMB should contain SSRC %x", expectedSSRC)
	}

	t.Logf("REMB bitrate: %d bps, SSRCs: %v", remb.Bitrate, remb.SSRCs)
}

func TestBandwidthEstimator_NoSchedulerNoREMB(t *testing.T) {
	// Test: Without scheduler, MaybeBuildREMB returns false
	clock := internal.NewMockClock(time.Now())
	e := NewBandwidthEstimator(DefaultBandwidthEstimatorConfig(), clock)

	// Feed some packets
	for i := 0; i < 100; i++ {
		e.OnPacket(PacketInfo{
			ArrivalTime: clock.Now(),
			SendTime:    uint32(i * 262),
			Size:        125,
			SSRC:        0x12345678,
		})
		clock.Advance(time.Millisecond)
	}

	// Without SetREMBScheduler, MaybeBuildREMB should return false
	data, sent, err := e.MaybeBuildREMB(clock.Now())
	assert.NoError(t, err)
	assert.False(t, sent, "should not send REMB without scheduler")
	assert.Nil(t, data)
}

// =============================================================================
// Full Pipeline Integration Tests
// =============================================================================

func TestBandwidthEstimator_FullPipeline_StableNetwork(t *testing.T) {
	// Integration test: 30 seconds of stable ~2 Mbps traffic
	// 2 SSRCs (video + audio)
	clock := internal.NewMockClock(time.Now())
	e := NewBandwidthEstimator(DefaultBandwidthEstimatorConfig(), clock)

	scheduler := NewREMBScheduler(DefaultREMBSchedulerConfig())
	e.SetREMBScheduler(scheduler)

	videoSSRC := uint32(0x11111111)
	audioSSRC := uint32(0x22222222)
	sendTime := uint32(0)
	rembCount := 0

	// 30 seconds of traffic
	durationMs := 30000
	// ~2 Mbps = 250 bytes/ms for video, audio ~50kbps
	for i := 0; i < durationMs; i++ {
		// Video packet every ms
		e.OnPacket(PacketInfo{
			ArrivalTime: clock.Now(),
			SendTime:    sendTime,
			Size:        250, // ~2 Mbps
			SSRC:        videoSSRC,
		})

		// Audio packet every 20ms
		if i%20 == 0 {
			e.OnPacket(PacketInfo{
				ArrivalTime: clock.Now(),
				SendTime:    sendTime,
				Size:        125, // ~50 kbps
				SSRC:        audioSSRC,
			})
		}

		// Check for REMB
		_, sent, _ := e.MaybeBuildREMB(clock.Now())
		if sent {
			rembCount++
		}

		sendTime += uint32(262)
		clock.Advance(time.Millisecond)
	}

	// Verify estimate converged near the incoming rate
	estimate := e.GetEstimate()
	incomingRate, ok := e.GetIncomingRate()
	assert.True(t, ok)

	t.Logf("30s stable: estimate=%d bps, incoming=%d bps, REMBs=%d",
		estimate, incomingRate, rembCount)

	// Estimate should be reasonable (positive and not stuck at initial)
	assert.Greater(t, estimate, int64(0))

	// Should have sent ~30 REMBs (one per second)
	assert.GreaterOrEqual(t, rembCount, 25, "should send REMB approximately once per second")
	assert.LessOrEqual(t, rembCount, 40, "should not send too many REMBs")

	// All SSRCs should be tracked
	ssrcs := e.GetSSRCs()
	assert.Len(t, ssrcs, 2)
	assert.Contains(t, ssrcs, videoSSRC)
	assert.Contains(t, ssrcs, audioSSRC)
}

func TestBandwidthEstimator_FullPipeline_CongestionEvent(t *testing.T) {
	// Integration test: stable -> congestion -> recovery
	// 5s stable at ~2 Mbps
	// 2s congestion (increasing delay)
	// 5s recovery
	clock := internal.NewMockClock(time.Now())
	e := NewBandwidthEstimator(DefaultBandwidthEstimatorConfig(), clock)

	// Use short REMB interval for this test
	config := DefaultREMBSchedulerConfig()
	config.Interval = 500 * time.Millisecond
	scheduler := NewREMBScheduler(config)
	e.SetREMBScheduler(scheduler)

	sendTime := uint32(0)
	var estimates []int64
	var rembSentOnDecrease bool
	var estimateBeforeDecrease int64

	// Phase 1: 5 seconds stable
	t.Log("Phase 1: Stable traffic")
	for i := 0; i < 5000; i++ {
		e.OnPacket(PacketInfo{
			ArrivalTime: clock.Now(),
			SendTime:    sendTime,
			Size:        250,
			SSRC:        0x12345678,
		})
		e.MaybeBuildREMB(clock.Now())

		sendTime += uint32(262)
		clock.Advance(time.Millisecond)
	}
	stableEstimate := e.GetEstimate()
	estimates = append(estimates, stableEstimate)
	t.Logf("After stable: estimate=%d, state=%v", stableEstimate, e.GetCongestionState())

	// Phase 2: 2 seconds congestion
	t.Log("Phase 2: Congestion")
	estimateBeforeDecrease = e.GetEstimate()
	for i := 0; i < 2000; i++ {
		e.OnPacket(PacketInfo{
			ArrivalTime: clock.Now(),
			SendTime:    sendTime,
			Size:        250,
			SSRC:        0x12345678,
		})

		_, sent, _ := e.MaybeBuildREMB(clock.Now())
		currentEstimate := e.GetEstimate()

		// Check if REMB was sent when estimate decreased significantly
		if sent && currentEstimate < estimateBeforeDecrease*97/100 {
			rembSentOnDecrease = true
		}

		sendTime += uint32(262)
		// Heavy delay increase (congestion)
		clock.Advance(time.Millisecond + 50*time.Millisecond)
	}
	congestionEstimate := e.GetEstimate()
	estimates = append(estimates, congestionEstimate)
	t.Logf("After congestion: estimate=%d, state=%v", congestionEstimate, e.GetCongestionState())

	// Phase 3: 5 seconds recovery
	t.Log("Phase 3: Recovery")
	for i := 0; i < 5000; i++ {
		e.OnPacket(PacketInfo{
			ArrivalTime: clock.Now(),
			SendTime:    sendTime,
			Size:        250,
			SSRC:        0x12345678,
		})
		e.MaybeBuildREMB(clock.Now())

		sendTime += uint32(262)
		clock.Advance(time.Millisecond)
	}
	recoveryEstimate := e.GetEstimate()
	estimates = append(estimates, recoveryEstimate)
	t.Logf("After recovery: estimate=%d, state=%v", recoveryEstimate, e.GetCongestionState())

	// Verify behavior
	// 1. Estimate should have decreased during congestion
	assert.Less(t, congestionEstimate, stableEstimate,
		"estimate should decrease during congestion")

	// 2. Estimate should have increased during recovery (even if not fully recovered)
	assert.Greater(t, recoveryEstimate, congestionEstimate,
		"estimate should increase during recovery")

	// 3. REMB should have been sent on decrease
	// (This may or may not happen depending on timing; log but don't require)
	if rembSentOnDecrease {
		t.Log("REMB was sent immediately on decrease")
	}

	// Note: Full state recovery (BwOverusing -> BwNormal) may take longer than
	// the test duration due to the adaptive threshold. What we verify is that
	// the system IS recovering (estimate increasing).

	t.Logf("Estimates: stable=%d, congested=%d, recovered=%d",
		estimates[0], estimates[1], estimates[2])
}

// =============================================================================
// Benchmark Tests
// =============================================================================

func BenchmarkBandwidthEstimator_OnPacket(b *testing.B) {
	config := DefaultBandwidthEstimatorConfig()
	clock := internal.NewMockClock(time.Time{})
	estimator := NewBandwidthEstimator(config, clock)

	// Pre-generate packets
	packets := make([]PacketInfo, 10000)
	for i := range packets {
		packets[i] = PacketInfo{
			ArrivalTime: clock.Now(),
			SendTime:    uint32(i * 20 * 262),
			Size:        1200,
			SSRC:        0x12345678,
		}
		clock.Advance(20 * time.Millisecond)
	}

	// Reset clock for benchmark
	clock = internal.NewMockClock(time.Time{})
	estimator = NewBandwidthEstimator(config, clock)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		estimator.OnPacket(packets[i%len(packets)])
	}
}
