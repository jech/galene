// Copyright (c) 2020 by Juliusz Chroboczek.

// This is not open source software.  Copy it, and I'll break into your
// house and tell your three year-old that Santa doesn't exist.

package main

import (
	"encoding/json"
	"errors"
	"io"
	"log"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"sfu/estimator"
	"sfu/packetcache"

	"github.com/gorilla/websocket"
	"github.com/pion/rtcp"
	"github.com/pion/rtp"
	"github.com/pion/sdp"
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
	Del         bool                       `json:"del,omitempty"`
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
			cache:      packetcache.New(96),
			rate:       estimator.New(time.Second),
			maxBitrate: ^uint64(0),
		}
		u.tracks = append(u.tracks, track)
		done := len(u.tracks) >= u.trackCount
		c.mu.Unlock()

		clients := c.group.getClients(c)
		for _, cc := range clients {
			cc.action(addTrackAction{track, u, done})
			if done && u.label != "" {
				cc.action(addLabelAction{id, u.label})
			}
		}

		go upLoop(conn, track)

		go rtcpUpListener(conn, track, receiver)
	})

	return conn, nil
}

func upLoop(conn *upConnection, track *upTrack) {
	buf := make([]byte, packetcache.BufSize)
	var packet rtp.Packet
	var local []*downTrack
	var localTime time.Time
	for {
		now := time.Now()
		if now.Sub(localTime) > time.Second/2 {
			local = track.getLocal()
			localTime = now
		}

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

		first := track.cache.Store(packet.SequenceNumber, buf[:bytes])
		if packet.SequenceNumber-first > 24 {
			first, bitmap := track.cache.BitmapGet()
			if bitmap != ^uint16(0) {
				err := conn.sendNACK(track, first, ^bitmap)
				if err != nil {
					log.Printf("%v", err)
				}
			}
		}

		for _, l := range local {
			if l.muted() {
				continue
			}
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

	ssrc := conn.tracks[0].track.SSRC()

	reports := make([]rtcp.ReceptionReport, 0, len(conn.tracks))
	for _, t := range conn.tracks {
		expected, lost, eseqno := t.cache.GetStats(true)
		if expected == 0 {
			expected = 1
		}
		if lost >= expected {
			lost = expected - 1
		}
		reports = append(reports, rtcp.ReceptionReport{
			SSRC:               t.track.SSRC(),
			LastSenderReport:   atomic.LoadUint32(&t.lastSenderReport),
			FractionLost:       uint8((lost * 256) / expected),
			TotalLost:          lost,
			LastSequenceNumber: eseqno,
		})
	}
	c.mu.Unlock()

	return conn.pc.WriteRTCP([]rtcp.Packet{
		&rtcp.ReceiverReport{
			SSRC:    ssrc,
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

func delUpConn(c *client, id string) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.up == nil {
		log.Printf("Deleting unknown connection")
		return
	}

	conn := c.up[id]
	if conn == nil {
		log.Printf("Deleting unknown connection")
		return
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

	for _, cid := range cids {
		cid.client.action(delConnAction{cid.id})
	}

	conn.pc.Close()
	delete(c.up, id)
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

func delDownConn(c *client, id string) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.down == nil {
		log.Printf("Deleting unknown connection")
		return
	}
	conn := c.down[id]
	if conn == nil {
		log.Printf("Deleting unknown connection")
		return
	}

	for _, track := range conn.tracks {
		found := track.remote.delLocal(track)
		if !found {
			log.Printf("Couldn't find remote track")
		}
		track.remote = nil
	}
	conn.pc.Close()
	delete(c.down, id)
}

func addDownTrack(c *client, id string, remoteTrack *upTrack, remoteConn *upConnection) (*downConnection, *webrtc.RTPSender, error) {
	conn := getDownConn(c, id)
	if conn == nil {
		var err error
		conn, err = addDownConn(c, id, remoteConn)
		if err != nil {
			return nil, nil, err
		}
	}

	local, err := conn.pc.NewTrack(
		remoteTrack.track.PayloadType(),
		remoteTrack.track.SSRC(),
		remoteTrack.track.ID(),
		remoteTrack.track.Label(),
	)
	if err != nil {
		return nil, nil, err
	}

	s, err := conn.pc.AddTrack(local)
	if err != nil {
		return nil, nil, err
	}

	track := &downTrack{
		track:      local,
		remote:     remoteTrack,
		maxBitrate: new(timeStampedBitrate),
		rate:       estimator.New(time.Second),
	}
	conn.tracks = append(conn.tracks, track)
	remoteTrack.addLocal(track)

	go rtcpDownListener(c.group, conn, track, s)

	return conn, s, nil
}

var epoch = time.Now()

func msSinceEpoch() uint64 {
	return uint64(time.Since(epoch) / time.Millisecond)
}

func rtcpDownListener(g *group, conn *downConnection, track *downTrack, s *webrtc.RTPSender) {
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
				if track.muted() {
					continue
				}
				err := conn.remote.sendPLI(track.remote)
				if err != nil {
					log.Printf("sendPLI: %v", err)
				}
			case *rtcp.ReceiverEstimatedMaximumBitrate:
				ms := msSinceEpoch()
				// this is racy -- a reader might read the
				// data between the two writes.  This shouldn't
				// matter, we'll recover at the next sample.
				atomic.StoreUint64(
					&track.maxBitrate.bitrate,
					p.Bitrate,
				)
				atomic.StoreUint64(
					&track.maxBitrate.timestamp,
					uint64(ms),
				)
			case *rtcp.ReceiverReport:
				for _, r := range p.Reports {
					if r.SSRC == track.track.SSRC() {
						atomic.StoreUint32(
							&track.loss,
							uint32(r.FractionLost),
						)
					}
				}
			case *rtcp.TransportLayerNack:
				if track.muted() {
					continue
				}
				sendRecovery(p, track)
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

func updateUpBitrate(up *upConnection) {
	now := msSinceEpoch()

	for _, track := range up.tracks {
		track.maxBitrate = ^uint64(0)
		local := track.getLocal()
		for _, l := range local {
			ms := atomic.LoadUint64(&l.maxBitrate.timestamp)
			bitrate := atomic.LoadUint64(&l.maxBitrate.bitrate)
			loss := atomic.LoadUint32(&l.loss)
			if now < ms || now > ms+5000 || bitrate == 0 {
				// no rate information
				l.setMuted(false)
				continue
			}

			isvideo := l.track.Kind() == webrtc.RTPCodecTypeVideo
			minrate1 := uint64(9600)
			minrate2 := uint64(19200)
			if isvideo {
				minrate1 = 256000
				minrate2 = 512000
			}
			if bitrate < minrate2 {
				if loss <= 13 {
					// less than 10% loss, go ahead
					bitrate = minrate2
				} else if loss <= 64 || !isvideo {
					if bitrate < minrate1 {
						bitrate = minrate1
					}
				} else {
					// video track with dramatic loss
					l.setMuted(true)
					continue
				}
			}
			l.setMuted(false)
			if track.maxBitrate > bitrate {
				track.maxBitrate = bitrate
			}
		}
	}
}

func (up *upConnection) sendPLI(track *upTrack) error {
	last := atomic.LoadUint64(&track.lastPLI)
	now := msSinceEpoch()
	if now >= last && now-last < 200 {
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
	return sendNACK(up.pc, track.track.SSRC(), first, bitmap)
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
	for _, nack := range p.Nacks {
		for _, seqno := range nack.PacketList() {
			raw := track.remote.cache.Get(seqno)
			if raw != nil {
				err := packet.Unmarshal(raw)
				if err != nil {
					continue
				}
				err = track.track.WriteRTP(&packet)
				if err != nil {
					log.Printf("%v", err)
				}
				track.rate.Add(uint32(len(raw)))
			}
		}
	}
}

func countMediaStreams(data string) (int, error) {
	desc := sdp.NewJSEPSessionDescription(false)
	err := desc.Unmarshal(data)
	if err != nil {
		return 0, err
	}
	return len(desc.MediaDescriptions), nil
}

func negotiate(c *client, id string, pc *webrtc.PeerConnection) error {
	offer, err := pc.CreateOffer(nil)
	if err != nil {
		return err
	}
	err = pc.SetLocalDescription(offer)
	if err != nil {
		return err
	}
	return c.write(clientMessage{
		Type:  "offer",
		Id:    id,
		Offer: &offer,
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

func gotOffer(c *client, offer webrtc.SessionDescription, id string) error {
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
	n, err := countMediaStreams(offer.SDP)
	if err != nil {
		log.Printf("Couldn't parse SDP: %v", err)
		n = 2
	}
	up.trackCount = n
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

	return c.write(clientMessage{
		Type:   "answer",
		Id:     id,
		Answer: &answer,
	})
}

func gotAnswer(c *client, answer webrtc.SessionDescription, id string) error {
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

func clientLoop(c *client, conn *websocket.Conn) error {
	read := make(chan interface{}, 1)
	go clientReader(conn, read, c.done)

	defer func() {
		if c.down != nil {
			for id := range c.down {
				c.write(clientMessage{
					Type: "close",
					Id:   id,
				})
				delDownConn(c, id)
			}
		}

		if c.up != nil {
			for id := range c.up {
				delUpConn(c, id)
			}
		}
	}()

	g := c.group

	c.write(clientMessage{
		Type:        "permissions",
		Permissions: c.permissions,
	})

	for _, cc := range g.getClients(c) {
		cc.action(pushTracksAction{c})
	}

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
			case addTrackAction:
				down, _, err :=
					addDownTrack(
						c, a.remote.id, a.track,
						a.remote)
				if err != nil {
					return err
				}
				if a.done {
					err = negotiate(c, a.remote.id, down.pc)
					if err != nil {
						return err
					}
				}
			case delConnAction:
				c.write(clientMessage{
					Type: "close",
					Id:   a.id,
				})
				delDownConn(c, a.id)
			case addLabelAction:
				c.write(clientMessage{
					Type:  "label",
					Id:    a.id,
					Value: a.label,
				})
			case pushTracksAction:
				for _, u := range c.up {
					var done bool
					for i, t := range u.tracks {
						done = i >= u.trackCount-1
						a.c.action(addTrackAction{
							t, u, done,
						})
					}
					if done && u.label != "" {
						a.c.action(addLabelAction{
							u.id, u.label,
						})

					}
				}
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
		err := gotOffer(c, *m.Offer, m.Id)
		if err != nil {
			return err
		}
	case "answer":
		if m.Answer == nil {
			return protocolError("null answer")
		}
		err := gotAnswer(c, *m.Answer, m.Id)
		if err != nil {
			return err
		}
	case "close":
		delUpConn(c, m.Id)
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

	c.mu.Lock()
	for _, u := range c.up {
		updateUpBitrate(u)
		for _, t := range u.tracks {
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
