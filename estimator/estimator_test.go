package estimator

import (
	"testing"
	"time"
	"sync"
	"sync/atomic"

	"github.com/jech/galene/rtptime"
)

func TestEstimator(t *testing.T) {
	now := rtptime.Jiffies()
	e := new(now, time.Second)

	e.estimate(now)
	e.Accumulate(42)
	e.Accumulate(128)
	e.estimate(now + rtptime.JiffiesPerSec)
	rate, packetRate :=
		e.estimate(now + (rtptime.JiffiesPerSec*1001)/1000)

	if rate != 42+128 {
		t.Errorf("Expected %v, got %v", 42+128, rate)
	}
	if packetRate != 2 {
		t.Errorf("Expected 2, got %v", packetRate)
	}

	totalP, totalB := e.Totals()
	if totalP != 2 {
		t.Errorf("Expected 2, got %v", totalP)
	}
	if totalB != 42+128 {
		t.Errorf("Expected %v, got %v", 42+128, totalB)
	}

	e.Accumulate(12)

	totalP, totalB = e.Totals()
	if totalP != 3 {
		t.Errorf("Expected 2, got %v", totalP)
	}
	if totalB != 42+128+12 {
		t.Errorf("Expected %v, got %v", 42+128, totalB)
	}

}

func TestEstimatorMany(t *testing.T) {
	now := rtptime.Jiffies()
	e := new(now, time.Second)

	for i := 0; i < 10000; i++ {
		e.Accumulate(42)
		now += rtptime.JiffiesPerSec / 1000
		b, p := e.estimate(now)
		if i >= 1000 {
			if p != 1000 || b != p*42 {
				t.Errorf("Got %v %v (%v), expected %v %v",
					p, b, 1000, i, p*42,
				)
			}
		}
	}
}

func TestEstimatorParallel(t *testing.T) {
	now := make([]uint64, 1)
	now[0] = rtptime.Jiffies()
	getNow := func() uint64 {
		return atomic.LoadUint64(&now[0])
	}
	addNow := func(v uint64) {
		atomic.AddUint64(&now[0], v)
	}
	e := new(getNow(), time.Second)
	estimate := func() (uint32, uint32) {
		e.mu.Lock()
		defer e.mu.Unlock()
		return e.estimate(getNow())
	}

	f := func(n int) {
		for i := 0; i < 10000; i++ {
			e.Accumulate(42)
			addNow(rtptime.JiffiesPerSec / 1000)
			b, p := estimate()
			if i >= 1000 {
				if b != p * 42 {
					t.Errorf("%v: Got %v %v (%v), expected %v %v",
						n, p, b, i, 1000, p*42,
					)
				}
			}
		}
	}

	var wg sync.WaitGroup
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func(i int) {
			f(i)
			wg.Done()
		}(i)
	}
	wg.Wait()
}

func BenchmarkEstimator(b *testing.B) {
	e := New(time.Second)

	e.Estimate()
	time.Sleep(time.Millisecond)
	e.Estimate()
	b.ResetTimer()

	for i := 0; i < 1000 * b.N; i++ {
		e.Accumulate(100)

	}
	e.Estimate()
}

func BenchmarkEstimatorParallel(b *testing.B) {
	e := New(time.Second)

	e.Estimate()
	time.Sleep(time.Millisecond)
	e.Estimate()
	b.ResetTimer()

	b.RunParallel(func (pb *testing.PB) {
		for pb.Next() {
			for i := 0; i < 1000; i++ {
				e.Accumulate(100)
			}
		}

	})
	e.Estimate()
}
