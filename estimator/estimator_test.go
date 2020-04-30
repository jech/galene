package estimator

import (
	"testing"
	"time"
)

func TestEstimator(t *testing.T) {
	now := time.Now()
	e := New(time.Second)

	e.estimate(now)
	e.Add(42)
	e.Add(128)
	e.estimate(now.Add(time.Second))
	rate := e.estimate(now.Add(time.Second + time.Millisecond))

	if rate != 42+128 {
		t.Errorf("Expected %v, got %v", 42+128, rate)
	}
}
