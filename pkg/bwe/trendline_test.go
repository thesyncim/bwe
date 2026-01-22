package bwe

import (
	"math"
	"testing"
	"time"
)

func TestTrendlineEstimator_DefaultConfig(t *testing.T) {
	config := DefaultTrendlineConfig()

	if config.WindowSize != 20 {
		t.Errorf("WindowSize = %d, want 20", config.WindowSize)
	}
	if config.SmoothingCoef != 0.9 {
		t.Errorf("SmoothingCoef = %f, want 0.9", config.SmoothingCoef)
	}
	if config.ThresholdGain != 4.0 {
		t.Errorf("ThresholdGain = %f, want 4.0", config.ThresholdGain)
	}
}

func TestTrendlineEstimator_InvalidWindowSize(t *testing.T) {
	// Window size < 2 should default to 20
	config := TrendlineConfig{
		WindowSize:    1,
		SmoothingCoef: 0.9,
		ThresholdGain: 4.0,
	}
	estimator := NewTrendlineEstimator(config)

	// Should have been corrected to default
	if estimator.config.WindowSize != 20 {
		t.Errorf("WindowSize = %d, want 20 (should default for invalid)", estimator.config.WindowSize)
	}
}

func TestTrendlineEstimator_InitialState(t *testing.T) {
	estimator := NewTrendlineEstimator(DefaultTrendlineConfig())

	// First few updates with 0ms delay should return ~0 (no trend)
	baseTime := time.Now()

	// First sample
	result := estimator.Update(baseTime, 0)
	if result != 0 {
		t.Errorf("First sample result = %f, want 0 (not enough samples for regression)", result)
	}

	// Second sample, still small time difference
	result = estimator.Update(baseTime.Add(20*time.Millisecond), 0)
	// With only 2 samples of 0 delay, slope should be 0
	if math.Abs(result) > 0.0001 {
		t.Errorf("Second sample result = %f, want ~0", result)
	}
}

func TestTrendlineEstimator_PositiveTrend(t *testing.T) {
	estimator := NewTrendlineEstimator(DefaultTrendlineConfig())

	baseTime := time.Now()

	// Feed increasing delays: 0, 1, 2, 3, 4, 5... ms over time
	// This simulates a queue building up
	var lastResult float64
	for i := 0; i < 25; i++ {
		arrivalTime := baseTime.Add(time.Duration(i*20) * time.Millisecond)
		delayVariation := float64(i) // 0, 1, 2, 3, ...
		lastResult = estimator.Update(arrivalTime, delayVariation)
	}

	// Output should be positive (indicating increasing delays / congestion)
	if lastResult <= 0 {
		t.Errorf("Positive trend result = %f, want > 0", lastResult)
	}
}

func TestTrendlineEstimator_NegativeTrend(t *testing.T) {
	estimator := NewTrendlineEstimator(DefaultTrendlineConfig())

	baseTime := time.Now()

	// Feed decreasing delays: 10, 9, 8, 7, 6... ms
	// This simulates a queue draining
	var lastResult float64
	for i := 0; i < 25; i++ {
		arrivalTime := baseTime.Add(time.Duration(i*20) * time.Millisecond)
		delayVariation := float64(10 - i) // 10, 9, 8, 7, ...
		lastResult = estimator.Update(arrivalTime, delayVariation)
	}

	// Output should be negative (indicating decreasing delays / queue draining)
	if lastResult >= 0 {
		t.Errorf("Negative trend result = %f, want < 0", lastResult)
	}
}

func TestTrendlineEstimator_StableNetwork(t *testing.T) {
	estimator := NewTrendlineEstimator(DefaultTrendlineConfig())

	baseTime := time.Now()

	// Feed constant 0ms variations - stable network
	var lastResult float64
	for i := 0; i < 25; i++ {
		arrivalTime := baseTime.Add(time.Duration(i*20) * time.Millisecond)
		lastResult = estimator.Update(arrivalTime, 0)
	}

	// Output should stay near 0
	if math.Abs(lastResult) > 0.0001 {
		t.Errorf("Stable network result = %f, want ~0", lastResult)
	}
}

func TestTrendlineEstimator_WindowSliding(t *testing.T) {
	config := TrendlineConfig{
		WindowSize:    5, // Small window for testing
		SmoothingCoef: 0.9,
		ThresholdGain: 4.0,
	}
	estimator := NewTrendlineEstimator(config)

	baseTime := time.Now()

	// Fill window with positive trend samples (increasing delays)
	for i := 0; i < 5; i++ {
		arrivalTime := baseTime.Add(time.Duration(i*20) * time.Millisecond)
		estimator.Update(arrivalTime, float64(i))
	}

	// Get result with positive trend established
	arrivalTime := baseTime.Add(time.Duration(5*20) * time.Millisecond)
	positiveResult := estimator.Update(arrivalTime, 5.0)

	// Now add samples with negative trend (decreasing delays)
	// Old positive samples should slide out
	for i := 6; i < 15; i++ {
		arrivalTime := baseTime.Add(time.Duration(i*20) * time.Millisecond)
		estimator.Update(arrivalTime, float64(20-i)) // 14, 13, 12, 11, ...
	}

	arrivalTime = baseTime.Add(time.Duration(15*20) * time.Millisecond)
	negativeResult := estimator.Update(arrivalTime, 5.0)

	// Result should have changed from positive to negative
	if positiveResult <= 0 {
		t.Errorf("Initial positive result = %f, want > 0", positiveResult)
	}
	if negativeResult >= positiveResult {
		t.Errorf("After window slide result = %f, should be less than initial %f", negativeResult, positiveResult)
	}
}

func TestTrendlineEstimator_SmoothingEffect(t *testing.T) {
	// High smoothing coefficient should filter out noise
	config := TrendlineConfig{
		WindowSize:    20,
		SmoothingCoef: 0.9,
		ThresholdGain: 4.0,
	}
	estimator := NewTrendlineEstimator(config)

	baseTime := time.Now()

	// Feed noisy data with underlying positive trend
	// Pattern: 1, -0.5, 2, -0.3, 3, -0.2, 4... (positive trend with negative noise)
	noisyValues := []float64{1, -0.5, 2, -0.3, 3, -0.2, 4, -0.1, 5, 0, 6, 0.1, 7, 0.2, 8, 0.3, 9, 0.4, 10, 0.5}

	var lastResult float64
	for i, v := range noisyValues {
		arrivalTime := baseTime.Add(time.Duration(i*20) * time.Millisecond)
		lastResult = estimator.Update(arrivalTime, v)
	}

	// Despite noise, smoothed output should track the positive trend
	if lastResult <= 0 {
		t.Errorf("Noisy positive trend result = %f, want > 0 (smoothing should filter noise)", lastResult)
	}
}

func TestTrendlineEstimator_ThresholdGain(t *testing.T) {
	baseTime := time.Now()

	// Create two estimators with different threshold gains
	config1 := TrendlineConfig{
		WindowSize:    10,
		SmoothingCoef: 0.9,
		ThresholdGain: 2.0,
	}
	config2 := TrendlineConfig{
		WindowSize:    10,
		SmoothingCoef: 0.9,
		ThresholdGain: 4.0,
	}

	estimator1 := NewTrendlineEstimator(config1)
	estimator2 := NewTrendlineEstimator(config2)

	// Feed same input to both
	var result1, result2 float64
	for i := 0; i < 15; i++ {
		arrivalTime := baseTime.Add(time.Duration(i*20) * time.Millisecond)
		delayVariation := float64(i)
		result1 = estimator1.Update(arrivalTime, delayVariation)
		result2 = estimator2.Update(arrivalTime, delayVariation)
	}

	// Same input should produce proportionally different outputs
	// result2 should be ~2x result1 (gain 4.0 vs 2.0)
	ratio := result2 / result1
	expectedRatio := 2.0
	if math.Abs(ratio-expectedRatio) > 0.01 {
		t.Errorf("Gain ratio = %f, want %f (result1=%f, result2=%f)", ratio, expectedRatio, result1, result2)
	}
}

func TestTrendlineEstimator_Reset(t *testing.T) {
	estimator := NewTrendlineEstimator(DefaultTrendlineConfig())

	baseTime := time.Now()

	// Fill estimator with samples
	for i := 0; i < 20; i++ {
		arrivalTime := baseTime.Add(time.Duration(i*20) * time.Millisecond)
		estimator.Update(arrivalTime, float64(i))
	}

	// Verify it has state
	if estimator.numDeltas == 0 {
		t.Error("Expected numDeltas > 0 before reset")
	}
	if len(estimator.history) == 0 {
		t.Error("Expected history > 0 before reset")
	}

	// Reset
	estimator.Reset()

	// Verify state is cleared
	if estimator.numDeltas != 0 {
		t.Errorf("numDeltas = %d after reset, want 0", estimator.numDeltas)
	}
	if len(estimator.history) != 0 {
		t.Errorf("history len = %d after reset, want 0", len(estimator.history))
	}
	if estimator.smoothedDelay != 0 {
		t.Errorf("smoothedDelay = %f after reset, want 0", estimator.smoothedDelay)
	}
	if !estimator.firstArrival.IsZero() {
		t.Error("firstArrival should be zero time after reset")
	}

	// Next sample should start fresh
	newBaseTime := time.Now()
	result := estimator.Update(newBaseTime, 0)

	// First sample should return 0 (not enough samples for regression)
	if result != 0 {
		t.Errorf("First sample after reset = %f, want 0", result)
	}
}

func TestTrendlineEstimator_ArrivalTimeSpacing(t *testing.T) {
	config := TrendlineConfig{
		WindowSize:    10,
		SmoothingCoef: 0.5, // Lower smoothing for clearer signal
		ThresholdGain: 1.0, // Unity gain for clearer math
	}
	estimator := NewTrendlineEstimator(config)

	baseTime := time.Now()

	// Feed samples with known time spacing
	// Sample at t=0, delay=0
	estimator.Update(baseTime, 0)

	// Sample at t=100ms, delay=10 (should give slope ~0.1 after smoothing)
	estimator.Update(baseTime.Add(100*time.Millisecond), 10)

	// Sample at t=200ms, delay=20
	estimator.Update(baseTime.Add(200*time.Millisecond), 20)

	// Sample at t=300ms, delay=30
	result := estimator.Update(baseTime.Add(300*time.Millisecond), 30)

	// With consistent 10ms/100ms = 0.1 slope rate, should be positive
	if result <= 0 {
		t.Errorf("Time-spaced positive trend result = %f, want > 0", result)
	}
}

func TestTrendlineEstimator_NumDeltasCap(t *testing.T) {
	estimator := NewTrendlineEstimator(DefaultTrendlineConfig())

	baseTime := time.Now()

	// Feed more than 60 samples
	var result60, result100 float64
	for i := 0; i < 100; i++ {
		arrivalTime := baseTime.Add(time.Duration(i*20) * time.Millisecond)
		result := estimator.Update(arrivalTime, 1.0) // Constant positive delay
		if i == 59 {
			result60 = result
		}
		if i == 99 {
			result100 = result
		}
	}

	// numDeltas multiplier is capped at 60, so results should be similar
	// (they won't be exactly equal due to window sliding, but the multiplier cap should limit growth)
	if estimator.numDeltas != 100 {
		t.Errorf("numDeltas = %d, want 100", estimator.numDeltas)
	}

	// The multiplier cap means result at 100 shouldn't be much larger than at 60
	// (window has fully rolled over by then with same input, slope is similar)
	ratio := result100 / result60
	if ratio > 1.5 { // Allow some variance due to window effects
		t.Errorf("Result at 100 samples is %f times result at 60, expected capped growth (ratio=%f)", ratio, ratio)
	}
}

func TestTrendlineEstimator_LinearFitSlope_EdgeCases(t *testing.T) {
	estimator := NewTrendlineEstimator(DefaultTrendlineConfig())

	// With empty history, slope should be 0
	slope := estimator.linearFitSlope()
	if slope != 0 {
		t.Errorf("Empty history slope = %f, want 0", slope)
	}

	// Add one sample
	estimator.history = append(estimator.history, sample{arrivalTimeMs: 0, smoothedDelay: 0})
	slope = estimator.linearFitSlope()
	if slope != 0 {
		t.Errorf("Single sample slope = %f, want 0", slope)
	}

	// Add identical x values (should return 0 due to zero denominator)
	estimator.history = []sample{
		{arrivalTimeMs: 100, smoothedDelay: 0},
		{arrivalTimeMs: 100, smoothedDelay: 10}, // Same x, different y
	}
	slope = estimator.linearFitSlope()
	if slope != 0 {
		t.Errorf("Identical x values slope = %f, want 0 (degenerate case)", slope)
	}
}
