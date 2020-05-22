// Copyright (c) 2020 by Juliusz Chroboczek.

// This is not open source software.  Copy it, and I'll break into your
// house and tell your three year-old that Santa doesn't exist.

package main

import (
	"sync"
	"sync/atomic"

	"sfu/estimator"
	"sfu/jitter"
	"sfu/packetcache"

	"github.com/pion/webrtc/v2"
)

type connection interface {
	getPC() *webrtc.PeerConnection
}

type upTrack struct {
	track                *webrtc.Track
	label                string
	rate                 *estimator.Estimator
	cache                *packetcache.Cache
	jitter               *jitter.Estimator
	maxBitrate           uint64
	lastPLI              uint64
	lastSenderReport     uint32
	lastSenderReportTime uint32

	localCh    chan struct{} // signals that local has changed
	writerDone chan struct{} // closed when the loop dies

	mu    sync.Mutex
	local []*downTrack
}

func (up *upTrack) notifyLocal() {
	var s struct{}
	select {
	case up.localCh <- s:
	case <-up.writerDone:
	}
}

func (up *upTrack) addLocal(local *downTrack) {
	up.mu.Lock()
	for _, t := range up.local {
		if t == local {
			up.mu.Unlock()
			return
		}
	}
	up.local = append(up.local, local)
	up.mu.Unlock()
	up.notifyLocal()
}

func (up *upTrack) delLocal(local *downTrack) bool {
	up.mu.Lock()
	for i, l := range up.local {
		if l == local {
			up.local = append(up.local[:i], up.local[i+1:]...)
			up.mu.Unlock()
			up.notifyLocal()
			return true
		}
	}
	up.mu.Unlock()
	return false
}

func (up *upTrack) getLocal() []*downTrack {
	up.mu.Lock()
	defer up.mu.Unlock()
	local := make([]*downTrack, len(up.local))
	copy(local, up.local)
	return local
}

func (up *upTrack) hasRtcpFb(tpe, parameter string) bool {
	for _, fb := range up.track.Codec().RTCPFeedback {
		if fb.Type == tpe && fb.Parameter == parameter {
			return true
		}
	}
	return false
}

type upConnection struct {
	id            string
	label         string
	pc            *webrtc.PeerConnection
	tracks        []*upTrack
	labels        map[string]string
	iceCandidates []*webrtc.ICECandidateInit
}

func (up *upConnection) getPC() *webrtc.PeerConnection {
	return up.pc
}

func getUpMid(pc *webrtc.PeerConnection, track *webrtc.Track) string {
	for _, t := range pc.GetTransceivers() {
		if t.Receiver() != nil && t.Receiver().Track() == track {
			return t.Mid()
		}
	}
	return ""
}

func (up *upConnection) complete() bool {
	for mid, _ := range up.labels {
		found := false
		for _, t := range up.tracks {
			m := getUpMid(up.pc, t.track)
			if m == mid {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	return true
}

type bitrate struct {
	bitrate      uint64
	microseconds uint64
}

const receiverReportTimeout = 8000000

func (br *bitrate) Set(bitrate uint64, now uint64) {
	// this is racy -- a reader might read the
	// data between the two writes.  This shouldn't
	// matter, we'll recover at the next sample.
	atomic.StoreUint64(&br.bitrate, bitrate)
	atomic.StoreUint64(&br.microseconds, now)
}

func (br *bitrate) Get(now uint64) uint64 {
	ts := atomic.LoadUint64(&br.microseconds)
	if now < ts || now > ts+receiverReportTimeout {
		return ^uint64(0)
	}
	return atomic.LoadUint64(&br.bitrate)
}

type receiverStats struct {
	loss         uint32
	jitter       uint32
	microseconds uint64
}

func (s *receiverStats) Set(loss uint8, jitter uint32, now uint64) {
	atomic.StoreUint32(&s.loss, uint32(loss))
	atomic.StoreUint32(&s.jitter, jitter)
	atomic.StoreUint64(&s.microseconds, now)
}

func (s *receiverStats) Get(now uint64) (uint8, uint32) {
	ts := atomic.LoadUint64(&s.microseconds)
	if now < ts || now > ts+receiverReportTimeout {
		return 0, 0
	}
	return uint8(atomic.LoadUint32(&s.loss)), atomic.LoadUint32(&s.jitter)
}

type downTrack struct {
	track          *webrtc.Track
	remote         *upTrack
	maxLossBitrate *bitrate
	maxREMBBitrate *bitrate
	rate           *estimator.Estimator
	stats          *receiverStats
}

func (down *downTrack) GetMaxBitrate(now uint64) uint64 {
	br1 := down.maxLossBitrate.Get(now)
	br2 := down.maxREMBBitrate.Get(now)
	if br1 < br2 {
		return br1
	}
	return br2
}

type downConnection struct {
	id            string
	pc            *webrtc.PeerConnection
	remote        *upConnection
	tracks        []*downTrack
	iceCandidates []*webrtc.ICECandidateInit
}

func (down *downConnection) getPC() *webrtc.PeerConnection {
	return down.pc
}
