package interceptor

import (
	"sync"
	"time"

	"bwe/pkg/bwe"
)

// packetInfoPool is a sync.Pool for reusing PacketInfo objects.
// This reduces GC pressure when processing high volumes of RTP packets.
var packetInfoPool = sync.Pool{
	New: func() any {
		return &bwe.PacketInfo{}
	},
}

// getPacketInfo retrieves a PacketInfo from the pool.
// The returned PacketInfo has all fields at their zero values.
func getPacketInfo() *bwe.PacketInfo {
	return packetInfoPool.Get().(*bwe.PacketInfo)
}

// putPacketInfo returns a PacketInfo to the pool after resetting its fields.
// This ensures the next Get() returns a clean object.
func putPacketInfo(pkt *bwe.PacketInfo) {
	// Reset all fields to zero values
	pkt.ArrivalTime = time.Time{}
	pkt.SendTime = 0
	pkt.Size = 0
	pkt.SSRC = 0
	packetInfoPool.Put(pkt)
}
