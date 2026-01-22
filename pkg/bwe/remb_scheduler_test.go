package bwe

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestREMBScheduler_FirstCallAlwaysSends(t *testing.T) {
	s := NewREMBScheduler(DefaultREMBSchedulerConfig())
	t0 := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)

	data, sent, err := s.MaybeSendREMB(1_000_000, []uint32{0x1234}, t0)
	require.NoError(t, err)
	assert.True(t, sent, "first call should send")
	assert.NotEmpty(t, data, "should return REMB packet data")
}

func TestREMBScheduler_RegularInterval(t *testing.T) {
	s := NewREMBScheduler(DefaultREMBSchedulerConfig())
	t0 := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	ssrcs := []uint32{0x1234}
	estimate := int64(1_000_000)

	// First call sends
	_, sent, err := s.MaybeSendREMB(estimate, ssrcs, t0)
	require.NoError(t, err)
	assert.True(t, sent, "t=0: first call should send")

	// 500ms later - too soon
	_, sent, err = s.MaybeSendREMB(estimate, ssrcs, t0.Add(500*time.Millisecond))
	require.NoError(t, err)
	assert.False(t, sent, "t=500ms: too soon, should not send")

	// 1 second later - interval elapsed
	_, sent, err = s.MaybeSendREMB(estimate, ssrcs, t0.Add(time.Second))
	require.NoError(t, err)
	assert.True(t, sent, "t=1s: interval elapsed, should send")

	// 1.5 seconds later - too soon after last send
	_, sent, err = s.MaybeSendREMB(estimate, ssrcs, t0.Add(1500*time.Millisecond))
	require.NoError(t, err)
	assert.False(t, sent, "t=1.5s: too soon after last send, should not send")

	// 2 seconds later - interval elapsed again
	_, sent, err = s.MaybeSendREMB(estimate, ssrcs, t0.Add(2*time.Second))
	require.NoError(t, err)
	assert.True(t, sent, "t=2s: interval elapsed, should send")
}

func TestREMBScheduler_ImmediateDecrease(t *testing.T) {
	s := NewREMBScheduler(DefaultREMBSchedulerConfig())
	t0 := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	ssrcs := []uint32{0x1234}

	// First call at 1 Mbps
	_, sent, err := s.MaybeSendREMB(1_000_000, ssrcs, t0)
	require.NoError(t, err)
	assert.True(t, sent, "first call should send")

	// 100ms later with 4% decrease (960000 = 96% of 1000000)
	// Should trigger immediate send even though interval not elapsed
	_, sent, err = s.MaybeSendREMB(960_000, ssrcs, t0.Add(100*time.Millisecond))
	require.NoError(t, err)
	assert.True(t, sent, "4% decrease should trigger immediate send")
}

func TestREMBScheduler_SmallDecreaseNoImmediateSend(t *testing.T) {
	s := NewREMBScheduler(DefaultREMBSchedulerConfig())
	t0 := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	ssrcs := []uint32{0x1234}

	// First call at 1 Mbps
	_, sent, err := s.MaybeSendREMB(1_000_000, ssrcs, t0)
	require.NoError(t, err)
	assert.True(t, sent, "first call should send")

	// 100ms later with 2% decrease (980000 = 98% of 1000000)
	// Should NOT trigger immediate send (below 3% threshold)
	_, sent, err = s.MaybeSendREMB(980_000, ssrcs, t0.Add(100*time.Millisecond))
	require.NoError(t, err)
	assert.False(t, sent, "2% decrease should not trigger immediate send")

	// Wait for regular interval
	_, sent, err = s.MaybeSendREMB(980_000, ssrcs, t0.Add(time.Second))
	require.NoError(t, err)
	assert.True(t, sent, "regular interval should send")
}

func TestREMBScheduler_IncreaseNoImmediateSend(t *testing.T) {
	s := NewREMBScheduler(DefaultREMBSchedulerConfig())
	t0 := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	ssrcs := []uint32{0x1234}

	// First call at 1 Mbps
	_, sent, err := s.MaybeSendREMB(1_000_000, ssrcs, t0)
	require.NoError(t, err)
	assert.True(t, sent, "first call should send")

	// 100ms later with 10% increase
	// Should NOT trigger immediate send (only decreases trigger immediate)
	_, sent, err = s.MaybeSendREMB(1_100_000, ssrcs, t0.Add(100*time.Millisecond))
	require.NoError(t, err)
	assert.False(t, sent, "increase should not trigger immediate send")

	// Wait for regular interval
	_, sent, err = s.MaybeSendREMB(1_100_000, ssrcs, t0.Add(time.Second))
	require.NoError(t, err)
	assert.True(t, sent, "regular interval should send")
}

func TestREMBScheduler_ConfigurableThreshold(t *testing.T) {
	config := DefaultREMBSchedulerConfig()
	config.DecreaseThreshold = 0.05 // 5% threshold
	s := NewREMBScheduler(config)

	t0 := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	ssrcs := []uint32{0x1234}

	// First call at 1 Mbps
	_, sent, err := s.MaybeSendREMB(1_000_000, ssrcs, t0)
	require.NoError(t, err)
	assert.True(t, sent, "first call should send")

	// 4% decrease - should NOT trigger with 5% threshold
	_, sent, err = s.MaybeSendREMB(960_000, ssrcs, t0.Add(100*time.Millisecond))
	require.NoError(t, err)
	assert.False(t, sent, "4% decrease should not trigger with 5% threshold")

	// 6% decrease - should trigger with 5% threshold
	_, sent, err = s.MaybeSendREMB(940_000, ssrcs, t0.Add(200*time.Millisecond))
	require.NoError(t, err)
	assert.True(t, sent, "6% decrease should trigger with 5% threshold")
}

func TestREMBScheduler_ConfigurableInterval(t *testing.T) {
	config := DefaultREMBSchedulerConfig()
	config.Interval = 500 * time.Millisecond
	s := NewREMBScheduler(config)

	t0 := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	ssrcs := []uint32{0x1234}
	estimate := int64(1_000_000)

	// First call sends
	_, sent, err := s.MaybeSendREMB(estimate, ssrcs, t0)
	require.NoError(t, err)
	assert.True(t, sent, "first call should send")

	// 300ms later - too soon
	_, sent, err = s.MaybeSendREMB(estimate, ssrcs, t0.Add(300*time.Millisecond))
	require.NoError(t, err)
	assert.False(t, sent, "300ms: too soon with 500ms interval")

	// 500ms later - interval elapsed
	_, sent, err = s.MaybeSendREMB(estimate, ssrcs, t0.Add(500*time.Millisecond))
	require.NoError(t, err)
	assert.True(t, sent, "500ms: should send with 500ms interval")
}

func TestREMBScheduler_REMBPacketContent(t *testing.T) {
	config := DefaultREMBSchedulerConfig()
	config.SenderSSRC = 0xABCD1234
	s := NewREMBScheduler(config)

	t0 := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	ssrcs := []uint32{0x5678, 0x9ABC}
	estimate := int64(2_500_000)

	data, sent, err := s.MaybeSendREMB(estimate, ssrcs, t0)
	require.NoError(t, err)
	require.True(t, sent)
	require.NotEmpty(t, data)

	// Parse the returned packet
	parsed, err := ParseREMB(data)
	require.NoError(t, err)

	assert.Equal(t, uint32(0xABCD1234), parsed.SenderSSRC)
	// Note: REMB encoding may have some precision loss
	assert.InDelta(t, estimate, int64(parsed.Bitrate), float64(estimate)*0.01)
	assert.Equal(t, ssrcs, parsed.SSRCs)
}

func TestREMBScheduler_LastSentTracking(t *testing.T) {
	s := NewREMBScheduler(DefaultREMBSchedulerConfig())
	t0 := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	ssrcs := []uint32{0x1234}

	// Before any send
	assert.Equal(t, int64(0), s.LastSentValue())
	assert.True(t, s.LastSentTime().IsZero())

	// First send
	_, sent, err := s.MaybeSendREMB(1_000_000, ssrcs, t0)
	require.NoError(t, err)
	require.True(t, sent)

	assert.Equal(t, int64(1_000_000), s.LastSentValue())
	assert.Equal(t, t0, s.LastSentTime())

	// No send (interval not elapsed)
	_, sent, err = s.MaybeSendREMB(1_000_000, ssrcs, t0.Add(500*time.Millisecond))
	require.NoError(t, err)
	require.False(t, sent)

	// Values should NOT update when not sending
	assert.Equal(t, int64(1_000_000), s.LastSentValue())
	assert.Equal(t, t0, s.LastSentTime())

	// Send at t=1s
	t1 := t0.Add(time.Second)
	_, sent, err = s.MaybeSendREMB(1_200_000, ssrcs, t1)
	require.NoError(t, err)
	require.True(t, sent)

	assert.Equal(t, int64(1_200_000), s.LastSentValue())
	assert.Equal(t, t1, s.LastSentTime())
}

func TestREMBScheduler_Reset(t *testing.T) {
	s := NewREMBScheduler(DefaultREMBSchedulerConfig())
	t0 := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	ssrcs := []uint32{0x1234}

	// First send
	_, sent, err := s.MaybeSendREMB(1_000_000, ssrcs, t0)
	require.NoError(t, err)
	require.True(t, sent)

	// Verify state is set
	assert.Equal(t, int64(1_000_000), s.LastSentValue())
	assert.Equal(t, t0, s.LastSentTime())

	// Reset
	s.Reset()

	// Verify state is cleared
	assert.Equal(t, int64(0), s.LastSentValue())
	assert.True(t, s.LastSentTime().IsZero())

	// Next call should send again (as if first call)
	_, sent, err = s.MaybeSendREMB(2_000_000, ssrcs, t0.Add(100*time.Millisecond))
	require.NoError(t, err)
	assert.True(t, sent, "after reset, next call should send")
}

func TestREMBScheduler_ShouldSendREMB_Direct(t *testing.T) {
	// Test ShouldSendREMB directly for edge cases
	s := NewREMBScheduler(DefaultREMBSchedulerConfig())
	t0 := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)

	// First call should return true (lastSent is zero)
	assert.True(t, s.ShouldSendREMB(1_000_000, t0))

	// Simulate a send
	s.lastSent = t0
	s.lastValue = 1_000_000

	// Exact threshold (3%) should trigger
	assert.True(t, s.ShouldSendREMB(970_000, t0.Add(100*time.Millisecond)))

	// Just under threshold should not trigger
	s.lastSent = t0
	s.lastValue = 1_000_000
	assert.False(t, s.ShouldSendREMB(971_000, t0.Add(100*time.Millisecond)))
}

func TestREMBScheduler_BuildAndRecordREMB(t *testing.T) {
	config := DefaultREMBSchedulerConfig()
	config.SenderSSRC = 0x12345678
	s := NewREMBScheduler(config)

	t0 := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	ssrcs := []uint32{0x1111, 0x2222}

	data, err := s.BuildAndRecordREMB(5_000_000, ssrcs, t0)
	require.NoError(t, err)
	require.NotEmpty(t, data)

	// Verify internal state was updated
	assert.Equal(t, int64(5_000_000), s.lastValue)
	assert.Equal(t, t0, s.lastSent)

	// Verify packet content
	parsed, err := ParseREMB(data)
	require.NoError(t, err)
	assert.Equal(t, uint32(0x12345678), parsed.SenderSSRC)
}

func TestREMBScheduler_ZeroEstimate(t *testing.T) {
	s := NewREMBScheduler(DefaultREMBSchedulerConfig())
	t0 := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	ssrcs := []uint32{0x1234}

	// First call with zero estimate should still send
	_, sent, err := s.MaybeSendREMB(0, ssrcs, t0)
	require.NoError(t, err)
	assert.True(t, sent, "first call should send even with zero estimate")

	// Next call at interval should send
	_, sent, err = s.MaybeSendREMB(0, ssrcs, t0.Add(time.Second))
	require.NoError(t, err)
	assert.True(t, sent, "interval elapsed should send")
}

func TestREMBScheduler_ConsecutiveDecreases(t *testing.T) {
	s := NewREMBScheduler(DefaultREMBSchedulerConfig())
	t0 := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	ssrcs := []uint32{0x1234}

	// First send at 1 Mbps
	_, sent, _ := s.MaybeSendREMB(1_000_000, ssrcs, t0)
	assert.True(t, sent)

	// 5% decrease -> immediate send
	_, sent, _ = s.MaybeSendREMB(950_000, ssrcs, t0.Add(50*time.Millisecond))
	assert.True(t, sent, "first 5% decrease should trigger")

	// Another 5% decrease from new value -> immediate send
	_, sent, _ = s.MaybeSendREMB(902_500, ssrcs, t0.Add(100*time.Millisecond))
	assert.True(t, sent, "second 5% decrease should trigger")

	// Small decrease (2%) -> no send
	_, sent, _ = s.MaybeSendREMB(884_450, ssrcs, t0.Add(150*time.Millisecond))
	assert.False(t, sent, "2% decrease should not trigger")
}
