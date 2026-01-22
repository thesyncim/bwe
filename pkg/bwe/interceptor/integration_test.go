package interceptor

import (
	"sync"
	"testing"
	"time"

	"github.com/pion/interceptor"
	"github.com/pion/rtcp"
	"github.com/pion/rtp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- Helper Functions ---

// generateRTPPackets creates a sequence of RTP packets with abs-send-time extensions.
// The send times increment progressively to simulate steady packet flow.
func generateRTPPackets(ssrc uint32, extID uint8, count int, baseTime uint32, timeIncrement uint32) [][]byte {
	packets := make([][]byte, count)
	for i := 0; i < count; i++ {
		sendTime := (baseTime + uint32(i)*timeIncrement) & 0xFFFFFF // 24-bit wrap
		pkt := &rtp.Packet{
			Header: rtp.Header{
				Version:        2,
				PayloadType:    96,
				SequenceNumber: uint16(1000 + i),
				Timestamp:      uint32(90000 + i*3000), // 30fps-ish
				SSRC:           ssrc,
			},
			Payload: make([]byte, 1000), // 1000 byte payload
		}
		// Add abs-send-time extension
		extData := []byte{
			byte(sendTime >> 16),
			byte(sendTime >> 8),
			byte(sendTime),
		}
		_ = pkt.Header.SetExtension(extID, extData)
		packets[i], _ = pkt.Marshal()
	}
	return packets
}

// mockRTPReaderWithData returns a reader that provides packets one at a time.
type mockRTPReaderWithData struct {
	packets [][]byte
	mu      sync.Mutex
	index   int
}

func newMockRTPReaderWithData(packets [][]byte) *mockRTPReaderWithData {
	return &mockRTPReaderWithData{packets: packets}
}

func (m *mockRTPReaderWithData) Read(b []byte, a interceptor.Attributes) (int, interceptor.Attributes, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.index >= len(m.packets) {
		return 0, nil, nil
	}
	pkt := m.packets[m.index]
	m.index++
	n := copy(b, pkt)
	return n, a, nil
}

// AddPacket appends a packet to the reader.
func (m *mockRTPReaderWithData) AddPacket(pkt []byte) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.packets = append(m.packets, pkt)
}

// captureRTCPWriter captures all RTCP packets written to it.
type captureRTCPWriter struct {
	mu      sync.Mutex
	packets []rtcp.Packet
}

func newCaptureRTCPWriter() *captureRTCPWriter {
	return &captureRTCPWriter{}
}

func (c *captureRTCPWriter) Write(pkts []rtcp.Packet, _ interceptor.Attributes) (int, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.packets = append(c.packets, pkts...)
	return len(pkts), nil
}

func (c *captureRTCPWriter) GetPackets() []rtcp.Packet {
	c.mu.Lock()
	defer c.mu.Unlock()
	result := make([]rtcp.Packet, len(c.packets))
	copy(result, c.packets)
	return result
}

func (c *captureRTCPWriter) GetREMBs() []*rtcp.ReceiverEstimatedMaximumBitrate {
	c.mu.Lock()
	defer c.mu.Unlock()
	var rembs []*rtcp.ReceiverEstimatedMaximumBitrate
	for _, pkt := range c.packets {
		if remb, ok := pkt.(*rtcp.ReceiverEstimatedMaximumBitrate); ok {
			rembs = append(rembs, remb)
		}
	}
	return rembs
}

// --- Integration Tests ---

// TestIntegration_EndToEnd tests the complete flow:
// Factory -> Interceptor -> BindRemoteStream -> ProcessRTP -> BindRTCPWriter -> REMB
func TestIntegration_EndToEnd(t *testing.T) {
	// Create factory with short REMB interval for testing
	factory, err := NewBWEInterceptorFactory(
		WithFactoryREMBInterval(50*time.Millisecond),
		WithFactorySenderSSRC(0xDEADBEEF),
		WithInitialBitrate(500000),
	)
	require.NoError(t, err)

	// Create interceptor via factory
	inter, err := factory.NewInterceptor("test-connection-id")
	require.NoError(t, err)
	require.NotNil(t, inter)

	bweInter := inter.(*BWEInterceptor)
	defer bweInter.Close()

	// Set up stream info with extension
	testSSRC := uint32(0x12345678)
	extID := uint8(3)

	streamInfo := &interceptor.StreamInfo{
		SSRC: testSSRC,
		RTPHeaderExtensions: []interceptor.RTPHeaderExtension{
			{URI: AbsSendTimeURI, ID: int(extID)},
		},
	}

	// Generate RTP packets
	packets := generateRTPPackets(testSSRC, extID, 30, 0x010000, 0x1000)
	reader := newMockRTPReaderWithData(packets)

	// Bind remote stream
	wrappedReader := bweInter.BindRemoteStream(streamInfo, reader)
	require.NotNil(t, wrappedReader)

	// Verify extension ID was extracted
	assert.Equal(t, uint32(extID), bweInter.absExtID.Load())

	// Read packets through wrapped reader (triggers processRTP)
	buf := make([]byte, 1500)
	for i := 0; i < len(packets); i++ {
		n, _, err := wrappedReader.Read(buf, nil)
		require.NoError(t, err)
		require.Greater(t, n, 0)
		time.Sleep(10 * time.Millisecond) // Spread arrivals
	}

	// Bind RTCP writer - this starts REMB loop
	rtcpWriter := newCaptureRTCPWriter()
	returnedWriter := bweInter.BindRTCPWriter(rtcpWriter)
	assert.Equal(t, rtcpWriter, returnedWriter)

	// Wait for REMB packets to be sent
	time.Sleep(200 * time.Millisecond)

	// Verify REMB was sent
	rembs := rtcpWriter.GetREMBs()
	assert.Greater(t, len(rembs), 0, "Expected at least one REMB packet")

	if len(rembs) > 0 {
		// Verify REMB structure
		remb := rembs[0]
		assert.Greater(t, remb.Bitrate, float32(0), "REMB bitrate should be positive")
		assert.Contains(t, remb.SSRCs, testSSRC, "REMB should include stream SSRC")
		t.Logf("End-to-end REMB: bitrate=%.0f bps, SSRCs=%v", remb.Bitrate, remb.SSRCs)
	}
}

// TestIntegration_MultiStream tests multiple SSRCs with REMB including both.
func TestIntegration_MultiStream(t *testing.T) {
	factory, err := NewBWEInterceptorFactory(
		WithFactoryREMBInterval(50*time.Millisecond),
	)
	require.NoError(t, err)

	inter, err := factory.NewInterceptor("multi-stream-test")
	require.NoError(t, err)

	bweInter := inter.(*BWEInterceptor)
	defer bweInter.Close()

	ssrc1 := uint32(0x11111111)
	ssrc2 := uint32(0x22222222)
	extID := uint8(3)

	// Bind first stream
	info1 := &interceptor.StreamInfo{
		SSRC: ssrc1,
		RTPHeaderExtensions: []interceptor.RTPHeaderExtension{
			{URI: AbsSendTimeURI, ID: int(extID)},
		},
	}
	packets1 := generateRTPPackets(ssrc1, extID, 20, 0x000000, 0x1000)
	reader1 := newMockRTPReaderWithData(packets1)
	wrapped1 := bweInter.BindRemoteStream(info1, reader1)

	// Bind second stream
	info2 := &interceptor.StreamInfo{
		SSRC: ssrc2,
		RTPHeaderExtensions: []interceptor.RTPHeaderExtension{
			{URI: AbsSendTimeURI, ID: int(extID)},
		},
	}
	packets2 := generateRTPPackets(ssrc2, extID, 20, 0x000100, 0x1000)
	reader2 := newMockRTPReaderWithData(packets2)
	wrapped2 := bweInter.BindRemoteStream(info2, reader2)

	// Read packets from both streams interleaved
	buf := make([]byte, 1500)
	for i := 0; i < 20; i++ {
		wrapped1.Read(buf, nil)
		wrapped2.Read(buf, nil)
		time.Sleep(5 * time.Millisecond)
	}

	// Bind RTCP writer
	rtcpWriter := newCaptureRTCPWriter()
	bweInter.BindRTCPWriter(rtcpWriter)

	// Wait for REMB
	time.Sleep(200 * time.Millisecond)

	// Verify REMB includes both SSRCs
	rembs := rtcpWriter.GetREMBs()
	require.Greater(t, len(rembs), 0, "Expected at least one REMB packet")

	// Find a REMB with both SSRCs
	var foundBothSSRCs bool
	for _, remb := range rembs {
		hasSSRC1, hasSSRC2 := false, false
		for _, ssrc := range remb.SSRCs {
			if ssrc == ssrc1 {
				hasSSRC1 = true
			}
			if ssrc == ssrc2 {
				hasSSRC2 = true
			}
		}
		if hasSSRC1 && hasSSRC2 {
			foundBothSSRCs = true
			t.Logf("Multi-stream REMB: bitrate=%.0f bps, SSRCs=%v", remb.Bitrate, remb.SSRCs)
			break
		}
	}
	assert.True(t, foundBothSSRCs, "Expected REMB to include both SSRCs")
}

// TestIntegration_StreamTimeout verifies streams are cleaned up after 2s inactivity.
func TestIntegration_StreamTimeout(t *testing.T) {
	factory, err := NewBWEInterceptorFactory()
	require.NoError(t, err)

	inter, err := factory.NewInterceptor("timeout-test")
	require.NoError(t, err)

	bweInter := inter.(*BWEInterceptor)
	defer bweInter.Close()

	testSSRC := uint32(0xDEADBEEF)
	extID := uint8(3)

	// Bind stream
	info := &interceptor.StreamInfo{
		SSRC: testSSRC,
		RTPHeaderExtensions: []interceptor.RTPHeaderExtension{
			{URI: AbsSendTimeURI, ID: int(extID)},
		},
	}
	packets := generateRTPPackets(testSSRC, extID, 5, 0x010000, 0x1000)
	reader := newMockRTPReaderWithData(packets)
	wrapped := bweInter.BindRemoteStream(info, reader)

	// Read a few packets (keeps stream active briefly)
	buf := make([]byte, 1500)
	for i := 0; i < 5; i++ {
		wrapped.Read(buf, nil)
	}

	// Verify stream exists
	_, exists := bweInter.streams.Load(testSSRC)
	require.True(t, exists, "stream should exist initially")

	// Wait for timeout (2s) + cleanup interval (1s) + margin
	time.Sleep(3500 * time.Millisecond)

	// Verify stream was cleaned up
	_, exists = bweInter.streams.Load(testSSRC)
	assert.False(t, exists, "stream should be removed after timeout (PION-04)")
}

// TestIntegration_FactoryCreatesIndependentEstimators verifies each interceptor
// has its own BandwidthEstimator instance.
func TestIntegration_FactoryCreatesIndependentEstimators(t *testing.T) {
	factory, err := NewBWEInterceptorFactory(
		WithInitialBitrate(500000),
	)
	require.NoError(t, err)

	// Create two interceptors
	inter1, err := factory.NewInterceptor("conn-1")
	require.NoError(t, err)
	inter2, err := factory.NewInterceptor("conn-2")
	require.NoError(t, err)

	bwe1 := inter1.(*BWEInterceptor)
	bwe2 := inter2.(*BWEInterceptor)
	defer bwe1.Close()
	defer bwe2.Close()

	// Verify they have different estimator instances
	assert.NotSame(t, bwe1.estimator, bwe2.estimator,
		"Each interceptor should have its own estimator")

	// Feed packets only to first interceptor
	ssrc := uint32(0x12345678)
	extID := uint8(3)
	info := &interceptor.StreamInfo{
		SSRC: ssrc,
		RTPHeaderExtensions: []interceptor.RTPHeaderExtension{
			{URI: AbsSendTimeURI, ID: int(extID)},
		},
	}
	packets := generateRTPPackets(ssrc, extID, 10, 0, 0x1000)
	reader := newMockRTPReaderWithData(packets)
	wrapped := bwe1.BindRemoteStream(info, reader)

	buf := make([]byte, 1500)
	for i := 0; i < 10; i++ {
		wrapped.Read(buf, nil)
	}

	// First estimator should have SSRCs, second should not
	ssrcs1 := bwe1.estimator.GetSSRCs()
	ssrcs2 := bwe2.estimator.GetSSRCs()

	assert.Contains(t, ssrcs1, ssrc, "First estimator should have the SSRC")
	assert.NotContains(t, ssrcs2, ssrc, "Second estimator should NOT have the SSRC")
}

// TestIntegration_CloseStopsAllGoroutines verifies Clean shutdown.
func TestIntegration_CloseStopsAllGoroutines(t *testing.T) {
	factory, err := NewBWEInterceptorFactory(
		WithFactoryREMBInterval(10*time.Millisecond),
	)
	require.NoError(t, err)

	inter, err := factory.NewInterceptor("close-test")
	require.NoError(t, err)

	bweInter := inter.(*BWEInterceptor)

	// Start both goroutines
	ssrc := uint32(0x12345678)
	info := &interceptor.StreamInfo{
		SSRC: ssrc,
		RTPHeaderExtensions: []interceptor.RTPHeaderExtension{
			{URI: AbsSendTimeURI, ID: 3},
		},
	}
	_ = bweInter.BindRemoteStream(info, newMockRTPReaderWithData(nil))
	bweInter.BindRTCPWriter(newCaptureRTCPWriter())

	// Let goroutines run briefly
	time.Sleep(50 * time.Millisecond)

	// Close should complete quickly
	done := make(chan struct{})
	go func() {
		err := bweInter.Close()
		assert.NoError(t, err)
		close(done)
	}()

	select {
	case <-done:
		// Good - Close() completed
	case <-time.After(2 * time.Second):
		t.Fatal("Close() timed out - goroutines may not have stopped")
	}
}
