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
	"time"

	"github.com/pion/webrtc/v3"
)

type chatHistoryEntry struct {
	id    string
	user  string
	kind  string
	value string
}

const (
	minBitrate = 200000
)

type group struct {
	name string

	mu          sync.Mutex
	description *groupDescription
	// indicates that the group no longer exists, but it still has clients
	dead    bool
	locked  bool
	clients map[string]client
	history []chatHistoryEntry
}

func (g *group) Locked() bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.locked
}

func (g *group) SetLocked(locked bool) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.locked = locked
}

func (g *group) Public() bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.description.Public
}

func (g *group) Redirect() string {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.description.Redirect
}

func (g *group) AllowRecording() bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.description.AllowRecording
}

var groups struct {
	mu     sync.Mutex
	groups map[string]*group
	api    *webrtc.API
}

func (g *group) API() *webrtc.API {
	return groups.api
}

func addGroup(name string, desc *groupDescription) (*group, error) {
	groups.mu.Lock()
	defer groups.mu.Unlock()

	if groups.groups == nil {
		groups.groups = make(map[string]*group)
		s := webrtc.SettingEngine{}
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
		return g, nil
	}

	g.mu.Lock()
	defer g.mu.Unlock()

	if desc != nil {
		g.description = desc
		g.dead = false
		return g, nil
	}

	if g.dead || time.Since(g.description.loadTime) > 5*time.Second {
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

func rangeGroups(f func(g *group) bool) {
	groups.mu.Lock()
	defer groups.mu.Unlock()

	for _, g := range groups.groups {
		ok := f(g)
		if !ok {
			break
		}
	}
}

func getGroupNames() []string {
	names := make([]string, 0)

	rangeGroups(func(g *group) bool {
		names = append(names, g.name)
		return true
	})
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

func addClient(name string, c client) (*group, error) {
	g, err := addGroup(name, nil)
	if err != nil {
		return nil, err
	}

	g.mu.Lock()
	defer g.mu.Unlock()

	perms, err := getPermission(g.description, c.Credentials())
	if err != nil {
		return nil, err
	}

	c.SetPermissions(perms)

	if !perms.Op && g.locked {
		return nil, userError("group is locked")
	}

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
		u := c.Credentials().Username
		c.pushClient(c.Id(), u, true)
		for _, cc := range clients {
			uu := cc.Credentials().Username
			err := c.pushClient(cc.Id(), uu, true)
			if err == ErrClientDead {
				return
			}
			cc.pushClient(c.Id(), u, true)
		}
	}(g.getClientsUnlocked(c))

	return g, nil
}

func delClient(c client) {
	g := c.Group()
	if g == nil {
		return
	}
	g.mu.Lock()
	defer g.mu.Unlock()

	if g.clients[c.Id()] != c {
		log.Printf("Deleting unknown client")
		return
	}
	delete(g.clients, c.Id())

	go func(clients []client) {
		for _, cc := range clients {
			cc.pushClient(c.Id(), c.Credentials().Username, false)
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

func (g *group) getClient(id string) client {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.getClientUnlocked(id)
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

func (g *group) shutdown(message string) {
	g.Range(func(c client) bool {
		cc, ok := c.(kickable)
		if ok {
			cc.kick(message)
		}
		return true
	})
}

const maxChatHistory = 20

func (g *group) clearChatHistory() {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.history = nil
}

func (g *group) addToChatHistory(id, user, kind, value string) {
	g.mu.Lock()
	defer g.mu.Unlock()

	if len(g.history) >= maxChatHistory {
		copy(g.history, g.history[1:])
		g.history = g.history[:len(g.history)-1]
	}
	g.history = append(g.history,
		chatHistoryEntry{id: id, user: user, kind: kind, value: value},
	)
}

func (g *group) getChatHistory() []chatHistoryEntry {
	g.mu.Lock()
	defer g.mu.Unlock()

	h := make([]chatHistoryEntry, len(g.history))
	copy(h, g.history)
	return h
}

func matchUser(user clientCredentials, users []clientCredentials) (bool, bool) {
	for _, u := range users {
		if u.Username == "" {
			if u.Password == "" || u.Password == user.Password {
				return true, true
			}
		} else if u.Username == user.Username {
			return true,
				(u.Password == "" || u.Password == user.Password)
		}
	}
	return false, false
}

type groupDescription struct {
	loadTime       time.Time           `json:"-"`
	modTime        time.Time           `json:"-"`
	fileSize       int64               `json:"-"`
	Redirect       string              `json:"redirect,omitempty"`
	Public         bool                `json:"public,omitempty"`
	MaxClients     int                 `json:"max-clients,omitempty"`
	AllowAnonymous bool                `json:"allow-anonymous,omitempty"`
	AllowRecording bool                `json:"allow-recording,omitempty"`
	Op             []clientCredentials `json:"op,omitempty"`
	Presenter      []clientCredentials `json:"presenter,omitempty"`
	Other          []clientCredentials `json:"other,omitempty"`
}

func descriptionChanged(name string, old *groupDescription) (bool, error) {
	fi, err := os.Stat(filepath.Join(groupsDir, name+".json"))
	if err != nil {
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

func getPermission(desc *groupDescription, creds clientCredentials) (clientPermissions, error) {
	var p clientPermissions
	if !desc.AllowAnonymous && creds.Username == "" {
		return p, userError("anonymous users not allowed in this group, please choose a username")
	}
	if found, good := matchUser(creds, desc.Op); found {
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
	if found, good := matchUser(creds, desc.Presenter); found {
		if good {
			p.Present = true
			return p, nil
		}
		return p, userError("not authorised")
	}
	if found, good := matchUser(creds, desc.Other); found {
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
	rangeGroups(func(g *group) bool {
		if g.Public() {
			gs = append(gs, publicGroup{
				Name:        g.name,
				ClientCount: len(g.clients),
			})
		}
		return true
	})
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
