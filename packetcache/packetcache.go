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

// The maximum number of packets that constitute a keyframe.
const maxFrame = 1024

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

// frame is used for storing the last keyframe
type frame struct {
	timestamp uint32
	complete  bool
	entries   []entry
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
	// bitmap
	bitmap bitmap
	// buffered keyframe
	keyframe frame
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

// insert inserts a packet into a frame.
func (frame *frame) insert(seqno uint16, timestamp uint32, marker bool, data []byte) bool {
	n := len(frame.entries)
	i := 0
	if n == 0 || seqno > frame.entries[n-1].seqno {
		// fast path
		i = n
	} else {
		for i < n {
			if frame.entries[i].seqno >= seqno {
				break
			}
			i++
		}

		if i < n && frame.entries[i].seqno == seqno {
			// duplicate
			return false
		}
	}

	if n >= maxFrame {
		// overflow
		return false
	}

	lam := uint16(len(data))
	if marker {
		lam |= 0x8000
	}
	e := entry{
		seqno:           seqno,
		lengthAndMarker: lam,
		timestamp:       timestamp,
	}
	copy(e.buf[:], data)

	if i >= n {
		frame.entries = append(frame.entries, e)
		return true
	}
	frame.entries = append(frame.entries, entry{})
	copy(frame.entries[i+1:], frame.entries[i:])
	frame.entries[i] = e
	return true
}

// store checks whether a packet is part of the current keyframe and, if
// so, inserts it.
func (frame *frame) store(seqno uint16, timestamp uint32, first bool, marker bool, data []byte) bool {
	if first {
		if frame.timestamp != timestamp {
			frame.timestamp = timestamp
			frame.complete = false
			frame.entries = frame.entries[:0]
		}
	} else if len(frame.entries) > 0 {
		if frame.timestamp != timestamp {
			delta := seqno - frame.entries[0].seqno
			if (delta&0x8000) == 0 && delta > 0x4000 {
				frame.complete = false
				frame.entries = frame.entries[:0]
			}
			return false
		}
	} else {
		return false
	}

	done := frame.insert(seqno, timestamp, marker, data)
	if done && !frame.complete {
		marker := false
		fst := frame.entries[0].seqno
		for i := 1; i < len(frame.entries); i++ {
			if frame.entries[i].seqno != fst+uint16(i) {
				return done
			}
			if frame.entries[i].marker() {
				marker = true
			}
		}
		if marker {
			frame.complete = true
		}
	}
	return done
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
		} else if cmp > 0 {
			if cache.received < cache.expected {
				cache.received++
			}
		}
	}
	cache.bitmap.set(seqno)

	done := cache.keyframe.store(seqno, timestamp, keyframe, marker, buf)
	if done && !cache.keyframe.complete {
		completeKeyframe(cache)
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

// completeKeyFrame attempts to complete the current keyframe.
func completeKeyframe(cache *Cache) {
	l := len(cache.keyframe.entries)
	if l == 0 {
		return
	}
	first := cache.keyframe.entries[0].seqno
	last := cache.keyframe.entries[l-1].seqno
	count := (last - first) // may wrap around
	if count > 0x4000 {
		// this shouldn't happen
		return
	}
	var buf []byte
	if count > 1 {
		if buf == nil {
			buf = make([]byte, BufSize)
		}
		for i := uint16(1); i < count; i++ {
			n, ts, marker := get(first+i, cache.entries, buf)
			if n > 0 {
				cache.keyframe.store(
					first+i, ts, false, marker, buf,
				)
			}
		}
	}
	if !cache.keyframe.complete {
		// Try to find packets after the last one.
		for {
			l := len(cache.keyframe.entries)
			if cache.keyframe.entries[l-1].marker() {
				break
			}
			if buf == nil {
				buf = make([]byte, BufSize)
			}
			seqno := cache.keyframe.entries[l-1].seqno + 1
			n, ts, marker := get(seqno, cache.entries, buf)
			if n <= 0 {
				break
			}
			done := cache.keyframe.store(
				seqno, ts, false, marker, buf,
			)
			if !done || marker {
				break
			}
		}
	}
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

	n, _, _ := get(seqno, cache.keyframe.entries, result)
	if n > 0 {
		return n
	}

	n, _, _ = get(seqno, cache.entries, result)
	if n > 0 {
		return n
	}

	return 0
}

func (cache *Cache) Last() (bool, uint16, uint32) {
	cache.mu.Lock()
	defer cache.mu.Unlock()
	if !cache.lastValid {
		return false, 0, 0
	}
	len, ts, _ := get(cache.last, cache.entries, nil)
	if len == 0 {
		return false, 0, 0
	}
	return true, cache.last, ts
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

// Keyframe returns the last buffered keyframe.  It returns the frame's
// timestamp and a boolean indicating if the frame is complete.
func (cache *Cache) Keyframe() (uint32, bool, []uint16) {
	cache.mu.Lock()
	defer cache.mu.Unlock()

	if len(cache.keyframe.entries) == 0 {
		return 0, false, nil
	}

	seqnos := make([]uint16, len(cache.keyframe.entries))
	for i := range cache.keyframe.entries {
		seqnos[i] = cache.keyframe.entries[i].seqno
	}
	return cache.keyframe.timestamp, cache.keyframe.complete, seqnos
}

func (cache *Cache) KeyframeSeqno() (bool, uint16, uint32) {
	cache.mu.Lock()
	defer cache.mu.Unlock()

	if len(cache.keyframe.entries) == 0 {
		return false, 0, 0
	}

	return true, cache.keyframe.entries[0].seqno, cache.keyframe.timestamp
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
