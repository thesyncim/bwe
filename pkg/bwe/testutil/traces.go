// Package testutil provides testing utilities for the bwe package.
// It includes synthetic packet trace generators for testing various network conditions.
//
// Note: This package is designed for external test usage. For internal bwe tests,
// equivalent trace generators are defined directly in the test files to avoid import cycles.
package testutil

import (
	"time"

	"bwe/pkg/bwe/internal"
)

// PacketInfo mirrors bwe.PacketInfo for trace generation without import cycle.
// Use this struct with trace generators, then convert to bwe.PacketInfo in tests.
type PacketInfo struct {
	ArrivalTime time.Time
	SendTime    uint32
	Size        int
	SSRC        uint32
}

// Constants mirroring bwe package constants.
const (
	// AbsSendTimeMax is the maximum value of the 24-bit abs-send-time field.
	AbsSendTimeMax = 1 << 24 // 16777216
)

// StableNetworkTrace generates packets with constant delay (no congestion).
// Packets arrive at the same rate they were sent - no queue building or draining.
//
// Parameters:
//   - clock: MockClock for deterministic time control
//   - count: Number of packets to generate
//   - intervalMs: Inter-packet interval in milliseconds
//
// Returns a slice of PacketInfo simulating a stable network.
func StableNetworkTrace(clock *internal.MockClock, count int, intervalMs int) []PacketInfo {
	packets := make([]PacketInfo, count)
	sendTime := uint32(0)

	for i := 0; i < count; i++ {
		packets[i] = PacketInfo{
			ArrivalTime: clock.Now(),
			SendTime:    sendTime,
			Size:        1200,
			SSRC:        0x12345678,
		}
		// Both send and receive advance by same interval
		// abs-send-time units: ~262 units per ms (262144 units / 1000 ms)
		sendTime += uint32(intervalMs * 262)
		clock.Advance(time.Duration(intervalMs) * time.Millisecond)
	}
	return packets
}

// CongestingNetworkTrace generates packets where receive delay increases.
// Simulates queue building: each packet arrives slightly later than expected.
// This produces positive delay variation (congestion signal).
//
// Parameters:
//   - clock: MockClock for deterministic time control
//   - count: Number of packets to generate
//   - intervalMs: Nominal inter-packet interval in milliseconds
//   - delayIncreaseMs: Additional delay per packet (queue growth rate)
//
// Returns a slice of PacketInfo simulating congestion.
func CongestingNetworkTrace(clock *internal.MockClock, count int, intervalMs int, delayIncreaseMs float64) []PacketInfo {
	packets := make([]PacketInfo, count)
	sendTime := uint32(0)

	for i := 0; i < count; i++ {
		packets[i] = PacketInfo{
			ArrivalTime: clock.Now(),
			SendTime:    sendTime,
			Size:        1200,
			SSRC:        0x12345678,
		}
		// Send time advances normally
		sendTime += uint32(intervalMs * 262)
		// Receive time advances more (queue building)
		clock.Advance(time.Duration(float64(intervalMs)+delayIncreaseMs) * time.Millisecond)
	}
	return packets
}

// DrainingNetworkTrace generates packets where receive delay decreases.
// Simulates queue draining: packets arrive faster than expected.
// This produces negative delay variation (underuse signal).
//
// Parameters:
//   - clock: MockClock for deterministic time control
//   - count: Number of packets to generate
//   - intervalMs: Nominal inter-packet interval in milliseconds
//   - delayDecreaseMs: Delay decrease per packet (queue drain rate)
//
// Returns a slice of PacketInfo simulating underuse.
func DrainingNetworkTrace(clock *internal.MockClock, count int, intervalMs int, delayDecreaseMs float64) []PacketInfo {
	packets := make([]PacketInfo, count)
	sendTime := uint32(0)

	for i := 0; i < count; i++ {
		packets[i] = PacketInfo{
			ArrivalTime: clock.Now(),
			SendTime:    sendTime,
			Size:        1200,
			SSRC:        0x12345678,
		}
		// Send time advances normally
		sendTime += uint32(intervalMs * 262)
		// Receive time advances less (queue draining)
		advanceMs := float64(intervalMs) - delayDecreaseMs
		if advanceMs < 1 {
			advanceMs = 1 // Minimum 1ms advance to maintain monotonicity
		}
		clock.Advance(time.Duration(advanceMs) * time.Millisecond)
	}
	return packets
}

// WraparoundTrace generates packets that exercise 24-bit abs-send-time wraparound.
// The abs-send-time field wraps every 64 seconds (AbsSendTimeMax = 16777216).
//
// Parameters:
//   - clock: MockClock for deterministic time control
//   - count: Number of packets to generate
//
// Returns packets spanning across the 64-second wraparound boundary.
func WraparoundTrace(clock *internal.MockClock, count int) []PacketInfo {
	// Start near max (64 second mark), generate packets across wrap
	packets := make([]PacketInfo, count)

	// Start 100 packets * 20ms = 2 seconds before wrap
	sendTime := uint32(AbsSendTimeMax - 100*20*262)

	for i := 0; i < count; i++ {
		packets[i] = PacketInfo{
			ArrivalTime: clock.Now(),
			SendTime:    sendTime,
			Size:        1200,
			SSRC:        0x12345678,
		}
		// 20ms intervals, wrap when needed
		sendTime = (sendTime + 20*262) % uint32(AbsSendTimeMax)
		clock.Advance(20 * time.Millisecond)
	}
	return packets
}

// BurstTrace generates packets in bursts that should be grouped together.
// Useful for testing burst grouping in InterArrivalCalculator.
//
// Parameters:
//   - clock: MockClock for deterministic time control
//   - burstCount: Number of bursts
//   - packetsPerBurst: Packets in each burst
//   - interBurstMs: Gap between bursts in milliseconds
//   - intraBurstMs: Gap within burst (should be < burst threshold, typically < 5ms)
//
// Returns packets organized in distinct bursts.
func BurstTrace(clock *internal.MockClock, burstCount, packetsPerBurst, interBurstMs, intraBurstMs int) []PacketInfo {
	packets := make([]PacketInfo, burstCount*packetsPerBurst)
	sendTime := uint32(0)
	idx := 0

	for b := 0; b < burstCount; b++ {
		for p := 0; p < packetsPerBurst; p++ {
			packets[idx] = PacketInfo{
				ArrivalTime: clock.Now(),
				SendTime:    sendTime,
				Size:        1200,
				SSRC:        0x12345678,
			}
			sendTime += uint32(intraBurstMs * 262)
			idx++

			if p < packetsPerBurst-1 {
				// Within burst: small interval
				clock.Advance(time.Duration(intraBurstMs) * time.Millisecond)
			}
		}
		// Between bursts: larger interval
		if b < burstCount-1 {
			clock.Advance(time.Duration(interBurstMs) * time.Millisecond)
			sendTime += uint32(interBurstMs * 262)
		}
	}
	return packets
}
