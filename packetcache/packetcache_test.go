package packetcache

import (
	"bytes"
	"math/rand"
	"testing"

	"github.com/pion/rtcp"
)

func randomBuf() []byte {
	length := rand.Int31n(BufSize-1) + 1
	buf := make([]byte, length)
	rand.Read(buf)
	return buf
}

func TestCache(t *testing.T) {
	buf1 := randomBuf()
	buf2 := randomBuf()
	cache := New(16)
	cache.Store(13, buf1)
	cache.Store(17, buf2)

	if bytes.Compare(cache.Get(13), buf1) != 0 {
		t.Errorf("Couldn't get 13")
	}
	if bytes.Compare(cache.Get(17), buf2) != 0 {
		t.Errorf("Couldn't get 17")
	}
	if cache.Get(42) != nil {
		t.Errorf("Creation ex nihilo")
	}
}

func TestCacheOverflow(t *testing.T) {
	cache := New(16)

	for i := 0; i < 32; i++ {
		cache.Store(uint16(i), []byte{uint8(i)})
	}

	for i := 0; i < 32; i++ {
		buf := cache.Get(uint16(i))
		if i < 16 {
			if buf != nil {
				t.Errorf("Creation ex nihilo: %v", i)
			}
		} else {
			if len(buf) != 1 || buf[0] != uint8(i) {
				t.Errorf("Expected [%v], got %v", i, buf)
			}
		}
	}
}

func TestBitmap(t *testing.T) {
	value := uint64(0xcdd58f1e035379c0)
	packet := make([]byte, 1)

	cache := New(16)

	var first uint16
	for i := 0; i < 64; i++ {
		if (value & (1 << i)) != 0 {
			first = cache.Store(uint16(42 + i), packet)
		}
	}

	value >>= uint16(first - 42)
	if uint32(value) != cache.bitmap {
		t.Errorf("Got %b, expected %b", cache.bitmap, value)
	}
}

func TestBitmapWrap(t *testing.T) {
	value := uint64(0xcdd58f1e035379c0)
	packet := make([]byte, 1)

	cache := New(16)

	cache.Store(0x7000, packet)
	cache.Store(0xA000, packet)

	var first uint16
	for i := 0; i < 64; i++ {
		if (value & (1 << i)) != 0 {
			first = cache.Store(uint16(42 + i), packet)
		}
	}

	value >>= uint16(first - 42)
	if uint32(value) != cache.bitmap {
		t.Errorf("Got %b, expected %b", cache.bitmap, value)
	}
}

func TestBitmapGet(t *testing.T) {
	value := uint64(0xcdd58f1e035379c0)
	packet := make([]byte, 1)

	cache := New(16)

	for i := 0; i < 64; i++ {
		if (value & (1 << i)) != 0 {
			cache.Store(uint16(42 + i), packet)
		}
	}

	pos := uint16(42)
	for cache.bitmap != 0 {
		first, bitmap := cache.BitmapGet()
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

func TestBitmapPacket(t *testing.T) {
	value := uint64(0xcdd58f1e035379c0)
	packet := make([]byte, 1)

	cache := New(16)

	for i := 0; i < 64; i++ {
		if (value & (1 << i)) != 0 {
			cache.Store(uint16(42 + i), packet)
		}
	}

	first, bitmap := cache.BitmapGet()

	p := rtcp.NackPair{first, rtcp.PacketBitmap(^bitmap)}
	pl := p.PacketList()

	for _, s := range pl {
		if s < 42 || s >= 42+64 {
			if (value & (1 << (s - 42))) != 0 {
				t.Errorf("Bit %v unexpectedly set", s-42)
			}
		}
	}
}
