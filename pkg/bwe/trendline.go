// Package bwe implements Google Congestion Control (GCC) receiver-side
// bandwidth estimation for WebRTC.
package bwe

import "time"

// TrendlineConfig contains configuration parameters for the trendline estimator.
// The trendline estimator uses linear regression over a sliding window of samples
// to estimate the delay trend, providing an alternative to Kalman filtering.
type TrendlineConfig struct {
	// WindowSize is the number of samples in the regression window.
	// A larger window provides more stability but slower response.
	// Default: 20 samples.
	WindowSize int

	// SmoothingCoef is the exponential smoothing coefficient for accumulated delay.
	// Higher values (closer to 1.0) give more weight to history.
	// Default: 0.9
	SmoothingCoef float64

	// ThresholdGain is the multiplier for slope output.
	// Scales the output to match the overuse detector's expected input range.
	// Default: 4.0
	ThresholdGain float64
}

// DefaultTrendlineConfig returns the default configuration for the trendline estimator.
// These values are based on WebRTC reference implementation defaults.
func DefaultTrendlineConfig() TrendlineConfig {
	return TrendlineConfig{
		WindowSize:    20,
		SmoothingCoef: 0.9,
		ThresholdGain: 4.0,
	}
}

// sample represents a single delay sample in the trendline history.
type sample struct {
	arrivalTimeMs float64 // Arrival time in ms since estimator start
	smoothedDelay float64 // Accumulated smoothed delay at this point
}

// TrendlineEstimator estimates delay trends using linear regression over a
// sliding window of samples. This is the modern approach used in WebRTC,
// providing an alternative to Kalman filtering for the overuse detector.
//
// The estimator:
// 1. Applies exponential smoothing to incoming delay variations
// 2. Maintains a sliding window of (time, smoothed_delay) samples
// 3. Computes the linear regression slope over the window
// 4. Outputs a modified trend value scaled by sample count and threshold gain
type TrendlineEstimator struct {
	config        TrendlineConfig
	history       []sample  // Sliding window of samples
	smoothedDelay float64   // Running smoothed delay accumulator
	numDeltas     int       // Total number of samples seen
	firstArrival  time.Time // Reference time for arrivalTimeMs calculation
}

// NewTrendlineEstimator creates a new trendline estimator with the given configuration.
// If WindowSize is less than 2, it defaults to 20.
func NewTrendlineEstimator(config TrendlineConfig) *TrendlineEstimator {
	// Validate window size - need at least 2 samples for linear regression
	if config.WindowSize < 2 {
		config.WindowSize = 20
	}

	return &TrendlineEstimator{
		config:        config,
		history:       make([]sample, 0, config.WindowSize),
		smoothedDelay: 0,
		numDeltas:     0,
		// firstArrival is zero time, will be set on first sample
	}
}

// Update processes a new delay sample and returns the modified trend value.
//
// Parameters:
//   - arrivalTime: The monotonic arrival time of the packet
//   - delayVariationMs: The inter-packet delay variation in milliseconds
//     (positive = increasing delay, negative = decreasing delay)
//
// Returns the modified trend value, which is positive when delays are increasing
// (congestion building) and negative when delays are decreasing (queue draining).
func (t *TrendlineEstimator) Update(arrivalTime time.Time, delayVariationMs float64) float64 {
	// Set reference time on first sample
	if t.firstArrival.IsZero() {
		t.firstArrival = arrivalTime
	}

	// Compute arrival time in ms since start
	arrivalMs := float64(arrivalTime.Sub(t.firstArrival).Milliseconds())

	// Exponential smoothing of accumulated delay
	// smoothedDelay represents the accumulated trend
	t.smoothedDelay = t.config.SmoothingCoef*t.smoothedDelay + (1-t.config.SmoothingCoef)*delayVariationMs

	// Add sample to history
	t.history = append(t.history, sample{arrivalMs, t.smoothedDelay})

	// Maintain window size by removing oldest sample
	if len(t.history) > t.config.WindowSize {
		t.history = t.history[1:]
	}

	t.numDeltas++

	// Compute slope via linear regression
	slope := t.linearFitSlope()

	// Modified trend: min(numDeltas, 60) * slope * gain
	// The min(60) caps the multiplier to prevent runaway values during startup
	numSamples := float64(t.numDeltas)
	if numSamples > 60 {
		numSamples = 60
	}

	return numSamples * slope * t.config.ThresholdGain
}

// linearFitSlope computes the slope of the best-fit line through the sample history
// using ordinary least squares linear regression.
//
// Returns the slope in units of smoothedDelay per millisecond.
func (t *TrendlineEstimator) linearFitSlope() float64 {
	n := len(t.history)
	if n < 2 {
		return 0
	}

	// Least squares: slope = (n*sum(xy) - sum(x)*sum(y)) / (n*sum(x^2) - (sum(x))^2)
	var sumX, sumY, sumXX, sumXY float64
	for _, s := range t.history {
		sumX += s.arrivalTimeMs
		sumY += s.smoothedDelay
		sumXX += s.arrivalTimeMs * s.arrivalTimeMs
		sumXY += s.arrivalTimeMs * s.smoothedDelay
	}

	nf := float64(n)
	denom := nf*sumXX - sumX*sumX
	if denom == 0 {
		return 0
	}

	return (nf*sumXY - sumX*sumY) / denom
}

// Reset clears the estimator state, allowing it to be reused.
// This should be called when switching streams or after a long pause.
func (t *TrendlineEstimator) Reset() {
	t.history = t.history[:0] // Clear but keep capacity
	t.smoothedDelay = 0
	t.numDeltas = 0
	t.firstArrival = time.Time{} // Zero time
}
