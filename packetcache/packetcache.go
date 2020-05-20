package packetcache

import (
	"sync"
)

const BufSize = 1500

type entry struct {
	seqno  uint16
	length uint16
	buf    [BufSize]byte
	pad    [32 - (BufSize+4)%32]byte // avoid false sharing
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
	first  uint16
	bitmap uint32
	// packet cache
	tail    int
	entries []entry
}

func New(capacity int) *Cache {
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

// Set a bit in the bitmap, shifting first if necessary.
func (cache *Cache) set(seqno uint16) {
	if cache.bitmap == 0 || seqnoInvalid(seqno, cache.first) {
		cache.first = seqno
		cache.bitmap = 1
		return
	}

	if ((seqno - cache.first) & 0x8000) != 0 {
		return
	}

	if seqno-cache.first < 32 {
		cache.bitmap |= (1 << uint16(seqno-cache.first))
		return
	}

	shift := seqno - cache.first - 31
	cache.bitmap >>= shift
	cache.first += shift
	cache.bitmap |= (1 << uint16(seqno-cache.first))
	return
}

// Store a packet, setting bitmap at the same time
func (cache *Cache) Store(seqno uint16, buf []byte) uint16 {
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

	cache.set(seqno)

	cache.entries[cache.tail].seqno = seqno
	copy(cache.entries[cache.tail].buf[:], buf)
	cache.entries[cache.tail].length = uint16(len(buf))
	cache.tail = (cache.tail + 1) % len(cache.entries)

	return cache.first
}

func (cache *Cache) Expect(n int) {
	if n <= 0 {
		return
	}
	cache.mu.Lock()
	defer cache.mu.Unlock()
	cache.expected += uint32(n)
}

func (cache *Cache) Get(seqno uint16, result []byte) uint16 {
	cache.mu.Lock()
	defer cache.mu.Unlock()

	for i := range cache.entries {
		if cache.entries[i].length == 0 ||
			cache.entries[i].seqno != seqno {
			continue
		}
		return uint16(copy(
			result[:cache.entries[i].length],
			cache.entries[i].buf[:]),
		)
	}
	return 0
}

// Shift 17 bits out of the bitmap.  Return a boolean indicating if any
// were 0, the index of the first 0 bit, and a bitmap indicating any
// 0 bits after the first one.
func (cache *Cache) BitmapGet() (bool, uint16, uint16) {
	cache.mu.Lock()
	defer cache.mu.Unlock()

	first := cache.first
	bitmap := (^cache.bitmap) & 0x1FFFF
	cache.bitmap >>= 17
	cache.first += 17

	if bitmap == 0 {
		return false, first, 0
	}

	for bitmap&1 == 0 {
		bitmap >>= 1
		first++
	}

	return true, first, uint16(bitmap >> 1)
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
