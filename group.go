// Copyright (c) 2020 by Juliusz Chroboczek.

// This is not open source software.  Copy it, and I'll break into your
// house and tell your three year-old that Santa doesn't exist.

package main

import (
	"encoding/json"
	"log"
	"os"
	"path/filepath"
	"sync"
	"strings"
	"time"

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
	group       *group
	id          string
	username    string
	permissions userPermission
	done        chan struct{}
	writeCh     chan interface{}
	writerDone  chan struct{}
	actionCh    chan interface{}
	down        map[string]*downConnection
	up          map[string]*upConnection
}

type group struct {
	name        string
	dead        bool
	description *groupDescription

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

func addGroup(name string, desc *groupDescription) (*group, error) {
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

	var err error

	g := groups.groups[name]
	if g == nil {
		if(desc == nil) {
			desc, err = getDescription(name)
			if err != nil {
				return nil, err
			}
		}
		g = &group{
			name:        name,
			description: desc,
		}
		groups.groups[name] = g
	} else if desc != nil {
		g.description = desc
	} else if g.dead || time.Since(g.description.loadTime) > 5*time.Second {
		changed, err := descriptionChanged(name, g.description)
		if err != nil {
			g.dead = true
			if !g.description.Public {
				delGroupUnlocked(name)
			}
			return nil, err
		}
		if changed {
			desc, err := getDescription(name)
			if err != nil {
				g.dead = true
				if !g.description.Public {
					delGroupUnlocked(name)
				}
				return nil, err
			}
			g.dead = false
			g.description = desc
		} else {
			g.description.loadTime = time.Now()
		}
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

func addClient(name string, client *client, user, pass string) (*group, []userid, error) {
	g, err := addGroup(name, nil)
	if err != nil {
		return nil, nil, err
	}

	perms, err := getPermission(g.description, user, pass)
	if err != nil {
		return nil, nil, err
	}
	client.permissions = perms

	var users []userid
	g.mu.Lock()
	defer g.mu.Unlock()
	if !perms.Admin && g.description.MaxClients > 0 {
		if len(g.clients) >= g.description.MaxClients {
			return nil, nil, userError("too many users")
		}
	}
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
			if len(g.clients) == 0 && !g.description.Public {
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
		if !ok {
			break
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

type groupUser struct {
	Username string `json:"username,omitempty"`
	Password string `json:"password,omitempty"`
}

func matchUser(user, pass string, users []groupUser) (bool, bool) {
	for _, u := range users {
		if (u.Username == "" || u.Username == user) {
			return true, (u.Password == "" || u.Password == pass)
		}
	}
	return false, false
}

type groupDescription struct {
	loadTime       time.Time   `json:"-"`
	modTime        time.Time   `json:"-"`
	fileSize       int64       `json:"-"`
	Public         bool        `json:"public,omitempty"`
	MaxClients     int         `json:"max-clients,omitempty"`
	AllowAnonymous bool        `json:"allow-anonymous,omitempty"`
	Admin          []groupUser `json:"admin,omitempty"`
	Presenter      []groupUser `json:"presenter,omitempty"`
	Other          []groupUser `json:"other,omitempty"`
}

func descriptionChanged(name string, old *groupDescription) (bool, error) {
	fi, err := os.Stat(filepath.Join(groupsDir, name+".json"))
	if err != nil {
		if os.IsNotExist(err) {
			err = userError("group does not exist")
		}
		return false, err
	}
	if fi.Size() != old.fileSize || fi.ModTime() != old.modTime {
		return true, err
	}
	return false, err
}

func getDescription(name string) (*groupDescription, error) {
	r, err := os.Open(filepath.Join(groupsDir, name+".json"))
	if err != nil {
		if os.IsNotExist(err) {
			err = userError("group does not exist")
		}
		return nil, err
	}
	defer r.Close()

	var desc groupDescription

	fi, err := r.Stat()
	if err != nil {
		return nil, err
	}
	desc.fileSize = fi.Size()
	desc.modTime = fi.ModTime()

	d := json.NewDecoder(r)
	err = d.Decode(&desc)
	if err != nil {
		return nil, err
	}
	desc.loadTime = time.Now()
	return &desc, nil
}

type userPermission struct {
	Admin   bool `json:"admin,omitempty"`
	Present bool `json:"present,omitempty"`
}

func getPermission(desc *groupDescription, user, pass string) (userPermission, error) {
	var p userPermission
	if !desc.AllowAnonymous && user == "" {
		return p, userError("anonymous users not allowed in this group")
	}
	if found, good := matchUser(user, pass, desc.Admin); found {
		if good {
			p.Admin = true
			p.Present = true
			return p, nil
		}
		return p, userError("not authorized")
	}
	if found, good := matchUser(user, pass, desc.Presenter); found {
		if good {
			p.Present = true
			return p, nil
		}
		return p, userError("not authorized")
	}
	if found, good := matchUser(user, pass, desc.Other); found {
		if good {
			return p, nil
		}
		return p, userError("not authorized")
	}
	return p, userError("not authorized")
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
		if g.description.Public {
			gs = append(gs, publicGroup{
				Name:        g.name,
				ClientCount: len(g.clients),
			})
		}
	}
	return gs
}

func readPublicGroups() {
	dir, err := os.Open(groupsDir)
	if err != nil {
		return
	}
	defer dir.Close()

	fis, err := dir.Readdir(-1)
	if err != nil {
		log.Printf("readPublicGroups: %v", err)
		return
	}

	for _, fi := range fis {
		if !strings.HasSuffix(fi.Name(), ".json") {
			continue
		}
		name := fi.Name()[:len(fi.Name()) - 5]
		desc, err := getDescription(name)
		if err != nil {
			continue
		}
		if desc.Public {
			addGroup(name, desc)
		}
	}
}
