package bwe

import (
	"testing"
	"time"

	"multicodecsimulcast/pkg/bwe/internal"
)

func TestOveruseDetector_InitialState(t *testing.T) {
	clock := internal.NewMockClock(time.Time{})
	config := DefaultOveruseConfig()
	detector := NewOveruseDetector(config, clock)

	// New detector should start in BwNormal state
	if got := detector.State(); got != BwNormal {
		t.Errorf("initial state = %v, want %v", got, BwNormal)
	}

	// Threshold should equal InitialThreshold
	if got := detector.Threshold(); got != config.InitialThreshold {
		t.Errorf("initial threshold = %v, want %v", got, config.InitialThreshold)
	}
}

func TestOveruseDetector_NilClock(t *testing.T) {
	config := DefaultOveruseConfig()
	// Should not panic with nil clock
	detector := NewOveruseDetector(config, nil)
	if detector == nil {
		t.Error("NewOveruseDetector with nil clock returned nil")
	}
}

func TestOveruseDetector_NormalOperation(t *testing.T) {
	clock := internal.NewMockClock(time.Time{})
	config := DefaultOveruseConfig()
	detector := NewOveruseDetector(config, clock)

	// Feed estimates within threshold: 5, -3, 7, -5, 10 ms
	// All should remain in BwNormal state
	estimates := []float64{5, -3, 7, -5, 10}
	for i, est := range estimates {
		clock.Advance(20 * time.Millisecond)
		state := detector.Detect(est)
		if state != BwNormal {
			t.Errorf("estimate[%d]=%v: state = %v, want %v", i, est, state, BwNormal)
		}
	}
}

func TestOveruseDetector_SustainedOveruse(t *testing.T) {
	clock := internal.NewMockClock(time.Time{})
	config := DefaultOveruseConfig()
	detector := NewOveruseDetector(config, clock)

	var callbackCalled bool
	var oldState, newState BandwidthUsage
	detector.SetCallback(func(old, new BandwidthUsage) {
		callbackCalled = true
		oldState = old
		newState = new
	})

	// First call to initialize lastUpdateTime
	detector.Detect(0)

	// Feed increasing estimates exceeding threshold: 15, 16, 17, 18 ms
	// Advance clock 5ms each to exceed 10ms OveruseTimeThresh
	estimates := []float64{15, 16, 17, 18}
	for i, est := range estimates {
		clock.Advance(5 * time.Millisecond)
		state := detector.Detect(est)

		// Overuse should only be signaled after sustained period (>10ms)
		// and with more than 1 consecutive detection
		if i >= 2 { // After 15ms (3 x 5ms intervals)
			if state != BwOverusing {
				t.Errorf("estimate[%d]=%v after %dms: state = %v, want %v",
					i, est, (i+1)*5, state, BwOverusing)
			}
		}
	}

	// Verify callback was invoked with correct states
	if !callbackCalled {
		t.Error("callback was not called on state change to BwOverusing")
	}
	if oldState != BwNormal {
		t.Errorf("callback oldState = %v, want %v", oldState, BwNormal)
	}
	if newState != BwOverusing {
		t.Errorf("callback newState = %v, want %v", newState, BwOverusing)
	}
}

func TestOveruseDetector_SignalSuppression(t *testing.T) {
	clock := internal.NewMockClock(time.Time{})
	config := DefaultOveruseConfig()
	detector := NewOveruseDetector(config, clock)

	// Initialize
	detector.Detect(0)
	clock.Advance(5 * time.Millisecond)

	// Feed increasing estimates: 15, 16, 17 ms (above threshold)
	detector.Detect(15)
	clock.Advance(5 * time.Millisecond)
	detector.Detect(16)
	clock.Advance(5 * time.Millisecond)
	detector.Detect(17)
	clock.Advance(5 * time.Millisecond)

	// Now feed decreasing estimates (still above threshold but decreasing)
	// Signal suppression should prevent overuse
	state := detector.Detect(16) // Decreasing from 17 to 16
	if state != BwNormal {
		t.Errorf("decreasing estimate 16 (from 17): state = %v, want %v (suppression)", state, BwNormal)
	}

	clock.Advance(5 * time.Millisecond)
	state = detector.Detect(15) // Still decreasing
	if state != BwNormal {
		t.Errorf("decreasing estimate 15: state = %v, want %v (suppression)", state, BwNormal)
	}
}

func TestOveruseDetector_Underuse(t *testing.T) {
	clock := internal.NewMockClock(time.Time{})
	config := DefaultOveruseConfig()
	detector := NewOveruseDetector(config, clock)

	var callbackCalled bool
	var oldState, newState BandwidthUsage
	detector.SetCallback(func(old, new BandwidthUsage) {
		callbackCalled = true
		oldState = old
		newState = new
	})

	// Initialize
	detector.Detect(0)
	clock.Advance(20 * time.Millisecond)

	// Feed strongly negative estimates: -15, -16, -17 ms
	// Should trigger BwUnderusing immediately (no sustained requirement)
	estimates := []float64{-15, -16, -17}
	for i, est := range estimates {
		clock.Advance(5 * time.Millisecond)
		state := detector.Detect(est)
		if state != BwUnderusing {
			t.Errorf("estimate[%d]=%v: state = %v, want %v", i, est, state, BwUnderusing)
		}
	}

	// Verify callback
	if !callbackCalled {
		t.Error("callback was not called on state change to BwUnderusing")
	}
	if oldState != BwNormal {
		t.Errorf("callback oldState = %v, want %v", oldState, BwNormal)
	}
	if newState != BwUnderusing {
		t.Errorf("callback newState = %v, want %v", newState, BwUnderusing)
	}
}

func TestOveruseDetector_AdaptiveThresholdIncrease(t *testing.T) {
	clock := internal.NewMockClock(time.Time{})
	config := DefaultOveruseConfig()
	detector := NewOveruseDetector(config, clock)

	initialThreshold := detector.Threshold()

	// Initialize
	detector.Detect(0)
	clock.Advance(100 * time.Millisecond)

	// Feed estimates exceeding threshold repeatedly
	// Threshold should increase over time using Ku coefficient
	for i := 0; i < 50; i++ {
		clock.Advance(100 * time.Millisecond)
		detector.Detect(30) // Well above initial threshold of 12.5
	}

	// Threshold should have increased
	if detector.Threshold() <= initialThreshold {
		t.Errorf("threshold after overuse = %v, should be > %v", detector.Threshold(), initialThreshold)
	}
}

func TestOveruseDetector_AdaptiveThresholdDecrease(t *testing.T) {
	clock := internal.NewMockClock(time.Time{})
	config := DefaultOveruseConfig()
	detector := NewOveruseDetector(config, clock)

	// First, elevate the threshold
	detector.Detect(0)
	for i := 0; i < 100; i++ {
		clock.Advance(100 * time.Millisecond)
		detector.Detect(100) // High estimates to increase threshold
	}

	elevatedThreshold := detector.Threshold()
	if elevatedThreshold <= config.InitialThreshold {
		t.Fatalf("threshold not elevated: got %v, want > %v", elevatedThreshold, config.InitialThreshold)
	}

	// Now feed low estimates - threshold should decrease (slowly due to Kd < Ku)
	for i := 0; i < 100; i++ {
		clock.Advance(100 * time.Millisecond)
		detector.Detect(5) // Well below elevated threshold
	}

	// Threshold should have decreased
	if detector.Threshold() >= elevatedThreshold {
		t.Errorf("threshold after low estimates = %v, should be < %v", detector.Threshold(), elevatedThreshold)
	}
}

func TestOveruseDetector_ThresholdClamping(t *testing.T) {
	clock := internal.NewMockClock(time.Time{})
	config := DefaultOveruseConfig()
	detector := NewOveruseDetector(config, clock)

	// Test max clamping: feed very high estimates
	detector.Detect(0)
	for i := 0; i < 1000; i++ {
		clock.Advance(100 * time.Millisecond)
		detector.Detect(1000) // Very high
	}

	if detector.Threshold() > config.MaxThreshold {
		t.Errorf("threshold = %v, should not exceed MaxThreshold %v", detector.Threshold(), config.MaxThreshold)
	}

	// Reset and test min clamping
	detector.Reset()
	detector.Detect(0)

	// Keep feeding estimates just above threshold, then drop to 0
	// to allow threshold to decrease
	for i := 0; i < 10000; i++ {
		clock.Advance(100 * time.Millisecond)
		detector.Detect(0) // Very low - will cause threshold to decrease toward it
	}

	if detector.Threshold() < config.MinThreshold {
		t.Errorf("threshold = %v, should not go below MinThreshold %v", detector.Threshold(), config.MinThreshold)
	}
}

func TestOveruseDetector_StateTransitionToNormal(t *testing.T) {
	clock := internal.NewMockClock(time.Time{})
	config := DefaultOveruseConfig()
	detector := NewOveruseDetector(config, clock)

	var callbackCount int
	detector.SetCallback(func(old, new BandwidthUsage) {
		callbackCount++
	})

	// Enter BwOverusing state
	detector.Detect(0)
	for i := 0; i < 10; i++ {
		clock.Advance(5 * time.Millisecond)
		detector.Detect(20 + float64(i)) // Increasing to avoid suppression
	}

	if detector.State() != BwOverusing {
		t.Fatalf("failed to enter BwOverusing state: got %v", detector.State())
	}

	initialCallbackCount := callbackCount

	// Feed estimates within threshold to return to normal
	estimates := []float64{5, 3, -2}
	for _, est := range estimates {
		clock.Advance(20 * time.Millisecond)
		detector.Detect(est)
	}

	if detector.State() != BwNormal {
		t.Errorf("state after normal estimates = %v, want %v", detector.State(), BwNormal)
	}

	// Callback should have been invoked for transition back to normal
	if callbackCount <= initialCallbackCount {
		t.Error("callback was not called on transition from BwOverusing to BwNormal")
	}
}

func TestOveruseDetector_CallbackNil(t *testing.T) {
	clock := internal.NewMockClock(time.Time{})
	config := DefaultOveruseConfig()
	detector := NewOveruseDetector(config, clock)

	// Don't set callback, ensure no panic on state change
	detector.Detect(0)
	clock.Advance(20 * time.Millisecond)

	// Trigger underuse (immediate state change)
	state := detector.Detect(-20)
	if state != BwUnderusing {
		t.Errorf("state = %v, want %v", state, BwUnderusing)
	}

	// Now set callback and then set to nil
	callbackCalled := false
	detector.SetCallback(func(old, new BandwidthUsage) {
		callbackCalled = true
	})
	detector.SetCallback(nil)

	clock.Advance(20 * time.Millisecond)
	detector.Detect(0) // Back to normal

	if callbackCalled {
		t.Error("callback should not be called after setting to nil")
	}
}

func TestOveruseDetector_CallbackCorrectStates(t *testing.T) {
	clock := internal.NewMockClock(time.Time{})
	config := DefaultOveruseConfig()
	detector := NewOveruseDetector(config, clock)

	type transition struct {
		old, new BandwidthUsage
	}
	var transitions []transition

	detector.SetCallback(func(old, new BandwidthUsage) {
		transitions = append(transitions, transition{old, new})
	})

	// Normal -> Underusing
	detector.Detect(0)
	clock.Advance(20 * time.Millisecond)
	detector.Detect(-20)

	// Underusing -> Normal
	clock.Advance(20 * time.Millisecond)
	detector.Detect(0)

	// Normal -> Overusing (need sustained)
	for i := 0; i < 10; i++ {
		clock.Advance(5 * time.Millisecond)
		detector.Detect(20 + float64(i))
	}

	// Overusing -> Normal
	clock.Advance(20 * time.Millisecond)
	detector.Detect(0)

	// Verify transitions
	expected := []transition{
		{BwNormal, BwUnderusing},
		{BwUnderusing, BwNormal},
		{BwNormal, BwOverusing},
		{BwOverusing, BwNormal},
	}

	if len(transitions) != len(expected) {
		t.Errorf("got %d transitions, want %d", len(transitions), len(expected))
	}

	for i, tr := range transitions {
		if i >= len(expected) {
			break
		}
		if tr.old != expected[i].old || tr.new != expected[i].new {
			t.Errorf("transition[%d] = %v->%v, want %v->%v",
				i, tr.old, tr.new, expected[i].old, expected[i].new)
		}
	}
}

func TestOveruseDetector_Reset(t *testing.T) {
	clock := internal.NewMockClock(time.Time{})
	config := DefaultOveruseConfig()
	detector := NewOveruseDetector(config, clock)

	// Modify state
	detector.Detect(0)
	for i := 0; i < 50; i++ {
		clock.Advance(5 * time.Millisecond)
		detector.Detect(30 + float64(i)) // Enter overuse and elevate threshold
	}

	if detector.State() == BwNormal && detector.Threshold() == config.InitialThreshold {
		t.Fatal("state was not modified from initial")
	}

	// Reset
	detector.Reset()

	// Verify reset
	if detector.State() != BwNormal {
		t.Errorf("state after reset = %v, want %v", detector.State(), BwNormal)
	}
	if detector.Threshold() != config.InitialThreshold {
		t.Errorf("threshold after reset = %v, want %v", detector.Threshold(), config.InitialThreshold)
	}
}

func TestOveruseDetector_CustomConfig(t *testing.T) {
	clock := internal.NewMockClock(time.Time{})
	config := OveruseConfig{
		InitialThreshold:  20.0,
		MinThreshold:      10.0,
		MaxThreshold:      100.0,
		Ku:                0.1,  // Faster increase
		Kd:                0.01, // Faster decrease
		OveruseTimeThresh: 5 * time.Millisecond,
	}
	detector := NewOveruseDetector(config, clock)

	// Verify initial threshold
	if detector.Threshold() != config.InitialThreshold {
		t.Errorf("threshold = %v, want %v", detector.Threshold(), config.InitialThreshold)
	}

	// With faster Ku, threshold should increase faster
	// Formula: threshold += deltaT * k * (|m| - threshold)
	// Each iteration: threshold += 0.1 * 0.1 * (50 - threshold) = 0.01 * (50 - threshold)
	// This is exponential approach to 50, but limited by the 10% coefficient
	detector.Detect(0)
	for i := 0; i < 10; i++ {
		clock.Advance(100 * time.Millisecond)
		detector.Detect(50)
	}

	// Threshold should have increased from initial 20.0
	// After 10 iterations with Ku=0.1 and deltaT=0.1s, the threshold approaches 50 slowly
	// The formula results in ~22.9 after 10 iterations (converging to 50)
	if detector.Threshold() <= config.InitialThreshold {
		t.Errorf("threshold = %v, expected higher than initial %v with Ku=%v",
			detector.Threshold(), config.InitialThreshold, config.Ku)
	}
}

func TestOveruseDetector_EdgeCases(t *testing.T) {
	clock := internal.NewMockClock(time.Time{})
	config := DefaultOveruseConfig()
	detector := NewOveruseDetector(config, clock)

	// Test with exactly threshold value
	threshold := detector.Threshold()
	detector.Detect(0)
	clock.Advance(20 * time.Millisecond)

	state := detector.Detect(threshold) // Exactly at threshold
	if state != BwNormal {
		t.Errorf("estimate at threshold: state = %v, want %v", state, BwNormal)
	}

	// Test with exactly negative threshold
	clock.Advance(20 * time.Millisecond)
	state = detector.Detect(-threshold) // Exactly at negative threshold
	if state != BwNormal {
		t.Errorf("estimate at -threshold: state = %v, want %v", state, BwNormal)
	}

	// Test with zero estimate
	clock.Advance(20 * time.Millisecond)
	state = detector.Detect(0)
	if state != BwNormal {
		t.Errorf("zero estimate: state = %v, want %v", state, BwNormal)
	}
}

func TestOveruseDetector_OveruseRequiresSustainedPeriod(t *testing.T) {
	clock := internal.NewMockClock(time.Time{})
	config := DefaultOveruseConfig()
	detector := NewOveruseDetector(config, clock)

	// Initialize
	detector.Detect(0)

	// Single high estimate should not trigger overuse (not sustained)
	clock.Advance(1 * time.Millisecond)
	state := detector.Detect(20)
	if state == BwOverusing {
		t.Error("single estimate should not trigger overuse")
	}

	// Reset and try with short time period
	detector.Reset()
	detector.Detect(0)

	// Two estimates within 10ms should not trigger overuse
	clock.Advance(3 * time.Millisecond)
	detector.Detect(20)
	clock.Advance(3 * time.Millisecond) // Total: 6ms < 10ms
	state = detector.Detect(21)
	if state == BwOverusing {
		t.Error("estimates within OveruseTimeThresh should not trigger overuse")
	}
}

func TestOveruseDetector_OveruseCounterRequired(t *testing.T) {
	clock := internal.NewMockClock(time.Time{})
	config := DefaultOveruseConfig()
	detector := NewOveruseDetector(config, clock)

	// Initialize
	detector.Detect(0)

	// Even with enough time, need > 1 consecutive detection
	clock.Advance(15 * time.Millisecond) // > 10ms but only 1 detection
	state := detector.Detect(20)
	if state == BwOverusing {
		t.Error("single detection should not trigger overuse even with sufficient time")
	}

	// Second consecutive detection should trigger
	clock.Advance(1 * time.Millisecond)
	state = detector.Detect(21)
	if state != BwOverusing {
		t.Errorf("second consecutive detection: state = %v, want %v", state, BwOverusing)
	}
}
