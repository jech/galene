// Package packetcache implement a packet cache that maintains a history
// of recently seen packets, the last keyframe, and a number of statistics
// that are needed for sending receiver reports.
package packetcache

import (
	"math/bits"
	"sync"
)

// The maximum size of packets stored in the cache.  Chosen to be
// a multiple of 8.
const BufSize = 1504

// entry represents a cached packet.
type entry struct {
	seqno           uint16
	lengthAndMarker uint16 // 1 bit of marker, 15 bits of length
	timestamp       uint32
	buf             [BufSize]byte
}

func (e *entry) length() uint16 {
	return e.lengthAndMarker & 0x7FFF
}

func (e *entry) marker() bool {
	return (e.lengthAndMarker & 0x8000) != 0
}

// bitmap keeps track of recent loss history
type bitmap struct {
	valid  bool
	first  uint16
	bitmap uint32
}

type Cache struct {
	mu sync.Mutex
	//stats
	last          uint16
	cycle         uint16
	lastValid     bool
	expected      uint32
	totalExpected uint32
	received      uint32
	totalReceived uint32
	// last seen keyframe
	keyframe      uint16
	keyframeValid bool
	// bitmap
	bitmap bitmap
	// the actual cache
	tail    uint16
	entries []entry
}

// New creates a cache with the given capacity.
func New(capacity int) *Cache {
	if capacity > int(^uint16(0)) {
		return nil
	}
	return &Cache{
		entries: make([]entry, capacity),
	}
}

// compare performs comparison modulo 2^16.
func compare(s1, s2 uint16) int {
	if s1 == s2 {
		return 0
	}
	if ((s2 - s1) & 0x8000) != 0 {
		return 1
	}
	return -1
}

// seqnoInvalid returns true if seqno is unreasonably far in the past
func seqnoInvalid(seqno, reference uint16) bool {
	if compare(reference, seqno) < 0 {
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

	if compare(bitmap.first, seqno) > 0 {
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
	if compare(first, next) >= 0 {
		return false, first, 0
	}
	count := next - first
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

// Store stores a packet in the cache.  It returns the first seqno in the
// bitmap, and the index at which the packet was stored.
func (cache *Cache) Store(seqno uint16, timestamp uint32, keyframe bool, marker bool, buf []byte) (uint16, uint16) {
	cache.mu.Lock()
	defer cache.mu.Unlock()

	if !cache.lastValid || seqnoInvalid(seqno, cache.last) {
		cache.last = seqno
		cache.lastValid = true
		cache.expected++
		cache.received++
	} else {
		cmp := compare(cache.last, seqno)
		if cmp < 0 {
			cache.received++
			cache.expected += uint32(seqno - cache.last)
			if seqno < cache.last {
				cache.cycle++
			}
			cache.last = seqno
			if cache.keyframeValid &&
				compare(cache.keyframe, seqno) > 0 {
				cache.keyframeValid = false
			}
		} else if cmp > 0 {
			if cache.received < cache.expected {
				cache.received++
			}
		}
	}
	cache.bitmap.set(seqno)

	if keyframe {
		cache.keyframe = seqno
		cache.keyframeValid = true
	}

	i := cache.tail
	cache.entries[i].seqno = seqno
	copy(cache.entries[i].buf[:], buf)
	lam := uint16(len(buf))
	if marker {
		lam |= 0x8000
	}
	cache.entries[i].lengthAndMarker = lam
	cache.entries[i].timestamp = timestamp
	cache.tail = (i + 1) % uint16(len(cache.entries))

	return cache.bitmap.first, i
}

// Expect records that we expect n additional packets.
func (cache *Cache) Expect(n int) {
	if n <= 0 {
		return
	}
	cache.mu.Lock()
	defer cache.mu.Unlock()
	cache.expected += uint32(n)
}

// get retrieves a packet from a slice of entries.
func get(seqno uint16, entries []entry, result []byte) (uint16, uint32, bool) {
	for i := range entries {
		if entries[i].lengthAndMarker == 0 || entries[i].seqno != seqno {
			continue
		}
		var n uint16
		if len(result) > 0 {
			n = uint16(copy(
				result[:entries[i].length()],
				entries[i].buf[:]))
		} else {
			n = entries[i].length()
		}
		return n, entries[i].timestamp, entries[i].marker()
	}
	return 0, 0, false
}

// Get retrieves a packet from the cache, returns the number of bytes
// copied.  If result is of length 0, returns the size of the packet.
func (cache *Cache) Get(seqno uint16, result []byte) uint16 {
	cache.mu.Lock()
	defer cache.mu.Unlock()

	n, _, _ := get(seqno, cache.entries, result)
	if n > 0 {
		return n
	}

	return 0
}

func (cache *Cache) Last() (uint16, bool) {
	cache.mu.Lock()
	defer cache.mu.Unlock()
	if !cache.lastValid {
		return 0, false
	}
	return cache.last, true
}

// GetAt retrieves a packet from the cache assuming it is at the given index.
func (cache *Cache) GetAt(seqno uint16, index uint16, result []byte) uint16 {
	cache.mu.Lock()
	defer cache.mu.Unlock()

	if int(index) >= len(cache.entries) {
		return 0
	}
	if cache.entries[index].seqno != seqno {
		return 0
	}
	return uint16(copy(
		result[:cache.entries[index].length()],
		cache.entries[index].buf[:]),
	)
}

// Keyframe returns the seqno of the last seen keyframe
func (cache *Cache) Keyframe() (uint16, bool) {
	cache.mu.Lock()
	defer cache.mu.Unlock()

	if !cache.keyframeValid {
		return 0, false
	}
	return cache.keyframe, true
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

// Resize resizes the cache to the given capacity.  This might invalidate
// indices of recently stored packets.
func (cache *Cache) Resize(capacity int) {
	cache.mu.Lock()
	defer cache.mu.Unlock()

	cache.resize(capacity)
}

// ResizeCond is like Resize, but avoids invalidating recent indices.
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

// Stats contains cache statistics
type Stats struct {
	Received, TotalReceived uint32
	Expected, TotalExpected uint32
	ESeqno                  uint32
}

// GetStats returns statistics about received packets.  If reset is true,
// the statistics are reset.
func (cache *Cache) GetStats(reset bool) Stats {
	cache.mu.Lock()
	defer cache.mu.Unlock()

	s := Stats{
		Received:      cache.received,
		TotalReceived: cache.totalReceived + cache.received,
		Expected:      cache.expected,
		TotalExpected: cache.totalExpected + cache.expected,
		ESeqno:        uint32(cache.cycle)<<16 | uint32(cache.last),
	}

	if reset {
		cache.totalExpected += cache.expected
		cache.expected = 0
		cache.totalReceived += cache.received
		cache.received = 0
	}
	return s
}

// ToBitmap takes a non-empty sorted list of seqnos, and computes a bitmap
// covering a prefix of the list.  It returns the part of the list that
// couldn't be covered.
func ToBitmap(seqnos []uint16) (first uint16, bitmap uint16, remain []uint16) {
	first = seqnos[0]
	bitmap = uint16(0)
	remain = seqnos[1:]
	for len(remain) > 0 {
		delta := remain[0] - first - 1
		if delta >= 16 {
			break
		}
		bitmap = bitmap | (1 << delta)
		remain = remain[1:]
	}
	return
}
