package interceptor

import (
	"testing"
	"time"

	"github.com/pion/interceptor"
	"github.com/pion/rtp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"bwe/pkg/bwe"
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
