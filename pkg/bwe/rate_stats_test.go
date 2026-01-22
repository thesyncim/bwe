package bwe

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

// =============================================================================
// Basic Functionality Tests
// =============================================================================

func TestRateStats_EmptyReturnsNotOk(t *testing.T) {
	r := NewRateStats(DefaultRateStatsConfig())

	rate, ok := r.Rate(time.Now())
	assert.False(t, ok, "Rate() should return ok=false when no samples")
	assert.Equal(t, int64(0), rate, "Rate should be 0 when not ok")
}

func TestRateStats_SingleSampleReturnsNotOk(t *testing.T) {
	r := NewRateStats(DefaultRateStatsConfig())
	t0 := time.Now()

	r.Update(1000, t0)

	rate, ok := r.Rate(t0)
	assert.False(t, ok, "Rate() should return ok=false with only one sample")
	assert.Equal(t, int64(0), rate, "Rate should be 0 when not ok")
}

func TestRateStats_TwoSamplesWithSmallGapReturnsNotOk(t *testing.T) {
	r := NewRateStats(DefaultRateStatsConfig())
	t0 := time.Now()

	// Two samples within 1ms - insufficient time span
	r.Update(1000, t0)
	r.Update(1000, t0.Add(500*time.Microsecond))

	rate, ok := r.Rate(t0.Add(500 * time.Microsecond))
	assert.False(t, ok, "Rate() should return ok=false with elapsed < 1ms")
	assert.Equal(t, int64(0), rate)
}

func TestRateStats_BasicRate(t *testing.T) {
	r := NewRateStats(DefaultRateStatsConfig())
	t0 := time.Now()

	// 1000 bytes over 1 second = 8000 bps
	r.Update(1000, t0)
	r.Update(0, t0.Add(time.Second))

	rate, ok := r.Rate(t0.Add(time.Second))
	assert.True(t, ok, "Rate() should return ok=true with sufficient data")
	assert.Equal(t, int64(8000), rate, "1000 bytes over 1 second = 8000 bps")
}

func TestRateStats_OneMbps(t *testing.T) {
	r := NewRateStats(DefaultRateStatsConfig())
	t0 := time.Now()

	// 125000 bytes over 1 second = 1 Mbps (1,000,000 bps)
	r.Update(125000, t0)
	r.Update(0, t0.Add(time.Second))

	rate, ok := r.Rate(t0.Add(time.Second))
	assert.True(t, ok)
	// Allow small tolerance due to floating point
	assert.InDelta(t, 1_000_000, rate, 100, "125000 bytes/second = 1 Mbps")
}

func TestRateStats_MultipleSamples(t *testing.T) {
	r := NewRateStats(DefaultRateStatsConfig())
	t0 := time.Now()

	// Add 500 bytes at t=0, 500 bytes at t=500ms
	// Total: 1000 bytes over 500ms = 16000 bps
	r.Update(500, t0)
	r.Update(500, t0.Add(500*time.Millisecond))

	rate, ok := r.Rate(t0.Add(500 * time.Millisecond))
	assert.True(t, ok)
	assert.Equal(t, int64(16000), rate, "1000 bytes over 500ms = 16000 bps")
}

// =============================================================================
// Window Sliding Tests
// =============================================================================

func TestRateStats_WindowSliding(t *testing.T) {
	config := RateStatsConfig{
		WindowSize: time.Second, // 1 second window
	}
	r := NewRateStats(config)
	t0 := time.Now()

	// Add samples at t=0, t=500ms
	r.Update(1000, t0)
	r.Update(1000, t0.Add(500*time.Millisecond))

	// At t=500ms, both samples are in window
	// Total: 2000 bytes over 500ms = 32000 bps
	rate, ok := r.Rate(t0.Add(500 * time.Millisecond))
	assert.True(t, ok)
	assert.Equal(t, int64(32000), rate)

	// Add sample at t=1.5s - this should slide out the t=0 sample
	r.Update(1000, t0.Add(1500*time.Millisecond))

	// Now window is [500ms, 1.5s] - 1 second span, 2000 bytes = 16000 bps
	// (t=0 sample is expired, not counted)
	rate, ok = r.Rate(t0.Add(1500 * time.Millisecond))
	assert.True(t, ok)
	assert.Equal(t, int64(16000), rate, "Window should have slid out t=0 sample")
}

func TestRateStats_WindowSlidingRemovesMultiple(t *testing.T) {
	config := RateStatsConfig{
		WindowSize: 500 * time.Millisecond,
	}
	r := NewRateStats(config)
	t0 := time.Now()

	// Add samples at t=0, t=100ms, t=200ms
	r.Update(100, t0)
	r.Update(100, t0.Add(100*time.Millisecond))
	r.Update(100, t0.Add(200*time.Millisecond))

	// At t=200ms with 500ms window, all 3 samples are in window
	rate, ok := r.Rate(t0.Add(200 * time.Millisecond))
	assert.True(t, ok)
	// 300 bytes over 200ms = 12000 bps
	assert.Equal(t, int64(12000), rate)

	// At t=800ms with 500ms window, window is [300ms, 800ms]
	// Only samples at t >= 300ms are in window, which is none of the above
	r.Update(200, t0.Add(800*time.Millisecond))

	// Check at t=800ms - only the t=800ms sample should be in window
	rate, ok = r.Rate(t0.Add(800 * time.Millisecond))
	// Only 1 sample (t=800ms), so should return ok=false
	assert.False(t, ok, "Should return not ok when all but one sample expired")
}

// =============================================================================
// Gap Handling Tests
// =============================================================================

func TestRateStats_GapHandling(t *testing.T) {
	config := RateStatsConfig{
		WindowSize: time.Second,
	}
	r := NewRateStats(config)
	t0 := time.Now()

	// Add sample at t=0
	r.Update(1000, t0)
	r.Update(1000, t0.Add(100*time.Millisecond))

	// Query at t=5s - gap larger than window, all samples should be expired
	rate, ok := r.Rate(t0.Add(5 * time.Second))
	assert.False(t, ok, "Rate() should return ok=false when all samples expired due to gap")
	assert.Equal(t, int64(0), rate)
}

func TestRateStats_GapRecovery(t *testing.T) {
	config := RateStatsConfig{
		WindowSize: time.Second,
	}
	r := NewRateStats(config)
	t0 := time.Now()

	// Add sample at t=0
	r.Update(1000, t0)

	// Gap of 5 seconds - should expire all samples
	// Then add new samples
	r.Update(500, t0.Add(5*time.Second))
	r.Update(500, t0.Add(5*time.Second+100*time.Millisecond))

	// Rate should be based only on new samples
	// 1000 bytes over 100ms = 80000 bps
	rate, ok := r.Rate(t0.Add(5*time.Second + 100*time.Millisecond))
	assert.True(t, ok)
	assert.Equal(t, int64(80000), rate)
}

// =============================================================================
// High Packet Rate Tests
// =============================================================================

func TestRateStats_HighPacketRate(t *testing.T) {
	r := NewRateStats(DefaultRateStatsConfig())
	t0 := time.Now()

	// Simulate 30fps video at ~1 Mbps = ~4167 bytes/frame
	// ~33ms between frames, but packets are smaller
	// Simulate 1000 packets over 1 second (1 packet per ms)
	bytesPerPacket := int64(125) // 125 bytes * 1000 packets * 8 bits = 1 Mbps

	for i := 0; i < 1000; i++ {
		r.Update(bytesPerPacket, t0.Add(time.Duration(i)*time.Millisecond))
	}

	rate, ok := r.Rate(t0.Add(999 * time.Millisecond))
	assert.True(t, ok)
	// 125000 bytes over 999ms ~ 1.001 Mbps
	assert.InDelta(t, 1_001_001, rate, 10000, "Should handle high packet rates accurately")
}

func TestRateStats_HighPacketRateWithSliding(t *testing.T) {
	config := RateStatsConfig{
		WindowSize: 500 * time.Millisecond,
	}
	r := NewRateStats(config)
	t0 := time.Now()

	// Add 2000 packets over 2 seconds
	bytesPerPacket := int64(125)

	for i := 0; i < 2000; i++ {
		r.Update(bytesPerPacket, t0.Add(time.Duration(i)*time.Millisecond))
	}

	// Rate should be based on last 500ms only
	rate, ok := r.Rate(t0.Add(1999 * time.Millisecond))
	assert.True(t, ok)
	// 500 samples * 125 bytes = 62500 bytes over 499ms ~ 1.003 Mbps
	assert.InDelta(t, 1_000_000, rate, 50000, "Should compute rate from window samples only")
}

// =============================================================================
// Reset Tests
// =============================================================================

func TestRateStats_Reset(t *testing.T) {
	r := NewRateStats(DefaultRateStatsConfig())
	t0 := time.Now()

	// Add samples
	r.Update(1000, t0)
	r.Update(1000, t0.Add(time.Second))

	// Verify we have a rate
	rate, ok := r.Rate(t0.Add(time.Second))
	assert.True(t, ok)
	assert.Equal(t, int64(16000), rate)

	// Reset
	r.Reset()

	// Should return not ok after reset
	rate, ok = r.Rate(t0.Add(time.Second))
	assert.False(t, ok, "Rate() should return ok=false after reset")
	assert.Equal(t, int64(0), rate)
}

func TestRateStats_ResetAndReuse(t *testing.T) {
	r := NewRateStats(DefaultRateStatsConfig())
	t0 := time.Now()

	// First use
	r.Update(1000, t0)
	r.Update(0, t0.Add(time.Second))
	rate, ok := r.Rate(t0.Add(time.Second))
	assert.True(t, ok)
	assert.Equal(t, int64(8000), rate)

	// Reset
	r.Reset()

	// Reuse with different data
	t1 := t0.Add(10 * time.Second)
	r.Update(500, t1)
	r.Update(500, t1.Add(500*time.Millisecond))

	rate, ok = r.Rate(t1.Add(500 * time.Millisecond))
	assert.True(t, ok)
	assert.Equal(t, int64(16000), rate, "Should compute rate correctly after reset and reuse")
}

// =============================================================================
// Edge Cases
// =============================================================================

func TestRateStats_ZeroBytes(t *testing.T) {
	r := NewRateStats(DefaultRateStatsConfig())
	t0 := time.Now()

	// Add zero-byte samples
	r.Update(0, t0)
	r.Update(0, t0.Add(time.Second))

	rate, ok := r.Rate(t0.Add(time.Second))
	assert.True(t, ok, "Should return ok=true even with zero bytes")
	assert.Equal(t, int64(0), rate, "Rate should be 0 with zero-byte samples")
}

func TestRateStats_MixedZeroAndNonZero(t *testing.T) {
	r := NewRateStats(DefaultRateStatsConfig())
	t0 := time.Now()

	r.Update(0, t0)
	r.Update(1000, t0.Add(250*time.Millisecond))
	r.Update(0, t0.Add(500*time.Millisecond))
	r.Update(1000, t0.Add(750*time.Millisecond))
	r.Update(0, t0.Add(time.Second))

	// 2000 bytes over 1 second = 16000 bps
	rate, ok := r.Rate(t0.Add(time.Second))
	assert.True(t, ok)
	assert.Equal(t, int64(16000), rate)
}

func TestRateStats_DefaultConfigUsedForZeroWindow(t *testing.T) {
	config := RateStatsConfig{
		WindowSize: 0, // Invalid, should default to 1 second
	}
	r := NewRateStats(config)
	t0 := time.Now()

	// This should work with default 1 second window
	r.Update(1000, t0)
	r.Update(0, t0.Add(time.Second))

	rate, ok := r.Rate(t0.Add(time.Second))
	assert.True(t, ok)
	assert.Equal(t, int64(8000), rate)
}

func TestRateStats_NegativeWindowUsesDefault(t *testing.T) {
	config := RateStatsConfig{
		WindowSize: -time.Second, // Invalid, should default to 1 second
	}
	r := NewRateStats(config)
	t0 := time.Now()

	r.Update(1000, t0)
	r.Update(0, t0.Add(time.Second))

	rate, ok := r.Rate(t0.Add(time.Second))
	assert.True(t, ok)
	assert.Equal(t, int64(8000), rate)
}

// =============================================================================
// Table-Driven Tests
// =============================================================================

func TestRateStats_Rates(t *testing.T) {
	tests := []struct {
		name         string
		bytes        []int64
		durations    []time.Duration
		expectedRate int64
		expectedOk   bool
		tolerance    int64
	}{
		{
			name:         "simple 8kbps",
			bytes:        []int64{1000, 0},
			durations:    []time.Duration{0, time.Second},
			expectedRate: 8000,
			expectedOk:   true,
			tolerance:    0,
		},
		{
			name:         "simple 16kbps",
			bytes:        []int64{1000, 1000},
			durations:    []time.Duration{0, time.Second},
			expectedRate: 16000,
			expectedOk:   true,
			tolerance:    0,
		},
		{
			name:         "1Mbps",
			bytes:        []int64{125000, 0},
			durations:    []time.Duration{0, time.Second},
			expectedRate: 1_000_000,
			expectedOk:   true,
			tolerance:    100,
		},
		{
			name:         "10Mbps",
			bytes:        []int64{1250000, 0},
			durations:    []time.Duration{0, time.Second},
			expectedRate: 10_000_000,
			expectedOk:   true,
			tolerance:    100,
		},
		{
			name:         "half second window",
			bytes:        []int64{500, 0},
			durations:    []time.Duration{0, 500 * time.Millisecond},
			expectedRate: 8000, // 500 bytes over 500ms = 8000 bps
			expectedOk:   true,
			tolerance:    0,
		},
		{
			name:         "insufficient time span",
			bytes:        []int64{1000, 1000},
			durations:    []time.Duration{0, 500 * time.Microsecond},
			expectedRate: 0,
			expectedOk:   false,
			tolerance:    0,
		},
		{
			name:         "single sample",
			bytes:        []int64{1000},
			durations:    []time.Duration{0},
			expectedRate: 0,
			expectedOk:   false,
			tolerance:    0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := NewRateStats(DefaultRateStatsConfig())
			t0 := time.Now()

			for i, bytes := range tt.bytes {
				r.Update(bytes, t0.Add(tt.durations[i]))
			}

			lastDuration := tt.durations[len(tt.durations)-1]
			rate, ok := r.Rate(t0.Add(lastDuration))

			assert.Equal(t, tt.expectedOk, ok)
			if tt.expectedOk {
				if tt.tolerance == 0 {
					assert.Equal(t, tt.expectedRate, rate)
				} else {
					assert.InDelta(t, tt.expectedRate, rate, float64(tt.tolerance))
				}
			}
		})
	}
}

// =============================================================================
// Benchmark Tests (for performance verification)
// =============================================================================

func BenchmarkRateStats_Update(b *testing.B) {
	r := NewRateStats(DefaultRateStatsConfig())
	t0 := time.Now()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		r.Update(1000, t0.Add(time.Duration(i)*time.Millisecond))
	}
}

func BenchmarkRateStats_Rate(b *testing.B) {
	r := NewRateStats(DefaultRateStatsConfig())
	t0 := time.Now()

	// Pre-populate with samples
	for i := 0; i < 1000; i++ {
		r.Update(125, t0.Add(time.Duration(i)*time.Millisecond))
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		r.Rate(t0.Add(999 * time.Millisecond))
	}
}

func BenchmarkRateStats_HighVolume(b *testing.B) {
	config := RateStatsConfig{
		WindowSize: time.Second,
	}
	r := NewRateStats(config)
	t0 := time.Now()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		// Simulate one second of high rate packets
		for j := 0; j < 1000; j++ {
			r.Update(125, t0.Add(time.Duration(i*1000+j)*time.Millisecond))
		}
		r.Rate(t0.Add(time.Duration(i*1000+999) * time.Millisecond))
	}
}
