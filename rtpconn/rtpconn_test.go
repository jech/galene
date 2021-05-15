package rtpconn

import (
	"testing"

	"github.com/jech/galene/rtptime"
)

func TestDownTrackAtomics(t *testing.T) {
	down := &rtpDownTrack{
		atomics:        &downTrackAtomics{},
		maxBitrate:     new(bitrate),
		maxREMBBitrate: new(bitrate),
	}

	down.SetTimeOffset(1, 2)
	down.setRTT(3)
	down.setSRTime(4, 5)
	down.maxBitrate.Set(6, rtptime.Jiffies())
	down.maxREMBBitrate.Set(7, rtptime.Jiffies())
	down.setLayerInfo(8, 9, 10)
	ntp, rtp := down.getTimeOffset()
	rtt := down.getRTT()
	sr, srntp := down.getSRTime()
	br, lbr := down.GetMaxBitrate()
	l, w, m := down.getLayerInfo()
	if ntp != 1 || rtp != 2 || rtt != 3 || sr != 4 || srntp != 5 ||
		br != 6 || lbr != 8 || l != 8 || w != 9 || m != 10 {
		t.Errorf(
			"Expected 1 2 3 4 5 6 8 8 9 10, "+
				"got %v %v %v %v %v %v %v %v %v %v",
			ntp, rtp, rtt, sr, srntp, br, lbr, l, w, m,
		)
	}
}

