// Package conn defines interfaces for connections and tracks.
package conn

import (
	"errors"

	"github.com/pion/webrtc/v3"
)

var ErrConnectionClosed = errors.New("connection is closed")

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
	Label() string
	Codec() webrtc.RTPCodecCapability
	// GetPacket fetches a recent packet.  Returns 0 if the packet is
	// not in cache, and, in that case, optionally schedules a NACK.
	GetPacket(seqno uint16, result []byte, nack bool) uint16
	RequestKeyframe() error
}

// Type Down represents a connection in the server to client direction.
type Down interface {
}

// Type DownTrack represents a track in the server to client direction.
type DownTrack interface {
	Write(buf []byte) (int, error)
	SetTimeOffset(ntp uint64, rtp uint32)
	SetCname(string)
	GetMaxBitrate() (uint64, int, int)
}
