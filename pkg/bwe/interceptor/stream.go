package interceptor

import (
	"sync/atomic"
	"time"
)

// streamState tracks per-stream state for the BWE interceptor.
//
// This type is unexported and used internally to track:
// - Stream SSRC for identification
// - Last packet arrival time for timeout detection
//
// The lastPacketTime uses atomic.Value for thread-safe access because:
// - BindRemoteStream reader updates it on every incoming packet
// - cleanupLoop reads it periodically to detect inactive streams
// - Both operations happen concurrently without explicit locking
type streamState struct {
	ssrc           uint32
	lastPacketTime atomic.Value // stores time.Time
}

// newStreamState creates a new stream state for the given SSRC.
// The lastPacketTime is initialized to the current time.
func newStreamState(ssrc uint32) *streamState {
	s := &streamState{
		ssrc: ssrc,
	}
	s.lastPacketTime.Store(time.Now())
	return s
}

// UpdateLastPacket stores the given time as the last packet arrival time.
// This is called on every incoming RTP packet for this stream.
func (s *streamState) UpdateLastPacket(t time.Time) {
	s.lastPacketTime.Store(t)
}

// LastPacket returns the arrival time of the most recent packet for this stream.
// Used by the cleanup loop to detect inactive streams (>2s without packets).
func (s *streamState) LastPacket() time.Time {
	return s.lastPacketTime.Load().(time.Time)
}

// SSRC returns the stream's SSRC identifier.
func (s *streamState) SSRC() uint32 {
	return s.ssrc
}
