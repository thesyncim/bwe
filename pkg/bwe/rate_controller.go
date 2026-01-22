// Package bwe implements Google Congestion Control (GCC) receiver-side
// bandwidth estimation for WebRTC.
package bwe

import (
	"math"
	"time"
)

// RateControlState represents the AIMD state machine state.
// The state machine transitions based on congestion signals (BandwidthUsage)
// from the delay estimator, following GCC spec Section 6.
type RateControlState int

const (
	// RateHold indicates the rate should be maintained (no change).
	// This is the initial state and serves as a transition buffer between
	// Decrease and Increase states.
	RateHold RateControlState = iota
	// RateIncrease indicates the rate can grow multiplicatively.
	RateIncrease
	// RateDecrease indicates congestion detected - apply multiplicative decrease.
	RateDecrease
)

// String returns a string representation of the RateControlState.
func (s RateControlState) String() string {
	switch s {
	case RateHold:
		return "Hold"
	case RateIncrease:
		return "Increase"
	case RateDecrease:
		return "Decrease"
	default:
		return "Unknown"
	}
}

// RateControllerConfig configures the AIMD rate controller.
type RateControllerConfig struct {
	// MinBitrate is the minimum allowed bitrate in bits per second.
	// Default: 10,000 (10 kbps)
	MinBitrate int64

	// MaxBitrate is the maximum allowed bitrate in bits per second.
	// Default: 30,000,000 (30 Mbps)
	MaxBitrate int64

	// InitialBitrate is the starting bitrate estimate in bits per second.
	// Default: 300,000 (300 kbps)
	InitialBitrate int64

	// Beta is the multiplicative decrease factor applied during congestion.
	// On overuse, new_rate = beta * incoming_rate
	// Default: 0.85 (15% reduction)
	Beta float64
}

// DefaultRateControllerConfig returns the default configuration for the rate controller.
func DefaultRateControllerConfig() RateControllerConfig {
	return RateControllerConfig{
		MinBitrate:     10_000,     // 10 kbps minimum
		MaxBitrate:     30_000_000, // 30 Mbps maximum
		InitialBitrate: 300_000,    // 300 kbps initial
		Beta:           0.85,       // 15% decrease on congestion
	}
}

// RateController implements AIMD (Additive Increase Multiplicative Decrease)
// rate control based on GCC spec Section 6.
//
// The controller maintains three states:
//   - Hold: Maintain current rate (transition buffer)
//   - Increase: Multiplicatively increase rate (1.08^elapsed)
//   - Decrease: Multiplicatively decrease rate (beta * incoming_rate)
//
// State transitions follow the GCC specification:
//
//	Signal     | Hold     | Increase | Decrease
//	-----------+----------+----------+----------
//	Overusing  | Decrease | Decrease | (stay)
//	Normal     | Increase | (stay)   | Hold
//	Underusing | (stay)   | Hold     | Hold
//
// CRITICAL: The multiplicative decrease uses the measured incoming rate,
// NOT the current estimate. This ensures the controller responds to what
// the sender is actually transmitting, not what we estimated.
type RateController struct {
	config      RateControllerConfig
	state       RateControlState
	currentRate int64
	lastUpdate  time.Time
}

// NewRateController creates a new rate controller with the given configuration.
func NewRateController(config RateControllerConfig) *RateController {
	// Apply defaults for zero values
	if config.MinBitrate <= 0 {
		config.MinBitrate = 10_000
	}
	if config.MaxBitrate <= 0 {
		config.MaxBitrate = 30_000_000
	}
	if config.InitialBitrate <= 0 {
		config.InitialBitrate = 300_000
	}
	if config.Beta <= 0 || config.Beta >= 1.0 {
		config.Beta = 0.85
	}

	return &RateController{
		config:      config,
		state:       RateHold, // Start in Hold state
		currentRate: config.InitialBitrate,
		lastUpdate:  time.Time{}, // Zero time indicates first update
	}
}

// Update processes a congestion signal and incoming rate measurement,
// returning the new bandwidth estimate in bits per second.
//
// Parameters:
//   - signal: The congestion signal from the delay estimator (BwNormal, BwOverusing, BwUnderusing)
//   - incomingRate: The measured incoming bitrate from RateStats in bits per second
//   - now: Current time for rate increase calculations
//
// CRITICAL: incomingRate is the measured incoming bitrate. Multiplicative
// decrease uses incomingRate, NOT currentRate. This is essential for
// proper AIMD behavior when the sender has already reduced its rate.
//
// Returns the new bandwidth estimate in bits per second.
func (c *RateController) Update(signal BandwidthUsage, incomingRate int64, now time.Time) int64 {
	// Transition state based on signal
	c.transitionState(signal)

	// Apply rate adjustment based on new state
	c.adjustRate(incomingRate, now)

	// Enforce bounds
	c.clampRate()

	// Enforce ratio constraint: estimate <= 1.5 * incomingRate
	// This prevents estimate from diverging too far from actual incoming rate
	if incomingRate > 0 {
		maxByRatio := int64(1.5 * float64(incomingRate))
		if c.currentRate > maxByRatio {
			c.currentRate = maxByRatio
		}
	}

	// Update timestamp for next increase calculation
	c.lastUpdate = now

	return c.currentRate
}

// transitionState applies the GCC state transition table.
//
//	Signal     | Hold     | Increase | Decrease
//	-----------+----------+----------+----------
//	Overusing  | Decrease | Decrease | (stay)
//	Normal     | Increase | (stay)   | Hold
//	Underusing | (stay)   | Hold     | Hold
func (c *RateController) transitionState(signal BandwidthUsage) {
	switch c.state {
	case RateHold:
		switch signal {
		case BwOverusing:
			c.state = RateDecrease
		case BwNormal:
			c.state = RateIncrease
		case BwUnderusing:
			// Stay in Hold
		}

	case RateIncrease:
		switch signal {
		case BwOverusing:
			c.state = RateDecrease
		case BwNormal:
			// Stay in Increase
		case BwUnderusing:
			c.state = RateHold
		}

	case RateDecrease:
		switch signal {
		case BwOverusing:
			// Stay in Decrease
		case BwNormal:
			c.state = RateHold // CRITICAL: Go to Hold, NOT Increase
		case BwUnderusing:
			c.state = RateHold
		}
	}
}

// adjustRate applies rate adjustment based on current state.
func (c *RateController) adjustRate(incomingRate int64, now time.Time) {
	switch c.state {
	case RateDecrease:
		// CRITICAL: Use incomingRate, NOT currentRate
		// This handles the case where sender has already reduced rate
		c.currentRate = int64(c.config.Beta * float64(incomingRate))

	case RateIncrease:
		// Multiplicative increase: rate = rate * 1.08^elapsed
		// Cap elapsed at 1 second to prevent excessive jumps after idle periods
		if !c.lastUpdate.IsZero() {
			elapsed := now.Sub(c.lastUpdate).Seconds()
			elapsed = math.Min(elapsed, 1.0) // Cap at 1 second
			if elapsed > 0 {
				eta := math.Pow(1.08, elapsed)
				c.currentRate = int64(eta * float64(c.currentRate))
			}
		}

	case RateHold:
		// No change to rate
	}
}

// clampRate enforces min/max bitrate bounds.
func (c *RateController) clampRate() {
	if c.currentRate < c.config.MinBitrate {
		c.currentRate = c.config.MinBitrate
	}
	if c.currentRate > c.config.MaxBitrate {
		c.currentRate = c.config.MaxBitrate
	}
}

// State returns the current rate control state.
func (c *RateController) State() RateControlState {
	return c.state
}

// Estimate returns the current bandwidth estimate without updating.
// Returns the estimate in bits per second.
func (c *RateController) Estimate() int64 {
	return c.currentRate
}

// Reset resets the controller to initial state.
// Call this when switching streams or after extended silence.
func (c *RateController) Reset() {
	c.state = RateHold
	c.currentRate = c.config.InitialBitrate
	c.lastUpdate = time.Time{}
}
