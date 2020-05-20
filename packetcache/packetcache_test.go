package packetcache

import (
	"bytes"
	"math/rand"
	"testing"
	"unsafe"

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

	buf := make([]byte, BufSize)

	l := cache.Get(13, buf)
	if bytes.Compare(buf[:l], buf1) != 0 {
		t.Errorf("Couldn't get 13")
	}

	l = cache.Get(17, buf)
	if bytes.Compare(buf[:l], buf2) != 0 {
		t.Errorf("Couldn't get 17")
	}

	l = cache.Get(42, buf)
	if l != 0 {
		t.Errorf("Creation ex nihilo")
	}
}

func TestCacheOverflow(t *testing.T) {
	cache := New(16)

	for i := 0; i < 32; i++ {
		cache.Store(uint16(i), []byte{uint8(i)})
	}

	for i := 0; i < 32; i++ {
		buf := make([]byte, BufSize)
		l := cache.Get(uint16(i), buf)
		if i < 16 {
			if l > 0 {
				t.Errorf("Creation ex nihilo: %v", i)
			}
		} else {
			if l != 1 || buf[0] != uint8(i) {
				t.Errorf("Expected [%v], got %v", i, buf[:l])
			}
		}
	}
}

func TestCacheAlignment(t *testing.T) {
	cache := New(16)
	for i := range cache.entries {
		p := unsafe.Pointer(&cache.entries[i])
		if uintptr(p) % 32 != 0 {
			t.Errorf("%v: alignment %v", i, uintptr(p) % 32)
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
		found, first, bitmap := cache.BitmapGet()
		if first < pos || first >= pos+64 {
			t.Errorf("First is %v, pos is %v", first, pos)
		}
		if !found {
			t.Fatalf("Didn't find any 0 bits")
		}
		value >>= (first - pos)
		pos = first
		if (value & 1) != 0 {
			t.Errorf("Value is odd")
		}
		value >>= 1
		pos += 1
		for bitmap != 0 {
			if uint8(bitmap & 1) == uint8(value & 1) {
				t.Errorf("Bitmap mismatch")
			}
			bitmap >>= 1
			value >>= 1
			pos += 1
		}
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

	found, first, bitmap := cache.BitmapGet()

	if !found {
		t.Fatalf("Didn't find any 0 bits")
	}

	p := rtcp.NackPair{first, rtcp.PacketBitmap(bitmap)}
	pl := p.PacketList()

	for _, s := range pl {
		if s < 42 || s >= 42+64 {
			if (value & (1 << (s - 42))) != 0 {
				t.Errorf("Bit %v unexpectedly set", s-42)
			}
		}
	}
}
