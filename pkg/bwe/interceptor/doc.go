// Package interceptor provides a Pion WebRTC interceptor for receiver-side
// bandwidth estimation using Google Congestion Control (GCC).
//
// This interceptor observes incoming RTP packets, extracts timing information
// from abs-send-time or abs-capture-time extensions, and generates REMB
// (Receiver Estimated Maximum Bitrate) RTCP feedback packets.
//
// # Quick Start
//
// Register the interceptor factory with your Pion WebRTC API:
//
//	import (
//	    "github.com/pion/interceptor"
//	    "github.com/pion/webrtc/v4"
//	    bweint "github.com/thesyncim/bwe/pkg/bwe/interceptor"
//	)
//
//	func setupPeerConnection() (*webrtc.PeerConnection, error) {
//	    // Create media engine with abs-send-time extension
//	    m := &webrtc.MediaEngine{}
//	    if err := m.RegisterDefaultCodecs(); err != nil {
//	        return nil, err
//	    }
//
//	    // Create interceptor registry
//	    i := &interceptor.Registry{}
//
//	    // Register BWE interceptor
//	    bweFactory, err := bweint.NewBWEInterceptorFactory()
//	    if err != nil {
//	        return nil, err
//	    }
//	    i.Add(bweFactory)
//
//	    // Create API
//	    api := webrtc.NewAPI(
//	        webrtc.WithMediaEngine(m),
//	        webrtc.WithInterceptorRegistry(i),
//	    )
//
//	    return api.NewPeerConnection(webrtc.Configuration{})
//	}
//
// # Configuration
//
// The factory accepts options to customize behavior:
//
//	factory, err := bweint.NewBWEInterceptorFactory(
//	    bweint.WithInitialBitrate(500000),    // Start at 500 kbps
//	    bweint.WithMinBitrate(50000),         // Never go below 50 kbps
//	    bweint.WithMaxBitrate(10000000),      // Cap at 10 Mbps
//	    bweint.WithFactoryREMBInterval(500*time.Millisecond), // Send REMB twice per second
//	)
//
// # How It Works
//
// 1. When a remote stream is bound (BindRemoteStream), the interceptor extracts
// extension IDs for abs-send-time and abs-capture-time from the SDP negotiation.
//
// 2. For each incoming RTP packet, the interceptor parses the timing extension,
// computes inter-arrival delay variations, and updates the bandwidth estimate.
//
// 3. When the RTCP writer is bound (BindRTCPWriter), the interceptor starts a
// background goroutine that sends REMB packets at configured intervals.
//
// 4. Inactive streams (no packets for 2 seconds) are automatically cleaned up.
//
// # Requirements
//
// The sender must include abs-send-time or abs-capture-time RTP header extensions.
// Register the appropriate extension with your MediaEngine to enable SDP negotiation.
package interceptor
