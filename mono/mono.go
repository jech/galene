package mono

import (
	"time"
)

var epoch = time.Now()

func fromDuration(d time.Duration, hz uint32) uint64 {
	return uint64(d) * uint64(hz) / uint64(time.Second)
}

func Now(hz uint32) uint64 {
	return fromDuration(time.Since(epoch), hz)
}

func Microseconds() uint64 {
	return Now(1000000)
}
