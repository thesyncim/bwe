// Package bwe implements Google Congestion Control (GCC) receiver-side
// bandwidth estimation for WebRTC.
package bwe

import "time"

// RateStatsConfig configures the sliding window rate measurement.
type RateStatsConfig struct {
	// WindowSize is the duration of the sliding window for rate calculation.
	// Default: 1 second (matches libwebrtc RateStatistics).
	WindowSize time.Duration
}

// DefaultRateStatsConfig returns default configuration for rate statistics.
func DefaultRateStatsConfig() RateStatsConfig {
	return RateStatsConfig{
		WindowSize: time.Second, // 1 second window
	}
}

// rateSample represents a single byte count measurement at a point in time.
type rateSample struct {
	timestamp time.Time
	bytes     int64
}

// RateStats tracks incoming bitrate over a sliding time window.
// It computes bits-per-second from accumulated byte samples within the window.
//
// Usage:
//
//	r := NewRateStats(DefaultRateStatsConfig())
//	r.Update(packetSize, arrivalTime)
//	if rate, ok := r.Rate(now); ok {
//	    fmt.Printf("Current rate: %d bps\n", rate)
//	}
type RateStats struct {
	windowSize time.Duration
	samples    []rateSample
	totalBytes int64
}

// NewRateStats creates a new rate statistics tracker with the given configuration.
func NewRateStats(config RateStatsConfig) *RateStats {
	windowSize := config.WindowSize
	if windowSize <= 0 {
		windowSize = time.Second // Default to 1 second
	}
	return &RateStats{
		windowSize: windowSize,
		samples:    make([]rateSample, 0, 64), // Pre-allocate for typical packet rates
		totalBytes: 0,
	}
}

// Update adds a new byte count sample at the given time.
// Call this for each received packet with the packet size.
//
// The method automatically removes samples that have expired beyond the
// sliding window. If called after a gap larger than the window size,
// all previous samples will be removed.
func (r *RateStats) Update(bytes int64, now time.Time) {
	// Remove expired samples first
	r.removeExpired(now)

	// Add new sample
	r.samples = append(r.samples, rateSample{
		timestamp: now,
		bytes:     bytes,
	})
	r.totalBytes += bytes
}

// Rate returns the current bitrate in bits per second.
// Returns (rate, true) if sufficient data exists to compute a meaningful rate.
// Returns (0, false) if:
//   - No samples exist
//   - Only one sample exists (need at least 2 for time span)
//   - Time span between oldest and newest sample is less than 1ms
//   - All samples have expired
//
// The rate is computed as: (totalBytes * 8) / elapsed.Seconds()
func (r *RateStats) Rate(now time.Time) (bitsPerSec int64, ok bool) {
	// Remove expired samples first
	r.removeExpired(now)

	// Need at least 2 samples to compute a rate
	if len(r.samples) < 2 {
		return 0, false
	}

	// Calculate time span from oldest to newest sample
	oldest := r.samples[0].timestamp
	newest := r.samples[len(r.samples)-1].timestamp
	elapsed := newest.Sub(oldest)

	// Require at least 1ms of elapsed time to avoid division issues
	if elapsed < time.Millisecond {
		return 0, false
	}

	// Rate = (bytes * 8 bits/byte) / elapsed seconds
	// Using float64 for precise calculation, then convert to int64
	elapsedSeconds := elapsed.Seconds()
	rate := float64(r.totalBytes*8) / elapsedSeconds

	return int64(rate), true
}

// Reset clears all samples and accumulated state.
// Call this when switching streams or after extended silence.
func (r *RateStats) Reset() {
	r.samples = r.samples[:0] // Keep capacity, clear contents
	r.totalBytes = 0
}

// removeExpired removes all samples older than windowSize from now.
// This maintains the sliding window invariant.
func (r *RateStats) removeExpired(now time.Time) {
	cutoff := now.Add(-r.windowSize)

	// Find index of first non-expired sample
	expiredCount := 0
	for i, s := range r.samples {
		if s.timestamp.After(cutoff) || s.timestamp.Equal(cutoff) {
			break
		}
		// Sample is expired (before cutoff)
		r.totalBytes -= s.bytes
		expiredCount = i + 1
	}

	// Remove expired samples by shifting slice
	if expiredCount > 0 {
		r.samples = r.samples[expiredCount:]
	}
}
