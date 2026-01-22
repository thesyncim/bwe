// Package interceptor provides a Pion WebRTC interceptor for receiver-side
// bandwidth estimation using the Google Congestion Control (GCC) algorithm.
package interceptor

import (
	"github.com/pion/interceptor"
)

// RTP header extension URIs for timing information.
// These are registered during SDP negotiation and their IDs are provided
// via StreamInfo.RTPHeaderExtensions.
const (
	// AbsSendTimeURI is the URI for the absolute send time extension (3-byte, 24-bit).
	// Format: 6.18 fixed point representing seconds since arbitrary epoch.
	// Wraps every ~64 seconds.
	AbsSendTimeURI = "http://www.webrtc.org/experiments/rtp-hdrext/abs-send-time"

	// AbsCaptureTimeURI is the URI for the absolute capture time extension (8-byte, 64-bit).
	// Contains the NTP timestamp when the frame was captured.
	AbsCaptureTimeURI = "http://www.webrtc.org/experiments/rtp-hdrext/abs-capture-time"
)

// FindExtensionID searches for an extension with the given URI in the list
// of negotiated RTP header extensions and returns its ID.
//
// Returns 0 if the extension is not found. Extension ID 0 is invalid per RFC 5285,
// so callers should treat a return value of 0 as "extension not available" and
// handle gracefully (e.g., skip timing-based processing for packets).
func FindExtensionID(exts []interceptor.RTPHeaderExtension, uri string) uint8 {
	for _, ext := range exts {
		if ext.URI == uri {
			return uint8(ext.ID)
		}
	}
	return 0
}

// FindAbsSendTimeID is a convenience function that searches for the abs-send-time
// extension ID in the list of negotiated extensions.
//
// Returns 0 if abs-send-time was not negotiated.
func FindAbsSendTimeID(exts []interceptor.RTPHeaderExtension) uint8 {
	return FindExtensionID(exts, AbsSendTimeURI)
}

// FindAbsCaptureTimeID is a convenience function that searches for the abs-capture-time
// extension ID in the list of negotiated extensions.
//
// Returns 0 if abs-capture-time was not negotiated.
func FindAbsCaptureTimeID(exts []interceptor.RTPHeaderExtension) uint8 {
	return FindExtensionID(exts, AbsCaptureTimeURI)
}
