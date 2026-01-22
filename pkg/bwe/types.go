// Package bwe implements Google Congestion Control (GCC) receiver-side
// bandwidth estimation for WebRTC.
package bwe

import "time"

// BandwidthUsage represents the current bandwidth usage state as determined
// by the delay-based detector.
type BandwidthUsage int

const (
	// BwNormal indicates bandwidth usage is normal - no congestion detected.
	BwNormal BandwidthUsage = iota
	// BwUnderusing indicates the link is underutilized - can increase rate.
	BwUnderusing
	// BwOverusing indicates congestion detected - should decrease rate.
	BwOverusing
)

// String returns a string representation of the BandwidthUsage state.
func (b BandwidthUsage) String() string {
	switch b {
	case BwNormal:
		return "Normal"
	case BwUnderusing:
		return "Underusing"
	case BwOverusing:
		return "Overusing"
	default:
		return "Unknown"
	}
}

// PacketInfo contains information about a received RTP packet used for
// bandwidth estimation. This is the primary input to the delay-based detector.
type PacketInfo struct {
	// ArrivalTime is the monotonic time when the packet was received.
	// Must be obtained from a monotonic clock source (time.Now() in Go).
	ArrivalTime time.Time

	// SendTime is the 24-bit abs-send-time value from the RTP header extension.
	// This is a 6.18 fixed-point representation of NTP time modulo 64 seconds.
	SendTime uint32

	// Size is the payload size of the packet in bytes.
	Size int

	// SSRC is the synchronization source identifier for the media stream.
	SSRC uint32
}

// Constants for abs-send-time (AST) header extension parsing.
// The abs-send-time extension uses a 24-bit 6.18 fixed-point format
// representing NTP time in seconds modulo 64 seconds.
const (
	// AbsSendTimeMax is the maximum value of the 24-bit abs-send-time field.
	// Values wrap around at this point (every 64 seconds).
	AbsSendTimeMax = 1 << 24 // 16777216

	// AbsSendTimeResolution is the time resolution of one abs-send-time unit.
	// With 18 bits for the fractional part: 1/2^18 = ~3.8 microseconds.
	AbsSendTimeResolution = 1.0 / (1 << 18) // ~3.8147e-6 seconds per unit
)
