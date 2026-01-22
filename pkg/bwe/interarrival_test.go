package bwe

import (
	"testing"
	"time"
)

// msToAbsSendTime converts milliseconds to abs-send-time units.
// 1ms = 2^18 / 1000 = 262.144 units
func msToAbsSendTime(ms int) uint32 {
	return uint32(float64(ms) * 262.144)
}

func TestInterArrivalCalculator_BurstGrouping(t *testing.T) {
	calc := NewInterArrivalCalculator(5 * time.Millisecond)
	baseTime := time.Now()

	// First packet - starts a new group
	pkt1 := PacketInfo{
		ArrivalTime: baseTime,
		SendTime:    1000,
		Size:        100,
	}
	_, hasResult := calc.AddPacket(pkt1)
	if hasResult {
		t.Error("First packet should not produce result")
	}

	// Verify first group started
	if calc.CurrentGroup() == nil {
		t.Fatal("Current group should not be nil after first packet")
	}
	if calc.CurrentGroup().NumPackets != 1 {
		t.Errorf("Expected 1 packet in group, got %d", calc.CurrentGroup().NumPackets)
	}

	// Second packet at +2ms - should be in same group
	pkt2 := PacketInfo{
		ArrivalTime: baseTime.Add(2 * time.Millisecond),
		SendTime:    1100,
		Size:        150,
	}
	_, hasResult = calc.AddPacket(pkt2)
	if hasResult {
		t.Error("Second packet in burst should not produce result")
	}
	if calc.CurrentGroup().NumPackets != 2 {
		t.Errorf("Expected 2 packets in group, got %d", calc.CurrentGroup().NumPackets)
	}

	// Third packet at +4ms (2ms after second) - should still be in same group
	pkt3 := PacketInfo{
		ArrivalTime: baseTime.Add(4 * time.Millisecond),
		SendTime:    1200,
		Size:        200,
	}
	_, hasResult = calc.AddPacket(pkt3)
	if hasResult {
		t.Error("Third packet in burst should not produce result")
	}
	if calc.CurrentGroup().NumPackets != 3 {
		t.Errorf("Expected 3 packets in group, got %d", calc.CurrentGroup().NumPackets)
	}
	if calc.CurrentGroup().Size != 450 {
		t.Errorf("Expected 450 bytes in group, got %d", calc.CurrentGroup().Size)
	}

	// Fourth packet at +14ms (10ms after third) - should start new group
	pkt4 := PacketInfo{
		ArrivalTime: baseTime.Add(14 * time.Millisecond),
		SendTime:    1300,
		Size:        120,
	}
	_, hasResult = calc.AddPacket(pkt4)
	// Now we should have a result (previous group completed)
	if !hasResult {
		t.Error("Fourth packet should produce result (new group started)")
	}

	// Verify groups
	if calc.PreviousGroup().NumPackets != 3 {
		t.Errorf("Previous group should have 3 packets, got %d", calc.PreviousGroup().NumPackets)
	}
	if calc.CurrentGroup().NumPackets != 1 {
		t.Errorf("Current group should have 1 packet, got %d", calc.CurrentGroup().NumPackets)
	}
}

func TestInterArrivalCalculator_DelayVariation_Stable(t *testing.T) {
	calc := NewInterArrivalCalculator(5 * time.Millisecond)
	baseTime := time.Now()

	// Group 1: arrive at t0, send_time=1000
	pkt1 := PacketInfo{
		ArrivalTime: baseTime,
		SendTime:    1000,
		Size:        100,
	}
	calc.AddPacket(pkt1)

	// Group 2: arrive at t0+100ms, send_time matches 100ms later
	// 100ms in abs-send-time units = 26214 (approximately)
	sendTimeDelta := msToAbsSendTime(100) // ~26214 units
	pkt2 := PacketInfo{
		ArrivalTime: baseTime.Add(100 * time.Millisecond),
		SendTime:    1000 + sendTimeDelta,
		Size:        100,
	}
	delayVariation, hasResult := calc.AddPacket(pkt2)

	if !hasResult {
		t.Fatal("Expected result from second group")
	}

	// Delay variation should be approximately 0 (stable network)
	// Allow some tolerance due to float conversion
	tolerance := 100 * time.Microsecond
	if delayVariation > tolerance || delayVariation < -tolerance {
		t.Errorf("Expected delay variation ~0 for stable network, got %v", delayVariation)
	}
}

func TestInterArrivalCalculator_DelayVariation_QueueBuilding(t *testing.T) {
	calc := NewInterArrivalCalculator(5 * time.Millisecond)
	baseTime := time.Now()

	// Group 1: arrive at t0, send_time=1000
	pkt1 := PacketInfo{
		ArrivalTime: baseTime,
		SendTime:    1000,
		Size:        100,
	}
	calc.AddPacket(pkt1)

	// Group 2: arrive at t0+120ms, but sender sent 100ms apart
	// This simulates 20ms of queue build-up
	sendTimeDelta := msToAbsSendTime(100) // sender interval = 100ms
	pkt2 := PacketInfo{
		ArrivalTime: baseTime.Add(120 * time.Millisecond), // receiver sees 120ms gap
		SendTime:    1000 + sendTimeDelta,
		Size:        100,
	}
	delayVariation, hasResult := calc.AddPacket(pkt2)

	if !hasResult {
		t.Fatal("Expected result from second group")
	}

	// Delay variation should be ~+20ms (positive = queue building)
	expected := 20 * time.Millisecond
	tolerance := 500 * time.Microsecond
	if delayVariation < expected-tolerance || delayVariation > expected+tolerance {
		t.Errorf("Expected delay variation ~+20ms for queue building, got %v", delayVariation)
	}
}

func TestInterArrivalCalculator_DelayVariation_QueueDraining(t *testing.T) {
	calc := NewInterArrivalCalculator(5 * time.Millisecond)
	baseTime := time.Now()

	// Group 1: arrive at t0, send_time=1000
	pkt1 := PacketInfo{
		ArrivalTime: baseTime,
		SendTime:    1000,
		Size:        100,
	}
	calc.AddPacket(pkt1)

	// Group 2: arrive at t0+80ms, but sender sent 100ms apart
	// This simulates 20ms of queue draining
	sendTimeDelta := msToAbsSendTime(100) // sender interval = 100ms
	pkt2 := PacketInfo{
		ArrivalTime: baseTime.Add(80 * time.Millisecond), // receiver sees 80ms gap
		SendTime:    1000 + sendTimeDelta,
		Size:        100,
	}
	delayVariation, hasResult := calc.AddPacket(pkt2)

	if !hasResult {
		t.Fatal("Expected result from second group")
	}

	// Delay variation should be ~-20ms (negative = queue draining)
	expected := -20 * time.Millisecond
	tolerance := 500 * time.Microsecond
	if delayVariation < expected-tolerance || delayVariation > expected+tolerance {
		t.Errorf("Expected delay variation ~-20ms for queue draining, got %v", delayVariation)
	}
}

func TestInterArrivalCalculator_Wraparound(t *testing.T) {
	calc := NewInterArrivalCalculator(5 * time.Millisecond)
	baseTime := time.Now()

	// Group 1: send_time near max (16777000)
	pkt1 := PacketInfo{
		ArrivalTime: baseTime,
		SendTime:    16777000,
		Size:        100,
	}
	calc.AddPacket(pkt1)

	// Group 2: send_time wrapped around
	// If sender sent 100ms apart, the wrapped value would be:
	// 16777000 + 26214 = 16803214, which wraps to ~26000
	sendTimeDelta := msToAbsSendTime(100)
	wrappedSendTime := (16777000 + sendTimeDelta) % AbsSendTimeMax

	pkt2 := PacketInfo{
		ArrivalTime: baseTime.Add(100 * time.Millisecond),
		SendTime:    wrappedSendTime,
		Size:        100,
	}
	delayVariation, hasResult := calc.AddPacket(pkt2)

	if !hasResult {
		t.Fatal("Expected result from second group")
	}

	// Delay variation should be ~0 (stable network despite wraparound)
	// Not a huge number like 63 seconds!
	if delayVariation > time.Second || delayVariation < -time.Second {
		t.Errorf("Wraparound handling failed: got unreasonable delay variation %v", delayVariation)
	}

	// Should be approximately 0
	tolerance := time.Millisecond
	if delayVariation > tolerance || delayVariation < -tolerance {
		t.Errorf("Expected delay variation ~0 with wraparound handling, got %v", delayVariation)
	}
}

func TestInterArrivalCalculator_ConfigurableBurstThreshold(t *testing.T) {
	// Create calculator with 10ms threshold
	calc := NewInterArrivalCalculator(10 * time.Millisecond)
	baseTime := time.Now()

	// First packet
	pkt1 := PacketInfo{
		ArrivalTime: baseTime,
		SendTime:    1000,
		Size:        100,
	}
	calc.AddPacket(pkt1)

	// Second packet at +8ms - should be in same group (within 10ms threshold)
	pkt2 := PacketInfo{
		ArrivalTime: baseTime.Add(8 * time.Millisecond),
		SendTime:    1100,
		Size:        100,
	}
	_, hasResult := calc.AddPacket(pkt2)

	if hasResult {
		t.Error("Packet 8ms apart should be in same group with 10ms threshold")
	}
	if calc.CurrentGroup().NumPackets != 2 {
		t.Errorf("Expected 2 packets in group, got %d", calc.CurrentGroup().NumPackets)
	}

	// Third packet at +12ms (4ms after second) - still in same group
	pkt3 := PacketInfo{
		ArrivalTime: baseTime.Add(12 * time.Millisecond),
		SendTime:    1200,
		Size:        100,
	}
	_, hasResult = calc.AddPacket(pkt3)

	if hasResult {
		t.Error("Packet 4ms apart should be in same group with 10ms threshold")
	}
	if calc.CurrentGroup().NumPackets != 3 {
		t.Errorf("Expected 3 packets in group, got %d", calc.CurrentGroup().NumPackets)
	}

	// Fourth packet at +25ms (13ms after third) - should start new group
	pkt4 := PacketInfo{
		ArrivalTime: baseTime.Add(25 * time.Millisecond),
		SendTime:    1300,
		Size:        100,
	}
	_, hasResult = calc.AddPacket(pkt4)

	if !hasResult {
		t.Error("Packet 13ms apart should start new group with 10ms threshold")
	}
}

func TestInterArrivalCalculator_DefaultBurstThreshold(t *testing.T) {
	// Test with zero threshold - should use default 5ms
	calc := NewInterArrivalCalculator(0)
	if calc.BurstThreshold() != DefaultBurstThreshold {
		t.Errorf("Expected default threshold %v, got %v", DefaultBurstThreshold, calc.BurstThreshold())
	}

	// Test with negative threshold - should use default 5ms
	calc = NewInterArrivalCalculator(-1 * time.Millisecond)
	if calc.BurstThreshold() != DefaultBurstThreshold {
		t.Errorf("Expected default threshold %v, got %v", DefaultBurstThreshold, calc.BurstThreshold())
	}
}

func TestInterArrivalCalculator_Reset(t *testing.T) {
	calc := NewInterArrivalCalculator(5 * time.Millisecond)
	baseTime := time.Now()

	// Add some packets
	pkt1 := PacketInfo{
		ArrivalTime: baseTime,
		SendTime:    1000,
		Size:        100,
	}
	calc.AddPacket(pkt1)

	pkt2 := PacketInfo{
		ArrivalTime: baseTime.Add(100 * time.Millisecond),
		SendTime:    1000 + msToAbsSendTime(100),
		Size:        100,
	}
	calc.AddPacket(pkt2)

	// Verify state exists
	if calc.CurrentGroup() == nil || calc.PreviousGroup() == nil {
		t.Fatal("Expected both groups to exist before reset")
	}

	// Reset
	calc.Reset()

	// Verify state cleared
	if calc.CurrentGroup() != nil {
		t.Error("CurrentGroup should be nil after reset")
	}
	if calc.PreviousGroup() != nil {
		t.Error("PreviousGroup should be nil after reset")
	}

	// Verify calculator still works after reset
	pkt3 := PacketInfo{
		ArrivalTime: baseTime.Add(200 * time.Millisecond),
		SendTime:    2000,
		Size:        100,
	}
	_, hasResult := calc.AddPacket(pkt3)
	if hasResult {
		t.Error("First packet after reset should not produce result")
	}
	if calc.CurrentGroup() == nil {
		t.Error("CurrentGroup should exist after adding packet post-reset")
	}
}

func TestInterArrivalCalculator_BelongsToBurst(t *testing.T) {
	calc := NewInterArrivalCalculator(5 * time.Millisecond)
	baseTime := time.Now()

	// Test with no current group
	pkt := PacketInfo{
		ArrivalTime: baseTime,
		SendTime:    1000,
		Size:        100,
	}
	if calc.BelongsToBurst(pkt) {
		t.Error("BelongsToBurst should return false when no current group")
	}

	// Add first packet
	calc.AddPacket(pkt)

	// Test packet within threshold
	pktInBurst := PacketInfo{
		ArrivalTime: baseTime.Add(3 * time.Millisecond),
		SendTime:    1100,
		Size:        100,
	}
	if !calc.BelongsToBurst(pktInBurst) {
		t.Error("BelongsToBurst should return true for packet within threshold")
	}

	// Test packet outside threshold
	pktOutside := PacketInfo{
		ArrivalTime: baseTime.Add(10 * time.Millisecond),
		SendTime:    1200,
		Size:        100,
	}
	if calc.BelongsToBurst(pktOutside) {
		t.Error("BelongsToBurst should return false for packet outside threshold")
	}
}

func TestInterArrivalCalculator_MultipleGroups(t *testing.T) {
	calc := NewInterArrivalCalculator(5 * time.Millisecond)
	baseTime := time.Now()

	// Create a sequence of packets forming multiple groups
	// Group 1: t=0, t=2ms, t=4ms (3 packets)
	// Group 2: t=50ms, t=52ms (2 packets)
	// Group 3: t=100ms (1 packet)

	packets := []PacketInfo{
		{ArrivalTime: baseTime, SendTime: 1000, Size: 100},
		{ArrivalTime: baseTime.Add(2 * time.Millisecond), SendTime: 1100, Size: 100},
		{ArrivalTime: baseTime.Add(4 * time.Millisecond), SendTime: 1200, Size: 100},
		{ArrivalTime: baseTime.Add(50 * time.Millisecond), SendTime: 1000 + msToAbsSendTime(50), Size: 150},
		{ArrivalTime: baseTime.Add(52 * time.Millisecond), SendTime: 1100 + msToAbsSendTime(50), Size: 150},
		{ArrivalTime: baseTime.Add(100 * time.Millisecond), SendTime: 1000 + msToAbsSendTime(100), Size: 200},
	}

	results := 0
	for _, pkt := range packets {
		_, hasResult := calc.AddPacket(pkt)
		if hasResult {
			results++
		}
	}

	// Should have 2 results (group 2 and group 3 each produce one when they start)
	if results != 2 {
		t.Errorf("Expected 2 delay variation results, got %d", results)
	}
}
