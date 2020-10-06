package packetcache

import (
	"math/bits"
	"sync"
)

const BufSize = 1500
const maxFrame = 1024

type entry struct {
	seqno  uint16
	length uint16
	buf    [BufSize]byte
}

type bitmap struct {
	valid  bool
	first  uint16
	bitmap uint32
}

type frame struct {
	timestamp uint32
	entries   []entry
}

type Cache struct {
	mu sync.Mutex
	//stats
	last      uint16
	cycle     uint16
	lastValid bool
	expected  uint32
	lost      uint32
	totalLost uint32
	// bitmap
	bitmap bitmap
	// buffered keyframe
	keyframe frame
	// the actual cache
	tail    uint16
	entries []entry
}

func New(capacity int) *Cache {
	if capacity > int(^uint16(0)) {
		return nil
	}
	return &Cache{
		entries: make([]entry, capacity),
	}
}

func seqnoInvalid(seqno, reference uint16) bool {
	if ((seqno - reference) & 0x8000) == 0 {
		return false
	}

	if reference-seqno > 0x100 {
		return true
	}

	return false
}

// set sets a bit in the bitmap, shifting if necessary
func (bitmap *bitmap) set(seqno uint16) {
	if !bitmap.valid || seqnoInvalid(seqno, bitmap.first) {
		bitmap.first = seqno
		bitmap.bitmap = 1
		bitmap.valid = true
		return
	}

	if ((seqno - bitmap.first) & 0x8000) != 0 {
		return
	}

	if seqno-bitmap.first >= 32 {
		shift := seqno - bitmap.first - 31
		bitmap.bitmap >>= shift
		bitmap.first += shift
	}

	if (bitmap.bitmap & 1) == 1 {
		ones := bits.TrailingZeros32(^bitmap.bitmap)
		bitmap.bitmap >>= ones
		bitmap.first += uint16(ones)
	}

	bitmap.bitmap |= (1 << uint16(seqno-bitmap.first))
	return
}

// BitmapGet shifts up to 17 bits out of the bitmap.  It returns a boolean
// indicating if any were 0, the index of the first 0 bit, and a bitmap
// indicating any 0 bits after the first one.
func (cache *Cache) BitmapGet(next uint16) (bool, uint16, uint16) {
	cache.mu.Lock()
	defer cache.mu.Unlock()
	return cache.bitmap.get(next)
}

func (bitmap *bitmap) get(next uint16) (bool, uint16, uint16) {
	first := bitmap.first
	count := next - first
	if (count&0x8000) != 0 || count == 0 {
		// next is in the past
		return false, first, 0
	}
	if count > 17 {
		count = 17
	}
	bm := (^bitmap.bitmap) & ^((^uint32(0)) << count)
	bitmap.bitmap >>= count
	bitmap.first += count

	if bm == 0 {
		return false, first, 0
	}

	if (bm & 1) == 0 {
		count := bits.TrailingZeros32(bm)
		bm >>= count
		first += uint16(count)
	}

	return true, first, uint16(bm >> 1)
}

func (frame *frame) store(seqno uint16, timestamp uint32, first bool, data []byte) {
	if first {
		if frame.timestamp != timestamp {
			frame.timestamp = timestamp
			frame.entries = frame.entries[:0]
		}
	} else if len(frame.entries) > 0 {
		if frame.timestamp != timestamp {
			delta := seqno - frame.entries[0].seqno
			if (delta & 0x8000) == 0 && delta > 0x4000 {
				frame.entries = frame.entries[:0]
			}
			return
		}
	} else {
		return
	}

	i := 0
	for i < len(frame.entries) {
		if frame.entries[i].seqno >= seqno {
			break
		}
		i++
	}

	if i < len(frame.entries) && frame.entries[i].seqno == seqno {
		// duplicate
		return
	}

	if len(frame.entries) >= maxFrame {
		// overflow
		return
	}

	e := entry{
		seqno:  seqno,
		length: uint16(len(data)),
	}
	copy(e.buf[:], data)

	if i >= len(frame.entries) {
		frame.entries = append(frame.entries, e)
		return
	}
	frame.entries = append(frame.entries, entry{})
	copy(frame.entries[i+1:], frame.entries[i:])
	frame.entries[i] = e
}

// Store a packet, setting bitmap at the same time
func (cache *Cache) Store(seqno uint16, timestamp uint32, keyframe bool, buf []byte) (uint16, uint16) {
	cache.mu.Lock()
	defer cache.mu.Unlock()

	if !cache.lastValid || seqnoInvalid(seqno, cache.last) {
		cache.last = seqno
		cache.lastValid = true
		cache.expected++
	} else {
		if ((cache.last - seqno) & 0x8000) != 0 {
			cache.expected += uint32(seqno - cache.last)
			cache.lost += uint32(seqno - cache.last - 1)
			if seqno < cache.last {
				cache.cycle++
			}
			cache.last = seqno
		} else {
			if cache.lost > 0 {
				cache.lost--
			}
		}
	}
	cache.bitmap.set(seqno)

	cache.keyframe.store(seqno, timestamp, keyframe, buf)

	i := cache.tail
	cache.entries[i].seqno = seqno
	copy(cache.entries[i].buf[:], buf)
	cache.entries[i].length = uint16(len(buf))
	cache.tail = (i + 1) % uint16(len(cache.entries))

	return cache.bitmap.first, i
}

func (cache *Cache) Expect(n int) {
	if n <= 0 {
		return
	}
	cache.mu.Lock()
	defer cache.mu.Unlock()
	cache.expected += uint32(n)
}

func get(seqno uint16, entries []entry, result []byte) uint16 {
	for i := range entries {
		if entries[i].length == 0 || entries[i].seqno != seqno {
			continue
		}
		return uint16(copy(
			result[:entries[i].length],
			entries[i].buf[:]),
		)
	}
	return 0
}

func (cache *Cache) Get(seqno uint16, result []byte) uint16 {
	cache.mu.Lock()
	defer cache.mu.Unlock()

	n := get(seqno, cache.keyframe.entries, result)
	if n > 0 {
		return n
	}

	n = get(seqno, cache.entries, result)
	if n > 0 {
		return n
	}

	return 0
}

func (cache *Cache) GetAt(seqno uint16, index uint16, result []byte) uint16 {
	cache.mu.Lock()
	defer cache.mu.Unlock()

	if int(index) > len(cache.entries) {
		return 0
	}
	if cache.entries[index].seqno != seqno {
		return 0
	}
	return uint16(copy(
		result[:cache.entries[index].length],
		cache.entries[index].buf[:]),
	)
}

func (cache *Cache) Keyframe() (uint32, []uint16) {
	cache.mu.Lock()
	defer cache.mu.Unlock()

	if len(cache.keyframe.entries) == 0 {
		return 0, nil
	}

	seqnos := make([]uint16, len(cache.keyframe.entries))
	for i := range cache.keyframe.entries {
		seqnos[i] = cache.keyframe.entries[i].seqno
	}
	return cache.keyframe.timestamp, seqnos
}

func (cache *Cache) resize(capacity int) {
	if len(cache.entries) == capacity {
		return
	}

	entries := make([]entry, capacity)

	if capacity > len(cache.entries) {
		copy(entries, cache.entries[:cache.tail])
		copy(entries[int(cache.tail)+capacity-len(cache.entries):],
			cache.entries[cache.tail:])
	} else if capacity > int(cache.tail) {
		copy(entries, cache.entries[:cache.tail])
		copy(entries[cache.tail:],
			cache.entries[int(cache.tail)+
				len(cache.entries)-capacity:])
	} else {
		// too bad, invalidate all indices
		copy(entries,
			cache.entries[int(cache.tail)-capacity:cache.tail])
		cache.tail = 0
	}
	cache.entries = entries
}

func (cache *Cache) Resize(capacity int) {
	cache.mu.Lock()
	defer cache.mu.Unlock()

	cache.resize(capacity)
}

func (cache *Cache) ResizeCond(capacity int) bool {
	cache.mu.Lock()
	defer cache.mu.Unlock()

	current := len(cache.entries)

	if current >= capacity*3/4 && current < capacity*2 {
		return false
	}

	if capacity < current {
		if int(cache.tail) > capacity {
			// this would invalidate too many indices
			return false
		}
	}

	cache.resize(capacity)
	return true
}

func (cache *Cache) GetStats(reset bool) (uint32, uint32, uint32, uint32) {
	cache.mu.Lock()
	defer cache.mu.Unlock()

	expected := cache.expected
	lost := cache.lost
	totalLost := cache.totalLost + cache.lost
	eseqno := uint32(cache.cycle)<<16 | uint32(cache.last)

	if reset {
		cache.expected = 0
		cache.totalLost += cache.lost
		cache.lost = 0
	}
	return expected, lost, totalLost, eseqno
}
