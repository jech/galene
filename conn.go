// Copyright (c) 2020 by Juliusz Chroboczek.

// This is not open source software.  Copy it, and I'll break into your
// house and tell your three year-old that Santa doesn't exist.

package main

import (
	"errors"
	"sync"
	"sync/atomic"

	"sfu/estimator"
	"sfu/jitter"
	"sfu/packetcache"

	"github.com/pion/rtp"
	"github.com/pion/webrtc/v2"
)

type localTrackAction struct {
	add   bool
	track downTrack
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

	localCh    chan localTrackAction // signals that local has changed
	writerDone chan struct{}         // closed when the loop dies

	mu    sync.Mutex
	local []downTrack
}

func (up *upTrack) notifyLocal(add bool, track downTrack) {
	select {
	case up.localCh <- localTrackAction{add, track}:
	case <-up.writerDone:
	}
}

func (up *upTrack) addLocal(local downTrack) {
	up.mu.Lock()
	for _, t := range up.local {
		if t == local {
			up.mu.Unlock()
			return
		}
	}
	up.local = append(up.local, local)
	up.mu.Unlock()
	up.notifyLocal(true, local)
}

func (up *upTrack) delLocal(local downTrack) bool {
	up.mu.Lock()
	for i, l := range up.local {
		if l == local {
			up.local = append(up.local[:i], up.local[i+1:]...)
			up.mu.Unlock()
			up.notifyLocal(false, l)
			return true
		}
	}
	up.mu.Unlock()
	return false
}

func (up *upTrack) getLocal() []downTrack {
	up.mu.Lock()
	defer up.mu.Unlock()
	local := make([]downTrack, len(up.local))
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

type iceConnection interface {
	addICECandidate(candidate *webrtc.ICECandidateInit) error
	flushICECandidates() error
}

type upConnection struct {
	id            string
	label         string
	pc            *webrtc.PeerConnection
	tracks        []*upTrack
	labels        map[string]string
	iceCandidates []*webrtc.ICECandidateInit

	mu    sync.Mutex
	local []downConnection
}

func (up *upConnection) addLocal(local downConnection) {
	up.mu.Lock()
	defer up.mu.Unlock()
	for _, t := range up.local {
		if t == local {
			up.mu.Unlock()
			return
		}
	}
	up.local = append(up.local, local)
}

func (up *upConnection) delLocal(local downConnection) bool {
	up.mu.Lock()
	defer up.mu.Unlock()
	for i, l := range up.local {
		if l == local {
			up.local = append(up.local[:i], up.local[i+1:]...)
			return true
		}
	}
	return false
}

func (up *upConnection) getLocal() []downConnection {
	up.mu.Lock()
	defer up.mu.Unlock()
	local := make([]downConnection, len(up.local))
	copy(local, up.local)
	return local
}

func (up *upConnection) addICECandidate(candidate *webrtc.ICECandidateInit) error {
	if up.pc.RemoteDescription() != nil {
		return up.pc.AddICECandidate(*candidate)
	}
	up.iceCandidates = append(up.iceCandidates, candidate)
	return nil
}

func flushICECandidates(pc *webrtc.PeerConnection, candidates []*webrtc.ICECandidateInit) error {
	if pc.RemoteDescription() == nil {
		return errors.New("flushICECandidates called in bad state")
	}

	var err error
	for _, candidate := range candidates {
		err2 := pc.AddICECandidate(*candidate)
		if err == nil {
			err = err2
		}
	}
	return err
}

func (up *upConnection) flushICECandidates() error {
	err := flushICECandidates(up.pc, up.iceCandidates)
	up.iceCandidates = nil
	return err
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

type downTrack interface {
	WriteRTP(packat *rtp.Packet) error
	Accumulate(bytes uint32)
	GetMaxBitrate(now uint64) uint64
}

type rtpDownTrack struct {
	track          *webrtc.Track
	remote         *upTrack
	maxLossBitrate *bitrate
	maxREMBBitrate *bitrate
	rate           *estimator.Estimator
	stats          *receiverStats
}

func (down *rtpDownTrack) WriteRTP(packet *rtp.Packet) error {
	return down.track.WriteRTP(packet)
}

func (down *rtpDownTrack) Accumulate(bytes uint32) {
	down.rate.Add(bytes)
}

func (down *rtpDownTrack) GetMaxBitrate(now uint64) uint64 {
	br1 := down.maxLossBitrate.Get(now)
	br2 := down.maxREMBBitrate.Get(now)
	if br1 < br2 {
		return br1
	}
	return br2
}

type downConnection interface {
	Close() error
}

type rtpDownConnection struct {
	id            string
	client        *client
	pc            *webrtc.PeerConnection
	remote        *upConnection
	tracks        []*rtpDownTrack
	iceCandidates []*webrtc.ICECandidateInit
}

func (down *rtpDownConnection) Close() error {
	return down.client.action(delConnAction{down.id})
}

func (down *rtpDownConnection) addICECandidate(candidate *webrtc.ICECandidateInit) error {
	if down.pc.RemoteDescription() != nil {
		return down.pc.AddICECandidate(*candidate)
	}
	down.iceCandidates = append(down.iceCandidates, candidate)
	return nil
}

func (down *rtpDownConnection) flushICECandidates() error {
	err := flushICECandidates(down.pc, down.iceCandidates)
	down.iceCandidates = nil
	return err
}
