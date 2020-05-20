// Copyright (c) 2020 by Juliusz Chroboczek.

// This is not open source software.  Copy it, and I'll break into your
// house and tell your three year-old that Santa doesn't exist.

package main

import (
	"encoding/json"
	"errors"
	"io"
	"log"
	"math"
	"math/bits"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"sfu/estimator"
	"sfu/jitter"
	"sfu/mono"
	"sfu/packetcache"

	"github.com/gorilla/websocket"
	"github.com/pion/rtcp"
	"github.com/pion/rtp"
	"github.com/pion/webrtc/v2"
)

var iceConf webrtc.Configuration
var iceOnce sync.Once

func iceConfiguration() webrtc.Configuration {
	iceOnce.Do(func() {
		var iceServers []webrtc.ICEServer
		file, err := os.Open(iceFilename)
		if err != nil {
			log.Printf("Open %v: %v", iceFilename, err)
			return
		}
		defer file.Close()
		d := json.NewDecoder(file)
		err = d.Decode(&iceServers)
		if err != nil {
			log.Printf("Get ICE configuration: %v", err)
			return
		}
		iceConf = webrtc.Configuration{
			ICEServers: iceServers,
		}
	})

	return iceConf
}

type protocolError string

func (err protocolError) Error() string {
	return string(err)
}

type userError string

func (err userError) Error() string {
	return string(err)
}

func errorToWSCloseMessage(err error) (string, []byte) {
	var code int
	var text string
	switch e := err.(type) {
	case *websocket.CloseError:
		code = websocket.CloseNormalClosure
	case protocolError:
		code = websocket.CloseProtocolError
		text = string(e)
	case userError:
		code = websocket.CloseNormalClosure
		text = string(e)
	default:
		code = websocket.CloseInternalServerErr
	}
	return text, websocket.FormatCloseMessage(code, text)
}

func isWSNormalError(err error) bool {
	return websocket.IsCloseError(err,
		websocket.CloseNormalClosure,
		websocket.CloseGoingAway)
}

type rateMap map[string]uint32

func (v *rateMap) UnmarshalJSON(b []byte) error {
	var m map[string]interface{}

	err := json.Unmarshal(b, &m)
	if err != nil {
		return err
	}

	n := make(map[string]uint32, len(m))
	for k, w := range m {
		switch w := w.(type) {
		case bool:
			if w {
				n[k] = ^uint32(0)
			} else {
				n[k] = 0
			}
		case float64:
			if w < 0 || w >= float64(^uint32(0)) {
				return errors.New("overflow")
			}
			n[k] = uint32(w)
		default:
			return errors.New("unexpected type in JSON map")
		}
	}
	*v = n
	return nil
}

func (v rateMap) MarshalJSON() ([]byte, error) {
	m := make(map[string]interface{}, len(v))
	for k, w := range v {
		switch w {
		case 0:
			m[k] = false
		case ^uint32(0):
			m[k] = true
		default:
			m[k] = w
		}
	}
	return json.Marshal(m)
}

type clientMessage struct {
	Type        string                     `json:"type"`
	Id          string                     `json:"id,omitempty"`
	Username    string                     `json:"username,omitempty"`
	Password    string                     `json:"password,omitempty"`
	Permissions userPermission             `json:"permissions,omitempty"`
	Group       string                     `json:"group,omitempty"`
	Value       string                     `json:"value,omitempty"`
	Me          bool                       `json:"me,omitempty"`
	Offer       *webrtc.SessionDescription `json:"offer,omitempty"`
	Answer      *webrtc.SessionDescription `json:"answer,omitempty"`
	Candidate   *webrtc.ICECandidateInit   `json:"candidate,omitempty"`
	Labels      map[string]string          `json:"labels,omitempty"`
	Del         bool                       `json:"del,omitempty"`
	Request     rateMap                    `json:"request,omitempty"`
}

type closeMessage struct {
	data []byte
}

func startClient(conn *websocket.Conn) (err error) {
	var m clientMessage

	err = conn.SetReadDeadline(time.Now().Add(15 * time.Second))
	if err != nil {
		conn.Close()
		return
	}
	err = conn.ReadJSON(&m)
	if err != nil {
		conn.Close()
		return
	}
	err = conn.SetReadDeadline(time.Time{})
	if err != nil {
		conn.Close()
		return
	}

	if m.Type != "handshake" {
		conn.Close()
		return
	}

	c := &client{
		id:       m.Id,
		username: m.Username,
		actionCh: make(chan interface{}, 10),
		done:     make(chan struct{}),
	}

	defer close(c.done)

	c.writeCh = make(chan interface{}, 25)
	defer func() {
		if isWSNormalError(err) {
			err = nil
		} else {
			m, e := errorToWSCloseMessage(err)
			if m != "" {
				c.write(clientMessage{
					Type:  "error",
					Value: m,
				})
			}
			select {
			case c.writeCh <- closeMessage{e}:
			case <-c.writerDone:
			}
		}
		close(c.writeCh)
		c.writeCh = nil
	}()

	c.writerDone = make(chan struct{})
	go clientWriter(conn, c.writeCh, c.writerDone)

	if strings.ContainsRune(m.Username, ' ') {
		err = userError("don't put spaces in your username")
		return
	}

	g, users, err := addClient(m.Group, c, m.Username, m.Password)
	if err != nil {
		return
	}
	c.group = g
	defer delClient(c)

	for _, u := range users {
		c.write(clientMessage{
			Type:     "user",
			Id:       u.id,
			Username: u.username,
		})
	}

	clients := g.getClients(nil)
	u := clientMessage{
		Type:     "user",
		Id:       c.id,
		Username: c.username,
	}
	for _, c := range clients {
		c.write(u)
	}

	defer func() {
		clients := g.getClients(c)
		u := clientMessage{
			Type:     "user",
			Id:       c.id,
			Username: c.username,
			Del:      true,
		}
		for _, c := range clients {
			c.write(u)
		}
	}()

	return clientLoop(c, conn)
}

func getUpConn(c *client, id string) *upConnection {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.up == nil {
		return nil
	}
	conn := c.up[id]
	if conn == nil {
		return nil
	}
	return conn
}

func getUpConns(c *client) []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	up := make([]string, 0, len(c.up))
	for id := range c.up {
		up = append(up, id)
	}
	return up
}

func addUpConn(c *client, id string) (*upConnection, error) {
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

	conn := &upConnection{id: id, pc: pc}

	c.mu.Lock()
	defer c.mu.Unlock()

	if c.up == nil {
		c.up = make(map[string]*upConnection)
	}
	if c.up[id] != nil {
		conn.pc.Close()
		return nil, errors.New("Adding duplicate connection")
	}
	c.up[id] = conn

	pc.OnICECandidate(func(candidate *webrtc.ICECandidate) {
		sendICE(c, id, candidate)
	})

	pc.OnICEConnectionStateChange(func(state webrtc.ICEConnectionState) {
		if state == webrtc.ICEConnectionStateFailed {
			c.action(connectionFailedAction{id: id})
		}
	})

	go rtcpUpSender(c, conn)

	pc.OnTrack(func(remote *webrtc.Track, receiver *webrtc.RTPReceiver) {
		c.mu.Lock()
		u, ok := c.up[id]
		if !ok {
			log.Printf("Unknown connection")
			c.mu.Unlock()
			return
		}
		track := &upTrack{
			track:      remote,
			label:      u.labels[remote.ID()],
			cache:      packetcache.New(96),
			rate:       estimator.New(time.Second),
			jitter:     jitter.New(remote.Codec().ClockRate),
			maxBitrate: ^uint64(0),
		}
		u.tracks = append(u.tracks, track)
		var tracks []*upTrack
		if u.complete() {
			tracks = make([]*upTrack, len(u.tracks))
			copy(tracks, u.tracks)
		}
		if remote.Kind() == webrtc.RTPCodecTypeVideo {
			atomic.AddUint32(&c.group.videoCount, 1)
		}
		c.mu.Unlock()

		if tracks != nil {
			clients := c.group.getClients(c)
			for _, cc := range clients {
				pushConn(cc, u, tracks, u.label)
			}
		}

		go readLoop(conn, track)

		go rtcpUpListener(conn, track, receiver)
	})

	return conn, nil
}

type packetIndex struct {
	seqno uint16
	index uint16
}

func readLoop(conn *upConnection, track *upTrack) {
	ch := make(chan packetIndex, 32)
	defer close(ch)
	go writeLoop(conn, track, ch)

	buf := make([]byte, packetcache.BufSize)
	var packet rtp.Packet
	for {
		bytes, err := track.track.Read(buf)
		if err != nil {
			if err != io.EOF {
				log.Printf("%v", err)
			}
			break
		}
		track.rate.Add(uint32(bytes))

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

		select {
		case ch <- packetIndex{packet.SequenceNumber, index}:
		default:
			// The writer is congested.  Drop the packet, and
			// leave it to NACK recovery if possible.
		}
	}
}

func writeLoop(conn *upConnection, track *upTrack, ch <-chan packetIndex) {
	var localTime uint64
	var local []*downTrack

	buf := make([]byte, packetcache.BufSize)
	var packet rtp.Packet

	for {
		now := mono.Microseconds()
		if now < localTime || now > localTime+500000 {
			local = track.getLocal()
			localTime = now
		}

		pi, ok := <-ch
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

		for _, l := range local {
			err := l.track.WriteRTP(&packet)
			if err != nil && err != io.ErrClosedPipe {
				log.Printf("%v", err)
			}
			l.rate.Add(uint32(bytes))
		}
	}
}

func rtcpUpListener(conn *upConnection, track *upTrack, r *webrtc.RTPReceiver) {
	for {
		ps, err := r.ReadRTCP()
		if err != nil {
			if err != io.EOF {
				log.Printf("ReadRTCP: %v", err)
			}
			return
		}

		for _, p := range ps {
			switch p := p.(type) {
			case *rtcp.SenderReport:
				atomic.StoreUint32(&track.lastSenderReport,
					uint32(p.NTPTime>>16))
				atomic.StoreUint32(&track.lastSenderReportTime,
					uint32(mono.Now(0x10000)))
			case *rtcp.SourceDescription:
			}
		}
	}
}

func sendRR(c *client, conn *upConnection) error {
	c.mu.Lock()
	if len(conn.tracks) == 0 {
		c.mu.Unlock()
		return nil
	}

	now := uint32(mono.Now(0x10000))

	reports := make([]rtcp.ReceptionReport, 0, len(conn.tracks))
	for _, t := range conn.tracks {
		expected, lost, totalLost, eseqno := t.cache.GetStats(true)
		if expected == 0 {
			expected = 1
		}
		if lost >= expected {
			lost = expected - 1
		}
		lastSR := atomic.LoadUint32(&t.lastSenderReport)
		delay := now - atomic.LoadUint32(&t.lastSenderReportTime)

		reports = append(reports, rtcp.ReceptionReport{
			SSRC:               t.track.SSRC(),
			FractionLost:       uint8((lost * 256) / expected),
			TotalLost:          totalLost,
			LastSequenceNumber: eseqno,
			Jitter:             t.jitter.Jitter(),
			LastSenderReport:   lastSR,
			Delay:              delay,
		})
	}
	c.mu.Unlock()

	return conn.pc.WriteRTCP([]rtcp.Packet{
		&rtcp.ReceiverReport{
			SSRC:    1,
			Reports: reports,
		},
	})
}

func rtcpUpSender(c *client, conn *upConnection) {
	for {
		time.Sleep(time.Second)
		err := sendRR(c, conn)
		if err != nil {
			if err == io.EOF || err == io.ErrClosedPipe {
				return
			}
			log.Printf("WriteRTCP: %v", err)
		}
	}
}

func delUpConn(c *client, id string) bool {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.up == nil {
		return false
	}

	conn := c.up[id]
	if conn == nil {
		return false
	}

	for _, track := range conn.tracks {
		if track.track.Kind() == webrtc.RTPCodecTypeVideo {
			count := atomic.AddUint32(&c.group.videoCount,
				^uint32(0))
			if count == ^uint32(0) {
				log.Printf("Negative track count!")
				atomic.StoreUint32(&c.group.videoCount, 0)
			}
		}
	}

	type clientId struct {
		client *client
		id     string
	}
	cids := make([]clientId, 0)

	clients := c.group.getClients(c)
	for _, cc := range clients {
		cc.mu.Lock()
		for _, otherconn := range cc.down {
			if otherconn.remote == conn {
				cids = append(cids, clientId{cc, otherconn.id})
			}
		}
		cc.mu.Unlock()
	}

	go func(cids []clientId) {
		for _, cid := range cids {
			cid.client.action(delConnAction{cid.id})
		}
	}(cids)

	conn.pc.Close()
	delete(c.up, id)
	return true
}

func getDownConn(c *client, id string) *downConnection {
	if c.down == nil {
		return nil
	}

	c.mu.Lock()
	defer c.mu.Unlock()
	conn := c.down[id]
	if conn == nil {
		return nil
	}
	return conn
}

func addDownConn(c *client, id string, remote *upConnection) (*downConnection, error) {
	pc, err := groups.api.NewPeerConnection(iceConfiguration())
	if err != nil {
		return nil, err
	}

	pc.OnICECandidate(func(candidate *webrtc.ICECandidate) {
		sendICE(c, id, candidate)
	})

	pc.OnTrack(func(remote *webrtc.Track, receiver *webrtc.RTPReceiver) {
		log.Printf("Got track on downstream connection")
	})

	pc.OnICEConnectionStateChange(func(state webrtc.ICEConnectionState) {
		if state == webrtc.ICEConnectionStateFailed {
			c.action(connectionFailedAction{id: id})
		}
	})

	if c.down == nil {
		c.down = make(map[string]*downConnection)
	}
	conn := &downConnection{id: id, pc: pc, remote: remote}

	c.mu.Lock()
	defer c.mu.Unlock()
	if c.down[id] != nil {
		conn.pc.Close()
		return nil, errors.New("Adding duplicate connection")
	}
	c.down[id] = conn
	return conn, nil
}

func delDownConn(c *client, id string) bool {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.down == nil {
		return false
	}
	conn := c.down[id]
	if conn == nil {
		return false
	}

	for _, track := range conn.tracks {
		found := track.remote.delLocal(track)
		if !found {
			log.Printf("Couldn't find remote track")
		}
	}
	conn.pc.Close()
	delete(c.down, id)
	return true
}

func addDownTrack(c *client, conn *downConnection, remoteTrack *upTrack, remoteConn *upConnection) (*webrtc.RTPSender, error) {
	local, err := conn.pc.NewTrack(
		remoteTrack.track.PayloadType(),
		remoteTrack.track.SSRC(),
		remoteTrack.track.ID(),
		remoteTrack.track.Label(),
	)
	if err != nil {
		return nil, err
	}

	s, err := conn.pc.AddTrack(local)
	if err != nil {
		return nil, err
	}

	track := &downTrack{
		track:          local,
		remote:         remoteTrack,
		maxLossBitrate: new(bitrate),
		maxREMBBitrate: new(bitrate),
		stats:          new(receiverStats),
		rate:           estimator.New(time.Second),
	}
	conn.tracks = append(conn.tracks, track)
	remoteTrack.addLocal(track)

	go rtcpDownListener(conn, track, s)

	return s, nil
}

const (
	minLossRate  = 9600
	initLossRate = 512 * 1000
	maxLossRate  = 1 << 30
)

func (track *downTrack) updateRate(loss uint8, now uint64) {
	rate := track.maxLossBitrate.Get(now)
	if rate > maxLossRate {
		// no recent feedback, reset
		rate = initLossRate
	}
	if loss < 5 {
		// if our actual rate is low, then we're not probing the
		// bottleneck
		actual := 8 * uint64(track.rate.Estimate())
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

func rtcpDownListener(conn *downConnection, track *downTrack, s *webrtc.RTPSender) {
	for {
		ps, err := s.ReadRTCP()
		if err != nil {
			if err != io.EOF {
				log.Printf("ReadRTCP: %v", err)
			}
			return
		}

		for _, p := range ps {
			switch p := p.(type) {
			case *rtcp.PictureLossIndication:
				err := conn.remote.sendPLI(track.remote)
				if err != nil {
					log.Printf("sendPLI: %v", err)
				}
			case *rtcp.ReceiverEstimatedMaximumBitrate:
				track.maxREMBBitrate.Set(
					p.Bitrate, mono.Microseconds(),
				)
			case *rtcp.ReceiverReport:
				for _, r := range p.Reports {
					if r.SSRC == track.track.SSRC() {
						now := mono.Microseconds()
						track.stats.Set(
							r.FractionLost,
							r.Jitter,
							now,
						)
						track.updateRate(
							r.FractionLost,
							now,
						)
					}
				}
			case *rtcp.TransportLayerNack:
				maxBitrate := track.GetMaxBitrate(
					mono.Microseconds(),
				)
				bitrate := track.rate.Estimate()
				if uint64(bitrate) < maxBitrate {
					sendRecovery(p, track)
				}
			}
		}
	}
}

func trackKinds(down *downConnection) (audio bool, video bool) {
	if down.pc == nil {
		return
	}

	for _, s := range down.pc.GetSenders() {
		track := s.Track()
		if track == nil {
			continue
		}
		switch track.Kind() {
		case webrtc.RTPCodecTypeAudio:
			audio = true
		case webrtc.RTPCodecTypeVideo:
			video = true
		}
	}
	return
}

func updateUpBitrate(up *upConnection, maxVideoRate uint64) {
	now := mono.Microseconds()

	for _, track := range up.tracks {
		isvideo := track.track.Kind() == webrtc.RTPCodecTypeVideo
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
		}
		track.maxBitrate = rate
	}
}

func (up *upConnection) sendPLI(track *upTrack) error {
	if !track.hasRtcpFb("nack", "pli") {
		return nil
	}
	last := atomic.LoadUint64(&track.lastPLI)
	now := mono.Microseconds()
	if now >= last && now-last < 200000 {
		return nil
	}
	atomic.StoreUint64(&track.lastPLI, now)
	return sendPLI(up.pc, track.track.SSRC())
}

func sendPLI(pc *webrtc.PeerConnection, ssrc uint32) error {
	return pc.WriteRTCP([]rtcp.Packet{
		&rtcp.PictureLossIndication{MediaSSRC: ssrc},
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

func (up *upConnection) sendNACK(track *upTrack, first uint16, bitmap uint16) error {
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

func sendRecovery(p *rtcp.TransportLayerNack, track *downTrack) {
	var packet rtp.Packet
	buf := make([]byte, packetcache.BufSize)
	for _, nack := range p.Nacks {
		for _, seqno := range nack.PacketList() {
			l := track.remote.cache.Get(seqno, buf)
			if l == 0 {
				continue
			}
			err := packet.Unmarshal(buf[:l])
			if err != nil {
				continue
			}
			err = track.track.WriteRTP(&packet)
			if err != nil {
				log.Printf("%v", err)
				continue
			}
			track.rate.Add(uint32(l))
		}
	}
}

func negotiate(c *client, down *downConnection) error {
	offer, err := down.pc.CreateOffer(nil)
	if err != nil {
		return err
	}

	err = down.pc.SetLocalDescription(offer)
	if err != nil {
		return err
	}

	labels := make(map[string]string)
	for _, t := range down.tracks {
		labels[t.track.ID()] = t.remote.label
	}

	return c.write(clientMessage{
		Type:   "offer",
		Id:     down.id,
		Offer:  &offer,
		Labels: labels,
	})
}

func sendICE(c *client, id string, candidate *webrtc.ICECandidate) error {
	if candidate == nil {
		return nil
	}
	cand := candidate.ToJSON()
	return c.write(clientMessage{
		Type:      "ice",
		Id:        id,
		Candidate: &cand,
	})
}

func gotOffer(c *client, id string, offer webrtc.SessionDescription, labels map[string]string) error {
	var err error
	up, ok := c.up[id]
	if !ok {
		up, err = addUpConn(c, id)
		if err != nil {
			return err
		}
	}
	if c.username != "" {
		up.label = c.username
	}
	err = up.pc.SetRemoteDescription(offer)
	if err != nil {
		return err
	}

	answer, err := up.pc.CreateAnswer(nil)
	if err != nil {
		return err
	}

	err = up.pc.SetLocalDescription(answer)
	if err != nil {
		return err
	}

	up.labels = labels

	return c.write(clientMessage{
		Type:   "answer",
		Id:     id,
		Answer: &answer,
	})
}

func gotAnswer(c *client, id string, answer webrtc.SessionDescription) error {
	conn := getDownConn(c, id)
	if conn == nil {
		return protocolError("unknown id in answer")
	}
	err := conn.pc.SetRemoteDescription(answer)
	if err != nil {
		return err
	}
	return nil
}

func gotICE(c *client, candidate *webrtc.ICECandidateInit, id string) error {
	var pc *webrtc.PeerConnection
	down := getDownConn(c, id)
	if down != nil {
		pc = down.pc
	} else {
		up := getUpConn(c, id)
		if up == nil {
			return errors.New("unknown id in ICE")
		}
		pc = up.pc
	}
	return pc.AddICECandidate(*candidate)
}

func (c *client) setRequested(requested map[string]uint32) error {
	if c.down != nil {
		for id := range c.down {
			c.write(clientMessage{
				Type: "close",
				Id:   id,
			})
			delDownConn(c, id)
		}
	}

	c.requested = requested

	go func() {
		clients := c.group.getClients(c)
		for _, cc := range clients {
			cc.action(pushConnsAction{c})
		}
	}()

	return nil
}

func (c *client) isRequested(label string) bool {
	return c.requested[label] != 0
}

func addDownConnTracks(c *client, remote *upConnection, tracks []*upTrack) (*downConnection, error) {
	requested := false
	for _, t := range tracks {
		if c.isRequested(t.label) {
			requested = true
			break
		}
	}
	if !requested {
		return nil, nil
	}

	down, err := addDownConn(c, remote.id, remote)
	if err != nil {
		return nil, err
	}

	for _, t := range tracks {
		if !c.isRequested(t.label) {
			continue
		}
		_, err = addDownTrack(c, down, t, remote)
		if err != nil {
			delDownConn(c, down.id)
			return nil, err
		}
	}

	return down, nil
}

func pushConn(c *client, conn *upConnection, tracks []*upTrack, label string) {
	c.action(addConnAction{conn, tracks})
	if label != "" {
		c.action(addLabelAction{conn.id, conn.label})
	}
}

func clientLoop(c *client, conn *websocket.Conn) error {
	read := make(chan interface{}, 1)
	go clientReader(conn, read, c.done)

	defer func() {
		c.setRequested(map[string]uint32{})
		if c.up != nil {
			for id := range c.up {
				delUpConn(c, id)
			}
		}
	}()

	c.write(clientMessage{
		Type:        "permissions",
		Permissions: c.permissions,
	})

	h := c.group.getChatHistory()
	for _, m := range h {
		err := c.write(clientMessage{
			Type:     "chat",
			Id:       m.id,
			Username: m.user,
			Value:    m.value,
			Me:       m.me,
		})
		if err != nil {
			return err
		}
	}

	readTime := time.Now()

	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	slowTicker := time.NewTicker(10 * time.Second)
	defer slowTicker.Stop()

	for {
		select {
		case m, ok := <-read:
			if !ok {
				return errors.New("reader died")
			}
			switch m := m.(type) {
			case clientMessage:
				readTime = time.Now()
				err := handleClientMessage(c, m)
				if err != nil {
					return err
				}
			case error:
				return m
			}
		case a := <-c.actionCh:
			switch a := a.(type) {
			case addConnAction:
				down, err := addDownConnTracks(
					c, a.conn, a.tracks,
				)
				if err != nil {
					return err
				}
				if down != nil {
					err = negotiate(c, down)
					if err != nil {
						return err
					}
				}
			case delConnAction:
				found := delDownConn(c, a.id)
				if found {
					c.write(clientMessage{
						Type: "close",
						Id:   a.id,
					})
				}
			case addLabelAction:
				c.write(clientMessage{
					Type:  "label",
					Id:    a.id,
					Value: a.label,
				})
			case pushConnsAction:
				for _, u := range c.up {
					tracks := make([]*upTrack, len(u.tracks))
					copy(tracks, u.tracks)
					go pushConn(a.c, u, tracks, u.label)
				}
			case connectionFailedAction:
				found := delUpConn(c, a.id)
				if found {
					c.write(clientMessage{
						Type:  "error",
						Value: "connection failed",
					})
					c.write(clientMessage{
						Type: "abort",
						Id:   a.id,
					})
					continue
				}
				// What should we do if a downstream
				// connection fails?  Renegotiate?
			case permissionsChangedAction:
				c.write(clientMessage{
					Type:        "permissions",
					Permissions: c.permissions,
				})
				if !c.permissions.Present {
					ids := getUpConns(c)
					for _, id := range ids {
						c.write(clientMessage{
							Type: "abort",
							Id:   id,
						})
						delUpConn(c, id)
					}
				}
			case kickAction:
				return userError("you have been kicked")
			default:
				log.Printf("unexpected action %T", a)
				return errors.New("unexpected action")
			}
		case <-ticker.C:
			sendRateUpdate(c)
		case <-slowTicker.C:
			if time.Since(readTime) > 90*time.Second {
				return errors.New("client is dead")
			}
			if time.Since(readTime) > 60*time.Second {
				err := c.write(clientMessage{
					Type: "ping",
				})
				if err != nil {
					return err
				}
			}
		}
	}
}

func handleClientMessage(c *client, m clientMessage) error {
	switch m.Type {
	case "request":
		err := c.setRequested(m.Request)
		if err != nil {
			return err
		}
	case "offer":
		if !c.permissions.Present {
			c.write(clientMessage{
				Type: "abort",
				Id:   m.Id,
			})
			return c.error(userError("not authorised"))
		}
		if m.Offer == nil {
			return protocolError("null offer")
		}
		err := gotOffer(c, m.Id, *m.Offer, m.Labels)
		if err != nil {
			return err
		}
	case "answer":
		if m.Answer == nil {
			return protocolError("null answer")
		}
		err := gotAnswer(c, m.Id, *m.Answer)
		if err != nil {
			return err
		}
	case "close":
		found := delUpConn(c, m.Id)
		if !found {
			log.Printf("Deleting unknown up connection %v", m.Id)
		}
	case "ice":
		if m.Candidate == nil {
			return protocolError("null candidate")
		}
		err := gotICE(c, m.Candidate, m.Id)
		if err != nil {
			log.Printf("ICE: %v", err)
		}
	case "chat":
		c.group.addToChatHistory(m.Id, m.Username, m.Value, m.Me)
		clients := c.group.getClients(c)
		for _, cc := range clients {
			cc.write(m)
		}
	case "clearchat":
		c.group.clearChatHistory()
		m := clientMessage{Type: "clearchat"}
		clients := c.group.getClients(nil)
		for _, cc := range clients {
			cc.write(m)
		}
	case "op", "unop", "present", "unpresent":
		if !c.permissions.Op {
			c.error(userError("not authorised"))
			return nil
		}
		err := setPermission(c.group, m.Id, m.Type)
		if err != nil {
			return c.error(err)
		}
	case "lock", "unlock":
		if !c.permissions.Op {
			c.error(userError("not authorised"))
			return nil
		}
		var locked uint32
		if m.Type == "lock" {
			locked = 1
		}
		atomic.StoreUint32(&c.group.locked, locked)
	case "kick":
		if !c.permissions.Op {
			c.error(userError("not authorised"))
			return nil
		}
		err := kickClient(c.group, m.Id)
		if err != nil {
			return c.error(err)
		}
	case "pong":
		// nothing
	case "ping":
		c.write(clientMessage{
			Type: "pong",
		})
	default:
		log.Printf("unexpected message: %v", m.Type)
		return protocolError("unexpected message")
	}
	return nil
}

func sendRateUpdate(c *client) {
	type remb struct {
		pc      *webrtc.PeerConnection
		ssrc    uint32
		bitrate uint64
	}
	rembs := make([]remb, 0)

	maxVideoRate := ^uint64(0)
	count := atomic.LoadUint32(&c.group.videoCount)
	if count >= 3 {
		maxVideoRate = uint64(2000000 / math.Sqrt(float64(count)))
		if maxVideoRate < minVideoRate {
			maxVideoRate = minVideoRate
		}
	}

	c.mu.Lock()
	for _, u := range c.up {
		updateUpBitrate(u, maxVideoRate)
		for _, t := range u.tracks {
			if !t.hasRtcpFb("goog-remb", "") {
				continue
			}
			bitrate := t.maxBitrate
			if bitrate == ^uint64(0) {
				continue
			}
			rembs = append(rembs,
				remb{u.pc, t.track.SSRC(), bitrate})
		}
	}
	c.mu.Unlock()

	for _, r := range rembs {
		err := sendREMB(r.pc, r.ssrc, r.bitrate)
		if err != nil {
			log.Printf("sendREMB: %v", err)
		}
	}
}

func clientReader(conn *websocket.Conn, read chan<- interface{}, done <-chan struct{}) {
	defer close(read)
	for {
		var m clientMessage
		err := conn.ReadJSON(&m)
		if err != nil {
			select {
			case read <- err:
				return
			case <-done:
				return
			}
		}
		select {
		case read <- m:
		case <-done:
			return
		}
	}
}

func clientWriter(conn *websocket.Conn, ch <-chan interface{}, done chan<- struct{}) {
	defer func() {
		close(done)
		conn.Close()
	}()

	for {
		m, ok := <-ch
		if !ok {
			break
		}
		err := conn.SetWriteDeadline(
			time.Now().Add(2 * time.Second))
		if err != nil {
			return
		}
		switch m := m.(type) {
		case clientMessage:
			err := conn.WriteJSON(m)
			if err != nil {
				return
			}
		case closeMessage:
			err := conn.WriteMessage(websocket.CloseMessage, m.data)
			if err != nil {
				return
			}
		default:
			log.Printf("clientWiter: unexpected message %T", m)
			return
		}
	}
}
