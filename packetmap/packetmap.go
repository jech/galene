// Package packetmap implements remapping of sequence numbers and picture ids.
package packetmap

import (
	"sync"
)

const maxEntries = 128

type Map struct {
	mu        sync.Mutex
	next      uint16
	nextPid   uint16
	delta     uint16
	pidDelta  uint16
	lastEntry uint16
	entries   []entry
}

type entry struct {
	first, count uint16
	delta        uint16
	pidDelta     uint16
}

// Map maps a seqno, adding the mapping if required.  It returns whether
// the seqno could be mapped, the target seqno, and the pid delta to apply.
func (m *Map) Map(seqno uint16, pid uint16) (bool, uint16, uint16) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.delta == 0 && m.entries == nil {
		m.next = seqno + 1
		m.nextPid = pid
		return true, seqno, 0
	}

	if compare(m.next, seqno) <= 0 {
		if uint16(seqno-m.next) > 8*1024 {
			m.reset()
			m.next = seqno + 1
			m.nextPid = pid
			return true, seqno, 0
		}
		addMapping(m, seqno, pid, m.delta, m.pidDelta)
		m.next = seqno + 1
		m.nextPid = pid
		return true, seqno + m.delta, m.pidDelta
	}

	if uint16(m.next-seqno) > 8*1024 {
		m.reset()
		m.next = seqno + 1
		m.nextPid = pid
		return true, seqno, 0
	}

	return m.direct(seqno)
}

func (m *Map) reset() {
	m.next = 0
	m.nextPid = 0
	m.delta = 0
	m.pidDelta = 0
	m.lastEntry = 0
	m.entries = nil
}

func addMapping(m *Map, seqno, pid uint16, delta, pidDelta uint16) {
	if len(m.entries) == 0 {
		m.entries = []entry{
			entry{
				first:    seqno,
				count:    1,
				delta:    delta,
				pidDelta: pidDelta,
			},
		}
		return
	}

	i := m.lastEntry
	if delta == m.entries[i].delta && pidDelta == m.entries[i].pidDelta {
		m.entries[m.lastEntry].count = seqno - m.entries[i].first + 1
		return
	}

	e := entry{
		first:    seqno,
		count:    1,
		delta:    delta,
		pidDelta: pidDelta,
	}

	if len(m.entries) < maxEntries {
		m.entries = append(m.entries, e)
		m.lastEntry = uint16(len(m.entries) - 1)
		return
	}

	j := (m.lastEntry + 1) % maxEntries
	m.entries[j] = e
	m.lastEntry = j
}

// direct maps a seqno to a target seqno.  It returns true if the seqno
// could be mapped, the target seqno, and the pid delta to apply.
// Called with the m.mu taken.
func (m *Map) direct(seqno uint16) (bool, uint16, uint16) {
	if len(m.entries) == 0 {
		return false, 0, 0
	}
	i := m.lastEntry
	for {
		f := m.entries[i].first
		if seqno >= f {
			if seqno < f+m.entries[i].count {
				return true,
					seqno + m.entries[i].delta,
					m.entries[i].pidDelta
			}
			return false, 0, 0
		}
		if i > 0 {
			i--
		} else {
			i = uint16(len(m.entries) - 1)
		}
		if i == m.lastEntry {
			break
		}
	}
	return false, 0, 0
}

// Reverse maps a target seqno to the original seqno.  It returns true if
// the seqno could be mapped, the original seqno, and the pid delta to
// apply in reverse.
func (m *Map) Reverse(seqno uint16) (bool, uint16, uint16) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.delta == 0 && m.entries == nil {
		return true, seqno, 0
	}
	if m.entries == nil {
		if m.delta == 0 {
			return true, seqno, 0
		}
		return false, 0, 0
	}

	i := m.lastEntry
	for {
		f := m.entries[i].first + m.entries[i].delta
		if seqno >= f {
			if seqno < f+m.entries[i].count {
				return true,
					seqno - m.entries[i].delta,
					m.entries[i].pidDelta
			}
			return false, 0, 0
		}
		if i > 0 {
			i--
		} else {
			i = uint16(len(m.entries) - 1)
		}
		if i == m.lastEntry {
			break
		}
	}
	return false, 0, 0
}

// Drop attempts to record a dropped packet.  It returns true if the
// packet is safe to drop.
func (m *Map) Drop(seqno uint16, pid uint16) bool {
	m.mu.Lock()
	defer m.mu.Unlock()

	if seqno != m.next {
		return false
	}

	m.pidDelta += pid - m.nextPid
	m.nextPid = pid

	m.delta--
	m.next = seqno + 1
	return true
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
