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
	flags, err := getPacketFlags("video/vp8", buf)
	if flags.seqno != 42 || !flags.start || flags.pid != 57 ||
		flags.sid != 0 || flags.tid != 0 ||
		flags.tidupsync || flags.discardable || err != nil {
		t.Errorf("Got %v, %v, %v, %v, %v, %v (%v)",
			flags.seqno, flags.start, flags.pid, flags.sid,
			flags.tidupsync, flags.discardable, err,
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
		flags, err := getPacketFlags("video/vp8", buf)
		if err != nil || flags.seqno != i ||
			flags.pid != (57 + i) & 0x7FFF {
			t.Errorf("Expected %v %v, got %v %v (%v)",
				i, (57 + i) & 0x7FFF,
				flags.seqno, flags.pid, err)
		}
	}
}
