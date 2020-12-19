package rtptime

import (
	"time"
)

var epoch = time.Now()

func FromDuration(d time.Duration, hz uint32) uint64 {
	return uint64(d) * uint64(hz) / uint64(time.Second)
}

func ToDuration(tm uint64, hz uint32) time.Duration {
	return time.Duration(tm * uint64(time.Second) / uint64(hz))
}

func Now(hz uint32) uint64 {
	return FromDuration(time.Since(epoch), hz)
}

func Microseconds() uint64 {
	return Now(1000000)
}

// JiffiesPerSec is the LCM of 48000, 96000 and 65536
const JiffiesPerSec = 24576000

func Jiffies() uint64 {
	return Now(JiffiesPerSec)
}

func TimeToJiffies(tm time.Time) uint64 {
	return FromDuration(tm.Sub(epoch), JiffiesPerSec)
}

var ntpEpoch = time.Date(1900, 1, 1, 0, 0, 0, 0, time.UTC)

func NTPToTime(ntp uint64) time.Time {
	sec := uint32(ntp >> 32)
	frac := uint32(ntp & 0xFFFFFFFF)
	return ntpEpoch.Add(
		time.Duration(sec)*time.Second +
			((time.Duration(frac) * time.Second) >> 32),
	)
}

func TimeToNTP(tm time.Time) uint64 {
	d := tm.Sub(ntpEpoch)
	sec := uint32(d / time.Second)
	frac := uint32(d % time.Second)
	return (uint64(sec) << 32) + (uint64(frac)<<32)/uint64(time.Second)
}
