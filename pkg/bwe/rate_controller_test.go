package bwe

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestRateController_InitialState(t *testing.T) {
	config := DefaultRateControllerConfig()
	rc := NewRateController(config)

	// Should start in Hold state
	assert.Equal(t, RateHold, rc.State(), "should start in Hold state")

	// Should have initial bitrate
	assert.Equal(t, config.InitialBitrate, rc.Estimate(), "should have initial bitrate")
}

func TestRateController_StateTransitions(t *testing.T) {
	// Test the state transition table from GCC spec Section 6:
	//
	// Signal     | Hold     | Increase | Decrease
	// -----------+----------+----------+----------
	// Overusing  | Decrease | Decrease | (stay)
	// Normal     | Increase | (stay)   | Hold
	// Underusing | (stay)   | Hold     | Hold

	tests := []struct {
		name       string
		startState RateControlState
		signal     BandwidthUsage
		endState   RateControlState
	}{
		// From Hold state
		{"Hold + Overusing -> Decrease", RateHold, BwOverusing, RateDecrease},
		{"Hold + Normal -> Increase", RateHold, BwNormal, RateIncrease},
		{"Hold + Underusing -> Hold", RateHold, BwUnderusing, RateHold},

		// From Increase state
		{"Increase + Overusing -> Decrease", RateIncrease, BwOverusing, RateDecrease},
		{"Increase + Normal -> Increase", RateIncrease, BwNormal, RateIncrease},
		{"Increase + Underusing -> Hold", RateIncrease, BwUnderusing, RateHold},

		// From Decrease state
		{"Decrease + Overusing -> Decrease", RateDecrease, BwOverusing, RateDecrease},
		{"Decrease + Normal -> Hold", RateDecrease, BwNormal, RateHold},
		{"Decrease + Underusing -> Hold", RateDecrease, BwUnderusing, RateHold},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rc := NewRateController(DefaultRateControllerConfig())
			rc.state = tt.startState // Force starting state

			now := time.Now()
			rc.Update(tt.signal, 1_000_000, now)

			assert.Equal(t, tt.endState, rc.State(), "unexpected state after transition")
		})
	}
}

func TestRateController_MultiplicativeDecrease(t *testing.T) {
	config := DefaultRateControllerConfig()
	config.Beta = 0.85
	rc := NewRateController(config)

	incomingRate := int64(1_000_000) // 1 Mbps
	now := time.Now()

	// Start in Hold, send Overusing to trigger decrease
	estimate := rc.Update(BwOverusing, incomingRate, now)

	// Should be 0.85 * incomingRate = 850,000
	expectedRate := int64(850_000)
	assert.Equal(t, expectedRate, estimate, "decrease should be 0.85 * incoming rate")
	assert.Equal(t, RateDecrease, rc.State(), "should be in Decrease state")
}

func TestRateController_DecreasesFromIncomingNotEstimate(t *testing.T) {
	// CRITICAL TEST: Verify that multiplicative decrease uses incomingRate,
	// NOT the current estimate. This is crucial when sender has already
	// reduced their rate but our estimate hasn't caught up.

	config := DefaultRateControllerConfig()
	config.Beta = 0.85
	config.InitialBitrate = 2_000_000 // Start with 2 Mbps estimate
	rc := NewRateController(config)

	// Sender has reduced to 1 Mbps
	incomingRate := int64(1_000_000)
	now := time.Now()

	// Trigger decrease
	estimate := rc.Update(BwOverusing, incomingRate, now)

	// Should be 0.85 * 1M = 850,000 (NOT 0.85 * 2M = 1,700,000)
	expectedRate := int64(850_000)
	wrongRate := int64(1_700_000) // What we'd get if using estimate

	assert.Equal(t, expectedRate, estimate,
		"decrease MUST use incoming rate, not estimate")
	assert.NotEqual(t, wrongRate, estimate,
		"should NOT use current estimate for decrease")
}

func TestRateController_MultiplicativeIncrease(t *testing.T) {
	config := DefaultRateControllerConfig()
	config.InitialBitrate = 1_000_000 // 1 Mbps
	rc := NewRateController(config)

	baseTime := time.Now()
	incomingRate := int64(2_000_000) // 2 Mbps incoming (plenty of headroom)

	// First update to set lastUpdate - go to Increase state
	rc.Update(BwNormal, incomingRate, baseTime)
	assert.Equal(t, RateIncrease, rc.State(), "should be in Increase state")

	// Record initial rate
	initialRate := rc.Estimate()

	// Second update 1 second later - should increase by 1.08x
	oneSecLater := baseTime.Add(time.Second)
	estimate := rc.Update(BwNormal, incomingRate, oneSecLater)

	// Expected: 1.08^1 * initialRate
	expectedRate := int64(1.08 * float64(initialRate))
	tolerance := int64(100) // Allow small rounding errors

	assert.InDelta(t, expectedRate, estimate, float64(tolerance),
		"increase should be ~1.08x after 1 second")
}

func TestRateController_HoldNoChange(t *testing.T) {
	config := DefaultRateControllerConfig()
	config.InitialBitrate = 1_000_000
	rc := NewRateController(config)

	baseTime := time.Now()
	incomingRate := int64(2_000_000)

	// Stay in Hold with Underusing signal
	estimate1 := rc.Update(BwUnderusing, incomingRate, baseTime)
	assert.Equal(t, RateHold, rc.State(), "should stay in Hold")

	// Update again in Hold state - should not change
	oneSecLater := baseTime.Add(time.Second)
	estimate2 := rc.Update(BwUnderusing, incomingRate, oneSecLater)

	assert.Equal(t, estimate1, estimate2, "rate should not change in Hold state")
}

func TestRateController_BoundsEnforced(t *testing.T) {
	t.Run("MinBitrate enforced on decrease", func(t *testing.T) {
		config := DefaultRateControllerConfig()
		config.MinBitrate = 50_000 // 50 kbps minimum
		config.Beta = 0.85
		rc := NewRateController(config)

		// Incoming rate high enough that 1.5x constraint doesn't apply
		// But 0.85 * 40k = 34k < 50k, so MinBitrate should kick in
		incomingRate := int64(40_000) // 40 kbps, 0.85 * 40k = 34k < 50k min
		now := time.Now()

		estimate := rc.Update(BwOverusing, incomingRate, now)

		// Should be clamped to MinBitrate (50k), not 34k
		// Note: 1.5 * 40k = 60k, which is > 50k, so min constraint applies
		assert.Equal(t, config.MinBitrate, estimate,
			"should not go below MinBitrate")
	})

	t.Run("MaxBitrate enforced on increase", func(t *testing.T) {
		config := DefaultRateControllerConfig()
		config.MaxBitrate = 1_000_000   // 1 Mbps maximum
		config.InitialBitrate = 950_000 // Start very near max
		rc := NewRateController(config)

		baseTime := time.Now()
		incomingRate := int64(5_000_000) // 5 Mbps incoming (high to avoid ratio constraint)

		// Go to Increase state
		rc.Update(BwNormal, incomingRate, baseTime)

		// Update with time elapsed - 1.08 * 950k = 1.026M > 1M max
		oneSecLater := baseTime.Add(time.Second)
		estimate := rc.Update(BwNormal, incomingRate, oneSecLater)

		// Should be clamped to MaxBitrate
		assert.Equal(t, config.MaxBitrate, estimate,
			"should not exceed MaxBitrate")
	})
}

func TestRateController_RatioConstraint(t *testing.T) {
	// Verify estimate <= 1.5 * incomingRate constraint
	config := DefaultRateControllerConfig()
	config.InitialBitrate = 10_000_000 // 10 Mbps - high estimate
	rc := NewRateController(config)

	// Low incoming rate
	incomingRate := int64(1_000_000) // 1 Mbps
	now := time.Now()

	// Even in Hold state, should be clamped to 1.5 * incomingRate
	estimate := rc.Update(BwUnderusing, incomingRate, now)

	// Should be clamped to 1.5 * 1M = 1.5M, not 10M
	maxAllowed := int64(1.5 * float64(incomingRate))
	assert.LessOrEqual(t, estimate, maxAllowed,
		"estimate should be clamped to 1.5 * incoming rate")
}

func TestRateController_NoDirectDecreaseToIncrease(t *testing.T) {
	// CRITICAL: From Decrease state, Normal signal should go to Hold, NOT Increase
	// This prevents oscillation
	config := DefaultRateControllerConfig()
	rc := NewRateController(config)

	now := time.Now()
	incomingRate := int64(1_000_000)

	// Get to Decrease state
	rc.Update(BwOverusing, incomingRate, now)
	assert.Equal(t, RateDecrease, rc.State(), "should be in Decrease")

	// Send Normal signal - should go to Hold, NOT Increase
	rc.Update(BwNormal, incomingRate, now.Add(100*time.Millisecond))
	assert.Equal(t, RateHold, rc.State(),
		"from Decrease, Normal should go to Hold, not Increase")
	assert.NotEqual(t, RateIncrease, rc.State(),
		"must NOT transition directly from Decrease to Increase")
}

func TestRateController_Reset(t *testing.T) {
	config := DefaultRateControllerConfig()
	config.InitialBitrate = 500_000
	rc := NewRateController(config)

	baseTime := time.Now()
	incomingRate := int64(2_000_000) // High to avoid ratio constraint

	// First update to set lastUpdate and go to Increase state
	rc.Update(BwNormal, incomingRate, baseTime)
	assert.Equal(t, RateIncrease, rc.State(), "should be in Increase")

	// Second update with time elapsed to actually change the rate
	oneSecLater := baseTime.Add(time.Second)
	rc.Update(BwNormal, incomingRate, oneSecLater)
	assert.NotEqual(t, config.InitialBitrate, rc.Estimate(), "rate should have changed after increase")

	// Reset
	rc.Reset()

	// Should be back to initial state
	assert.Equal(t, RateHold, rc.State(), "should be in Hold after reset")
	assert.Equal(t, config.InitialBitrate, rc.Estimate(), "should have initial bitrate after reset")
}

func TestRateControlState_String(t *testing.T) {
	tests := []struct {
		state    RateControlState
		expected string
	}{
		{RateHold, "Hold"},
		{RateIncrease, "Increase"},
		{RateDecrease, "Decrease"},
		{RateControlState(99), "Unknown"},
	}

	for _, tt := range tests {
		t.Run(tt.expected, func(t *testing.T) {
			assert.Equal(t, tt.expected, tt.state.String())
		})
	}
}

func TestDefaultRateControllerConfig(t *testing.T) {
	config := DefaultRateControllerConfig()

	assert.Equal(t, int64(10_000), config.MinBitrate, "default min should be 10 kbps")
	assert.Equal(t, int64(30_000_000), config.MaxBitrate, "default max should be 30 Mbps")
	assert.Equal(t, int64(300_000), config.InitialBitrate, "default initial should be 300 kbps")
	assert.Equal(t, 0.85, config.Beta, "default beta should be 0.85")
}

func TestNewRateController_AppliesDefaults(t *testing.T) {
	// Zero config should use defaults
	rc := NewRateController(RateControllerConfig{})

	assert.Equal(t, int64(300_000), rc.Estimate(), "should use default initial bitrate")
}

func TestRateController_IncreaseCapsElapsed(t *testing.T) {
	// Verify elapsed time is capped at 1 second to prevent huge jumps
	config := DefaultRateControllerConfig()
	config.InitialBitrate = 1_000_000
	rc := NewRateController(config)

	baseTime := time.Now()
	incomingRate := int64(50_000_000) // High enough to not hit ratio constraint

	// Go to Increase state
	rc.Update(BwNormal, incomingRate, baseTime)

	// Update 10 seconds later - should only increase by 1.08^1, not 1.08^10
	tenSecsLater := baseTime.Add(10 * time.Second)
	estimate := rc.Update(BwNormal, incomingRate, tenSecsLater)

	// Should be capped at 1.08x, not 1.08^10 = 2.16x
	maxExpected := int64(1.15 * float64(config.InitialBitrate)) // Allow some margin
	assert.Less(t, estimate, int64(2.0*float64(config.InitialBitrate)),
		"increase should be capped, not 1.08^10")
	assert.Less(t, estimate, maxExpected,
		"increase should be ~1.08x max")
}

func TestRateController_ContinuousOveruse(t *testing.T) {
	// Verify behavior during sustained overuse
	config := DefaultRateControllerConfig()
	config.Beta = 0.85
	rc := NewRateController(config)

	baseTime := time.Now()
	incomingRate := int64(1_000_000)

	// First overuse
	estimate1 := rc.Update(BwOverusing, incomingRate, baseTime)
	assert.Equal(t, int64(850_000), estimate1, "first decrease")

	// Continued overuse with same incoming rate - stay in Decrease
	estimate2 := rc.Update(BwOverusing, incomingRate, baseTime.Add(100*time.Millisecond))
	assert.Equal(t, RateDecrease, rc.State(), "should stay in Decrease")

	// Each overuse applies beta * incomingRate
	assert.Equal(t, int64(850_000), estimate2,
		"continued overuse applies beta to incoming rate")
}

func TestRateController_RecoverySequence(t *testing.T) {
	// Test typical recovery: Decrease -> Hold -> Increase
	config := DefaultRateControllerConfig()
	rc := NewRateController(config)

	baseTime := time.Now()
	incomingRate := int64(1_000_000)

	// 1. Overuse detected -> Decrease
	rc.Update(BwOverusing, incomingRate, baseTime)
	assert.Equal(t, RateDecrease, rc.State(), "step 1: should be Decrease")

	// 2. Congestion clears, Normal -> Hold (not Increase!)
	rc.Update(BwNormal, incomingRate, baseTime.Add(100*time.Millisecond))
	assert.Equal(t, RateHold, rc.State(), "step 2: should be Hold")

	// 3. Still Normal -> Increase
	rc.Update(BwNormal, incomingRate, baseTime.Add(200*time.Millisecond))
	assert.Equal(t, RateIncrease, rc.State(), "step 3: should be Increase")
}
