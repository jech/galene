package estimator

import (
	"testing"

	"github.com/jech/galene/rtptime"
)

func TestEstimator(t *testing.T) {
	now := rtptime.Jiffies()
	e := New(rtptime.JiffiesPerSec)

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
