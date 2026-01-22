// Package bwe implements Google Congestion Control (GCC) receiver-side
// bandwidth estimation for WebRTC.
package bwe

import "math"

// KalmanConfig holds tunable parameters for the Kalman filter.
// Default values are specified in IETF draft-ietf-rmcat-gcc.
type KalmanConfig struct {
	// ProcessNoise (q) is the state noise variance.
	// Spec default: 10^-3
	ProcessNoise float64

	// InitialError e(0) is the initial error covariance.
	// Spec default: 0.1
	InitialError float64

	// Chi is the exponential smoothing coefficient for measurement noise variance.
	// Recommended range: [0.001, 0.1]
	// Spec default: 0.01
	Chi float64
}

// DefaultKalmanConfig returns the spec-compliant default configuration.
func DefaultKalmanConfig() KalmanConfig {
	return KalmanConfig{
		ProcessNoise: 0.001, // q = 10^-3 per spec
		InitialError: 0.1,   // e(0) = 0.1 per spec
		Chi:          0.01,  // recommended range [0.001, 0.1]
	}
}

// KalmanFilter implements a scalar Kalman filter for delay gradient estimation.
// It takes noisy inter-arrival delay measurements and produces smoothed delay
// gradient estimates (m_hat) that track queuing delay trends.
//
// The filter tracks the TREND of delay, not absolute delay. A positive estimate
// means delay is increasing (queue building up). A negative estimate means delay
// is decreasing (queue draining).
type KalmanFilter struct {
	config       KalmanConfig
	estimate     float64 // m_hat(i) - current delay gradient estimate in ms
	errorCov     float64 // e(i) - error covariance
	measureNoise float64 // var_v_hat - measurement noise variance
}

// NewKalmanFilter creates a new Kalman filter with the given configuration.
func NewKalmanFilter(config KalmanConfig) *KalmanFilter {
	return &KalmanFilter{
		config:       config,
		estimate:     0,                   // assume no initial delay gradient
		errorCov:     config.InitialError, // e(0) from config
		measureNoise: 1.0,                 // initial measurement noise variance, will adapt
	}
}

// Update processes a new delay variation measurement and returns the updated
// delay gradient estimate. The measurement is delay variation in milliseconds.
func (k *KalmanFilter) Update(measurement float64) float64 {
	// Innovation (prediction error): difference between measurement and estimate
	z := measurement - k.estimate

	// Outlier filtering: cap innovation at 3*sqrt(measurement_variance)
	// This prevents large outliers from destabilizing the filter
	maxDeviation := 3 * math.Sqrt(k.measureNoise)
	zCapped := z
	if z > maxDeviation {
		zCapped = maxDeviation
	} else if z < -maxDeviation {
		zCapped = -maxDeviation
	}

	// Update measurement noise estimate using exponential averaging
	// var_v_hat = (1 - chi) * var_v_hat + chi * z_capped^2
	// min variance of 1.0 prevents division issues
	k.measureNoise = math.Max(1.0, (1-k.config.Chi)*k.measureNoise+k.config.Chi*zCapped*zCapped)

	// Kalman gain: K = (e + q) / (var_v + e + q)
	// Higher gain means more weight on measurement, lower means more on prediction
	gain := (k.errorCov + k.config.ProcessNoise) / (k.measureNoise + k.errorCov + k.config.ProcessNoise)

	// State update: m_hat(i) = m_hat(i-1) + K * z
	// Use uncapped z for the state update (capping is only for variance estimation)
	k.estimate = k.estimate + z*gain

	// Error covariance update: e(i) = (1 - K) * (e(i-1) + q)
	k.errorCov = (1 - gain) * (k.errorCov + k.config.ProcessNoise)

	return k.estimate
}

// Estimate returns the current delay gradient estimate without updating.
// Useful for inspection without processing a new measurement.
func (k *KalmanFilter) Estimate() float64 {
	return k.estimate
}

// Reset reinitializes the filter state to initial conditions.
// This can be used when switching streams or after long gaps.
func (k *KalmanFilter) Reset() {
	k.estimate = 0
	k.errorCov = k.config.InitialError
	k.measureNoise = 1.0
}
