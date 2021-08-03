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
	info := layerInfo{8, 9, 10, 11, 12, 13, true}
	down.setLayerInfo(info)
	ntp, rtp := down.getTimeOffset()
	rtt := down.getRTT()
	sr, srntp := down.getSRTime()
	br, sbr, tbr := down.GetMaxBitrate()
	info2 := down.getLayerInfo()
	if ntp != 1 || rtp != 2 || rtt != 3 || sr != 4 || srntp != 5 ||
		br != 6 || sbr != 8 || tbr != 11 {
		t.Errorf(
			"Expected 1 2 3 4 5 6 8 11, "+
				"got %v %v %v %v %v %v %v %v",
			ntp, rtp, rtt, sr, srntp, br, sbr, tbr,
		)
	}
	if info2 != info {
		t.Errorf("Expected %v, got %v", info, info2)
	}
}

