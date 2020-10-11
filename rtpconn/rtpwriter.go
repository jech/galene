package rtpconn

import (
	"errors"
	"log"
	"time"

	"github.com/pion/rtp"
	"github.com/pion/webrtc/v3"

	"sfu/conn"
	"sfu/packetcache"
	"sfu/rtptime"
)

// packetIndex is a request to send a packet from the cache.
type packetIndex struct {
	// the packet's seqno
	seqno uint16
	// the index in the cache
	index uint16
}

// An rtpWriterPool is a set of rtpWriters
type rtpWriterPool struct {
	conn    *rtpUpConnection
	track   *rtpUpTrack
	writers []*rtpWriter
	count   int
}

// sqrt computes the integer square root
func sqrt(n int) int {
	if n < 2 {
		return n
	}

	s := sqrt(n/2) * 2
	l := s + 1
	if l*l > n {
		return s
	} else {
		return l
	}
}

// add adds or removes a track from a writer pool
func (wp *rtpWriterPool) add(track conn.DownTrack, add bool) error {
	n := 4
	if wp.count > 16 {
		n = sqrt(wp.count)
	}

	i := 0
	for i < len(wp.writers) {
		w := wp.writers[i]
		err := w.add(track, add, n)
		if err == nil {
			if add {
				wp.count++
			} else {
				if wp.count > 0 {
					wp.count--
				} else {
					log.Printf("Negative writer count!")
				}
			}
			return nil
		} else if err == ErrWriterDead {
			wp.del(wp.writers[i])
		} else {
			i++
		}
	}

	if add {
		writer := newRtpWriter(wp.conn, wp.track)
		wp.writers = append(wp.writers, writer)
		err := writer.add(track, true, n)
		if err == nil {
			wp.count++
		}
		return err
	} else {
		return errors.New("deleting unknown track")
	}
}

// del deletes a writer.
func (wp *rtpWriterPool) del(w *rtpWriter) bool {
	for i, ww := range wp.writers {
		if ww == w {
			close(w.ch)
			wp.writers = append(wp.writers[:i], wp.writers[i+1:]...)
			return true
		}
	}
	return false
}

// close deletes all writers.
func (wp *rtpWriterPool) close() {
	for _, w := range wp.writers {
		close(w.ch)
	}
	wp.writers = nil
	wp.count = 0
}

// write writes a packet stored in the packet cache to all local tracks
func (wp *rtpWriterPool) write(seqno uint16, index uint16, delay uint32, isvideo bool, marker bool) {
	pi := packetIndex{seqno, index}

	var dead []*rtpWriter
	for _, w := range wp.writers {
		if w.drop > 0 {
			// currently dropping
			if marker {
				// last packet in frame
				w.drop = 0
			} else {
				w.drop--
			}
			continue
		}
		select {
		case w.ch <- pi:
			// all is well
		case <-w.done:
			// the writer is dead.
			dead = append(dead, w)
		default:
			// the writer is congested
			if isvideo {
				// drop until the end of the frame
				if !marker {
					w.drop = 7
				}
				continue
			}
			// audio, try again with a delay
			d := delay / uint32(2*len(wp.writers))
			timer := time.NewTimer(rtptime.ToDuration(
				uint64(d), rtptime.JiffiesPerSec,
			))

			select {
			case w.ch <- pi:
				timer.Stop()
			case <-w.done:
				dead = append(dead, w)
			case <-timer.C:
			}
		}
	}

	if dead != nil {
		for _, d := range dead {
			wp.del(d)
		}
		dead = nil
	}
}

var ErrWriterDead = errors.New("writer is dead")
var ErrWriterBusy = errors.New("writer is busy")
var ErrUnknownTrack = errors.New("unknown track")

type writerAction struct {
	add       bool
	track     conn.DownTrack
	maxTracks int
	ch        chan error
}

// an rtpWriter is a thread writing to a set of tracks.
type rtpWriter struct {
	ch     chan packetIndex
	done   chan struct{}
	action chan writerAction

	// this is not touched by the writer loop, used by the caller
	drop int
}

func newRtpWriter(conn *rtpUpConnection, track *rtpUpTrack) *rtpWriter {
	writer := &rtpWriter{
		ch:     make(chan packetIndex, 32),
		done:   make(chan struct{}),
		action: make(chan writerAction, 1),
	}
	go rtpWriterLoop(writer, conn, track)
	return writer
}

// add adds or removes a track from a writer.
func (writer *rtpWriter) add(track conn.DownTrack, add bool, max int) error {
	ch := make(chan error, 1)
	select {
	case writer.action <- writerAction{add, track, max, ch}:
		select {
		case err := <-ch:
			return err
		case <-writer.done:
			return ErrWriterDead
		}
	case <-writer.done:
		return ErrWriterDead
	}
}

func sendKeyframe(kf []uint16, track conn.DownTrack, cache *packetcache.Cache) {
	buf := make([]byte, packetcache.BufSize)
	var packet rtp.Packet
	for _, seqno := range kf {
		bytes := cache.Get(seqno, buf)
		if bytes == 0 {
			return
		}
		err := packet.Unmarshal(buf[:bytes])
		if err != nil {
			return
		}
		err = track.WriteRTP(&packet)
		if err != nil && err != conn.ErrKeyframeNeeded {
			return
		}
		track.Accumulate(uint32(bytes))
	}
}

// rtpWriterLoop is the main loop of an rtpWriter.
func rtpWriterLoop(writer *rtpWriter, up *rtpUpConnection, track *rtpUpTrack) {
	defer close(writer.done)

	codec := track.track.Codec().Name

	buf := make([]byte, packetcache.BufSize)
	var packet rtp.Packet

	local := make([]conn.DownTrack, 0)

	// 3 means we want a new keyframe, 2 means we already sent FIR but
	// haven't gotten a keyframe yet, 1 means we want a PLI.
	kfNeeded := 0

	for {
		select {
		case action := <-writer.action:
			if action.add {
				if len(local) >= action.maxTracks {
					action.ch <- ErrWriterBusy
					close(action.ch)
					continue
				}
				local = append(local, action.track)
				action.ch <- nil
				close(action.ch)

				track.mu.Lock()
				ntp := track.srNTPTime
				rtp := track.srRTPTime
				cname := track.cname
				track.mu.Unlock()
				if ntp != 0 {
					action.track.SetTimeOffset(ntp, rtp)
				}
				if cname != "" {
					action.track.SetCname(cname)
				}

				found, _, lts := track.cache.Last()
				kts, _, kf := track.cache.Keyframe()
				if codec == webrtc.VP8 && found && len(kf) > 0 {
					if ((lts-kts)&0x80000000) != 0 ||
						lts-kts < 2*90000 {
						// we got a recent keyframe
						go sendKeyframe(
							kf,
							action.track,
							track.cache,
						)
					} else {
						// Request a new keyframe
						kfNeeded = 3
					}
				} else {
					// no keyframe yet, one should
					// arrive soon.  Do nothing.
				}
			} else {
				found := false
				for i, t := range local {
					if t == action.track {
						local = append(local[:i],
							local[i+1:]...)
						found = true
						break
					}
				}
				if !found {
					action.ch <- ErrUnknownTrack
				} else {
					action.ch <- nil
				}
				close(action.ch)
				if len(local) == 0 {
					return
				}
			}
		case pi, ok := <-writer.ch:
			if !ok {
				return
			}

			bytes := track.cache.GetAt(pi.seqno, pi.index, buf)
			if bytes == 0 {
				continue
			}

			err := packet.Unmarshal(buf[:bytes])
			if err != nil {
				continue
			}

			for _, l := range local {
				err := l.WriteRTP(&packet)
				if err != nil {
					if err == conn.ErrKeyframeNeeded {
						kfNeeded = 1
					} else {
						continue
					}
				}
				l.Accumulate(uint32(bytes))
			}

			if kfNeeded > 0 {
				kf := false
				switch codec {
				case webrtc.VP8:
					kf = isVP8Keyframe(&packet)
				default:
					kf = true
				}
				if kf {
					kfNeeded = 0
				}
			}

			if kfNeeded >= 2 {
				err := up.sendFIR(track, kfNeeded >= 3)
				if err == ErrUnsupportedFeedback {
					up.sendPLI(track)
				}
				kfNeeded = 2
			} else if kfNeeded > 0 {
				up.sendPLI(track)
			}
		}
	}
}
