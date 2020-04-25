// Copyright (c) 2020 by Juliusz Chroboczek.

// This is not open source software.  Copy it, and I'll break into your
// house and tell your three year-old that Santa doesn't exist.

package main

import (
	"log"
	"sync"

	"github.com/pion/webrtc/v2"
)

type trackPair struct {
	remote, local *webrtc.Track
}

type upConnection struct {
	id              string
	label           string
	pc              *webrtc.PeerConnection
	maxAudioBitrate uint32
	maxVideoBitrate uint32
	streamCount     int
	pairs           []trackPair
}

type downConnection struct {
	id         string
	pc         *webrtc.PeerConnection
	remote     *upConnection
	maxBitrate uint32
}

type client struct {
	group      *group
	id         string
	username   string
	done       chan struct{}
	writeCh    chan interface{}
	writerDone chan struct{}
	actionCh   chan interface{}
	down       map[string]*downConnection
	up         map[string]*upConnection
}

type group struct {
	name   string
	public bool

	mu      sync.Mutex
	clients []*client
}

type delPCAction struct {
	id string
}

type addTrackAction struct {
	id     string
	track  *webrtc.Track
	remote *upConnection
	done   bool
}

type addLabelAction struct {
	id    string
	label string
}

type getUpAction struct {
	ch chan<- string
}

type pushTracksAction struct {
	c *client
}

var groups struct {
	mu     sync.Mutex
	groups map[string]*group
	api    *webrtc.API
}

func addGroup(name string) (*group, error) {
	groups.mu.Lock()
	defer groups.mu.Unlock()

	if groups.groups == nil {
		groups.groups = make(map[string]*group)
		m := webrtc.MediaEngine{}
		m.RegisterCodec(webrtc.NewRTPVP8Codec(
			webrtc.DefaultPayloadTypeVP8, 90000))
		m.RegisterCodec(webrtc.NewRTPOpusCodec(
			webrtc.DefaultPayloadTypeOpus, 48000))
		groups.api = webrtc.NewAPI(
			webrtc.WithMediaEngine(m),
		)
	}

	g := groups.groups[name]

	if g == nil {
		g = &group{
			name: name,
		}
		groups.groups[name] = g
	}

	return g, nil
}

func delGroupUnlocked(name string) bool {
	g := groups.groups[name]
	if g == nil {
		return true
	}

	if len(g.clients) != 0 {
		return false
	}

	delete(groups.groups, name)
	return true
}

type userid struct {
	id       string
	username string
}

func addClient(name string, client *client) (*group, []userid, error) {
	g, err := addGroup(name)
	if err != nil {
		return nil, nil, err
	}

	var users []userid
	g.mu.Lock()
	defer g.mu.Unlock()
	for _, c := range g.clients {
		users = append(users, userid{c.id, c.username})
	}
	g.clients = append(g.clients, client)
	return g, users, nil
}

func delClient(c *client) {
	c.group.mu.Lock()
	defer c.group.mu.Unlock()
	g := c.group
	for i, cc := range g.clients {
		if cc == c {
			g.clients =
				append(g.clients[:i], g.clients[i+1:]...)
			c.group = nil
			if len(g.clients) == 0 {
				delGroupUnlocked(g.name)
			}
			return
		}
	}
	log.Printf("Deleting unknown client")
	c.group = nil
}

func (g *group) getClients(except *client) []*client {
	g.mu.Lock()
	defer g.mu.Unlock()
	clients := make([]*client, 0, len(g.clients))
	for _, c := range g.clients {
		if c != except {
			clients = append(clients, c)
		}
	}
	return clients
}

func (g *group) Range(f func(c *client) bool) {
	g.mu.Lock()
	defer g.mu.Unlock()
	for _, c := range g.clients {
		ok := f(c)
		if(!ok){
			break;
		}
	}
}

type writerDeadError int

func (err writerDeadError) Error() string {
	return "client writer died"
}

func (c *client) write(m clientMessage) error {
	select {
	case c.writeCh <- m:
		return nil
	case <-c.writerDone:
		return writerDeadError(0)
	}
}

type clientDeadError int

func (err clientDeadError) Error() string {
	return "client dead"
}

func (c *client) action(m interface{}) error {
	select {
	case c.actionCh <- m:
		return nil
	case <-c.done:
		return clientDeadError(0)
	}
}

type publicGroup struct {
	Name        string `json:"name"`
	ClientCount int    `json:"clientCount"`
}

func getPublicGroups() []publicGroup {
	gs := make([]publicGroup, 0)
	groups.mu.Lock()
	defer groups.mu.Unlock()
	for _, g := range groups.groups {
		if g.public {
			gs = append(gs, publicGroup{
				Name:        g.name,
				ClientCount: len(g.clients),
			})
		}
	}
	return gs
}
