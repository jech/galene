package rtpconn

import (
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"github.com/pion/webrtc/v3"

	"github.com/jech/galene/conn"
	"github.com/jech/galene/diskwriter"
	"github.com/jech/galene/estimator"
	"github.com/jech/galene/group"
	"github.com/jech/galene/ice"
)

func errorToWSCloseMessage(id string, err error) (*clientMessage, []byte) {
	var code int
	var m *clientMessage
	var text string
	switch e := err.(type) {
	case *websocket.CloseError:
		code = websocket.CloseNormalClosure
	case group.ProtocolError:
		code = websocket.CloseProtocolError
		m = &clientMessage{
			Type:       "usermessage",
			Kind:       "error",
			Dest:       id,
			Privileged: true,
			Value:      e.Error(),
		}
		text = e.Error()
	case group.UserError, group.KickError:
		code = websocket.CloseNormalClosure
		m = errorMessage(id, err)
		text = e.Error()
	default:
		code = websocket.CloseInternalServerErr
	}
	return m, websocket.FormatCloseMessage(code, text)
}

func isWSNormalError(err error) bool {
	return websocket.IsCloseError(err,
		websocket.CloseNormalClosure,
		websocket.CloseGoingAway)
}

type webClient struct {
	group       *group.Group
	id          string
	username    string
	password    string
	permissions group.ClientPermissions
	status      map[string]interface{}
	requested   map[string][]string
	done        chan struct{}
	writeCh     chan interface{}
	writerDone  chan struct{}
	actionCh    chan struct{}

	mu      sync.Mutex
	down    map[string]*rtpDownConnection
	up      map[string]*rtpUpConnection
	actions []interface{}
}

func (c *webClient) Group() *group.Group {
	return c.group
}

func (c *webClient) Id() string {
	return c.id
}

func (c *webClient) Username() string {
	return c.username
}

func (c *webClient) Challenge(group string, creds group.ClientCredentials) bool {
	if creds.Password == nil {
		return true
	}
	m, err := creds.Password.Match(c.password)
	if err != nil {
		log.Printf("Password match: %v", err)
		return false
	}
	return m
}

func (c *webClient) Permissions() group.ClientPermissions {
	return c.permissions
}

func (c *webClient) Status() map[string]interface{} {
	return c.status
}

func (c *webClient) SetPermissions(perms group.ClientPermissions) {
	c.permissions = perms
}

func (c *webClient) OverridePermissions(g *group.Group) bool {
	return false
}

func (c *webClient) PushClient(id, username string, permissions group.ClientPermissions, status map[string]interface{}, kind string) error {
	return c.write(clientMessage{
		Type:        "user",
		Kind:        kind,
		Id:          id,
		Username:    username,
		Permissions: &permissions,
		Status:      status,
	})
}

type clientMessage struct {
	Type             string                   `json:"type"`
	Kind             string                   `json:"kind,omitempty"`
	Id               string                   `json:"id,omitempty"`
	Replace          string                   `json:"replace,omitempty"`
	Source           string                   `json:"source,omitempty"`
	Dest             string                   `json:"dest,omitempty"`
	Username         string                   `json:"username,omitempty"`
	Password         string                   `json:"password,omitempty"`
	Privileged       bool                     `json:"privileged,omitempty"`
	Permissions      *group.ClientPermissions `json:"permissions,omitempty"`
	Status           map[string]interface{}   `json:"status,omitempty"`
	Group            string                   `json:"group,omitempty"`
	Value            interface{}              `json:"value,omitempty"`
	NoEcho           bool                     `json:"noecho,omitempty"`
	Time             int64                    `json:"time,omitempty"`
	SDP              string                   `json:"sdp,omitempty"`
	Candidate        *webrtc.ICECandidateInit `json:"candidate,omitempty"`
	Label            string                   `json:"label,omitempty"`
	Request          map[string][]string      `json:"request,omitempty"`
	RTCConfiguration *webrtc.Configuration    `json:"rtcConfiguration,omitempty"`
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

func addUpConn(c *webClient, id, label string, offer string) (*rtpUpConnection, bool, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.up == nil {
		c.up = make(map[string]*rtpUpConnection)
	}
	if c.down != nil && c.down[id] != nil {
		return nil, false, errors.New("Adding duplicate connection")
	}

	old := c.up[id]
	if old != nil {
		return old, false, nil
	}

	conn, err := newUpConn(c, id, label, offer)
	if err != nil {
		return nil, false, err
	}

	c.up[id] = conn

	conn.pc.OnICECandidate(func(candidate *webrtc.ICECandidate) {
		sendICE(c, id, candidate)
	})

	conn.pc.OnICEConnectionStateChange(func(state webrtc.ICEConnectionState) {
		if state == webrtc.ICEConnectionStateFailed {
			c.action(connectionFailedAction{id: id})
		}
	})

	return conn, true, nil
}

var ErrUserMismatch = errors.New("user id mismatch")

// delUpConn deletes an up connection.  If push is closed, the close is
// pushed to all corresponding down connections.
func delUpConn(c *webClient, id string, userId string, push bool) error {
	c.mu.Lock()
	if c.up == nil {
		c.mu.Unlock()
		return os.ErrNotExist
	}
	conn := c.up[id]
	if conn == nil {
		c.mu.Unlock()
		return os.ErrNotExist
	}
	if userId != "" && conn.userId != userId {
		c.mu.Unlock()
		return ErrUserMismatch
	}

	replace := conn.getReplace(true)

	delete(c.up, id)
	g := c.group
	c.mu.Unlock()

	conn.pc.Close()

	if push && g != nil {
		for _, c := range g.GetClients(c) {
			err := c.PushConn(g, id, nil, nil, replace)
			if err != nil {
				log.Printf("PushConn: %v", err)
			}
		}
	}

	return nil
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

func addDownConn(c *webClient, remote conn.Up) (*rtpDownConnection, bool, error) {
	id := remote.Id()

	c.mu.Lock()
	defer c.mu.Unlock()

	if c.up != nil && c.up[id] != nil {
		return nil, false, errors.New("adding duplicate connection")
	}

	if c.down == nil {
		c.down = make(map[string]*rtpDownConnection)
	}

	if down := c.down[id]; down != nil {
		return down, false, nil
	}

	down, err := newDownConn(c, id, remote)
	if err != nil {
		return nil, false, err
	}

	down.pc.OnICECandidate(func(candidate *webrtc.ICECandidate) {
		sendICE(c, down.id, candidate)
	})

	down.pc.OnICEConnectionStateChange(func(state webrtc.ICEConnectionState) {
		if state == webrtc.ICEConnectionStateFailed {
			c.action(connectionFailedAction{id: down.id})
		}
	})

	err = remote.AddLocal(down)
	if err != nil {
		down.pc.Close()
		return nil, false, err
	}

	c.down[down.id] = down

	go rtcpDownSender(down)

	return down, true, nil
}

func delDownConn(c *webClient, id string) error {
	conn := delDownConnHelper(c, id)
	if conn != nil {
		conn.pc.Close()
		return nil
	}
	return os.ErrNotExist
}

func delDownConnHelper(c *webClient, id string) *rtpDownConnection {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.down == nil {
		return nil
	}
	conn := c.down[id]
	if conn == nil {
		return nil
	}

	conn.remote.DelLocal(conn)
	for _, track := range conn.tracks {
		// we only insert the track after we get an answer, so
		// ignore errors here.
		track.remote.DelLocal(track)
	}
	delete(c.down, id)
	return conn
}

var errUnexpectedTrackType = errors.New("unexpected track type, this shouldn't happen")

func addDownTrackUnlocked(conn *rtpDownConnection, remoteTrack *rtpUpTrack, remoteConn conn.Up) error {
	for _, t := range conn.tracks {
		tt, ok := t.remote.(*rtpUpTrack)
		if !ok {
			return errUnexpectedTrackType
		}
		if tt == remoteTrack {
			return os.ErrExist
		}
	}

	local, err := webrtc.NewTrackLocalStaticRTP(
		remoteTrack.Codec(),
		remoteTrack.track.ID(), remoteTrack.track.StreamID(),
	)
	if err != nil {
		return err
	}

	sender, err := conn.pc.AddTrack(local)
	if err != nil {
		return err
	}

	parms := sender.GetParameters()
	if len(parms.Encodings) != 1 {
		return errors.New("got multiple encodings")
	}

	track := &rtpDownTrack{
		track:      local,
		sender:     sender,
		ssrc:       parms.Encodings[0].SSRC,
		remote:     remoteTrack,
		maxBitrate: new(bitrate),
		stats:      new(receiverStats),
		rate:       estimator.New(time.Second),
		atomics:    &downTrackAtomics{},
	}

	conn.tracks = append(conn.tracks, track)

	go rtcpDownListener(conn, track, sender)

	return nil
}

func delDownTrackUnlocked(conn *rtpDownConnection, track *rtpDownTrack) error {
	for i := range conn.tracks {
		if conn.tracks[i] == track {
			track.remote.DelLocal(track)
			conn.tracks =
				append(conn.tracks[:i], conn.tracks[i+1:]...)
			return conn.pc.RemoveTrack(track.sender)
		}
	}
	return os.ErrNotExist
}

func replaceTracks(conn *rtpDownConnection, remote []conn.UpTrack, remoteConn conn.Up) error {
	conn.mu.Lock()
	defer conn.mu.Unlock()

	var add []*rtpUpTrack
	var del []*rtpDownTrack

outer:
	for _, rtrack := range remote {
		rt, ok := rtrack.(*rtpUpTrack)
		if !ok {
			return errUnexpectedTrackType
		}
		for _, track := range conn.tracks {
			rt2, ok := track.remote.(*rtpUpTrack)
			if !ok {
				return errUnexpectedTrackType
			}
			if rt == rt2 {
				continue outer
			}
		}
		add = append(add, rt)
	}

outer2:
	for _, track := range conn.tracks {
		rt, ok := track.remote.(*rtpUpTrack)
		if !ok {
			return errUnexpectedTrackType
		}
		for _, rtrack := range remote {
			rt2, ok := rtrack.(*rtpUpTrack)
			if !ok {
				return errUnexpectedTrackType
			}
			if rt == rt2 {
				continue outer2
			}
		}
		del = append(del, track)
	}

	for _, t := range del {
		err := delDownTrackUnlocked(conn, t)
		if err != nil {
			return err
		}
	}

	for _, rt := range add {
		err := addDownTrackUnlocked(conn, rt, remoteConn)
		if err != nil {
			return err
		}
	}

	return nil
}

func negotiate(c *webClient, down *rtpDownConnection, restartIce bool, replace string) error {
	if down.pc.SignalingState() == webrtc.SignalingStateHaveLocalOffer {
		// avoid sending multiple offers back-to-back
		if restartIce {
			down.negotiationNeeded = negotiationRestartIce
		} else if down.negotiationNeeded == negotiationUnneeded {
			down.negotiationNeeded = negotiationNeeded
		}
		return nil
	}

	down.negotiationNeeded = negotiationUnneeded

	options := webrtc.OfferOptions{ICERestart: restartIce}
	offer, err := down.pc.CreateOffer(&options)
	if err != nil {
		return err
	}

	err = down.pc.SetLocalDescription(offer)
	if err != nil {
		return err
	}

	source, username := down.remote.User()

	return c.write(clientMessage{
		Type:     "offer",
		Id:       down.id,
		Label:    down.remote.Label(),
		Replace:  replace,
		Source:   source,
		Username: username,
		SDP:      down.pc.LocalDescription().SDP,
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

func gotOffer(c *webClient, id, label string, sdp string, replace string) error {
	up, _, err := addUpConn(c, id, label, sdp)
	if err != nil {
		return err
	}

	up.userId = c.Id()
	up.username = c.Username()
	if replace != "" {
		up.replace = replace
		delUpConn(c, replace, c.Id(), false)
	}

	err = up.pc.SetRemoteDescription(webrtc.SessionDescription{
		Type: webrtc.SDPTypeOffer,
		SDP:  sdp,
	})
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

	err = up.flushICECandidates()
	if err != nil {
		log.Printf("ICE: %v", err)
	}

	return c.write(clientMessage{
		Type: "answer",
		Id:   id,
		SDP:  up.pc.LocalDescription().SDP,
	})
}

var ErrUnknownId = errors.New("unknown id")

func gotAnswer(c *webClient, id string, sdp string) error {
	down := getDownConn(c, id)
	if down == nil {
		return ErrUnknownId
	}

	err := down.pc.SetRemoteDescription(webrtc.SessionDescription{
		Type: webrtc.SDPTypeAnswer,
		SDP:  sdp,
	})
	if err != nil {
		return err
	}

	for _, t := range down.tracks {
		local := t.track.Codec()
		remote := t.remote.Codec()
		if local.MimeType != remote.MimeType ||
			local.ClockRate != remote.ClockRate {
			return errors.New("negotiation failed")
		}
	}

	err = down.flushICECandidates()
	if err != nil {
		log.Printf("ICE: %v", err)
	}

	add := func() {
		down.pc.OnConnectionStateChange(nil)
		for _, t := range down.tracks {
			t.remote.AddLocal(t)
		}
	}
	down.pc.OnConnectionStateChange(func(state webrtc.PeerConnectionState) {
		if state == webrtc.PeerConnectionStateConnected {
			add()
		}
	})
	if down.pc.ConnectionState() == webrtc.PeerConnectionStateConnected {
		add()
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

func (c *webClient) setRequested(requested map[string][]string) error {
	if c.group == nil {
		return errors.New("attempted to request with no group joined")
	}
	c.requested = requested

	pushConns(c, c.group)
	return nil
}

func pushConns(c group.Client, g *group.Group) {
	clients := g.GetClients(c)
	for _, cc := range clients {
		ccc, ok := cc.(*webClient)
		if ok {
			ccc.action(pushConnsAction{g, c})
		}
	}
}

func requestedTracks(c *webClient, up conn.Up, tracks []conn.UpTrack) []conn.UpTrack {
	r, ok := c.requested[up.Label()]
	if !ok {
		r, ok = c.requested[""]
	}
	if !ok || len(r) == 0 {
		return nil
	}

	var audio, video bool
	for _, s := range r {
		switch s {
		case "audio":
			audio = true
		case "video":
			video = true
		default:
			log.Printf("client requested unknown value %v", s)
		}
	}

	var ts []conn.UpTrack
	if audio {
		for _, t := range tracks {
			if t.Kind() == webrtc.RTPCodecTypeAudio {
				ts = append(ts, t)
				break
			}
		}
	}
	if video {
		for _, t := range tracks {
			if t.Kind() == webrtc.RTPCodecTypeVideo {
				ts = append(ts, t)
				break
			}
		}
	}

	return ts
}

func (c *webClient) PushConn(g *group.Group, id string, up conn.Up, tracks []conn.UpTrack, replace string) error {
	err := c.action(pushConnAction{g, id, up, tracks, replace})
	if err != nil {
		return err
	}
	return nil
}

func readMessage(conn *websocket.Conn, m *clientMessage) error {
	err := conn.SetReadDeadline(time.Now().Add(15 * time.Second))
	if err != nil {
		return err
	}
	defer conn.SetReadDeadline(time.Time{})

	return conn.ReadJSON(&m)
}

func StartClient(conn *websocket.Conn) (err error) {
	var m clientMessage

	err = readMessage(conn, &m)
	if err != nil {
		conn.Close()
		return
	}

	if m.Type != "handshake" {
		conn.WriteMessage(websocket.CloseMessage,
			websocket.FormatCloseMessage(
				websocket.CloseProtocolError,
				"you must handshake first",
			),
		)
		conn.Close()
		err = group.ProtocolError("client didn't handshake")
		return
	}

	c := &webClient{
		id:       m.Id,
		actionCh: make(chan struct{}, 1),
		done:     make(chan struct{}),
	}

	defer close(c.done)

	c.writeCh = make(chan interface{}, 100)
	c.writerDone = make(chan struct{})
	go clientWriter(conn, c.writeCh, c.writerDone)
	defer func() {
		m, e := errorToWSCloseMessage(c.id, err)
		if isWSNormalError(err) {
			err = nil
		} else if _, ok := err.(group.KickError); ok {
			err = nil
		}
		if m != nil {
			c.write(*m)
		}
		c.close(e)
	}()

	return clientLoop(c, conn)
}

type pushConnAction struct {
	group   *group.Group
	id      string
	conn    conn.Up
	tracks  []conn.UpTrack
	replace string
}

type pushConnsAction struct {
	group  *group.Group
	client group.Client
}

type connectionFailedAction struct {
	id string
}

type permissionsChangedAction struct{}

type kickAction struct {
	id       string
	username string
	message  string
}

var errEmptyId = group.ProtocolError("empty id")

func clientLoop(c *webClient, ws *websocket.Conn) error {
	read := make(chan interface{}, 1)
	go clientReader(ws, read, c.done)

	defer leaveGroup(c)

	readTime := time.Now()

	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()

	err := c.write(clientMessage{
		Type: "handshake",
	})
	if err != nil {
		return err
	}

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
		case <-c.actionCh:
			c.mu.Lock()
			actions := c.actions
			c.actions = nil
			c.mu.Unlock()
			for _, a := range actions {
				err := handleAction(c, a)
				if err != nil {
					return err
				}
			}
		case <-ticker.C:
			if time.Since(readTime) > 75*time.Second {
				return errors.New("client is dead")
			}
			// Some reverse proxies timeout connexions at 60
			// seconds, make sure we generate some activity
			// after 55s at most.
			if time.Since(readTime) > 45*time.Second {
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

func handleAction(c *webClient, a interface{}) error {
	switch a := a.(type) {
	case pushConnAction:
		g := c.group
		if g == nil || a.group != g {
			return nil
		}
		var tracks []conn.UpTrack
		if a.conn != nil {
			tracks = requestedTracks(c, a.conn, a.tracks)
		}

		if len(tracks) == 0 {
			closeDownConn(c, a.id, "")
			if a.replace != "" {
				closeDownConn(
					c, a.replace, "",
				)
			}
			return nil
		}

		down, _, err := addDownConn(c, a.conn)
		if err != nil {
			return err
		}
		err = replaceTracks(down, tracks, a.conn)
		if err != nil {
			return err
		}
		if a.replace != "" {
			err := delDownConn(c, a.replace)
			if err != nil {
				log.Printf("Replace: %v", err)
			}
		}
		err = negotiate(
			c, down, false, a.replace,
		)
		if err != nil {
			log.Printf(
				"Negotiation failed: %v",
				err)
			closeDownConn(c, down.id,
				"negotiation failed")
		}
	case pushConnsAction:
		g := c.group
		if g == nil || a.group != g {
			return nil
		}
		for _, u := range c.up {
			tracks := u.getTracks()
			replace := u.getReplace(false)

			ts := make([]conn.UpTrack, len(tracks))
			for i, t := range tracks {
				ts[i] = t
			}
			err := a.client.PushConn(g, u.id, u, ts, replace)
			if err != nil {
				log.Printf("PushConn: %v", err)
			}
		}
	case connectionFailedAction:
		if down := getDownConn(c, a.id); down != nil {
			err := negotiate(c, down, true, "")
			if err != nil {
				return err
			}
			tracks := make(
				[]conn.UpTrack, len(down.tracks),
			)
			for i, t := range down.tracks {
				tracks[i] = t.remote
			}
			c.PushConn(
				c.group,
				down.remote.Id(), down.remote,
				tracks, "",
			)
		} else if up := getUpConn(c, a.id); up != nil {
			c.write(clientMessage{
				Type: "renegotiate",
				Id:   a.id,
			})
		} else {
			log.Printf("Attempting to renegotiate " +
				"unknown connection")
		}

	case permissionsChangedAction:
		g := c.Group()
		if g == nil {
			return errors.New("Permissions changed in no group")
		}
		perms := c.permissions
		c.write(clientMessage{
			Type:             "joined",
			Kind:             "change",
			Group:            g.Name(),
			Username:         c.username,
			Permissions:      &perms,
			RTCConfiguration: ice.ICEConfiguration(),
		})
		if !c.permissions.Present {
			up := getUpConns(c)
			for _, u := range up {
				err := delUpConn(
					c, u.id, c.id, true,
				)
				if err == nil {
					failUpConnection(
						c, u.id,
						"permission denied",
					)
				}
			}
		}
		id := c.Id()
		user := c.Username()
		s := c.Status()
		clients := g.GetClients(nil)
		go func(clients []group.Client) {
			for _, cc := range clients {
				cc.PushClient(id, user, perms, s, "change")
			}
		}(clients)
	case kickAction:
		return group.KickError{
			a.id, a.username, a.message,
		}
	default:
		log.Printf("unexpected action %T", a)
		return errors.New("unexpected action")
	}
	return nil
}

func failUpConnection(c *webClient, id string, message string) error {
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
		err := c.error(group.UserError(message))
		if err != nil {
			return err
		}
	}
	return nil
}

func leaveGroup(c *webClient) {
	if c.group == nil {
		return
	}

	if c.up != nil {
		for id := range c.up {
			delUpConn(c, id, c.id, true)
		}
	}
	if c.down != nil {
		for id := range c.down {
			delDownConn(c, id)
		}
	}

	group.DelClient(c)
	c.permissions = group.ClientPermissions{}
	c.status = nil
	c.requested = make(map[string][]string)
	c.group = nil
}

func closeDownConn(c *webClient, id string, message string) error {
	err := delDownConn(c, id)
	if err != nil && !os.IsNotExist(err) {
		log.Printf("Close down connection: %v", err)
	}
	err = c.write(clientMessage{
		Type: "close",
		Id:   id,
	})
	if err != nil {
		return err
	}
	if message != "" {
		err := c.error(group.UserError(message))
		if err != nil {
			return err
		}
	}
	return nil
}

func setPermissions(g *group.Group, id string, perm string) error {
	client := g.GetClient(id)
	if client == nil {
		return group.UserError("no such user")
	}

	c, ok := client.(*webClient)
	if !ok {
		return group.UserError("this is not a real user")
	}

	switch perm {
	case "op":
		c.permissions.Op = true
		if g.AllowRecording() {
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
		return group.UserError("unknown permission")
	}
	return c.action(permissionsChangedAction{})
}

func (c *webClient) Kick(id, user, message string) error {
	return c.action(kickAction{id, user, message})
}

func kickClient(g *group.Group, id, user, dest string, message string) error {
	client := g.GetClient(dest)
	if client == nil {
		return group.UserError("no such user")
	}

	return client.Kick(id, user, message)
}

func handleClientMessage(c *webClient, m clientMessage) error {
	if m.Source != "" {
		if m.Source != c.Id() {
			return group.ProtocolError("spoofed client id")
		}
	}

	if m.Type != "join" {
		if m.Username != "" {
			if m.Username != c.Username() {
				return group.ProtocolError("spoofed username")
			}
		}
	}

	switch m.Type {
	case "join":
		if m.Kind == "leave" {
			if c.group == nil || c.group.Name() != m.Group {
				return group.ProtocolError("you are not joined")
			}
			leaveGroup(c)
			perms := c.permissions
			return c.write(clientMessage{
				Type:        "joined",
				Kind:        "leave",
				Group:       m.Group,
				Username:    c.username,
				Permissions: &perms,
			})
		}

		if m.Kind != "join" {
			return group.ProtocolError("unknown kind")
		}

		if c.group != nil {
			return group.ProtocolError("cannot join multiple groups")
		}
		c.username = m.Username
		c.password = m.Password
		g, err := group.AddClient(m.Group, c)
		if err != nil {
			var s string
			if os.IsNotExist(err) {
				s = "group does not exist"
			} else if err == group.ErrNotAuthorised {
				s = "not authorised"
				time.Sleep(200 * time.Millisecond)
			} else if e, ok := err.(group.UserError); ok {
				s = string(e)
			} else {
				s = "internal server error"
				log.Printf("Join group: %v", err)
			}
			return c.write(clientMessage{
				Type:        "joined",
				Kind:        "fail",
				Group:       m.Group,
				Username:    c.username,
				Permissions: &group.ClientPermissions{},
				Value:       s,
			})
		}
		if redirect := g.Redirect(); redirect != "" {
			// We normally redirect at the HTTP level, but the group
			// description could have been edited in the meantime.
			return c.write(clientMessage{
				Type:        "joined",
				Kind:        "redirect",
				Group:       m.Group,
				Username:    c.username,
				Permissions: &group.ClientPermissions{},
				Value:       redirect,
			})
		}
		c.group = g
		perms := c.permissions
		err = c.write(clientMessage{
			Type:             "joined",
			Kind:             "join",
			Group:            m.Group,
			Username:         c.username,
			Permissions:      &perms,
			RTCConfiguration: ice.ICEConfiguration(),
		})
		if err != nil {
			return err
		}
		h := c.group.GetChatHistory()
		for _, m := range h {
			err := c.write(clientMessage{
				Type:     "chat",
				Id:       m.Id,
				Username: m.User,
				Time:     m.Time,
				Value:    m.Value,
				Kind:     m.Kind,
			})
			if err != nil {
				return err
			}
		}
	case "request":
		return c.setRequested(m.Request)
	case "offer":
		if m.Id == "" {
			return errEmptyId
		}
		if !c.permissions.Present {
			if m.Replace != "" {
				delUpConn(c, m.Replace, c.id, true)
			}
			c.write(clientMessage{
				Type: "abort",
				Id:   m.Id,
			})
			return c.error(group.UserError("not authorised"))
		}
		err := gotOffer(c, m.Id, m.Label, m.SDP, m.Replace)
		if err != nil {
			log.Printf("gotOffer: %v", err)
			return failUpConnection(c, m.Id, "negotiation failed")
		}
	case "answer":
		if m.Id == "" {
			return errEmptyId
		}
		err := gotAnswer(c, m.Id, m.SDP)
		if err != nil {
			log.Printf("gotAnswer: %v", err)
			message := ""
			if err != ErrUnknownId {
				message = "negotiation failed"
			}
			return closeDownConn(c, m.Id, message)
		}
		down := getDownConn(c, m.Id)
		if down.negotiationNeeded > negotiationUnneeded {
			err := negotiate(
				c, down,
				down.negotiationNeeded == negotiationRestartIce,
				"",
			)
			if err != nil {
				return closeDownConn(
					c, m.Id, "negotiation failed",
				)
			}
		}
	case "renegotiate":
		if m.Id == "" {
			return errEmptyId
		}
		down := getDownConn(c, m.Id)
		if down != nil {
			err := negotiate(c, down, true, "")
			if err != nil {
				return closeDownConn(
					c, m.Id, "renegotiation failed",
				)
			}
		} else {
			log.Printf("Trying to renegotiate unknown connection")
		}
	case "close":
		if m.Id == "" {
			return errEmptyId
		}
		err := delUpConn(c, m.Id, c.id, true)
		if err != nil {
			log.Printf("Deleting up connection %v: %v",
				m.Id, err)
			return nil
		}
	case "abort":
		if m.Id == "" {
			return errEmptyId
		}
		return closeDownConn(c, m.Id, "")
	case "ice":
		if m.Id == "" {
			return errEmptyId
		}
		if m.Candidate == nil {
			return group.ProtocolError("null candidate")
		}
		err := gotICE(c, m.Candidate, m.Id)
		if err != nil {
			log.Printf("ICE: %v", err)
		}
	case "chat", "usermessage":
		g := c.group
		if g == nil {
			return c.error(group.UserError("join a group first"))
		}

		tm := group.ToJSTime(time.Now())

		if m.Type == "chat" {
			if m.Dest == "" {
				g.AddToChatHistory(
					m.Source, m.Username, tm, m.Kind, m.Value,
				)
			}
		}
		mm := clientMessage{
			Type:       m.Type,
			Source:     m.Source,
			Dest:       m.Dest,
			Username:   m.Username,
			Privileged: c.permissions.Op,
			Time:       tm,
			Kind:       m.Kind,
			NoEcho:     m.NoEcho,
			Value:      m.Value,
		}
		if m.Dest == "" {
			var except group.Client
			if m.NoEcho {
				except = c
			}
			err := broadcast(g.GetClients(except), mm)
			if err != nil {
				log.Printf("broadcast(chat): %v", err)
			}
		} else {
			cc := g.GetClient(m.Dest)
			if cc == nil {
				return c.error(group.UserError("user unknown"))
			}
			ccc, ok := cc.(*webClient)
			if !ok {
				return c.error(group.UserError(
					"this user doesn't chat",
				))
			}
			ccc.write(mm)
		}
	case "groupaction":
		g := c.group
		if g == nil {
			return c.error(group.UserError("join a group first"))
		}
		switch m.Kind {
		case "clearchat":
			g.ClearChatHistory()
			m := clientMessage{
				Type:       "usermessage",
				Kind:       "clearchat",
				Privileged: true,
			}
			err := broadcast(g.GetClients(nil), m)
			if err != nil {
				log.Printf("broadcast(clearchat): %v", err)
			}
		case "lock", "unlock":
			if !c.permissions.Op {
				return c.error(group.UserError("not authorised"))
			}
			message := ""
			v, ok := m.Value.(string)
			if ok {
				message = v
			}
			g.SetLocked(m.Kind == "lock", message)
		case "record":
			if !c.permissions.Record {
				return c.error(group.UserError("not authorised"))
			}
			for _, cc := range g.GetClients(c) {
				_, ok := cc.(*diskwriter.Client)
				if ok {
					return c.error(group.UserError("already recording"))
				}
			}
			disk := diskwriter.New(g)
			_, err := group.AddClient(g.Name(), disk)
			if err != nil {
				disk.Close()
				return c.error(err)
			}
			pushConns(disk, c.group)
		case "unrecord":
			if !c.permissions.Record {
				return c.error(group.UserError("not authorised"))
			}
			for _, cc := range g.GetClients(c) {
				disk, ok := cc.(*diskwriter.Client)
				if ok {
					disk.Close()
					group.DelClient(disk)
				}
			}
		case "subgroups":
			if !c.permissions.Op {
				return c.error(group.UserError("not authorised"))
			}
			s := ""
			for _, sg := range group.GetSubGroups(g.Name()) {
				plural := ""
				if sg.Clients > 1 {
					plural = "s"
				}
				s = s + fmt.Sprintf("%v (%v client%v)",
					sg.Name, sg.Clients, plural)
			}
			c.write(clientMessage{
				Type:     "chat",
				Dest:     c.id,
				Username: "Server",
				Time:     group.ToJSTime(time.Now()),
				Value:    s,
			})
		default:
			return group.ProtocolError("unknown group action")
		}
	case "useraction":
		g := c.group
		if g == nil {
			return c.error(group.UserError("join a group first"))
		}
		switch m.Kind {
		case "op", "unop", "present", "unpresent":
			if !c.permissions.Op {
				return c.error(group.UserError("not authorised"))
			}
			err := setPermissions(g, m.Dest, m.Kind)
			if err != nil {
				return c.error(err)
			}
		case "kick":
			if !c.permissions.Op {
				return c.error(group.UserError("not authorised"))
			}
			message := ""
			v, ok := m.Value.(string)
			if ok {
				message = v
			}
			err := kickClient(g, m.Source, m.Username, m.Dest, message)
			if err != nil {
				return c.error(err)
			}
		case "setstatus":
			if m.Dest != c.Id() {
				return c.error(group.UserError("not authorised"))
			}
			s, ok := m.Value.(map[string]interface{})
			if !ok {
				return c.error(group.UserError(
					"Bad value in setstatus",
				))
			}
			if c.status == nil {
				c.status = make(map[string]interface{})
			}
			for k, v := range s {
				if v == nil {
					delete(c.status, k)
				} else {
					c.status[k] = v
				}
			}
			id := c.Id()
			user := c.Username()
			perms := c.Permissions()
			status := c.Status()
			go func(clients []group.Client) {
				for _, cc := range clients {
					cc.PushClient(id, user, perms, status,
						"change")
				}
			}(g.GetClients(nil))
		default:
			return group.ProtocolError("unknown user action")
		}
	case "pong":
		// nothing
	case "ping":
		return c.write(clientMessage{
			Type: "pong",
		})
	default:
		log.Printf("unexpected message: %v", m.Type)
		return group.ProtocolError("unexpected message")
	}
	return nil
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
			time.Now().Add(500 * time.Millisecond),
		)
		if err != nil {
			return
		}
		switch m := m.(type) {
		case clientMessage:
			err := conn.WriteJSON(m)
			if err != nil {
				return
			}
		case []byte:
			err := conn.WriteMessage(websocket.TextMessage, m)
			if err != nil {
				return
			}
		case closeMessage:
			if m.data != nil {
				conn.WriteMessage(
					websocket.CloseMessage,
					m.data,
				)
			}
			return
		default:
			log.Printf("clientWiter: unexpected message %T", m)
			return
		}
	}
}

func (c *webClient) Warn(oponly bool, message string) error {
	if oponly && !c.permissions.Op {
		return nil
	}

	return c.write(clientMessage{
		Type:       "usermessage",
		Kind:       "warning",
		Dest:       c.id,
		Privileged: true,
		Value:      message,
	})
}

var ErrClientDead = errors.New("client is dead")

func (c *webClient) action(a interface{}) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	empty := len(c.actions) == 0
	c.actions = append(c.actions, a)
	if empty {
		select {
		case c.actionCh <- struct{}{}:
			return nil
		case <-c.done:
			return ErrClientDead
		default:
		}
	}
	return nil
}

func (c *webClient) write(m clientMessage) error {
	select {
	case c.writeCh <- m:
		return nil
	case <-c.writerDone:
		return ErrClientDead
	}
}

func broadcast(cs []group.Client, m clientMessage) error {
	b, err := json.Marshal(m)
	if err != nil {
		return err
	}
	for _, c := range cs {
		cc, ok := c.(*webClient)
		if !ok {
			continue
		}
		select {
		case cc.writeCh <- b:
		case <-cc.writerDone:
		}
	}
	return nil
}

func (c *webClient) close(data []byte) error {
	select {
	case c.writeCh <- closeMessage{data}:
		return nil
	case <-c.writerDone:
		return ErrClientDead
	}
}

func errorMessage(id string, err error) *clientMessage {
	switch e := err.(type) {
	case group.UserError:
		return &clientMessage{
			Type:       "usermessage",
			Kind:       "error",
			Dest:       id,
			Privileged: true,
			Value:      e.Error(),
		}
	case group.KickError:
		message := e.Message
		if message == "" {
			message = "you have been kicked out"
		}
		return &clientMessage{
			Type:       "usermessage",
			Kind:       "error",
			Id:         e.Id,
			Username:   e.Username,
			Dest:       id,
			Privileged: true,
			Value:      message,
		}
	default:
		return nil
	}
}

func (c *webClient) error(err error) error {
	m := errorMessage(c.id, err)
	if m == nil {
		return err
	}
	return c.write(*m)
}
