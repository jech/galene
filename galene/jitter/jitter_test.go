package jitter

import (
	"testing"
)

func TestJitter(t *testing.T) {
	e := New(48000)
	e.accumulate(0, 0)
	e.accumulate(1000, 1000)
	e.accumulate(2000, 2000)
	e.accumulate(3000, 3000)

	if e.Jitter() != 0 {
		t.Errorf("Expected 0, got %v", e.Jitter())
	}

	e = New(48000)
	e.accumulate(0, 0)
	e.accumulate(1000, 1000)
	e.accumulate(2000, 2200)
	e.accumulate(3000, 3000)

	if e.Jitter() != 23 {
		t.Errorf("Expected 23, got %v", e.Jitter())
	}

	e = New(48000)
	e.accumulate(0, 0)
	e.accumulate(1000, 1000)
	e.accumulate(2000, 1800)
	e.accumulate(3000, 3000)

	if e.Jitter() != 23 {
		t.Errorf("Expected 23, got %v", e.Jitter())
	}
}
