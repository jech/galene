package stats

import (
	"encoding/json"
	"sort"
	"time"

	"github.com/jech/galene/group"
)

type GroupStats struct {
	Name    string    `json:"name"`
	Clients []*Client `json:"clients,omitempty"`
}

type Client struct {
	Id   string `json:"id"`
	Up   []Conn `json:"up,omitempty"`
	Down []Conn `json:"down,omitempty"`
}

type Statable interface {
	GetStats() *Client
}

type Conn struct {
	Id         string  `json:"id"`
	MaxBitrate uint64  `json:"maxBitrate,omitempty"`
	Tracks     []Track `json:"tracks"`
}

type Duration time.Duration

func (d Duration) MarshalJSON() ([]byte, error) {
	s := float64(d) / float64(time.Millisecond)
	return json.Marshal(s)
}

func (d *Duration) UnmarshalJSON(buf []byte) error {
	var s float64
	err := json.Unmarshal(buf, &s)
	if err != nil {
		return err
	}
	*d = Duration(s * float64(time.Millisecond))
	return nil
}

type Track struct {
	Bitrate    uint64   `json:"bitrate"`
	MaxBitrate uint64   `json:"maxBitrate,omitempty"`
	Loss       float64  `json:"loss"`
	Rtt        Duration `json:"rtt,omitempty"`
	Jitter     Duration `json:"jitter,omitempty"`
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
