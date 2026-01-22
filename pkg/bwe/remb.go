package bwe

import (
	"github.com/pion/rtcp"
)

// REMBPacket represents a REMB (Receiver Estimated Maximum Bitrate) packet.
// This is a convenience wrapper around pion/rtcp.ReceiverEstimatedMaximumBitrate.
type REMBPacket struct {
	// SenderSSRC is the SSRC of the sender of this REMB packet (us, the receiver).
	// This is typically set by the transport layer.
	SenderSSRC uint32

	// Bitrate is the estimated maximum bitrate in bits per second.
	Bitrate uint64

	// SSRCs is the list of media source SSRCs this estimate applies to.
	SSRCs []uint32
}

// BuildREMB creates a REMB RTCP packet from the given parameters.
// Returns the marshaled packet bytes ready to send.
//
// Parameters:
//   - senderSSRC: SSRC of this RTCP packet sender (receiver endpoint)
//   - bitrateBps: Estimated maximum bitrate in bits per second
//   - mediaSSRCs: List of media SSRCs this estimate applies to
//
// The bitrate is encoded using REMB's mantissa+exponent format:
//   - 6-bit exponent
//   - 18-bit mantissa
//
// This encoding is handled by pion/rtcp.
func BuildREMB(senderSSRC uint32, bitrateBps uint64, mediaSSRCs []uint32) ([]byte, error) {
	pkt := &rtcp.ReceiverEstimatedMaximumBitrate{
		SenderSSRC: senderSSRC,
		Bitrate:    float32(bitrateBps),
		SSRCs:      mediaSSRCs,
	}
	return pkt.Marshal()
}

// ParseREMB parses a REMB packet from raw bytes.
// Useful for testing and debugging.
func ParseREMB(data []byte) (*REMBPacket, error) {
	pkt := &rtcp.ReceiverEstimatedMaximumBitrate{}
	if err := pkt.Unmarshal(data); err != nil {
		return nil, err
	}
	return &REMBPacket{
		SenderSSRC: pkt.SenderSSRC,
		Bitrate:    uint64(pkt.Bitrate),
		SSRCs:      pkt.SSRCs,
	}, nil
}

// Marshal marshals a REMBPacket to bytes.
func (p *REMBPacket) Marshal() ([]byte, error) {
	return BuildREMB(p.SenderSSRC, p.Bitrate, p.SSRCs)
}
