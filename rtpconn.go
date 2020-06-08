// Copyright (c) 2020 by Juliusz Chroboczek.

// This is not open source software.  Copy it, and I'll break into your
// house and tell your three year-old that Santa doesn't exist.

package main

import (
	"errors"
	"io"
	"log"
	"math/bits"
	"sync"
	"sync/atomic"
	"time"

	"sfu/estimator"
	"sfu/jitter"
	"sfu/packetcache"
	"sfu/rtptime"

	"github.com/pion/rtcp"
	"github.com/pion/rtp"
	"github.com/pion/webrtc/v2"
)

type bitrate struct {
	bitrate uint64
	jiffies uint64
}

func (br *bitrate) Set(bitrate uint64, now uint64) {
	atomic.StoreUint64(&br.bitrate, bitrate)
	atomic.StoreUint64(&br.jiffies, now)
}

func (br *bitrate) Get(now uint64) uint64 {
	ts := atomic.LoadUint64(&br.jiffies)
	if now < ts || now-ts > receiverReportTimeout {
		return ^uint64(0)
	}
	return atomic.LoadUint64(&br.bitrate)
}

type receiverStats struct {
	loss    uint32
	jitter  uint32
	jiffies uint64
}

func (s *receiverStats) Set(loss uint8, jitter uint32, now uint64) {
	atomic.StoreUint32(&s.loss, uint32(loss))
	atomic.StoreUint32(&s.jitter, jitter)
	atomic.StoreUint64(&s.jiffies, now)
}

func (s *receiverStats) Get(now uint64) (uint8, uint32) {
	ts := atomic.LoadUint64(&s.jiffies)
	if now < ts || now > ts+receiverReportTimeout {
		return 0, 0
	}
	return uint8(atomic.LoadUint32(&s.loss)), atomic.LoadUint32(&s.jitter)
}

const receiverReportTimeout = 8 * rtptime.JiffiesPerSec

type iceConnection interface {
	addICECandidate(candidate *webrtc.ICECandidateInit) error
	flushICECandidates() error
}

type rtpDownTrack struct {
	track          *webrtc.Track
	remote         upTrack
	maxLossBitrate *bitrate
	maxREMBBitrate *bitrate
	rate           *estimator.Estimator
	stats          *receiverStats
	srTime         uint64
	srNTPTime      uint64
	rtt            uint64
}

func (down *rtpDownTrack) WriteRTP(packet *rtp.Packet) error {
	return down.track.WriteRTP(packet)
}

func (down *rtpDownTrack) Accumulate(bytes uint32) {
	down.rate.Accumulate(bytes)
}

func (down *rtpDownTrack) GetMaxBitrate(now uint64) uint64 {
	br1 := down.maxLossBitrate.Get(now)
	br2 := down.maxREMBBitrate.Get(now)
	if br1 < br2 {
		return br1
	}
	return br2
}

type rtpDownConnection struct {
	id            string
	pc            *webrtc.PeerConnection
	remote        upConnection
	tracks        []*rtpDownTrack
	iceCandidates []*webrtc.ICECandidateInit
	close         func() error
}

func newDownConn(id string, remote upConnection) (*rtpDownConnection, error) {
	pc, err := groups.api.NewPeerConnection(iceConfiguration())
	if err != nil {
		return nil, err
	}

	pc.OnTrack(func(remote *webrtc.Track, receiver *webrtc.RTPReceiver) {
		log.Printf("Got track on downstream connection")
	})

	conn := &rtpDownConnection{
		id:     id,
		pc:     pc,
		remote: remote,
	}

	return conn, nil
}

func (down *rtpDownConnection) Close() error {
	return down.close()
}

func (down *rtpDownConnection) addICECandidate(candidate *webrtc.ICECandidateInit) error {
	if down.pc.RemoteDescription() != nil {
		return down.pc.AddICECandidate(*candidate)
	}
	down.iceCandidates = append(down.iceCandidates, candidate)
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

func (down *rtpDownConnection) flushICECandidates() error {
	err := flushICECandidates(down.pc, down.iceCandidates)
	down.iceCandidates = nil
	return err
}

type rtpUpTrack struct {
	track      *webrtc.Track
	label      string
	rate       *estimator.Estimator
	cache      *packetcache.Cache
	jitter     *jitter.Estimator
	maxBitrate uint64
	lastPLI    uint64
	lastFIR    uint64
	firSeqno   uint32

	localCh    chan localTrackAction
	writerDone chan struct{}

	mu        sync.Mutex
	local     []downTrack
	srTime    uint64
	srNTPTime uint64
	srRTPTime uint32
}

type localTrackAction struct {
	add   bool
	track downTrack
}

func (up *rtpUpTrack) notifyLocal(add bool, track downTrack) {
	select {
	case up.localCh <- localTrackAction{add, track}:
	case <-up.writerDone:
	}
}

func (up *rtpUpTrack) addLocal(local downTrack) error {
	up.mu.Lock()
	for _, t := range up.local {
		if t == local {
			up.mu.Unlock()
			return nil
		}
	}
	up.local = append(up.local, local)
	up.mu.Unlock()

	up.notifyLocal(true, local)
	return nil
}

func (up *rtpUpTrack) delLocal(local downTrack) bool {
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

func (up *rtpUpTrack) getLocal() []downTrack {
	up.mu.Lock()
	defer up.mu.Unlock()
	local := make([]downTrack, len(up.local))
	copy(local, up.local)
	return local
}

func (up *rtpUpTrack) getRTP(seqno uint16, result []byte) uint16 {
	return up.cache.Get(seqno, result)
}

func (up *rtpUpTrack) getTimestamp() (uint32, bool) {
	buf := make([]byte, packetcache.BufSize)
	l := up.cache.GetLast(buf)
	if l == 0 {
		return 0, false
	}
	var packet rtp.Packet
	err := packet.Unmarshal(buf)
	if err != nil {
		return 0, false
	}
	return packet.Timestamp, true
}

func (up *rtpUpTrack) Label() string {
	return up.label
}

func (up *rtpUpTrack) Codec() *webrtc.RTPCodec {
	return up.track.Codec()
}

func (up *rtpUpTrack) hasRtcpFb(tpe, parameter string) bool {
	for _, fb := range up.track.Codec().RTCPFeedback {
		if fb.Type == tpe && fb.Parameter == parameter {
			return true
		}
	}
	return false
}

type rtpUpConnection struct {
	id            string
	label         string
	pc            *webrtc.PeerConnection
	labels        map[string]string
	iceCandidates []*webrtc.ICECandidateInit

	mu     sync.Mutex
	closed bool
	tracks []*rtpUpTrack
	local  []downConnection
}

func (up *rtpUpConnection) getTracks() []*rtpUpTrack {
	up.mu.Lock()
	defer up.mu.Unlock()
	tracks := make([]*rtpUpTrack, len(up.tracks))
	copy(tracks, up.tracks)
	return tracks
}

func (up *rtpUpConnection) Id() string {
	return up.id
}

func (up *rtpUpConnection) Label() string {
	return up.label
}

func (up *rtpUpConnection) addLocal(local downConnection) error {
	up.mu.Lock()
	defer up.mu.Unlock()
	if up.closed {
		return ErrConnectionClosed
	}
	for _, t := range up.local {
		if t == local {
			return nil
		}
	}
	up.local = append(up.local, local)
	return nil
}

func (up *rtpUpConnection) delLocal(local downConnection) bool {
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

func (up *rtpUpConnection) getLocal() []downConnection {
	up.mu.Lock()
	defer up.mu.Unlock()
	local := make([]downConnection, len(up.local))
	copy(local, up.local)
	return local
}

func (up *rtpUpConnection) Close() error {
	up.mu.Lock()
	defer up.mu.Unlock()

	go func(local []downConnection) {
		for _, l := range local {
			l.Close()
		}
	}(up.local)

	up.local = nil
	up.closed = true
	return up.pc.Close()
}

func (up *rtpUpConnection) addICECandidate(candidate *webrtc.ICECandidateInit) error {
	if up.pc.RemoteDescription() != nil {
		return up.pc.AddICECandidate(*candidate)
	}
	up.iceCandidates = append(up.iceCandidates, candidate)
	return nil
}

func (up *rtpUpConnection) flushICECandidates() error {
	err := flushICECandidates(up.pc, up.iceCandidates)
	up.iceCandidates = nil
	return err
}

func getTrackMid(pc *webrtc.PeerConnection, track *webrtc.Track) string {
	for _, t := range pc.GetTransceivers() {
		if t.Receiver() != nil && t.Receiver().Track() == track {
			return t.Mid()
		}
	}
	return ""
}

// called locked
func (up *rtpUpConnection) complete() bool {
	for mid, _ := range up.labels {
		found := false
		for _, t := range up.tracks {
			m := getTrackMid(up.pc, t.track)
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

func newUpConn(c client, id string) (*rtpUpConnection, error) {
	pc, err := groups.api.NewPeerConnection(iceConfiguration())
	if err != nil {
		return nil, err
	}

	_, err = pc.AddTransceiverFromKind(webrtc.RTPCodecTypeAudio,
		webrtc.RtpTransceiverInit{
			Direction: webrtc.RTPTransceiverDirectionRecvonly,
		},
	)
	if err != nil {
		pc.Close()
		return nil, err
	}

	_, err = pc.AddTransceiverFromKind(webrtc.RTPCodecTypeVideo,
		webrtc.RtpTransceiverInit{
			Direction: webrtc.RTPTransceiverDirectionRecvonly,
		},
	)
	if err != nil {
		pc.Close()
		return nil, err
	}

	conn := &rtpUpConnection{id: id, pc: pc}

	pc.OnTrack(func(remote *webrtc.Track, receiver *webrtc.RTPReceiver) {
		conn.mu.Lock()
		defer conn.mu.Unlock()

		mid := getTrackMid(pc, remote)
		if mid == "" {
			log.Printf("Couldn't get track's mid")
			return
		}

		label, ok := conn.labels[mid]
		if !ok {
			log.Printf("Couldn't get track's label")
			isvideo := remote.Kind() == webrtc.RTPCodecTypeVideo
			if isvideo {
				label = "video"
			} else {
				label = "audio"
			}
		}

		track := &rtpUpTrack{
			track:      remote,
			label:      label,
			cache:      packetcache.New(32),
			rate:       estimator.New(time.Second),
			jitter:     jitter.New(remote.Codec().ClockRate),
			maxBitrate: ^uint64(0),
			localCh:    make(chan localTrackAction, 2),
			writerDone: make(chan struct{}),
		}

		conn.tracks = append(conn.tracks, track)

		if remote.Kind() == webrtc.RTPCodecTypeVideo {
			atomic.AddUint32(&c.Group().videoCount, 1)
		}

		go readLoop(conn, track)

		go rtcpUpListener(conn, track, receiver)

		if conn.complete() {
			tracks := make([]upTrack, len(conn.tracks))
			for i, t := range conn.tracks {
				tracks[i] = t
			}
			clients := c.Group().getClients(c)
			for _, cc := range clients {
				cc.pushConn(conn, tracks, conn.label)
			}
			go rtcpUpSender(conn)
		}
	})

	return conn, nil
}

type packetIndex struct {
	seqno uint16
	index uint16
}

func readLoop(conn *rtpUpConnection, track *rtpUpTrack) {
	isvideo := track.track.Kind() == webrtc.RTPCodecTypeVideo
	ch := make(chan packetIndex, 32)
	defer close(ch)
	go writeLoop(conn, track, ch)

	buf := make([]byte, packetcache.BufSize)
	var packet rtp.Packet
	drop := 0
	for {
		bytes, err := track.track.Read(buf)
		if err != nil {
			if err != io.EOF {
				log.Printf("%v", err)
			}
			break
		}
		track.rate.Accumulate(uint32(bytes))

		err = packet.Unmarshal(buf[:bytes])
		if err != nil {
			log.Printf("%v", err)
			continue
		}

		track.jitter.Accumulate(packet.Timestamp)

		first, index :=
			track.cache.Store(packet.SequenceNumber, buf[:bytes])
		if packet.SequenceNumber-first > 24 {
			found, first, bitmap := track.cache.BitmapGet()
			if found {
				err := conn.sendNACK(track, first, bitmap)
				if err != nil {
					log.Printf("%v", err)
				}
			}
		}

		if drop > 0 {
			if packet.Marker {
				// last packet in frame
				drop = 0
			} else {
				drop--
			}
			continue
		}

		select {
		case ch <- packetIndex{packet.SequenceNumber, index}:
		default:
			if isvideo {
				// the writer is congested.  Drop until
				// the end of the frame.
				if isvideo && !packet.Marker {
					drop = 7
				}
			}
		}
	}
}

func writeLoop(conn *rtpUpConnection, track *rtpUpTrack, ch <-chan packetIndex) {
	defer close(track.writerDone)

	buf := make([]byte, packetcache.BufSize)
	var packet rtp.Packet

	local := make([]downTrack, 0)

	firSent := false

	for {
		select {
		case action := <-track.localCh:
			if action.add {
				local = append(local, action.track)
				firSent = false
			} else {
				found := false
				for i, t := range local {
					if t == action.track {
						local = append(local[:i], local[i+1:]...)
						found = true
						break
					}
				}
				if !found {
					log.Printf("Deleting unknown track!")
				}
			}
		case pi, ok := <-ch:
			if !ok {
				return
			}

			bytes := track.cache.GetAt(pi.seqno, pi.index, buf)
			if bytes == 0 {
				continue
			}

			err := packet.Unmarshal(buf[:bytes])
			if err != nil {
				log.Printf("%v", err)
				continue
			}

			kfNeeded := false

			for _, l := range local {
				err := l.WriteRTP(&packet)
				if err != nil {
					if err == ErrKeyframeNeeded {
						kfNeeded = true
					} else if err != io.ErrClosedPipe {
						log.Printf("WriteRTP: %v", err)
					}
					continue
				}
				l.Accumulate(uint32(bytes))
			}

			if kfNeeded {
				err := conn.sendFIR(track, !firSent)
				if err == ErrUnsupportedFeedback {
					err := conn.sendPLI(track)
					if err != nil &&
						err != ErrUnsupportedFeedback {
						log.Printf("sendPLI: %v", err)
					}
				} else if err != nil {
					log.Printf("sendFIR: %v", err)
				}
				firSent = true
			}
		}
	}
}

var ErrUnsupportedFeedback = errors.New("unsupported feedback type")
var ErrRateLimited = errors.New("rate limited")

func (up *rtpUpConnection) sendPLI(track *rtpUpTrack) error {
	if !track.hasRtcpFb("nack", "pli") {
		return ErrUnsupportedFeedback
	}
	last := atomic.LoadUint64(&track.lastPLI)
	now := rtptime.Jiffies()
	if now >= last && now-last < rtptime.JiffiesPerSec/5 {
		return ErrRateLimited
	}
	atomic.StoreUint64(&track.lastPLI, now)
	return sendPLI(up.pc, track.track.SSRC())
}

func sendPLI(pc *webrtc.PeerConnection, ssrc uint32) error {
	return pc.WriteRTCP([]rtcp.Packet{
		&rtcp.PictureLossIndication{MediaSSRC: ssrc},
	})
}

func (up *rtpUpConnection) sendFIR(track *rtpUpTrack, increment bool) error {
	// we need to reliably increment the seqno, even if we are going
	// to drop the packet due to rate limiting.
	var seqno uint8
	if increment {
		seqno = uint8(atomic.AddUint32(&track.firSeqno, 1) & 0xFF)
	} else {
		seqno = uint8(atomic.LoadUint32(&track.firSeqno) & 0xFF)
	}

	if !track.hasRtcpFb("ccm", "fir") {
		return ErrUnsupportedFeedback
	}
	last := atomic.LoadUint64(&track.lastFIR)
	now := rtptime.Jiffies()
	if now >= last && now-last < rtptime.JiffiesPerSec/5 {
		return ErrRateLimited
	}
	atomic.StoreUint64(&track.lastFIR, now)
	return sendFIR(up.pc, track.track.SSRC(), seqno)
}

func sendFIR(pc *webrtc.PeerConnection, ssrc uint32, seqno uint8) error {
	return pc.WriteRTCP([]rtcp.Packet{
		&rtcp.FullIntraRequest{
			FIR: []rtcp.FIREntry{
				rtcp.FIREntry{
					SSRC:           ssrc,
					SequenceNumber: seqno,
				},
			},
		},
	})
}

func sendREMB(pc *webrtc.PeerConnection, ssrc uint32, bitrate uint64) error {
	return pc.WriteRTCP([]rtcp.Packet{
		&rtcp.ReceiverEstimatedMaximumBitrate{
			Bitrate: bitrate,
			SSRCs:   []uint32{ssrc},
		},
	})
}

func (up *rtpUpConnection) sendNACK(track *rtpUpTrack, first uint16, bitmap uint16) error {
	if !track.hasRtcpFb("nack", "") {
		return nil
	}
	err := sendNACK(up.pc, track.track.SSRC(), first, bitmap)
	if err == nil {
		track.cache.Expect(1 + bits.OnesCount16(bitmap))
	}
	return err
}

func sendNACK(pc *webrtc.PeerConnection, ssrc uint32, first uint16, bitmap uint16) error {
	packet := rtcp.Packet(
		&rtcp.TransportLayerNack{
			MediaSSRC: ssrc,
			Nacks: []rtcp.NackPair{
				rtcp.NackPair{
					first,
					rtcp.PacketBitmap(bitmap),
				},
			},
		},
	)
	return pc.WriteRTCP([]rtcp.Packet{packet})
}

func sendRecovery(p *rtcp.TransportLayerNack, track *rtpDownTrack) {
	var packet rtp.Packet
	buf := make([]byte, packetcache.BufSize)
	for _, nack := range p.Nacks {
		for _, seqno := range nack.PacketList() {
			l := track.remote.getRTP(seqno, buf)
			if l == 0 {
				continue
			}
			err := packet.Unmarshal(buf[:l])
			if err != nil {
				continue
			}
			err = track.track.WriteRTP(&packet)
			if err != nil {
				log.Printf("WriteRTP: %v", err)
				continue
			}
			track.rate.Accumulate(uint32(l))
		}
	}
}

func rtcpUpListener(conn *rtpUpConnection, track *rtpUpTrack, r *webrtc.RTPReceiver) {
	for {
		firstSR := false
		ps, err := r.ReadRTCP()
		if err != nil {
			if err != io.EOF {
				log.Printf("ReadRTCP: %v", err)
			}
			return
		}

		now := rtptime.Jiffies()

		for _, p := range ps {
			switch p := p.(type) {
			case *rtcp.SenderReport:
				track.mu.Lock()
				if track.srTime == 0 {
					firstSR = true
				}
				track.srTime = now
				track.srNTPTime = p.NTPTime
				track.srRTPTime = p.RTPTime
				track.mu.Unlock()
			case *rtcp.SourceDescription:
			}
		}

		if firstSR {
			// this is the first SR we got for at least one track,
			// quickly propagate the time offsets downstream
			local := conn.getLocal()
			for _, l := range local {
				l, ok := l.(*rtpDownConnection)
				if ok {
					err := sendSR(l)
					if err != nil {
						log.Printf("sendSR: %v", err)
					}
				}
			}
		}
	}
}

func sendRR(conn *rtpUpConnection) error {
	conn.mu.Lock()
	defer conn.mu.Unlock()

	if len(conn.tracks) == 0 {
		return nil
	}

	now := rtptime.Jiffies()

	reports := make([]rtcp.ReceptionReport, 0, len(conn.tracks))
	for _, t := range conn.tracks {
		expected, lost, totalLost, eseqno := t.cache.GetStats(true)
		if expected == 0 {
			expected = 1
		}
		if lost >= expected {
			lost = expected - 1
		}

		t.mu.Lock()
		srTime := t.srTime
		srNTPTime := t.srNTPTime
		t.mu.Unlock()

		var delay uint64
		if srTime != 0 {
			delay = (now - srTime) /
				(rtptime.JiffiesPerSec / 0x10000)
		}

		reports = append(reports, rtcp.ReceptionReport{
			SSRC:               t.track.SSRC(),
			FractionLost:       uint8((lost * 256) / expected),
			TotalLost:          totalLost,
			LastSequenceNumber: eseqno,
			Jitter:             t.jitter.Jitter(),
			LastSenderReport:   uint32(srNTPTime >> 16),
			Delay:              uint32(delay),
		})
	}

	return conn.pc.WriteRTCP([]rtcp.Packet{
		&rtcp.ReceiverReport{
			Reports: reports,
		},
	})
}

func rtcpUpSender(conn *rtpUpConnection) {
	for {
		time.Sleep(time.Second)
		err := sendRR(conn)
		if err != nil {
			if err == io.EOF || err == io.ErrClosedPipe {
				return
			}
			log.Printf("sendRR: %v", err)
		}
	}
}

func sendSR(conn *rtpDownConnection) error {
	// since this is only called after all tracks have been created,
	// there is no need for locking.
	packets := make([]rtcp.Packet, 0, len(conn.tracks))

	now := time.Now()
	nowNTP := rtptime.TimeToNTP(now)
	jiffies := rtptime.TimeToJiffies(now)

	for _, t := range conn.tracks {
		clockrate := t.track.Codec().ClockRate
		remote := t.remote

		var nowRTP uint32

		switch r := remote.(type) {
		case *rtpUpTrack:
			r.mu.Lock()
			lastTime := r.srTime
			srNTPTime := r.srNTPTime
			srRTPTime := r.srRTPTime
			r.mu.Unlock()
			if lastTime == 0 {
				// we never got a remote SR, skip this track
				continue
			}
			if srNTPTime != 0 {
				srTime := rtptime.NTPToTime(srNTPTime)
				d := now.Sub(srTime)
				if d > 0 && d < time.Hour {
					delay := rtptime.FromDuration(
						d, clockrate,
					)
					nowRTP = srRTPTime + uint32(delay)
				}
			}
		default:
			ts, ok := remote.getTimestamp()
			if !ok {
				continue
			}
			nowRTP = ts
		}

		p, b := t.rate.Totals()
		packets = append(packets,
			&rtcp.SenderReport{
				SSRC:        t.track.SSRC(),
				NTPTime:     nowNTP,
				RTPTime:     nowRTP,
				PacketCount: p,
				OctetCount:  b,
			})
		atomic.StoreUint64(&t.srTime, jiffies)
		atomic.StoreUint64(&t.srNTPTime, nowNTP)
	}

	if len(packets) == 0 {
		return nil
	}

	return conn.pc.WriteRTCP(packets)
}

func rtcpDownSender(conn *rtpDownConnection) {
	for {
		time.Sleep(time.Second)
		err := sendSR(conn)
		if err != nil {
			if err == io.EOF || err == io.ErrClosedPipe {
				return
			}
			log.Printf("sendSR: %v", err)
		}
	}
}

const (
	minLossRate  = 9600
	initLossRate = 512 * 1000
	maxLossRate  = 1 << 30
)

func (track *rtpDownTrack) updateRate(loss uint8, now uint64) {
	rate := track.maxLossBitrate.Get(now)
	if rate < minLossRate || rate > maxLossRate {
		// no recent feedback, reset
		rate = initLossRate
	}
	if loss < 5 {
		// if our actual rate is low, then we're not probing the
		// bottleneck
		r, _ := track.rate.Estimate()
		actual := 8 * uint64(r)
		if actual >= (rate*7)/8 {
			// loss < 0.02, multiply by 1.05
			rate = rate * 269 / 256
			if rate > maxLossRate {
				rate = maxLossRate
			}
		}
	} else if loss > 25 {
		// loss > 0.1, multiply by (1 - loss/2)
		rate = rate * (512 - uint64(loss)) / 512
		if rate < minLossRate {
			rate = minLossRate
		}
	}

	// update unconditionally, to set the timestamp
	track.maxLossBitrate.Set(rate, now)
}

func rtcpDownListener(conn *rtpDownConnection, track *rtpDownTrack, s *webrtc.RTPSender) {
	var gotFir bool
	lastFirSeqno := uint8(0)

	for {
		ps, err := s.ReadRTCP()
		if err != nil {
			if err != io.EOF {
				log.Printf("ReadRTCP: %v", err)
			}
			return
		}
		jiffies := rtptime.Jiffies()

		for _, p := range ps {
			switch p := p.(type) {
			case *rtcp.PictureLossIndication:
				remote, ok := conn.remote.(*rtpUpConnection)
				if !ok {
					continue
				}
				rt, ok := track.remote.(*rtpUpTrack)
				if !ok {
					continue
				}
				err := remote.sendPLI(rt)
				if err != nil {
					log.Printf("sendPLI: %v", err)
				}
			case *rtcp.FullIntraRequest:
				found := false
				var seqno uint8
				for _, entry := range p.FIR {
					if entry.SSRC == track.track.SSRC() {
						found = true
						seqno = entry.SequenceNumber
						break
					}
				}
				if !found {
					log.Printf("Misdirected FIR")
					continue
				}

				increment := true
				if gotFir {
					increment = seqno != lastFirSeqno
				}
				gotFir = true
				lastFirSeqno = seqno

				remote, ok := conn.remote.(*rtpUpConnection)
				if !ok {
					continue
				}
				rt, ok := track.remote.(*rtpUpTrack)
				if !ok {
					continue
				}
				err := remote.sendFIR(rt, increment)
				if err == ErrUnsupportedFeedback {
					err := remote.sendPLI(rt)
					if err != nil {
						log.Printf("sendPLI: %v", err)
					}
				} else if err != nil {
					log.Printf("sendFIR: %v", err)
				}
			case *rtcp.ReceiverEstimatedMaximumBitrate:
				track.maxREMBBitrate.Set(p.Bitrate, jiffies)
			case *rtcp.ReceiverReport:
				for _, r := range p.Reports {
					if r.SSRC == track.track.SSRC() {
						handleReport(track, r, jiffies)
					}
				}
			case *rtcp.SenderReport:
				for _, r := range p.Reports {
					if r.SSRC == track.track.SSRC() {
						handleReport(track, r, jiffies)
					}
				}
			case *rtcp.TransportLayerNack:
				maxBitrate := track.GetMaxBitrate(jiffies)
				bitrate, _ := track.rate.Estimate()
				if uint64(bitrate)*7/8 < maxBitrate {
					sendRecovery(p, track)
				}
			}
		}
	}
}

func handleReport(track *rtpDownTrack, report rtcp.ReceptionReport, jiffies uint64) {
	track.stats.Set(report.FractionLost, report.Jitter, jiffies)
	track.updateRate(report.FractionLost, jiffies)

	if report.LastSenderReport != 0 {
		jiffies := rtptime.Jiffies()
		srTime := atomic.LoadUint64(&track.srTime)
		if jiffies < srTime || jiffies-srTime > 8*rtptime.JiffiesPerSec {
			return
		}
		srNTPTime := atomic.LoadUint64(&track.srNTPTime)
		if report.LastSenderReport == uint32(srNTPTime>>16) {
			delay := uint64(report.Delay) *
				(rtptime.JiffiesPerSec / 0x10000)
			if delay > jiffies-srTime {
				return
			}
			rtt := (jiffies - srTime) - delay
			oldrtt := atomic.LoadUint64(&track.rtt)
			newrtt := rtt
			if oldrtt > 0 {
				newrtt = (3*oldrtt + rtt) / 4
			}
			atomic.StoreUint64(&track.rtt, newrtt)
		}
	}
}

func updateUpTrack(track *rtpUpTrack, maxVideoRate uint64) uint64 {
	now := rtptime.Jiffies()

	isvideo := track.track.Kind() == webrtc.RTPCodecTypeVideo
	clockrate := track.track.Codec().ClockRate
	minrate := uint64(minAudioRate)
	rate := ^uint64(0)
	if isvideo {
		minrate = minVideoRate
		rate = maxVideoRate
		if rate < minrate {
			rate = minrate
		}
	}
	local := track.getLocal()
	var maxrto uint64
	for _, l := range local {
		bitrate := l.GetMaxBitrate(now)
		if bitrate == ^uint64(0) {
			continue
		}
		if bitrate <= minrate {
			rate = minrate
			break
		}
		if rate > bitrate {
			rate = bitrate
		}
		ll, ok := l.(*rtpDownTrack)
		if ok {
			_, j := ll.stats.Get(now)
			jitter := uint64(j) *
				(rtptime.JiffiesPerSec /
					uint64(clockrate))
			rtt := atomic.LoadUint64(&ll.rtt)
			rto := rtt + 4*jitter
			if rto > maxrto {
				maxrto = rto
			}
		}
	}
	track.maxBitrate = rate
	_, r := track.rate.Estimate()
	packets := int((uint64(r) * maxrto * 4) / rtptime.JiffiesPerSec)
	if packets < 32 {
		packets = 32
	}
	if packets > 256 {
		packets = 256
	}
	track.cache.ResizeCond(packets)

	return rate
}
