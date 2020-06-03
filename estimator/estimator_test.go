package estimator

import (
	"testing"
	"time"
)

func TestEstimator(t *testing.T) {
	now := time.Now()
	e := New(time.Second)

	e.estimate(now)
	e.Accumulate(42)
	e.Accumulate(128)
	e.estimate(now.Add(time.Second))
	rate, packetRate := e.estimate(now.Add(time.Second + time.Millisecond))

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
