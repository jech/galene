// Copyright (c) 2020 by Juliusz Chroboczek.

// This is not open source software.  Copy it, and I'll break into your
// house and tell your three year-old that Santa doesn't exist.

package main

import (
	"encoding/json"
	"errors"
	"io"
	"log"
	"math"
	"math/bits"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"sfu/estimator"
	"sfu/jitter"
	"sfu/packetcache"
	"sfu/rtptime"

	"github.com/gorilla/websocket"
	"github.com/pion/rtcp"
	"github.com/pion/rtp"
	"github.com/pion/webrtc/v2"
)

var iceConf webrtc.Configuration
var iceOnce sync.Once

func iceConfiguration() webrtc.Configuration {
	iceOnce.Do(func() {
		var iceServers []webrtc.ICEServer
		file, err := os.Open(iceFilename)
		if err != nil {
			log.Printf("Open %v: %v", iceFilename, err)
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

type protocolError string

func (err protocolError) Error() string {
	return string(err)
}

type userError string

func (err userError) Error() string {
	return string(err)
}

func errorToWSCloseMessage(err error) (string, []byte) {
	var code int
	var text string
	switch e := err.(type) {
	case *websocket.CloseError:
		code = websocket.CloseNormalClosure
	case protocolError:
		code = websocket.CloseProtocolError
		text = string(e)
	case userError:
		code = websocket.CloseNormalClosure
		text = string(e)
	default:
		code = websocket.CloseInternalServerErr
	}
	return text, websocket.FormatCloseMessage(code, text)
}

func isWSNormalError(err error) bool {
	return websocket.IsCloseError(err,
		websocket.CloseNormalClosure,
		websocket.CloseGoingAway)
}

type webClient struct {
	group       *group
	id          string
	username    string
	permissions userPermission
	requested   map[string]uint32
	done        chan struct{}
	writeCh     chan interface{}
	writerDone  chan struct{}
	actionCh    chan interface{}

	mu   sync.Mutex
	down map[string]*rtpDownConnection
	up   map[string]*upConnection
}

func (c *webClient) getGroup() *group {
	return c.group
}

func (c *webClient) getId() string {
	return c.id
}

func (c *webClient) getUsername() string {
	return c.username
}

func (c *webClient) pushClient(id, username string, add bool) error {
	return c.write(clientMessage{
		Type:     "user",
		Id:       id,
		Username: username,
		Del:      !add,
	})
}

type rateMap map[string]uint32

func (v *rateMap) UnmarshalJSON(b []byte) error {
	var m map[string]interface{}

	err := json.Unmarshal(b, &m)
	if err != nil {
		return err
	}

	n := make(map[string]uint32, len(m))
	for k, w := range m {
		switch w := w.(type) {
		case bool:
			if w {
				n[k] = ^uint32(0)
			} else {
				n[k] = 0
			}
		case float64:
			if w < 0 || w >= float64(^uint32(0)) {
				return errors.New("overflow")
			}
			n[k] = uint32(w)
		default:
			return errors.New("unexpected type in JSON map")
		}
	}
	*v = n
	return nil
}

func (v rateMap) MarshalJSON() ([]byte, error) {
	m := make(map[string]interface{}, len(v))
	for k, w := range v {
		switch w {
		case 0:
			m[k] = false
		case ^uint32(0):
			m[k] = true
		default:
			m[k] = w
		}
	}
	return json.Marshal(m)
}

type clientMessage struct {
	Type        string                     `json:"type"`
	Id          string                     `json:"id,omitempty"`
	Username    string                     `json:"username,omitempty"`
	Password    string                     `json:"password,omitempty"`
	Permissions userPermission             `json:"permissions,omitempty"`
	Group       string                     `json:"group,omitempty"`
	Value       string                     `json:"value,omitempty"`
	Me          bool                       `json:"me,omitempty"`
	Offer       *webrtc.SessionDescription `json:"offer,omitempty"`
	Answer      *webrtc.SessionDescription `json:"answer,omitempty"`
	Candidate   *webrtc.ICECandidateInit   `json:"candidate,omitempty"`
	Labels      map[string]string          `json:"labels,omitempty"`
	Del         bool                       `json:"del,omitempty"`
	Request     rateMap                    `json:"request,omitempty"`
}

type closeMessage struct {
	data []byte
}

func startClient(conn *websocket.Conn) (err error) {
	var m clientMessage

	err = conn.SetReadDeadline(time.Now().Add(15 * time.Second))
	if err != nil {
		conn.Close()
		return
	}
	err = conn.ReadJSON(&m)
	if err != nil {
		conn.Close()
		return
	}
	err = conn.SetReadDeadline(time.Time{})
	if err != nil {
		conn.Close()
		return
	}

	if m.Type != "handshake" {
		conn.Close()
		return
	}

	if strings.ContainsRune(m.Username, ' ') {
		err = userError("don't put spaces in your username")
		return
	}

	c := &webClient{
		id:       m.Id,
		username: m.Username,
		actionCh: make(chan interface{}, 10),
		done:     make(chan struct{}),
	}

	defer close(c.done)

	c.writeCh = make(chan interface{}, 25)
	defer func() {
		if isWSNormalError(err) {
			err = nil
		} else {
			m, e := errorToWSCloseMessage(err)
			if m != "" {
				c.write(clientMessage{
					Type:  "error",
					Value: m,
				})
			}
			select {
			case c.writeCh <- closeMessage{e}:
			case <-c.writerDone:
			}
		}
		close(c.writeCh)
		c.writeCh = nil
	}()

	c.writerDone = make(chan struct{})
	go clientWriter(conn, c.writeCh, c.writerDone)

	g, err := addClient(m.Group, c, m.Username, m.Password)
	if err != nil {
		return
	}
	c.group = g
	defer delClient(c)

	return clientLoop(c, conn)
}

func getUpConn(c *webClient, id string) *upConnection {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.up == nil {
		return nil
	}
	conn := c.up[id]
	if conn == nil {
		return nil
	}
	return conn
}

func getUpConns(c *webClient) []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	up := make([]string, 0, len(c.up))
	for id := range c.up {
		up = append(up, id)
	}
	return up
}

func addUpConn(c *webClient, id string) (*upConnection, error) {
	pc, err := groups.api.NewPeerConnection(iceConfiguration())
	if err != nil {
		return nil, err
	}

	_, err = pc.AddTransceiverFromKind(webrtc.RTPCodecTypeAudio,
		webrtc.RtpTransceiverInit{
			Direction: webrtc.RTPTransceiverDirectionRecvonly,
		},
	)
	if err != nil {
		pc.Close()
		return nil, err
	}

	_, err = pc.AddTransceiverFromKind(webrtc.RTPCodecTypeVideo,
		webrtc.RtpTransceiverInit{
			Direction: webrtc.RTPTransceiverDirectionRecvonly,
		},
	)
	if err != nil {
		pc.Close()
		return nil, err
	}

	conn := &upConnection{id: id, pc: pc}

	c.mu.Lock()
	defer c.mu.Unlock()

	if c.up == nil {
		c.up = make(map[string]*upConnection)
	}
	if c.up[id] != nil || (c.down != nil && c.down[id] != nil) {
		conn.pc.Close()
		return nil, errors.New("Adding duplicate connection")
	}
	c.up[id] = conn

	pc.OnICECandidate(func(candidate *webrtc.ICECandidate) {
		sendICE(c, id, candidate)
	})

	pc.OnTrack(func(remote *webrtc.Track, receiver *webrtc.RTPReceiver) {
		c.mu.Lock()
		u, ok := c.up[id]
		if !ok {
			log.Printf("Unknown connection")
			c.mu.Unlock()
			return
		}

		mid := getUpMid(pc, remote)
		if mid == "" {
			log.Printf("Couldn't get track's mid")
			c.mu.Unlock()
			return
		}

		label, ok := u.labels[mid]
		if !ok {
			log.Printf("Couldn't get track's label")
			isvideo := remote.Kind() == webrtc.RTPCodecTypeVideo
			if isvideo {
				label = "video"
			} else {
				label = "audio"
			}
		}

		track := &upTrack{
			track:      remote,
			label:      label,
			cache:      packetcache.New(32),
			rate:       estimator.New(time.Second),
			jitter:     jitter.New(remote.Codec().ClockRate),
			maxBitrate: ^uint64(0),
			localCh:    make(chan localTrackAction, 2),
			writerDone: make(chan struct{}),
		}
		u.tracks = append(u.tracks, track)
		var tracks []*upTrack
		if u.complete() {
			tracks = make([]*upTrack, len(u.tracks))
			copy(tracks, u.tracks)
		}
		if remote.Kind() == webrtc.RTPCodecTypeVideo {
			atomic.AddUint32(&c.group.videoCount, 1)
		}
		c.mu.Unlock()

		go readLoop(conn, track)

		go rtcpUpListener(conn, track, receiver)

		if tracks != nil {
			clients := c.group.getClients(c)
			for _, cc := range clients {
				cc.pushConn(u, tracks, u.label)
			}
			go rtcpUpSender(conn)
		}
	})

	return conn, nil
}

type packetIndex struct {
	seqno uint16
	index uint16
}

func readLoop(conn *upConnection, track *upTrack) {
	isvideo := track.track.Kind() == webrtc.RTPCodecTypeVideo
	ch := make(chan packetIndex, 32)
	defer close(ch)
	go writeLoop(conn, track, ch)

	buf := make([]byte, packetcache.BufSize)
	var packet rtp.Packet
	drop := 0
	for {
		bytes, err := track.track.Read(buf)
		if err != nil {
			if err != io.EOF {
				log.Printf("%v", err)
			}
			break
		}
		track.rate.Accumulate(uint32(bytes))

		err = packet.Unmarshal(buf[:bytes])
		if err != nil {
			log.Printf("%v", err)
			continue
		}

		track.jitter.Accumulate(packet.Timestamp)

		first, index :=
			track.cache.Store(packet.SequenceNumber, buf[:bytes])
		if packet.SequenceNumber-first > 24 {
			found, first, bitmap := track.cache.BitmapGet()
			if found {
				err := conn.sendNACK(track, first, bitmap)
				if err != nil {
					log.Printf("%v", err)
				}
			}
		}

		if drop > 0 {
			if packet.Marker {
				// last packet in frame
				drop = 0
			} else {
				drop--
			}
			continue
		}

		select {
		case ch <- packetIndex{packet.SequenceNumber, index}:
		default:
			if isvideo {
				// the writer is congested.  Drop until
				// the end of the frame.
				if isvideo && !packet.Marker {
					drop = 7
				}
			}
		}
	}
}

func writeLoop(conn *upConnection, track *upTrack, ch <-chan packetIndex) {
	defer close(track.writerDone)

	buf := make([]byte, packetcache.BufSize)
	var packet rtp.Packet

	local := make([]downTrack, 0)

	firSent := false

	for {
		select {
		case action := <-track.localCh:
			if action.add {
				local = append(local, action.track)
				firSent = false
			} else {
				found := false
				for i, t := range local {
					if t == action.track {
						local = append(local[:i], local[i+1:]...)
						found = true
						break
					}
				}
				if !found {
					log.Printf("Deleting unknown track!")
				}
			}
		case pi, ok := <-ch:
			if !ok {
				return
			}

			bytes := track.cache.GetAt(pi.seqno, pi.index, buf)
			if bytes == 0 {
				continue
			}

			err := packet.Unmarshal(buf[:bytes])
			if err != nil {
				log.Printf("%v", err)
				continue
			}

			kfNeeded := false

			for _, l := range local {
				err := l.WriteRTP(&packet)
				if err != nil {
					if err == ErrKeyframeNeeded {
						kfNeeded = true
					} else if err != io.ErrClosedPipe {
						log.Printf("WriteRTP: %v", err)
					}
					continue
				}
				l.Accumulate(uint32(bytes))
			}

			if kfNeeded {
				err := conn.sendFIR(track, !firSent)
				if err == ErrUnsupportedFeedback {
					err := conn.sendPLI(track)
					if err != nil &&
						err != ErrUnsupportedFeedback {
						log.Printf("sendPLI: %v", err)
					}
				} else if err != nil {
					log.Printf("sendFIR: %v", err)
				}
				firSent = true
			}
		}
	}
}

func rtcpUpListener(conn *upConnection, track *upTrack, r *webrtc.RTPReceiver) {
	for {
		firstSR := false
		ps, err := r.ReadRTCP()
		if err != nil {
			if err != io.EOF {
				log.Printf("ReadRTCP: %v", err)
			}
			return
		}

		now := rtptime.Jiffies()

		for _, p := range ps {
			switch p := p.(type) {
			case *rtcp.SenderReport:
				track.mu.Lock()
				if track.srTime == 0 {
					firstSR = true
				}
				track.srTime = now
				track.srNTPTime = p.NTPTime
				track.srRTPTime = p.RTPTime
				track.mu.Unlock()
			case *rtcp.SourceDescription:
			}
		}

		if(firstSR) {
			// this is the first SR we got for at least one track,
			// quickly propagate the time offsets downstream
			local := conn.getLocal()
			for _, l := range local {
				l, ok := l.(*rtpDownConnection)
				if ok {
					err := sendSR(l)
					if err != nil {
						log.Printf("sendSR: %v", err)
					}
				}
			}
		}
	}
}

func sendRR(conn *upConnection) error {
	if len(conn.tracks) == 0 {
		return nil
	}

	now := rtptime.Jiffies()

	reports := make([]rtcp.ReceptionReport, 0, len(conn.tracks))
	for _, t := range conn.tracks {
		expected, lost, totalLost, eseqno := t.cache.GetStats(true)
		if expected == 0 {
			expected = 1
		}
		if lost >= expected {
			lost = expected - 1
		}

		t.mu.Lock()
		srTime := t.srTime
		srNTPTime := t.srNTPTime
		t.mu.Unlock()

		var delay uint64
		if srTime != 0 {
			delay = (now - srTime) /
				(rtptime.JiffiesPerSec / 0x10000)
		}

		reports = append(reports, rtcp.ReceptionReport{
			SSRC:               t.track.SSRC(),
			FractionLost:       uint8((lost * 256) / expected),
			TotalLost:          totalLost,
			LastSequenceNumber: eseqno,
			Jitter:             t.jitter.Jitter(),
			LastSenderReport:   uint32(srNTPTime >> 16),
			Delay:              uint32(delay),
		})
	}

	return conn.pc.WriteRTCP([]rtcp.Packet{
		&rtcp.ReceiverReport{
			Reports: reports,
		},
	})
}

func rtcpUpSender(conn *upConnection) {
	for {
		time.Sleep(time.Second)
		err := sendRR(conn)
		if err != nil {
			if err == io.EOF || err == io.ErrClosedPipe {
				return
			}
			log.Printf("sendRR: %v", err)
		}
	}
}

func sendSR(conn *rtpDownConnection) error {
	packets := make([]rtcp.Packet, 0, len(conn.tracks))

	now := time.Now()
	nowNTP := rtptime.TimeToNTP(now)
	jiffies := rtptime.TimeToJiffies(now)

	for _, t := range conn.tracks {
		clockrate := t.track.Codec().ClockRate
		remote := t.remote

		remote.mu.Lock()
		lastTime := remote.srTime
		srNTPTime := remote.srNTPTime
		srRTPTime := remote.srRTPTime
		remote.mu.Unlock()

		if lastTime == 0 {
			// we never got a remote SR, skip this track
			continue
		}

		nowRTP := srRTPTime
		if srNTPTime != 0 {
			srTime := rtptime.NTPToTime(srNTPTime)
			delay := now.Sub(srTime)
			if delay > 0 && delay < time.Hour {
				d := rtptime.FromDuration(delay, clockrate)
				nowRTP = srRTPTime + uint32(d)
			}
		}

		p, b := t.rate.Totals()
		packets = append(packets,
			&rtcp.SenderReport{
				SSRC:        t.track.SSRC(),
				NTPTime:     nowNTP,
				RTPTime:     nowRTP,
				PacketCount: p,
				OctetCount:  b,
			})
		atomic.StoreUint64(&t.srTime, jiffies)
		atomic.StoreUint64(&t.srNTPTime, nowNTP)
	}

	if len(packets) == 0 {
		return nil
	}

	return conn.pc.WriteRTCP(packets)
}

func rtcpDownSender(conn *rtpDownConnection) {
	for {
		time.Sleep(time.Second)
		err := sendSR(conn)
		if err != nil {
			if err == io.EOF || err == io.ErrClosedPipe {
				return
			}
			log.Printf("sendSR: %v", err)
		}
	}
}

func delUpConn(c *webClient, id string) bool {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.up == nil {
		return false
	}

	conn := c.up[id]
	if conn == nil {
		return false
	}

	for _, track := range conn.tracks {
		if track.track.Kind() == webrtc.RTPCodecTypeVideo {
			count := atomic.AddUint32(&c.group.videoCount,
				^uint32(0))
			if count == ^uint32(0) {
				log.Printf("Negative track count!")
				atomic.StoreUint32(&c.group.videoCount, 0)
			}
		}
	}

	conn.Close()
	delete(c.up, id)
	return true
}

func getDownConn(c *webClient, id string) *rtpDownConnection {
	if c.down == nil {
		return nil
	}

	c.mu.Lock()
	defer c.mu.Unlock()
	conn := c.down[id]
	if conn == nil {
		return nil
	}
	return conn
}

func getConn(c *webClient, id string) iceConnection {
	up := getUpConn(c, id)
	if up != nil {
		return up
	}
	down := getDownConn(c, id)
	if down != nil {
		return down
	}
	return nil
}

func addDownConn(c *webClient, id string, remote *upConnection) (*rtpDownConnection, error) {
	pc, err := groups.api.NewPeerConnection(iceConfiguration())
	if err != nil {
		return nil, err
	}

	pc.OnICECandidate(func(candidate *webrtc.ICECandidate) {
		sendICE(c, id, candidate)
	})

	pc.OnTrack(func(remote *webrtc.Track, receiver *webrtc.RTPReceiver) {
		log.Printf("Got track on downstream connection")
	})

	if c.down == nil {
		c.down = make(map[string]*rtpDownConnection)
	}
	conn := &rtpDownConnection{
		id:     id,
		client: c,
		pc:     pc,
		remote: remote,
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	if c.down[id] != nil || (c.up != nil && c.up[id] != nil) {
		conn.pc.Close()
		return nil, errors.New("Adding duplicate connection")
	}
	err = remote.addLocal(conn)
	if err != nil {
		conn.pc.Close()
		return nil, err
	}

	c.down[id] = conn
	return conn, nil
}

func delDownConn(c *webClient, id string) bool {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.down == nil {
		return false
	}
	conn := c.down[id]
	if conn == nil {
		return false
	}

	conn.remote.delLocal(conn)
	for _, track := range conn.tracks {
		// we only insert the track after we get an answer, so
		// ignore errors here.
		track.remote.delLocal(track)
	}
	conn.pc.Close()
	delete(c.down, id)
	return true
}

func addDownTrack(c *webClient, conn *rtpDownConnection, remoteTrack *upTrack, remoteConn *upConnection) (*webrtc.RTPSender, error) {
	local, err := conn.pc.NewTrack(
		remoteTrack.track.PayloadType(),
		remoteTrack.track.SSRC(),
		remoteTrack.track.ID(),
		remoteTrack.track.Label(),
	)
	if err != nil {
		return nil, err
	}

	s, err := conn.pc.AddTrack(local)
	if err != nil {
		return nil, err
	}

	track := &rtpDownTrack{
		track:          local,
		remote:         remoteTrack,
		maxLossBitrate: new(bitrate),
		maxREMBBitrate: new(bitrate),
		stats:          new(receiverStats),
		rate:           estimator.New(time.Second),
	}
	conn.tracks = append(conn.tracks, track)

	go rtcpDownListener(conn, track, s)

	return s, nil
}

const (
	minLossRate  = 9600
	initLossRate = 512 * 1000
	maxLossRate  = 1 << 30
)

func (track *rtpDownTrack) updateRate(loss uint8, now uint64) {
	rate := track.maxLossBitrate.Get(now)
	if rate < minLossRate || rate > maxLossRate {
		// no recent feedback, reset
		rate = initLossRate
	}
	if loss < 5 {
		// if our actual rate is low, then we're not probing the
		// bottleneck
		r, _ := track.rate.Estimate()
		actual := 8 * uint64(r)
		if actual >= (rate*7)/8 {
			// loss < 0.02, multiply by 1.05
			rate = rate * 269 / 256
			if rate > maxLossRate {
				rate = maxLossRate
			}
		}
	} else if loss > 25 {
		// loss > 0.1, multiply by (1 - loss/2)
		rate = rate * (512 - uint64(loss)) / 512
		if rate < minLossRate {
			rate = minLossRate
		}
	}

	// update unconditionally, to set the timestamp
	track.maxLossBitrate.Set(rate, now)
}

func rtcpDownListener(conn *rtpDownConnection, track *rtpDownTrack, s *webrtc.RTPSender) {
	var gotFir bool
	lastFirSeqno := uint8(0)

	for {
		ps, err := s.ReadRTCP()
		if err != nil {
			if err != io.EOF {
				log.Printf("ReadRTCP: %v", err)
			}
			return
		}
		jiffies := rtptime.Jiffies()

		for _, p := range ps {
			switch p := p.(type) {
			case *rtcp.PictureLossIndication:
				err := conn.remote.sendPLI(track.remote)
				if err != nil {
					log.Printf("sendPLI: %v", err)
				}
			case *rtcp.FullIntraRequest:
				found := false
				var seqno uint8
				for _, entry := range p.FIR {
					if entry.SSRC == track.track.SSRC() {
						found = true
						seqno = entry.SequenceNumber
						break
					}
				}
				if !found {
					log.Printf("Misdirected FIR")
					continue
				}

				increment := true
				if gotFir {
					increment = seqno != lastFirSeqno
				}
				gotFir = true
				lastFirSeqno = seqno

				err := conn.remote.sendFIR(
					track.remote, increment,
				)
				if err == ErrUnsupportedFeedback {
					err := conn.remote.sendPLI(track.remote)
					if err != nil {
						log.Printf("sendPLI: %v", err)
					}
				} else if err != nil {
					log.Printf("sendFIR: %v", err)
				}
			case *rtcp.ReceiverEstimatedMaximumBitrate:
				track.maxREMBBitrate.Set(p.Bitrate, jiffies)
			case *rtcp.ReceiverReport:
				for _, r := range p.Reports {
					if r.SSRC == track.track.SSRC() {
						handleReport(track, r, jiffies)
					}
				}
			case *rtcp.SenderReport:
				for _, r := range p.Reports {
					if r.SSRC == track.track.SSRC() {
						handleReport(track, r, jiffies)
					}
				}
			case *rtcp.TransportLayerNack:
				maxBitrate := track.GetMaxBitrate(jiffies)
				bitrate, _ := track.rate.Estimate()
				if uint64(bitrate)*7/8 < maxBitrate {
					sendRecovery(p, track)
				}
			}
		}
	}
}

func handleReport(track *rtpDownTrack, report rtcp.ReceptionReport, jiffies uint64) {
	track.stats.Set(report.FractionLost, report.Jitter, jiffies)
	track.updateRate(report.FractionLost, jiffies)

	if report.LastSenderReport != 0 {
		jiffies := rtptime.Jiffies()
		srTime := atomic.LoadUint64(&track.srTime)
		if jiffies < srTime || jiffies-srTime > 8*rtptime.JiffiesPerSec {
			return
		}
		srNTPTime := atomic.LoadUint64(&track.srNTPTime)
		if report.LastSenderReport == uint32(srNTPTime>>16) {
			delay := uint64(report.Delay) *
				(rtptime.JiffiesPerSec / 0x10000)
			if delay > jiffies-srTime {
				return
			}
			rtt := (jiffies - srTime) - delay
			oldrtt := atomic.LoadUint64(&track.rtt)
			newrtt := rtt
			if oldrtt > 0 {
				newrtt = (3*oldrtt + rtt) / 4
			}
			atomic.StoreUint64(&track.rtt, newrtt)
		}
	}
}

func updateUpTrack(up *upConnection, maxVideoRate uint64) {
	now := rtptime.Jiffies()

	for _, track := range up.tracks {
		isvideo := track.track.Kind() == webrtc.RTPCodecTypeVideo
		clockrate := track.track.Codec().ClockRate
		minrate := uint64(minAudioRate)
		rate := ^uint64(0)
		if isvideo {
			minrate = minVideoRate
			rate = maxVideoRate
			if rate < minrate {
				rate = minrate
			}
		}
		local := track.getLocal()
		var maxrto uint64
		for _, l := range local {
			bitrate := l.GetMaxBitrate(now)
			if bitrate == ^uint64(0) {
				continue
			}
			if bitrate <= minrate {
				rate = minrate
				break
			}
			if rate > bitrate {
				rate = bitrate
			}
			ll, ok := l.(*rtpDownTrack)
			if ok {
				_, j := ll.stats.Get(now)
				jitter := uint64(j) *
					(rtptime.JiffiesPerSec /
						uint64(clockrate))
				rtt := atomic.LoadUint64(&ll.rtt)
				rto := rtt + 4*jitter
				if rto > maxrto {
					maxrto = rto
				}
			}
		}
		track.maxBitrate = rate
		_, r := track.rate.Estimate()
		packets := int((uint64(r) * maxrto * 4) / rtptime.JiffiesPerSec)
		if packets < 32 {
			packets = 32
		}
		if packets > 256 {
			packets = 256
		}
		track.cache.ResizeCond(packets)
	}
}

var ErrUnsupportedFeedback = errors.New("unsupported feedback type")
var ErrRateLimited = errors.New("rate limited")

func (up *upConnection) sendPLI(track *upTrack) error {
	if !track.hasRtcpFb("nack", "pli") {
		return ErrUnsupportedFeedback
	}
	last := atomic.LoadUint64(&track.lastPLI)
	now := rtptime.Jiffies()
	if now >= last && now-last < rtptime.JiffiesPerSec/5 {
		return ErrRateLimited
	}
	atomic.StoreUint64(&track.lastPLI, now)
	return sendPLI(up.pc, track.track.SSRC())
}

func sendPLI(pc *webrtc.PeerConnection, ssrc uint32) error {
	return pc.WriteRTCP([]rtcp.Packet{
		&rtcp.PictureLossIndication{MediaSSRC: ssrc},
	})
}

func (up *upConnection) sendFIR(track *upTrack, increment bool) error {
	// we need to reliably increment the seqno, even if we are going
	// to drop the packet due to rate limiting.
	var seqno uint8
	if increment {
		seqno = uint8(atomic.AddUint32(&track.firSeqno, 1) & 0xFF)
	} else {
		seqno = uint8(atomic.LoadUint32(&track.firSeqno) & 0xFF)
	}

	if !track.hasRtcpFb("ccm", "fir") {
		return ErrUnsupportedFeedback
	}
	last := atomic.LoadUint64(&track.lastFIR)
	now := rtptime.Jiffies()
	if now >= last && now-last < rtptime.JiffiesPerSec/5 {
		return ErrRateLimited
	}
	atomic.StoreUint64(&track.lastFIR, now)
	return sendFIR(up.pc, track.track.SSRC(), seqno)
}

func sendFIR(pc *webrtc.PeerConnection, ssrc uint32, seqno uint8) error {
	return pc.WriteRTCP([]rtcp.Packet{
		&rtcp.FullIntraRequest{
			FIR: []rtcp.FIREntry{
				rtcp.FIREntry{
					SSRC:           ssrc,
					SequenceNumber: seqno,
				},
			},
		},
	})
}

func sendREMB(pc *webrtc.PeerConnection, ssrc uint32, bitrate uint64) error {
	return pc.WriteRTCP([]rtcp.Packet{
		&rtcp.ReceiverEstimatedMaximumBitrate{
			Bitrate: bitrate,
			SSRCs:   []uint32{ssrc},
		},
	})
}

func (up *upConnection) sendNACK(track *upTrack, first uint16, bitmap uint16) error {
	if !track.hasRtcpFb("nack", "") {
		return nil
	}
	err := sendNACK(up.pc, track.track.SSRC(), first, bitmap)
	if err == nil {
		track.cache.Expect(1 + bits.OnesCount16(bitmap))
	}
	return err
}

func sendNACK(pc *webrtc.PeerConnection, ssrc uint32, first uint16, bitmap uint16) error {
	packet := rtcp.Packet(
		&rtcp.TransportLayerNack{
			MediaSSRC: ssrc,
			Nacks: []rtcp.NackPair{
				rtcp.NackPair{
					first,
					rtcp.PacketBitmap(bitmap),
				},
			},
		},
	)
	return pc.WriteRTCP([]rtcp.Packet{packet})
}

func sendRecovery(p *rtcp.TransportLayerNack, track *rtpDownTrack) {
	var packet rtp.Packet
	buf := make([]byte, packetcache.BufSize)
	for _, nack := range p.Nacks {
		for _, seqno := range nack.PacketList() {
			l := track.remote.cache.Get(seqno, buf)
			if l == 0 {
				continue
			}
			err := packet.Unmarshal(buf[:l])
			if err != nil {
				continue
			}
			err = track.track.WriteRTP(&packet)
			if err != nil {
				log.Printf("WriteRTP: %v", err)
				continue
			}
			track.rate.Accumulate(uint32(l))
		}
	}
}

func negotiate(c *webClient, down *rtpDownConnection) error {
	offer, err := down.pc.CreateOffer(nil)
	if err != nil {
		return err
	}

	err = down.pc.SetLocalDescription(offer)
	if err != nil {
		return err
	}

	labels := make(map[string]string)
	for _, t := range down.pc.GetTransceivers() {
		var track *webrtc.Track
		if t.Sender() != nil {
			track = t.Sender().Track()
		}
		if track == nil {
			continue
		}

		for _, tr := range down.tracks {
			if tr.track == track {
				labels[t.Mid()] = tr.remote.label
			}
		}
	}

	return c.write(clientMessage{
		Type:   "offer",
		Id:     down.id,
		Offer:  &offer,
		Labels: labels,
	})
}

func sendICE(c *webClient, id string, candidate *webrtc.ICECandidate) error {
	if candidate == nil {
		return nil
	}
	cand := candidate.ToJSON()
	return c.write(clientMessage{
		Type:      "ice",
		Id:        id,
		Candidate: &cand,
	})
}

func gotOffer(c *webClient, id string, offer webrtc.SessionDescription, labels map[string]string) error {
	var err error
	up, ok := c.up[id]
	if !ok {
		up, err = addUpConn(c, id)
		if err != nil {
			return err
		}
	}
	if c.username != "" {
		up.label = c.username
	}
	err = up.pc.SetRemoteDescription(offer)
	if err != nil {
		return err
	}

	answer, err := up.pc.CreateAnswer(nil)
	if err != nil {
		return err
	}

	err = up.pc.SetLocalDescription(answer)
	if err != nil {
		return err
	}

	up.labels = labels

	err = up.flushICECandidates()
	if err != nil {
		log.Printf("ICE: %v", err)
	}

	return c.write(clientMessage{
		Type:   "answer",
		Id:     id,
		Answer: &answer,
	})
}

func gotAnswer(c *webClient, id string, answer webrtc.SessionDescription) error {
	down := getDownConn(c, id)
	if down == nil {
		return protocolError("unknown id in answer")
	}
	err := down.pc.SetRemoteDescription(answer)
	if err != nil {
		return err
	}

	err = down.flushICECandidates()
	if err != nil {
		log.Printf("ICE: %v", err)
	}

	for _, t := range down.tracks {
		t.remote.addLocal(t)
	}
	return nil
}

func gotICE(c *webClient, candidate *webrtc.ICECandidateInit, id string) error {
	conn := getConn(c, id)
	if conn == nil {
		return errors.New("unknown id in ICE")
	}
	return conn.addICECandidate(candidate)
}

func (c *webClient) setRequested(requested map[string]uint32) error {
	if c.down != nil {
		for id := range c.down {
			c.write(clientMessage{
				Type: "close",
				Id:   id,
			})
			delDownConn(c, id)
		}
	}

	c.requested = requested

	go pushConns(c)
	return nil
}

func pushConns(c client) {
	clients := c.getGroup().getClients(c)
	for _, cc := range clients {
		ccc, ok := cc.(*webClient)
		if ok {
			ccc.action(pushConnsAction{c})
		}
	}
}

func (c *webClient) isRequested(label string) bool {
	return c.requested[label] != 0
}

func addDownConnTracks(c *webClient, remote *upConnection, tracks []*upTrack) (*rtpDownConnection, error) {
	requested := false
	for _, t := range tracks {
		if c.isRequested(t.label) {
			requested = true
			break
		}
	}
	if !requested {
		return nil, nil
	}

	down, err := addDownConn(c, remote.id, remote)
	if err != nil {
		return nil, err
	}

	for _, t := range tracks {
		if !c.isRequested(t.label) {
			continue
		}
		_, err = addDownTrack(c, down, t, remote)
		if err != nil {
			delDownConn(c, down.id)
			return nil, err
		}
	}

	go rtcpDownSender(down)

	return down, nil
}

func (c *webClient) pushConn(conn *upConnection, tracks []*upTrack, label string) error {
	err := c.action(addConnAction{conn, tracks})
	if err != nil {
		return err
	}
	if label != "" {
		err := c.action(addLabelAction{conn.id, conn.label})
		if err != nil {
			return err
		}
	}
	return nil
}

func clientLoop(c *webClient, conn *websocket.Conn) error {
	read := make(chan interface{}, 1)
	go clientReader(conn, read, c.done)

	defer func() {
		c.setRequested(map[string]uint32{})
		if c.up != nil {
			for id := range c.up {
				delUpConn(c, id)
			}
		}
	}()

	c.write(clientMessage{
		Type:        "permissions",
		Permissions: c.permissions,
	})

	h := c.group.getChatHistory()
	for _, m := range h {
		err := c.write(clientMessage{
			Type:     "chat",
			Id:       m.id,
			Username: m.user,
			Value:    m.value,
			Me:       m.me,
		})
		if err != nil {
			return err
		}
	}

	readTime := time.Now()

	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	slowTicker := time.NewTicker(10 * time.Second)
	defer slowTicker.Stop()

	for {
		select {
		case m, ok := <-read:
			if !ok {
				return errors.New("reader died")
			}
			switch m := m.(type) {
			case clientMessage:
				readTime = time.Now()
				err := handleClientMessage(c, m)
				if err != nil {
					return err
				}
			case error:
				return m
			}
		case a := <-c.actionCh:
			switch a := a.(type) {
			case addConnAction:
				down, err := addDownConnTracks(
					c, a.conn, a.tracks,
				)
				if err != nil {
					return err
				}
				if down != nil {
					err = negotiate(c, down)
					if err != nil {
						log.Printf("Negotiate: %v", err)
						delDownConn(c, down.id)
						err = failConnection(
							c, down.id,
							"negotiation failed",
						)
						if err != nil {
							return err
						}
						continue
					}
				}
			case delConnAction:
				found := delDownConn(c, a.id)
				if found {
					c.write(clientMessage{
						Type: "close",
						Id:   a.id,
					})
				}
			case addLabelAction:
				c.write(clientMessage{
					Type:  "label",
					Id:    a.id,
					Value: a.label,
				})
			case pushConnsAction:
				for _, u := range c.up {
					tracks := make([]*upTrack, len(u.tracks))
					copy(tracks, u.tracks)
					go a.c.pushConn(u, tracks, u.label)
				}
			case connectionFailedAction:
				found := delUpConn(c, a.id)
				if found {
					err := failConnection(c, a.id,
						"connection failed")
					if err != nil {
						return err
					}
					continue
				}
				// What should we do if a downstream
				// connection fails?  Renegotiate?
			case permissionsChangedAction:
				c.write(clientMessage{
					Type:        "permissions",
					Permissions: c.permissions,
				})
				if !c.permissions.Present {
					ids := getUpConns(c)
					for _, id := range ids {
						found := delUpConn(c, id)
						if found {
							failConnection(
								c, id,
								"permission denied",
							)
						}
					}
				}
			case kickAction:
				return userError("you have been kicked")
			default:
				log.Printf("unexpected action %T", a)
				return errors.New("unexpected action")
			}
		case <-ticker.C:
			sendRateUpdate(c)
		case <-slowTicker.C:
			if time.Since(readTime) > 90*time.Second {
				return errors.New("client is dead")
			}
			if time.Since(readTime) > 60*time.Second {
				err := c.write(clientMessage{
					Type: "ping",
				})
				if err != nil {
					return err
				}
			}
		}
	}
}

func failConnection(c *webClient, id string, message string) error {
	if id != "" {
		err := c.write(clientMessage{
			Type: "abort",
			Id:   id,
		})
		if err != nil {
			return err
		}
	}
	if message != "" {
		err := c.error(userError(message))
		if err != nil {
			return err
		}
	}
	return nil
}

func setPermissions(g *group, id string, perm string) error {
	g.mu.Lock()
	defer g.mu.Unlock()

	client := g.getClientUnlocked(id)
	if client == nil {
		return userError("no such user")
	}

	c, ok := client.(*webClient)
	if !ok {
		return userError("this is not a real user")
	}

	switch perm {
	case "op":
		c.permissions.Op = true
		if g.description.AllowRecording {
			c.permissions.Record = true
		}
	case "unop":
		c.permissions.Op = false
		c.permissions.Record = false
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

	client := g.getClientUnlocked(id)
	if client == nil {
		return userError("no such user")
	}

	c, ok := client.(*webClient)
	if !ok {
		return userError("this is not a real user")
	}

	return c.action(kickAction{})
}

func handleClientMessage(c *webClient, m clientMessage) error {
	switch m.Type {
	case "request":
		err := c.setRequested(m.Request)
		if err != nil {
			return err
		}
	case "offer":
		if !c.permissions.Present {
			c.write(clientMessage{
				Type: "abort",
				Id:   m.Id,
			})
			return c.error(userError("not authorised"))
		}
		if m.Offer == nil {
			return protocolError("null offer")
		}
		err := gotOffer(c, m.Id, *m.Offer, m.Labels)
		if err != nil {
			log.Printf("gotOffer: %v", err)
			return failConnection(c, m.Id, "negotiation failed")
		}
	case "answer":
		if m.Answer == nil {
			return protocolError("null answer")
		}
		err := gotAnswer(c, m.Id, *m.Answer)
		if err != nil {
			return err
		}
	case "close":
		found := delUpConn(c, m.Id)
		if !found {
			log.Printf("Deleting unknown up connection %v", m.Id)
		}
	case "ice":
		if m.Candidate == nil {
			return protocolError("null candidate")
		}
		err := gotICE(c, m.Candidate, m.Id)
		if err != nil {
			log.Printf("ICE: %v", err)
		}
	case "chat":
		c.group.addToChatHistory(m.Id, m.Username, m.Value, m.Me)
		clients := c.group.getClients(c)
		for _, cc := range clients {
			cc, ok := cc.(*webClient)
			if ok {
				cc.write(m)
			}
		}
	case "clearchat":
		c.group.clearChatHistory()
		m := clientMessage{Type: "clearchat"}
		clients := c.group.getClients(nil)
		for _, cc := range clients {
			cc, ok := cc.(*webClient)
			if ok {
				cc.write(m)
			}
		}
	case "op", "unop", "present", "unpresent":
		if !c.permissions.Op {
			return c.error(userError("not authorised"))
		}
		err := setPermissions(c.group, m.Id, m.Type)
		if err != nil {
			return c.error(err)
		}
	case "lock", "unlock":
		if !c.permissions.Op {
			return c.error(userError("not authorised"))
		}
		var locked uint32
		if m.Type == "lock" {
			locked = 1
		}
		atomic.StoreUint32(&c.group.locked, locked)
	case "record":
		if !c.permissions.Record {
			return c.error(userError("not authorised"))
		}
		for _, cc := range c.group.getClients(c) {
			_, ok := cc.(*diskClient)
			if ok {
				return c.error(userError("already recording"))
			}
		}
		disk := &diskClient{
			group: c.group,
			id:    "recording",
		}
		_, err := addClient(c.group.name, disk, "", "")
		if err != nil {
			disk.Close()
			return c.error(err)
		}
		go pushConns(disk)
	case "unrecord":
		if !c.permissions.Record {
			return c.error(userError("not authorised"))
		}
		for _, cc := range c.group.getClients(c) {
			disk, ok := cc.(*diskClient)
			if ok {
				disk.Close()
				delClient(disk)
			}
		}
	case "kick":
		if !c.permissions.Op {
			return c.error(userError("not authorised"))
		}
		err := kickClient(c.group, m.Id)
		if err != nil {
			return c.error(err)
		}
	case "pong":
		// nothing
	case "ping":
		c.write(clientMessage{
			Type: "pong",
		})
	default:
		log.Printf("unexpected message: %v", m.Type)
		return protocolError("unexpected message")
	}
	return nil
}

func sendRateUpdate(c *webClient) {
	type remb struct {
		pc      *webrtc.PeerConnection
		ssrc    uint32
		bitrate uint64
	}
	rembs := make([]remb, 0)

	maxVideoRate := ^uint64(0)
	count := atomic.LoadUint32(&c.group.videoCount)
	if count >= 3 {
		maxVideoRate = uint64(2000000 / math.Sqrt(float64(count)))
		if maxVideoRate < minVideoRate {
			maxVideoRate = minVideoRate
		}
	}

	c.mu.Lock()
	for _, u := range c.up {
		updateUpTrack(u, maxVideoRate)
		for _, t := range u.tracks {
			if !t.hasRtcpFb("goog-remb", "") {
				continue
			}
			bitrate := t.maxBitrate
			if bitrate == ^uint64(0) {
				continue
			}
			rembs = append(rembs,
				remb{u.pc, t.track.SSRC(), bitrate})
		}
	}
	c.mu.Unlock()

	for _, r := range rembs {
		err := sendREMB(r.pc, r.ssrc, r.bitrate)
		if err != nil {
			log.Printf("sendREMB: %v", err)
		}
	}
}

func clientReader(conn *websocket.Conn, read chan<- interface{}, done <-chan struct{}) {
	defer close(read)
	for {
		var m clientMessage
		err := conn.ReadJSON(&m)
		if err != nil {
			select {
			case read <- err:
				return
			case <-done:
				return
			}
		}
		select {
		case read <- m:
		case <-done:
			return
		}
	}
}

func clientWriter(conn *websocket.Conn, ch <-chan interface{}, done chan<- struct{}) {
	defer func() {
		close(done)
		conn.Close()
	}()

	for {
		m, ok := <-ch
		if !ok {
			break
		}
		err := conn.SetWriteDeadline(
			time.Now().Add(2 * time.Second))
		if err != nil {
			return
		}
		switch m := m.(type) {
		case clientMessage:
			err := conn.WriteJSON(m)
			if err != nil {
				return
			}
		case closeMessage:
			err := conn.WriteMessage(websocket.CloseMessage, m.data)
			if err != nil {
				return
			}
		default:
			log.Printf("clientWiter: unexpected message %T", m)
			return
		}
	}
}

var ErrWriterDead = errors.New("client writer died")
var ErrClientDead = errors.New("client dead")

func (c *webClient) action(m interface{}) error {
	select {
	case c.actionCh <- m:
		return nil
	case <-c.done:
		return ErrClientDead
	}
}

func (c *webClient) write(m clientMessage) error {
	select {
	case c.writeCh <- m:
		return nil
	case <-c.writerDone:
		return ErrWriterDead
	}
}

func (c *webClient) error(err error) error {
	switch e := err.(type) {
	case userError:
		return c.write(clientMessage{
			Type:  "error",
			Value: string(e),
		})
	default:
		return err
	}
}
