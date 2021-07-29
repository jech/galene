package codecs

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
	flags, err := PacketFlags("video/vp8", buf)
	if flags.Seqno != 42 || !flags.Start || flags.Pid != 57 ||
		flags.Sid != 0 || flags.Tid != 0 ||
		flags.TidUpSync || flags.Discardable || err != nil {
		t.Errorf("Got %v, %v, %v, %v, %v, %v (%v)",
			flags.Seqno, flags.Start, flags.Pid, flags.Sid,
			flags.TidUpSync, flags.Discardable, err,
		)
	}
}

func TestRewrite(t *testing.T) {
	for i := uint16(0); i < 0x7fff; i++ {
		buf := append([]byte{}, vp8...)
		err := RewritePacket("video/vp8", buf, i, i)
		if err != nil {
			t.Errorf("rewrite: %v", err)
			continue
		}
		flags, err := PacketFlags("video/vp8", buf)
		if err != nil || flags.Seqno != i ||
			flags.Pid != (57+i)&0x7FFF {
			t.Errorf("Expected %v %v, got %v %v (%v)",
				i, (57+i)&0x7FFF,
				flags.Seqno, flags.Pid, err)
		}
	}
}
