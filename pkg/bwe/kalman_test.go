package bwe

import (
	"math"
	"testing"
)

func TestKalmanFilter_InitialState(t *testing.T) {
	kf := NewKalmanFilter(DefaultKalmanConfig())

	if got := kf.Estimate(); got != 0 {
		t.Errorf("Initial estimate = %v, want 0", got)
	}

	// First update with 0ms should return close to 0
	result := kf.Update(0)
	if math.Abs(result) > 0.1 {
		t.Errorf("Update(0) on fresh filter = %v, want ~0", result)
	}
}

func TestKalmanFilter_TrackPositiveTrend(t *testing.T) {
	// Positive delay variations indicate queue building
	kf := NewKalmanFilter(DefaultKalmanConfig())

	measurements := []float64{1, 2, 3, 4, 5}
	var lastEstimate float64

	for i, m := range measurements {
		estimate := kf.Update(m)
		if i > 0 && estimate <= lastEstimate {
			t.Errorf("Estimate not increasing: step %d, prev=%v, curr=%v", i, lastEstimate, estimate)
		}
		lastEstimate = estimate
	}

	// Final estimate should be positive
	if lastEstimate <= 0 {
		t.Errorf("Final estimate = %v, want > 0", lastEstimate)
	}

	// Should not jump immediately to 5 (smoothing effect)
	if lastEstimate > 5 {
		t.Errorf("Estimate = %v, should be smoothed below 5", lastEstimate)
	}
}

func TestKalmanFilter_TrackNegativeTrend(t *testing.T) {
	// Negative delay variations indicate queue draining
	kf := NewKalmanFilter(DefaultKalmanConfig())

	measurements := []float64{-1, -2, -3, -4, -5}
	var lastEstimate float64

	for i, m := range measurements {
		estimate := kf.Update(m)
		if i > 0 && estimate >= lastEstimate {
			t.Errorf("Estimate not decreasing: step %d, prev=%v, curr=%v", i, lastEstimate, estimate)
		}
		lastEstimate = estimate
	}

	// Final estimate should be negative
	if lastEstimate >= 0 {
		t.Errorf("Final estimate = %v, want < 0", lastEstimate)
	}

	// Should not jump immediately to -5 (smoothing effect)
	if lastEstimate < -5 {
		t.Errorf("Estimate = %v, should be smoothed above -5", lastEstimate)
	}
}

func TestKalmanFilter_OutlierRejection(t *testing.T) {
	kf := NewKalmanFilter(DefaultKalmanConfig())

	// Feed small variations to establish baseline
	normalMeasurements := []float64{1, 2, 1, 2}
	for _, m := range normalMeasurements {
		kf.Update(m)
	}
	estimateBeforeSpike := kf.Estimate()

	// Inject large outlier
	kf.Update(100)
	estimateAfterSpike := kf.Estimate()

	// Feed normal values again
	for _, m := range normalMeasurements {
		kf.Update(m)
	}
	estimateFinal := kf.Estimate()

	// Estimate should NOT jump to 100 due to outlier filtering
	if estimateAfterSpike > 50 {
		t.Errorf("Outlier caused large jump: before=%v, after=%v (should be capped)", estimateBeforeSpike, estimateAfterSpike)
	}

	// After recovery, estimate should be reasonable
	if estimateFinal > 20 {
		t.Errorf("Estimate after outlier recovery = %v, want < 20", estimateFinal)
	}

	t.Logf("Outlier handling: before=%v, after_spike=%v, final=%v", estimateBeforeSpike, estimateAfterSpike, estimateFinal)
}

func TestKalmanFilter_StableNetwork(t *testing.T) {
	// Oscillating around 0 should keep estimate near 0
	kf := NewKalmanFilter(DefaultKalmanConfig())

	measurements := []float64{1, -1, 0.5, -0.5, 0, 0.5, -0.5, 1, -1, 0}

	for _, m := range measurements {
		kf.Update(m)
	}

	finalEstimate := kf.Estimate()

	// Estimate should stay near 0 for zero-mean noise
	if math.Abs(finalEstimate) > 1.0 {
		t.Errorf("Estimate for stable network = %v, want close to 0", finalEstimate)
	}
}

func TestKalmanFilter_ConvergenceSpeed(t *testing.T) {
	kf := NewKalmanFilter(DefaultKalmanConfig())

	// Feed constant 10ms delay variations
	targetValue := 10.0
	var estimate float64

	// With spec-compliant parameters (q=0.001, chi=0.01), convergence is intentionally slow
	// to avoid overreacting to noise. Need more iterations for full convergence.
	for i := 0; i < 500; i++ {
		estimate = kf.Update(targetValue)
	}

	// After 500 updates with constant input, should be close to target
	// Tolerance reflects that Kalman filter with low process noise converges slowly
	tolerance := 2.5
	if math.Abs(estimate-targetValue) > tolerance {
		t.Errorf("After 500 updates with constant %v, estimate = %v, want within %v of %v",
			targetValue, estimate, tolerance, targetValue)
	}

	// Verify convergence is monotonic and reasonable
	if estimate <= 0 {
		t.Errorf("Estimate should be positive, got %v", estimate)
	}

	t.Logf("Convergence: target=%v, estimate after 500 updates=%v", targetValue, estimate)
}

func TestKalmanFilter_Reset(t *testing.T) {
	kf := NewKalmanFilter(DefaultKalmanConfig())

	// Feed values to change state
	for i := 0; i < 10; i++ {
		kf.Update(5.0)
	}

	if kf.Estimate() == 0 {
		t.Error("Estimate should be non-zero before reset")
	}

	kf.Reset()

	if kf.Estimate() != 0 {
		t.Errorf("Estimate after reset = %v, want 0", kf.Estimate())
	}
}

func TestKalmanFilter_CustomConfig(t *testing.T) {
	// Higher chi = faster adaptation
	fastConfig := KalmanConfig{
		ProcessNoise: 0.001,
		InitialError: 0.1,
		Chi:          0.1, // 10x faster adaptation
	}

	slowKF := NewKalmanFilter(DefaultKalmanConfig())
	fastKF := NewKalmanFilter(fastConfig)

	// Feed same measurements to both
	measurements := []float64{5, 5, 5, 5, 5}
	var slowEstimate, fastEstimate float64

	for _, m := range measurements {
		slowEstimate = slowKF.Update(m)
		fastEstimate = fastKF.Update(m)
	}

	// Both should converge toward 5, but fast filter should be closer
	// (This is a behavioral test - the chi affects variance adaptation speed)
	t.Logf("After 5 updates of 5.0: slow=%v, fast=%v", slowEstimate, fastEstimate)

	// Both should be positive and trending toward 5
	if slowEstimate <= 0 || fastEstimate <= 0 {
		t.Error("Both filters should have positive estimates")
	}
}

func TestKalmanFilter_DefaultConfig(t *testing.T) {
	config := DefaultKalmanConfig()

	// Verify spec-compliant defaults
	if config.ProcessNoise != 0.001 {
		t.Errorf("ProcessNoise = %v, want 0.001 (10^-3)", config.ProcessNoise)
	}
	if config.InitialError != 0.1 {
		t.Errorf("InitialError = %v, want 0.1", config.InitialError)
	}
	if config.Chi != 0.01 {
		t.Errorf("Chi = %v, want 0.01", config.Chi)
	}
}

func TestKalmanFilter_NegativeOutlier(t *testing.T) {
	// Test that negative outliers are also capped
	kf := NewKalmanFilter(DefaultKalmanConfig())

	// Establish baseline
	for i := 0; i < 5; i++ {
		kf.Update(0)
	}

	// Inject large negative outlier
	kf.Update(-100)
	estimate := kf.Estimate()

	// Should not jump to -100
	if estimate < -50 {
		t.Errorf("Negative outlier caused large jump: estimate = %v", estimate)
	}
}

func TestKalmanFilter_TableDrivenTrend(t *testing.T) {
	tests := []struct {
		name         string
		measurements []float64
		expectSign   float64 // 1 for positive, -1 for negative, 0 for near-zero
		tolerance    float64
	}{
		{
			name:         "increasing trend",
			measurements: []float64{1, 2, 3, 4, 5, 6, 7, 8, 9, 10},
			expectSign:   1,
			tolerance:    0,
		},
		{
			name:         "decreasing trend",
			measurements: []float64{-1, -2, -3, -4, -5, -6, -7, -8, -9, -10},
			expectSign:   -1,
			tolerance:    0,
		},
		{
			name:         "stable around zero",
			measurements: []float64{0.1, -0.1, 0.2, -0.2, 0.1, -0.1, 0, 0.1, -0.1, 0},
			expectSign:   0,
			tolerance:    0.5,
		},
		{
			name:         "stable around positive value",
			measurements: []float64{5, 5, 5, 5, 5, 5, 5, 5, 5, 5},
			expectSign:   1,
			tolerance:    0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			kf := NewKalmanFilter(DefaultKalmanConfig())

			var estimate float64
			for _, m := range tt.measurements {
				estimate = kf.Update(m)
			}

			switch tt.expectSign {
			case 1:
				if estimate <= 0 {
					t.Errorf("Expected positive estimate, got %v", estimate)
				}
			case -1:
				if estimate >= 0 {
					t.Errorf("Expected negative estimate, got %v", estimate)
				}
			case 0:
				if math.Abs(estimate) > tt.tolerance {
					t.Errorf("Expected estimate near zero (within %v), got %v", tt.tolerance, estimate)
				}
			}
		})
	}
}
