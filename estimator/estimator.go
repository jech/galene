// Package estimator implements a packet and byte rate estimator.

package estimator

import (
	"sync"
	"time"

	"github.com/jech/galene/rtptime"
)

type Estimator struct {
	interval uint64

	mu           sync.Mutex
	time         uint64
	bytes        uint32
	packets      uint32
	totalBytes   uint64
	totalPackets uint64
	rate         uint32
	packetRate   uint32
}

// New creates a new estimator that estimates rate over the last interval.
func New(interval time.Duration) *Estimator {
	return new(rtptime.Now(rtptime.JiffiesPerSec), interval)
}

func new(now uint64, interval time.Duration) *Estimator {
	return &Estimator{
		interval: uint64(
			rtptime.FromDuration(interval, rtptime.JiffiesPerSec),
		),
		time: now,
	}
}

// called locked
func (e *Estimator) swap(now uint64) {
	jiffies := now - e.time
	bytes := e.bytes
	e.bytes = 0
	packets := e.packets
	e.packets = 0
	e.totalBytes += uint64(bytes)
	e.totalPackets += uint64(packets)

	var rate, packetRate uint32
	if jiffies >= rtptime.JiffiesPerSec/1000 {
		rate = uint32((uint64(bytes)*rtptime.JiffiesPerSec + jiffies/2) / jiffies)
		packetRate = uint32((uint64(packets)*rtptime.JiffiesPerSec + jiffies/2) / jiffies)
	}
	e.rate = rate
	e.packetRate = packetRate
	e.time = now
}

// Accumulate records one packet of size bytes
func (e *Estimator) Accumulate(bytes uint32) {
	e.mu.Lock()
	if e.bytes < ^uint32(0)-bytes {
		e.bytes += bytes
	}
	if e.packets < ^uint32(0)-1 {
		e.packets += 1
	}
	e.mu.Unlock()
}

// called locked
func (e *Estimator) estimate(now uint64) (uint32, uint32) {
	if now < e.time {
		// time went backwards
		if e.time-now > e.interval {
			e.time = now
			e.packets = 0
			e.bytes = 0
		}
	} else if now-e.time >= e.interval {
		e.swap(now)
	}

	return e.rate, e.packetRate
}

// Estimate returns an estimate of the rate over the last interval.
// It starts a new interval if the last interval is larger than the value
// passed to New.  It returns the byte rate and the packet rate, in units
// per second.
func (e *Estimator) Estimate() (uint32, uint32) {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.estimate(rtptime.Now(rtptime.JiffiesPerSec))
}

// Totals returns the total number of bytes and packets accumulated.
func (e *Estimator) Totals() (uint64, uint64) {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.totalPackets + uint64(e.packets),
		e.totalBytes + uint64(e.bytes)
}
