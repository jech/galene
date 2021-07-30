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

	"github.com/at-wat/ebml-go/mkvcore"
	"github.com/at-wat/ebml-go/webm"
	"github.com/pion/rtp"
	"github.com/pion/rtp/codecs"
	"github.com/pion/webrtc/v3/pkg/media"

	"github.com/jech/samplebuilder"

	gcodecs "github.com/jech/galene/codecs"
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

func (client *Client) SetPermissions(perms group.ClientPermissions) {
	return
}

func (client *Client) Permissions() group.ClientPermissions {
	return group.ClientPermissions{
		System: true,
	}
}

func (client *Client) Status() map[string]interface{} {
	return nil
}

func (client *Client) PushClient(group, kind, id, username string, permissions group.ClientPermissions, status map[string]interface{}) error {
	return nil
}

func (client *Client) RequestConns(target group.Client, g *group.Group, id string) error {
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

func (client *Client) Joined(group, kind string) error {
	return nil
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
func (conn *diskConn) reopen(extension string) error {
	for _, t := range conn.tracks {
		if t.writer != nil {
			t.writeBuffered(true)
			t.writer.Close()
			t.writer = nil
		}
	}
	conn.file = nil

	file, err := openDiskFile(conn.directory, conn.username, extension)
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
			t.writeBuffered(true)
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

func openDiskFile(directory, username, extension string) (*os.File, error) {
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
			fn = fmt.Sprintf("%v.%v", filename, extension)
		} else {
			fn = fmt.Sprintf("%v-%02d.%v",
				filename, counter, extension,
			)
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

type maybeUint32 uint64

const none maybeUint32 = 0

func some(value uint32) maybeUint32 {
	return maybeUint32(uint64(1<<32) | uint64(value))
}

func valid(m maybeUint32) bool {
	return (m & (1 << 32)) != 0
}

func value(m maybeUint32) uint32 {
	return uint32(m)
}

type diskTrack struct {
	remote conn.UpTrack
	conn   *diskConn

	writer    mkvcore.BlockWriteCloser
	builder   *samplebuilder.SampleBuilder
	lastSeqno maybeUint32
	origin    maybeUint32

	kfRequested time.Time
	lastKf      time.Time
	savedKf     *rtp.Packet
}

func newDiskConn(client *Client, directory string, up conn.Up, remoteTracks []conn.UpTrack) (*diskConn, error) {
	var audio, video conn.UpTrack

	for _, remote := range remoteTracks {
		codec := remote.Codec().MimeType
		if strings.EqualFold(codec, "audio/opus") {
			if audio == nil {
				audio = remote
			} else {
				client.group.WallOps("Multiple audio tracks, recording just one")
			}
		} else if strings.EqualFold(codec, "video/vp8") ||
			strings.EqualFold(codec, "video/vp9") ||
			strings.EqualFold(codec, "video/h264") {
			if video == nil || video.Label() == "l" {
				video = remote
			} else if remote.Label() != "l" {
				client.group.WallOps("Multiple video tracks, recording just one")
			}
		} else {
			client.group.WallOps("Unknown codec, " + codec + ", not recording")
		}
	}

	if video == nil && audio == nil {
		return nil, errors.New("no usable tracks found")
	}

	tracks := make([]conn.UpTrack, 0, 2)
	if audio != nil {
		tracks = append(tracks, audio)
	}
	if video != nil {
		tracks = append(tracks, video)
	}

	_, username := up.User()
	conn := diskConn{
		client:    client,
		directory: directory,
		username:  username,
		tracks:    make([]*diskTrack, 0, len(tracks)),
		remote:    up,
	}

	truePartitionTailChecker := func(p *rtp.Packet) bool {
		return true
	}

	markerPartitionTailChecker := func(p *rtp.Packet) bool {
		return p.Marker
	}

	for _, remote := range tracks {
		var builder *samplebuilder.SampleBuilder
		codec := remote.Codec()
		if strings.EqualFold(codec.MimeType, "audio/opus") {
			builder = samplebuilder.New(
				16, &codecs.OpusPacket{}, codec.ClockRate,
				samplebuilder.WithPartitionHeadChecker(
					&codecs.OpusPartitionHeadChecker{},
				),
				samplebuilder.WithPartitionTailChecker(
					truePartitionTailChecker,
				),
			)
		} else if strings.EqualFold(codec.MimeType, "video/vp8") {
			builder = samplebuilder.New(
				128, &codecs.VP8Packet{}, codec.ClockRate,
				samplebuilder.WithPartitionHeadChecker(
					&codecs.VP8PartitionHeadChecker{},
				),
				samplebuilder.WithPartitionTailChecker(
					markerPartitionTailChecker,
				),
			)
			conn.hasVideo = true
		} else if strings.EqualFold(codec.MimeType, "video/vp9") {
			builder = samplebuilder.New(
				128, &codecs.VP9Packet{}, codec.ClockRate,
				samplebuilder.WithPartitionHeadChecker(
					&codecs.VP9PartitionHeadChecker{},
				),
				samplebuilder.WithPartitionTailChecker(
					markerPartitionTailChecker,
				),
			)
			conn.hasVideo = true
		} else if strings.EqualFold(codec.MimeType, "video/h264") {
			builder = samplebuilder.New(
				128, &codecs.H264Packet{}, codec.ClockRate,
				samplebuilder.WithPartitionHeadChecker(
					&codecs.H264PartitionHeadChecker{},
				),
				samplebuilder.WithPartitionTailChecker(
					markerPartitionTailChecker,
				),
			)
			conn.hasVideo = true
		} else {
			// this shouldn't happen
			return nil, errors.New(
				"cannot record codec " + codec.MimeType,
			)
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

func (t *diskTrack) Write(buf []byte) (int, error) {
	t.conn.mu.Lock()
	defer t.conn.mu.Unlock()

	if t.builder == nil {
		return 0, nil
	}

	// samplebuilder retains packets
	data := make([]byte, len(buf))
	copy(data, buf)
	p := new(rtp.Packet)
	err := p.Unmarshal(data)
	if err != nil {
		log.Printf("Diskwriter: %v", err)
		return 0, nil
	}

	if valid(t.lastSeqno) {
		lastSeqno := uint16(value(t.lastSeqno))
		if ((p.SequenceNumber - lastSeqno) & 0x8000) == 0 {
			count := p.SequenceNumber - lastSeqno
			if count > 0 && count < 128 {
				for i := lastSeqno + 1; i != p.SequenceNumber; i++ {
					// different buf each time
					buf := make([]byte, 1504)
					n := t.remote.GetPacket(i, buf, true)
					if n == 0 {
						continue
					}
					p := new(rtp.Packet)
					err := p.Unmarshal(buf)
					if err == nil {
						t.writeRTP(p)
					}
				}
			}
		}
	}

	t.lastSeqno = some(uint32(p.SequenceNumber))

	err = t.writeRTP(p)
	if err != nil {
		return 0, err
	}
	return len(buf), nil
}

// writeRTP writes the packet without doing any loss recovery.
// Called locked.
func (t *diskTrack) writeRTP(p *rtp.Packet) error {
	codec := t.remote.Codec().MimeType
	if len(codec) > 6 && strings.EqualFold(codec[:6], "video/") {
		kf, _ := gcodecs.Keyframe(codec, p)
		if kf {
			t.savedKf = p
		}
	}

	t.builder.Push(p)

	return t.writeBuffered(false)
}

// writeBuffered writes any buffered samples to disk.  If force is true,
// then samples will be flushed even if they are preceded by incomplete
// samples.
func (t *diskTrack) writeBuffered(force bool) error {
	codec := t.remote.Codec().MimeType

	for {
		var sample *media.Sample
		var ts uint32
		if !force {
			sample, ts = t.builder.PopWithTimestamp()
		} else {
			sample, ts = t.builder.ForcePopWithTimestamp()
		}
		if sample == nil {
			return nil
		}

		var keyframe bool
		if len(codec) > 6 && strings.EqualFold(codec[:6], "video/") {
			if t.savedKf == nil {
				keyframe = false
			} else {
				keyframe = (ts == t.savedKf.Timestamp)
			}

			if keyframe {
				err := t.conn.initWriter(
					gcodecs.KeyframeDimensions(
						codec, t.savedKf,
					),
				)
				if err != nil {
					t.conn.warn(
						"Write to disk " + err.Error(),
					)
					return err
				}
			}
		} else {
			keyframe = true
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

		now := time.Now()
		if keyframe {
			t.lastKf = now
		} else if t.writer == nil || now.Sub(t.lastKf) > 4*time.Second {
			if now.Sub(t.kfRequested) > time.Second {
				t.remote.RequestKeyframe()
				t.kfRequested = now
			}
			return nil
		}

		if t.writer == nil {
			continue
		}

		if !valid(t.origin) {
			t.origin = some(ts)
		}
		ts -= value(t.origin)

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
	isWebm := true
	var desc []mkvcore.TrackDescription
	for i, t := range conn.tracks {
		var entry webm.TrackEntry
		codec := t.remote.Codec()
		if strings.EqualFold(codec.MimeType, "audio/opus") {
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
		} else if strings.EqualFold(codec.MimeType, "video/vp8") {
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
		} else if strings.EqualFold(codec.MimeType, "video/vp9") {
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
		} else if strings.EqualFold(codec.MimeType, "video/h264") {
			entry = webm.TrackEntry{
				Name:        "Video",
				TrackNumber: uint64(i + 1),
				CodecID:     "V_MPEG4/ISO/AVC",
				TrackType:   1,
				Video: &webm.Video{
					PixelWidth:  uint64(width),
					PixelHeight: uint64(height),
				},
			}
			isWebm = false
		} else {
			return errors.New("unknown track type")
		}
		desc = append(desc,
			mkvcore.TrackDescription{
				TrackNumber: uint64(i + 1),
				TrackEntry:  entry,
			},
		)
	}

	extension := "webm"
	header := webm.DefaultEBMLHeader
	if !isWebm {
		extension = "mkv"
		h := *header
		h.DocType = "matroska"
		header = &h
	}

	err := conn.reopen(extension)
	if err != nil {
		return err
	}

	ws, err := mkvcore.NewSimpleBlockWriter(
		conn.file, desc,
		mkvcore.WithEBMLHeader(header),
		mkvcore.WithSegmentInfo(webm.DefaultSegmentInfo),
		mkvcore.WithBlockInterceptor(webm.DefaultBlockInterceptor),
	)
	if err != nil {
		conn.file.Close()
		conn.file = nil
		return err
	}

	if len(ws) != len(conn.tracks) {
		conn.file.Close()
		conn.file = nil
		return errors.New("unexpected number of writers")
	}

	conn.width = width
	conn.height = height

	for i, t := range conn.tracks {
		t.writer = ws[i]
	}
	return nil
}

func (t *diskTrack) GetMaxBitrate() (uint64, int, int) {
	return ^uint64(0), -1, -1
}
