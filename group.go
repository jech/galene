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

	"sfu/estimator"
	"sfu/jitter"
	"sfu/mono"
	"sfu/packetcache"

	"github.com/pion/webrtc/v2"
)

type upTrack struct {
	track                *webrtc.Track
	rate                 *estimator.Estimator
	cache                *packetcache.Cache
	jitter               *jitter.Estimator
	maxBitrate           uint64
	lastPLI              uint64
	lastSenderReport     uint32
	lastSenderReportTime uint32

	mu    sync.Mutex
	local []*downTrack
}

func (up *upTrack) addLocal(local *downTrack) {
	up.mu.Lock()
	defer up.mu.Unlock()
	up.local = append(up.local, local)
}

func (up *upTrack) delLocal(local *downTrack) bool {
	up.mu.Lock()
	defer up.mu.Unlock()
	for i, l := range up.local {
		if l == local {
			up.local = append(up.local[:i], up.local[i+1:]...)
			return true
		}
	}
	return false
}

func (up *upTrack) getLocal() []*downTrack {
	up.mu.Lock()
	defer up.mu.Unlock()
	local := make([]*downTrack, len(up.local))
	copy(local, up.local)
	return local
}

type upConnection struct {
	id         string
	label      string
	pc         *webrtc.PeerConnection
	trackCount int
	tracks     []*upTrack
}

type bitrate struct {
	bitrate      uint64
	microseconds uint64
}

const receiverReportTimeout = 8000000

func (br *bitrate) Set(bitrate uint64, now uint64) {
	// this is racy -- a reader might read the
	// data between the two writes.  This shouldn't
	// matter, we'll recover at the next sample.
	atomic.StoreUint64(&br.bitrate, bitrate)
	atomic.StoreUint64(&br.microseconds, now)
}

func (br *bitrate) Get(now uint64) uint64 {
	ts := atomic.LoadUint64(&br.microseconds)
	if now < ts || now > ts+receiverReportTimeout {
		return ^uint64(0)
	}
	return atomic.LoadUint64(&br.bitrate)
}

type receiverStats struct {
	loss         uint32
	jitter       uint32
	microseconds uint64
}

func (s *receiverStats) Set(loss uint8, jitter uint32, now uint64) {
	atomic.StoreUint32(&s.loss, uint32(loss))
	atomic.StoreUint32(&s.jitter, jitter)
	atomic.StoreUint64(&s.microseconds, now)
}

func (s *receiverStats) Get(now uint64) (uint8, uint32) {
	ts := atomic.LoadUint64(&s.microseconds)
	if now < ts || now > ts+receiverReportTimeout {
		return 0, 0
	}
	return uint8(atomic.LoadUint32(&s.loss)), atomic.LoadUint32(&s.jitter)
}

type downTrack struct {
	track          *webrtc.Track
	remote         *upTrack
	maxLossBitrate *bitrate
	maxREMBBitrate *bitrate
	rate           *estimator.Estimator
	stats          *receiverStats
}

func (down *downTrack) GetMaxBitrate(now uint64) uint64 {
	br1 := down.maxLossBitrate.Get(now)
	br2 := down.maxREMBBitrate.Get(now)
	if br1 < br2 {
		return br1
	}
	return br2
}

type downConnection struct {
	id     string
	pc     *webrtc.PeerConnection
	remote *upConnection
	tracks []*downTrack
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

	mu   sync.Mutex
	down map[string]*downConnection
	up   map[string]*upConnection
}

type chatHistoryEntry struct {
	id    string
	user  string
	value string
	me    bool
}

type group struct {
	name        string
	dead        bool
	description *groupDescription

	mu      sync.Mutex
	clients map[string]*client
	history []chatHistoryEntry
}

type delConnAction struct {
	id string
}

type addTrackAction struct {
	track  *upTrack
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
			clients:     make(map[string]*client),
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

	g.mu.Lock()
	defer g.mu.Unlock()

	if !perms.Op && g.description.MaxClients > 0 {
		if len(g.clients) >= g.description.MaxClients {
			return nil, nil, userError("too many users")
		}
	}
	if g.clients[client.id] != nil {
		return nil, nil, protocolError("duplicate client id")
	}

	var users []userid
	for _, c := range g.clients {
		users = append(users, userid{c.id, c.username})
	}
	g.clients[client.id] = client
	return g, users, nil
}

func delClient(c *client) {
	c.group.mu.Lock()
	defer c.group.mu.Unlock()
	g := c.group

	if g.clients[c.id] != c {
		log.Printf("Deleting unknown client")
		return
	}
	delete(g.clients, c.id)

	if len(g.clients) == 0 && !g.description.Public {
		delGroupUnlocked(g.name)
	}
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

func (g *group) getClientUnlocked(id string) *client {
	for _, c := range g.clients {
		if c.id == id {
			return c
		}
	}
	return nil
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
	g.mu.Unlock()

	h := make([]chatHistoryEntry, len(g.history))
	copy(h, g.history)
	return h
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

func (c *client) error(err error) error {
	switch e := err.(type) {
	case userError:
		return c.write(clientMessage{
			Type:  "error",
			Value: "The server said: " + string(e),
		})
	default:
		return err
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

func setPermission(g *group, id string, perm string) error {
	g.mu.Lock()
	defer g.mu.Unlock()

	c := g.getClientUnlocked(id)
	if c == nil {
		return userError("no such user")
	}

	switch perm {
	case "op":
		c.permissions.Op = true
	case "unop":
		c.permissions.Op = false
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

	c := g.getClientUnlocked(id)
	if c == nil {
		return userError("no such user")
	}

	return c.action(kickAction{})
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
			cs := getClientStats(c)
			stats.clients = append(stats.clients, cs)
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

func getClientStats(c *client) clientStats {
	c.mu.Lock()
	defer c.mu.Unlock()

	cs := clientStats{
		id: c.id,
	}

	for _, up := range c.up {
		conns := connStats{id: up.id}
		for _, t := range up.tracks {
			expected, lost, _, _ := t.cache.GetStats(false)
			if expected == 0 {
				expected = 1
			}
			loss := uint8(lost * 100 / expected)
			jitter := time.Duration(t.jitter.Jitter()) *
				(time.Second / time.Duration(t.jitter.HZ()))
			conns.tracks = append(conns.tracks, trackStats{
				bitrate:    uint64(t.rate.Estimate()) * 8,
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
			loss, jitter := t.stats.Get(mono.Microseconds())
			j := time.Duration(jitter) * time.Second /
				time.Duration(t.track.Codec().ClockRate)
			conns.tracks = append(conns.tracks, trackStats{
				bitrate:    uint64(t.rate.Estimate()) * 8,
				maxBitrate: t.GetMaxBitrate(mono.Microseconds()),
				loss:       uint8(uint32(loss) * 100 / 256),
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
