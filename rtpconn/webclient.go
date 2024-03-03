package rtpconn

import (
	crand "crypto/rand"
	"encoding/base64"
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
	"github.com/jech/galene/token"
	"github.com/jech/galene/unbounded"
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
	permissions []string
	data        map[string]interface{}
	requested   map[string][]string
	done        chan struct{}
	writeCh     chan interface{}
	writerDone  chan struct{}
	actions     *unbounded.Channel[any]

	mu   sync.Mutex
	down map[string]*rtpDownConnection
	up   map[string]*rtpUpConnection
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

func (c *webClient) SetUsername(username string) {
	c.username = username
}

func (c *webClient) Permissions() []string {
	return c.permissions
}

func (c *webClient) Data() map[string]interface{} {
	return c.data
}

func (c *webClient) SetPermissions(perms []string) {
	c.permissions = perms
}

func (c *webClient) PushClient(group, kind, id string, username string, perms []string, data map[string]interface{}) error {
	c.action(pushClientAction{
		group, kind, id, username, perms, data,
	})
	return nil
}

type clientMessage struct {
	Type             string                   `json:"type"`
	Version          []string                 `json:"version,omitempty"`
	Kind             string                   `json:"kind,omitempty"`
	Error            string                   `json:"error,omitempty"`
	Id               string                   `json:"id,omitempty"`
	Replace          string                   `json:"replace,omitempty"`
	Source           string                   `json:"source,omitempty"`
	Dest             string                   `json:"dest,omitempty"`
	Username         *string                  `json:"username,omitempty"`
	Password         string                   `json:"password,omitempty"`
	Token            string                   `json:"token,omitempty"`
	Privileged       bool                     `json:"privileged,omitempty"`
	Permissions      []string                 `json:"permissions,omitempty"`
	Status           *group.Status            `json:"status,omitempty"`
	Data             map[string]interface{}   `json:"data,omitempty"`
	Group            string                   `json:"group,omitempty"`
	Value            interface{}              `json:"value,omitempty"`
	NoEcho           bool                     `json:"noecho,omitempty"`
	Time             string                   `json:"time,omitempty"`
	SDP              string                   `json:"sdp,omitempty"`
	Candidate        *webrtc.ICECandidateInit `json:"candidate,omitempty"`
	Label            string                   `json:"label,omitempty"`
	Request          interface{}              `json:"request,omitempty"`
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
		return nil, false, errors.New("adding duplicate connection")
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
	if userId != "" {
		id, _ := conn.User()
		if id != userId {
			c.mu.Unlock()
			return ErrUserMismatch
		}
	}

	replace := conn.getReplace(false)

	delete(c.up, id)
	g := c.group
	c.mu.Unlock()

	conn.mu.Lock()
	conn.closed = true
	conn.mu.Unlock()

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

func addDownTrackUnlocked(conn *rtpDownConnection, remoteTrack *rtpUpTrack) error {
	for _, t := range conn.tracks {
		tt, ok := t.remote.(*rtpUpTrack)
		if !ok {
			return errUnexpectedTrackType
		}
		if tt == remoteTrack {
			return os.ErrExist
		}
	}

	id := remoteTrack.track.ID()
	if id == "" {
		log.Println("Got track with empty id")
		id = remoteTrack.track.RID()
	}
	if id == "" {
		id = remoteTrack.track.Kind().String()
	}
	msid := remoteTrack.track.StreamID()
	if msid == "" || msid == "-" {
		log.Println("Got track with empty msid")
		msid = remoteTrack.conn.Label()
	}
	if msid == "" {
		msid = "dummy"
	}

	local, err := webrtc.NewTrackLocalStaticRTP(
		remoteTrack.Codec(), id, msid,
	)
	if err != nil {
		return err
	}

	transceiver, err := conn.pc.AddTransceiverFromTrack(local,
		webrtc.RTPTransceiverInit{
			Direction: webrtc.RTPTransceiverDirectionSendonly,
		},
	)
	if err != nil {
		return err
	}

	codec := local.Codec()
	ptype, err := group.CodecPayloadType(local.Codec())
	if err != nil {
		log.Printf("Couldn't determine ptype for codec %v: %v",
			codec.MimeType, err)
	} else {
		err := transceiver.SetCodecPreferences(
			[]webrtc.RTPCodecParameters{
				{
					RTPCodecCapability: codec,
					PayloadType:        ptype,
				},
			},
		)
		if err != nil {
			log.Printf("Couldn't set ptype for codec %v: %v",
				codec.MimeType, err)
		}
	}

	parms := transceiver.Sender().GetParameters()
	if len(parms.Encodings) != 1 {
		return errors.New("got multiple encodings")
	}

	track := &rtpDownTrack{
		track:          local,
		sender:         transceiver.Sender(),
		ssrc:           parms.Encodings[0].SSRC,
		conn:           conn,
		remote:         remoteTrack,
		maxBitrate:     new(bitrate),
		maxREMBBitrate: new(bitrate),
		stats:          new(receiverStats),
		rate:           estimator.New(time.Second),
		atomics:        &downTrackAtomics{},
	}

	conn.tracks = append(conn.tracks, track)

	go rtcpDownListener(track)

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

func replaceTracks(conn *rtpDownConnection, remote []conn.UpTrack, limitSid bool) (bool, error) {
	conn.mu.Lock()
	defer conn.mu.Unlock()

	var add []*rtpUpTrack
	var del []*rtpDownTrack

outer:
	for _, rtrack := range remote {
		rt, ok := rtrack.(*rtpUpTrack)
		if !ok {
			return false, errUnexpectedTrackType
		}
		for _, track := range conn.tracks {
			rt2, ok := track.remote.(*rtpUpTrack)
			if !ok {
				return false, errUnexpectedTrackType
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
			return false, errUnexpectedTrackType
		}
		for _, rtrack := range remote {
			rt2, ok := rtrack.(*rtpUpTrack)
			if !ok {
				return false, errUnexpectedTrackType
			}
			if rt == rt2 {
				continue outer2
			}
		}
		del = append(del, track)
	}

	defer func() {
		for _, t := range conn.tracks {
			layer := t.getLayerInfo()
			layer.limitSid = limitSid
			if limitSid {
				layer.wantedSid = 0
			}
			t.setLayerInfo(layer)
		}
	}()

	if len(del) == 0 && len(add) == 0 {
		return false, nil
	}

	for _, t := range del {
		err := delDownTrackUnlocked(conn, t)
		if err != nil {
			return false, err
		}
	}

	for _, rt := range add {
		err := addDownTrackUnlocked(conn, rt)
		if err != nil {
			return false, err
		}
	}

	return true, nil
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
		Username: &username,
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

	err = down.flushICECandidates()
	if err != nil {
		log.Printf("ICE: %v", err)
	}

	add := func() {
		down.pc.OnConnectionStateChange(nil)
		for _, t := range down.tracks {
			err := t.remote.AddLocal(t)
			if err != nil && err != os.ErrClosed {
				log.Printf("Add track: %v", err)
			}
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

var errBadType = errors.New("bad type")

func toStringArray(r interface{}) ([]string, error) {
	if r == nil {
		return nil, nil
	}
	rr, ok := r.([]interface{})
	if !ok {
		return nil, errBadType
	}
	if rr == nil {
		return nil, nil
	}

	rrr := make([]string, len(rr))
	for i, s := range rr {
		rrr[i], ok = s.(string)
		if !ok {
			return nil, errBadType
		}
	}
	return rrr, nil
}

func parseRequested(r interface{}) (map[string][]string, error) {
	if r == nil {
		return nil, nil
	}
	rr, ok := r.(map[string]interface{})
	if !ok {
		return nil, errBadType
	}
	if rr == nil {
		return nil, nil
	}
	rrr := make(map[string][]string)
	for k, v := range rr {
		vv, err := toStringArray(v)
		if err != nil {
			return nil, err
		}
		rrr[k] = vv
	}
	return rrr, nil
}

func (c *webClient) setRequested(requested map[string][]string) error {
	if c.group == nil {
		return errors.New("attempted to request with no group joined")
	}
	c.requested = requested

	requestConns(c, c.group, "")
	return nil
}

func (c *webClient) setRequestedStream(down *rtpDownConnection, requested []string) error {
	var remoteClient group.Client
	remote, ok := down.remote.(*rtpUpConnection)
	if ok {
		remoteClient = remote.client
	}
	down.requested = requested
	return remoteClient.RequestConns(c, c.group, remote.id)
}

func (c *webClient) RequestConns(target group.Client, g *group.Group, id string) error {
	c.action(requestConnsAction{g, target, id})
	return nil
}

func requestConns(target group.Client, g *group.Group, id string) {
	clients := g.GetClients(target)
	for _, c := range clients {
		c.RequestConns(target, g, id)
	}
}

func requestedTracks(c *webClient, requested []string, tracks []conn.UpTrack) ([]conn.UpTrack, bool) {
	if len(requested) == 0 {
		return nil, false
	}
	var audio, video, videoLow bool
	for _, s := range requested {
		switch s {
		case "audio":
			audio = true
		case "video":
			video = true
		case "video-low":
			videoLow = true
		default:
			log.Printf("client requested unknown value %v", s)
		}
	}

	find := func(kind webrtc.RTPCodecType, last bool) (conn.UpTrack, int) {
		var track conn.UpTrack
		count := 0
		for _, t := range tracks {
			if t.Kind() != kind {
				continue
			}
			track = t
			count++
			if !last {
				break
			}
		}
		return track, count
	}

	var ts []conn.UpTrack
	limitSid := false
	if audio {
		t, _ := find(webrtc.RTPCodecTypeAudio, false)
		if t != nil {
			ts = append(ts, t)
		}
	}
	if video {
		t, _ := find(webrtc.RTPCodecTypeVideo, false)
		if t != nil {
			ts = append(ts, t)
		}
	} else if videoLow {
		t, count := find(webrtc.RTPCodecTypeVideo, true)
		if t != nil {
			ts = append(ts, t)
		}
		if count < 2 {
			limitSid = true
		}
	}

	return ts, limitSid
}

func (c *webClient) PushConn(g *group.Group, id string, up conn.Up, tracks []conn.UpTrack, replace string) error {
	c.action(pushConnAction{g, id, up, tracks, replace})
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

const protocolVersion = "2"

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

	versionError := true
	if m.Version != nil {
		for _, v := range m.Version {
			if v == protocolVersion {
				versionError = false
			}
		}
	}

	c := &webClient{
		id:      m.Id,
		actions: unbounded.New[any](),
		done:    make(chan struct{}),
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

	return clientLoop(c, conn, versionError)
}

type pushConnAction struct {
	group   *group.Group
	id      string
	conn    conn.Up
	tracks  []conn.UpTrack
	replace string
}

type requestConnsAction struct {
	group  *group.Group
	target group.Client
	id     string
}

type connectionFailedAction struct {
	id string
}

type pushClientAction struct {
	group       string
	kind        string
	id          string
	username    string
	permissions []string
	data        map[string]interface{}
}

type permissionsChangedAction struct{}

type joinedAction struct {
	group string
	kind  string
}

type kickAction struct {
	id       string
	username *string
	message  string
}

var errEmptyId = group.ProtocolError("empty id")

func member(v string, l []string) bool {
	for _, w := range l {
		if v == w {
			return true
		}
	}
	return false
}

func remove(v string, l []string) []string {
	for i, w := range l {
		if v == w {
			l = append(l[:i], l[i+1:]...)
			return l
		}
	}
	return l
}

func addnew(v string, l []string) []string {
	if member(v, l) {
		return l
	}
	l = append(l, v)
	return l
}

func clientLoop(c *webClient, ws *websocket.Conn, versionError bool) error {
	read := make(chan interface{}, 1)
	go clientReader(ws, read, c.done)

	defer leaveGroup(c)

	readTime := time.Now()

	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()

	err := c.write(clientMessage{
		Type:    "handshake",
		Version: []string{protocolVersion},
	})
	if err != nil {
		return err
	}

	if versionError {
		c.write(clientMessage{
			Type:       "usermessage",
			Kind:       "warning",
			Dest:       c.id,
			Privileged: true,
			Value: "This client is using an unknown protocol version.\n" +
				"Perhaps it needs upgrading?\n" +
				"Trying to continue, things may break.",
		})
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
		case <-c.actions.Ch:
			actions := c.actions.Get()
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

func pushDownConn(c *webClient, id string, up conn.Up, tracks []conn.UpTrack, replace string) error {
	var requested []conn.UpTrack
	limitSid := false
	if up != nil {
		var old *rtpDownConnection
		if replace != "" {
			old = getDownConn(c, replace)
		} else {
			old = getDownConn(c, up.Id())
		}
		var req []string
		if old != nil {
			req = old.requested
		}
		if req == nil {
			var ok bool
			req, ok = c.requested[up.Label()]
			if !ok {
				req = c.requested[""]
			}
		}
		requested, limitSid = requestedTracks(c, req, tracks)
	}

	if replace != "" {
		err := delDownConn(c, replace)
		if err != nil {
			log.Printf("Replace: %v", err)
		}
	}

	// closes over replace, which will be modified below
	defer func() {
		if replace != "" {
			closeDownConn(c, replace, "")
		}
	}()

	if len(requested) == 0 {
		closeDownConn(c, id, "")
		return nil
	}

	down, _, err := addDownConn(c, up)
	if err != nil {
		if errors.Is(err, os.ErrClosed) {
			return nil
		}
		return err
	}
	done, err := replaceTracks(down, requested, limitSid)
	if err != nil || !done {
		return err
	}
	err = negotiate(c, down, false, replace)
	if err != nil {
		log.Printf("Negotiation failed: %v", err)
		closeDownConn(c, down.id, err.Error())
		return err
	}
	replace = ""
	return nil
}

func handleAction(c *webClient, a any) error {
	switch a := a.(type) {
	case pushConnAction:
		if c.group == nil || c.group != a.group {
			log.Printf("Got connectsions for wrong group")
			return nil
		}
		return pushDownConn(c, a.id, a.conn, a.tracks, a.replace)
	case requestConnsAction:
		g := c.group
		if g == nil || a.group != g {
			log.Printf("Misdirected pushConns")
			return nil
		}
		for _, u := range c.up {
			if a.id != "" && a.id != u.id {
				continue
			}
			tracks := u.getTracks()
			replace := u.getReplace(false)

			ts := make([]conn.UpTrack, len(tracks))
			for i, t := range tracks {
				ts[i] = t
			}
			err := a.target.PushConn(g, u.id, u, ts, replace)
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

	case pushClientAction:
		if a.group != c.group.Name() {
			log.Printf("got client for wrong group")
			return nil
		}
		perms := append([]string(nil), a.permissions...)
		username := a.username
		return c.write(clientMessage{
			Type:        "user",
			Kind:        a.kind,
			Id:          a.id,
			Username:    &username,
			Permissions: perms,
			Data:        a.data,
		})
	case joinedAction:
		var status *group.Status
		var data map[string]interface{}
		var g *group.Group
		if a.group != "" {
			g = group.Get(a.group)
			if g != nil {
				s := g.Status(true, nil)
				status = &s
				data = g.Data()
			}
		}
		perms := append([]string(nil), c.permissions...)
		username := c.username
		err := c.write(clientMessage{
			Type:             "joined",
			Kind:             a.kind,
			Group:            a.group,
			Username:         &username,
			Permissions:      perms,
			Status:           status,
			Data:             data,
			RTCConfiguration: ice.ICEConfiguration(),
		})
		if err != nil {
			return err
		}
		if a.kind == "join" {
			if g == nil {
				log.Println("g is null when joining" +
					"this shouldn't happen")
				return nil
			}
			h := g.GetChatHistory()
			for _, m := range h {
				err := c.write(clientMessage{
					Type:     "chathistory",
					Source:   m.Id,
					Username: m.User,
					Time:     m.Time.Format(time.RFC3339),
					Value:    m.Value,
					Kind:     m.Kind,
				})
				if err != nil {
					return err
				}
			}
		}
	case permissionsChangedAction:
		g := c.Group()
		if g == nil {
			return errors.New("Permissions changed in no group")
		}
		perms := append([]string(nil), c.permissions...)
		status := g.Status(true, nil)
		username := c.username
		c.write(clientMessage{
			Type:             "joined",
			Kind:             "change",
			Group:            g.Name(),
			Username:         &username,
			Permissions:      perms,
			Status:           &status,
			RTCConfiguration: ice.ICEConfiguration(),
		})
		if !member("present", c.permissions) {
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
		d := c.Data()
		clients := g.GetClients(nil)
		go func(clients []group.Client) {
			for _, cc := range clients {
				cc.PushClient(
					g.Name(), "change", id, user, perms, d,
				)
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
	c.permissions = nil
	c.data = nil
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
		c.permissions = addnew("op", c.permissions)
		if g.Description().AllowRecording {
			c.permissions = addnew("record", c.permissions)
		}
	case "unop":
		c.permissions = remove("op", c.permissions)
		c.permissions = remove("record", c.permissions)
	case "present":
		c.permissions = addnew("present", c.permissions)
	case "unpresent":
		c.permissions = remove("present", c.permissions)
	default:
		return group.UserError("unknown permission")
	}
	c.action(permissionsChangedAction{})
	return nil
}

func (c *webClient) Kick(id string, user *string, message string) error {
	c.action(kickAction{id, user, message})
	return nil
}

func (c *webClient) Joined(group, kind string) error {
	c.action(joinedAction{group, kind})
	return nil
}

func kickClient(g *group.Group, id string, user *string, dest string, message string) error {
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
		if m.Username != nil {
			if *m.Username != c.Username() {
				return group.ProtocolError("spoofed username")
			}
		}
	}

	switch m.Type {
	case "join":
		if m.Kind == "leave" {
			if c.group == nil || c.group.Name() != m.Group {
				return group.UserError("you are not joined")
			}
			leaveGroup(c)
			return nil
		}

		if m.Kind != "join" {
			return group.ProtocolError("unknown kind")
		}

		if c.group != nil {
			return group.ProtocolError(
				"cannot join multiple groups",
			)
		}
		c.data = m.Data
		g, err := group.AddClient(m.Group, c,
			group.ClientCredentials{
				Username: m.Username,
				Password: m.Password,
				Token:    m.Token,
			},
		)
		if err != nil {
			var e, s string
			var autherr *group.NotAuthorisedError
			if os.IsNotExist(err) {
				s = "group does not exist"
			} else if errors.Is(err, group.ErrAnonymousNotAuthorised) {
				s = "please choose a username"
			} else if errors.Is(err, token.ErrUsernameRequired) {
				s = err.Error()
				e = "need-username"
			} else if errors.Is(err, group.ErrDuplicateUsername) {
				s = err.Error()
				e = "duplicate-username"
			} else if errors.As(err, &autherr) {
				s = "not authorised"
				time.Sleep(200 * time.Millisecond)
				log.Printf("Join group: %v", err)
			} else if _, ok := err.(group.UserError); ok {
				s = err.Error()
			} else {
				s = "internal server error"
				log.Printf("Join group: %v", err)
			}
			username := c.username
			return c.write(clientMessage{
				Type:     "joined",
				Kind:     "fail",
				Error:    e,
				Group:    m.Group,
				Username: &username,
				Value:    s,
			})
		}
		if redirect := g.Description().Redirect; redirect != "" {
			// We normally redirect at the HTTP level, but the group
			// description could have been edited in the meantime.
			username := c.username
			return c.write(clientMessage{
				Type:     "joined",
				Kind:     "redirect",
				Group:    m.Group,
				Username: &username,
				Value:    redirect,
			})
		}
		c.group = g
	case "request":
		requested, err := parseRequested(m.Request)
		if err != nil {
			return err
		}
		return c.setRequested(requested)
	case "requestStream":
		down := getDownConn(c, m.Id)
		if down == nil {
			return ErrUnknownId
		}
		requested, err := toStringArray(m.Request)
		if err != nil {
			return err
		}
		c.setRequestedStream(down, requested)
	case "offer":
		if m.Id == "" {
			return errEmptyId
		}
		if !member("present", c.permissions) {
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
			return failUpConnection(c, m.Id, err.Error())
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
				message = err.Error()
			}
			return closeDownConn(c, m.Id, message)
		}
		down := getDownConn(c, m.Id)
		if down == nil {
			return ErrUnknownId
		}
		if down.negotiationNeeded > negotiationUnneeded {
			err := negotiate(
				c, down,
				down.negotiationNeeded == negotiationRestartIce,
				"",
			)
			if err != nil {
				return closeDownConn(c, m.Id, err.Error())
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
				return closeDownConn(c, m.Id, err.Error())
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

		now := time.Now()

		if m.Type == "chat" {
			if m.Dest == "" {
				g.AddToChatHistory(
					m.Source, m.Username, now, m.Kind, m.Value,
				)
			}
		}
		mm := clientMessage{
			Type:       m.Type,
			Source:     m.Source,
			Dest:       m.Dest,
			Username:   m.Username,
			Privileged: member("op", c.permissions),
			Time:       now.Format(time.RFC3339),
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
			if !member("op", c.permissions) {
				return c.error(group.UserError("not authorised"))
			}
			message := ""
			v, ok := m.Value.(string)
			if ok {
				message = v
			}
			g.SetLocked(m.Kind == "lock", message)
		case "record":
			if !member("record", c.permissions) {
				return c.error(group.UserError("not authorised"))
			}
			for _, cc := range g.GetClients(c) {
				_, ok := cc.(*diskwriter.Client)
				if ok {
					return c.error(group.UserError("already recording"))
				}
			}
			disk := diskwriter.New(g)
			_, err := group.AddClient(g.Name(), disk,
				group.ClientCredentials{
					System: true,
				},
			)
			if err != nil {
				disk.Close()
				return c.error(err)
			}
			requestConns(disk, c.group, "")
		case "unrecord":
			if !member("record", c.permissions) {
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
			if !member("op", c.permissions) {
				return c.error(group.UserError("not authorised"))
			}
			s := ""
			for _, sg := range group.GetSubGroups(g.Name()) {
				plural := ""
				if sg.Clients > 1 {
					plural = "s"
				}
				s = s + fmt.Sprintf("%v (%v client%v)\n",
					sg.Name, sg.Clients, plural)
			}
			username := "Server"
			c.write(clientMessage{
				Type:     "chat",
				Dest:     c.id,
				Username: &username,
				Time:     time.Now().Format(time.RFC3339),
				Value:    s,
			})
		case "setdata":
			if !member("op", c.permissions) {
				return c.error(group.UserError("not authorised"))
			}
			data, ok := m.Value.(map[string]interface{})
			if !ok {
				return c.error(group.UserError(
					"Bad value in setdata",
				))
			}
			g.UpdateData(data)
		case "maketoken":
			terror := func(e, m string) error {
				return c.write(clientMessage{
					Type:       "usermessage",
					Kind:       "token",
					Privileged: true,
					Error:      e,
					Value:      m,
				})
			}
			if !member("token", c.permissions) {
				return terror("not-authorised", "not authorised")
			}
			tok, err := parseStatefulToken(m.Value)
			if err != nil {
				return terror("error", err.Error())
			}

			if tok.Token == "" {
				buf := make([]byte, 8)
				crand.Read(buf)
				tok.Token =
					base64.RawURLEncoding.EncodeToString(buf)
			} else {
				return terror("error", "client specified token")
			}

			if tok.Group != c.group.Name() {
				return terror("error", "wrong group in token")
			}

			if tok.Expires == nil {
				return terror("error", "token doesn't expire")
			}

			if tok.Username != nil &&
				c.group.UserExists(*tok.Username) {
				return terror("error", "that username is taken")
			}

			for _, p := range tok.Permissions {
				if !member(p, c.permissions) {
					return terror(
						"not-authorised",
						"not authorised",
					)
				}
			}

			user := c.username
			if user != "" {
				tok.IssuedBy = &user
			}

			now := time.Now()
			tok.IssuedAt = &now

			new, err := token.Add(tok)
			if err != nil {
				return terror("error", err.Error())
			}
			c.write(clientMessage{
				Type:       "usermessage",
				Kind:       "token",
				Privileged: true,
				Value:      new,
			})
		case "edittoken":
			terror := func(e, m string) error {
				return c.write(clientMessage{
					Type:       "usermessage",
					Kind:       "token",
					Privileged: true,
					Error:      e,
					Value:      m,
				})
			}
			if !member("op", c.permissions) ||
				!member("token", c.permissions) {
				return terror("not-authorised", "not authorised")
			}
			tok, err := parseStatefulToken(m.Value)
			if err != nil {
				return terror("error", err.Error())
			}
			if tok.Group != "" || tok.Username != nil ||
				tok.Permissions != nil ||
				tok.NotBefore != nil ||
				tok.IssuedBy != nil ||
				tok.IssuedAt != nil {
				return terror(
					"error", "this field cannot be edited",
				)
			}
			if tok.Expires == nil {
				return terror("error", "trying to edit nothing")
			}
			new, err := token.Edit(
				c.group.Name(), tok.Token, *tok.Expires,
			)
			if err != nil {
				return terror("error", err.Error())
			}
			c.write(clientMessage{
				Type:       "usermessage",
				Kind:       "token",
				Privileged: true,
				Value:      new,
			})
		case "listtokens":
			terror := func(e, m string) error {
				return c.write(clientMessage{
					Type:       "usermessage",
					Kind:       "tokenlist",
					Privileged: true,
					Error:      e,
					Value:      m,
				})
			}
			if !member("op", c.permissions) ||
				!member("token", c.permissions) {
				return terror("not-authorised", "not authorised")
			}
			tokens, err := token.List(c.group.Name())
			if err != nil {
				return terror("error", err.Error())
			}
			c.write(clientMessage{
				Type:       "usermessage",
				Kind:       "tokenlist",
				Privileged: true,
				Value:      tokens,
			})
		default:
			return group.UserError("unknown group action")
		}
	case "useraction":
		g := c.group
		if g == nil {
			return c.error(group.UserError("join a group first"))
		}
		switch m.Kind {
		case "op", "unop", "present", "unpresent":
			if !member("op", c.permissions) {
				return c.error(group.UserError("not authorised"))
			}
			err := setPermissions(g, m.Dest, m.Kind)
			if err != nil {
				return c.error(err)
			}
		case "kick":
			if !member("op", c.permissions) {
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
		case "setdata":
			if m.Dest != c.Id() {
				return c.error(group.UserError("not authorised"))
			}
			data, ok := m.Value.(map[string]interface{})
			if !ok {
				return c.error(group.UserError(
					"Bad value in setdata",
				))
			}
			if c.data == nil {
				c.data = make(map[string]interface{})
			}
			for k, v := range data {
				if v == nil {
					delete(c.data, k)
				} else {
					c.data[k] = v
				}
			}
			id := c.Id()
			user := c.Username()
			perms := c.Permissions()
			data = c.Data()
			go func(clients []group.Client) {
				for _, cc := range clients {
					cc.PushClient(
						g.Name(), "change",
						id, user, perms, data,
					)
				}
			}(g.GetClients(nil))
		default:
			return group.UserError("unknown user action")
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

func parseStatefulToken(value interface{}) (*token.Stateful, error) {
	data, ok := value.(map[string]interface{})
	if !ok || data == nil {
		return nil, errors.New("bad token value")
	}
	parseString := func(key string) (*string, error) {
		v := data[key]
		if v == nil {
			return nil, nil
		}
		vv, ok := v.(string)
		if !ok {
			return nil, errors.New("bad string value")
		}
		return &vv, nil
	}
	parseStringList := func(key string) ([]string, error) {
		v := data[key]
		if v == nil {
			return nil, nil
		}
		vv, ok := v.([]interface{})
		if !ok {
			return nil, errors.New("bad string list")
		}
		vvv := make([]string, 0, len(vv))
		for _, s := range vv {
			ss, ok := s.(string)
			if !ok {
				return nil, errors.New("bad string list")
			}
			vvv = append(vvv, ss)
		}
		return vvv, nil
	}
	parseTime := func(key string) (*time.Time, error) {
		v := data[key]
		if v == nil {
			return nil, nil
		}
		switch v := v.(type) {
		case string:
			vv, err := time.Parse(time.RFC3339, v)
			if err != nil {
				return nil, errors.New("bad time value")
			}
			return &vv, nil
		case float64: // relative time
			vv := time.Now().Add(time.Duration(v) * time.Millisecond)
			return &vv, nil
		default:
			return nil, errors.New("bad time value")
		}
	}

	t, err := parseString("token")
	if err != nil {
		return nil, err
	}
	tt := ""
	if t != nil {
		tt = *t
	}
	u, err := parseString("username")
	if err != nil {
		return nil, err
	}
	g, err := parseString("group")
	if err != nil {
		return nil, err
	}
	gg := ""
	if g != nil {
		gg = *g
	}
	p, err := parseStringList("permissions")
	if err != nil {
		return nil, err
	}
	e, err := parseTime("expires")
	if err != nil {
		return nil, err
	}
	n, err := parseTime("not-before")
	if err != nil {
		return nil, err
	}
	return &token.Stateful{
		Token:       tt,
		Group:       gg,
		Username:    u,
		Permissions: p,
		Expires:     e,
		NotBefore:   n,
	}, nil
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
			log.Printf("clientWriter: unexpected message %T", m)
			return
		}
	}
}

func (c *webClient) Warn(oponly bool, message string) error {
	if oponly && !member("op", c.permissions) {
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

func (c *webClient) action(a interface{}) {
	c.actions.Put(a)
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
			Kind:       "kicked",
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
