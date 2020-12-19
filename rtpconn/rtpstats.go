package rtpconn

import (
	"sort"
	"sync/atomic"
	"time"

	"github.com/jech/galene/rtptime"
	"github.com/jech/galene/stats"
)

func (c *webClient) GetStats() *stats.Client {
	c.mu.Lock()
	defer c.mu.Unlock()

	cs := stats.Client{
		Id: c.id,
	}

	for _, up := range c.up {
		conns := stats.Conn{
			Id: up.id,
		}
		tracks := up.getTracks()
		for _, t := range tracks {
			expected, lost, _, _ := t.cache.GetStats(false)
			if expected == 0 {
				expected = 1
			}
			loss := uint8(lost * 100 / expected)
			jitter := time.Duration(t.jitter.Jitter()) *
				(time.Second / time.Duration(t.jitter.HZ()))
			rate, _ := t.rate.Estimate()
			conns.Tracks = append(conns.Tracks, stats.Track{
				Bitrate: uint64(rate) * 8,
				Loss:    loss,
				Jitter:  jitter,
			})
		}
		cs.Up = append(cs.Up, conns)
	}
	sort.Slice(cs.Up, func(i, j int) bool {
		return cs.Up[i].Id < cs.Up[j].Id
	})

	jiffies := rtptime.Jiffies()
	for _, down := range c.down {
		conns := stats.Conn{
			Id:         down.id,
			MaxBitrate: down.GetMaxBitrate(jiffies),
		}
		for _, t := range down.tracks {
			rate, _ := t.rate.Estimate()
			rtt := rtptime.ToDuration(atomic.LoadUint64(&t.rtt),
				rtptime.JiffiesPerSec)
			loss, jitter := t.stats.Get(jiffies)
			j := time.Duration(jitter) * time.Second /
				time.Duration(t.track.Codec().ClockRate)
			conns.Tracks = append(conns.Tracks, stats.Track{
				Bitrate:    uint64(rate) * 8,
				MaxBitrate: t.maxBitrate.Get(jiffies),
				Loss:       uint8(uint32(loss) * 100 / 256),
				Rtt:        rtt,
				Jitter:     j,
			})
		}
		cs.Down = append(cs.Down, conns)
	}
	sort.Slice(cs.Down, func(i, j int) bool {
		return cs.Down[i].Id < cs.Down[j].Id
	})

	return &cs
}
