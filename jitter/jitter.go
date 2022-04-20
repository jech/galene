// package jitter implements a jitter estimator
package jitter

import (
	"sync/atomic"

	"github.com/jech/galene/rtptime"
)

type Estimator struct {
	hz        uint32
	timestamp uint32
	time      uint32

	jitter uint32 // atomic
}

// New returns a new jitter estimator that uses units of 1/hz seconds.
func New(hz uint32) *Estimator {
	return &Estimator{hz: hz}
}

func (e *Estimator) accumulate(timestamp, now uint32) {
	if e.time == 0 {
		e.timestamp = timestamp
		e.time = now
	}

	d := uint32((e.time - now) - (e.timestamp - timestamp))
	if d&0x80000000 != 0 {
		d = uint32(-int32(d))
	}
	oldjitter := atomic.LoadUint32(&e.jitter)
	jitter := (oldjitter*15 + d) / 16
	atomic.StoreUint32(&e.jitter, jitter)

	e.timestamp = timestamp
	e.time = now
}

// Accumulate accumulates a new sample for the jitter estimator.
func (e *Estimator) Accumulate(timestamp uint32) {
	e.accumulate(timestamp, uint32(rtptime.Now(e.hz)))
}

// Jitter returns the estimated jitter, in units of 1/hz seconds.
// This function is safe to call concurrently.
func (e *Estimator) Jitter() uint32 {
	return atomic.LoadUint32(&e.jitter)
}

func (e *Estimator) HZ() uint32 {
	return e.hz
}
