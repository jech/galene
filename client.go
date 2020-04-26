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

	"github.com/gorilla/websocket"
	"github.com/pion/rtcp"
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
	c.group.mu.Lock()
	defer c.group.mu.Unlock()

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
	c.group.mu.Lock()
	defer c.group.mu.Unlock()
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

	pc.OnICECandidate(func(candidate *webrtc.ICECandidate) {
		sendICE(c, id, candidate)
	})

	pc.OnTrack(func(remote *webrtc.Track, receiver *webrtc.RTPReceiver) {
		local, err := pc.NewTrack(
			remote.PayloadType(),
			remote.SSRC(),
			remote.ID(),
			remote.Label())
		if err != nil {
			log.Printf("%v", err)
			return
		}

		c.group.mu.Lock()
		u, ok := c.up[id]
		if !ok {
			log.Printf("Unknown connection")
			c.group.mu.Unlock()
			return
		}
		u.pairs = append(u.pairs, trackPair{
			remote:     remote,
			local:      local,
			maxBitrate: ^uint64(0),
		})
		done := len(u.pairs) >= u.trackCount
		c.group.mu.Unlock()

		clients := c.group.getClients(c)
		for _, cc := range clients {
			cc.action(addTrackAction{id, local, u, done})
			if done && u.label != "" {
				cc.action(addLabelAction{id, u.label})
			}
		}

		go func() {
			buf := make([]byte, 1500)
			for {
				i, err := remote.Read(buf)
				if err != nil {
					if err != io.EOF {
						log.Printf("%v", err)
					}
					break
				}

				_, err = local.Write(buf[:i])
				if err != nil && err != io.ErrClosedPipe {
					log.Printf("%v", err)
				}
			}
		}()
	})

	conn := &upConnection{id: id, pc: pc}

	c.group.mu.Lock()
	defer c.group.mu.Unlock()

	if c.up == nil {
		c.up = make(map[string]*upConnection)
	}
	if c.up[id] != nil {
		conn.pc.Close()
		return nil, errors.New("Adding duplicate connection")
	}
	c.up[id] = conn
	return conn, nil
}

func delUpConn(c *client, id string) {
	c.group.mu.Lock()
	defer c.group.mu.Unlock()

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
	for _, cc := range c.group.clients {
		for _, otherconn := range cc.down {
			if otherconn.remote == conn {
				cids = append(cids, clientId{cc, otherconn.id})
			}
		}
	}

	for _, cid := range cids {
		cid.client.action(delPCAction{cid.id})
	}

	conn.pc.Close()
	delete(c.up, id)
}

func getDownConn(c *client, id string) *downConnection {
	if c.down == nil {
		return nil
	}

	c.group.mu.Lock()
	defer c.group.mu.Unlock()
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

	c.group.mu.Lock()
	defer c.group.mu.Unlock()
	if c.down[id] != nil {
		conn.pc.Close()
		return nil, errors.New("Adding duplicate connection")
	}
	c.down[id] = conn
	return conn, nil
}

func delDownConn(c *client, id string) {
	c.group.mu.Lock()
	defer c.group.mu.Unlock()

	if c.down == nil {
		log.Printf("Deleting unknown connection")
		return
	}
	conn := c.down[id]
	if conn == nil {
		log.Printf("Deleting unknown connection")
		return
	}
	conn.pc.Close()
	delete(c.down, id)
}

func addDownTrack(c *client, id string, track *webrtc.Track, remote *upConnection) (*downConnection, *webrtc.RTPSender, error) {
	conn := getDownConn(c, id)
	if conn == nil {
		var err error
		conn, err = addDownConn(c, id, remote)
		if err != nil {
			return nil, nil, err
		}
	}

	s, err := conn.pc.AddTrack(track)
	if err != nil {
		return nil, nil, err
	}

	conn.tracks = append(conn.tracks,
		downTrack{track.SSRC(), new(timeStampedBitrate)},
	)

	go rtcpListener(c.group, conn, s,
		conn.tracks[len(conn.tracks)-1].maxBitrate)

	return conn, s, nil
}

var epoch = time.Now()

func msSinceEpoch() uint64 {
	return uint64(time.Since(epoch) / time.Millisecond)
}

func rtcpListener(g *group, c *downConnection, s *webrtc.RTPSender,
	bitrate *timeStampedBitrate) {
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
				err := sendPLI(c.remote.pc, p.MediaSSRC)
				if err != nil {
					log.Printf("sendPLI: %v", err)
				}
			case *rtcp.ReceiverEstimatedMaximumBitrate:
				ms := msSinceEpoch()
				// this is racy -- a reader might read the
				// data between the two writes.  This shouldn't
				// matter, we'll recover at the next sample.
				atomic.StoreUint64(
					&bitrate.bitrate,
					p.Bitrate,
				)
				atomic.StoreUint64(
					&bitrate.timestamp,
					uint64(ms),
				)
			case *rtcp.ReceiverReport:
			default:
				log.Printf("RTCP: %T", p)
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

func splitBitrate(bitrate uint32, audio, video bool) (uint32, uint32) {
	if audio && !video {
		return bitrate, 0
	}
	if !audio && video {
		return 0, bitrate
	}

	if bitrate < 6000 {
		return 6000, 0
	}

	if bitrate < 12000 {
		return bitrate, 0
	}
	audioRate := 8000 + (bitrate-8000)/4
	if audioRate > 96000 {
		audioRate = 96000
	}
	return audioRate, bitrate - audioRate
}

func updateUpBitrate(g *group, up *upConnection) {
	for i := range up.pairs {
		up.pairs[i].maxBitrate = ^uint64(0)
	}

	now := msSinceEpoch()

	g.Range(func(c *client) bool {
		for _, down := range c.down {
			if down.remote == up {
				for _, dt := range down.tracks {
					ms := atomic.LoadUint64(
						&dt.maxBitrate.timestamp,
					)
					bitrate := atomic.LoadUint64(
						&dt.maxBitrate.bitrate,
					)
					if bitrate == 0 {
						continue
					}

					if now - ms > 5000 {
						continue
					}

					for i, p := range up.pairs {
						if p.local.SSRC() == dt.ssrc {
							if p.maxBitrate > bitrate {
								up.pairs[i].maxBitrate = bitrate
								break
							}
						}
					}
				}
			}
		}
		return true
	})
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
			SSRCs: []uint32{ssrc},
		},
	})
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
						c, a.id, a.track,
						a.remote)
				if err != nil {
					return err
				}
				if a.done {
					err = negotiate(c, a.id, down.pc)
					if err != nil {
						return err
					}
				}
			case delPCAction:
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
					for i, p := range u.pairs {
						done = i >= u.trackCount-1
						a.c.action(addTrackAction{
							u.id, p.local, u,
							done,
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
	for _, u := range c.up {
		updateUpBitrate(c.group, u)
		for _, p := range u.pairs {
			bitrate := p.maxBitrate
			if bitrate != ^uint64(0) {
				if bitrate < 6000 {
					bitrate = 6000
				}
				err := sendREMB(u.pc, p.remote.SSRC(),
					uint64(bitrate))
				if err != nil {
					log.Printf("sendREMB: %v", err)
				}
			}
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
