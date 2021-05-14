package rtpconn

import (
	"testing"
)

var vp8 = []byte{
	0x80, 0, 0, 42,
	0, 0, 0, 0,
	0, 0, 0, 0,

	0x90, 0x80, 0x80, 57,

	0, 0, 0, 0,
}

func TestPacketFlags(t *testing.T) {
	buf := append([]byte{}, vp8...)
	seqno, start, pid, tid, sid, layersync, discardable, err :=
		packetFlags("video/vp8", buf)
	if seqno != 42 || !start || pid != 57 || sid != 0 || tid != 0 ||
		layersync || discardable || err != nil {
		t.Errorf("Got %v, %v, %v, %v, %v, %v (%v)",
			seqno, start, pid, sid, layersync, discardable, err,
		)
	}
}

func TestRewrite(t *testing.T) {
	for i := uint16(0); i < 0x7fff; i++ {
		buf := append([]byte{}, vp8...)
		err := rewritePacket("video/vp8", buf, i, i)
		if err != nil {
			t.Errorf("rewrite: %v", err)
			continue
		}
		seqno, _, pid, _, _, _, _, err := packetFlags("video/vp8", buf)
		if err != nil || seqno != i || pid != (57 + i) & 0x7FFF {
			t.Errorf("Expected %v %v, got %v %v (%v)",
				i, (57 + i) & 0x7FFF, seqno, pid, err)
		}
	}
}
