package packetcache

import (
	"sync"
)

const BufSize = 1500

type entry struct {
	seqno  uint16
	length int
	buf    [BufSize]byte
}

type Cache struct {
	mu      sync.Mutex
	first   uint16 // the first seqno
	bitmap  uint32
	tail    int // the next entry to be rewritten
	entries []entry
}

func New(capacity int) *Cache {
	return &Cache{
		entries: make([]entry, capacity),
	}
}

// Set a bit in the bitmap, shifting first if necessary.
func (cache *Cache) set(seqno uint16) {
	if cache.bitmap == 0 {
		cache.first = seqno
		cache.bitmap = 1
		return
	}

	if ((seqno - cache.first) & 0x8000) != 0 {
		return
	}

	if seqno == cache.first {
		cache.bitmap >>= 1
		cache.first += 1
		for (cache.bitmap & 1) == 1 {
			cache.bitmap >>= 1
			cache.first += 1
		}
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

	cache.set(seqno)

	cache.entries[cache.tail].seqno = seqno
	copy(cache.entries[cache.tail].buf[:], buf)
	cache.entries[cache.tail].length = len(buf)
	cache.tail = (cache.tail + 1) % len(cache.entries)

	return cache.first
}

func (cache *Cache) Get(seqno uint16) []byte {
	cache.mu.Lock()
	defer cache.mu.Unlock()

	for i := range cache.entries {
		if cache.entries[i].length == 0 ||
			cache.entries[i].seqno != seqno {
			continue
		}
		buf := make([]byte, cache.entries[i].length)
		copy(buf, cache.entries[i].buf[:])
		return buf
	}
	return nil
}

// Shift 17 bits out of the bitmap, return first index and remaining 16.
func (cache *Cache) BitmapGet() (uint16, uint16) {
	cache.mu.Lock()
	defer cache.mu.Unlock()

	first := cache.first
	bitmap := uint16((cache.bitmap >> 1) & 0xFFFF)
	cache.bitmap >>= 17
	cache.first += 17
	return first, bitmap
}
