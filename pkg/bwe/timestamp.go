package bwe

import (
	"errors"
	"time"
)

// ErrInvalidAbsSendTime is returned when the input data is too short to parse.
var ErrInvalidAbsSendTime = errors.New("bwe: invalid abs-send-time data, need at least 3 bytes")

// ParseAbsSendTime parses a 24-bit abs-send-time value from a 3-byte big-endian
// representation. This is the format used in the RTP header extension.
//
// The abs-send-time extension uses 24 bits in 6.18 fixed-point format,
// representing NTP time modulo 64 seconds.
func ParseAbsSendTime(data []byte) (uint32, error) {
	if len(data) < 3 {
		return 0, ErrInvalidAbsSendTime
	}
	return (uint32(data[0]) << 16) | (uint32(data[1]) << 8) | uint32(data[2]), nil
}

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
