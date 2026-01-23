// Package bwe implements Google Congestion Control (GCC) receiver-side
// bandwidth estimation for WebRTC.
package bwe

import (
	"sync"
	"time"

	"github.com/thesyncim/bwe/pkg/bwe/internal"
)

// BandwidthEstimatorConfig configures the complete bandwidth estimator.
type BandwidthEstimatorConfig struct {
	// DelayConfig configures the delay-based detector.
	DelayConfig DelayEstimatorConfig

	// RateStatsConfig configures incoming rate measurement.
	RateStatsConfig RateStatsConfig

	// RateControllerConfig configures the AIMD rate controller.
	RateControllerConfig RateControllerConfig
}

// DefaultBandwidthEstimatorConfig returns default configuration.
func DefaultBandwidthEstimatorConfig() BandwidthEstimatorConfig {
	return BandwidthEstimatorConfig{
		DelayConfig:          DefaultDelayEstimatorConfig(),
		RateStatsConfig:      DefaultRateStatsConfig(),
		RateControllerConfig: DefaultRateControllerConfig(),
	}
}

// BandwidthEstimator is the main entry point for bandwidth estimation.
// It combines:
//   - DelayEstimator for congestion signal detection
//   - RateStats for incoming bitrate measurement
//   - RateController for AIMD-based bandwidth estimation
//
// This is a standalone core library with NO Pion dependencies.
// BandwidthEstimator is safe for concurrent use from multiple goroutines.
type BandwidthEstimator struct {
	config         BandwidthEstimatorConfig
	clock          internal.Clock
	delayEstimator *DelayEstimator
	rateStats      *RateStats
	rateController *RateController

	// Mutex protects concurrent access to state fields below.
	// Required when multiple streams call OnPacket concurrently.
	mu sync.Mutex

	// Current state (protected by mu)
	estimate int64
	ssrcs    map[uint32]struct{} // Track seen SSRCs

	// REMB scheduling (optional, set via SetREMBScheduler)
	rembScheduler *REMBScheduler

	// Track last packet time for REMB scheduling convenience
	lastPacketTime time.Time
}

// NewBandwidthEstimator creates a new bandwidth estimator.
// If clock is nil, a default MonotonicClock is used.
func NewBandwidthEstimator(config BandwidthEstimatorConfig, clock internal.Clock) *BandwidthEstimator {
	if clock == nil {
		clock = internal.MonotonicClock{}
	}

	return &BandwidthEstimator{
		config:         config,
		clock:          clock,
		delayEstimator: NewDelayEstimator(config.DelayConfig, clock),
		rateStats:      NewRateStats(config.RateStatsConfig),
		rateController: NewRateController(config.RateControllerConfig),
		estimate:       config.RateControllerConfig.InitialBitrate,
		ssrcs:          make(map[uint32]struct{}),
	}
}

// OnPacket processes a received packet and updates the bandwidth estimate.
// This is the main entry point - call this for every received RTP packet.
//
// Parameters:
//   - pkt: Packet information (arrival time, send time, size, SSRC)
//
// Returns the current bandwidth estimate in bits per second.
// This method is safe for concurrent calls from multiple goroutines.
func (e *BandwidthEstimator) OnPacket(pkt PacketInfo) int64 {
	e.mu.Lock()
	defer e.mu.Unlock()

	// Track SSRC
	e.ssrcs[pkt.SSRC] = struct{}{}

	// Update incoming rate measurement
	e.rateStats.Update(int64(pkt.Size), pkt.ArrivalTime)

	// Get congestion signal from delay estimator
	signal := e.delayEstimator.OnPacket(pkt)

	// Get measured incoming rate
	incomingRate, ok := e.rateStats.Rate(pkt.ArrivalTime)
	if !ok {
		// Not enough data for rate measurement yet
		// Keep current estimate
		return e.estimate
	}

	// Update rate controller with signal and incoming rate
	e.estimate = e.rateController.Update(signal, incomingRate, pkt.ArrivalTime)

	// Track last packet time for REMB scheduling
	e.lastPacketTime = pkt.ArrivalTime

	return e.estimate
}

// GetEstimate returns the current bandwidth estimate in bits per second.
// Call this at any time to get the latest estimate without processing a packet.
func (e *BandwidthEstimator) GetEstimate() int64 {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.estimate
}

// GetSSRCs returns the list of SSRCs seen so far.
// This is useful for building REMB packets.
func (e *BandwidthEstimator) GetSSRCs() []uint32 {
	e.mu.Lock()
	defer e.mu.Unlock()
	result := make([]uint32, 0, len(e.ssrcs))
	for ssrc := range e.ssrcs {
		result = append(result, ssrc)
	}
	return result
}

// GetCongestionState returns the current congestion state.
func (e *BandwidthEstimator) GetCongestionState() BandwidthUsage {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.delayEstimator.State()
}

// GetRateControlState returns the current AIMD rate control state.
func (e *BandwidthEstimator) GetRateControlState() RateControlState {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.rateController.State()
}

// GetIncomingRate returns the measured incoming bitrate in bits per second.
// Returns (rate, true) if available, (0, false) otherwise.
func (e *BandwidthEstimator) GetIncomingRate() (int64, bool) {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.rateStats.Rate(e.clock.Now())
}

// Reset resets the estimator to initial state.
// Call this when switching streams or after extended silence.
func (e *BandwidthEstimator) Reset() {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.delayEstimator.Reset()
	e.rateStats.Reset()
	e.rateController.Reset()
	e.estimate = e.config.RateControllerConfig.InitialBitrate
	e.ssrcs = make(map[uint32]struct{})
	e.lastPacketTime = time.Time{}
	// Note: We don't reset the REMB scheduler here, as it's externally provided.
	// The caller can reset it separately if needed.
}

// SetREMBScheduler attaches a REMB scheduler to the estimator.
// Once attached, MaybeBuildREMB can be used to generate REMB packets.
func (e *BandwidthEstimator) SetREMBScheduler(scheduler *REMBScheduler) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.rembScheduler = scheduler
}

// MaybeBuildREMB checks if a REMB packet should be sent and builds it.
// Returns (packet, true, nil) if REMB should be sent.
// Returns (nil, false, nil) if no REMB needed now.
// Returns (nil, false, error) if encoding fails.
//
// This should be called after OnPacket() or periodically.
//
// Multi-SSRC: The REMB packet includes ALL SSRCs seen by this estimator,
// as receiver-side estimation produces ONE estimate for the entire session.
func (e *BandwidthEstimator) MaybeBuildREMB(now time.Time) ([]byte, bool, error) {
	e.mu.Lock()
	defer e.mu.Unlock()

	if e.rembScheduler == nil {
		return nil, false, nil // No scheduler, no REMB
	}

	if len(e.ssrcs) == 0 {
		return nil, false, nil // No SSRCs seen yet
	}

	// Build SSRCs slice while holding lock
	ssrcs := make([]uint32, 0, len(e.ssrcs))
	for ssrc := range e.ssrcs {
		ssrcs = append(ssrcs, ssrc)
	}

	return e.rembScheduler.MaybeSendREMB(e.estimate, ssrcs, now)
}

// GetLastPacketTime returns the arrival time of the last processed packet.
// Useful for REMB scheduling when calling MaybeBuildREMB.
func (e *BandwidthEstimator) GetLastPacketTime() time.Time {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.lastPacketTime
}
