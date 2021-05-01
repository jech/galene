// Package rtptime manipulates RTP and NTP time
package rtptime

import (
	"time"
)

// epoch is the origin of NTP time
var epoch = time.Now()

// FromDuration converts a time.Duration into units of 1/hz.
func FromDuration(d time.Duration, hz uint32) uint64 {
	return uint64(d) * uint64(hz) / uint64(time.Second)
}

// ToDuration converts units of 1/hz into a time.Duration.
func ToDuration(tm uint64, hz uint32) time.Duration {
	return time.Duration(tm * uint64(time.Second) / uint64(hz))
}

// Now returns the current time in units of 1/hz from an arbitrary origin.
func Now(hz uint32) uint64 {
	return FromDuration(time.Since(epoch), hz)
}

// Microseconds is like Now, but uses microseconds.
func Microseconds() uint64 {
	return Now(1000000)
}

// JiffiesPerSec is the number of jiffies in a second.  This is the LCM of
// 48000, 96000 and 65536.
const JiffiesPerSec = 24576000

// Jiffies returns the current time in jiffies.
func Jiffies() uint64 {
	return Now(JiffiesPerSec)
}

// TimeToJiffies converts a time.Time into jiffies.
func TimeToJiffies(tm time.Time) uint64 {
	return FromDuration(tm.Sub(epoch), JiffiesPerSec)
}

// The origin of NTP time.
var ntpEpoch = time.Date(1900, 1, 1, 0, 0, 0, 0, time.UTC)

// NTPToTime converts an NTP time into a time.Time.
func NTPToTime(ntp uint64) time.Time {
	sec := uint32(ntp >> 32)
	frac := uint32(ntp & 0xFFFFFFFF)
	return ntpEpoch.Add(
		time.Duration(sec)*time.Second +
			((time.Duration(frac) * time.Second) >> 32),
	)
}

// TimeToNTP converts a time.Time into an NTP time.
func TimeToNTP(tm time.Time) uint64 {
	d := tm.Sub(ntpEpoch)
	sec := uint32(d / time.Second)
	frac := uint32(d % time.Second)
	return (uint64(sec) << 32) + (uint64(frac)<<32)/uint64(time.Second)
}
