package main

import (
	"sort"
	"sync/atomic"
	"time"

	"sfu/group"
	"sfu/rtptime"
)

type groupStats struct {
	name    string
	clients []clientStats
}

type clientStats struct {
	id       string
	up, down []connStats
}

type connStats struct {
	id         string
	maxBitrate uint64
	tracks     []trackStats
}

type trackStats struct {
	bitrate    uint64
	maxBitrate uint64
	loss       uint8
	rtt        time.Duration
	jitter     time.Duration
}

func getGroupStats() []groupStats {
	names := group.GetNames()

	gs := make([]groupStats, 0, len(names))
	for _, name := range names {
		g := group.Get(name)
		if g == nil {
			continue
		}
		clients := g.GetClients(nil)
		stats := groupStats{
			name:    name,
			clients: make([]clientStats, 0, len(clients)),
		}
		for _, c := range clients {
			c, ok := c.(*webClient)
			if ok {
				cs := getClientStats(c)
				stats.clients = append(stats.clients, cs)
			}
		}
		sort.Slice(stats.clients, func(i, j int) bool {
			return stats.clients[i].id < stats.clients[j].id
		})
		gs = append(gs, stats)
	}
	sort.Slice(gs, func(i, j int) bool {
		return gs[i].name < gs[j].name
	})

	return gs
}

func getClientStats(c *webClient) clientStats {
	c.mu.Lock()
	defer c.mu.Unlock()

	cs := clientStats{
		id: c.id,
	}

	for _, up := range c.up {
		conns := connStats{
			id: up.id,
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
			conns.tracks = append(conns.tracks, trackStats{
				bitrate: uint64(rate) * 8,
				loss:    loss,
				jitter:  jitter,
			})
		}
		cs.up = append(cs.up, conns)
	}
	sort.Slice(cs.up, func(i, j int) bool {
		return cs.up[i].id < cs.up[j].id
	})

	jiffies := rtptime.Jiffies()
	for _, down := range c.down {
		conns := connStats{
			id:         down.id,
			maxBitrate: down.GetMaxBitrate(jiffies),
		}
		for _, t := range down.tracks {
			rate, _ := t.rate.Estimate()
			rtt := rtptime.ToDuration(atomic.LoadUint64(&t.rtt),
				rtptime.JiffiesPerSec)
			loss, jitter := t.stats.Get(jiffies)
			j := time.Duration(jitter) * time.Second /
				time.Duration(t.track.Codec().ClockRate)
			conns.tracks = append(conns.tracks, trackStats{
				bitrate:    uint64(rate) * 8,
				maxBitrate: t.maxBitrate.Get(jiffies),
				loss:       uint8(uint32(loss) * 100 / 256),
				rtt:        rtt,
				jitter:     j,
			})
		}
		cs.down = append(cs.down, conns)
	}
	sort.Slice(cs.down, func(i, j int) bool {
		return cs.down[i].id < cs.down[j].id
	})

	return cs
}
