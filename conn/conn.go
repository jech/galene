// Package conn defines interfaces for connections and tracks.
package conn

import (
	"errors"

	"github.com/pion/rtp"
	"github.com/pion/webrtc/v3"
)

var ErrConnectionClosed = errors.New("connection is closed")
var ErrKeyframeNeeded = errors.New("keyframe needed")

// Type Up represents a connection in the client to server direction.
type Up interface {
	AddLocal(Down) error
	DelLocal(Down) bool
	Id() string
	Label() string
	User() (string, string)
}

// Type UpTrack represents a track in the client to server direction.
type UpTrack interface {
	AddLocal(DownTrack) error
	DelLocal(DownTrack) bool
	Kind() webrtc.RTPCodecType
	Codec() webrtc.RTPCodecCapability
	// get a recent packet.  Returns 0 if the packet is not in cache.
	GetRTP(seqno uint16, result []byte) uint16
	Nack(conn Up, seqnos []uint16) error
}

// Type Down represents a connection in the server to client direction.
type Down interface {
	GetMaxBitrate(now uint64) uint64
}

// Type DownTrack represents a track in the server to client direction.
type DownTrack interface {
	WriteRTP(packat *rtp.Packet) error
	Accumulate(bytes uint32)
	SetTimeOffset(ntp uint64, rtp uint32)
	SetCname(string)
}
