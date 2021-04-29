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
var UDPMin, UDPMax uint16

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

	mu          sync.Mutex
	description *Description
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

func (g *Group) API() (*webrtc.API, error) {
	g.mu.Lock()
	codecs := g.description.Codecs
	g.mu.Unlock()

	return APIFromNames(codecs)
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
			"minptime=10;useinbandfec=1;stereo=1;sprop-stereo=1",
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

func APIFromCodecs(codecs []webrtc.RTPCodecCapability) (*webrtc.API, error) {
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
		err = m.RegisterCodec(
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
		if err != nil {
			log.Printf("%v", err)
			continue
		}
	}

	if UDPMin > 0 && UDPMax > 0 {
		s.SetEphemeralUDPPortRange(UDPMin, UDPMax)
	}

	return webrtc.NewAPI(
		webrtc.WithSettingEngine(s),
		webrtc.WithMediaEngine(&m),
	), nil
}

func APIFromNames(names []string) (*webrtc.API, error) {
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

	return APIFromCodecs(codecs)
}

func Add(name string, desc *Description) (*Group, error) {
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

		g = &Group{
			name:        name,
			description: desc,
			clients:     make(map[string]Client),
			timestamp:   time.Now(),
		}
		autoLockKick(g, g.getClientsUnlocked(nil))
		groups.groups[name] = g
		return g, nil
	}

	g.mu.Lock()
	defer g.mu.Unlock()

	if desc != nil {
		g.description = desc
	} else if !descriptionChanged(name, g.description) {
		return g, nil
	}

	desc, err = GetDescription(name)
	if err != nil {
		if !os.IsNotExist(err) {
			log.Printf("Reading group %v: %v", name, err)
		}
		deleteUnlocked(g)
		return nil, err
	}
	g.description = desc
	autoLockKick(g, g.getClientsUnlocked(nil))

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

	clients := g.getClientsUnlocked(nil)

	if !c.OverridePermissions(g) {
		perms, err := g.description.GetPermission(group, c)
		if err != nil {
			return nil, err
		}

		c.SetPermissions(perms)

		if !perms.Op {
			if g.locked != nil {
				m := *g.locked
				if m == "" {
					m = "this group is locked"
				}
				return nil, UserError(m)
			}
			if g.description.Autokick {
				ops := false
				for _, c := range clients {
					if c.Permissions().Op {
						ops = true
						break
					}
				}
				if !ops {
					return nil, UserError(
						"there are no operators " +
							"in this group",
					)
				}
			}
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

	id := c.Id()
	u := c.Username()
	p := c.Permissions()
	s := c.Status()
	c.PushClient(c.Id(), u, p, s, "add")
	for _, cc := range clients {
		c.PushClient(
			cc.Id(), cc.Username(), cc.Permissions(), cc.Status(),
			"add",
		)
		cc.PushClient(id, u, p, s, "add")
	}

	return g, nil
}

// called locked
func autoLockKick(g *Group, clients []Client) {
	if !(g.description.Autolock && g.locked == nil) &&
		!g.description.Autokick {
		return
	}
	for _, c := range clients {
		if c.Permissions().Op {
			return
		}
	}
	if g.description.Autolock && g.locked == nil {
		m := "this group is locked"
		g.locked = &m
	}

	if g.description.Autokick {
		go kickall(g, "there are no operators in this group")
	}
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

	clients := g.getClientsUnlocked(nil)

	go func(clients []Client) {
		for _, cc := range clients {
			cc.PushClient(c.Id(), c.Username(), c.Permissions(), c.Status(), "delete")
		}
	}(clients)

	autoLockKick(g, clients)
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

func kickall(g *Group, message string) {
	g.Range(func(c Client) bool {
		c.Kick("", "", message)
		return true
	})
}

func (g *Group) Shutdown(message string) {
	kickall(g, message)
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
	matched := false
	for _, u := range users {
		if u.Username == c.Username() {
			matched = true
			if c.Challenge(group, u) {
				return true, true
			}
		}
	}
	if matched {
		return true, false
	}

	for _, u := range users {
		if u.Username == "" {
			if c.Challenge(group, u) {
				return true, true
			}
		}
	}
	return false, false
}

// Type Description represents a group description together with some
// metadata about the JSON file it was deserialised from.
type Description struct {
	// The file this was deserialised from.  This is not necessarily
	// the name of the group, for example in case of a subgroup.
	FileName string `json:"-"`

	// The modtime and size of the file.  These are used to detect
	// when a file has changed on disk.
	modTime  time.Time `json:"-"`
	fileSize int64     `json:"-"`

	// A user-readable description of the group.
	Description string `json:"description,omitempty"`

	// A user-readable contact, typically an e-mail address.
	Contact string `json:"contact,omitempty"`

	// A user-readable comment.  Ignored by the server.
	Comment string `json:"comment,omitempty"`

	// Whether to display the group on the landing page.
	Public bool `json:"public,omitempty"`

	// A URL to redirect the group to.  If this is not empty, most
	// other fields are ignored.
	Redirect string `json:"redirect,omitempty"`

	// The maximum number of simultaneous clients.  Unlimited if 0.
	MaxClients int `json:"max-clients,omitempty"`

	// The time for which history entries are kept.
	MaxHistoryAge int `json:"max-history-age,omitempty"`

	// Whether users are allowed to log in with an empty username.
	AllowAnonymous bool `json:"allow-anonymous,omitempty"`

	// Whether recording is allowed.
	AllowRecording bool `json:"allow-recording,omitempty"`

	// Whether subgroups are created on the fly.
	AllowSubgroups bool `json:"allow-subgroups,omitempty"`

	// Whether to lock the group when the last op logs out.
	Autolock bool `json:"autolock,omitempty"`

	// Whether to kick all users when the last op logs out.
	Autokick bool `json:"autokick,omitempty"`

	// A list of logins for ops.
	Op []ClientCredentials `json:"op,omitempty"`

	// A list of logins for presenters.
	Presenter []ClientCredentials `json:"presenter,omitempty"`

	// A list of logins for non-presenting users.
	Other []ClientCredentials `json:"other,omitempty"`

	// Codec preferences.  If empty, a suitable default is chosen in
	// the APIFromNames function.
	Codecs []string `json:"codecs,omitempty"`
}

const DefaultMaxHistoryAge = 4 * time.Hour

func maxHistoryAge(desc *Description) time.Duration {
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
func descriptionChanged(name string, desc *Description) bool {
	fi, fileName, _, err := statDescriptionFile(name)
	if err != nil || fileName != desc.FileName {
		return true
	}

	if fi.Size() != desc.fileSize || fi.ModTime() != desc.modTime {
		return true
	}
	return false
}

func GetDescription(name string) (*Description, error) {
	r, fileName, isParent, err := openDescriptionFile(name)
	if err != nil {
		return nil, err
	}
	defer r.Close()

	var desc Description

	fi, err := r.Stat()
	if err != nil {
		return nil, err
	}

	d := json.NewDecoder(r)
	d.DisallowUnknownFields()
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

	desc.FileName = fileName
	desc.fileSize = fi.Size()
	desc.modTime = fi.ModTime()

	return &desc, nil
}

func (desc *Description) GetPermission(group string, c Challengeable) (ClientPermissions, error) {
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
	err := filepath.Walk(
		Directory,
		func(path string, fi os.FileInfo, err error) error {
			if err != nil {
				log.Printf("Group file %v: %v", path, err)
				return nil
			}
			if fi.IsDir() {
				return nil
			}
			filename, err := filepath.Rel(Directory, path)
			if err != nil {
				log.Printf("Group file %v: %v", path, err)
				return nil
			}
			if !strings.HasSuffix(filename, ".json") {
				log.Printf(
					"Unexpected extension for group file %v",
					path,
				)
				return nil
			}
			name := filename[:len(filename)-5]
			desc, err := GetDescription(name)
			if err != nil {
				log.Printf("Group file %v: %v", path, err)
				return nil
			}
			if desc.Public {
				Add(name, desc)
			}
			return nil
		},
	)

	if err != nil {
		log.Printf("Couldn't read groups: %v", err)
	}
}
