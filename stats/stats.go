package stats

import (
	"sort"
	"time"

	"github.com/jech/galene/group"
)

type GroupStats struct {
	Name    string    `json:"name"`
	Clients []*Client `json:"clients"`
}

type Client struct {
	Id   string `json:"id"`
	Up   []Conn `json:"up"`
	Down []Conn `json:"down"`
}

type Statable interface {
	GetStats() *Client
}

type Conn struct {
	Id         string  `json:"id"`
	MaxBitrate uint64  `json:"max_bitrate"`
	Tracks     []Track `json:"tracks"`
}

type Track struct {
	Bitrate    uint64        `json:"bitrate"`
	MaxBitrate uint64        `json:"max_bitrate"`
	Loss       uint8         `json:"loss"`
	Rtt        time.Duration `json:"rtt"`
	Jitter     time.Duration `json:"jitter"`
}

func GetGroups() []GroupStats {
	names := group.GetNames()
	gs := make([]GroupStats, 0, len(names))
	for _, name := range names {
		gs = append(gs, *GetGroup(name))
	}
	sort.Slice(gs, func(i, j int) bool {
		return gs[i].Name < gs[j].Name
	})
	return gs
}

func GetGroup(name string) *GroupStats {
	g := group.Get(name)
	if g == nil {
		return nil
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
	return &stats
}
