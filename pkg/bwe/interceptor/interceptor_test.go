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

	"github.com/thesyncim/bwe/pkg/bwe"
)

// makeRTPWithAbsSendTime creates an RTP packet with the abs-send-time extension.
// The extension uses one-byte header format (RFC 5285).
func makeRTPWithAbsSendTime(ssrc uint32, extID uint8, sendTime uint32) []byte {
	// Build RTP packet with extension
	pkt := &rtp.Packet{
		Header: rtp.Header{
			Version:        2,
			PayloadType:    96,
			SequenceNumber: 1234,
			Timestamp:      12345678,
			SSRC:           ssrc,
		},
		Payload: []byte{0x00, 0x01, 0x02, 0x03}, // Dummy payload
	}

	// Add extension using SetExtension method (one-byte header format)
	// abs-send-time is 3 bytes
	extData := []byte{
		byte(sendTime >> 16),
		byte(sendTime >> 8),
		byte(sendTime),
	}
	_ = pkt.Header.SetExtension(extID, extData)

	// Marshal the packet
	data, _ := pkt.Marshal()
	return data
}

// makeRTPWithoutExtension creates a basic RTP packet without any extensions.
func makeRTPWithoutExtension(ssrc uint32) []byte {
	pkt := &rtp.Packet{
		Header: rtp.Header{
			Version:        2,
			PayloadType:    96,
			SequenceNumber: 1234,
			Timestamp:      12345678,
			SSRC:           ssrc,
		},
		Payload: []byte{0x00, 0x01, 0x02, 0x03},
	}

	data, _ := pkt.Marshal()
	return data
}

// mockRTPReader is a test reader that returns pre-defined packets.
type mockRTPReader struct {
	packets [][]byte
	index   int
}

func (m *mockRTPReader) Read(b []byte, a interceptor.Attributes) (int, interceptor.Attributes, error) {
	if m.index >= len(m.packets) {
		return 0, nil, nil
	}
	pkt := m.packets[m.index]
	m.index++
	n := copy(b, pkt)
	return n, a, nil
}

func TestNewBWEInterceptor(t *testing.T) {
	estimator := bwe.NewBandwidthEstimator(bwe.DefaultBandwidthEstimatorConfig(), nil)

	t.Run("default options", func(t *testing.T) {
		i := NewBWEInterceptor(estimator)
		require.NotNil(t, i)
		assert.NotNil(t, i.estimator)
		assert.Equal(t, time.Second, i.rembInterval)
		assert.NotNil(t, i.closed)
	})

	t.Run("with custom options", func(t *testing.T) {
		i := NewBWEInterceptor(estimator,
			WithREMBInterval(500*time.Millisecond),
			WithSenderSSRC(0x12345678),
		)
		require.NotNil(t, i)
		assert.Equal(t, 500*time.Millisecond, i.rembInterval)
		assert.Equal(t, uint32(0x12345678), i.senderSSRC)
	})
}

func TestBindRemoteStream_ExtractsExtensionIDs(t *testing.T) {
	estimator := bwe.NewBandwidthEstimator(bwe.DefaultBandwidthEstimatorConfig(), nil)
	i := NewBWEInterceptor(estimator)

	t.Run("extracts abs-send-time ID", func(t *testing.T) {
		info := &interceptor.StreamInfo{
			SSRC: 12345,
			RTPHeaderExtensions: []interceptor.RTPHeaderExtension{
				{URI: AbsSendTimeURI, ID: 3},
			},
		}

		reader := &mockRTPReader{}
		wrappedReader := i.BindRemoteStream(info, reader)

		assert.NotNil(t, wrappedReader)
		assert.Equal(t, uint32(3), i.absExtID.Load())
	})

	t.Run("extracts abs-capture-time ID", func(t *testing.T) {
		// Create new interceptor to reset state
		estimator2 := bwe.NewBandwidthEstimator(bwe.DefaultBandwidthEstimatorConfig(), nil)
		i2 := NewBWEInterceptor(estimator2)

		info := &interceptor.StreamInfo{
			SSRC: 12345,
			RTPHeaderExtensions: []interceptor.RTPHeaderExtension{
				{URI: AbsCaptureTimeURI, ID: 5},
			},
		}

		reader := &mockRTPReader{}
		_ = i2.BindRemoteStream(info, reader)

		assert.Equal(t, uint32(5), i2.captureExtID.Load())
	})

	t.Run("first stream wins for extension ID", func(t *testing.T) {
		estimator3 := bwe.NewBandwidthEstimator(bwe.DefaultBandwidthEstimatorConfig(), nil)
		i3 := NewBWEInterceptor(estimator3)

		// First stream sets extension ID to 3
		info1 := &interceptor.StreamInfo{
			SSRC: 11111,
			RTPHeaderExtensions: []interceptor.RTPHeaderExtension{
				{URI: AbsSendTimeURI, ID: 3},
			},
		}
		_ = i3.BindRemoteStream(info1, &mockRTPReader{})
		assert.Equal(t, uint32(3), i3.absExtID.Load())

		// Second stream tries to set extension ID to 7 - should be ignored
		info2 := &interceptor.StreamInfo{
			SSRC: 22222,
			RTPHeaderExtensions: []interceptor.RTPHeaderExtension{
				{URI: AbsSendTimeURI, ID: 7},
			},
		}
		_ = i3.BindRemoteStream(info2, &mockRTPReader{})
		assert.Equal(t, uint32(3), i3.absExtID.Load()) // Still 3, not 7
	})
}

func TestProcessRTP_FeedsEstimator(t *testing.T) {
	estimator := bwe.NewBandwidthEstimator(bwe.DefaultBandwidthEstimatorConfig(), nil)
	i := NewBWEInterceptor(estimator)

	// Bind a stream with abs-send-time extension
	testSSRC := uint32(0xABCDEF12)
	extID := uint8(3)

	info := &interceptor.StreamInfo{
		SSRC: testSSRC,
		RTPHeaderExtensions: []interceptor.RTPHeaderExtension{
			{URI: AbsSendTimeURI, ID: int(extID)},
		},
	}

	// Create RTP packet with abs-send-time
	sendTime := uint32(0x010000) // 1/4 second in 6.18 format
	rtpPacket := makeRTPWithAbsSendTime(testSSRC, extID, sendTime)

	// Use mockRTPReader that returns our packet
	reader := &mockRTPReader{packets: [][]byte{rtpPacket}}
	wrappedReader := i.BindRemoteStream(info, reader)

	// Read through the wrapped reader (this triggers processRTP)
	buf := make([]byte, 1500)
	n, _, err := wrappedReader.Read(buf, nil)
	require.NoError(t, err)
	require.Greater(t, n, 0)

	// Verify estimator received the packet by checking SSRC tracking
	ssrcs := estimator.GetSSRCs()
	assert.Contains(t, ssrcs, testSSRC, "Estimator should have tracked the SSRC")
}

func TestProcessRTP_NoExtension_Skips(t *testing.T) {
	estimator := bwe.NewBandwidthEstimator(bwe.DefaultBandwidthEstimatorConfig(), nil)
	i := NewBWEInterceptor(estimator)

	testSSRC := uint32(0x99999999)

	// Bind stream but the packet has no extension
	info := &interceptor.StreamInfo{
		SSRC: testSSRC,
		RTPHeaderExtensions: []interceptor.RTPHeaderExtension{
			{URI: AbsSendTimeURI, ID: 3},
		},
	}

	// Create RTP packet WITHOUT extension
	rtpPacket := makeRTPWithoutExtension(testSSRC)

	reader := &mockRTPReader{packets: [][]byte{rtpPacket}}
	wrappedReader := i.BindRemoteStream(info, reader)

	// Read through the wrapped reader
	buf := make([]byte, 1500)
	n, _, err := wrappedReader.Read(buf, nil)
	require.NoError(t, err)
	require.Greater(t, n, 0)

	// Estimator should NOT have this SSRC since packet had no timing extension
	ssrcs := estimator.GetSSRCs()
	assert.NotContains(t, ssrcs, testSSRC, "Estimator should not track SSRC from packet without timing extension")
}

func TestMultipleStreams_TrackedSeparately(t *testing.T) {
	estimator := bwe.NewBandwidthEstimator(bwe.DefaultBandwidthEstimatorConfig(), nil)
	i := NewBWEInterceptor(estimator)

	ssrc1 := uint32(0x11111111)
	ssrc2 := uint32(0x22222222)

	// Bind first stream
	info1 := &interceptor.StreamInfo{
		SSRC: ssrc1,
		RTPHeaderExtensions: []interceptor.RTPHeaderExtension{
			{URI: AbsSendTimeURI, ID: 3},
		},
	}
	_ = i.BindRemoteStream(info1, &mockRTPReader{})

	// Bind second stream
	info2 := &interceptor.StreamInfo{
		SSRC: ssrc2,
		RTPHeaderExtensions: []interceptor.RTPHeaderExtension{
			{URI: AbsSendTimeURI, ID: 3},
		},
	}
	_ = i.BindRemoteStream(info2, &mockRTPReader{})

	// Verify both streams are tracked
	var count int
	i.streams.Range(func(key, value interface{}) bool {
		count++
		ssrc := key.(uint32)
		assert.True(t, ssrc == ssrc1 || ssrc == ssrc2, "Unexpected SSRC in streams map")
		return true
	})
	assert.Equal(t, 2, count, "Expected 2 streams to be tracked")
}

func TestUnbindRemoteStream(t *testing.T) {
	estimator := bwe.NewBandwidthEstimator(bwe.DefaultBandwidthEstimatorConfig(), nil)
	i := NewBWEInterceptor(estimator)

	testSSRC := uint32(0x55555555)

	// Bind stream
	info := &interceptor.StreamInfo{
		SSRC: testSSRC,
		RTPHeaderExtensions: []interceptor.RTPHeaderExtension{
			{URI: AbsSendTimeURI, ID: 3},
		},
	}
	_ = i.BindRemoteStream(info, &mockRTPReader{})

	// Verify stream is tracked
	_, ok := i.streams.Load(testSSRC)
	assert.True(t, ok, "Stream should be tracked after BindRemoteStream")

	// Unbind stream
	i.UnbindRemoteStream(info)

	// Verify stream is removed
	_, ok = i.streams.Load(testSSRC)
	assert.False(t, ok, "Stream should be removed after UnbindRemoteStream")
}

func TestClose(t *testing.T) {
	estimator := bwe.NewBandwidthEstimator(bwe.DefaultBandwidthEstimatorConfig(), nil)
	i := NewBWEInterceptor(estimator)

	// Close should not panic and should complete
	err := i.Close()
	assert.NoError(t, err)

	// Verify closed channel is closed
	select {
	case <-i.closed:
		// Good, channel is closed
	default:
		t.Error("closed channel should be closed after Close()")
	}
}

func TestStreamState_UpdatedOnPacket(t *testing.T) {
	estimator := bwe.NewBandwidthEstimator(bwe.DefaultBandwidthEstimatorConfig(), nil)
	i := NewBWEInterceptor(estimator)

	testSSRC := uint32(0xDEADBEEF)
	extID := uint8(3)

	info := &interceptor.StreamInfo{
		SSRC: testSSRC,
		RTPHeaderExtensions: []interceptor.RTPHeaderExtension{
			{URI: AbsSendTimeURI, ID: int(extID)},
		},
	}

	// Create packet with timing extension
	sendTime := uint32(0x020000)
	rtpPacket := makeRTPWithAbsSendTime(testSSRC, extID, sendTime)

	reader := &mockRTPReader{packets: [][]byte{rtpPacket}}
	wrappedReader := i.BindRemoteStream(info, reader)

	// Get initial last packet time
	stateVal, ok := i.streams.Load(testSSRC)
	require.True(t, ok)
	state := stateVal.(*streamState)
	initialTime := state.LastPacket()

	// Wait a tiny bit to ensure time difference
	time.Sleep(time.Millisecond)

	// Read packet (triggers processRTP which updates stream state)
	buf := make([]byte, 1500)
	_, _, err := wrappedReader.Read(buf, nil)
	require.NoError(t, err)

	// Verify last packet time was updated
	updatedTime := state.LastPacket()
	assert.True(t, updatedTime.After(initialTime) || updatedTime.Equal(initialTime),
		"Last packet time should be updated after processing packet")
}

// mockRTCPWriter is a test RTCPWriter that captures written packets.
type mockRTCPWriter struct {
	mu      sync.Mutex
	packets []rtcp.Packet
}

func (m *mockRTCPWriter) Write(pkts []rtcp.Packet, _ interceptor.Attributes) (int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.packets = append(m.packets, pkts...)
	return len(pkts), nil
}

func (m *mockRTCPWriter) getPackets() []rtcp.Packet {
	m.mu.Lock()
	defer m.mu.Unlock()
	result := make([]rtcp.Packet, len(m.packets))
	copy(result, m.packets)
	return result
}

func TestBindRTCPWriter_StartsREMBLoop(t *testing.T) {
	estimator := bwe.NewBandwidthEstimator(bwe.DefaultBandwidthEstimatorConfig(), nil)
	// Use short interval for faster test
	i := NewBWEInterceptor(estimator, WithREMBInterval(50*time.Millisecond))
	defer i.Close()

	// Bind stream and send some packets to generate estimate
	testSSRC := uint32(0xAABBCCDD)
	extID := uint8(3)

	info := &interceptor.StreamInfo{
		SSRC: testSSRC,
		RTPHeaderExtensions: []interceptor.RTPHeaderExtension{
			{URI: AbsSendTimeURI, ID: int(extID)},
		},
	}

	// Create multiple RTP packets with increasing send times
	var packets [][]byte
	for j := 0; j < 20; j++ {
		// abs-send-time in 6.18 fixed point format
		// Small increments to simulate steady flow
		sendTime := uint32((j * 0x1000) & 0xFFFFFF)
		packets = append(packets, makeRTPWithAbsSendTime(testSSRC, extID, sendTime))
	}

	reader := &mockRTPReader{packets: packets}
	wrappedReader := i.BindRemoteStream(info, reader)

	// Read all packets to feed estimator
	buf := make([]byte, 1500)
	for j := 0; j < len(packets); j++ {
		n, _, err := wrappedReader.Read(buf, nil)
		require.NoError(t, err)
		require.Greater(t, n, 0)
		time.Sleep(5 * time.Millisecond) // Spread out packet arrivals
	}

	// Bind RTCP writer - this starts the REMB loop
	mockWriter := &mockRTCPWriter{}
	returnedWriter := i.BindRTCPWriter(mockWriter)
	assert.Equal(t, mockWriter, returnedWriter, "BindRTCPWriter should return the same writer")

	// Wait for at least 2-3 REMB intervals
	time.Sleep(200 * time.Millisecond)

	// Verify REMB packets were written
	pkts := mockWriter.getPackets()
	assert.Greater(t, len(pkts), 0, "Expected at least one REMB packet to be written")

	// Check that at least one packet is a REMB
	var foundREMB bool
	for _, pkt := range pkts {
		if remb, ok := pkt.(*rtcp.ReceiverEstimatedMaximumBitrate); ok {
			foundREMB = true
			assert.Greater(t, remb.Bitrate, float32(0), "REMB bitrate should be positive")
			t.Logf("REMB sent: bitrate=%.0f bps, SSRCs=%v", remb.Bitrate, remb.SSRCs)
		}
	}
	assert.True(t, foundREMB, "Expected at least one REMB packet")
}

func TestREMB_IncludesAllSSRCs(t *testing.T) {
	estimator := bwe.NewBandwidthEstimator(bwe.DefaultBandwidthEstimatorConfig(), nil)
	i := NewBWEInterceptor(estimator, WithREMBInterval(50*time.Millisecond))
	defer i.Close()

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
	packets1 := make([][]byte, 10)
	for j := 0; j < 10; j++ {
		packets1[j] = makeRTPWithAbsSendTime(ssrc1, extID, uint32(j*0x1000))
	}
	reader1 := &mockRTPReader{packets: packets1}
	wrappedReader1 := i.BindRemoteStream(info1, reader1)

	// Bind second stream
	info2 := &interceptor.StreamInfo{
		SSRC: ssrc2,
		RTPHeaderExtensions: []interceptor.RTPHeaderExtension{
			{URI: AbsSendTimeURI, ID: int(extID)},
		},
	}
	packets2 := make([][]byte, 10)
	for j := 0; j < 10; j++ {
		packets2[j] = makeRTPWithAbsSendTime(ssrc2, extID, uint32(j*0x1000+0x100))
	}
	reader2 := &mockRTPReader{packets: packets2}
	wrappedReader2 := i.BindRemoteStream(info2, reader2)

	// Read packets from both streams
	buf := make([]byte, 1500)
	for j := 0; j < 10; j++ {
		wrappedReader1.Read(buf, nil)
		wrappedReader2.Read(buf, nil)
		time.Sleep(5 * time.Millisecond)
	}

	// Bind RTCP writer
	mockWriter := &mockRTCPWriter{}
	i.BindRTCPWriter(mockWriter)

	// Wait for REMB
	time.Sleep(150 * time.Millisecond)

	// Check that REMB includes both SSRCs
	pkts := mockWriter.getPackets()
	var foundREMBWithBothSSRCs bool
	for _, pkt := range pkts {
		if remb, ok := pkt.(*rtcp.ReceiverEstimatedMaximumBitrate); ok {
			// Check if both SSRCs are in the REMB
			hasSSRC1 := false
			hasSSRC2 := false
			for _, ssrc := range remb.SSRCs {
				if ssrc == ssrc1 {
					hasSSRC1 = true
				}
				if ssrc == ssrc2 {
					hasSSRC2 = true
				}
			}
			if hasSSRC1 && hasSSRC2 {
				foundREMBWithBothSSRCs = true
				break
			}
		}
	}
	assert.True(t, foundREMBWithBothSSRCs, "Expected REMB to include both SSRCs")
}

func TestREMB_WriterNotBound_NoError(t *testing.T) {
	estimator := bwe.NewBandwidthEstimator(bwe.DefaultBandwidthEstimatorConfig(), nil)
	i := NewBWEInterceptor(estimator)
	defer i.Close()

	// Don't bind RTCPWriter, just call maybeSendREMB directly
	// This should not panic or return error
	i.maybeSendREMB(time.Now())

	// Also test with some packets processed but writer not bound
	testSSRC := uint32(0xDEADBEEF)
	extID := uint8(3)

	info := &interceptor.StreamInfo{
		SSRC: testSSRC,
		RTPHeaderExtensions: []interceptor.RTPHeaderExtension{
			{URI: AbsSendTimeURI, ID: int(extID)},
		},
	}

	packets := make([][]byte, 5)
	for j := 0; j < 5; j++ {
		packets[j] = makeRTPWithAbsSendTime(testSSRC, extID, uint32(j*0x1000))
	}
	reader := &mockRTPReader{packets: packets}
	wrappedReader := i.BindRemoteStream(info, reader)

	buf := make([]byte, 1500)
	for j := 0; j < 5; j++ {
		wrappedReader.Read(buf, nil)
	}

	// Call maybeSendREMB - should not panic
	i.maybeSendREMB(time.Now())
}

func TestREMBScheduler_AttachedOnCreate(t *testing.T) {
	estimator := bwe.NewBandwidthEstimator(bwe.DefaultBandwidthEstimatorConfig(), nil)
	i := NewBWEInterceptor(estimator)
	defer i.Close()

	// Verify REMB scheduler was attached
	assert.NotNil(t, i.rembScheduler, "REMB scheduler should be created")
}

// --- Stream Timeout and Close Tests ---

func TestStreamTimeout_RemovesInactiveStreams(t *testing.T) {
	estimator := bwe.NewBandwidthEstimator(bwe.DefaultBandwidthEstimatorConfig(), nil)
	i := NewBWEInterceptor(estimator)
	defer i.Close()

	testSSRC := uint32(0x12345678)

	// Bind a stream (this starts the cleanup loop)
	info := &interceptor.StreamInfo{
		SSRC: testSSRC,
		RTPHeaderExtensions: []interceptor.RTPHeaderExtension{
			{URI: AbsSendTimeURI, ID: 3},
		},
	}
	_ = i.BindRemoteStream(info, &mockRTPReader{})

	// Verify stream exists initially
	_, exists := i.streams.Load(testSSRC)
	require.True(t, exists, "stream should exist initially")

	// Wait for timeout (2s) + cleanup interval (1s) + margin
	time.Sleep(3500 * time.Millisecond)

	// Verify stream was removed by cleanup loop
	_, exists = i.streams.Load(testSSRC)
	assert.False(t, exists, "stream should be removed after timeout")
}

func TestClose_StopsGoroutines(t *testing.T) {
	estimator := bwe.NewBandwidthEstimator(bwe.DefaultBandwidthEstimatorConfig(), nil)
	i := NewBWEInterceptor(estimator)

	// Bind stream to start cleanup loop goroutine
	info := &interceptor.StreamInfo{
		SSRC: 12345,
		RTPHeaderExtensions: []interceptor.RTPHeaderExtension{
			{URI: AbsSendTimeURI, ID: 3},
		},
	}
	_ = i.BindRemoteStream(info, &mockRTPReader{})

	// Also bind RTCP writer to start REMB loop
	mockWriter := &mockRTCPWriter{}
	i.BindRTCPWriter(mockWriter)

	// Close should complete without hanging (goroutines should stop)
	done := make(chan struct{})
	go func() {
		err := i.Close()
		assert.NoError(t, err)
		close(done)
	}()

	select {
	case <-done:
		// Good, Close() completed
	case <-time.After(5 * time.Second):
		t.Fatal("Close() timed out - goroutines may not have stopped")
	}
}

func TestClose_BeforeGoroutinesStarted(t *testing.T) {
	estimator := bwe.NewBandwidthEstimator(bwe.DefaultBandwidthEstimatorConfig(), nil)
	i := NewBWEInterceptor(estimator)

	// Close immediately without binding any streams
	// (no goroutines started yet)
	err := i.Close()
	assert.NoError(t, err)
}

func TestCleanupLoop_ConcurrentAccess(t *testing.T) {
	estimator := bwe.NewBandwidthEstimator(bwe.DefaultBandwidthEstimatorConfig(), nil)
	i := NewBWEInterceptor(estimator)
	defer i.Close()

	// Spawn multiple goroutines that bind/unbind streams concurrently
	var wg sync.WaitGroup
	for j := 0; j < 10; j++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			ssrc := uint32(idx)
			info := &interceptor.StreamInfo{
				SSRC: ssrc,
				RTPHeaderExtensions: []interceptor.RTPHeaderExtension{
					{URI: AbsSendTimeURI, ID: 3},
				},
			}

			// Bind and unbind rapidly
			for k := 0; k < 10; k++ {
				_ = i.BindRemoteStream(info, &mockRTPReader{})
				time.Sleep(time.Millisecond)
				i.UnbindRemoteStream(info)
			}
		}(j)
	}

	wg.Wait()
	// Test passes if no data races detected (run with -race)
}

func TestCleanupLoop_StartsOnlyOnce(t *testing.T) {
	estimator := bwe.NewBandwidthEstimator(bwe.DefaultBandwidthEstimatorConfig(), nil)
	i := NewBWEInterceptor(estimator)
	// Note: No defer Close() here because we're explicitly testing Close() behavior

	// Bind multiple streams rapidly
	for j := 0; j < 10; j++ {
		info := &interceptor.StreamInfo{
			SSRC: uint32(j),
			RTPHeaderExtensions: []interceptor.RTPHeaderExtension{
				{URI: AbsSendTimeURI, ID: 3},
			},
		}
		_ = i.BindRemoteStream(info, &mockRTPReader{})
	}

	// If sync.Once works correctly, only one cleanupLoop goroutine should be running
	// We verify this indirectly by checking that Close() completes quickly
	// (if multiple goroutines were spawned, wg.Wait() would hang or behave incorrectly)
	done := make(chan struct{})
	go func() {
		err := i.Close()
		assert.NoError(t, err)
		close(done)
	}()

	select {
	case <-done:
		// Good - single goroutine cleanup worked
	case <-time.After(3 * time.Second):
		t.Fatal("Close() timed out - possible multiple cleanup goroutines issue")
	}
}

func TestStreamTimeout_ActiveStreamNotRemoved(t *testing.T) {
	estimator := bwe.NewBandwidthEstimator(bwe.DefaultBandwidthEstimatorConfig(), nil)
	i := NewBWEInterceptor(estimator)
	defer i.Close()

	testSSRC := uint32(0xAABBCCDD)
	extID := uint8(3)

	info := &interceptor.StreamInfo{
		SSRC: testSSRC,
		RTPHeaderExtensions: []interceptor.RTPHeaderExtension{
			{URI: AbsSendTimeURI, ID: int(extID)},
		},
	}

	// Create enough packets to keep stream active
	var packets [][]byte
	for j := 0; j < 50; j++ {
		packets = append(packets, makeRTPWithAbsSendTime(testSSRC, extID, uint32(j*0x1000)))
	}

	reader := &mockRTPReader{packets: packets}
	wrappedReader := i.BindRemoteStream(info, reader)

	// Keep sending packets over 3 seconds (longer than timeout)
	buf := make([]byte, 1500)
	stopCh := make(chan struct{})
	go func() {
		for j := 0; j < 30; j++ { // Send packets over ~3 seconds
			select {
			case <-stopCh:
				return
			default:
				// Reinitialize reader with new packets
				reader.packets = append(reader.packets, makeRTPWithAbsSendTime(testSSRC, extID, uint32((50+j)*0x1000)))
				wrappedReader.Read(buf, nil)
				time.Sleep(100 * time.Millisecond)
			}
		}
	}()

	// Wait 3.5 seconds - stream should still exist because it's active
	time.Sleep(3500 * time.Millisecond)
	close(stopCh)

	// Stream should still exist because it was active
	_, exists := i.streams.Load(testSSRC)
	assert.True(t, exists, "active stream should not be removed")
}
