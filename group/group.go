// Copyright (c) 2020 by Juliusz Chroboczek.

// This is not open source software.  Copy it, and I'll break into your
// house and tell your three year-old that Santa doesn't exist.

package group

import (
	"encoding/json"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/pion/ice/v2"
	"github.com/pion/webrtc/v3"
)

var Directory string
var UseMDNS bool

type UserError string

func (err UserError) Error() string {
	return string(err)
}

type ProtocolError string

func (err ProtocolError) Error() string {
	return string(err)
}

var IceFilename string

var iceConf webrtc.Configuration
var iceOnce sync.Once

func IceConfiguration() webrtc.Configuration {
	iceOnce.Do(func() {
		var iceServers []webrtc.ICEServer
		file, err := os.Open(IceFilename)
		if err != nil {
			log.Printf("Open %v: %v", IceFilename, err)
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

type ChatHistoryEntry struct {
	Id    string
	User  string
	Time  int64
	Kind  string
	Value string
}

const (
	MinBitrate = 200000
)

type Group struct {
	name string

	mu          sync.Mutex
	description *description
	// indicates that the group no longer exists, but it still has clients
	dead    bool
	locked  *string
	clients map[string]Client
	history []ChatHistoryEntry
}

func (g *Group) Name() string {
	return g.name
}

func (g *Group) Locked() (bool, string) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.locked != nil {
		return true, *g.locked
	} else {
		return false, ""
	}
}

func (g *Group) SetLocked(locked bool, message string) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if locked {
		g.locked = &message
	} else {
		g.locked = nil
	}
}

func (g *Group) Public() bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.description.Public
}

func (g *Group) Redirect() string {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.description.Redirect
}

func (g *Group) AllowRecording() bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.description.AllowRecording
}

var groups struct {
	mu     sync.Mutex
	groups map[string]*Group
	api    *webrtc.API
}

func (g *Group) API() *webrtc.API {
	return groups.api
}

func Add(name string, desc *description) (*Group, error) {
	groups.mu.Lock()
	defer groups.mu.Unlock()

	if groups.groups == nil {
		groups.groups = make(map[string]*Group)
		s := webrtc.SettingEngine{}
		s.SetSRTPReplayProtectionWindow(512)
		if !UseMDNS {
			s.SetICEMulticastDNSMode(ice.MulticastDNSModeDisabled)
		}
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
			desc, err = GetDescription(name)
			if err != nil {
				return nil, err
			}
		}
		g = &Group{
			name:        name,
			description: desc,
			clients:     make(map[string]Client),
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
			desc, err := GetDescription(name)
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

func Range(f func(g *Group) bool) {
	groups.mu.Lock()
	defer groups.mu.Unlock()

	for _, g := range groups.groups {
		ok := f(g)
		if !ok {
			break
		}
	}
}

func GetNames() []string {
	names := make([]string, 0)

	Range(func(g *Group) bool {
		names = append(names, g.name)
		return true
	})
	return names
}

func Get(name string) *Group {
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

func AddClient(name string, c Client) (*Group, error) {
	g, err := Add(name, nil)
	if err != nil {
		return nil, err
	}

	override := c.OverridePermissions(g)

	g.mu.Lock()
	defer g.mu.Unlock()

	perms, err := g.description.GetPermission(c.Credentials())
	if !override && err != nil {
		return nil, err
	}

	c.SetPermissions(perms)

	if !override {
		if !perms.Op && g.locked != nil {
			m := *g.locked
			if m == "" {
				m = "group is locked"
			}
			return nil, UserError(m)
		}

		if !perms.Op && g.description.MaxClients > 0 {
			if len(g.clients) >= g.description.MaxClients {
				return nil, UserError("too many users")
			}
		}
	}

	if g.clients[c.Id()] != nil {
		return nil, ProtocolError("duplicate client id")
	}

	g.clients[c.Id()] = c

	go func(clients []Client) {
		u := c.Credentials().Username
		c.PushClient(c.Id(), u, true)
		for _, cc := range clients {
			uu := cc.Credentials().Username
			c.PushClient(cc.Id(), uu, true)
			cc.PushClient(c.Id(), u, true)
		}
	}(g.getClientsUnlocked(c))

	return g, nil
}

func DelClient(c Client) {
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

	go func(clients []Client) {
		for _, cc := range clients {
			cc.PushClient(c.Id(), c.Credentials().Username, false)
		}
	}(g.getClientsUnlocked(nil))
}

func (g *Group) GetClients(except Client) []Client {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.getClientsUnlocked(except)
}

func (g *Group) getClientsUnlocked(except Client) []Client {
	clients := make([]Client, 0, len(g.clients))
	for _, c := range g.clients {
		if c != except {
			clients = append(clients, c)
		}
	}
	return clients
}

func (g *Group) GetClient(id string) Client {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.getClientUnlocked(id)
}

func (g *Group) getClientUnlocked(id string) Client {
	for idd, c := range g.clients {
		if idd == id {
			return c
		}
	}
	return nil
}

func (g *Group) Range(f func(c Client) bool) {
	g.mu.Lock()
	defer g.mu.Unlock()
	for _, c := range g.clients {
		ok := f(c)
		if !ok {
			break
		}
	}
}

func (g *Group) Shutdown(message string) {
	g.Range(func(c Client) bool {
		cc, ok := c.(Kickable)
		if ok {
			cc.Kick(message)
		}
		return true
	})
}

func FromJSTime(tm int64) time.Time {
	if tm == 0 {
		return time.Time{}
	}
	return time.Unix(int64(tm)/1000, (int64(tm)%1000)*1000000)
}

func ToJSTime(tm time.Time) int64 {
	return int64((tm.Sub(time.Unix(0, 0)) + time.Millisecond/2) /
		time.Millisecond)
}

const maxChatHistory = 50

func (g *Group) ClearChatHistory() {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.history = nil
}

func (g *Group) AddToChatHistory(id, user string, time int64, kind, value string) {
	g.mu.Lock()
	defer g.mu.Unlock()

	if len(g.history) >= maxChatHistory {
		copy(g.history, g.history[1:])
		g.history = g.history[:len(g.history)-1]
	}
	g.history = append(g.history,
		ChatHistoryEntry{Id: id, User: user, Time: time, Kind: kind, Value: value},
	)
}

func discardObsoleteHistory(h []ChatHistoryEntry, seconds int) []ChatHistoryEntry {
	now := time.Now()
	d := 4 * time.Hour
	if seconds > 0 {
		d = time.Duration(seconds) * time.Second
	}

	i := 0
	for i < len(h) {
		log.Println(h[i].Time, FromJSTime(h[i].Time), now.Sub(FromJSTime(h[i].Time)))
		if now.Sub(FromJSTime(h[i].Time)) <= d {
			break
		}
		i++
	}
	if i > 0 {
		copy(h, h[i:])
		h = h[:len(h)-i]
	}
	return h
}

func (g *Group) GetChatHistory() []ChatHistoryEntry {
	g.mu.Lock()
	defer g.mu.Unlock()

	g.history = discardObsoleteHistory(g.history, g.description.MaxHistoryAge)

	h := make([]ChatHistoryEntry, len(g.history))
	copy(h, g.history)
	return h
}

func matchUser(user ClientCredentials, users []ClientCredentials) (bool, bool) {
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

type description struct {
	loadTime       time.Time           `json:"-"`
	modTime        time.Time           `json:"-"`
	fileSize       int64               `json:"-"`
	Description    string              `json:"description,omitempty"`
	Redirect       string              `json:"redirect,omitempty"`
	Public         bool                `json:"public,omitempty"`
	MaxClients     int                 `json:"max-clients,omitempty"`
	MaxHistoryAge  int                 `json:"max-history-age",omitempty`
	AllowAnonymous bool                `json:"allow-anonymous,omitempty"`
	AllowRecording bool                `json:"allow-recording,omitempty"`
	Op             []ClientCredentials `json:"op,omitempty"`
	Presenter      []ClientCredentials `json:"presenter,omitempty"`
	Other          []ClientCredentials `json:"other,omitempty"`
}

func descriptionChanged(name string, old *description) (bool, error) {
	fi, err := os.Stat(filepath.Join(Directory, name+".json"))
	if err != nil {
		return false, err
	}
	if fi.Size() != old.fileSize || fi.ModTime() != old.modTime {
		return true, err
	}
	return false, err
}

func GetDescription(name string) (*description, error) {
	r, err := os.Open(filepath.Join(Directory, name+".json"))
	if err != nil {
		return nil, err
	}
	defer r.Close()

	var desc description

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

func (desc *description) GetPermission(creds ClientCredentials) (ClientPermissions, error) {
	var p ClientPermissions
	if !desc.AllowAnonymous && creds.Username == "" {
		return p, UserError("anonymous users not allowed in this group, please choose a username")
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
		return p, UserError("not authorised")
	}
	if found, good := matchUser(creds, desc.Presenter); found {
		if good {
			p.Present = true
			return p, nil
		}
		return p, UserError("not authorised")
	}
	if found, good := matchUser(creds, desc.Other); found {
		if good {
			return p, nil
		}
		return p, UserError("not authorised")
	}
	return p, UserError("not authorised")
}

type Public struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	ClientCount int    `json:"clientCount"`
}

func GetPublic() []Public {
	gs := make([]Public, 0)
	Range(func(g *Group) bool {
		if g.Public() {
			gs = append(gs, Public{
				Name:        g.name,
				Description: g.description.Description,
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

func ReadPublicGroups() {
	dir, err := os.Open(Directory)
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
		desc, err := GetDescription(name)
		if err != nil {
			if !os.IsNotExist(err) {
				log.Printf("Reading group %v: %v", name, err)
			}
			continue
		}
		if desc.Public {
			Add(name, desc)
		}
	}
}
