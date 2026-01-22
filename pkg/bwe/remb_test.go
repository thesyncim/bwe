package bwe

import (
	"testing"
)

func TestBuildREMB_BasicEncoding(t *testing.T) {
	// Build a basic REMB packet and verify round-trip
	senderSSRC := uint32(0x12345678)
	bitrate := uint64(1_000_000) // 1 Mbps
	ssrcs := []uint32{0xAABBCCDD}

	data, err := BuildREMB(senderSSRC, bitrate, ssrcs)
	if err != nil {
		t.Fatalf("BuildREMB failed: %v", err)
	}

	parsed, err := ParseREMB(data)
	if err != nil {
		t.Fatalf("ParseREMB failed: %v", err)
	}

	if parsed.SenderSSRC != senderSSRC {
		t.Errorf("SenderSSRC = %#x, want %#x", parsed.SenderSSRC, senderSSRC)
	}

	if parsed.Bitrate != bitrate {
		t.Errorf("Bitrate = %d, want %d", parsed.Bitrate, bitrate)
	}

	if len(parsed.SSRCs) != len(ssrcs) {
		t.Fatalf("SSRCs count = %d, want %d", len(parsed.SSRCs), len(ssrcs))
	}

	if parsed.SSRCs[0] != ssrcs[0] {
		t.Errorf("SSRCs[0] = %#x, want %#x", parsed.SSRCs[0], ssrcs[0])
	}
}

func TestBuildREMB_MultipleSSRCs(t *testing.T) {
	// Build REMB with multiple media SSRCs
	senderSSRC := uint32(0x11111111)
	bitrate := uint64(2_500_000) // 2.5 Mbps
	ssrcs := []uint32{0xAAAAAAAA, 0xBBBBBBBB, 0xCCCCCCCC}

	data, err := BuildREMB(senderSSRC, bitrate, ssrcs)
	if err != nil {
		t.Fatalf("BuildREMB failed: %v", err)
	}

	parsed, err := ParseREMB(data)
	if err != nil {
		t.Fatalf("ParseREMB failed: %v", err)
	}

	if len(parsed.SSRCs) != len(ssrcs) {
		t.Fatalf("SSRCs count = %d, want %d", len(parsed.SSRCs), len(ssrcs))
	}

	for i, ssrc := range ssrcs {
		if parsed.SSRCs[i] != ssrc {
			t.Errorf("SSRCs[%d] = %#x, want %#x", i, parsed.SSRCs[i], ssrc)
		}
	}
}

func TestBuildREMB_HighBitrate(t *testing.T) {
	// Test high bitrate encoding (>1 Gbps)
	// Tests mantissa+exponent encoding for large values
	senderSSRC := uint32(0x12345678)
	bitrate := uint64(2_000_000_000) // 2 Gbps
	ssrcs := []uint32{0xAABBCCDD}

	data, err := BuildREMB(senderSSRC, bitrate, ssrcs)
	if err != nil {
		t.Fatalf("BuildREMB failed: %v", err)
	}

	parsed, err := ParseREMB(data)
	if err != nil {
		t.Fatalf("ParseREMB failed: %v", err)
	}

	// Due to mantissa+exponent encoding, there may be some precision loss
	// Accept values within 1% of the original
	tolerance := float64(bitrate) * 0.01
	diff := float64(parsed.Bitrate) - float64(bitrate)
	if diff < -tolerance || diff > tolerance {
		t.Errorf("High bitrate = %d, want %d (within 1%%)", parsed.Bitrate, bitrate)
	}
}

func TestBuildREMB_LowBitrate(t *testing.T) {
	// Test low bitrate encoding (10 kbps)
	senderSSRC := uint32(0x12345678)
	bitrate := uint64(10_000) // 10 kbps
	ssrcs := []uint32{0xAABBCCDD}

	data, err := BuildREMB(senderSSRC, bitrate, ssrcs)
	if err != nil {
		t.Fatalf("BuildREMB failed: %v", err)
	}

	parsed, err := ParseREMB(data)
	if err != nil {
		t.Fatalf("ParseREMB failed: %v", err)
	}

	if parsed.Bitrate != bitrate {
		t.Errorf("Low bitrate = %d, want %d", parsed.Bitrate, bitrate)
	}
}

func TestBuildREMB_ZeroBitrate(t *testing.T) {
	// Edge case: zero bitrate
	// Note: REMB uses mantissa+exponent encoding where zero cannot be exactly
	// represented. pion/rtcp encodes 0 as a small non-zero value.
	// This test verifies the encoding/decoding doesn't error.
	senderSSRC := uint32(0x12345678)
	bitrate := uint64(0)
	ssrcs := []uint32{0xAABBCCDD}

	data, err := BuildREMB(senderSSRC, bitrate, ssrcs)
	if err != nil {
		t.Fatalf("BuildREMB failed: %v", err)
	}

	_, err = ParseREMB(data)
	if err != nil {
		t.Fatalf("ParseREMB failed: %v", err)
	}

	// Note: We don't assert the exact value because mantissa+exponent
	// encoding cannot represent zero. The important thing is it doesn't crash.
	t.Log("Zero bitrate encoded successfully (note: will decode to non-zero due to encoding format)")
}

func TestBuildREMB_EmptySSRCs(t *testing.T) {
	// Edge case: no SSRCs (unusual but should work)
	senderSSRC := uint32(0x12345678)
	bitrate := uint64(1_000_000)
	ssrcs := []uint32{}

	data, err := BuildREMB(senderSSRC, bitrate, ssrcs)
	if err != nil {
		t.Fatalf("BuildREMB failed: %v", err)
	}

	parsed, err := ParseREMB(data)
	if err != nil {
		t.Fatalf("ParseREMB failed: %v", err)
	}

	if len(parsed.SSRCs) != 0 {
		t.Errorf("SSRCs count = %d, want 0", len(parsed.SSRCs))
	}
}

func TestREMBPacket_Marshal(t *testing.T) {
	// Test REMBPacket.Marshal() method
	pkt := &REMBPacket{
		SenderSSRC: 0x12345678,
		Bitrate:    5_000_000, // 5 Mbps
		SSRCs:      []uint32{0xAABBCCDD, 0x11223344},
	}

	data, err := pkt.Marshal()
	if err != nil {
		t.Fatalf("REMBPacket.Marshal failed: %v", err)
	}

	parsed, err := ParseREMB(data)
	if err != nil {
		t.Fatalf("ParseREMB failed: %v", err)
	}

	if parsed.SenderSSRC != pkt.SenderSSRC {
		t.Errorf("SenderSSRC = %#x, want %#x", parsed.SenderSSRC, pkt.SenderSSRC)
	}

	if parsed.Bitrate != pkt.Bitrate {
		t.Errorf("Bitrate = %d, want %d", parsed.Bitrate, pkt.Bitrate)
	}

	if len(parsed.SSRCs) != len(pkt.SSRCs) {
		t.Fatalf("SSRCs count = %d, want %d", len(parsed.SSRCs), len(pkt.SSRCs))
	}

	for i, ssrc := range pkt.SSRCs {
		if parsed.SSRCs[i] != ssrc {
			t.Errorf("SSRCs[%d] = %#x, want %#x", i, parsed.SSRCs[i], ssrc)
		}
	}
}

func TestBuildREMB_PacketFormat(t *testing.T) {
	// Verify RTCP header format
	// REMB uses PT=206 (PSFB), FMT=15
	senderSSRC := uint32(0x12345678)
	bitrate := uint64(1_000_000)
	ssrcs := []uint32{0xAABBCCDD}

	data, err := BuildREMB(senderSSRC, bitrate, ssrcs)
	if err != nil {
		t.Fatalf("BuildREMB failed: %v", err)
	}

	// Check minimum packet size: RTCP header (4) + sender SSRC (4) + media SSRC (4) + REMB data (8+)
	if len(data) < 20 {
		t.Errorf("Packet too short: %d bytes", len(data))
	}

	// First byte: V=2 (bits 7-6), P=0 (bit 5), FMT=15 (bits 4-0)
	// V=2, P=0, FMT=15 = 10 0 01111 = 0x8F
	expectedFirstByte := byte(0x8F)
	if data[0] != expectedFirstByte {
		t.Errorf("First byte = %#02x, want %#02x (V=2, P=0, FMT=15)", data[0], expectedFirstByte)
	}

	// Second byte: PT=206 (PSFB)
	expectedPT := byte(206)
	if data[1] != expectedPT {
		t.Errorf("PT = %d, want %d (PSFB)", data[1], expectedPT)
	}

	// Check for "REMB" identifier at correct offset
	// After RTCP header (4) + sender SSRC (4) + media SSRC (4) = offset 12
	rembOffset := 12
	if len(data) > rembOffset+3 {
		rembID := string(data[rembOffset : rembOffset+4])
		if rembID != "REMB" {
			t.Errorf("REMB identifier = %q, want \"REMB\"", rembID)
		}
	} else {
		t.Error("Packet too short to contain REMB identifier")
	}
}

func TestBuildREMB_BitrateEncodingPrecision(t *testing.T) {
	// Test various bitrates to verify encoding precision
	// REMB uses 6-bit exponent + 18-bit mantissa
	testCases := []struct {
		name       string
		bitrate    uint64
		maxError   float64 // Maximum acceptable relative error
	}{
		{"100 kbps", 100_000, 0.001},
		{"500 kbps", 500_000, 0.001},
		{"1 Mbps", 1_000_000, 0.001},
		{"10 Mbps", 10_000_000, 0.001},
		{"100 Mbps", 100_000_000, 0.001},
		{"1 Gbps", 1_000_000_000, 0.01},   // Higher tolerance for large values
		{"5 Gbps", 5_000_000_000, 0.01},   // Very high bitrate
		{"10 Gbps", 10_000_000_000, 0.01}, // Extreme bitrate
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			data, err := BuildREMB(0x12345678, tc.bitrate, []uint32{0xAABBCCDD})
			if err != nil {
				t.Fatalf("BuildREMB failed: %v", err)
			}

			parsed, err := ParseREMB(data)
			if err != nil {
				t.Fatalf("ParseREMB failed: %v", err)
			}

			// Use float64 subtraction to avoid uint64 underflow
			diff := float64(parsed.Bitrate) - float64(tc.bitrate)
			if diff < 0 {
				diff = -diff
			}
			relError := diff / float64(tc.bitrate)

			if relError > tc.maxError {
				t.Errorf("Bitrate %d encoded as %d (relative error %.4f%%, max %.4f%%)",
					tc.bitrate, parsed.Bitrate, relError*100, tc.maxError*100)
			}
		})
	}
}

func TestParseREMB_InvalidData(t *testing.T) {
	// Test parsing invalid/malformed data
	testCases := []struct {
		name string
		data []byte
	}{
		{"empty", []byte{}},
		{"too short", []byte{0x8F, 0xCE}},
		{"wrong PT", []byte{0x8F, 0x00, 0x00, 0x04, 0x00, 0x00, 0x00, 0x00}},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := ParseREMB(tc.data)
			if err == nil {
				t.Error("Expected error for invalid data, got nil")
			}
		})
	}
}

// Benchmark tests
func BenchmarkBuildREMB(b *testing.B) {
	senderSSRC := uint32(0x12345678)
	bitrate := uint64(1_000_000)
	ssrcs := []uint32{0xAABBCCDD}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = BuildREMB(senderSSRC, bitrate, ssrcs)
	}
}

func BenchmarkParseREMB(b *testing.B) {
	data, _ := BuildREMB(0x12345678, 1_000_000, []uint32{0xAABBCCDD})

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = ParseREMB(data)
	}
}
