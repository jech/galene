package unbounded

import (
	"testing"
	"time"
)

func TestUnbounded(t *testing.T) {
	ch := New[int]()

	go func() {
		for i := 0; i < 1000; i++ {
			ch.Put(i)
		}
	}()

	n := 0
	for n < 1000 {
		<-ch.Ch
		vs := ch.Get()
		for _, v := range vs {
			if n != v {
				t.Errorf("Expected %v, got %v", n, v)
			}
			n++
		}
	}

	go func() {
		for i := 0; i < 1000; i++ {
			ch.Put(i)
			time.Sleep(time.Microsecond)
		}
	}()

	n = 0
	for n < 1000 {
		<-ch.Ch
		vs := ch.Get()
		for _, v := range vs {
			if n != v {
				t.Errorf("Expected %v, got %v", n, v)
			}
			n++
		}
	}

	vs := ch.Get()
	if len(vs) != 0 {
		t.Errorf("Channel is not empty (%v)", len(vs))
	}
}
