package estimator

import (
	"sync"
	"sync/atomic"
	"time"
)

type Estimator struct {
	interval time.Duration
	count    uint32

	mu   sync.Mutex
	rate uint32
	time time.Time
}

func New(interval time.Duration) *Estimator {
	return &Estimator{
		interval: interval,
		time:     time.Now(),
	}
}

func (e *Estimator) swap(now time.Time) {
	interval := now.Sub(e.time)
	count := atomic.SwapUint32(&e.count, 0)
	if interval < time.Millisecond {
		e.rate = 0
	} else {
		e.rate = uint32(uint64(count*1000) / uint64(interval/time.Millisecond))
	}
	e.time = now
}

func (e *Estimator) Accumulate(count uint32) {
	atomic.AddUint32(&e.count, count)
}

func (e *Estimator) estimate(now time.Time) uint32 {
	if now.Sub(e.time) > e.interval {
		e.swap(now)
	}

	return e.rate
}

func (e *Estimator) Estimate() uint32 {
	now := time.Now()

	e.mu.Lock()
	defer e.mu.Unlock()
	return e.estimate(now)
}
