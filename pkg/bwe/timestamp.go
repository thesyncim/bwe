package bwe

import (
	"time"
)

// AbsSendTimeToDuration converts a 24-bit abs-send-time value to a time.Duration.
// The value is interpreted as seconds using 6.18 fixed-point format.
//
// Example: value 262144 (1 << 18) equals exactly 1 second.
func AbsSendTimeToDuration(value uint32) time.Duration {
	// Convert from 6.18 fixed-point to seconds, then to Duration
	seconds := float64(value) * AbsSendTimeResolution
	return time.Duration(seconds * float64(time.Second))
}

// UnwrapAbsSendTime computes the signed delta between two abs-send-time values,
// correctly handling wraparound at the 64-second boundary.
//
// The abs-send-time field is 24 bits and wraps every 64 seconds. This function
// uses half-range comparison to determine if the timestamp has wrapped:
//   - If the raw difference is greater than half the range (>32 seconds forward),
//     it's interpreted as a backward jump (the value wrapped).
//   - If the raw difference is less than negative half the range (<-32 seconds),
//     it's interpreted as a forward jump across the wrap boundary.
//
// Returns the signed delta in abs-send-time units (not seconds).
func UnwrapAbsSendTime(prev, curr uint32) int64 {
	// Compute raw signed difference
	diff := int32(curr) - int32(prev)

	// Half-range comparison for wraparound detection
	// AbsSendTimeMax/2 = 8388608 units = 32 seconds
	halfRange := int32(AbsSendTimeMax / 2)

	if diff > halfRange {
		// Apparent forward jump > 32s means we actually went backward across wrap
		diff -= int32(AbsSendTimeMax)
	} else if diff < -halfRange {
		// Apparent backward jump > 32s means we actually went forward across wrap
		diff += int32(AbsSendTimeMax)
	}

	return int64(diff)
}

// UnwrapAbsSendTimeDuration computes the time delta between two abs-send-time
// values, correctly handling wraparound, and returns the result as a Duration.
//
// This is a convenience function combining UnwrapAbsSendTime and duration conversion.
func UnwrapAbsSendTimeDuration(prev, curr uint32) time.Duration {
	delta := UnwrapAbsSendTime(prev, curr)
	seconds := float64(delta) * AbsSendTimeResolution
	return time.Duration(seconds * float64(time.Second))
}

// =============================================================================
// Abs-Capture-Time (64-bit UQ32.32 format)
// =============================================================================

// AbsCaptureTimeResolution is the time resolution of one abs-capture-time unit.
// The UQ32.32 format has 32 bits for the fractional part: 1/2^32 seconds (~233 picoseconds).
const AbsCaptureTimeResolution = 1.0 / (1 << 32) // ~2.33e-10 seconds per unit

// AbsCaptureTimeToDuration converts a 64-bit abs-capture-time value to a time.Duration.
// The value is interpreted using UQ32.32 format: upper 32 bits are seconds,
// lower 32 bits are fractions of a second.
//
// Example: value 0x0000000100000000 (1 << 32) equals exactly 1 second.
func AbsCaptureTimeToDuration(value uint64) time.Duration {
	// Split into seconds (upper 32 bits) and fraction (lower 32 bits)
	seconds := value >> 32
	fraction := value & 0xFFFFFFFF

	// Convert seconds directly, then add fractional part
	// Fractional part: (fraction / 2^32) * 1 second
	fractionDuration := time.Duration(float64(fraction) * AbsCaptureTimeResolution * float64(time.Second))

	return time.Duration(seconds)*time.Second + fractionDuration
}

// UnwrapAbsCaptureTime computes the signed delta between two abs-capture-time values.
//
// Unlike abs-send-time, the 64-bit abs-capture-time has a range of ~136 years,
// so wraparound within any practical session is not a concern. The function
// simply computes the signed difference between the two values.
//
// Returns the signed delta in abs-capture-time units (not seconds).
func UnwrapAbsCaptureTime(prev, curr uint64) int64 {
	// For 64-bit timestamps, the range is so large (~136 years) that
	// simple signed subtraction is sufficient for any practical use.
	return int64(curr) - int64(prev)
}

// UnwrapAbsCaptureTimeDuration computes the time delta between two abs-capture-time
// values and returns the result as a Duration.
//
// This is a convenience function combining UnwrapAbsCaptureTime and duration conversion.
func UnwrapAbsCaptureTimeDuration(prev, curr uint64) time.Duration {
	delta := UnwrapAbsCaptureTime(prev, curr)
	seconds := float64(delta) * AbsCaptureTimeResolution
	return time.Duration(seconds * float64(time.Second))
}
