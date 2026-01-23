// Package bwe implements Google Congestion Control (GCC) receiver-side
// bandwidth estimation for WebRTC.
package bwe

import (
	"time"

	"github.com/thesyncim/bwe/pkg/bwe/internal"
)

// FilterType specifies which delay filter to use in the delay estimator.
type FilterType int

const (
	// FilterKalman uses Kalman filtering for delay gradient estimation.
	// This is the traditional approach from the GCC specification.
	FilterKalman FilterType = iota

	// FilterTrendline uses linear regression trendline estimation.
	// This is the modern approach used in WebRTC reference implementations.
	FilterTrendline
)

// DelayEstimatorConfig holds configuration for the delay-based bandwidth estimator.
type DelayEstimatorConfig struct {
	// FilterType specifies which delay filter to use.
	FilterType FilterType

	// BurstThreshold is the time window for grouping packets into bursts.
	// Packets arriving within this duration are considered part of the same burst.
	BurstThreshold time.Duration

	// KalmanConfig is used if FilterType == FilterKalman.
	KalmanConfig KalmanConfig

	// TrendlineConfig is used if FilterType == FilterTrendline.
	TrendlineConfig TrendlineConfig

	// OveruseConfig configures the overuse detector behavior.
	OveruseConfig OveruseConfig
}

// DefaultDelayEstimatorConfig returns the default configuration for the delay estimator.
// Uses Kalman filter (traditional approach) with standard burst threshold.
func DefaultDelayEstimatorConfig() DelayEstimatorConfig {
	return DelayEstimatorConfig{
		FilterType:      FilterKalman, // Kalman is traditional, Trendline is modern
		BurstThreshold:  5 * time.Millisecond,
		KalmanConfig:    DefaultKalmanConfig(),
		TrendlineConfig: DefaultTrendlineConfig(),
		OveruseConfig:   DefaultOveruseConfig(),
	}
}

// delayFilter is an internal interface abstracting Kalman and Trendline filters.
// Both filters take delay variation samples and produce smoothed estimates.
type delayFilter interface {
	// Update processes a new delay variation sample and returns the filtered estimate.
	// arrivalTime is the packet arrival time (used by trendline, ignored by Kalman).
	// delayMs is the delay variation in milliseconds.
	Update(arrivalTime time.Time, delayMs float64) float64

	// Reset clears the filter state to initial conditions.
	Reset()
}

// kalmanAdapter adapts KalmanFilter to the delayFilter interface.
// KalmanFilter.Update only takes the measurement, so arrivalTime is ignored.
type kalmanAdapter struct {
	filter *KalmanFilter
}

// Update processes a delay sample through the Kalman filter.
// The arrivalTime parameter is ignored by Kalman filtering.
func (k *kalmanAdapter) Update(arrivalTime time.Time, delayMs float64) float64 {
	return k.filter.Update(delayMs)
}

// Reset clears the Kalman filter state.
func (k *kalmanAdapter) Reset() {
	k.filter.Reset()
}

// trendlineAdapter adapts TrendlineEstimator to the delayFilter interface.
// TrendlineEstimator already has a compatible signature.
type trendlineAdapter struct {
	estimator *TrendlineEstimator
}

// Update processes a delay sample through the trendline estimator.
func (t *trendlineAdapter) Update(arrivalTime time.Time, delayMs float64) float64 {
	return t.estimator.Update(arrivalTime, delayMs)
}

// Reset clears the trendline estimator state.
func (t *trendlineAdapter) Reset() {
	t.estimator.Reset()
}

// DelayEstimator orchestrates the complete delay-based bandwidth estimation pipeline.
// It combines:
//   - InterArrivalCalculator for burst grouping and delay variation measurement
//   - Kalman or Trendline filter for noise reduction
//   - OveruseDetector for congestion state detection
//
// The estimator processes packets via OnPacket() and produces BandwidthUsage signals.
type DelayEstimator struct {
	config       DelayEstimatorConfig
	clock        internal.Clock
	interarrival *InterArrivalCalculator
	filter       delayFilter
	detector     *OveruseDetector
}

// NewDelayEstimator creates a new DelayEstimator with the given configuration.
// If clock is nil, a default MonotonicClock is used.
func NewDelayEstimator(config DelayEstimatorConfig, clock internal.Clock) *DelayEstimator {
	if clock == nil {
		clock = internal.MonotonicClock{}
	}

	// Create the inter-arrival calculator with burst threshold
	interarrival := NewInterArrivalCalculator(config.BurstThreshold)

	// Create the appropriate filter based on configuration
	var filter delayFilter
	switch config.FilterType {
	case FilterTrendline:
		filter = &trendlineAdapter{
			estimator: NewTrendlineEstimator(config.TrendlineConfig),
		}
	default: // FilterKalman
		filter = &kalmanAdapter{
			filter: NewKalmanFilter(config.KalmanConfig),
		}
	}

	// Create the overuse detector
	detector := NewOveruseDetector(config.OveruseConfig, clock)

	return &DelayEstimator{
		config:       config,
		clock:        clock,
		interarrival: interarrival,
		filter:       filter,
		detector:     detector,
	}
}

// OnPacket processes a received packet and returns the current bandwidth usage state.
// This is the main entry point for the delay-based estimator pipeline.
//
// The pipeline:
//  1. Groups packet into bursts using InterArrivalCalculator
//  2. Computes delay variation between burst groups
//  3. Filters the delay variation (Kalman or Trendline)
//  4. Detects overuse/underuse via OveruseDetector
//
// Returns the current BandwidthUsage state (Normal, Underusing, or Overusing).
func (e *DelayEstimator) OnPacket(pkt PacketInfo) BandwidthUsage {
	// Feed packet to inter-arrival calculator
	delayVariation, hasResult := e.interarrival.AddPacket(pkt)
	if !hasResult {
		// Still accumulating group, return current state
		return e.detector.State()
	}

	// Convert delay variation to milliseconds for filter
	delayMs := float64(delayVariation.Microseconds()) / 1000.0

	// Feed to filter (Kalman or Trendline)
	estimate := e.filter.Update(pkt.ArrivalTime, delayMs)

	// Feed estimate to overuse detector
	return e.detector.Detect(estimate)
}

// State returns the current bandwidth usage state without processing a packet.
// This is useful for querying state between packet arrivals.
func (e *DelayEstimator) State() BandwidthUsage {
	return e.detector.State()
}

// SetCallback registers a callback that will be invoked when bandwidth usage
// state changes. Pass nil to disable callbacks.
func (e *DelayEstimator) SetCallback(cb StateChangeCallback) {
	e.detector.SetCallback(cb)
}

// Reset resets all components to their initial state.
// This should be called when switching streams or after extended silence.
func (e *DelayEstimator) Reset() {
	e.interarrival.Reset()
	e.filter.Reset()
	e.detector.Reset()
}
