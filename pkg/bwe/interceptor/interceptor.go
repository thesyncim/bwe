// Package interceptor provides a Pion WebRTC interceptor for receiver-side
// bandwidth estimation using the Google Congestion Control (GCC) algorithm.
package interceptor

import (
	"sync"
	"sync/atomic"
	"time"

	"github.com/pion/interceptor"
	"github.com/pion/rtp"

	"bwe/pkg/bwe"
)

const (
	// streamTimeout is how long to keep tracking an inactive stream.
	// Streams with no packets for this duration are removed.
	streamTimeout = 2 * time.Second
)

// BWEInterceptor is a Pion interceptor that performs receiver-side bandwidth
// estimation using the GCC algorithm. It observes incoming RTP packets,
// extracts timing information from header extensions, and feeds them to
// the BandwidthEstimator.
//
// Usage:
//
//	estimator := bwe.NewBandwidthEstimator(bwe.DefaultBandwidthEstimatorConfig(), nil)
//	interceptor := NewBWEInterceptor(estimator)
//	// Add to interceptor registry...
type BWEInterceptor struct {
	interceptor.NoOp // Embed for interface compliance

	estimator *bwe.BandwidthEstimator
	streams   sync.Map // SSRC (uint32) -> *streamState

	// Extension IDs (atomic for concurrent access)
	absExtID     atomic.Uint32
	captureExtID atomic.Uint32

	// RTCP writer and REMB scheduling (will be set in Plan 03)
	mu           sync.Mutex
	rtcpWriter   interceptor.RTCPWriter
	rembInterval time.Duration
	senderSSRC   uint32

	// Lifecycle
	closed chan struct{}
	wg     sync.WaitGroup
}

// InterceptorOption is a functional option for configuring BWEInterceptor.
type InterceptorOption func(*BWEInterceptor)

// WithREMBInterval sets the interval for sending REMB packets.
// Default is 1 second (1Hz).
func WithREMBInterval(d time.Duration) InterceptorOption {
	return func(i *BWEInterceptor) {
		i.rembInterval = d
	}
}

// WithSenderSSRC sets the sender SSRC to use in REMB packets.
// This is typically the SSRC of the local receiver's RTCP packets.
func WithSenderSSRC(ssrc uint32) InterceptorOption {
	return func(i *BWEInterceptor) {
		i.senderSSRC = ssrc
	}
}

// NewBWEInterceptor creates a new bandwidth estimation interceptor.
//
// The estimator parameter is the core BandwidthEstimator from the bwe package
// that performs the actual GCC algorithm calculations.
//
// Options can be provided to customize behavior:
//   - WithREMBInterval: Set REMB sending interval (default 1s)
//   - WithSenderSSRC: Set sender SSRC for REMB packets
func NewBWEInterceptor(estimator *bwe.BandwidthEstimator, opts ...InterceptorOption) *BWEInterceptor {
	i := &BWEInterceptor{
		estimator:    estimator,
		closed:       make(chan struct{}),
		rembInterval: time.Second, // default 1Hz
	}
	for _, opt := range opts {
		opt(i)
	}
	return i
}

// Close shuts down the interceptor and releases resources.
func (i *BWEInterceptor) Close() error {
	close(i.closed)
	i.wg.Wait()
	return nil
}

// BindRemoteStream is called by Pion when a new remote stream is detected.
// It extracts RTP header extension IDs and wraps the reader to observe packets.
func (i *BWEInterceptor) BindRemoteStream(info *interceptor.StreamInfo, reader interceptor.RTPReader) interceptor.RTPReader {
	// Extract extension IDs (first stream to provide them wins)
	if absID := FindAbsSendTimeID(info.RTPHeaderExtensions); absID != 0 {
		i.absExtID.CompareAndSwap(0, uint32(absID))
	}
	if captureID := FindAbsCaptureTimeID(info.RTPHeaderExtensions); captureID != 0 {
		i.captureExtID.CompareAndSwap(0, uint32(captureID))
	}

	// Track stream
	state := newStreamState(info.SSRC)
	i.streams.Store(info.SSRC, state)

	// Return observing reader
	return interceptor.RTPReaderFunc(func(b []byte, a interceptor.Attributes) (int, interceptor.Attributes, error) {
		n, a, err := reader.Read(b, a)
		if err == nil && n > 0 {
			i.processRTP(b[:n], info.SSRC)
		}
		return n, a, err
	})
}

// UnbindRemoteStream is called by Pion when a remote stream is removed.
func (i *BWEInterceptor) UnbindRemoteStream(info *interceptor.StreamInfo) {
	i.streams.Delete(info.SSRC)
}

// processRTP parses an RTP packet and feeds timing information to the estimator.
func (i *BWEInterceptor) processRTP(raw []byte, ssrc uint32) {
	// Parse RTP header
	var header rtp.Header
	if _, err := header.Unmarshal(raw); err != nil {
		return // Invalid RTP, skip
	}

	now := time.Now()

	// Update stream state
	if state, ok := i.streams.Load(ssrc); ok {
		state.(*streamState).UpdateLastPacket(now)
	}

	// Get extension IDs
	absID := uint8(i.absExtID.Load())
	captureID := uint8(i.captureExtID.Load())

	// Try abs-send-time first (preferred, 3 bytes)
	var sendTime uint32
	if absID != 0 {
		if ext := header.GetExtension(absID); len(ext) >= 3 {
			sendTime, _ = bwe.ParseAbsSendTime(ext)
		}
	}

	// Fallback to abs-capture-time (8 bytes, convert to abs-send-time scale)
	if sendTime == 0 && captureID != 0 {
		if ext := header.GetExtension(captureID); len(ext) >= 8 {
			captureTime, err := bwe.ParseAbsCaptureTime(ext)
			if err == nil {
				// Convert 64-bit UQ32.32 to 24-bit 6.18 fixed point
				// AbsCaptureTime: upper 32 bits = seconds, lower 32 bits = fraction
				// We need seconds (6 bits) + fraction (18 bits) = 24 bits total
				// Extract: (seconds mod 64) << 18 | (fraction >> 14)
				seconds := (captureTime >> 32) & 0x3F   // 6 bits of seconds (mod 64)
				fraction := (captureTime >> 14) & 0x3FFFF // 18 bits of fraction
				sendTime = uint32((seconds << 18) | fraction)
			}
		}
	}

	// No timing extension found, skip packet
	if sendTime == 0 {
		return
	}

	// Feed to estimator
	pkt := bwe.PacketInfo{
		ArrivalTime: now,
		SendTime:    sendTime,
		Size:        len(raw),
		SSRC:        ssrc,
	}
	i.estimator.OnPacket(pkt)
}
