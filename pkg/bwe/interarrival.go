package bwe

import "time"

// DefaultBurstThreshold is the default time threshold for grouping packets
// into bursts. Packets arriving within this window are considered part of
// the same burst (typically a single video frame).
const DefaultBurstThreshold = 5 * time.Millisecond

// PacketGroup represents a group of packets that arrived in a burst.
// Video frames typically arrive as multiple packets in quick succession.
// Grouping them reduces noise in delay variation measurements.
type PacketGroup struct {
	// FirstSendTime is the abs-send-time of the first packet in the group.
	FirstSendTime uint32

	// LastSendTime is the abs-send-time of the last packet in the group.
	// This is used for computing inter-group send delta.
	LastSendTime uint32

	// FirstArriveTime is the arrival time of the first packet (monotonic clock).
	FirstArriveTime time.Time

	// LastArriveTime is the arrival time of the last packet (monotonic clock).
	// This is used for computing inter-group receive delta.
	LastArriveTime time.Time

	// Size is the total bytes of all packets in the group.
	Size int

	// NumPackets is the count of packets in the group.
	NumPackets int
}

// InterArrivalCalculator computes delay variation between packet groups.
// It groups packets arriving within a burst threshold and computes the
// delay variation d(i) = (receive_delta) - (send_delta) between consecutive groups.
//
// Positive delay variation indicates queue building (congestion).
// Negative delay variation indicates queue draining (underutilization).
type InterArrivalCalculator struct {
	// burstThreshold is the maximum inter-arrival time for packets
	// to be considered part of the same burst.
	burstThreshold time.Duration

	// currentGroup is the group currently being accumulated.
	currentGroup *PacketGroup

	// previousGroup is the last completed group, used for inter-group calculations.
	previousGroup *PacketGroup
}

// NewInterArrivalCalculator creates a new InterArrivalCalculator with the
// specified burst threshold. If burstThreshold is <= 0, DefaultBurstThreshold (5ms)
// is used.
func NewInterArrivalCalculator(burstThreshold time.Duration) *InterArrivalCalculator {
	if burstThreshold <= 0 {
		burstThreshold = DefaultBurstThreshold
	}
	return &InterArrivalCalculator{
		burstThreshold: burstThreshold,
		currentGroup:   nil,
		previousGroup:  nil,
	}
}

// BelongsToBurst returns true if the packet should be added to the current group
// (burst), based on the arrival time difference from the last packet in the group.
func (c *InterArrivalCalculator) BelongsToBurst(pkt PacketInfo) bool {
	if c.currentGroup == nil {
		return false
	}
	arrivalDelta := pkt.ArrivalTime.Sub(c.currentGroup.LastArriveTime)
	return arrivalDelta <= c.burstThreshold
}

// AddPacket processes a received packet and returns the delay variation
// if a new inter-group measurement is available.
//
// Returns:
//   - delayVariation: the computed delay variation (only valid if hasResult is true)
//   - hasResult: true if a new delay variation measurement is available
//
// The delay variation formula is: d(i) = t(i) - t(i-1) - (T(i) - T(i-1))
// where t is receive time and T is send time. Positive values indicate
// increasing delay (queue building), negative values indicate decreasing
// delay (queue draining).
func (c *InterArrivalCalculator) AddPacket(pkt PacketInfo) (delayVariation time.Duration, hasResult bool) {
	if c.BelongsToBurst(pkt) {
		// Add to current group
		c.currentGroup.LastSendTime = pkt.SendTime
		c.currentGroup.LastArriveTime = pkt.ArrivalTime
		c.currentGroup.Size += pkt.Size
		c.currentGroup.NumPackets++
		return 0, false
	}

	// New group needed
	// First, move current to previous (if exists)
	if c.currentGroup != nil {
		c.previousGroup = c.currentGroup
	}

	// Create new current group from this packet
	c.currentGroup = &PacketGroup{
		FirstSendTime:   pkt.SendTime,
		LastSendTime:    pkt.SendTime,
		FirstArriveTime: pkt.ArrivalTime,
		LastArriveTime:  pkt.ArrivalTime,
		Size:            pkt.Size,
		NumPackets:      1,
	}

	// Compute delay variation if we have a previous group
	if c.previousGroup != nil {
		return c.computeDelayVariation(), true
	}

	return 0, false
}

// computeDelayVariation calculates the delay variation between the previous
// and current groups using their last packet timestamps.
func (c *InterArrivalCalculator) computeDelayVariation() time.Duration {
	// Receive delta: difference in arrival times between groups
	receiveDelta := c.currentGroup.LastArriveTime.Sub(c.previousGroup.LastArriveTime)

	// Send delta: difference in send times between groups (handles wraparound)
	sendDelta := UnwrapAbsSendTimeDuration(c.previousGroup.LastSendTime, c.currentGroup.LastSendTime)

	// Delay variation = receive delta - send delta
	// Positive = queue building (packets arriving later than expected)
	// Negative = queue draining (packets arriving earlier than expected)
	return receiveDelta - sendDelta
}

// Reset clears the calculator state. Call this when the stream resets,
// after a large gap in packets, or when switching streams.
func (c *InterArrivalCalculator) Reset() {
	c.currentGroup = nil
	c.previousGroup = nil
}

// CurrentGroup returns the current packet group being accumulated.
// Returns nil if no group is being accumulated.
func (c *InterArrivalCalculator) CurrentGroup() *PacketGroup {
	return c.currentGroup
}

// PreviousGroup returns the last completed packet group.
// Returns nil if no group has been completed yet.
func (c *InterArrivalCalculator) PreviousGroup() *PacketGroup {
	return c.previousGroup
}

// BurstThreshold returns the configured burst threshold duration.
func (c *InterArrivalCalculator) BurstThreshold() time.Duration {
	return c.burstThreshold
}
