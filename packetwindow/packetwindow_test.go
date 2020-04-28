package packetwindow

import (
	"testing"

	"github.com/pion/rtcp"
)

func TestWindow(t *testing.T) {
	value := uint64(0xcdd58f1e035379c0)

	w := New()

	for i := 0; i < 64; i++ {
		if (value & (1 << i)) != 0 {
			w.Set(uint16(42 + i))
		}
	}

	value >>= uint16(w.first - 42)
	if uint32(value) != w.bitmap {
		t.Errorf("Got %b, expected %b", w.bitmap, value)
	}
}

func TestWindowWrap(t *testing.T) {
	value := uint64(0xcdd58f1e035379c0)

	w := New()

	w.Set(0x7000)
	w.Set(0xA000)
	for i := 0; i < 64; i++ {
		if (value & (1 << i)) != 0 {
			w.Set(uint16(42 + i))
		}
	}

	value >>= uint16(w.first - 42)
	if uint32(value) != w.bitmap {
		t.Errorf("Got %b, expected %b", w.bitmap, value)
	}
}

func TestWindowGet(t *testing.T) {
	value := uint64(0xcdd58f1e035379c0)

	w := New()

	for i := 0; i < 64; i++ {
		if (value & (1 << i)) != 0 {
			w.Set(uint16(42 + i))
		}
	}

	pos := uint16(42)
	for w.bitmap != 0 {
		first, bitmap := w.Get17()
		if first < pos || first >= pos+64 {
			t.Errorf("First is %v, pos is %v", first, pos)
		}
		value >>= (first - pos)
		pos = first
		if (value & 1) != 0 {
			t.Errorf("Value is odd")
		}
		value >>= 1
		pos += 1
		if bitmap != uint16(value&0xFFFF) {
			t.Errorf("Got %b, expected %b", bitmap, (value & 0xFFFF))
		}
		value >>= 16
		pos += 16
	}
	if value != 0 {
		t.Errorf("Value is %v", value)
	}
}

func TestWindowPacket(t *testing.T) {
	value := uint64(0xcdd58f1e035379c0)
	w := New()

	for i := 0; i < 64; i++ {
		if (value & (1 << i)) != 0 {
			w.Set(uint16(42 + i))
		}
	}

	first, bitmap := w.Get17()

	p := rtcp.NackPair{first, rtcp.PacketBitmap(^bitmap)}
	list := p.PacketList()

	for _, s := range list {
		if s < 42 || s >= 42 + 64 {
			if (value & (1 << (s - 42))) != 0 {
				t.Errorf("Bit %v unexpectedly set", s - 42)
			}
		}
	}
}
