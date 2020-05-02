package mono

import (
	"testing"
	"time"
)

func differs(a, b, delta uint64) bool {
	if a < b {
		a, b = b, a
	}
	return a - b >= delta
}

func TestMono(t *testing.T) {
	a := Now(48000)
	time.Sleep(4 * time.Millisecond)
	b := Now(48000) - a
	if differs(b, 4 * 48, 16) {
		t.Errorf("Expected %v, got %v", 4 * 48, b)
	}

	c := Microseconds()
	time.Sleep(4 * time.Millisecond)
	d := Microseconds() - c
	if differs(d, 4000, 1000) {
		t.Errorf("Expected %v, got %v", 4000, d)
	}
}
