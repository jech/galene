package main

import (
	"errors"
	"log"
	"time"

	"github.com/pion/rtp"

	"sfu/packetcache"
	"sfu/rtptime"
)

// packetIndex is a request to send a packet from the cache.
type packetIndex struct {
	// the packet's seqno
	seqno uint16
	// the index in the cache
	index uint16
	// the expected delay until the next packet, in jiffies
	delay uint32
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
func (wp *rtpWriterPool) add(track downTrack, add bool) error {
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
	pi := packetIndex{seqno, index, delay}

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
			d := delay/uint32(2*len(wp.writers))
			timer := time.NewTimer(rtptime.ToDuration(
				uint64(d), rtptime.JiffiesPerSec,
			))
			if pi.delay > d {
				pi.delay -= d
			} else {
				pi.delay = 0
			}

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
	track     downTrack
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
func (writer *rtpWriter) add(track downTrack, add bool, max int) error {
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

// rtpWriterLoop is the main loop of an rtpWriter.
func rtpWriterLoop(writer *rtpWriter, conn *rtpUpConnection, track *rtpUpTrack) {
	defer close(writer.done)

	buf := make([]byte, packetcache.BufSize)
	var packet rtp.Packet

	local := make([]downTrack, 0)

	// reset whenever a new track is inserted
	firSent := false

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

				firSent = false
				track.mu.Lock()
				ntp := track.srNTPTime
				rtp := track.srRTPTime
				cname := track.cname
				track.mu.Unlock()
				if ntp != 0 {
					action.track.setTimeOffset(ntp, rtp)
				}
				if cname != "" {
					action.track.setCname(cname)
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

			var delay time.Duration
			if len(local) > 0 {
				delay = rtptime.ToDuration(
					uint64(pi.delay/uint32(len(local))),
					rtptime.JiffiesPerSec,
				)
			}

			kfNeeded := false
			for _, l := range local {
				err := l.WriteRTP(&packet)
				if err != nil {
					if err == ErrKeyframeNeeded {
						kfNeeded = true
					}
					continue
				}
				l.Accumulate(uint32(bytes))
				if delay > 0 {
					time.Sleep(delay)
				}
			}

			if kfNeeded {
				err := conn.sendFIR(track, !firSent)
				if err == ErrUnsupportedFeedback {
					conn.sendPLI(track)
				}
				firSent = true
			}
		}
	}
}
