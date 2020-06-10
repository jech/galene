// Copyright (c) 2020 by Juliusz Chroboczek.

// This is not open source software.  Copy it, and I'll break into your
// house and tell your three year-old that Santa doesn't exist.

package main

import (
	"errors"

	"github.com/pion/rtp"
	"github.com/pion/webrtc/v2"
)

var ErrConnectionClosed = errors.New("connection is closed")
var ErrKeyframeNeeded = errors.New("keyframe needed")

type upConnection interface {
	addLocal(downConnection) error
	delLocal(downConnection) bool
	Id() string
	Label() string
}

type upTrack interface {
	addLocal(downTrack) error
	delLocal(downTrack) bool
	Label() string
	Codec() *webrtc.RTPCodec
	// get a recent packet.  Returns 0 if the packet is not in cache.
	getRTP(seqno uint16, result []byte) uint16
	// returns the last timestamp, if possible
	getTimestamp() (uint32, bool)
}

type downConnection interface {
}

type downTrack interface {
	WriteRTP(packat *rtp.Packet) error
	Accumulate(bytes uint32)
	GetMaxBitrate(now uint64) uint64
	setTimeOffset(ntp uint64, rtp uint32)
}
