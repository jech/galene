package rtpconn

import (
	"encoding/json"
	"errors"
	"log"
	"os"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"github.com/pion/webrtc/v3"

	"sfu/conn"
	"sfu/diskwriter"
	"sfu/estimator"
	"sfu/group"
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
			Type:        "usermessage",
			Kind:        "error",
			Dest:        id,
			Priviledged: true,
			Value:       e.Error(),
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
	requested   map[string]uint32
	done        chan struct{}
	writeCh     chan interface{}
	writerDone  chan struct{}
	actionCh    chan interface{}

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

func (c *webClient) SetPermissions(perms group.ClientPermissions) {
	c.permissions = perms
}

func (c *webClient) OverridePermissions(g *group.Group) bool {
	return false
}

func (c *webClient) PushClient(id, username string, add bool) error {
	kind := "add"
	if !add {
		kind = "delete"
	}
	return c.write(clientMessage{
		Type:     "user",
		Kind:     kind,
		Id:       id,
		Username: username,
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
	Kind        string                     `json:"kind,omitempty"`
	Id          string                     `json:"id,omitempty"`
	Dest        string                     `json:"dest,omitempty"`
	Username    string                     `json:"username,omitempty"`
	Password    string                     `json:"password,omitempty"`
	Priviledged bool                       `json:"priviledged,omitempty"`
	Permissions *group.ClientPermissions   `json:"permissions,omitempty"`
	Group       string                     `json:"group,omitempty"`
	Value       string                     `json:"value,omitempty"`
	Time        int64                      `json:"time,omitempty"`
	Offer       *webrtc.SessionDescription `json:"offer,omitempty"`
	Answer      *webrtc.SessionDescription `json:"answer,omitempty"`
	Candidate   *webrtc.ICECandidateInit   `json:"candidate,omitempty"`
	Labels      map[string]string          `json:"labels,omitempty"`
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

func addUpConn(c *webClient, id string) (*rtpUpConnection, bool, error) {
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

	conn, err := newUpConn(c, id)
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

	go func(clients []group.Client) {
		for _, c := range clients {
			c.PushConn(conn.id, nil, nil, "")
		}
	}(c.Group().GetClients(c))

	conn.pc.Close()
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

func addDownConn(c *webClient, id string, remote conn.Up) (*rtpDownConnection, error) {
	conn, err := newDownConn(c, id, remote)
	if err != nil {
		return nil, err
	}

	err = addDownConnHelper(c, conn, remote)
	if err != nil {
		conn.pc.Close()
		return nil, err
	}
	return conn, err
}

func addDownConnHelper(c *webClient, conn *rtpDownConnection, remote conn.Up) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.up != nil && c.up[conn.id] != nil {
		return errors.New("Adding duplicate connection")
	}

	if c.down == nil {
		c.down = make(map[string]*rtpDownConnection)
	}

	old := c.down[conn.id]
	if old != nil {
		// Avoid calling Close under a lock
		go old.pc.Close()
	}

	conn.pc.OnICECandidate(func(candidate *webrtc.ICECandidate) {
		sendICE(c, conn.id, candidate)
	})

	conn.pc.OnICEConnectionStateChange(func(state webrtc.ICEConnectionState) {
		if state == webrtc.ICEConnectionStateFailed {
			c.action(connectionFailedAction{id: conn.id})
		}
	})

	err := remote.AddLocal(conn)
	if err != nil {
		return err
	}

	c.down[conn.id] = conn
	return nil
}

func delDownConn(c *webClient, id string) bool {
	conn := delDownConnHelper(c, id)
	if conn != nil {
		conn.pc.Close()
		return true
	}
	return false
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

func addDownTrack(c *webClient, conn *rtpDownConnection, remoteTrack conn.UpTrack, remoteConn conn.Up) (*webrtc.RTPSender, error) {
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
		track:      local,
		remote:     remoteTrack,
		maxBitrate: new(bitrate),
		stats:      new(receiverStats),
		rate:       estimator.New(time.Second),
	}
	conn.tracks = append(conn.tracks, track)

	go rtcpDownListener(conn, track, s)

	return s, nil
}

func negotiate(c *webClient, down *rtpDownConnection, renegotiate, restartIce bool) error {
	options := webrtc.OfferOptions{ICERestart: restartIce}
	offer, err := down.pc.CreateOffer(&options)
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

	kind := ""
	if renegotiate {
		kind = "renegotiate"
	}

	return c.write(clientMessage{
		Type:   "offer",
		Kind:   kind,
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

func gotOffer(c *webClient, id string, offer webrtc.SessionDescription, renegotiate bool, labels map[string]string) error {
	if !renegotiate {
		// unless the client indicates that this is a compatible
		// renegotiation, tear down the existing connection.
		delUpConn(c, id)
	}

	up, isnew, err := addUpConn(c, id)
	if err != nil {
		return err
	}

	if u := c.Username(); u != "" {
		up.label = u
	}
	err = up.pc.SetRemoteDescription(offer)
	if err != nil {
		if renegotiate && !isnew {
			// create a new PC from scratch
			log.Printf("SetRemoteDescription(offer): %v", err)
			return gotOffer(c, id, offer, false, labels)
		}
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

var ErrUnknownId = errors.New("unknown id")

func gotAnswer(c *webClient, id string, answer webrtc.SessionDescription) error {
	down := getDownConn(c, id)
	if down == nil {
		return ErrUnknownId
	}
	err := down.pc.SetRemoteDescription(answer)
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

func (c *webClient) setRequested(requested map[string]uint32) error {
	if c.down != nil {
		down := make([]string, 0, len(c.down))
		for id := range c.down {
			down = append(down, id)
		}
		for _, id := range down {
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

func pushConns(c group.Client) {
	clients := c.Group().GetClients(c)
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

func addDownConnTracks(c *webClient, remote conn.Up, tracks []conn.UpTrack) (*rtpDownConnection, error) {
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

func (c *webClient) PushConn(id string, up conn.Up, tracks []conn.UpTrack, label string) error {
	err := c.action(pushConnAction{id, up, tracks})
	if err != nil {
		return err
	}
	if up != nil && label != "" {
		err := c.action(addLabelAction{up.Id(), up.Label()})
		if err != nil {
			return err
		}
	}
	return nil
}

func StartClient(conn *websocket.Conn) (err error) {
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

	if m.Type != "login" {
		conn.WriteMessage(websocket.CloseMessage,
			websocket.FormatCloseMessage(
				websocket.CloseProtocolError,
				"you must login first",
			),
		)
		conn.Close()
		return
	}

	c := &webClient{
		id:       m.Id,
		username: m.Username,
		password: m.Password,
		actionCh: make(chan interface{}, 10),
		done:     make(chan struct{}),
	}

	defer close(c.done)

	c.writeCh = make(chan interface{}, 25)
	defer func() {
		if isWSNormalError(err) {
			err = nil
			c.close(nil)
		} else {
			m, e := errorToWSCloseMessage(c.id, err)
			if m != nil {
				c.write(*m)
			}
			c.close(e)
		}
	}()

	c.writerDone = make(chan struct{})
	go clientWriter(conn, c.writeCh, c.writerDone)

	err = conn.ReadJSON(&m)
	if err != nil {
		return err
	}

	if m.Type != "join" {
		return group.ProtocolError("you must join a group first")
	}

	g, err := group.AddClient(m.Group, c)
	if err != nil {
		if os.IsNotExist(err) {
			err = group.UserError("group does not exist")
		} else if err == group.ErrNotAuthorised {
			err = group.UserError("not authorised")
			time.Sleep(200 * time.Millisecond)
		}
		return
	}
	if redirect := g.Redirect(); redirect != "" {
		// We normally redirect at the HTTP level, but the group
		// description could have been edited in the meantime.
		err = group.UserError("group is now at " + redirect)
		return
	}
	c.group = g
	defer group.DelClient(c)

	return clientLoop(c, conn)
}

type pushConnAction struct {
	id     string
	conn   conn.Up
	tracks []conn.UpTrack
}

type addLabelAction struct {
	id    string
	label string
}

type pushConnsAction struct {
	c group.Client
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

func clientLoop(c *webClient, ws *websocket.Conn) error {
	read := make(chan interface{}, 1)
	go clientReader(ws, read, c.done)

	defer func() {
		c.setRequested(map[string]uint32{})
		if c.up != nil {
			for id := range c.up {
				delUpConn(c, id)
			}
		}
	}()

	perms := c.permissions
	c.write(clientMessage{
		Type:        "permissions",
		Permissions: &perms,
	})

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

	readTime := time.Now()

	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()

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
			case pushConnAction:
				if a.conn == nil {
					found := delDownConn(c, a.id)
					if found {
						c.write(clientMessage{
							Type: "close",
							Id:   a.id,
						})
					} else {
						log.Printf("Deleting unknown " +
							"down connection")
					}
					continue
				}
				down, err := addDownConnTracks(
					c, a.conn, a.tracks,
				)
				if err != nil {
					return err
				}
				if down != nil {
					err = negotiate(c, down, false, false)
					if err != nil {
						log.Printf("Negotiate: %v", err)
						delDownConn(c, down.id)
						err = failUpConnection(
							c, down.id,
							"negotiation failed",
						)
						if err != nil {
							return err
						}
						continue
					}
				}
			case addLabelAction:
				c.write(clientMessage{
					Type:  "label",
					Id:    a.id,
					Value: a.label,
				})
			case pushConnsAction:
				for _, u := range c.up {
					if !u.complete() {
						continue
					}
					tracks := u.getTracks()
					ts := make([]conn.UpTrack, len(tracks))
					for i, t := range tracks {
						ts[i] = t
					}
					go a.c.PushConn(u.id, u, ts, u.label)
				}
			case connectionFailedAction:
				if down := getDownConn(c, a.id); down != nil {
					err := negotiate(c, down, true, true)
					if err != nil {
						return err
					}
					tracks := make(
						[]conn.UpTrack, len(down.tracks),
					)
					for i, t := range down.tracks {
						tracks[i] = t.remote
					}
					go c.PushConn(
						down.remote.Id(), down.remote,
						tracks, down.remote.Label(),
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
				perms := c.permissions
				c.write(clientMessage{
					Type:        "permissions",
					Permissions: &perms,
				})
				if !c.permissions.Present {
					up := getUpConns(c)
					for _, u := range up {
						found := delUpConn(c, u.id)
						if found {
							failUpConnection(
								c, u.id,
								"permission denied",
							)
						}
					}
				}
			case kickAction:
				return group.KickError{
					a.id, a.username, a.message,
				}
			default:
				log.Printf("unexpected action %T", a)
				return errors.New("unexpected action")
			}
		case <-ticker.C:
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

func failDownConnection(c *webClient, id string, message string) error {
	if id != "" {
		err := c.write(clientMessage{
			Type: "close",
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

	c, ok := client.(group.Kickable)
	if !ok {
		return group.UserError("this client is not kickable")
	}

	return c.Kick(id, user, message)
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
			return c.error(group.UserError("not authorised"))
		}
		if m.Offer == nil {
			return group.ProtocolError("null offer")
		}
		err := gotOffer(
			c, m.Id, *m.Offer, m.Kind == "renegotiate", m.Labels,
		)
		if err != nil {
			log.Printf("gotOffer: %v", err)
			return failUpConnection(c, m.Id, "negotiation failed")
		}
	case "answer":
		if m.Answer == nil {
			return group.ProtocolError("null answer")
		}
		err := gotAnswer(c, m.Id, *m.Answer)
		if err != nil {
			log.Printf("gotAnswer: %v", err)
			message := ""
			if err != ErrUnknownId {
				message = "negotiation failed"
			}
			return failDownConnection(c, m.Id, message)
		}
	case "renegotiate":
		down := getDownConn(c, m.Id)
		if down != nil {
			err := negotiate(c, down, true, true)
			if err != nil {
				return failDownConnection(c, m.Id,
					"renegotiation failed")
			}
		} else {
			log.Printf("Trying to renegotiate unknown connection")
		}
	case "close":
		found := delUpConn(c, m.Id)
		if !found {
			log.Printf("Deleting unknown up connection %v", m.Id)
		}
	case "ice":
		if m.Candidate == nil {
			return group.ProtocolError("null candidate")
		}
		err := gotICE(c, m.Candidate, m.Id)
		if err != nil {
			log.Printf("ICE: %v", err)
		}
	case "chat", "usermessage":
		if m.Id != c.id {
			return group.UserError("wrong sender id")
		}
		if m.Username != "" && m.Username != c.username {
			return group.UserError("wrong sender username")
		}

		tm := group.ToJSTime(time.Now())
		if m.Type == "chat" {
			if m.Dest == "" {
				c.group.AddToChatHistory(
					m.Id, m.Username, tm, m.Kind, m.Value,
				)
			}
		}
		mm := clientMessage{
			Type:        m.Type,
			Id:          m.Id,
			Dest:        m.Dest,
			Username:    m.Username,
			Priviledged: c.permissions.Op,
			Time:        tm,
			Kind:        m.Kind,
			Value:       m.Value,
		}
		if m.Dest == "" {
			clients := c.group.GetClients(nil)
			for _, cc := range clients {
				ccc, ok := cc.(*webClient)
				if ok {
					ccc.write(mm)
				}
			}
		} else {
			cc := c.group.GetClient(m.Dest)
			if cc == nil {
				return c.error(group.UserError("user unknown"))
			}
			ccc, ok := cc.(*webClient)
			if !ok {
				return c.error(group.UserError("this user doesn't chat"))
			}
			ccc.write(mm)
		}
	case "groupaction":
		if m.Id != c.id {
			return group.UserError("wrong sender id")
		}
		if m.Username != "" && m.Username != c.username {
			return group.UserError("wrong sender username")
		}
		switch m.Kind {
		case "clearchat":
			c.group.ClearChatHistory()
			m := clientMessage{Type: "clearchat"}
			clients := c.group.GetClients(nil)
			for _, cc := range clients {
				cc, ok := cc.(*webClient)
				if ok {
					cc.write(m)
				}
			}
		case "lock", "unlock":
			if !c.permissions.Op {
				return c.error(group.UserError("not authorised"))
			}
			c.group.SetLocked(m.Kind == "lock", m.Value)
		case "record":
			if !c.permissions.Record {
				return c.error(group.UserError("not authorised"))
			}
			for _, cc := range c.group.GetClients(c) {
				_, ok := cc.(*diskwriter.Client)
				if ok {
					return c.error(group.UserError("already recording"))
				}
			}
			disk := diskwriter.New(c.group)
			_, err := group.AddClient(c.group.Name(), disk)
			if err != nil {
				disk.Close()
				return c.error(err)
			}
			go pushConns(disk)
		case "unrecord":
			if !c.permissions.Record {
				return c.error(group.UserError("not authorised"))
			}
			for _, cc := range c.group.GetClients(c) {
				disk, ok := cc.(*diskwriter.Client)
				if ok {
					disk.Close()
					group.DelClient(disk)
				}
			}
		default:
			return group.ProtocolError("unknown group action")
		}
	case "useraction":
		if m.Id != c.id {
			return group.UserError("wrong sender id")
		}
		if m.Username != "" && m.Username != c.username {
			return group.UserError("wrong sender username")
		}
		switch m.Kind {
		case "op", "unop", "present", "unpresent":
			if !c.permissions.Op {
				return c.error(group.UserError("not authorised"))
			}
			err := setPermissions(c.group, m.Dest, m.Kind)
			if err != nil {
				return c.error(err)
			}
		case "kick":
			if !c.permissions.Op {
				return c.error(group.UserError("not authorised"))
			}
			err := kickClient(c.group, m.Id, m.Username, m.Dest, m.Value)
			if err != nil {
				return c.error(err)
			}
		default:
			return group.ProtocolError("unknown user action")
		}
	case "pong":
		// nothing
	case "ping":
		c.write(clientMessage{
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

var ErrClientDead = errors.New("client is dead")

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
		return ErrClientDead
	}
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
			Type:        "usermessage",
			Kind:        "error",
			Dest:        id,
			Priviledged: true,
			Value:       e.Error(),
		}
	case group.KickError:
		message := e.Message
		if message == "" {
			message = "you have been kicked out"
		}
		return &clientMessage{
			Type:        "usermessage",
			Kind:        "error",
			Id:          e.Id,
			Username:    e.Username,
			Dest:        id,
			Priviledged: true,
			Value:       message,
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
