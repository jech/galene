// Package estimator implements a packet and byte rate estimator.

package estimator

import (
	"sync/atomic"
	"time"

	"github.com/jech/galene/rtptime"
)

type Estimator struct {
	interval     uint64
	time         uint64
	bytes        uint32
	packets      uint32
	totalBytes   uint32
	totalPackets uint32
	rate         uint32
	packetRate   uint32
}

// New creates a new estimator that estimates rate over the last interval.
func New(interval time.Duration) *Estimator {
	return &Estimator{
		interval: rtptime.FromDuration(interval, rtptime.JiffiesPerSec),
		time:     rtptime.Now(rtptime.JiffiesPerSec),
	}
}

func (e *Estimator) swap(now uint64) {
	tm := atomic.LoadUint64(&e.time)
	jiffies := now - tm
	bytes := atomic.SwapUint32(&e.bytes, 0)
	packets := atomic.SwapUint32(&e.packets, 0)
	atomic.AddUint32(&e.totalBytes, bytes)
	atomic.AddUint32(&e.totalPackets, packets)

	var rate, packetRate uint32
	if jiffies >= rtptime.JiffiesPerSec/1000 {
		rate = uint32(uint64(bytes) * rtptime.JiffiesPerSec / jiffies)
		packetRate =
			uint32(uint64(packets) * rtptime.JiffiesPerSec / jiffies)
	}
	atomic.StoreUint32(&e.rate, rate)
	atomic.StoreUint32(&e.packetRate, packetRate)
	atomic.StoreUint64(&e.time, now)
}

// Accumulate records one packet of size bytes
func (e *Estimator) Accumulate(bytes uint32) {
	atomic.AddUint32(&e.bytes, bytes)
	atomic.AddUint32(&e.packets, 1)
}

func (e *Estimator) estimate(now uint64) (uint32, uint32) {
	tm := atomic.LoadUint64(&e.time)
	if now < tm || now-tm > e.interval {
		e.swap(now)
	}

	return atomic.LoadUint32(&e.rate), atomic.LoadUint32(&e.packetRate)
}

// Estimate returns an estimate of the rate over the last interval.
// It starts a new interval if the last interval is larger than the value
// passed to New.  It returns the byte rate and the packet rate, in units
// per second.
func (e *Estimator) Estimate() (uint32, uint32) {
	return e.estimate(rtptime.Now(rtptime.JiffiesPerSec))
}

// Totals returns the total number of bytes and packets accumulated.
func (e *Estimator) Totals() (uint32, uint32) {
	b := atomic.LoadUint32(&e.totalBytes) + atomic.LoadUint32(&e.bytes)
	p := atomic.LoadUint32(&e.totalPackets) + atomic.LoadUint32(&e.packets)
	return p, b
}
