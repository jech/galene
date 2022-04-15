package rtpconn

import (
	"errors"
	"log"
	"sort"
	"time"

	"github.com/jech/galene/conn"
	"github.com/jech/galene/packetcache"
	"github.com/jech/galene/rtptime"
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
		writer := newRtpWriter(wp.track)
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
				int64(d), rtptime.JiffiesPerSec,
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

func newRtpWriter(track *rtpUpTrack) *rtpWriter {
	writer := &rtpWriter{
		ch:     make(chan packetIndex, 32),
		done:   make(chan struct{}),
		action: make(chan writerAction, 1),
	}
	go rtpWriterLoop(writer, track)
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

func sendSequence(kf, last uint16, track conn.DownTrack, cache *packetcache.Cache) {
	buf := make([]byte, packetcache.BufSize)
	seqno := kf
	for ((last - seqno) & 0x8000) == 0 {
		bytes := cache.Get(seqno, buf)
		if bytes == 0 {
			return
		}

		_, err := track.Write(buf[:bytes])
		if err != nil {
			return
		}
		seqno++
	}
}

// rtpWriterLoop is the main loop of an rtpWriter.
func rtpWriterLoop(writer *rtpWriter, track *rtpUpTrack) {
	defer close(writer.done)

	buf := make([]byte, packetcache.BufSize)
	local := make([]conn.DownTrack, 0)

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
				track.mu.Unlock()
				if ntp != 0 {
					action.track.SetTimeOffset(ntp, rtp)
				}
				cname, ok := track.cname.Load().(string)
				if ok && cname != "" {
					action.track.SetCname(cname)
				}

				last, foundLast := track.cache.Last()
				kf, foundKf := track.cache.Keyframe()
				if foundLast && foundKf {
					if last-kf < 40 { // modulo 2^16
						go sendSequence(
							kf, last,
							action.track,
							track.cache,
						)
					} else {
						track.RequestKeyframe()
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

			for _, l := range local {
				_, err := l.Write(buf[:bytes])
				if err != nil {
					continue
				}
			}
		}
	}
}

// nackWriter is called when bufferedNACKs becomes non-empty.  It decides
// which nacks to ship out.
func nackWriter(track *rtpUpTrack) {
	// a client might send us a NACK for a packet that has already
	// been nacked by the reader loop.  Give recovery a chance.
	time.Sleep(50 * time.Millisecond)

	track.mu.Lock()
	nacks := track.bufferedNACKs
	track.bufferedNACKs = nil
	track.mu.Unlock()

	if len(nacks) == 0 || !track.hasRtcpFb("nack", "") {
		return
	}

	// drop any nacks before the last keyframe
	var cutoff uint16
	seqno, found := track.cache.Keyframe()
	if found {
		cutoff = seqno
	} else {
		lastSeqno, last := track.cache.Last()
		if !last {
			// NACK on a fresh track?  Give up.
			return
		}
		// no keyframe, use an arbitrary cutoff
		cutoff = lastSeqno - 256
	}

	i := 0
	for i < len(nacks) {
		if ((nacks[i] - cutoff) & 0x8000) != 0 {
			// earlier than the cutoff, drop
			nacks = append(nacks[:i], nacks[i+1:]...)
			continue
		}
		l := track.cache.Get(nacks[i], nil)
		if l > 0 {
			// the packet arrived in the meantime
			nacks = append(nacks[:i], nacks[i+1:]...)
			continue
		}
		i++
	}

	sort.Slice(nacks, func(i, j int) bool {
		return nacks[i]-cutoff < nacks[j]-cutoff
	})

	if len(nacks) > 0 {
		track.sendNACKs(nacks)
	}
}
