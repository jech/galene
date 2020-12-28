package group

import (
	"encoding/json"
	"errors"
	"log"
	"os"
	"path"
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

var ErrNotAuthorised = errors.New("not authorised")

type UserError string

func (err UserError) Error() string {
	return string(err)
}

type KickError struct {
	Id       string
	Username string
	Message  string
}

func (err KickError) Error() string {
	m := "kicked out"
	if err.Message != "" {
		m += "(" + err.Message + ")"
	}
	if err.Username != "" {
		m += " by " + err.Username
	}
	return m
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
	Value interface{}
}

const (
	MinBitrate = 200000
)

type Group struct {
	name string
	api  *webrtc.API

	mu          sync.Mutex
	description *description
	locked      *string
	clients     map[string]Client
	history     []ChatHistoryEntry
	timestamp   time.Time
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
}

func (g *Group) API() *webrtc.API {
	return g.api
}

func codecFromName(name string) (webrtc.RTPCodecCapability, error) {
	switch name {
	case "vp8":
		return webrtc.RTPCodecCapability{
			"video/VP8", 90000, 0,
			"",
			nil,
		}, nil
	case "vp9":
		return webrtc.RTPCodecCapability{
			"video/VP9", 90000, 0,
			"profile-id=2",
			nil,
		}, nil
	case "h264":
		return webrtc.RTPCodecCapability{
			"video/H264", 90000, 0,
			"level-asymmetry-allowed=1;packetization-mode=1;profile-level-id=42001f",
			nil,
		}, nil
	case "opus":
		return webrtc.RTPCodecCapability{
			"audio/opus", 48000, 2,
			"minptime=10;useinbandfec=1",
			nil,
		}, nil
	case "g722":
		return webrtc.RTPCodecCapability{
			"audio/G722", 8000, 1,
			"",
			nil,
		}, nil
	case "pcmu":
		return webrtc.RTPCodecCapability{
			"audio/PCMU", 8000, 1,
			"",
			nil,
		}, nil
	case "pcma":
		return webrtc.RTPCodecCapability{
			"audio/PCMA", 8000, 1,
			"",
			nil,
		}, nil
	default:
		return webrtc.RTPCodecCapability{}, errors.New("unknown codec")
	}
}

func payloadType(codec webrtc.RTPCodecCapability) (webrtc.PayloadType, error) {
	switch strings.ToLower(codec.MimeType) {
	case "video/vp8":
		return 96, nil
	case "video/vp9":
		return 98, nil
	case "video/h264":
		return 102, nil
	case "audio/opus":
		return 111, nil
	case "audio/g722":
		return 9, nil
	case "audio/pcmu":
		return 0, nil
	case "audio/pcma":
		return 8, nil
	default:
		return 0, errors.New("unknown codec")
	}
}

func APIFromCodecs(codecs []webrtc.RTPCodecCapability) *webrtc.API {
	s := webrtc.SettingEngine{}
	s.SetSRTPReplayProtectionWindow(512)
	if !UseMDNS {
		s.SetICEMulticastDNSMode(ice.MulticastDNSModeDisabled)
	}
	m := webrtc.MediaEngine{}

	for _, codec := range codecs {
		var tpe webrtc.RTPCodecType
		var fb []webrtc.RTCPFeedback
		if strings.HasPrefix(strings.ToLower(codec.MimeType), "video/") {
			tpe = webrtc.RTPCodecTypeVideo
			fb = []webrtc.RTCPFeedback{
				{"goog-remb", ""},
				{"nack", ""},
				{"nack", "pli"},
				{"ccm", "fir"},
			}
		} else if strings.HasPrefix(strings.ToLower(codec.MimeType), "audio/") {
			tpe = webrtc.RTPCodecTypeAudio
			fb = []webrtc.RTCPFeedback{}
		} else {
			continue
		}

		ptpe, err := payloadType(codec)
		if err != nil {
			log.Printf("%v", err)
			continue
		}
		m.RegisterCodec(
			webrtc.RTPCodecParameters{
				RTPCodecCapability: webrtc.RTPCodecCapability{
					MimeType:     codec.MimeType,
					ClockRate:    codec.ClockRate,
					Channels:     codec.Channels,
					SDPFmtpLine:  codec.SDPFmtpLine,
					RTCPFeedback: fb,
				},
				PayloadType: ptpe,
			},
			tpe,
		)
	}
	return webrtc.NewAPI(
		webrtc.WithSettingEngine(s),
		webrtc.WithMediaEngine(&m),
	)
}

func Add(name string, desc *description) (*Group, error) {
	if name == "" || strings.HasSuffix(name, "/") {
		return nil, UserError("illegal group name")
	}

	groups.mu.Lock()
	defer groups.mu.Unlock()

	if groups.groups == nil {
		groups.groups = make(map[string]*Group)
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

		names := desc.Codecs
		if len(names) == 0 {
			names = []string{"vp8", "opus"}
		}
		codecs := make([]webrtc.RTPCodecCapability, 0, len(names))
		for _, n := range names {
			codec, err := codecFromName(n)
			if err != nil {
				log.Printf("Codec %v: %v", n, err)
				continue
			}
			codecs = append(codecs, codec)
		}

		api := APIFromCodecs(codecs)

		g = &Group{
			name:        name,
			description: desc,
			clients:     make(map[string]Client),
			timestamp:   time.Now(),
			api:         api,
		}
		groups.groups[name] = g
		return g, nil
	}

	g.mu.Lock()
	defer g.mu.Unlock()

	if desc != nil {
		g.description = desc
		return g, nil
	}

	if time.Since(g.description.loadTime) > 5*time.Second {
		if descriptionChanged(name, g.description) {
			desc, err := GetDescription(name)
			if err != nil {
				if !os.IsNotExist(err) {
					log.Printf("Reading group %v: %v",
						name, err)
				}
				deleteUnlocked(g)
				return nil, err
			}
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

type SubGroup struct {
	Name    string
	Clients int
}

func GetSubGroups(parent string) []SubGroup {
	prefix := parent + "/"
	subgroups := make([]SubGroup, 0)

	Range(func(g *Group) bool {
		if strings.HasPrefix(g.name, prefix) {
			g.mu.Lock()
			count := len(g.clients)
			g.mu.Unlock()
			if count > 0 {
				subgroups = append(subgroups,
					SubGroup{g.name, count})
			}
		}
		return true
	})
	return subgroups
}

func Get(name string) *Group {
	groups.mu.Lock()
	defer groups.mu.Unlock()

	return groups.groups[name]
}

func Delete(name string) bool {
	groups.mu.Lock()
	defer groups.mu.Unlock()
	g := groups.groups[name]
	if g == nil {
		return false
	}

	g.mu.Lock()
	defer g.mu.Unlock()
	return deleteUnlocked(g)
}

// Called with both groups.mu and g.mu taken.
func deleteUnlocked(g *Group) bool {
	if len(g.clients) != 0 {
		return false
	}

	delete(groups.groups, g.name)
	return true
}

func Expire() {
	names := GetNames()
	now := time.Now()

	for _, name := range names {
		g := Get(name)
		if g == nil {
			continue
		}

		old := false

		g.mu.Lock()
		empty := len(g.clients) == 0
		if empty && !g.description.Public {
			age := now.Sub(g.timestamp)
			old = age > maxHistoryAge(g.description)
		}
		// We cannot take groups.mu at this point without a deadlock.
		g.mu.Unlock()

		if empty && old {
			// Delete will check if the group is still empty
			Delete(name)
		}
	}
}

func AddClient(group string, c Client) (*Group, error) {
	g, err := Add(group, nil)
	if err != nil {
		return nil, err
	}

	g.mu.Lock()
	defer g.mu.Unlock()

	if !c.OverridePermissions(g) {
		perms, err := g.description.GetPermission(group, c)
		if err != nil {
			return nil, err
		}

		c.SetPermissions(perms)

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
	g.timestamp = time.Now()

	go func(clients []Client) {
		u := c.Username()
		c.PushClient(c.Id(), u, true)
		for _, cc := range clients {
			uu := cc.Username()
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
	g.timestamp = time.Now()

	go func(clients []Client) {
		for _, cc := range clients {
			cc.PushClient(c.Id(), c.Username(), false)
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
			cc.Kick("", "", message)
		}
		return true
	})
}

type warner interface {
	Warn(oponly bool, message string) error
}

func (g *Group) WallOps(message string) {
	clients := g.GetClients(nil)
	for _, c := range clients {
		w, ok := c.(warner)
		if !ok {
			continue
		}
		err := w.Warn(true, message)
		if err != nil {
			log.Printf("WallOps: %v", err)
		}
	}
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

func (g *Group) AddToChatHistory(id, user string, time int64, kind string, value interface{}) {
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

func discardObsoleteHistory(h []ChatHistoryEntry, duration time.Duration) []ChatHistoryEntry {
	i := 0
	for i < len(h) {
		if time.Since(FromJSTime(h[i].Time)) <= duration {
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

	g.history = discardObsoleteHistory(
		g.history, maxHistoryAge(g.description),
	)

	h := make([]ChatHistoryEntry, len(g.history))
	copy(h, g.history)
	return h
}

func matchClient(group string, c Challengeable, users []ClientCredentials) (bool, bool) {
	for _, u := range users {
		if u.Username == "" {
			if c.Challenge(group, u) {
				return true, true
			}
		} else if u.Username == c.Username() {
			if c.Challenge(group, u) {
				return true, true
			} else {
				return true, false
			}
		}
	}
	return false, false
}

type description struct {
	fileName       string              `json:"-"`
	loadTime       time.Time           `json:"-"`
	modTime        time.Time           `json:"-"`
	fileSize       int64               `json:"-"`
	Description    string              `json:"description,omitempty"`
	Redirect       string              `json:"redirect,omitempty"`
	Public         bool                `json:"public,omitempty"`
	MaxClients     int                 `json:"max-clients,omitempty"`
	MaxHistoryAge  int                 `json:"max-history-age,omitempty"`
	AllowAnonymous bool                `json:"allow-anonymous,omitempty"`
	AllowRecording bool                `json:"allow-recording,omitempty"`
	AllowSubgroups bool                `json:"allow-subgroups,omitempty"`
	Op             []ClientCredentials `json:"op,omitempty"`
	Presenter      []ClientCredentials `json:"presenter,omitempty"`
	Other          []ClientCredentials `json:"other,omitempty"`
	Codecs         []string            `json:"codecs,omitempty"`
}

const DefaultMaxHistoryAge = 4 * time.Hour

func maxHistoryAge(desc *description) time.Duration {
	if desc.MaxHistoryAge != 0 {
		return time.Duration(desc.MaxHistoryAge) * time.Second
	}
	return DefaultMaxHistoryAge
}

func openDescriptionFile(name string) (*os.File, string, bool, error) {
	isParent := false
	for name != "" {
		fileName := filepath.Join(
			Directory, path.Clean("/"+name)+".json",
		)
		r, err := os.Open(fileName)
		if !os.IsNotExist(err) {
			return r, fileName, isParent, err
		}
		isParent = true
		name, _ = path.Split(name)
		name = strings.TrimRight(name, "/")
	}
	return nil, "", false, os.ErrNotExist
}

func statDescriptionFile(name string) (os.FileInfo, string, bool, error) {
	isParent := false
	for name != "" {
		fileName := filepath.Join(
			Directory, path.Clean("/"+name)+".json",
		)
		fi, err := os.Stat(fileName)
		if !os.IsNotExist(err) {
			return fi, fileName, isParent, err
		}
		isParent = true
		name, _ = path.Split(name)
		name = strings.TrimRight(name, "/")
	}
	return nil, "", false, os.ErrNotExist
}

// descriptionChanged returns true if a group's description may have
// changed since it was last read.
func descriptionChanged(name string, desc *description) bool {
	fi, fileName, _, err := statDescriptionFile(name)
	if err != nil || fileName != desc.fileName {
		return true
	}

	if fi.Size() != desc.fileSize || fi.ModTime() != desc.modTime {
		return true
	}
	return false
}

func GetDescription(name string) (*description, error) {
	r, fileName, isParent, err := openDescriptionFile(name)
	if err != nil {
		return nil, err
	}
	defer r.Close()

	var desc description

	fi, err := r.Stat()
	if err != nil {
		return nil, err
	}

	d := json.NewDecoder(r)
	err = d.Decode(&desc)
	if err != nil {
		return nil, err
	}
	if isParent {
		if !desc.AllowSubgroups {
			return nil, os.ErrNotExist
		}
		desc.Public = false
		desc.Description = ""
	}

	desc.fileName = fileName
	desc.fileSize = fi.Size()
	desc.modTime = fi.ModTime()
	desc.loadTime = time.Now()

	return &desc, nil
}

func (desc *description) GetPermission(group string, c Challengeable) (ClientPermissions, error) {
	var p ClientPermissions
	if !desc.AllowAnonymous && c.Username() == "" {
		return p, UserError("anonymous users not allowed in this group, please choose a username")
	}
	if found, good := matchClient(group, c, desc.Op); found {
		if good {
			p.Op = true
			p.Present = true
			if desc.AllowRecording {
				p.Record = true
			}
			return p, nil
		}
		return p, ErrNotAuthorised
	}
	if found, good := matchClient(group, c, desc.Presenter); found {
		if good {
			p.Present = true
			return p, nil
		}
		return p, ErrNotAuthorised
	}
	if found, good := matchClient(group, c, desc.Other); found {
		if good {
			return p, nil
		}
		return p, ErrNotAuthorised
	}
	return p, ErrNotAuthorised
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
