package rtptime

import (
	"testing"
	"time"
)

func TestDuration(t *testing.T) {
	a := FromDuration(time.Second, 48000)
	if a != 48000 {
		t.Errorf("Expected 48000, got %v", a)
	}

	b := ToDuration(48000, 48000)
	if b != time.Second {
		t.Errorf("Expected %v, got %v", time.Second, b)
	}
}

func differs(a, b, delta uint64) bool {
	if a < b {
		a, b = b, a
	}
	return a-b >= delta
}

func TestTime(t *testing.T) {
	a := Now(48000)
	time.Sleep(40 * time.Millisecond)
	b := Now(48000) - a
	if differs(b, 40*48, 160) {
		t.Errorf("Expected %v, got %v", 4*48, b)
	}

	c := Microseconds()
	time.Sleep(4 * time.Millisecond)
	d := Microseconds() - c
	if differs(d, 4000, 1000) {
		t.Errorf("Expected %v, got %v", 4000, d)
	}

	c = Jiffies()
	time.Sleep(time.Second * 100000 / JiffiesPerSec)
	d = Jiffies() - c
	if differs(d, 100000, 10000) {
		t.Errorf("Expected %v, got %v", 4000, d)
	}
}

func TestNTP(t *testing.T) {
	now := time.Now()
	ntp := TimeToNTP(now)
	now2 := NTPToTime(ntp)
	ntp2 := TimeToNTP(now2)

	diff1 := now2.Sub(now)
	if diff1 < 0 {
		diff1 = -diff1
	}
	if diff1 > time.Nanosecond {
		t.Errorf("Expected %v, got %v (diff=%v)",
			now, now2, diff1)
	}

	diff2 := int64(ntp2 - ntp)
	if diff2 < 0 {
		diff2 = -diff2
	}
	if diff2 > (1 << 8) {
		t.Errorf("Expected %v, got %v (diff=%v)",
			ntp, ntp2, float64(diff2)/float64(1<<32))
	}

}
