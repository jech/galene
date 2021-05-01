package packetcache

import (
	"bytes"
	"math/rand"
	"reflect"
	"sync"
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

	found, _, _ := cache.Last()
	if found {
		t.Errorf("Found in empty cache")
	}

	_, i1 := cache.Store(13, 42, false, false, buf1)
	_, i2 := cache.Store(17, 42, false, false, buf2)

	found, seqno, ts := cache.Last()
	if !found {
		t.Errorf("Not found")
	}
	if seqno != 17 || ts != 42 {
		t.Errorf("Expected %v, %v, got %v, %v",
			17, 42, seqno, ts)
	}

	buf := make([]byte, BufSize)

	l := cache.Get(13, buf)
	if !bytes.Equal(buf[:l], buf1) {
		t.Errorf("Couldn't get 13")
	}
	l = cache.Get(13, nil)
	if l != uint16(len(buf1)) {
		t.Errorf("Couldn't retrieve length")
	}
	l = cache.GetAt(13, i1, buf)
	if !bytes.Equal(buf[:l], buf1) {
		t.Errorf("Couldn't get 13 at %v", i1)
	}
	l = cache.Get(17, buf)
	if !bytes.Equal(buf[:l], buf2) {
		t.Errorf("Couldn't get 17")
	}
	l = cache.GetAt(17, i2, buf)
	if !bytes.Equal(buf[:l], buf2) {
		t.Errorf("Couldn't get 17 at %v", i2)
	}

	l = cache.Get(42, buf)
	if l != 0 {
		t.Errorf("Creation ex nihilo")
	}

	l = cache.GetAt(17, i1, buf)
	if l != 0 {
		t.Errorf("Got 17 at %v", i1)
	}

	l = cache.GetAt(42, i2, buf)
	if l != 0 {
		t.Errorf("Got 42 at %v", i2)
	}
}

func TestCacheOverflow(t *testing.T) {
	cache := New(16)

	for i := 0; i < 32; i++ {
		cache.Store(uint16(i), 0, false, false, []byte{uint8(i)})
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

func TestCacheGrow(t *testing.T) {
	cache := New(16)

	for i := 0; i < 24; i++ {
		cache.Store(uint16(i), 0, false, false, []byte{uint8(i)})
	}

	cache.Resize(32)
	for i := 0; i < 32; i++ {
		expected := uint16(0)
		if i < 8 {
			expected = uint16(i + 16)
		}
		if i >= 24 {
			expected = uint16(i - 16)
		}
		if cache.entries[i].seqno != expected {
			t.Errorf("At %v, got %v, expected %v",
				i, cache.entries[i].seqno, expected)
		}
	}
}

func TestCacheShrink(t *testing.T) {
	cache := New(16)

	for i := 0; i < 24; i++ {
		cache.Store(uint16(i), 0, false, false, []byte{uint8(i)})
	}

	cache.Resize(12)
	for i := 0; i < 12; i++ {
		expected := uint16(i + 16)
		if i >= 8 {
			expected = uint16(i + 4)
		}
		if cache.entries[i].seqno != expected {
			t.Errorf("At %v, got %v, expected %v",
				i, cache.entries[i].seqno, expected)
		}
	}
}

func TestCacheGrowCond(t *testing.T) {
	cache := New(16)
	if len(cache.entries) != 16 {
		t.Errorf("Expected 16, got %v", len(cache.entries))
	}

	done := cache.ResizeCond(17)
	if done || len(cache.entries) != 16 {
		t.Errorf("Grew cache by 1")
	}

	done = cache.ResizeCond(15)
	if done || len(cache.entries) != 16 {
		t.Errorf("Shrunk cache by 1")
	}

	done = cache.ResizeCond(32)
	if !done || len(cache.entries) != 32 {
		t.Errorf("Didn't grow cache")
	}

	done = cache.ResizeCond(16)
	if !done || len(cache.entries) != 16 {
		t.Errorf("Didn't shrink cache")
	}
}

func TestKeyframe(t *testing.T) {
	cache := New(16)
	packet := make([]byte, 1)
	buf := make([]byte, BufSize)

	found, _, _ := cache.KeyframeSeqno()
	if found {
		t.Errorf("Found keyframe in empty cache")
	}

	cache.Store(7, 57, true, false, packet)
	if cache.keyframe.complete {
		t.Errorf("Expected false, got true")
	}
	cache.Store(8, 57, false, true, packet)
	if !cache.keyframe.complete {
		t.Errorf("Expected true, got false")
	}

	ts, c, kf := cache.Keyframe()
	if ts != 57 || !c || len(kf) != 2 {
		t.Errorf("Got %v %v %v, expected %v %v", ts, c, len(kf), 57, 2)
	}

	found, seqno, ts := cache.KeyframeSeqno()
	if !found || seqno != 7 || ts != 57 {
		t.Errorf("Got %v %v %v, expected %v %v", found, seqno, ts, 7, 57)
	}

	for _, i := range kf {
		l := cache.Get(i, buf)
		if int(l) != len(packet) {
			t.Errorf("Couldn't get %v", i)
		}
	}

	for i := 0; i < 32; i++ {
		cache.Store(uint16(9+i), uint32(58+i), false, false, packet)
	}

	ts, c, kf = cache.Keyframe()
	if ts != 57 || !c || len(kf) != 2 {
		t.Errorf("Got %v %v %v, expected %v %v", ts, c, len(kf), 57, 2)
	}
	for _, i := range kf {
		l := cache.Get(i, buf)
		if int(l) != len(packet) {
			t.Errorf("Couldn't get %v", i)
		}
	}
}

func TestKeyframeUnsorted(t *testing.T) {
	cache := New(16)
	packet := make([]byte, 1)

	cache.Store(7, 57, false, false, packet)
	cache.Store(9, 57, false, false, packet)
	cache.Store(10, 57, false, true, packet)
	cache.Store(6, 57, true, false, packet)
	_, c, kf := cache.Keyframe()
	if len(kf) != 2 || c {
		t.Errorf("Got %v %v, expected 2", c, kf)
	}
	cache.Store(8, 57, false, false, packet)

	_, c, kf = cache.Keyframe()
	if len(kf) != 5 || !c {
		t.Errorf("Got %v %v, expected 5", c, kf)
	}
	for i, v := range kf {
		if v != uint16(i+6) {
			t.Errorf("Position %v, expected %v, got %v\n",
				i, i+6, v)
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
			first, _ = cache.Store(uint16(42+i), 0, false, false, packet)
		}
	}

	value >>= uint16(first - 42)
	if uint32(value) != cache.bitmap.bitmap {
		t.Errorf("Got %b, expected %b", cache.bitmap.bitmap, value)
	}
}

func TestBitmapWrap(t *testing.T) {
	value := uint64(0xcdd58f1e035379c0)
	packet := make([]byte, 1)

	cache := New(16)

	cache.Store(0x7000, 0, false, false, packet)
	cache.Store(0xA000, 0, false, false, packet)

	var first uint16
	for i := 0; i < 64; i++ {
		if (value & (1 << i)) != 0 {
			first, _ = cache.Store(uint16(42+i), 0, false, false, packet)
		}
	}

	value >>= uint16(first - 42)
	if uint32(value) != cache.bitmap.bitmap {
		t.Errorf("Got %b, expected %b", cache.bitmap.bitmap, value)
	}
}

func TestBitmapGet(t *testing.T) {
	value := uint64(0xcdd58f1e035379c0)
	packet := make([]byte, 1)

	cache := New(16)

	for i := 0; i < 64; i++ {
		if (value & (1 << i)) != 0 {
			cache.Store(uint16(42+i), 0, false, false, packet)
		}
	}

	pos := uint16(42)
	for cache.bitmap.bitmap != 0 {
		found, first, bitmap := cache.BitmapGet(42 + 65)
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
			if uint8(bitmap&1) == uint8(value&1) {
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
			cache.Store(uint16(42+i), 0, false, false, packet)
		}
	}

	found, first, bitmap := cache.BitmapGet(42 + 65)

	if !found {
		t.Fatalf("Didn't find any 0 bits")
	}

	p := rtcp.NackPair{first, rtcp.PacketBitmap(bitmap)}
	p.Range(func(s uint16) bool {
		if s < 42 || s >= 42+64 {
			if (value & (1 << (s - 42))) != 0 {
				t.Errorf("Bit %v unexpectedly set", s-42)
			}
		}
		return true
	})
}

func BenchmarkCachePutGet(b *testing.B) {
	n := 10
	chans := make([]chan uint16, n)
	for i := range chans {
		chans[i] = make(chan uint16, 8)
	}

	cache := New(96)

	var wg sync.WaitGroup
	wg.Add(len(chans))

	for i := range chans {
		go func(ch <-chan uint16) {
			defer wg.Done()
			buf := make([]byte, BufSize)
			for {
				seqno, ok := <-ch
				if !ok {
					return
				}
				l := cache.Get(seqno, buf)
				if l == 0 {
					b.Errorf("Couldn't get %v", seqno)
				}
			}
		}(chans[i])
	}

	buf := make([]byte, 1200)

	b.SetBytes(1200)
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		seqno := uint16(i)
		cache.Store(seqno, 0, false, false, buf)
		for _, ch := range chans {
			ch <- seqno
		}
	}
	for _, ch := range chans {
		close(ch)
	}
	wg.Wait()
}

type is struct {
	index, seqno uint16
}

func BenchmarkCachePutGetAt(b *testing.B) {
	n := 10
	chans := make([]chan is, n)
	for i := range chans {
		chans[i] = make(chan is, 8)
	}

	cache := New(96)

	var wg sync.WaitGroup
	wg.Add(len(chans))

	for i := range chans {
		go func(ch <-chan is) {
			defer wg.Done()
			buf := make([]byte, BufSize)
			for {
				is, ok := <-ch
				if !ok {
					return
				}
				l := cache.GetAt(is.seqno, is.index, buf)
				if l == 0 {
					b.Errorf("Couldn't get %v", is)
				}
			}
		}(chans[i])
	}

	buf := make([]byte, 1200)

	b.SetBytes(1200)
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		seqno := uint16(i)
		_, index := cache.Store(seqno, 0, false, false, buf)
		for _, ch := range chans {
			ch <- is{index, seqno}
		}
	}
	for _, ch := range chans {
		close(ch)
	}
	wg.Wait()
}

func TestToBitmap(t *testing.T) {
	l := []uint16{18, 19, 32, 38}
	bb := uint16(1 | 1<<(32-18-1))
	f, b, r := ToBitmap(l)
	if f != 18 || b != bb {
		t.Errorf("Expected %v %v, Got %v %v", 18, bb, f, b)
	}
	if len(r) != 1 || r[0] != 38 {
		t.Errorf("Expected [38], got %v", r)
	}

	f2, b2, r2 := ToBitmap(r)
	if f2 != 38 || b2 != 0 || len(r2) != 0 {
		t.Errorf("Expected 38 0, got %v %v %v", f2, b2, r2)
	}
}

func TestToBitmapNack(t *testing.T) {
	l := []uint16{18, 19, 32, 38}
	var nacks []rtcp.NackPair
	m := l
	for len(m) > 0 {
		var f, b uint16
		f, b, m = ToBitmap(m)
		nacks = append(nacks, rtcp.NackPair{f, rtcp.PacketBitmap(b)})
	}
	var n []uint16
	for len(nacks) > 0 {
		n = append(n, nacks[0].PacketList()...)
		nacks = nacks[1:]
	}
	if !reflect.DeepEqual(l, n) {
		t.Errorf("Expected %v, got %v", l, n)
	}
}

func TestCacheStatsFull(t *testing.T) {
	cache := New(16)
	for i := 0; i < 32; i++ {
		cache.Store(uint16(i), 0, false, false, []byte{uint8(i)})
	}
	stats := cache.GetStats(false)
	if stats.Received != 32 ||
		stats.TotalReceived != 32 ||
		stats.Expected != 32 ||
		stats.TotalExpected != 32 ||
		stats.ESeqno != 31 {
		t.Errorf("Expected 32, 32, 32, 32, 31, got %v", stats)
	}
}

func TestCacheStatsDrop(t *testing.T) {
	cache := New(16)
	for i := 0; i < 32; i++ {
		if i != 8 && i != 10 {
			cache.Store(uint16(i), 0, false, false, []byte{uint8(i)})
		}
	}
	stats := cache.GetStats(false)
	if stats.Received != 30 ||
		stats.TotalReceived != 30 ||
		stats.Expected != 32 ||
		stats.TotalExpected != 32 ||
		stats.ESeqno != 31 {
		t.Errorf("Expected 30, 30, 32, 32, 31, got %v", stats)
	}
}

func TestCacheStatsUnordered(t *testing.T) {
	cache := New(16)
	for i := 0; i < 32; i++ {
		if i != 8 && i != 10 {
			cache.Store(uint16(i), 0, false, false, []byte{uint8(i)})
		}
	}
	cache.Store(uint16(8), 0, false, false, []byte{8})
	cache.Store(uint16(10), 0, false, false, []byte{10})
	stats := cache.GetStats(false)
	if stats.Received != 32 ||
		stats.TotalReceived != 32 ||
		stats.Expected != 32 ||
		stats.TotalExpected != 32 ||
		stats.ESeqno != 31 {
		t.Errorf("Expected 32, 32, 32, 32, 31, got %v", stats)
	}
}

func TestCacheStatsNack(t *testing.T) {
	cache := New(16)
	for i := 0; i < 32; i++ {
		if i != 8 && i != 10 {
			cache.Store(uint16(i), 0, false, false, []byte{uint8(i)})
		}
	}
	cache.Expect(2)
	cache.Store(uint16(8), 0, false, false, []byte{8})
	cache.Store(uint16(10), 0, false, false, []byte{10})
	stats := cache.GetStats(false)
	if stats.Received != 32 ||
		stats.TotalReceived != 32 ||
		stats.Expected != 34 ||
		stats.TotalExpected != 34 ||
		stats.ESeqno != 31 {
		t.Errorf("Expected 32, 32, 34, 34, 31, got %v", stats)
	}
}
