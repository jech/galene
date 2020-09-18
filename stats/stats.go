package stats

import (
	"sort"
	"time"

	"sfu/group"
)

type GroupStats struct {
	Name    string
	Clients []*Client
}

type Client struct {
	Id       string
	Up, Down []Conn
}

type Statable interface {
	GetStats() *Client
}

type Conn struct {
	Id         string
	MaxBitrate uint64
	Tracks     []Track
}

type Track struct {
	Bitrate    uint64
	MaxBitrate uint64
	Loss       uint8
	Rtt        time.Duration
	Jitter     time.Duration
}

func GetGroups() []GroupStats {
	names := group.GetNames()

	gs := make([]GroupStats, 0, len(names))
	for _, name := range names {
		g := group.Get(name)
		if g == nil {
			continue
		}
		clients := g.GetClients(nil)
		stats := GroupStats{
			Name:    name,
			Clients: make([]*Client, 0, len(clients)),
		}
		for _, c := range clients {
			s, ok := c.(Statable)
			if ok {
				cs := s.GetStats()
				stats.Clients = append(stats.Clients, cs)
			} else {
				stats.Clients = append(stats.Clients,
					&Client{Id: c.Id()},
				)
			}
		}
		sort.Slice(stats.Clients, func(i, j int) bool {
			return stats.Clients[i].Id < stats.Clients[j].Id
		})
		gs = append(gs, stats)
	}
	sort.Slice(gs, func(i, j int) bool {
		return gs[i].Name < gs[j].Name
	})

	return gs
}
