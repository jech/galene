package packetlist

import (
	"sync"
)

const BufSize = 1500

type entry struct {
	seqno  uint16
	length int
	buf    [BufSize]byte
}

type List struct {
	mu      sync.Mutex
	tail    int
	entries []entry
}

func New(capacity int) *List {
	return &List{
		entries: make([]entry, capacity),
	}
}

func (list *List) Store(seqno uint16, buf []byte) {
	list.mu.Lock()
	defer list.mu.Unlock()
	list.entries[list.tail].seqno = seqno
	copy(list.entries[list.tail].buf[:], buf)
	list.entries[list.tail].length = len(buf)
	list.tail = (list.tail + 1) % len(list.entries)

}

func (list *List) Get(seqno uint16) []byte {
	list.mu.Lock()
	defer list.mu.Unlock()

	for i := range list.entries {
		if list.entries[i].length == 0 ||
			list.entries[i].seqno != seqno {
			continue
		}
		buf := make([]byte, list.entries[i].length)
		copy(buf, list.entries[i].buf[:])
		return buf
	}
	return nil
}
