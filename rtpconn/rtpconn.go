package rtpconn

import (
	"errors"
	"io"
	"log"
	"math/bits"
	"sync"
	"sync/atomic"
	"time"

	"github.com/pion/rtcp"
	"github.com/pion/rtp"
	"github.com/pion/sdp/v3"
	"github.com/pion/webrtc/v3"

	"github.com/jech/galene/conn"
	"github.com/jech/galene/estimator"
	"github.com/jech/galene/group"
	"github.com/jech/galene/ice"
	"github.com/jech/galene/jitter"
	"github.com/jech/galene/packetcache"
	"github.com/jech/galene/packetmap"
	"github.com/jech/galene/rtptime"
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

const receiverReportTimeout = 30 * rtptime.JiffiesPerSec

func (s *receiverStats) Get(now uint64) (uint8, uint32) {
	ts := atomic.LoadUint64(&s.jiffies)
	if now < ts || now > ts+receiverReportTimeout {
		return 0, 0
	}
	return uint8(atomic.LoadUint32(&s.loss)), atomic.LoadUint32(&s.jitter)
}

type iceConnection interface {
	addICECandidate(candidate *webrtc.ICECandidateInit) error
	flushICECandidates() error
}

type downTrackAtomics struct {
	rtt       uint64
	sr        uint64
	srNTP     uint64
	remoteNTP uint64
	remoteRTP uint32
	layerInfo uint32
}

type rtpDownTrack struct {
	track          *webrtc.TrackLocalStaticRTP
	sender         *webrtc.RTPSender
	remote         conn.UpTrack
	ssrc           webrtc.SSRC
	packetmap      packetmap.Map
	maxBitrate     *bitrate
	maxREMBBitrate *bitrate
	rate           *estimator.Estimator
	stats          *receiverStats
	atomics        *downTrackAtomics
	cname          atomic.Value
}

func (down *rtpDownTrack) SetTimeOffset(ntp uint64, rtp uint32) {
	atomic.StoreUint64(&down.atomics.remoteNTP, ntp)
	atomic.StoreUint32(&down.atomics.remoteRTP, rtp)
}

func (down *rtpDownTrack) getTimeOffset() (uint64, uint32) {
	ntp := atomic.LoadUint64(&down.atomics.remoteNTP)
	rtp := atomic.LoadUint32(&down.atomics.remoteRTP)
	return ntp, rtp
}

func (down *rtpDownTrack) getRTT() uint64 {
	return atomic.LoadUint64(&down.atomics.rtt)
}

func (down *rtpDownTrack) setRTT(rtt uint64) {
	atomic.StoreUint64(&down.atomics.rtt, rtt)
}

func (down *rtpDownTrack) getSRTime() (uint64, uint64) {
	tm := atomic.LoadUint64(&down.atomics.sr)
	ntp := atomic.LoadUint64(&down.atomics.srNTP)
	return tm, ntp
}

func (down *rtpDownTrack) setSRTime(tm uint64, ntp uint64) {
	atomic.StoreUint64(&down.atomics.sr, tm)
	atomic.StoreUint64(&down.atomics.srNTP, ntp)
}

func (down *rtpDownTrack) SetCname(cname string) {
	down.cname.Store(cname)
}

func (down *rtpDownTrack) getLayerInfo() (uint8, uint8, uint8) {
	info := atomic.LoadUint32(&down.atomics.layerInfo)
	return uint8(info >> 16), uint8(info >> 8), uint8(info)
}

func (down *rtpDownTrack) setLayerInfo(layer, wanted, max uint8) {
	atomic.StoreUint32(&down.atomics.layerInfo,
		(uint32(layer)<<16)|(uint32(wanted)<<8)|uint32(max),
	)
}

const (
	negotiationUnneeded = iota
	negotiationNeeded
	negotiationRestartIce
)

type rtpDownConnection struct {
	id                string
	pc                *webrtc.PeerConnection
	remote            conn.Up
	iceCandidates     []*webrtc.ICECandidateInit
	negotiationNeeded int
	requested         []string

	mu     sync.Mutex
	tracks []*rtpDownTrack
}

func (down *rtpDownConnection) getTracks() []*rtpDownTrack {
	down.mu.Lock()
	defer down.mu.Unlock()
	tracks := make([]*rtpDownTrack, len(down.tracks))
	copy(tracks, down.tracks)
	return tracks
}

func newDownConn(c group.Client, id string, remote conn.Up) (*rtpDownConnection, error) {
	api, err := c.Group().API()
	if err != nil {
		return nil, err
	}
	pc, err := api.NewPeerConnection(*ice.ICEConfiguration())
	if err != nil {
		return nil, err
	}

	pc.OnTrack(func(remote *webrtc.TrackRemote, receiver *webrtc.RTPReceiver) {
		log.Printf("Got track on downstream connection")
	})

	conn := &rtpDownConnection{
		id:     id,
		pc:     pc,
		remote: remote,
	}

	return conn, nil
}

var packetBufPool = sync.Pool{
	New: func() interface{} {
		return make([]byte, packetcache.BufSize)
	},
}

func (down *rtpDownTrack) Write(buf []byte) (int, error) {
	codec := down.remote.Codec().MimeType

	seqno, start, pid, tid, _, u, _, err := packetFlags(codec, buf)
	if err != nil {
		return 0, err
	}

	layer, wantedLayer, maxLayer := down.getLayerInfo()

	if tid > maxLayer {
		if layer == maxLayer {
			wantedLayer = tid
			layer = tid
		}
		maxLayer = tid
		if wantedLayer > maxLayer {
			wantedLayer = maxLayer
		}
		down.setLayerInfo(layer, wantedLayer, maxLayer)
		down.adjustLayer()
	}
	if start && layer != wantedLayer {
		if u || wantedLayer < layer {
			layer = wantedLayer
			down.setLayerInfo(layer, wantedLayer, maxLayer)
		}
	}

	if tid > layer {
		ok := down.packetmap.Drop(seqno, pid)
		if ok {
			return 0, nil
		}
	}

	ok, newseqno, piddelta := down.packetmap.Map(seqno, pid)
	if !ok {
		return 0, nil
	}

	if newseqno == seqno && piddelta == 0 {
		return down.write(buf)
	}

	ibuf2 := packetBufPool.Get()
	defer packetBufPool.Put(ibuf2)
	buf2 := ibuf2.([]byte)

	n := copy(buf2, buf)
	err = rewritePacket(codec, buf2[:n], newseqno, piddelta)
	if err != nil {
		return 0, err
	}
	return down.write(buf2[:n])
}

func (down *rtpDownTrack) write(buf []byte) (int, error) {
	n, err := down.track.Write(buf)
	if err == nil {
		down.rate.Accumulate(uint32(n))
	}
	return n, err
}

func (t *rtpDownTrack) GetMaxBitrate() (uint64, int) {
	now := rtptime.Jiffies()
	layer, _, _ := t.getLayerInfo()
	r := t.maxBitrate.Get(now)
	if r == ^uint64(0) {
		r = 512 * 1024
	}
	rr := t.maxREMBBitrate.Get(now)
	if rr == 0 || r < rr {
		return r, int(layer)
	}
	return rr, int(layer)
}

func (t *rtpDownTrack) adjustLayer() {
	max, _ := t.GetMaxBitrate()
	r, _ := t.rate.Estimate()
	rate := uint64(r) * 8
	if rate < max*7/8 {
		layer, wanted, max := t.getLayerInfo()
		if layer < max {
			wanted = layer + 1
			t.setLayerInfo(layer, wanted, max)
		}
	} else if rate > max*3/2 {
		layer, wanted, max := t.getLayerInfo()
		if layer > 0 {
			wanted = layer - 1
			t.setLayerInfo(layer, wanted, max)
		}
	}
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

type upTrackAtomics struct {
	lastPLI uint64
}

type rtpUpTrack struct {
	track   *webrtc.TrackRemote
	rate    *estimator.Estimator
	cache   *packetcache.Cache
	jitter  *jitter.Estimator
	atomics *upTrackAtomics
	cname   atomic.Value

	localCh    chan trackAction
	readerDone chan struct{}

	mu            sync.Mutex
	srTime        uint64
	srNTPTime     uint64
	srRTPTime     uint32
	maxLayer      uint8
	local         []conn.DownTrack
	bufferedNACKs []uint16
}

const (
	trackActionAdd = iota
	trackActionDel
	trackActionKeyframe
)

type trackAction struct {
	action int
	track  conn.DownTrack
}

func (up *rtpUpTrack) action(action int, track conn.DownTrack) {
	select {
	case up.localCh <- trackAction{action, track}:
	case <-up.readerDone:
	}
}

func (up *rtpUpTrack) AddLocal(local conn.DownTrack) error {
	up.mu.Lock()
	defer up.mu.Unlock()

	for _, t := range up.local {
		if t == local {
			return nil
		}
	}
	up.local = append(up.local, local)

	// do this asynchronously, to avoid deadlocks when multiple
	// clients call this simultaneously.
	go up.action(trackActionAdd, local)
	return nil
}

func (up *rtpUpTrack) RequestKeyframe() error {
	go up.action(trackActionKeyframe, nil)
	return nil
}

func (up *rtpUpTrack) DelLocal(local conn.DownTrack) bool {
	up.mu.Lock()
	defer up.mu.Unlock()
	for i, l := range up.local {
		if l == local {
			up.local = append(up.local[:i], up.local[i+1:]...)
			// do this asynchronously, to avoid deadlocking when
			// multiple clients call this simultaneously.
			go up.action(trackActionDel, l)
			return true
		}
	}
	return false
}

func (up *rtpUpTrack) getLocal() []conn.DownTrack {
	up.mu.Lock()
	defer up.mu.Unlock()
	local := make([]conn.DownTrack, len(up.local))
	copy(local, up.local)
	return local
}

func (up *rtpUpTrack) GetRTP(seqno uint16, result []byte) uint16 {
	return up.cache.Get(seqno, result)
}

func (up *rtpUpTrack) Label() string {
	return up.track.RID()
}

func (up *rtpUpTrack) Kind() webrtc.RTPCodecType {
	return up.track.Kind()
}

func (up *rtpUpTrack) Codec() webrtc.RTPCodecCapability {
	return up.track.Codec().RTPCodecCapability
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
	client        group.Client
	label         string
	userId        string
	username      string
	pc            *webrtc.PeerConnection
	iceCandidates []*webrtc.ICECandidateInit

	mu      sync.Mutex
	pushed  bool
	replace string
	tracks  []*rtpUpTrack
	local   []conn.Down
}

func (up *rtpUpConnection) getTracks() []*rtpUpTrack {
	up.mu.Lock()
	defer up.mu.Unlock()
	tracks := make([]*rtpUpTrack, len(up.tracks))
	copy(tracks, up.tracks)
	return tracks
}

func (up *rtpUpConnection) getReplace(reset bool) string {
	up.mu.Lock()
	defer up.mu.Unlock()
	replace := up.replace
	if reset {
		up.replace = ""
	}
	return replace
}

func (up *rtpUpConnection) Id() string {
	return up.id
}

func (up *rtpUpConnection) Label() string {
	return up.label
}

func (up *rtpUpConnection) User() (string, string) {
	return up.userId, up.username
}

func (up *rtpUpConnection) AddLocal(local conn.Down) error {
	up.mu.Lock()
	defer up.mu.Unlock()
	for _, t := range up.local {
		if t == local {
			return nil
		}
	}
	up.local = append(up.local, local)
	return nil
}

func (up *rtpUpConnection) DelLocal(local conn.Down) bool {
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

func (up *rtpUpConnection) getLocal() []conn.Down {
	up.mu.Lock()
	defer up.mu.Unlock()
	local := make([]conn.Down, len(up.local))
	copy(local, up.local)
	return local
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

// pushConnNow pushes a connection to all of the clients in a group
func pushConnNow(up *rtpUpConnection, g *group.Group, cs []group.Client) {
	up.mu.Lock()
	up.pushed = true
	replace := up.replace
	up.replace = ""
	tracks := make([]conn.UpTrack, len(up.tracks))
	for i, t := range up.tracks {
		tracks[i] = t
	}
	up.mu.Unlock()

	for _, c := range cs {
		c.PushConn(g, up.id, up, tracks, replace)
	}
}

// pushConn schedules a call to pushConnNow
func pushConn(up *rtpUpConnection, g *group.Group, cs []group.Client) {
	up.mu.Lock()
	up.pushed = false
	up.mu.Unlock()

	go func(g *group.Group, cs []group.Client) {
		time.Sleep(200 * time.Millisecond)
		up.mu.Lock()
		pushed := up.pushed
		up.pushed = true
		up.mu.Unlock()
		if !pushed {
			pushConnNow(up, g, cs)
		}
	}(g, cs)
}

func newUpConn(c group.Client, id string, label string, offer string) (*rtpUpConnection, error) {
	var o sdp.SessionDescription
	err := o.Unmarshal([]byte(offer))
	if err != nil {
		return nil, err
	}

	api, err := c.Group().API()
	if err != nil {
		return nil, err
	}
	pc, err := api.NewPeerConnection(*ice.ICEConfiguration())
	if err != nil {
		return nil, err
	}

	for _, m := range o.MediaDescriptions {
		_, err = pc.AddTransceiverFromKind(
			webrtc.NewRTPCodecType(m.MediaName.Media),
			webrtc.RtpTransceiverInit{
				Direction: webrtc.RTPTransceiverDirectionRecvonly,
			},
		)
		if err != nil {
			pc.Close()
			return nil, err
		}
	}

	up := &rtpUpConnection{id: id, client: c, label: label, pc: pc}

	pc.OnTrack(func(remote *webrtc.TrackRemote, receiver *webrtc.RTPReceiver) {
		up.mu.Lock()

		track := &rtpUpTrack{
			track:      remote,
			cache:      packetcache.New(minPacketCache(remote)),
			rate:       estimator.New(time.Second),
			jitter:     jitter.New(remote.Codec().ClockRate),
			atomics:    &upTrackAtomics{},
			localCh:    make(chan trackAction, 2),
			readerDone: make(chan struct{}),
		}

		up.tracks = append(up.tracks, track)

		go readLoop(up, track)

		go rtcpUpListener(up, track, receiver)

		up.mu.Unlock()

		pushConn(up, c.Group(), c.Group().GetClients(c))
	})

	pushConn(up, c.Group(), c.Group().GetClients(c))
	go rtcpUpSender(up)

	return up, nil
}

var ErrUnsupportedFeedback = errors.New("unsupported feedback type")
var ErrRateLimited = errors.New("rate limited")

func (up *rtpUpConnection) sendPLI(track *rtpUpTrack) error {
	if !track.hasRtcpFb("nack", "pli") {
		return ErrUnsupportedFeedback
	}
	last := atomic.LoadUint64(&track.atomics.lastPLI)
	now := rtptime.Jiffies()
	if now >= last && now-last < rtptime.JiffiesPerSec/2 {
		return ErrRateLimited
	}
	atomic.StoreUint64(&track.atomics.lastPLI, now)
	return sendPLI(up.pc, track.track.SSRC())
}

func sendPLI(pc *webrtc.PeerConnection, ssrc webrtc.SSRC) error {
	return pc.WriteRTCP([]rtcp.Packet{
		&rtcp.PictureLossIndication{MediaSSRC: uint32(ssrc)},
	})
}

func (up *rtpUpConnection) sendNACK(track *rtpUpTrack, first uint16, bitmap uint16) error {
	if !track.hasRtcpFb("nack", "") {
		return ErrUnsupportedFeedback
	}

	err := sendNACKs(up.pc, track.track.SSRC(),
		[]rtcp.NackPair{{first, rtcp.PacketBitmap(bitmap)}},
	)
	if err == nil {
		track.cache.Expect(1 + bits.OnesCount16(bitmap))
	}
	return err
}

func (up *rtpUpConnection) sendNACKs(track *rtpUpTrack, seqnos []uint16) error {
	count := len(seqnos)
	if count == 0 {
		return nil
	}

	if !track.hasRtcpFb("nack", "") {
		return ErrUnsupportedFeedback
	}

	var nacks []rtcp.NackPair

	for len(seqnos) > 0 {
		if len(nacks) >= 240 {
			log.Printf("NACK: packet overflow")
			break
		}
		var f, b uint16
		f, b, seqnos = packetcache.ToBitmap(seqnos)
		nacks = append(nacks, rtcp.NackPair{f, rtcp.PacketBitmap(b)})
	}
	err := sendNACKs(up.pc, track.track.SSRC(), nacks)
	if err == nil {
		track.cache.Expect(count)
	}
	return err
}

func sendNACKs(pc *webrtc.PeerConnection, ssrc webrtc.SSRC, nacks []rtcp.NackPair) error {
	packet := rtcp.Packet(
		&rtcp.TransportLayerNack{
			MediaSSRC: uint32(ssrc),
			Nacks:     nacks,
		},
	)
	return pc.WriteRTCP([]rtcp.Packet{packet})
}

func gotNACK(conn *rtpDownConnection, track *rtpDownTrack, p *rtcp.TransportLayerNack) {
	var unhandled []uint16
	var packet rtp.Packet
	buf := make([]byte, packetcache.BufSize)
	for _, nack := range p.Nacks {
		nack.Range(func(s uint16) bool {
			ok, seqno, _ := track.packetmap.Reverse(s)
			if !ok {
				return true
			}
			l := track.remote.GetRTP(seqno, buf)
			if l == 0 {
				unhandled = append(unhandled, seqno)
				return true
			}
			err := packet.Unmarshal(buf[:l])
			if err != nil {
				return true
			}
			err = track.track.WriteRTP(&packet)
			if err != nil {
				log.Printf("WriteRTP: %v", err)
				return false
			}
			return true
		})
	}
	if len(unhandled) == 0 {
		return
	}

	track.remote.Nack(conn.remote, unhandled)
}

func (track *rtpUpTrack) Nack(conn conn.Up, nacks []uint16) error {
	track.mu.Lock()
	defer track.mu.Unlock()

	doit := len(track.bufferedNACKs) == 0

outer:
	for _, nack := range nacks {
		for _, seqno := range track.bufferedNACKs {
			if seqno == nack {
				continue outer
			}
		}
		track.bufferedNACKs = append(track.bufferedNACKs, nack)
	}

	if doit {
		up, ok := conn.(*rtpUpConnection)
		if !ok {
			log.Printf("Nack: unexpected type %T", conn)
			return errors.New("unexpected connection type")
		}
		go nackWriter(up, track)
	}
	return nil
}

func rtcpUpListener(conn *rtpUpConnection, track *rtpUpTrack, r *webrtc.RTPReceiver) {
	buf := make([]byte, 1500)

	for {
		firstSR := false
		n, _, err := r.ReadSimulcast(buf, track.track.RID())
		if err != nil {
			if err != io.EOF && err != io.ErrClosedPipe {
				log.Printf("Read RTCP: %v", err)
			}
			return
		}
		ps, err := rtcp.Unmarshal(buf[:n])
		if err != nil {
			log.Printf("Unmarshal RTCP: %v", err)
			continue
		}

		jiffies := rtptime.Jiffies()

		for _, p := range ps {
			local := track.getLocal()
			switch p := p.(type) {
			case *rtcp.SenderReport:
				track.mu.Lock()
				if track.srTime == 0 {
					firstSR = true
				}
				track.srTime = jiffies
				track.srNTPTime = p.NTPTime
				track.srRTPTime = p.RTPTime
				track.mu.Unlock()
				for _, l := range local {
					l.SetTimeOffset(p.NTPTime, p.RTPTime)
				}
			case *rtcp.SourceDescription:
				for _, c := range p.Chunks {
					if c.Source != uint32(track.track.SSRC()) {
						continue
					}
					for _, i := range c.Items {
						if i.Type != rtcp.SDESCNAME {
							continue
						}
						track.cname.Store(i.Text)
						for _, l := range local {
							l.SetCname(i.Text)
						}
					}
				}
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

func sendUpRTCP(up *rtpUpConnection) error {
	tracks := up.getTracks()

	if len(up.tracks) == 0 {
		state := up.pc.ConnectionState()
		if state == webrtc.PeerConnectionStateClosed {
			return io.ErrClosedPipe
		}
		return nil
	}

	now := rtptime.Jiffies()

	reports := make([]rtcp.ReceptionReport, 0, len(up.tracks))
	for _, t := range tracks {
		updateUpTrack(t)
		stats := t.cache.GetStats(true)
		var totalLost uint32
		if stats.TotalExpected > stats.TotalReceived {
			totalLost = stats.TotalExpected - stats.TotalReceived
		}
		var fractionLost uint32
		if stats.Expected > stats.Received {
			lost := stats.Expected - stats.Received
			fractionLost = lost * 256 / stats.Expected
			if fractionLost >= 255 {
				fractionLost = 255
			}
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
			SSRC:               uint32(t.track.SSRC()),
			FractionLost:       uint8(fractionLost),
			TotalLost:          totalLost,
			LastSequenceNumber: stats.ESeqno,
			Jitter:             t.jitter.Jitter(),
			LastSenderReport:   uint32(srNTPTime >> 16),
			Delay:              uint32(delay),
		})
	}

	packets := []rtcp.Packet{
		&rtcp.ReceiverReport{
			Reports: reports,
		},
	}

	var ssrcs []uint32
	var rate uint64
	for _, t := range tracks {
		if !t.hasRtcpFb("goog-remb", "") {
			continue
		}
		ssrcs = append(ssrcs, uint32(t.track.SSRC()))
		if t.Kind() == webrtc.RTPCodecTypeAudio {
			rate += 100 * 1024
		} else if t.Label() == "l" {
			rate += group.LowBitrate
		} else {
			minrate := ^uint64(0)
			maxrate := uint64(group.MinBitrate)
			maxlayer := 0
			local := t.getLocal()
			for _, down := range local {
				r, l := down.GetMaxBitrate()
				if maxlayer < l {
					maxlayer = l
				}
				if r < group.MinBitrate {
					r = group.MinBitrate
				}
				if minrate > r {
					minrate = r
				}
				if maxrate < r {
					maxrate = r
				}
			}
			// assume that each layer takes two times less
			// throughput than the higher one.  Then we've
			// got enough slack for a factor of 2^(layers-1).
			for i := 0; i < maxlayer; i++ {
				if minrate < ^uint64(0)/2 {
					minrate *= 2
				}
			}
			if minrate < maxrate {
				rate += minrate
			} else {
				rate += maxrate
			}
		}
	}

	if rate < ^uint64(0) && len(ssrcs) > 0 {
		packets = append(packets,
			&rtcp.ReceiverEstimatedMaximumBitrate{
				Bitrate: rate,
				SSRCs:   ssrcs,
			},
		)
	}
	return up.pc.WriteRTCP(packets)
}

func rtcpUpSender(conn *rtpUpConnection) {
	for {
		time.Sleep(time.Second)
		err := sendUpRTCP(conn)
		if err != nil {
			if err == io.EOF || err == io.ErrClosedPipe {
				return
			}
			log.Printf("sendUpRTCP: %v", err)
		}
	}
}

func sendSR(conn *rtpDownConnection) error {
	tracks := conn.getTracks()

	packets := make([]rtcp.Packet, 0, len(tracks))

	now := time.Now()
	nowNTP := rtptime.TimeToNTP(now)
	jiffies := rtptime.TimeToJiffies(now)

	for _, t := range tracks {
		clockrate := t.track.Codec().ClockRate

		var nowRTP uint32

		remoteNTP, remoteRTP := t.getTimeOffset()
		if remoteNTP != 0 {
			srTime := rtptime.NTPToTime(remoteNTP)
			d := now.Sub(srTime)
			if d > 0 && d < time.Hour {
				delay := rtptime.FromDuration(
					d, clockrate,
				)
				nowRTP = remoteRTP + uint32(delay)
			}

			p, b := t.rate.Totals()
			packets = append(packets,
				&rtcp.SenderReport{
					SSRC:        uint32(t.ssrc),
					NTPTime:     nowNTP,
					RTPTime:     nowRTP,
					PacketCount: p,
					OctetCount:  b,
				})
			t.setSRTime(jiffies, nowNTP)
		}

		cname, ok := t.cname.Load().(string)
		if ok && cname != "" {
			item := rtcp.SourceDescriptionItem{
				Type: rtcp.SDESCNAME,
				Text: cname,
			}
			packets = append(packets,
				&rtcp.SourceDescription{
					Chunks: []rtcp.SourceDescriptionChunk{
						{
							Source: uint32(t.ssrc),
							Items:  []rtcp.SourceDescriptionItem{item},
						},
					},
				},
			)
		}
	}

	if len(packets) == 0 {
		state := conn.pc.ConnectionState()
		if state == webrtc.PeerConnectionStateClosed {
			return io.ErrClosedPipe
		}
		return nil
	}

	return conn.pc.WriteRTCP(packets)
}

func rtcpDownSender(conn *rtpDownConnection) {
	for {
		time.Sleep(time.Second / 2)
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
	rate := track.maxBitrate.Get(now)
	if rate < minLossRate || rate > maxLossRate {
		// no recent feedback, reset
		rate = initLossRate
	}
	if loss < 5 {
		// if our actual rate is low, then we're not probing the
		// bottleneck
		r, _ := track.rate.Estimate()
		actual := 8 * uint64(r)
		if actual >= (rate*3)/4 {
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
	track.maxBitrate.Set(rate, now)
}

func rtcpDownListener(conn *rtpDownConnection, track *rtpDownTrack, s *webrtc.RTPSender) {
	lastFirSeqno := uint8(0)

	buf := make([]byte, 1500)

	for {
		n, _, err := s.Read(buf)
		if err != nil {
			if err != io.EOF && err != io.ErrClosedPipe {
				log.Printf("Read RTCP: %v", err)
			}
			return
		}
		ps, err := rtcp.Unmarshal(buf[:n])
		if err != nil {
			log.Printf("Unmarshal RTCP: %v", err)
			continue
		}

		adjust := false
		jiffies := rtptime.Jiffies()

		for _, p := range ps {
			switch p := p.(type) {
			case *rtcp.PictureLossIndication:
				track.remote.RequestKeyframe()
			case *rtcp.FullIntraRequest:
				found := false
				var seqno uint8
				for _, entry := range p.FIR {
					if entry.SSRC == uint32(track.ssrc) {
						found = true
						seqno = entry.SequenceNumber
						break
					}
				}
				if !found {
					log.Printf("Misdirected FIR")
					continue
				}

				if seqno != lastFirSeqno {
					track.remote.RequestKeyframe()
				}
			case *rtcp.ReceiverEstimatedMaximumBitrate:
				track.maxREMBBitrate.Set(p.Bitrate, jiffies)
				adjust = true
			case *rtcp.ReceiverReport:
				for _, r := range p.Reports {
					if r.SSRC == uint32(track.ssrc) {
						handleReport(track, r, jiffies)
						adjust = true
					}
				}
			case *rtcp.SenderReport:
				for _, r := range p.Reports {
					if r.SSRC == uint32(track.ssrc) {
						handleReport(track, r, jiffies)
					}
				}
			case *rtcp.TransportLayerNack:
				gotNACK(conn, track, p)
			}
		}
		if adjust {
			track.adjustLayer()
		}
	}
}

func handleReport(track *rtpDownTrack, report rtcp.ReceptionReport, jiffies uint64) {
	track.stats.Set(report.FractionLost, report.Jitter, jiffies)
	track.updateRate(report.FractionLost, jiffies)

	if report.LastSenderReport != 0 {
		jiffies := rtptime.Jiffies()
		srTime, srNTPTime := track.getSRTime()
		if jiffies < srTime || jiffies-srTime > 8*rtptime.JiffiesPerSec {
			return
		}
		if report.LastSenderReport == uint32(srNTPTime>>16) {
			delay := uint64(report.Delay) *
				(rtptime.JiffiesPerSec / 0x10000)
			if delay > jiffies-srTime {
				return
			}
			rtt := (jiffies - srTime) - delay
			oldrtt := track.getRTT()
			newrtt := rtt
			if oldrtt > 0 {
				newrtt = (3*oldrtt + rtt) / 4
			}
			track.setRTT(newrtt)
		}
	}
}

func minPacketCache(track *webrtc.TrackRemote) int {
	if track.Kind() == webrtc.RTPCodecTypeVideo {
		return 128
	}
	return 24
}

func updateUpTrack(track *rtpUpTrack) {
	now := rtptime.Jiffies()

	clockrate := track.track.Codec().ClockRate
	local := track.getLocal()
	var maxrto uint64
	for _, l := range local {
		ll, ok := l.(*rtpDownTrack)
		if ok {
			_, j := ll.stats.Get(now)
			jitter := uint64(j) *
				(rtptime.JiffiesPerSec / uint64(clockrate))
			rtt := ll.getRTT()
			rto := rtt + 4*jitter
			if rto > maxrto {
				maxrto = rto
			}
		}
	}
	_, r := track.rate.Estimate()
	packets := int((uint64(r) * maxrto * 4) / rtptime.JiffiesPerSec)
	min := minPacketCache(track.track)
	if packets < min {
		packets = min
	}
	if packets > 1024 {
		packets = 1024
	}
	track.cache.ResizeCond(packets)
}
