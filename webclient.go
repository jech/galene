// Copyright (c) 2020 by Juliusz Chroboczek.

// This is not open source software.  Copy it, and I'll break into your
// house and tell your three year-old that Santa doesn't exist.

package main

import (
	"encoding/json"
	"errors"
	"log"
	"math"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"sfu/estimator"

	"github.com/gorilla/websocket"
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

type webClient struct {
	group       *group
	id          string
	username    string
	permissions userPermission
	requested   map[string]uint32
	done        chan struct{}
	writeCh     chan interface{}
	writerDone  chan struct{}
	actionCh    chan interface{}

	mu   sync.Mutex
	down map[string]*rtpDownConnection
	up   map[string]*rtpUpConnection
}

func (c *webClient) Group() *group {
	return c.group
}

func (c *webClient) Id() string {
	return c.id
}

func (c *webClient) Username() string {
	return c.username
}

func (c *webClient) pushClient(id, username string, add bool) error {
	return c.write(clientMessage{
		Type:     "user",
		Id:       id,
		Username: username,
		Del:      !add,
	})
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

func getUpConn(c *webClient, id string) *rtpUpConnection {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.up == nil {
		return nil
	}
	return c.up[id]
}

func getUpConns(c *webClient) []*rtpUpConnection {
	c.mu.Lock()
	defer c.mu.Unlock()
	up := make([]*rtpUpConnection, 0, len(c.up))
	for _, u := range c.up {
		up = append(up, u)
	}
	return up
}

func addUpConn(c *webClient, id string) (*rtpUpConnection, error) {
	conn, err := newUpConn(c, id)
	if err != nil {
		return nil, err
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	if c.up == nil {
		c.up = make(map[string]*rtpUpConnection)
	}
	if c.up[id] != nil || (c.down != nil && c.down[id] != nil) {
		conn.pc.Close()
		return nil, errors.New("Adding duplicate connection")
	}
	c.up[id] = conn

	conn.pc.OnICECandidate(func(candidate *webrtc.ICECandidate) {
		sendICE(c, id, candidate)
	})

	return conn, nil
}

func delUpConn(c *webClient, id string) bool {
	c.mu.Lock()
	if c.up == nil {
		c.mu.Unlock()
		return false
	}
	conn := c.up[id]
	if conn == nil {
		c.mu.Unlock()
		return false
	}
	delete(c.up, id)
	c.mu.Unlock()

	conn.mu.Lock()
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
	conn.mu.Unlock()

	conn.Close()
	return true
}

func getDownConn(c *webClient, id string) *rtpDownConnection {
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

func getConn(c *webClient, id string) iceConnection {
	up := getUpConn(c, id)
	if up != nil {
		return up
	}
	down := getDownConn(c, id)
	if down != nil {
		return down
	}
	return nil
}

func addDownConn(c *webClient, id string, remote upConnection) (*rtpDownConnection, error) {
	conn, err := newDownConn(id, remote)
	if err != nil {
		return nil, err
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	if c.down == nil {
		c.down = make(map[string]*rtpDownConnection)
	}

	if c.down[id] != nil || (c.up != nil && c.up[id] != nil) {
		conn.pc.Close()
		return nil, errors.New("Adding duplicate connection")
	}

	conn.pc.OnICECandidate(func(candidate *webrtc.ICECandidate) {
		sendICE(c, id, candidate)
	})

	err = remote.addLocal(conn)
	if err != nil {
		conn.pc.Close()
		return nil, err
	}

	c.down[id] = conn
	conn.close = func() error {
		return c.action(delConnAction{conn.id})
	}

	return conn, nil
}

func delDownConn(c *webClient, id string) bool {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.down == nil {
		return false
	}
	conn := c.down[id]
	if conn == nil {
		return false
	}

	conn.remote.delLocal(conn)
	for _, track := range conn.tracks {
		// we only insert the track after we get an answer, so
		// ignore errors here.
		track.remote.delLocal(track)
	}
	conn.pc.Close()
	delete(c.down, id)
	return true
}

func addDownTrack(c *webClient, conn *rtpDownConnection, remoteTrack upTrack, remoteConn upConnection) (*webrtc.RTPSender, error) {
	var pt uint8
	var ssrc uint32
	var id, label string
	switch rt := remoteTrack.(type) {
	case *rtpUpTrack:
		pt = rt.track.PayloadType()
		ssrc = rt.track.SSRC()
		id = rt.track.ID()
		label = rt.track.Label()
	default:
		return nil, errors.New("not implemented yet")
	}

	local, err := conn.pc.NewTrack(pt, ssrc, id, label)
	if err != nil {
		return nil, err
	}

	s, err := conn.pc.AddTrack(local)
	if err != nil {
		return nil, err
	}

	track := &rtpDownTrack{
		track:          local,
		remote:         remoteTrack,
		maxLossBitrate: new(bitrate),
		maxREMBBitrate: new(bitrate),
		stats:          new(receiverStats),
		rate:           estimator.New(time.Second),
	}
	conn.tracks = append(conn.tracks, track)

	go rtcpDownListener(conn, track, s)

	return s, nil
}

func negotiate(c *webClient, down *rtpDownConnection) error {
	offer, err := down.pc.CreateOffer(nil)
	if err != nil {
		return err
	}

	err = down.pc.SetLocalDescription(offer)
	if err != nil {
		return err
	}

	labels := make(map[string]string)
	for _, t := range down.pc.GetTransceivers() {
		var track *webrtc.Track
		if t.Sender() != nil {
			track = t.Sender().Track()
		}
		if track == nil {
			continue
		}

		for _, tr := range down.tracks {
			if tr.track == track {
				labels[t.Mid()] = tr.remote.Label()
			}
		}
	}

	return c.write(clientMessage{
		Type:   "offer",
		Id:     down.id,
		Offer:  &offer,
		Labels: labels,
	})
}

func sendICE(c *webClient, id string, candidate *webrtc.ICECandidate) error {
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

func gotOffer(c *webClient, id string, offer webrtc.SessionDescription, labels map[string]string) error {
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

	err = up.flushICECandidates()
	if err != nil {
		log.Printf("ICE: %v", err)
	}

	return c.write(clientMessage{
		Type:   "answer",
		Id:     id,
		Answer: &answer,
	})
}

func gotAnswer(c *webClient, id string, answer webrtc.SessionDescription) error {
	down := getDownConn(c, id)
	if down == nil {
		return protocolError("unknown id in answer")
	}
	err := down.pc.SetRemoteDescription(answer)
	if err != nil {
		return err
	}

	err = down.flushICECandidates()
	if err != nil {
		log.Printf("ICE: %v", err)
	}

	for _, t := range down.tracks {
		t.remote.addLocal(t)
	}
	return nil
}

func gotICE(c *webClient, candidate *webrtc.ICECandidateInit, id string) error {
	conn := getConn(c, id)
	if conn == nil {
		return errors.New("unknown id in ICE")
	}
	return conn.addICECandidate(candidate)
}

func (c *webClient) setRequested(requested map[string]uint32) error {
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

	go pushConns(c)
	return nil
}

func pushConns(c client) {
	clients := c.Group().getClients(c)
	for _, cc := range clients {
		ccc, ok := cc.(*webClient)
		if ok {
			ccc.action(pushConnsAction{c})
		}
	}
}

func (c *webClient) isRequested(label string) bool {
	return c.requested[label] != 0
}

func addDownConnTracks(c *webClient, remote upConnection, tracks []upTrack) (*rtpDownConnection, error) {
	requested := false
	for _, t := range tracks {
		if c.isRequested(t.Label()) {
			requested = true
			break
		}
	}
	if !requested {
		return nil, nil
	}

	down, err := addDownConn(c, remote.Id(), remote)
	if err != nil {
		return nil, err
	}

	for _, t := range tracks {
		if !c.isRequested(t.Label()) {
			continue
		}
		_, err = addDownTrack(c, down, t, remote)
		if err != nil {
			delDownConn(c, down.id)
			return nil, err
		}
	}

	go rtcpDownSender(down)

	return down, nil
}

func (c *webClient) pushConn(conn upConnection, tracks []upTrack, label string) error {
	err := c.action(addConnAction{conn, tracks})
	if err != nil {
		return err
	}
	if label != "" {
		err := c.action(addLabelAction{conn.Id(), conn.Label()})
		if err != nil {
			return err
		}
	}
	return nil
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

	if strings.ContainsRune(m.Username, ' ') {
		err = userError("don't put spaces in your username")
		return
	}

	c := &webClient{
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

	g, err := addClient(m.Group, c, m.Username, m.Password)
	if err != nil {
		return
	}
	c.group = g
	defer delClient(c)

	return clientLoop(c, conn)
}

func clientLoop(c *webClient, conn *websocket.Conn) error {
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
						log.Printf("Negotiate: %v", err)
						delDownConn(c, down.id)
						err = failConnection(
							c, down.id,
							"negotiation failed",
						)
						if err != nil {
							return err
						}
						continue
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
					tracks := u.getTracks()
					ts := make([]upTrack, len(tracks))
					for i, t := range tracks {
						ts[i] = t
					}
					go a.c.pushConn(u, ts, u.label)
				}
			case connectionFailedAction:
				found := delUpConn(c, a.id)
				if found {
					err := failConnection(c, a.id,
						"connection failed")
					if err != nil {
						return err
					}
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
					up := getUpConns(c)
					for _, u := range up {
						found := delUpConn(c, u.id)
						if found {
							failConnection(
								c, u.id,
								"permission denied",
							)
						}
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

func failConnection(c *webClient, id string, message string) error {
	if id != "" {
		err := c.write(clientMessage{
			Type: "abort",
			Id:   id,
		})
		if err != nil {
			return err
		}
	}
	if message != "" {
		err := c.error(userError(message))
		if err != nil {
			return err
		}
	}
	return nil
}

func setPermissions(g *group, id string, perm string) error {
	g.mu.Lock()
	defer g.mu.Unlock()

	client := g.getClientUnlocked(id)
	if client == nil {
		return userError("no such user")
	}

	c, ok := client.(*webClient)
	if !ok {
		return userError("this is not a real user")
	}

	switch perm {
	case "op":
		c.permissions.Op = true
		if g.description.AllowRecording {
			c.permissions.Record = true
		}
	case "unop":
		c.permissions.Op = false
		c.permissions.Record = false
	case "present":
		c.permissions.Present = true
	case "unpresent":
		c.permissions.Present = false
	default:
		return userError("unknown permission")
	}
	return c.action(permissionsChangedAction{})
}

func kickClient(g *group, id string) error {
	g.mu.Lock()
	defer g.mu.Unlock()

	client := g.getClientUnlocked(id)
	if client == nil {
		return userError("no such user")
	}

	c, ok := client.(*webClient)
	if !ok {
		return userError("this is not a real user")
	}

	return c.action(kickAction{})
}

func handleClientMessage(c *webClient, m clientMessage) error {
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
			log.Printf("gotOffer: %v", err)
			return failConnection(c, m.Id, "negotiation failed")
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
			cc, ok := cc.(*webClient)
			if ok {
				cc.write(m)
			}
		}
	case "clearchat":
		c.group.clearChatHistory()
		m := clientMessage{Type: "clearchat"}
		clients := c.group.getClients(nil)
		for _, cc := range clients {
			cc, ok := cc.(*webClient)
			if ok {
				cc.write(m)
			}
		}
	case "op", "unop", "present", "unpresent":
		if !c.permissions.Op {
			return c.error(userError("not authorised"))
		}
		err := setPermissions(c.group, m.Id, m.Type)
		if err != nil {
			return c.error(err)
		}
	case "lock", "unlock":
		if !c.permissions.Op {
			return c.error(userError("not authorised"))
		}
		var locked uint32
		if m.Type == "lock" {
			locked = 1
		}
		atomic.StoreUint32(&c.group.locked, locked)
	case "record":
		if !c.permissions.Record {
			return c.error(userError("not authorised"))
		}
		for _, cc := range c.group.getClients(c) {
			_, ok := cc.(*diskClient)
			if ok {
				return c.error(userError("already recording"))
			}
		}
		disk := &diskClient{
			group: c.group,
			id:    "recording",
		}
		_, err := addClient(c.group.name, disk, "", "")
		if err != nil {
			disk.Close()
			return c.error(err)
		}
		go pushConns(disk)
	case "unrecord":
		if !c.permissions.Record {
			return c.error(userError("not authorised"))
		}
		for _, cc := range c.group.getClients(c) {
			disk, ok := cc.(*diskClient)
			if ok {
				disk.Close()
				delClient(disk)
			}
		}
	case "kick":
		if !c.permissions.Op {
			return c.error(userError("not authorised"))
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

func sendRateUpdate(c *webClient) {
	maxVideoRate := ^uint64(0)
	count := atomic.LoadUint32(&c.group.videoCount)
	if count >= 3 {
		maxVideoRate = uint64(2000000 / math.Sqrt(float64(count)))
		if maxVideoRate < minVideoRate {
			maxVideoRate = minVideoRate
		}
	}

	up := getUpConns(c)

	for _, u := range up {
		tracks := u.getTracks()
		for _, t := range tracks {
			rate := updateUpTrack(t, maxVideoRate)
			if !t.hasRtcpFb("goog-remb", "") {
				continue
			}
			if rate == ^uint64(0) {
				continue
			}
			err := sendREMB(u.pc, t.track.SSRC(), rate)
			if err != nil {
				log.Printf("sendREMB: %v", err)
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

var ErrWriterDead = errors.New("client writer died")
var ErrClientDead = errors.New("client dead")

func (c *webClient) action(m interface{}) error {
	select {
	case c.actionCh <- m:
		return nil
	case <-c.done:
		return ErrClientDead
	}
}

func (c *webClient) write(m clientMessage) error {
	select {
	case c.writeCh <- m:
		return nil
	case <-c.writerDone:
		return ErrWriterDead
	}
}

func (c *webClient) error(err error) error {
	switch e := err.(type) {
	case userError:
		return c.write(clientMessage{
			Type:  "error",
			Value: string(e),
		})
	default:
		return err
	}
}
