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
//
// Deprecated: Use rtp.AbsSendTimeExtension.Unmarshal() from github.com/pion/rtp instead.
// This function will be removed in v1.2. The Pion implementation is maintained upstream
// and handles validation. Example migration:
//
//	var ext rtp.AbsSendTimeExtension
//	if err := ext.Unmarshal(data); err == nil {
//	    sendTime = uint32(ext.Timestamp)
//	}
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

// =============================================================================
// Abs-Capture-Time (64-bit UQ32.32 format)
// =============================================================================

// ErrInvalidAbsCaptureTime is returned when the input data is too short to parse
// an abs-capture-time value.
var ErrInvalidAbsCaptureTime = errors.New("bwe: invalid abs-capture-time data, need at least 8 bytes")

// AbsCaptureTimeResolution is the time resolution of one abs-capture-time unit.
// The UQ32.32 format has 32 bits for the fractional part: 1/2^32 seconds (~233 picoseconds).
const AbsCaptureTimeResolution = 1.0 / (1 << 32) // ~2.33e-10 seconds per unit

// ParseAbsCaptureTime parses a 64-bit abs-capture-time value from an 8-byte
// big-endian representation. This is the UQ32.32 format where the upper 32 bits
// are seconds and the lower 32 bits are fractions of a second.
//
// The abs-capture-time extension uses 64 bits, providing ~136 years of range
// with sub-nanosecond precision.
//
// Deprecated: Use rtp.AbsCaptureTimeExtension.Unmarshal() from github.com/pion/rtp instead.
// This function will be removed in v1.2. The Pion implementation is maintained upstream
// and handles both 8-byte and 16-byte payloads (with optional clock offset). Example migration:
//
//	var ext rtp.AbsCaptureTimeExtension
//	if err := ext.Unmarshal(data); err == nil {
//	    captureTime = ext.Timestamp
//	}
func ParseAbsCaptureTime(data []byte) (uint64, error) {
	if len(data) < 8 {
		return 0, ErrInvalidAbsCaptureTime
	}
	return (uint64(data[0]) << 56) | (uint64(data[1]) << 48) | (uint64(data[2]) << 40) |
		(uint64(data[3]) << 32) | (uint64(data[4]) << 24) | (uint64(data[5]) << 16) |
		(uint64(data[6]) << 8) | uint64(data[7]), nil
}

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
