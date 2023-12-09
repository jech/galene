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

	b := FromDuration(-time.Second, 48000)
	if b != -48000 {
		t.Errorf("Expected -48000, got %v", b)
	}

	c := ToDuration(48000, 48000)
	if c != time.Second {
		t.Errorf("Expected %v, got %v", time.Second, c)
	}

	d := ToDuration(-48000, 48000)
	if d != -time.Second {
		t.Errorf("Expected %v, got %v", -time.Second, d)
	}
}

func TestDurationOverflow(t *testing.T) {
	delta := 10 * time.Minute
	dj := FromDuration(delta, JiffiesPerSec)
	var prev int64
	for d := time.Duration(0); d < time.Duration(1000*time.Hour); d += delta {
		jiffies := FromDuration(d, JiffiesPerSec)
		if d != 0 {
			if jiffies != prev+dj {
				t.Errorf("%v: %v, %v", d, jiffies, prev)
			}
		}
		d2 := ToDuration(jiffies, JiffiesPerSec)
		if d2 != d {
			t.Errorf("%v != %v (%v)", d2, d, jiffies)
		}
		prev = jiffies
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
	time.Sleep(time.Second * 10000000 / JiffiesPerSec)
	d = Jiffies() - c
	if differs(d, 10000000, 1000000) {
		t.Errorf("Expected %v, got %v", 10000000, d)
	}
}

func TestNTP(t *testing.T) {
	now := time.Now()
	ntp := TimeToNTP(now)
	now2 := NTPToTime(ntp)
	ntp2 := TimeToNTP(now2)

	diff1 := now2.Sub(now).Abs()
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
