package estimator

import (
	"sync"
	"sync/atomic"
	"time"
)

type Estimator struct {
	interval time.Duration
	bytes    uint32
	packets  uint32

	mu           sync.Mutex
	totalBytes   uint32
	totalPackets uint32
	rate         uint32
	packetRate   uint32
	time         time.Time
}

func New(interval time.Duration) *Estimator {
	return &Estimator{
		interval: interval,
		time:     time.Now(),
	}
}

func (e *Estimator) swap(now time.Time) {
	interval := now.Sub(e.time)
	bytes := atomic.SwapUint32(&e.bytes, 0)
	packets := atomic.SwapUint32(&e.packets, 0)
	atomic.AddUint32(&e.totalBytes, bytes)
	atomic.AddUint32(&e.totalPackets, packets)

	if interval < time.Millisecond {
		e.rate = 0
		e.packetRate = 0
	} else {
		e.rate = uint32(uint64(bytes*1000) /
			uint64(interval/time.Millisecond))
		e.packetRate = uint32(uint64(packets*1000) /
			uint64(interval/time.Millisecond))

	}
	e.time = now
}

func (e *Estimator) Accumulate(count uint32) {
	atomic.AddUint32(&e.bytes, count)
	atomic.AddUint32(&e.packets, 1)
}

func (e *Estimator) estimate(now time.Time) (uint32, uint32) {
	if now.Sub(e.time) > e.interval {
		e.swap(now)
	}

	return e.rate, e.packetRate
}

func (e *Estimator) Estimate() (uint32, uint32) {
	now := time.Now()

	e.mu.Lock()
	defer e.mu.Unlock()
	return e.estimate(now)
}

func (e *Estimator) Totals() (uint32, uint32) {
	b := atomic.LoadUint32(&e.totalBytes) + atomic.LoadUint32(&e.bytes)
	p := atomic.LoadUint32(&e.totalPackets) + atomic.LoadUint32(&e.packets)
	return p, b
}
