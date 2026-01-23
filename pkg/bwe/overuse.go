// Package bwe implements Google Congestion Control (GCC) receiver-side
// bandwidth estimation for WebRTC.
package bwe

import (
	"math"
	"time"

	"github.com/thesyncim/bwe/pkg/bwe/internal"
)

// StateChangeCallback is called when bandwidth usage state changes.
// The callback receives the previous state and the new state.
type StateChangeCallback func(old, new BandwidthUsage)

// OveruseConfig contains configuration parameters for the overuse detector.
// These parameters control the adaptive threshold behavior and overuse detection timing.
type OveruseConfig struct {
	// InitialThreshold is the initial value for the adaptive threshold in milliseconds.
	// Default: 12.5 ms
	InitialThreshold float64

	// MinThreshold is the minimum allowed threshold value in milliseconds.
	// The threshold will never decrease below this value.
	// Default: 6.0 ms
	MinThreshold float64

	// MaxThreshold is the maximum allowed threshold value in milliseconds.
	// The threshold will never increase above this value.
	// Default: 600.0 ms
	MaxThreshold float64

	// Ku is the threshold increase rate coefficient.
	// Used when the absolute estimate exceeds the threshold.
	// A larger value causes faster threshold increase.
	// Default: 0.01 (slow increase to avoid TCP starvation)
	Ku float64

	// Kd is the threshold decrease rate coefficient.
	// Used when the absolute estimate is below the threshold.
	// A smaller value relative to Ku causes slower decrease, preventing oscillation.
	// Default: 0.00018 (much slower than Ku for stability)
	Kd float64

	// OveruseTimeThresh is the minimum duration the estimate must exceed
	// the threshold before signaling overuse. This prevents false positives
	// from transient delay spikes.
	// Default: 10ms
	OveruseTimeThresh time.Duration
}

// DefaultOveruseConfig returns an OveruseConfig with default values matching
// the GCC specification for TCP fairness and stable detection.
func DefaultOveruseConfig() OveruseConfig {
	return OveruseConfig{
		InitialThreshold:  12.5,
		MinThreshold:      6.0,
		MaxThreshold:      600.0,
		Ku:                0.01,
		Kd:                0.00018,
		OveruseTimeThresh: 10 * time.Millisecond,
	}
}

// OveruseDetector determines network congestion state by comparing filtered
// delay gradient estimates against an adaptive threshold. It implements the
// GCC overuse detection algorithm with:
//   - Adaptive threshold using asymmetric K_u/K_d coefficients
//   - Sustained overuse requirement before signaling
//   - Signal suppression when gradient is decreasing
//   - State change callbacks for application notification
type OveruseDetector struct {
	config           OveruseConfig
	clock            internal.Clock
	threshold        float64            // Current adaptive threshold
	lastUpdateTime   time.Time          // For threshold adaptation timing
	overuseStart     time.Time          // When current overuse period started
	overuseCounter   int                // Consecutive overuse detections
	inOveruseRegion  bool               // Whether we're tracking potential overuse
	prevEstimate     float64            // Previous estimate for suppression check
	hypothesis       BandwidthUsage     // Current state
	callback         StateChangeCallback
}

// NewOveruseDetector creates a new OveruseDetector with the given configuration
// and clock. If clock is nil, a default MonotonicClock is used.
func NewOveruseDetector(config OveruseConfig, clock internal.Clock) *OveruseDetector {
	if clock == nil {
		clock = internal.MonotonicClock{}
	}
	return &OveruseDetector{
		config:     config,
		clock:      clock,
		threshold:  config.InitialThreshold,
		hypothesis: BwNormal,
		// lastUpdateTime is zero - will be set on first update
	}
}

// SetCallback registers a callback function that will be invoked whenever
// the bandwidth usage state changes. Pass nil to disable callbacks.
func (d *OveruseDetector) SetCallback(cb StateChangeCallback) {
	d.callback = cb
}

// updateThreshold adapts the threshold based on the current estimate and time.
// The threshold moves toward the estimate level at a rate determined by
// asymmetric coefficients: K_u when above threshold (slow increase),
// K_d when below threshold (slow decrease, but faster than K_u).
func (d *OveruseDetector) updateThreshold(estimate float64, now time.Time) {
	absEstimate := math.Abs(estimate)

	// Initialize lastUpdateTime on first call
	if d.lastUpdateTime.IsZero() {
		d.lastUpdateTime = now
		return
	}

	// Time since last update in seconds
	deltaT := now.Sub(d.lastUpdateTime).Seconds()
	d.lastUpdateTime = now

	// Select coefficient: Ku when over threshold, Kd when under
	k := d.config.Kd
	if absEstimate > d.threshold {
		k = d.config.Ku
	}

	// Update threshold: del_var_th += deltaT * K * (|m| - del_var_th)
	d.threshold += deltaT * k * (absEstimate - d.threshold)

	// Clamp to valid range
	if d.threshold < d.config.MinThreshold {
		d.threshold = d.config.MinThreshold
	}
	if d.threshold > d.config.MaxThreshold {
		d.threshold = d.config.MaxThreshold
	}
}

// Detect processes a filtered delay gradient estimate and returns the current
// bandwidth usage state. This is the main entry point for the detector.
//
// The estimate should come from a Kalman filter or Trendline estimator.
// Positive values indicate queue building (potential congestion),
// negative values indicate queue draining (potential underuse).
//
// The detector:
//  1. Updates the adaptive threshold based on the estimate
//  2. Compares the estimate against the threshold
//  3. Requires sustained overuse (>= OveruseTimeThresh) before signaling
//  4. Suppresses overuse signal when the gradient is decreasing
//  5. Invokes the callback on state changes
func (d *OveruseDetector) Detect(estimate float64) BandwidthUsage {
	now := d.clock.Now()
	d.updateThreshold(estimate, now)

	oldHypothesis := d.hypothesis

	if estimate > d.threshold {
		// Potential overuse - track if we're entering the overuse region
		if !d.inOveruseRegion {
			// Start tracking overuse period
			d.overuseStart = now
			d.overuseCounter = 0
			d.inOveruseRegion = true
		}
		d.overuseCounter++

		// Signal suppression: don't signal overuse if gradient is decreasing
		if estimate < d.prevEstimate {
			d.hypothesis = BwNormal
		} else if now.Sub(d.overuseStart) >= d.config.OveruseTimeThresh && d.overuseCounter > 1 {
			// Sustained overuse confirmed
			d.hypothesis = BwOverusing
		}
	} else if estimate < -d.threshold {
		// Underuse (negative delay = queue draining fast)
		d.hypothesis = BwUnderusing
		d.inOveruseRegion = false
	} else {
		// Normal operation
		d.hypothesis = BwNormal
		d.inOveruseRegion = false
	}

	d.prevEstimate = estimate

	// Invoke callback on state change
	if d.hypothesis != oldHypothesis && d.callback != nil {
		d.callback(oldHypothesis, d.hypothesis)
	}

	return d.hypothesis
}

// State returns the current bandwidth usage state without processing a new estimate.
// This is useful for querying the detector state between estimate updates.
func (d *OveruseDetector) State() BandwidthUsage {
	return d.hypothesis
}

// Threshold returns the current adaptive threshold value.
// This is primarily useful for debugging and monitoring.
func (d *OveruseDetector) Threshold() float64 {
	return d.threshold
}

// Reset resets the detector to its initial state.
// This clears all internal state including the threshold, hypothesis,
// and timing information. The configuration is preserved.
func (d *OveruseDetector) Reset() {
	d.threshold = d.config.InitialThreshold
	d.hypothesis = BwNormal
	d.overuseStart = time.Time{}
	d.overuseCounter = 0
	d.inOveruseRegion = false
	d.prevEstimate = 0
	d.lastUpdateTime = time.Time{}
}
