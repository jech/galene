package stats

import (
	"bufio"
	"fmt"
	"io"
	"math"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/jech/galene/group"
)

// This file implements a Prometheus exporter.  All values are computed
// at scrape time from the data that is already maintained by the
// connections, so that the exporter has no cost when nobody is scraping.
// We deliberately avoid the Prometheus client library, which maintains
// global atomic counters.
//
// Public groups are reported individually; all other groups are merged
// into a single aggregate, so that no information about private groups
// or individual clients is ever exported.

// summary accumulates the minimum, maximum, mean and standard deviation
// of a set of values.
type summary struct {
	n          int
	sum, sumsq float64
	min, max   float64
}

func (s *summary) add(v float64) {
	if s.n == 0 || v < s.min {
		s.min = v
	}
	if s.n == 0 || v > s.max {
		s.max = v
	}
	s.n++
	s.sum += v
	s.sumsq += v * v
}

func (s *summary) avg() float64 {
	return s.sum / float64(s.n)
}

func (s *summary) stddev() float64 {
	m := s.avg()
	v := s.sumsq/float64(s.n) - m*m
	if v <= 0 {
		return 0
	}
	return math.Sqrt(v)
}

type directionMetrics struct {
	conns   int
	bitrate summary
	loss    summary
	jitter  summary
	rtt     summary
}

func (d *directionMetrics) addConns(conns []Conn) {
	d.conns += len(conns)
	for _, c := range conns {
		for _, t := range c.Tracks {
			d.bitrate.add(float64(t.Bitrate))
			d.loss.add(t.Loss)
			d.jitter.add(time.Duration(t.Jitter).Seconds())
			// an RTT of zero means that it is unknown
			if t.Rtt > 0 {
				d.rtt.add(time.Duration(t.Rtt).Seconds())
			}
		}
	}
}

type groupMetrics struct {
	// the group name, empty for the private aggregate
	name string
	// the label set
	labels string
	// number of groups with at least one client
	active   int
	clients  int
	up, down directionMetrics
}

func (m *groupMetrics) addGroup(g *group.Group) {
	clients := g.GetClients(nil)
	if len(clients) > 0 {
		m.active++
	}
	m.clients += len(clients)
	for _, c := range clients {
		cs := getClient(c)
		m.up.addConns(cs.Up)
		m.down.addConns(cs.Down)
	}
}

var labelEscaper = strings.NewReplacer(`\`, `\\`, `"`, `\"`, "\n", `\n`)

func getMetrics() []*groupMetrics {
	var gs []*group.Group
	// Range holds the global group lock, don't do any work here.
	group.Range(func(g *group.Group) bool {
		gs = append(gs, g)
		return true
	})

	private := &groupMetrics{labels: `visibility="private"`}
	var public []*groupMetrics
	for _, g := range gs {
		if g.Description().Public {
			m := &groupMetrics{
				name: g.Name(),
				labels: `visibility="public",group="` +
					labelEscaper.Replace(g.Name()) + `"`,
			}
			m.addGroup(g)
			public = append(public, m)
		} else {
			private.addGroup(g)
		}
	}
	sort.Slice(public, func(i, j int) bool {
		return public[i].name < public[j].name
	})
	return append(public, private)
}

type metricsWriter struct {
	w *bufio.Writer
}

func (w metricsWriter) header(name, typ, help string) {
	fmt.Fprintf(w.w, "# HELP %s %s\n# TYPE %s %s\n", name, help, name, typ)
}

func (w metricsWriter) sample(name, labels string, v float64) {
	fmt.Fprintf(w.w, "%s{%s} %s\n",
		name, labels, strconv.FormatFloat(v, 'g', -1, 64))
}

func (w metricsWriter) gauge(name, help string, ms []*groupMetrics, f func(*groupMetrics) float64) {
	w.header(name, "gauge", help)
	for _, m := range ms {
		w.sample(name, m.labels, f(m))
	}
}

func (w metricsWriter) directionGauge(name, help string, ms []*groupMetrics, f func(*directionMetrics) float64) {
	w.header(name, "gauge", help)
	for _, m := range ms {
		w.sample(name, m.labels+`,direction="up"`, f(&m.up))
		w.sample(name, m.labels+`,direction="down"`, f(&m.down))
	}
}

// summaryGauges writes the min, avg, stddev and max of a per-track
// value.  Label sets with no values are omitted.
func (w metricsWriter) summaryGauges(name, help string, ms []*groupMetrics, f func(*directionMetrics) *summary) {
	stats := []struct {
		suffix, help string
		f            func(*summary) float64
	}{
		{"_min", "Minimum ", func(s *summary) float64 { return s.min }},
		{"_avg", "Average ", (*summary).avg},
		{"_stddev", "Standard deviation of ", (*summary).stddev},
		{"_max", "Maximum ", func(s *summary) float64 { return s.max }},
	}
	for _, st := range stats {
		n := name + st.suffix
		w.header(n, "gauge", st.help+help)
		for _, m := range ms {
			for _, d := range []struct {
				labels string
				dm     *directionMetrics
			}{
				{`,direction="up"`, &m.up},
				{`,direction="down"`, &m.down},
			} {
				s := f(d.dm)
				if s.n > 0 {
					w.sample(n, m.labels+d.labels, st.f(s))
				}
			}
		}
	}
}

// WritePrometheus writes the server metrics in the Prometheus text
// exposition format.
func WritePrometheus(out io.Writer) error {
	ms := getMetrics()
	w := metricsWriter{bufio.NewWriter(out)}

	w.gauge("galene_groups_active",
		"Number of groups with at least one client.",
		ms, func(m *groupMetrics) float64 {
			return float64(m.active)
		})
	w.gauge("galene_clients",
		"Number of clients.",
		ms, func(m *groupMetrics) float64 {
			return float64(m.clients)
		})
	w.directionGauge("galene_connections",
		"Number of WebRTC connections.",
		ms, func(d *directionMetrics) float64 {
			return float64(d.conns)
		})
	w.directionGauge("galene_tracks",
		"Number of tracks.",
		ms, func(d *directionMetrics) float64 {
			return float64(d.bitrate.n)
		})
	w.directionGauge("galene_bitrate_bits_per_second",
		"Total bitrate of all tracks.",
		ms, func(d *directionMetrics) float64 {
			return d.bitrate.sum
		})
	w.summaryGauges("galene_track_bitrate_bits_per_second",
		"per-track bitrate.",
		ms, func(d *directionMetrics) *summary { return &d.bitrate })
	w.summaryGauges("galene_track_loss_ratio",
		"per-track packet loss.",
		ms, func(d *directionMetrics) *summary { return &d.loss })
	w.summaryGauges("galene_track_jitter_seconds",
		"per-track jitter.",
		ms, func(d *directionMetrics) *summary { return &d.jitter })
	w.summaryGauges("galene_track_rtt_seconds",
		"per-track round-trip time.",
		ms, func(d *directionMetrics) *summary { return &d.rtt })

	return w.w.Flush()
}
