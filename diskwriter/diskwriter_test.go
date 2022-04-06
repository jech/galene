package diskwriter

import (
	"testing"
	"time"

	"github.com/jech/galene/rtptime"
)

func TestAdjustOriginLocalNow(t *testing.T) {
	now := time.Now()

	c := &diskConn{
		tracks: []*diskTrack{
			&diskTrack{},
		},
	}
	for _, t := range c.tracks {
		t.conn = c
	}
	c.tracks[0].setOrigin(132, now, 100)

	if !c.originLocal.Equal(now) {
		t.Errorf("Expected %v, got %v", now, c.originLocal)
	}

	if c.originRemote != 0 {
		t.Errorf("Expected 0, got %v", c.originRemote)
	}

	if c.tracks[0].origin != some(132) {
		t.Errorf("Expected 132, got %v", value(c.tracks[0].origin))
	}
}

func TestAdjustOriginLocalEarlier(t *testing.T) {
	now := time.Now()
	earlier := now.Add(-time.Second)

	c := &diskConn{
		originLocal: earlier,
		tracks: []*diskTrack{
			&diskTrack{},
		},
	}
	for _, t := range c.tracks {
		t.conn = c
	}
	c.tracks[0].setOrigin(132, now, 100)

	if !c.originLocal.Equal(earlier) {
		t.Errorf("Expected %v, got %v", earlier, c.originLocal)
	}

	if c.originRemote != 0 {
		t.Errorf("Expected 0, got %v", c.originRemote)
	}

	if c.tracks[0].origin != some(32) {
		t.Errorf("Expected 32, got %v", value(c.tracks[0].origin))
	}
}

func TestAdjustOriginLocalLater(t *testing.T) {
	now := time.Now()
	later := now.Add(time.Second)

	c := &diskConn{
		originLocal: later,
		tracks: []*diskTrack{
			&diskTrack{},
		},
	}
	for _, t := range c.tracks {
		t.conn = c
	}
	c.tracks[0].setOrigin(32, now, 100)

	if !c.originLocal.Equal(later) {
		t.Errorf("Expected %v, got %v", later, c.originLocal)
	}

	if c.originRemote != 0 {
		t.Errorf("Expected 0, got %v", c.originRemote)
	}

	if c.tracks[0].origin != some(132) {
		t.Errorf("Expected 132, got %v", value(c.tracks[0].origin))
	}
}

func TestAdjustOriginRemote(t *testing.T) {
	now := time.Now()
	earlier := now.Add(-time.Second)

	c := &diskConn{
		tracks: []*diskTrack{
			&diskTrack{
				remoteNTP: rtptime.TimeToNTP(earlier),
				remoteRTP: 32,
			},
		},
	}
	for _, t := range c.tracks {
		t.conn = c
	}
	c.tracks[0].setOrigin(132, now, 100)

	if !c.originLocal.Equal(now) {
		t.Errorf("Expected %v, got %v", now, c.originLocal)
	}

	d := now.Sub(rtptime.NTPToTime(c.originRemote))
	if d < -time.Millisecond || d > time.Millisecond {
		t.Errorf("Expected %v, got %v (delta %v)",
			rtptime.TimeToNTP(now),
			c.originRemote, d)
	}

	if c.tracks[0].origin != some(132) {
		t.Errorf("Expected 132, got %v", value(c.tracks[0].origin))
	}
}

func TestAdjustOriginLocalRemote(t *testing.T) {
	now := time.Now()
	earlier := now.Add(-time.Second)

	c := &diskConn{
		tracks: []*diskTrack{
			&diskTrack{},
		},
	}
	for _, t := range c.tracks {
		t.conn = c
	}
	c.tracks[0].setOrigin(132, now, 100)

	c.tracks[0].setTimeOffset(rtptime.TimeToNTP(earlier), 32, 100)

	c.tracks[0].setOrigin(132, now, 100)

	if !c.originLocal.Equal(now) {
		t.Errorf("Expected %v, got %v", now, c.originLocal)
	}

	d := now.Sub(rtptime.NTPToTime(c.originRemote))
	if d < -time.Millisecond || d > time.Millisecond {
		t.Errorf("Expected %v, got %v (delta %v)",
			rtptime.TimeToNTP(now),
			c.originRemote, d)
	}

	if c.tracks[0].origin != some(132) {
		t.Errorf("Expected 132, got %v", value(c.tracks[0].origin))
	}
}
