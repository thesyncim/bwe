// Package testutil provides testing utilities for the bwe package.
// This file provides reference trace replay infrastructure for validation testing.
//
// Note: This package is designed to avoid importing bwe to prevent import cycles.
// Tests that use this package should handle the conversion to bwe.PacketInfo themselves.
package testutil

import (
	"encoding/json"
	"fmt"
	"math"
	"os"
	"time"

	"bwe/pkg/bwe/internal"
)

// TracedPacket represents a single packet in a reference trace.
// It includes both the packet data needed for replay and the expected
// reference estimate from libwebrtc (if available).
type TracedPacket struct {
	// ArrivalTimeUs is the arrival time in microseconds since trace start.
	ArrivalTimeUs int64 `json:"arrival_time_us"`

	// SendTime is the 24-bit abs-send-time value from RTP header extension.
	SendTime uint32 `json:"send_time"`

	// Size is the packet size in bytes.
	Size int `json:"size"`

	// SSRC is the synchronization source identifier.
	SSRC uint32 `json:"ssrc"`

	// ReferenceEstimate is the expected bandwidth estimate from libwebrtc.
	// A value of 0 means "unknown" or "not yet converged" (should be skipped in comparison).
	ReferenceEstimate int64 `json:"reference_estimate"`
}

// ArrivalTime converts the arrival time from microseconds to time.Time.
// Useful for initializing mock clocks at the trace start time.
func (p TracedPacket) ArrivalTime() time.Time {
	return time.Unix(0, p.ArrivalTimeUs*1000) // microseconds to nanoseconds
}

// ReferenceTrace represents a complete packet trace with reference estimates.
// Traces can be:
//   - Synthetic: Generated programmatically for testing infrastructure
//   - Captured: Extracted from Chrome RTC event logs using rtc_event_log_visualizer
type ReferenceTrace struct {
	// Name is a short identifier for the trace (e.g., "congestion_recovery").
	Name string `json:"name"`

	// Description explains what network conditions the trace represents.
	Description string `json:"description"`

	// Packets is the ordered list of packets in the trace.
	Packets []TracedPacket `json:"packets"`
}

// LoadTrace reads a reference trace from a JSON file.
// Returns an error if the file cannot be read or parsed.
//
// File format:
//
//	{
//	    "name": "trace_name",
//	    "description": "Description of network scenario",
//	    "packets": [
//	        {"arrival_time_us": 0, "send_time": 0, "size": 1200, "ssrc": 12345, "reference_estimate": 0},
//	        ...
//	    ]
//	}
func LoadTrace(path string) (*ReferenceTrace, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read trace file %s: %w", path, err)
	}

	var trace ReferenceTrace
	if err := json.Unmarshal(data, &trace); err != nil {
		return nil, fmt.Errorf("failed to parse trace file %s: %w", path, err)
	}

	return &trace, nil
}

// PacketProcessor is a function that processes a single packet and returns an estimate.
// This allows the Replay method to work without depending on the bwe package.
// The function should:
//   - Accept the traced packet data
//   - Process it through the estimator being tested
//   - Return the bandwidth estimate
type PacketProcessor func(arrivalTime time.Time, sendTime uint32, size int, ssrc uint32) int64

// Replay replays the trace through a packet processor and returns the estimates.
// The clock is advanced to match packet arrival times in the trace.
//
// Parameters:
//   - processor: A function that processes packets and returns estimates
//   - clock: A MockClock for deterministic replay
//
// Returns a slice of bandwidth estimates, one per packet.
// The slice has the same length as trace.Packets.
func (t *ReferenceTrace) Replay(processor PacketProcessor, clock *internal.MockClock) []int64 {
	estimates := make([]int64, len(t.Packets))

	// Track the start time for calculating arrival deltas
	startTime := clock.Now()
	var lastArrivalUs int64 = 0

	for i, pkt := range t.Packets {
		// Advance clock to match packet arrival time
		if pkt.ArrivalTimeUs > lastArrivalUs {
			delta := time.Duration(pkt.ArrivalTimeUs-lastArrivalUs) * time.Microsecond
			clock.Advance(delta)
		}
		lastArrivalUs = pkt.ArrivalTimeUs

		// Calculate actual arrival time
		arrivalTime := startTime.Add(time.Duration(pkt.ArrivalTimeUs) * time.Microsecond)

		// Process packet and record estimate
		estimates[i] = processor(arrivalTime, pkt.SendTime, pkt.Size, pkt.SSRC)
	}

	return estimates
}

// DivergenceResult contains the results of a divergence calculation.
type DivergenceResult struct {
	// MaxDivergence is the maximum divergence observed (percentage).
	MaxDivergence float64

	// AvgDivergence is the average divergence (percentage).
	AvgDivergence float64

	// ComparedPackets is the number of packets actually compared
	// (excluding warmup and packets without reference estimates).
	ComparedPackets int

	// TotalPackets is the total number of packets in the trace.
	TotalPackets int
}

// CalculateDivergence compares our estimates against reference values.
// It calculates both maximum and average divergence as percentages.
//
// Parameters:
//   - ourEstimates: Slice of our bandwidth estimates (from Replay)
//   - trace: The reference trace containing expected estimates
//   - warmupPackets: Number of initial packets to skip (estimator needs warmup)
//
// Packets are only compared where ReferenceEstimate > 0 (meaning we have
// expected values from libwebrtc).
//
// Divergence is calculated as: abs(our - ref) / ref * 100%
func CalculateDivergence(ourEstimates []int64, trace *ReferenceTrace, warmupPackets int) DivergenceResult {
	result := DivergenceResult{
		TotalPackets: len(trace.Packets),
	}

	if len(ourEstimates) != len(trace.Packets) {
		// Mismatch - return zero result
		return result
	}

	var totalDivergence float64
	var maxDivergence float64
	var comparedCount int

	for i := warmupPackets; i < len(trace.Packets); i++ {
		ref := trace.Packets[i].ReferenceEstimate
		if ref <= 0 {
			// Skip packets without reference estimates
			continue
		}

		our := ourEstimates[i]
		divergence := math.Abs(float64(our-ref)) / float64(ref) * 100.0

		totalDivergence += divergence
		if divergence > maxDivergence {
			maxDivergence = divergence
		}
		comparedCount++
	}

	result.MaxDivergence = maxDivergence
	result.ComparedPackets = comparedCount

	if comparedCount > 0 {
		result.AvgDivergence = totalDivergence / float64(comparedCount)
	}

	return result
}

// GenerateSyntheticTrace creates a synthetic trace for testing.
// This is useful when real reference data is not available.
//
// The trace simulates a congestion event:
//   - Phase 1: Stable network (estimates should converge to incoming rate)
//   - Phase 2: Congestion (delays increase, estimates should decrease)
//   - Phase 3: Recovery (delays decrease, estimates should increase)
//
// Parameters:
//   - packetCount: Total number of packets
//   - packetIntervalMs: Interval between packets in milliseconds
//   - packetSize: Size of each packet in bytes
//   - ssrc: SSRC to use for packets
//
// Returns a ReferenceTrace with synthetic reference estimates set.
func GenerateSyntheticTrace(packetCount int, packetIntervalMs int, packetSize int, ssrc uint32) *ReferenceTrace {
	trace := &ReferenceTrace{
		Name:        "synthetic_congestion",
		Description: "Synthetic trace with stable, congestion, and recovery phases",
		Packets:     make([]TracedPacket, packetCount),
	}

	// Calculate phase boundaries (roughly 40% stable, 30% congestion, 30% recovery)
	phase1End := packetCount * 40 / 100
	phase2End := packetCount * 70 / 100

	// Track accumulated delay for congestion simulation
	var accumulatedDelayUs int64 = 0
	const delayIncreasePerPacketUs = 500 // 0.5ms per packet during congestion
	const delayDecreasePerPacketUs = 300 // 0.3ms per packet during recovery
	warmupPackets := packetCount / 5     // First 20% are warmup (no reference estimates)

	// Calculate incoming bitrate based on packet size and interval
	// bitrate = (packetSize * 8) / (packetIntervalMs / 1000) = packetSize * 8 * 1000 / packetIntervalMs
	stableBitrate := int64(packetSize * 8 * 1000 / packetIntervalMs)
	congestionBitrate := stableBitrate * 60 / 100 // 60% of stable during congestion
	recoveryBitrate := stableBitrate * 90 / 100   // 90% of stable during recovery

	var sendTimeUs int64 = 0

	for i := 0; i < packetCount; i++ {
		// Calculate arrival time (send time + network delay + accumulated congestion delay)
		baseDelayUs := int64(10000) // 10ms base network delay
		arrivalTimeUs := sendTimeUs + baseDelayUs + accumulatedDelayUs

		// Convert sendTimeUs to abs-send-time format (24-bit, ~3.8us resolution)
		// abs-send-time uses ~262 units per millisecond
		absSendTime := uint32((sendTimeUs / 1000) * 262) % (1 << 24)

		// Determine reference estimate based on phase
		var refEstimate int64 = 0
		if i >= warmupPackets {
			if i < phase1End {
				// Stable phase - estimate should be near incoming bitrate
				refEstimate = stableBitrate
			} else if i < phase2End {
				// Congestion phase - estimate should decrease
				progress := float64(i-phase1End) / float64(phase2End-phase1End)
				refEstimate = stableBitrate - int64(float64(stableBitrate-congestionBitrate)*progress)
			} else {
				// Recovery phase - estimate should increase
				progress := float64(i-phase2End) / float64(packetCount-phase2End)
				refEstimate = congestionBitrate + int64(float64(recoveryBitrate-congestionBitrate)*progress)
			}
		}

		trace.Packets[i] = TracedPacket{
			ArrivalTimeUs:     arrivalTimeUs,
			SendTime:          absSendTime,
			Size:              packetSize,
			SSRC:              ssrc,
			ReferenceEstimate: refEstimate,
		}

		// Update send time for next packet
		sendTimeUs += int64(packetIntervalMs * 1000)

		// Update accumulated delay based on phase
		if i >= phase1End && i < phase2End {
			// Congestion: queue building
			accumulatedDelayUs += delayIncreasePerPacketUs
		} else if i >= phase2End {
			// Recovery: queue draining
			accumulatedDelayUs -= delayDecreasePerPacketUs
			if accumulatedDelayUs < 0 {
				accumulatedDelayUs = 0
			}
		}
	}

	return trace
}
