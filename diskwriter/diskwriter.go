package diskwriter

import (
	crand "crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/at-wat/ebml-go/webm"
	"github.com/pion/rtp"
	"github.com/pion/rtp/codecs"
	"github.com/pion/webrtc/v3/pkg/media/samplebuilder"

	"github.com/jech/galene/conn"
	"github.com/jech/galene/group"
)

var Directory string

type Client struct {
	group *group.Group
	id    string

	mu     sync.Mutex
	down   map[string]*diskConn
	closed bool
}

func newId() string {
	b := make([]byte, 16)
	crand.Read(b)
	return hex.EncodeToString(b)
}

func New(g *group.Group) *Client {
	return &Client{group: g, id: newId()}
}

func (client *Client) Group() *group.Group {
	return client.group
}

func (client *Client) Id() string {
	return client.id
}

func (client *Client) Username() string {
	return "RECORDING"
}

func (client *Client) Challenge(group string, cred group.ClientCredentials) bool {
	return true
}

func (client *Client) OverridePermissions(g *group.Group) bool {
	return true
}

func (client *Client) SetPermissions(perms group.ClientPermissions) {
	return
}

func (client *Client) Permissions() group.ClientPermissions {
	return group.ClientPermissions{}
}

func (client *Client) PushClient(id, username string, permissions group.ClientPermissions, kind string) error {
	return nil
}

func (client *Client) Close() error {
	client.mu.Lock()
	defer client.mu.Unlock()

	for _, down := range client.down {
		down.Close()
	}
	client.down = nil
	client.closed = true
	return nil
}

func (client *Client) Kick(id, user, message string) error {
	err := client.Close()
	group.DelClient(client)
	return err
}

func (client *Client) PushConn(g *group.Group, id string, up conn.Up, tracks []conn.UpTrack, replace string) error {
	if client.group != g {
		return nil
	}

	client.mu.Lock()
	defer client.mu.Unlock()

	if client.closed {
		return errors.New("disk client is closed")
	}

	if replace != "" {
		rp := client.down[replace]
		if rp != nil {
			rp.Close()
			delete(client.down, replace)
		} else {
			log.Printf("Disk writer: replacing unknown connection")
		}
	}

	old := client.down[id]
	if old != nil {
		old.Close()
		delete(client.down, id)
	}

	if up == nil {
		return nil
	}

	directory := filepath.Join(Directory, client.group.Name())
	err := os.MkdirAll(directory, 0700)
	if err != nil {
		g.WallOps("Write to disk: " + err.Error())
		return err
	}

	if client.down == nil {
		client.down = make(map[string]*diskConn)
	}

	down, err := newDiskConn(client, directory, up, tracks)
	if err != nil {
		g.WallOps("Write to disk: " + err.Error())
		return err
	}

	client.down[up.Id()] = down
	return nil
}

type diskConn struct {
	client    *Client
	directory string
	username  string
	hasVideo  bool

	mu            sync.Mutex
	file          *os.File
	remote        conn.Up
	tracks        []*diskTrack
	width, height uint32
	lastWarning   time.Time
}

// called locked
func (conn *diskConn) warn(message string) {
	now := time.Now()
	if now.Sub(conn.lastWarning) < 10*time.Second {
		return
	}
	log.Println(message)
	conn.client.group.WallOps(message)
	conn.lastWarning = now
}

// called locked
func (conn *diskConn) reopen() error {
	for _, t := range conn.tracks {
		if t.writer != nil {
			t.writer.Close()
			t.writer = nil
		}
	}
	conn.file = nil

	file, err := openDiskFile(conn.directory, conn.username)
	if err != nil {
		return err
	}

	conn.file = file
	return nil
}

func (conn *diskConn) Close() error {
	conn.remote.DelLocal(conn)

	conn.mu.Lock()
	tracks := make([]*diskTrack, 0, len(conn.tracks))
	for _, t := range conn.tracks {
		if t.writer != nil {
			t.writer.Close()
			t.writer = nil
		}
		tracks = append(tracks, t)
	}
	conn.mu.Unlock()

	for _, t := range tracks {
		t.remote.DelLocal(t)
	}
	return nil
}

func openDiskFile(directory, username string) (*os.File, error) {
	filenameFormat := "2006-01-02T15:04:05.000"
	if runtime.GOOS == "windows" {
		filenameFormat = "2006-01-02T15-04-05-000"
	}

	filename := time.Now().Format(filenameFormat)
	if username != "" {
		filename = filename + "-" + username
	}
	for counter := 0; counter < 100; counter++ {
		var fn string
		if counter == 0 {
			fn = fmt.Sprintf("%v.webm", filename)
		} else {
			fn = fmt.Sprintf("%v-%02d.webm", filename, counter)
		}

		fn = filepath.Join(directory, fn)
		f, err := os.OpenFile(
			fn, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600,
		)
		if err == nil {
			return f, nil
		} else if !os.IsExist(err) {
			return nil, err
		}
	}
	return nil, errors.New("couldn't create file")
}

type diskTrack struct {
	remote conn.UpTrack
	conn   *diskConn

	writer  webm.BlockWriteCloser
	builder *samplebuilder.SampleBuilder

	// bit 32 is a boolean indicating that the origin is valid
	origin uint64

	lastKf  uint32
	savedKf *rtp.Packet
}

func newDiskConn(client *Client, directory string, up conn.Up, remoteTracks []conn.UpTrack) (*diskConn, error) {
	_, username := up.User()
	conn := diskConn{
		client:    client,
		directory: directory,
		username:  username,
		tracks:    make([]*diskTrack, 0, len(remoteTracks)),
		remote:    up,
	}
	for _, remote := range remoteTracks {
		var builder *samplebuilder.SampleBuilder
		codec := remote.Codec()
		switch strings.ToLower(codec.MimeType) {
		case "audio/opus":
			builder = samplebuilder.New(
				16, &codecs.OpusPacket{}, codec.ClockRate,
				samplebuilder.WithPartitionHeadChecker(
					&codecs.OpusPartitionHeadChecker{},
				),
			)
		case "video/vp8":
			if conn.hasVideo {
				return nil, errors.New("multiple video tracks not supported")
			}
			builder = samplebuilder.New(
				128, &codecs.VP8Packet{}, codec.ClockRate,
				samplebuilder.WithPartitionHeadChecker(
					&codecs.VP8PartitionHeadChecker{},
				),
			)
			conn.hasVideo = true
		case "video/vp9":
			if conn.hasVideo {
				return nil, errors.New("multiple video tracks not supported")
			}
			builder = samplebuilder.New(
				128, &codecs.VP9Packet{}, codec.ClockRate,
				samplebuilder.WithPartitionHeadChecker(
					&codecs.VP9PartitionHeadChecker{},
				),
			)
			conn.hasVideo = true
		default:
			client.group.WallOps(
				"Cannot record codec " + codec.MimeType,
			)
			continue
		}
		track := &diskTrack{
			remote:  remote,
			builder: builder,
			conn:    &conn,
		}
		conn.tracks = append(conn.tracks, track)
	}

	// Only do this after all tracks have been added to conn, to avoid
	// racing on hasVideo.
	for _, t := range conn.tracks {
		err := t.remote.AddLocal(t)
		if err != nil {
			log.Printf("Couldn't add disk track: %v", err)
			conn.warn("Couldn't add disk track: " + err.Error())
		}
	}
	err := up.AddLocal(&conn)
	if err != nil {
		return nil, err
	}

	return &conn, nil
}

func (t *diskTrack) SetTimeOffset(ntp uint64, rtp uint32) {
}

func (t *diskTrack) SetCname(string) {
}

func clonePacket(packet *rtp.Packet) *rtp.Packet {
	buf, err := packet.Marshal()
	if err != nil {
		return nil
	}
	var p rtp.Packet
	err = p.Unmarshal(buf)
	if err != nil {
		return nil
	}
	return &p
}

func isKeyframe(codec string, data []byte) bool {
	switch strings.ToLower(codec) {
	case "video/vp8":
		if len(data) < 1 {
			return false
		}
		return (data[0] & 0x1) == 0
	case "video/vp9":
		if len(data) < 1 {
			return false
		}
		if data[0]&0xC0 != 0x80 {
			return false
		}
		profile := (data[0] >> 4) & 0x3
		if profile != 3 {
			return (data[0] & 0xC) == 0
		}
		return (data[0] & 0x6) == 0
	default:
		panic("Eek!")
	}
}

func keyframeDimensions(codec string, data []byte, packet *rtp.Packet) (uint32, uint32) {
	switch strings.ToLower(codec) {
	case "video/vp8":
		if len(data) < 10 {
			return 0, 0
		}
		raw := uint32(data[6]) | uint32(data[7])<<8 |
			uint32(data[8])<<16 | uint32(data[9])<<24
		width := raw & 0x3FFF
		height := (raw >> 16) & 0x3FFF
		return width, height
	case "video/vp9":
		if packet == nil {
			return 0, 0
		}
		var vp9 codecs.VP9Packet
		_, err := vp9.Unmarshal(packet.Payload)
		if err != nil {
			return 0, 0
		}
		if !vp9.V {
			return 0, 0
		}
		w := uint32(0)
		h := uint32(0)
		for i := range vp9.Width {
			if i >= len(vp9.Height) {
				break
			}
			if w < uint32(vp9.Width[i]) {
				w = uint32(vp9.Width[i])
			}
			if h < uint32(vp9.Height[i]) {
				h = uint32(vp9.Height[i])
			}
		}
		return w, h
	default:
		return 0, 0
	}
}

func (t *diskTrack) WriteRTP(packet *rtp.Packet) error {
	// since we call initWriter, we take the connection lock for simplicity.
	t.conn.mu.Lock()
	defer t.conn.mu.Unlock()

	if t.builder == nil {
		return nil
	}

	codec := t.remote.Codec()

	p := clonePacket(packet)
	if p == nil {
		return nil
	}

	if strings.ToLower(codec.MimeType) == "video/vp9" {
		var vp9 codecs.VP9Packet
		_, err := vp9.Unmarshal(p.Payload)
		if err == nil && vp9.B && len(vp9.Payload) >= 1 {
			profile := (vp9.Payload[0] >> 4) & 0x3
			kf := false
			if profile != 3 {
				kf = (vp9.Payload[0] & 0xC) == 0
			} else {
				kf = (vp9.Payload[0] & 0x6) == 0
			}
			if kf {
				t.savedKf = p
			}
		}
	}

	kfNeeded := false

	t.builder.Push(p)

	for {
		sample, ts := t.builder.PopWithTimestamp()
		if sample == nil {
			if kfNeeded {
				return conn.ErrKeyframeNeeded
			}
			return nil
		}

		keyframe := true

		switch strings.ToLower(codec.MimeType) {
		case "video/vp8", "video/vp9":
			keyframe = isKeyframe(codec.MimeType, sample.Data)
			if keyframe {
				err := t.conn.initWriter(
					keyframeDimensions(
						codec.MimeType, sample.Data,
						t.savedKf,
					),
				)
				if err != nil {
					t.conn.warn(
						"Write to disk " + err.Error(),
					)
					return err
				}
				t.lastKf = ts
			} else if t.writer != nil {
				// Request a keyframe every 4s
				delta := ts - t.lastKf
				if (delta&0x80000000) != 0 || delta > 4*90000 {
					kfNeeded = true
				}
			}
		default:
			if t.writer == nil {
				if !t.conn.hasVideo {
					err := t.conn.initWriter(0, 0)
					if err != nil {
						t.conn.warn(
							"Write to disk " +
								err.Error(),
						)
						return err
					}
				}
			}
		}

		if t.writer == nil {
			if !keyframe {
				return conn.ErrKeyframeNeeded
			}
			return nil
		}

		if t.origin == 0 {
			t.origin = uint64(ts) | (1 << 32)
		}
		ts -= uint32(t.origin)

		tm := ts / (t.remote.Codec().ClockRate / 1000)
		_, err := t.writer.Write(keyframe, int64(tm), sample.Data)
		if err != nil {
			return err
		}
	}
}

// called locked
func (conn *diskConn) initWriter(width, height uint32) error {
	if conn.file != nil && width == conn.width && height == conn.height {
		return nil
	}
	var entries []webm.TrackEntry
	for i, t := range conn.tracks {
		var entry webm.TrackEntry
		codec := t.remote.Codec()
		switch strings.ToLower(codec.MimeType) {
		case "audio/opus":
			entry = webm.TrackEntry{
				Name:        "Audio",
				TrackNumber: uint64(i + 1),
				CodecID:     "A_OPUS",
				TrackType:   2,
				Audio: &webm.Audio{
					SamplingFrequency: float64(codec.ClockRate),
					Channels:          uint64(codec.Channels),
				},
			}
		case "video/vp8":
			entry = webm.TrackEntry{
				Name:        "Video",
				TrackNumber: uint64(i + 1),
				CodecID:     "V_VP8",
				TrackType:   1,
				Video: &webm.Video{
					PixelWidth:  uint64(width),
					PixelHeight: uint64(height),
				},
			}
		case "video/vp9":
			entry = webm.TrackEntry{
				Name:        "Video",
				TrackNumber: uint64(i + 1),
				CodecID:     "V_VP9",
				TrackType:   1,
				Video: &webm.Video{
					PixelWidth:  uint64(width),
					PixelHeight: uint64(height),
				},
			}
		default:
			return errors.New("unknown track type")
		}
		entries = append(entries, entry)
	}

	err := conn.reopen()
	if err != nil {
		return err
	}

	writers, err := webm.NewSimpleBlockWriter(conn.file, entries)
	if err != nil {
		conn.file.Close()
		conn.file = nil
		return err
	}

	if len(writers) != len(conn.tracks) {
		conn.file.Close()
		conn.file = nil
		return errors.New("unexpected number of writers")
	}

	conn.width = width
	conn.height = height

	for i, t := range conn.tracks {
		t.writer = writers[i]
	}
	return nil
}

func (down *diskConn) GetMaxBitrate(now uint64) uint64 {
	return ^uint64(0)
}

func (t *diskTrack) Accumulate(bytes uint32) {
	return
}
