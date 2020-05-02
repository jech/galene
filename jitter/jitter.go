package jitter

import (
	"sync/atomic"

	"sfu/mono"
)

type Estimator struct {
	hz        uint32
	timestamp uint32
	time uint32

	jitter uint32 // atomic
}

func New(hz uint32) *Estimator {
	return &Estimator{hz: hz}
}

func (e *Estimator) accumulate(timestamp, now uint32) {
	if e.time == 0 {
		e.timestamp = timestamp
		e.time = now
	}

	d := uint32((e.time - now) - (e.timestamp - timestamp))
	if d & 0x80000000 != 0 {
		d = uint32(-int32(d))
	}
	oldjitter := atomic.LoadUint32(&e.jitter)
	jitter := (oldjitter * 15 + d) / 16
	atomic.StoreUint32(&e.jitter, jitter)

	e.timestamp = timestamp
	e.time = now
}

func (e *Estimator) Accumulate(timestamp uint32) {
	e.accumulate(timestamp, uint32(mono.Now(e.hz)))
}

func (e *Estimator) Jitter() uint32 {
	return atomic.LoadUint32(&e.jitter)
}

func (e *Estimator) HZ() uint32 {
	return e.hz
}
