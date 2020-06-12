// Copyright (c) 2020 by Juliusz Chroboczek.

// This is not open source software.  Copy it, and I'll break into your
// house and tell your three year-old that Santa doesn't exist.

package main

import (
	"encoding/json"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"sfu/rtptime"

	"github.com/pion/webrtc/v2"
)

type client interface {
	Group() *group
	Id() string
	Username() string
	pushConn(id string, conn upConnection, tracks []upTrack, label string) error
	pushClient(id, username string, add bool) error
}

type chatHistoryEntry struct {
	id    string
	user  string
	value string
	me    bool
}

const (
	minVideoRate = 200000
	minAudioRate = 9600
)

type group struct {
	name        string
	dead        bool
	description *groupDescription
	locked      uint32

	mu      sync.Mutex
	clients map[string]client
	history []chatHistoryEntry
}

type pushConnAction struct {
	id     string
	conn   upConnection
	tracks []upTrack
}

type addLabelAction struct {
	id    string
	label string
}

type getUpAction struct {
	ch chan<- string
}

type pushConnsAction struct {
	c client
}

type connectionFailedAction struct {
	id string
}

type permissionsChangedAction struct{}

type kickAction struct{}

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
		s := webrtc.SettingEngine{}
		s.SetTrickle(true)
		m := webrtc.MediaEngine{}
		m.RegisterCodec(webrtc.NewRTPVP8CodecExt(
			webrtc.DefaultPayloadTypeVP8, 90000,
			[]webrtc.RTCPFeedback{
				{"goog-remb", ""},
				{"nack", ""},
				{"nack", "pli"},
				{"ccm", "fir"},
			},
			"",
		))
		m.RegisterCodec(webrtc.NewRTPOpusCodec(
			webrtc.DefaultPayloadTypeOpus, 48000,
		))
		groups.api = webrtc.NewAPI(
			webrtc.WithSettingEngine(s),
			webrtc.WithMediaEngine(m),
		)
	}

	var err error

	g := groups.groups[name]
	if g == nil {
		if desc == nil {
			desc, err = getDescription(name)
			if err != nil {
				return nil, err
			}
		}
		g = &group{
			name:        name,
			description: desc,
			clients:     make(map[string]client),
		}
		groups.groups[name] = g
	} else if desc != nil {
		g.description = desc
	} else if g.dead || time.Since(g.description.loadTime) > 5*time.Second {
		changed, err := descriptionChanged(name, g.description)
		if err != nil {
			if !os.IsNotExist(err) {
				log.Printf("Reading group %v: %v", name, err)
			}
			g.dead = true
			delGroupUnlocked(name)
			return nil, err
		}
		if changed {
			desc, err := getDescription(name)
			if err != nil {
				if !os.IsNotExist(err) {
					log.Printf("Reading group %v: %v",
						name, err)
				}
				g.dead = true
				delGroupUnlocked(name)
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

func getGroupNames() []string {
	groups.mu.Lock()
	defer groups.mu.Unlock()

	names := make([]string, 0, len(groups.groups))
	for name := range groups.groups {
		names = append(names, name)
	}
	return names
}

func getGroup(name string) *group {
	groups.mu.Lock()
	defer groups.mu.Unlock()

	return groups.groups[name]
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

func addClient(name string, c client, pass string) (*group, error) {
	g, err := addGroup(name, nil)
	if err != nil {
		return nil, err
	}

	perms, err := getPermission(g.description, c.Username(), pass)
	if err != nil {
		return nil, err
	}
	w, ok := c.(*webClient)
	if ok {
		w.permissions = perms
	}

	if !perms.Op && atomic.LoadUint32(&g.locked) != 0 {
		return nil, userError("group is locked")
	}

	g.mu.Lock()
	defer g.mu.Unlock()

	if !perms.Op && g.description.MaxClients > 0 {
		if len(g.clients) >= g.description.MaxClients {
			return nil, userError("too many users")
		}
	}
	if g.clients[c.Id()] != nil {
		return nil, protocolError("duplicate client id")
	}

	g.clients[c.Id()] = c

	go func(clients []client) {
		c.pushClient(c.Id(), c.Username(), true)
		for _, cc := range clients {
			err := c.pushClient(cc.Id(), cc.Username(), true)
			if err == ErrClientDead {
				return
			}
			cc.pushClient(c.Id(), c.Username(), true)
		}
	}(g.getClientsUnlocked(c))

	return g, nil
}

func delClient(c client) {
	g := c.Group()
	g.mu.Lock()
	defer g.mu.Unlock()

	if g.clients[c.Id()] != c {
		log.Printf("Deleting unknown client")
		return
	}
	delete(g.clients, c.Id())

	go func(clients []client) {
		for _, cc := range clients {
			cc.pushClient(c.Id(), c.Username(), false)
		}
	}(g.getClientsUnlocked(nil))
}

func (g *group) getClients(except client) []client {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.getClientsUnlocked(except)
}

func (g *group) getClientsUnlocked(except client) []client {
	clients := make([]client, 0, len(g.clients))
	for _, c := range g.clients {
		if c != except {
			clients = append(clients, c)
		}
	}
	return clients
}

func (g *group) getClientUnlocked(id string) client {
	for idd, c := range g.clients {
		if idd == id {
			return c
		}
	}
	return nil
}

func (g *group) Range(f func(c client) bool) {
	g.mu.Lock()
	defer g.mu.Unlock()
	for _, c := range g.clients {
		ok := f(c)
		if !ok {
			break
		}
	}
}

const maxChatHistory = 20

func (g *group) clearChatHistory() {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.history = nil
}

func (g *group) addToChatHistory(id, user, value string, me bool) {
	g.mu.Lock()
	defer g.mu.Unlock()

	if len(g.history) >= maxChatHistory {
		copy(g.history, g.history[1:])
		g.history = g.history[:len(g.history)-1]
	}
	g.history = append(g.history,
		chatHistoryEntry{id: id, user: user, value: value, me: me},
	)
}

func (g *group) getChatHistory() []chatHistoryEntry {
	g.mu.Lock()
	defer g.mu.Unlock()

	h := make([]chatHistoryEntry, len(g.history))
	copy(h, g.history)
	return h
}

type groupUser struct {
	Username string `json:"username,omitempty"`
	Password string `json:"password,omitempty"`
}

func matchUser(user, pass string, users []groupUser) (bool, bool) {
	for _, u := range users {
		if u.Username == "" {
			if u.Password == "" || u.Password == pass {
				return true, true
			}
		} else if u.Username == user {
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
	AllowRecording bool        `json:"allow-recording,omitempty"`
	Op             []groupUser `json:"op,omitempty"`
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
	Op      bool `json:"op,omitempty"`
	Present bool `json:"present,omitempty"`
	Record  bool `json:"record,omitempty"`
}

func getPermission(desc *groupDescription, user, pass string) (userPermission, error) {
	var p userPermission
	if !desc.AllowAnonymous && user == "" {
		return p, userError("anonymous users not allowed in this group, please choose a username")
	}
	if found, good := matchUser(user, pass, desc.Op); found {
		if good {
			p.Op = true
			p.Present = true
			if desc.AllowRecording {
				p.Record = true
			}
			return p, nil
		}
		return p, userError("not authorised")
	}
	if found, good := matchUser(user, pass, desc.Presenter); found {
		if good {
			p.Present = true
			return p, nil
		}
		return p, userError("not authorised")
	}
	if found, good := matchUser(user, pass, desc.Other); found {
		if good {
			return p, nil
		}
		return p, userError("not authorised")
	}
	return p, userError("not authorised")
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
	sort.Slice(gs, func(i, j int) bool {
		return gs[i].Name < gs[j].Name
	})
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
		name := fi.Name()[:len(fi.Name())-5]
		desc, err := getDescription(name)
		if err != nil {
			if !os.IsNotExist(err) {
				log.Printf("Reading group %v: %v", name, err)
			}
			continue
		}
		if desc.Public {
			addGroup(name, desc)
		}
	}
}

type groupStats struct {
	name    string
	clients []clientStats
}

type clientStats struct {
	id       string
	up, down []connStats
}

type connStats struct {
	id     string
	tracks []trackStats
}

type trackStats struct {
	bitrate    uint64
	maxBitrate uint64
	loss       uint8
	rtt        time.Duration
	jitter     time.Duration
}

func getGroupStats() []groupStats {
	names := getGroupNames()

	gs := make([]groupStats, 0, len(names))
	for _, name := range names {
		g := getGroup(name)
		if g == nil {
			continue
		}
		clients := g.getClients(nil)
		stats := groupStats{
			name:    name,
			clients: make([]clientStats, 0, len(clients)),
		}
		for _, c := range clients {
			c, ok := c.(*webClient)
			if ok {
				cs := getClientStats(c)
				stats.clients = append(stats.clients, cs)
			}
		}
		sort.Slice(stats.clients, func(i, j int) bool {
			return stats.clients[i].id < stats.clients[j].id
		})
		gs = append(gs, stats)
	}
	sort.Slice(gs, func(i, j int) bool {
		return gs[i].name < gs[j].name
	})

	return gs
}

func getClientStats(c *webClient) clientStats {
	c.mu.Lock()
	defer c.mu.Unlock()

	cs := clientStats{
		id: c.id,
	}

	for _, up := range c.up {
		conns := connStats{id: up.id}
		tracks := up.getTracks()
		for _, t := range tracks {
			expected, lost, _, _ := t.cache.GetStats(false)
			if expected == 0 {
				expected = 1
			}
			loss := uint8(lost * 100 / expected)
			jitter := time.Duration(t.jitter.Jitter()) *
				(time.Second / time.Duration(t.jitter.HZ()))
			rate, _ := t.rate.Estimate()
			conns.tracks = append(conns.tracks, trackStats{
				bitrate:    uint64(rate) * 8,
				maxBitrate: atomic.LoadUint64(&t.maxBitrate),
				loss:       loss,
				jitter:     jitter,
			})
		}
		cs.up = append(cs.up, conns)
	}
	sort.Slice(cs.up, func(i, j int) bool {
		return cs.up[i].id < cs.up[j].id
	})

	for _, down := range c.down {
		conns := connStats{id: down.id}
		for _, t := range down.tracks {
			jiffies := rtptime.Jiffies()
			rate, _ := t.rate.Estimate()
			rtt := rtptime.ToDuration(atomic.LoadUint64(&t.rtt),
				rtptime.JiffiesPerSec)
			loss, jitter := t.stats.Get(jiffies)
			j := time.Duration(jitter) * time.Second /
				time.Duration(t.track.Codec().ClockRate)
			conns.tracks = append(conns.tracks, trackStats{
				bitrate:    uint64(rate) * 8,
				maxBitrate: t.GetMaxBitrate(jiffies),
				loss:       uint8(uint32(loss) * 100 / 256),
				rtt:        rtt,
				jitter:     j,
			})
		}
		cs.down = append(cs.down, conns)
	}
	sort.Slice(cs.down, func(i, j int) bool {
		return cs.down[i].id < cs.down[j].id
	})

	return cs
}
