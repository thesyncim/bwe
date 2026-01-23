// Package interceptor provides a Pion WebRTC interceptor for receiver-side
// bandwidth estimation using the Google Congestion Control (GCC) algorithm.
package interceptor

import (
	"sync"
	"sync/atomic"
	"time"

	"github.com/pion/interceptor"
	"github.com/pion/rtcp"
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

	estimator     *bwe.BandwidthEstimator
	rembScheduler *bwe.REMBScheduler
	streams       sync.Map // SSRC (uint32) -> *streamState

	// Extension IDs (atomic for concurrent access)
	absExtID     atomic.Uint32
	captureExtID atomic.Uint32

	// RTCP writer and REMB scheduling
	mu           sync.Mutex
	rtcpWriter   interceptor.RTCPWriter
	rembInterval time.Duration
	senderSSRC   uint32
	onREMB       func(bitrate float32, ssrcs []uint32)

	// Lifecycle
	closed    chan struct{}
	wg        sync.WaitGroup
	startOnce sync.Once // Ensures cleanup loop starts only once
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

// WithOnREMB sets a callback that is invoked each time a REMB packet is sent.
// The callback receives the bitrate estimate and the SSRCs included in the REMB.
func WithOnREMB(fn func(bitrate float32, ssrcs []uint32)) InterceptorOption {
	return func(i *BWEInterceptor) {
		i.onREMB = fn
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

	// Create and attach REMB scheduler
	rembConfig := bwe.DefaultREMBSchedulerConfig()
	rembConfig.Interval = i.rembInterval
	rembConfig.SenderSSRC = i.senderSSRC
	i.rembScheduler = bwe.NewREMBScheduler(rembConfig)
	i.estimator.SetREMBScheduler(i.rembScheduler)

	return i
}

// Close shuts down the interceptor and releases resources.
func (i *BWEInterceptor) Close() error {
	close(i.closed)
	i.wg.Wait()
	return nil
}

// BindRTCPWriter is called by Pion when the RTCP writer is ready.
// It captures the writer for sending REMB packets and starts the REMB loop.
func (i *BWEInterceptor) BindRTCPWriter(writer interceptor.RTCPWriter) interceptor.RTCPWriter {
	i.mu.Lock()
	i.rtcpWriter = writer
	i.mu.Unlock()

	// Start REMB loop goroutine
	i.wg.Add(1)
	go i.rembLoop()

	return writer // Pass through unchanged
}

// BindRemoteStream is called by Pion when a new remote stream is detected.
// It extracts RTP header extension IDs and wraps the reader to observe packets.
func (i *BWEInterceptor) BindRemoteStream(info *interceptor.StreamInfo, reader interceptor.RTPReader) interceptor.RTPReader {
	// Start cleanup loop on first stream (only once)
	i.startOnce.Do(func() {
		i.wg.Add(1)
		go i.cleanupLoop()
	})

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
		if extData := header.GetExtension(absID); len(extData) >= 3 {
			var ext rtp.AbsSendTimeExtension // Stack allocated - CRITICAL for 0 allocs/op
			if err := ext.Unmarshal(extData); err == nil {
				sendTime = uint32(ext.Timestamp) // Cast from uint64 to uint32 (24-bit fits)
			}
		}
	}

	// Fallback to abs-capture-time (8 bytes, convert to abs-send-time scale)
	if sendTime == 0 && captureID != 0 {
		if extData := header.GetExtension(captureID); len(extData) >= 8 {
			var ext rtp.AbsCaptureTimeExtension // Stack allocated - CRITICAL for 0 allocs/op
			if err := ext.Unmarshal(extData); err == nil {
				// Convert 64-bit UQ32.32 to 24-bit 6.18 fixed point
				// AbsCaptureTime: upper 32 bits = seconds, lower 32 bits = fraction
				// We need seconds (6 bits) + fraction (18 bits) = 24 bits total
				seconds := (ext.Timestamp >> 32) & 0x3F    // 6 bits of seconds (mod 64)
				fraction := (ext.Timestamp >> 14) & 0x3FFFF // 18 bits of fraction
				sendTime = uint32((seconds << 18) | fraction)
			}
		}
	}

	// No timing extension found, skip packet
	if sendTime == 0 {
		return
	}

	// Get PacketInfo from pool
	pkt := getPacketInfo()
	pkt.ArrivalTime = now
	pkt.SendTime = sendTime
	pkt.Size = len(raw)
	pkt.SSRC = ssrc

	// Feed to estimator (OnPacket takes by value, so dereference)
	i.estimator.OnPacket(*pkt)

	// Return to pool
	putPacketInfo(pkt)
}

// rembLoop runs periodically to send REMB packets.
// It uses the configured rembInterval (default 1s).
func (i *BWEInterceptor) rembLoop() {
	defer i.wg.Done()

	ticker := time.NewTicker(i.rembInterval)
	defer ticker.Stop()

	for {
		select {
		case <-i.closed:
			return
		case now := <-ticker.C:
			i.maybeSendREMB(now)
		}
	}
}

// maybeSendREMB checks if a REMB should be sent and sends it via the RTCPWriter.
func (i *BWEInterceptor) maybeSendREMB(now time.Time) {
	// Get REMB data using estimator's scheduler
	data, shouldSend, err := i.estimator.MaybeBuildREMB(now)
	if err != nil || !shouldSend || len(data) == 0 {
		return
	}

	// Get writer under lock
	i.mu.Lock()
	writer := i.rtcpWriter
	i.mu.Unlock()

	if writer == nil {
		return // Not bound yet, skip
	}

	// Parse bytes back to RTCP packet for RTCPWriter interface
	// RTCPWriter.Write takes []rtcp.Packet, not raw bytes
	pkts, err := rtcp.Unmarshal(data)
	if err != nil {
		return // Should never happen with our own REMB bytes
	}

	// Send REMB
	_, _ = writer.Write(pkts, nil) // Ignore errors (network issues)

	// Invoke callback if set
	if i.onREMB != nil {
		if remb, ok := pkts[0].(*rtcp.ReceiverEstimatedMaximumBitrate); ok {
			i.onREMB(remb.Bitrate, remb.SSRCs)
		}
	}
}

// cleanupLoop runs periodically to remove inactive streams.
// It checks every second and removes streams that haven't received
// packets for longer than streamTimeout (2 seconds).
func (i *BWEInterceptor) cleanupLoop() {
	defer i.wg.Done()

	ticker := time.NewTicker(time.Second) // Check every second
	defer ticker.Stop()

	for {
		select {
		case <-i.closed:
			return
		case now := <-ticker.C:
			i.cleanupInactiveStreams(now)
		}
	}
}

// cleanupInactiveStreams removes streams that haven't received packets
// for longer than streamTimeout. Uses sync.Map.Range for thread-safe iteration.
func (i *BWEInterceptor) cleanupInactiveStreams(now time.Time) {
	i.streams.Range(func(key, value any) bool {
		state := value.(*streamState)
		if now.Sub(state.LastPacket()) > streamTimeout {
			i.streams.Delete(key)
		}
		return true // Continue iteration
	})
}
