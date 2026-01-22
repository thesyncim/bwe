// Package internal provides internal utilities for the bwe package.
package internal

import "time"

// Clock is an interface for obtaining monotonic time.
// This abstraction allows for deterministic testing of time-dependent code.
type Clock interface {
	// Now returns the current time. Implementations must return
	// monotonically increasing time values.
	Now() time.Time
}

// MonotonicClock is a Clock implementation that uses the system's monotonic clock.
// In Go, time.Now() includes monotonic clock readings, making it safe for
// measuring elapsed time without wall-clock adjustments.
type MonotonicClock struct{}

// Now returns the current system time with monotonic clock reading.
func (MonotonicClock) Now() time.Time {
	return time.Now()
}

// MockClock is a Clock implementation for testing that allows manual control
// of time progression. It is not safe for concurrent use.
type MockClock struct {
	current time.Time
}

// NewMockClock creates a new MockClock initialized to the given time.
// If t is zero, it initializes to a reasonable default start time.
func NewMockClock(t time.Time) *MockClock {
	if t.IsZero() {
		// Start at a reasonable time to avoid edge cases with zero time
		t = time.Unix(1000000000, 0) // 2001-09-09
	}
	return &MockClock{current: t}
}

// Now returns the mock clock's current time.
func (m *MockClock) Now() time.Time {
	return m.current
}

// Advance moves the clock forward by the given duration.
// Panics if d is negative to maintain monotonicity.
func (m *MockClock) Advance(d time.Duration) {
	if d < 0 {
		panic("MockClock.Advance: duration must be non-negative")
	}
	m.current = m.current.Add(d)
}

// Set sets the clock to the given time.
// This should only be used for initialization; prefer Advance for tests.
func (m *MockClock) Set(t time.Time) {
	m.current = t
}
